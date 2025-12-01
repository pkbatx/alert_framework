# AGENTS.md — Call Alert + Transcription Framework

This document is the single source of truth for how the alert + transcription service must behave. Any refactor, rewrite, or “framework” work must follow these rules.

---

## 1. High-Level Overview

Goal: Watch `/home/peebs/calls` for new audio files, immediately send a GroupMe alert with a public URL, transcribe the audio via OpenAI, store the transcript + metadata, and expose an HTTP API and HTML UI to retrieve both files and transcripts.

Current stack (reference implementation):

- Runtime: Python 3
- Web: FastAPI + Uvicorn
- Background work: `ThreadPoolExecutor` + inotify (`inotifywait`)
- Storage:
  - Audio: `/home/peebs/calls` (read-only)
  - Staging: `/home/peebs/ai_transcribe`
  - DB: SQLite at `/home/peebs/ai_transcribe/transcriptions.db`
- Integrations:
  - GroupMe bot: `https://api.groupme.com/v3/bots/post` with `bot_id=03926cdc985a046b27d6393ba6`
  - OpenAI: `OPENAI_API_KEY` env var, `gpt-4o-transcribe` model

Any future implementation in another language (Rust, Go, etc.) must keep these semantics and external behaviors stable.

---

## 2. Directories and Files

- **`/home/peebs/calls`**
  - Source of truth for uploaded call audio files.
  - Owned by FTP / existing production process.
  - This framework treats it as **read-only**.
  - All public URLs point to files in this directory.

- **`/home/peebs/ai_transcribe`**
  - Owned by this service.
  - Used for:
    - Staging copies of audio files for transcription.
    - SQLite DB: `transcriptions.db`.

- **Do NOT touch** `/home/peebs/transcribe`
  - That directory is used by another production process.
  - This system must not read/write/create/delete in that path.

---

## 3. Agents / Components

### 3.1 Watcher Agent

- Watches `/home/peebs/calls` using `inotifywait` for `create` and `moved_to` events.
- For each new file:
  - Immediately runs `pretty.sh <filename>` to get a human-readable description.
  - Sends a GroupMe message:
    - Format: `"<prettified> - https://calls.sussexcountyalerts.com/<urlencoded-filename>"`
    - Endpoint: `https://api.groupme.com/v3/bots/post`
    - Body: `{"bot_id": "03926cdc985a046b27d6393ba6", "text": "..."}`
  - Queues a transcription job (non-blocking) via a background worker.

### 3.2 Transcription Worker

- For a given source file in `/home/peebs/calls`:
  1. Writes/updates a row in `transcriptions` with:
     - `filename`
     - `source_path`
     - `status = 'processing'`
  2. Waits for the file to finish uploading:
     - Polls `st_size` every `interval` seconds.
     - Requires 2 consecutive equal, non-zero sizes to consider it “stable”.
  3. Copies the file to `/home/peebs/ai_transcribe/<filename>`.
  4. Calls OpenAI transcription:
     - `model = "gpt-4o-transcribe"`
     - `file = <staged file>`
  5. On success:
     - Updates DB row:
       - `status = 'done'`
       - `transcript_text = <OpenAI transcript>`
       - `last_error = NULL`
     - Sends GroupMe message with transcript:
       - `"<filename> transcript:\n<transcript>"`
  6. On failure:
     - Updates DB row:
       - `status = 'error'`
       - `last_error = "<error reason>"`

- Idempotency:
  - If `status` is already `processing` or `done` for a filename, do not start a duplicate job.

### 3.3 HTTP/API Server Agent

Single web app responsible for:

- Serving audio files from `/home/peebs/calls`.
- Exposing HTML for manual browsing.
- Exposing JSON APIs for automation.

Endpoints:

- `GET /`
  - HTML list of all files in `/home/peebs/calls`.
  - Each entry links to:
    - Raw file: `/<filename>`
    - Transcript API: `/api/transcription/<filename>`

- `GET /<filename-or-path>`
  - Returns raw file from `/home/peebs/calls`.
  - 404 if file does not exist.
  - `/api/...` paths are reserved for the API, not file serving.

- `GET /api/transcription/{filename}`
  - Behavior:
    - If audio file does not exist → 404.
    - If `transcriptions.filename` exists:
      - `status = 'done'` with non-null `transcript_text` → return transcript JSON.
      - `status = 'processing'` → return JSON `{ filename, status: 'processing' }`.
      - `status = 'error'` → queue a new job and return `{ filename, status: 'queued' }`.
    - If no row exists:
      - Queue a new job via worker.
      - Return `{ filename, status: 'queued' }`.

- `GET /api/transcriptions`
  - Returns a JSON list of all entries:
    - `filename`, `status`, `created_at`, `updated_at`.

---

## 4. Data Model

SQLite DB: `transcriptions` table

- `id` (INTEGER, PK, AUTOINCREMENT)
- `filename` (TEXT, UNIQUE, NOT NULL)
- `source_path` (TEXT, NOT NULL)
- `transcript_text` (TEXT, NULL)
- `status` (TEXT, NOT NULL) — one of:
  - `'processing'`
  - `'done'`
  - `'error'`
- `last_error` (TEXT, NULL)
- `created_at` (DATETIME, default `CURRENT_TIMESTAMP`)
- `updated_at` (DATETIME, updated on each change)

Upserts:

- On new or updated job, we must always bump `updated_at` and preserve `transcript_text` unless explicitly overwriting with a newer result.

---

## 5. Hard Rules for Any Implementation

Use these as “Guardrails” in your coding prompt:

1. **`/home/peebs/calls` is read-only.** Never modify, move, or delete files there. Only read + copy to a staging directory that this app owns.

2. **Use `/home/peebs/ai_transcribe` as the only staging + storage area** for AI work (copied audio + SQLite DB). Never touch `/home/peebs/transcribe` or other prod paths.

3. **Send GroupMe alerts immediately on new files** (non-blocking). The watcher must never wait for transcription before sending the initial alert.

4. **Always wait for upload completion before transcription.** Implement a size-stability check (same non-zero size across at least two intervals) before copying/transcribing any file.

5. **Use the DB as the single source of truth for transcription state.** Do not re-transcribe files with `status = 'done'`; expose status + transcripts via stable HTTP APIs (`/api/transcription/{filename}`, `/api/transcriptions`).

6. **For future rewrites/frameworks**, prefer a small, single-binary modern stack (e.g., Rust + Axum or Go + net/http) that preserves:
   - Directory layout and read-only guarantees.
   - GroupMe alert semantics.
   - OpenAI transcription behavior.
   - HTTP API surface and JSON contracts defined here.
