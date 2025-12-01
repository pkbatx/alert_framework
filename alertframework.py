#!/usr/bin/env python3
import os
import sys
import time
import shutil
import sqlite3
import threading
import subprocess
from pathlib import Path
from concurrent.futures import ThreadPoolExecutor
from urllib.parse import quote

import requests
from fastapi import FastAPI, HTTPException
from fastapi.responses import HTMLResponse, FileResponse, JSONResponse
from openai import OpenAI

# -----------------------------------------------------------------------------
# OpenAI client / env
# -----------------------------------------------------------------------------
OPENAI_API_KEY = os.getenv("OPENAI_API_KEY")
if not OPENAI_API_KEY:
    sys.stderr.write("ERROR: OPENAI_API_KEY env variable not set.\n")
    sys.exit(1)

client = OpenAI(api_key=OPENAI_API_KEY)

# -----------------------------------------------------------------------------
# Config
# -----------------------------------------------------------------------------
WEBHOOK_URL = "https://api.groupme.com/v3/bots/post"
BOT_ID = "03926cdc985a046b27d6393ba6"

TRANSCRIPT_WEBHOOK_URL = WEBHOOK_URL
TRANSCRIPT_BOT_ID = BOT_ID

# Prod call files (read-only)
CALLS_DIR = Path("/home/peebs/calls")

# Staging dir for AI transcription (separate from existing prod dirs)
AI_STAGE_DIR = Path("/home/peebs/ai_transcribe")
DB_PATH = AI_STAGE_DIR / "transcriptions.db"

PRETTY_SCRIPT = "./pretty.sh"
OPENAI_MODEL = "gpt-4o-transcribe"

INOTIFY_BIN = "inotifywait"
INOTIFY_ARGS = [
    INOTIFY_BIN,
    "-q",
    "-m",
    str(CALLS_DIR),
    "-e",
    "create",
    "-e",
    "moved_to",
    "--format",
    "%w|%e|%f",
]

# External URL base; nginx/proxy should map to this app
CALL_URL_BASE = "https://calls.sussexcountyalerts.com"

EXECUTOR = ThreadPoolExecutor(max_workers=4)

# -----------------------------------------------------------------------------
# DB helpers
# -----------------------------------------------------------------------------
def init_db() -> None:
    AI_STAGE_DIR.mkdir(parents=True, exist_ok=True)
    conn = sqlite3.connect(DB_PATH)
    conn.execute(
        """
        CREATE TABLE IF NOT EXISTS transcriptions (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            filename TEXT UNIQUE NOT NULL,
            source_path TEXT NOT NULL,
            transcript_text TEXT,
            status TEXT NOT NULL,
            last_error TEXT,
            created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
            updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
        );
        """
    )
    conn.commit()
    conn.close()


def db_update_transcription(
    filename: str,
    source_path: str,
    status: str,
    transcript_text: str | None = None,
    last_error: str | None = None,
) -> None:
    conn = sqlite3.connect(DB_PATH)
    conn.execute(
        """
        INSERT INTO transcriptions (filename, source_path, status, transcript_text, last_error, updated_at)
        VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
        ON CONFLICT(filename) DO UPDATE SET
            status = excluded.status,
            transcript_text = COALESCE(excluded.transcript_text, transcriptions.transcript_text),
            last_error = excluded.last_error,
            updated_at = CURRENT_TIMESTAMP;
        """,
        (filename, source_path, status, transcript_text, last_error),
    )
    conn.commit()
    conn.close()


def db_get_transcription(filename: str) -> dict | None:
    conn = sqlite3.connect(DB_PATH)
    conn.row_factory = sqlite3.Row
    cur = conn.execute(
        """
        SELECT filename, source_path, transcript_text, status, last_error, created_at, updated_at
        FROM transcriptions
        WHERE filename = ?
        """,
        (filename,),
    )
    row = cur.fetchone()
    conn.close()
    if not row:
        return None
    return dict(row)


# -----------------------------------------------------------------------------
# Helpers (GroupMe, file stability, copy, transcription)
# -----------------------------------------------------------------------------
def run_prettify(file_name: str) -> str:
    try:
        result = subprocess.run(
            [PRETTY_SCRIPT, file_name],
            check=True,
            capture_output=True,
            text=True,
        )
        return result.stdout.strip()
    except Exception as e:
        sys.stderr.write(f"[prettify] {e}\n")
        return file_name


def send_groupme(url: str, bot_id: str, text: str) -> None:
    if not url or not bot_id:
        return
    try:
        requests.post(url, json={"bot_id": bot_id, "text": text}, timeout=10)
    except Exception as e:
        sys.stderr.write(f"[groupme] {e}\n")


def wait_for_complete(src_path: Path, max_wait: float = 15.0, interval: float = 0.5) -> bool:
    """
    Wait for FTP upload to finish by watching file size.
    Read-only on CALLS_DIR.
    """
    elapsed = 0.0
    last_size = -1
    stable_count = 0

    while elapsed < max_wait:
        try:
            size = src_path.stat().st_size
        except FileNotFoundError:
            return False

        if size == last_size and size > 0:
            stable_count += 1
            if stable_count >= 2:
                return True
        else:
            stable_count = 0
            last_size = size

        time.sleep(interval)
        elapsed += interval

    sys.stderr.write(f"[wait_for_complete] timed out waiting for {src_path}\n")
    return False


def copy_to_stage(src_path: Path) -> Path:
    AI_STAGE_DIR.mkdir(parents=True, exist_ok=True)
    dst = AI_STAGE_DIR / src_path.name
    try:
        shutil.copy2(src_path, dst)
    except Exception as e:
        sys.stderr.write(f"[copy] {e}\n")
    return dst


def transcribe_file(audio_path: Path) -> str | None:
    try:
        with audio_path.open("rb") as f:
            tx = client.audio.transcriptions.create(
                model=OPENAI_MODEL,
                file=f,
            )
        return getattr(tx, "text", None)
    except Exception as e:
        sys.stderr.write(f"[transcribe] {audio_path}: {e}\n")
        return None


# -----------------------------------------------------------------------------
# Background transcription worker
# -----------------------------------------------------------------------------
def transcribe_worker(src_path: Path, file_name: str) -> None:
    db_update_transcription(
        filename=file_name,
        source_path=str(src_path),
        status="processing",
        transcript_text=None,
        last_error=None,
    )

    if not wait_for_complete(src_path):
        db_update_transcription(
            filename=file_name,
            source_path=str(src_path),
            status="error",
            last_error="file did not stabilize / disappeared",
        )
        return

    staged = copy_to_stage(src_path)

    transcript = transcribe_file(staged)
    if not transcript:
        db_update_transcription(
            filename=file_name,
            source_path=str(src_path),
            status="error",
            last_error="transcription failed",
        )
        return

    db_update_transcription(
        filename=file_name,
        source_path=str(src_path),
        status="done",
        transcript_text=transcript,
        last_error=None,
    )

    msg = f"{file_name} transcript:\n{transcript}"
    send_groupme(TRANSCRIPT_WEBHOOK_URL, TRANSCRIPT_BOT_ID, msg)


def ensure_transcription_queued(src_path: Path, file_name: str) -> str:
    """
    Ensures a transcription job exists if needed.
    Returns current/queued status.
    """
    row = db_get_transcription(file_name)
    if row and row["status"] in ("processing", "done"):
        return row["status"]

    if not src_path.is_file():
        raise FileNotFoundError(src_path)

    EXECUTOR.submit(transcribe_worker, src_path, file_name)
    return "queued"


# -----------------------------------------------------------------------------
# inotify watcher (alerts + background job)
# -----------------------------------------------------------------------------
def watcher_loop() -> None:
    try:
        subprocess.run(
            [INOTIFY_BIN, "--help"],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
            check=False,
        )
    except FileNotFoundError:
        sys.stderr.write("inotifywait not installed; watcher disabled.\n")
        return

    proc = subprocess.Popen(
        INOTIFY_ARGS,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
        bufsize=1,
    )

    sys.stderr.write(f"[watcher] Watching {CALLS_DIR}...\n")

    try:
        assert proc.stdout is not None

        for line in proc.stdout:
            line = line.strip()
            if not line:
                continue

            try:
                path_str, events, file_name = line.split("|", 2)
            except ValueError:
                sys.stderr.write(f"[watcher parse] {line!r}\n")
                continue

            src_path = Path(path_str) / file_name

            pretty = run_prettify(file_name)
            file_url = f"{CALL_URL_BASE}/{quote(file_name)}"
            alert_text = f"{pretty} - {file_url}"
            send_groupme(WEBHOOK_URL, BOT_ID, alert_text)

            try:
                ensure_transcription_queued(src_path, file_name)
            except FileNotFoundError:
                sys.stderr.write(f"[watcher] file vanished: {src_path}\n")
                continue

    except KeyboardInterrupt:
        sys.stderr.write("[watcher] stopping\n")
    finally:
        try:
            proc.terminate()
        except Exception:
            pass


# -----------------------------------------------------------------------------
# FastAPI app
# -----------------------------------------------------------------------------
app = FastAPI(title="Call Audio + Transcription Server")


@app.on_event("startup")
async def startup_event():
    init_db()
    t = threading.Thread(target=watcher_loop, daemon=True)
    t.start()


@app.get("/", response_class=HTMLResponse)
def index():
    files = sorted(p.name for p in CALLS_DIR.iterdir() if p.is_file())
    items = "\n".join(
        f'<li><a href="/{quote(name)}">{name}</a> '
        f'- <a href="/api/transcription/{quote(name)}">transcription</a></li>'
        for name in files
    )
    html = f"""
    <html>
      <head><title>Call Recordings</title></head>
      <body>
        <h1>Call Recordings</h1>
        <ul>
          {items}
        </ul>
      </body>
    </html>
    """
    return HTMLResponse(content=html)


@app.get("/{file_path:path}")
def get_audio_file(file_path: str):
    # Let /api/* be handled by API routes
    if file_path.startswith("api/"):
        raise HTTPException(status_code=404, detail="Not found")

    path = CALLS_DIR / file_path
    if not path.is_file():
        raise HTTPException(status_code=404, detail="File not found")
    return FileResponse(path)


@app.get("/api/transcription/{file_name}", response_class=JSONResponse)
def get_or_trigger_transcription(file_name: str):
    src_path = CALLS_DIR / file_name
    if not src_path.is_file():
        raise HTTPException(status_code=404, detail="Audio file not found")

    row = db_get_transcription(file_name)

    if row and row["status"] == "done" and row["transcript_text"]:
        return {
            "filename": row["filename"],
            "status": row["status"],
            "transcript": row["transcript_text"],
            "created_at": row["created_at"],
            "updated_at": row["updated_at"],
        }

    if row and row["status"] == "processing":
        return {
            "filename": row["filename"],
            "status": "processing",
        }

    try:
        status = ensure_transcription_queued(src_path, file_name)
    except FileNotFoundError:
        raise HTTPException(status_code=404, detail="Audio file not found")

    return {
        "filename": file_name,
        "status": status,
    }


@app.get("/api/transcriptions", response_class=JSONResponse)
def list_transcriptions():
    conn = sqlite3.connect(DB_PATH)
    conn.row_factory = sqlite3.Row
    cur = conn.execute(
        "SELECT filename, status, created_at, updated_at "
        "FROM transcriptions ORDER BY created_at DESC"
    )
    rows = [dict(r) for r in cur.fetchall()]
    conn.close()
    return rows


# -----------------------------------------------------------------------------
# Entry point
# -----------------------------------------------------------------------------
if __name__ == "__main__":
    import uvicorn

    # Run via: python3 alert_framework.py
    uvicorn.run(
        app,
        host="0.0.0.0",
        port=8000,
        reload=False,
    )
