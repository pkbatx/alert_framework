from __future__ import annotations

import json
import shutil
import time
from collections import deque
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Deque, Dict

from jsonschema import validate as jsonschema_validate
from jsonschema.exceptions import ValidationError

from caad_skillkit.ai.rollups import SkillError, build_metadata, build_rollup
from caad_skillkit.ai.schemas import get_schema
from caad_skillkit.ai.transcribe import TranscribeError, transcribe_audio
from caad_skillkit.config import get_env, load_env_file
from core import store

ALLOWED_EXTENSIONS = {".mp3", ".wav", ".m4a", ".aac", ".flac", ".ogg"}


class WorkerError(RuntimeError):
    pass


def _load_env() -> None:
    load_env_file(Path.cwd() / ".env")


def _emit_event(event: str, payload: Dict[str, Any]) -> None:
    message = {"event": event, **payload}
    print(json.dumps(message, ensure_ascii=False))


def _normalize_mode(value: str | None) -> str:
    if not value:
        return "copy"
    value = value.lower()
    if value not in {"copy", "move"}:
        return "copy"
    return value


def _relative_path(path: Path) -> str:
    try:
        return str(path.resolve().relative_to(store.REPO_ROOT))
    except ValueError:
        return str(path)


def _validate_payload(schema_type: str, payload: Dict[str, Any]) -> None:
    try:
        jsonschema_validate(payload, get_schema(schema_type))
    except ValidationError as err:
        raise WorkerError(f"{schema_type} validation failed: {err.message}") from err


def _call_record_template(
    call_id: str,
    calls_dir: Path,
    original_filename: str,
    stored_audio_path: Path,
    transcript_path: Path,
    metadata_path: Path,
    rollup_path: Path,
    status: str,
    error: str | None = None,
) -> Dict[str, Any]:
    payload: Dict[str, Any] = {
        "id": call_id,
        "ts": datetime.now(timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z"),
        "source": str(calls_dir),
        "original_filename": original_filename,
        "stored_audio_path": _relative_path(stored_audio_path),
        "transcript_path": _relative_path(transcript_path),
        "metadata_path": _relative_path(metadata_path),
        "rollup_path": _relative_path(rollup_path),
        "status": status,
    }
    if error:
        payload["error"] = error
    return payload


def process_file(path: Path, calls_dir: Path, copy_mode: str | None = None) -> Dict[str, Any]:
    _load_env()
    store.ensure_runtime_layout()

    file_path = path.expanduser().resolve()
    if not file_path.exists():
        raise WorkerError(f"file not found: {file_path}")

    call_id = store.call_id_from_audio(str(file_path))
    paths = store.paths_for_call(call_id)
    record_path = Path(paths["call_record"])
    if record_path.exists():
        try:
            return json.loads(record_path.read_text())
        except json.JSONDecodeError:
            pass

    audio_dir = Path(paths["audio"])
    audio_dir.mkdir(parents=True, exist_ok=True)
    dest_audio_path = audio_dir / file_path.name
    transcript_path = Path(paths["transcript"])
    metadata_path = Path(paths["metadata"])
    rollup_path = Path(paths["rollup"])

    mode = _normalize_mode(copy_mode)
    try:
        if mode == "move":
            shutil.move(str(file_path), str(dest_audio_path))
        else:
            shutil.copy2(str(file_path), str(dest_audio_path))
    except Exception as err:
        error = str(err)[:240]
        record = _call_record_template(
            call_id,
            calls_dir,
            file_path.name,
            dest_audio_path,
            transcript_path,
            metadata_path,
            rollup_path,
            "error",
            error,
        )
        store.write_call_record(record)
        return record

    try:
        transcript = transcribe_audio(str(dest_audio_path))
        _validate_payload("transcription", transcript)
        store.write_json_atomic(transcript_path.as_posix(), transcript)

        metadata = build_metadata(transcript.get("text", ""))
        _validate_payload("metadata", metadata)
        store.write_json_atomic(metadata_path.as_posix(), metadata)

        rollup = build_rollup(transcript.get("text", ""))
        _validate_payload("rollup", rollup)
        store.write_json_atomic(rollup_path.as_posix(), rollup)

        record = _call_record_template(
            call_id,
            calls_dir,
            file_path.name,
            dest_audio_path,
            transcript_path,
            metadata_path,
            rollup_path,
            "complete",
        )
        store.write_call_record(record)
        return record
    except (SkillError, TranscribeError, WorkerError, ValidationError) as err:
        error = str(err)[:240]
        record = _call_record_template(
            call_id,
            calls_dir,
            file_path.name,
            dest_audio_path,
            transcript_path,
            metadata_path,
            rollup_path,
            "error",
            error,
        )
        store.write_call_record(record)
        return record


def run_watcher() -> None:
    _load_env()
    store.ensure_runtime_layout()

    calls_dir = Path(get_env("CALLS_DIR") or "runtime/inbox")
    ingest_mode = _normalize_mode(get_env("INGEST_COPY_MODE") or "copy")
    poll_s = float(get_env("POLL_S") or "2")
    stable_polls = int(get_env("STABLE_POLLS") or "2")
    if stable_polls < 1:
        stable_polls = 1

    calls_dir.mkdir(parents=True, exist_ok=True)

    size_history: Dict[Path, Deque[int]] = {}
    processed: Dict[Path, Dict[str, Any]] = {}
    eligible_emitted: set[Path] = set()

    _emit_event("watch_start", {"calls_dir": str(calls_dir), "poll_s": poll_s, "stable_polls": stable_polls})

    try:
        while True:
            for entry in calls_dir.iterdir():
                if not entry.is_file():
                    continue
                name = entry.name
                if name.startswith(".") or ".writetest-" in name:
                    continue
                if entry.suffix.lower() not in ALLOWED_EXTENSIONS:
                    continue

                try:
                    stat = entry.stat()
                except FileNotFoundError:
                    continue

                if entry in processed:
                    prev = processed[entry]
                    if prev.get("status") == "error" and prev.get("size") != stat.st_size:
                        processed.pop(entry, None)
                    else:
                        continue

                history = size_history.setdefault(entry, deque(maxlen=stable_polls))
                history.append(stat.st_size)
                if len(history) < stable_polls or len(set(history)) != 1:
                    continue

                if entry not in eligible_emitted:
                    _emit_event("detected", {"path": str(entry)})
                    eligible_emitted.add(entry)

                _emit_event("processing", {"path": str(entry)})
                record = process_file(entry, calls_dir, ingest_mode)
                processed[entry] = {"status": record.get("status"), "size": stat.st_size}
                if record.get("status") == "complete":
                    _emit_event("complete", {"call_id": record.get("id")})
                else:
                    _emit_event("error", {"call_id": record.get("id"), "error": record.get("error", "")})

            time.sleep(poll_s)
    except KeyboardInterrupt:
        _emit_event("shutdown", {"reason": "interrupt"})
