
## 1. Purpose

This service watches a call-recordings directory, normalizes filenames, pushes alerts to GroupMe, and provides a UI to review calls and AI-generated transcripts.

Core behaviors:

- Detect new `.mp3` call files (live + backfill).
- Immediately send a **prettified “call started” alert** to the assigned GroupMe channel.
- Use OpenAI **speech-to-text** to generate a transcript for each call.
- Send a **second “call transcript” alert** to the same GroupMe channel using the **same formatting**, with the transcript appended.
- Store calls + transcripts in a local DB.
- Expose a UI that:
  - Shows the **last 24 hours** of calls by default.
  - Lets you **view** existing transcripts.
  - Lets you **generate/regenerate** a transcript on demand if one is missing/stale.

This document is the source of truth for how the agents (watcher, backfill, queue/workers, AI pipeline, UI) must behave.

---

## 2. High-Level Architecture

### Components

- **Config**
  - Reads env vars into a `Config` struct (paths, ports, GroupMe, OpenAI, queue, backfill, DB).
  - Provides safe defaults and clamping (e.g., backfill limit caps).

- **Queue / Worker Pool**
  - Single shared in-process job queue (bounded channel).
  - Fixed number of workers handling jobs concurrently.
  - All work (live calls, backfill, on-demand transcription) flows through this queue.

- **File Watcher**
  - Watches `CALLS_DIR` (e.g., `/home/peebs/calls`) for new/moved-in `.mp3` files.
  - On new file:
    - Builds a `Job` with parsed metadata.
    - Enqueues the job to the shared queue.
    - Job type: `"call_ingest"`.

- **Backfill**
  - On startup, scans `CALLS_DIR` for historical `.mp3`s.
  - Determines which calls are already processed vs unprocessed.
  - Selects at most `BACKFILL_LIMIT` **unprocessed** newest calls.
  - Enqueues them as `"call_ingest"` jobs.
  - Updates `LastBackfill` stats for metrics + UI.

- **Formatter / Metadata (Prettify)**
  - Parses filenames into `CallMetadata`:
    - `AgencyDisplay`, `TownDisplay`, `CallType`, `DateTime`, `RawFileName`.
    - Example filename: `Glenwood-Pochuck_EMS_2025_11_27_19_58_13.mp3`.
  - Generates a prettified title similar to the original `pretty.sh`:
    - Strips digits/underscores and `.mp3`.
    - Normalizes tokens (`TWP`, `FD`, `Gen`, `Duty` as separate words).
    - Adds spaces between lower→upper boundaries.
    - Timestamp suffix: `HH:MM on M/D/YYYY` in Eastern time.
  - Builds final alert text templates that are reused by both the initial alert and the transcript follow-up.

- **AI Transcription Pipeline**
  - Uses OpenAI’s `/v1/audio/transcriptions` with models like:
    - `gpt-4o-transcribe` (higher quality), or
    - `gpt-4o-mini-transcribe` (cheaper/faster).
  - Sends `.mp3` contents to the OpenAI API and stores the returned text in the DB.
  - Optionally uses `prompt` for improved domain accuracy (agency names, streets, acronyms).

- **Notifier (GroupMe)**
  - Sends messages to one or more GroupMe channels:
    - First message: “Call Alert” (no transcript).
    - Second message: “Call Alert + Transcript”.
  - Uses the same layout for both, just with transcript content appended on the second pass.

- **Local Store (DB)**
  - SQLite DB at `TRANSCRIPT_DB_PATH` (e.g., `/home/peebs/ai_transcribe/transcriptions.db`).
  - Tracks:
    - Call file (path, filename, metadata).
    - Timestamps (detected, alerted, transcribed).
    - Transcript text and status (`pending`, `success`, `failed`).
    - Group/channel routing info.

- **HTTP Server + UI**
  - Serves:
    - `/` – main UI for calls and transcripts.
    - `/healthz` – liveness.
    - `/readyz` – readiness.
    - `/debug/queue` – JSON queue + metrics.
  - UI shows the **last 24 hours of calls** by default, with controls to:
    - Filter by time range, call type, agency.
    - Trigger “(Re)Generate transcript” per call (enqueue job).
    - View call details + transcript.

---

## 3. Configuration

### Core

- `HTTP_PORT`
  - Default: `:8000`.

- `CALLS_DIR`
  - Directory for call audio.
  - Default: `/home/peebs/calls`.

- `TRANSCRIPT_DB_PATH`
  - SQLite file for metadata + transcripts.
  - Default: `/home/peebs/ai_transcribe/transcriptions.db`.

### Backfill / Queue

- `BACKFILL_LIMIT`
  - Max number of **unprocessed** calls to backfill on startup.
  - Default: `15`.
  - Hard max: `50`.

- `JOB_QUEUE_SIZE`
  - Channel capacity for jobs.
  - Default: `100` (clamped to a sane max, e.g. `1024`).

- `WORKER_COUNT`
  - Number of worker goroutines.
  - Default: `4` (min `1`).

### GroupMe / Webhooks

- `GROUPME_BOT_ID`
- `GROUPME_ACCESS_TOKEN`
- Optional routing env vars for different agencies/channels.

### OpenAI Transcription

- `OPENAI_API_KEY` (required).
- `OPENAI_TRANSCRIBE_MODEL`
  - Default: `gpt-4o-mini-transcribe`.
  - Alternative: `gpt-4o-transcribe`, `whisper-1`.
- `OPENAI_TRANSCRIBE_TIMEOUT_SECONDS`
  - Default: e.g. `60`.

---

## 4. Job Model & Types

### CallMetadata

Conceptual struct (language-agnostic):

- `RawFileName`
- `AgencyDisplay`
- `TownDisplay`
- `CallType`
- `DateTime` (parsed from filename + Eastern TZ)

### Job

Core job types:

1. **`"call_ingest"`**
   - Triggered by watcher or backfill.
   - Steps:
     1. Parse `CallMetadata` from filename.
     2. Send **initial alert** to GroupMe (prettified, no transcript).
     3. Insert/update DB record for the call:
        - Mark status `transcription_pending`.
     4. Enqueue a follow-up `"transcription"` job (or perform transcription inline if acceptable).

2. **`"transcription"`**
   - Triggered automatically after `"call_ingest"` or from UI on demand.
   - Steps:
     1. Read audio file from disk.
     2. Call OpenAI `/v1/audio/transcriptions`:
        - model: `OPENAI_TRANSCRIBE_MODEL`.
        - file: `.mp3` stream.
        - response_format: `text` or `json` with `.text`.
     3. Update DB with transcript text, mark status `success` or `failed`.
     4. Send **second alert** to GroupMe:
        - Same base layout as initial alert.
        - Adds a `Transcript:` section at the bottom.

3. **(Optional) `"transcription_regen"`**
   - Triggered by UI “Regenerate transcript” button.
   - Same as `"transcription"`, but overwrites existing text and logs a regen event.

Jobs share:

- `Source` – `"watcher"`, `"backfill"`, `"ui"`.
- `FilePath`, `FileName`.
- `Meta` – `CallMetadata`.
- `JobType`.

---

## 5. Alert Formatting (Prettify + Templates)

### Prettified Title

Behavior equivalent to the original `pretty.sh`:

- Input filename: `Glenwood-Pochuck_EMS_2025_11_27_19_58_13.mp3`.
- Steps:
  - Strip digits and underscores and `.mp3`.
  - Insert spaces between lower→upper letter boundaries.
  - Make `TWP`, `FD`, `Gen`, `Duty` separate tokens.
  - Collapse whitespace.
  - Add Eastern timestamp suffix: `HH:MM on M/D/YYYY`.
- Example output:
  - `Glenwood-Pochuck EMS at 18:40 on 12/1/2025`.

### CallMetadata-derived Fields

From filename:

- `CallType`: e.g. `EMS`, `FIRE`, `GEN`.
- `AgencyDisplay`: e.g. `Glenwood-Pochuck`.

### Alert Template (First Message)

**Initial “Call Alert” GroupMe message:**

- Uses prettified header + metadata.
- No transcript yet.

Example layout:

```text
Glenwood-Pochuck EMS at 18:40 on 12/1/2025
Call type: EMS
Agency/Town: Glenwood-Pochuck
Listen: https://calls.sussexcountyalerts.com/<path>

This message is sent as soon as the file is detected / ingested, before AI transcription.

Alert Template (Second Message with Transcript)

Follow-up “Call Transcript” GroupMe message:
	•	Same header + fields as the initial alert.
	•	Adds a Transcript: section.

Example:

Glenwood-Pochuck EMS at 18:40 on 12/1/2025
Call type: EMS
Agency/Town: Glenwood-Pochuck
Listen: https://calls.sussexcountyalerts.com/<path>

Transcript:
<full text from OpenAI transcription>

Rules:
	•	Both messages go to the same GroupMe channel used today.
	•	Second message should only send when a transcript exists and is marked success.

⸻

6. Transcription Pipeline (OpenAI)

API Usage

For each "transcription" job:
	•	Call POST /v1/audio/transcriptions with:
	•	Authorization: Bearer $OPENAI_API_KEY
	•	file: audio .mp3.
	•	model: OPENAI_TRANSCRIBE_MODEL (e.g., gpt-4o-mini-transcribe).
	•	response_format: text (simple) or json (then use .text).

Basic contract:
	•	Input ≤ 25 MB per file.
	•	Supported formats: .mp3 (already what we have).
	•	Timeout controlled by OPENAI_TRANSCRIBE_TIMEOUT_SECONDS.

Error Handling
	•	On API error/timeout:
	•	Mark transcript status failed with error payload (for debugging).
	•	Do not send transcript alert.
	•	The UI should show this call as “Transcript failed” and allow manual retry (enqueue "transcription_regen").

⸻

7. UI Behavior

Default View
	•	Shows last 24 hours of calls by default:
	•	Based on file timestamp or parsed DateTime.
	•	Each row/card includes:
	•	Time.
	•	Agency/Town.
	•	Call type.
	•	Source: watcher vs backfill.
	•	Transcript status: pending, success, failed.
	•	Actions:
	•	“View transcript” (if exists).
	•	“Generate transcript” or “Regenerate” (if missing/failed).

Transcript On-Demand
	•	When user clicks “Generate transcript”:
	•	Enqueue a "transcription_regen" job.
	•	UI marks as pending.
	•	When job completes:
	•	Show transcript in UI.
	•	Optionally send the transcript alert to GroupMe (same layout as above).

System Status Panel
	•	Shows:
	•	Queue length / capacity / workers.
	•	Processed/failed job counts.
	•	Last backfill stats (total, unprocessed, selected, enqueued, dropped_full, other_errors, already_processed).
	•	Data is sourced from a central Metrics struct and the queue (/debug/queue).

⸻

8. Lifecycle & Idempotency
	•	Initial alert idempotency:
	•	For each file, store a DB row keyed by filename/path.
	•	Only send the initial alert once per call (unless explicitly retried).
	•	Transcript idempotency:
	•	If status == success and transcript text is present, skip auto-regeneration.
	•	UI-driven “Regenerate” can override this but should be explicit.
	•	Backfill:
	•	On restart, backfill:
	•	Ignores calls that already have DB entries + transcript status.
	•	Only re-enqueues unprocessed calls up to BACKFILL_LIMIT.

⸻

9. Extension Guidelines

When adding new capabilities:
	•	Do not bypass the shared queue; all work should be jobs.
	•	Use CallMetadata + prettified formatting for all user-facing text.
	•	Keep GroupMe and other notifiers using the same two-stage alert pattern:
	•	Quick initial alert.
	•	Follow-up with AI transcript.
	•	Keep configs centralized and documented here.

This AGENTS.md is the reference for any future Codex/Copilot changes.

