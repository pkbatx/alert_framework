Yeah, it’ll help a lot—for you and for future Codex/Copilot runs. Here’s an updated AGENTS.md you can drop into the repo (or overwrite the old one).

# Alert Framework – Agents Guide

## 1. Purpose

This service watches a call recordings directory, queues work for transcription + alert delivery, and exposes a small HTTP API for viewing transcriptions and health.

It is built to:

- Process **new** calls as they arrive.
- Safely **backfill** the **last N unprocessed** calls on startup (default: 15).
- Run all work through a **bounded, observable job queue** with a fixed worker pool.

This doc is the source of truth for how the agents (watcher, backfill, workers, HTTP) are supposed to behave.

---

## 2. High-Level Architecture

### Components

- **Config**
  - Parses env vars into a `Config` struct.
  - Controls queue size, worker count, backfill limit, ports, paths, tokens.

- **Queue / Worker Pool**
  - Single shared in-process job queue.
  - Fixed-size buffered channel for jobs.
  - N worker goroutines pulling from the queue and executing jobs.

- **Backfill**
  - On startup, inspects historical calls in `/home/peebs/calls` (or configured path).
  - Selects at most **BACKFILL_LIMIT** unprocessed files (newest first).
  - Attempts to enqueue them into the shared job queue.
  - Logs a detailed summary: what it saw, what it selected, what it actually enqueued.

- **Watcher**
  - Uses fsnotify (or similar) to monitor the call directory.
  - On new file/move-in, builds a job and enqueues it into the same queue.
  - Identified as `job_source=watcher` in logs.

- **Processor**
  - Worker function that executes a pipeline per job:
    - Load + validate file.
    - Run transcription.
    - Push alert(s) to downstream (e.g., GroupMe, DB).
  - Logs per-step timings and outcome.

- **HTTP Server**
  - Serves:
    - `/api/transcriptions` (UI/data for current transcriptions).
    - `/healthz` (liveness).
    - `/readyz` (readiness).
    - `/debug/queue` (internal queue + backfill stats; JSON).

---

## 3. Configuration (Env Vars)

These names may be implemented slightly differently in code; this is the intended contract.

### Core

- `HTTP_PORT`
  - Port for the HTTP server (e.g., `:8000`).
  - Default: `:8000` if unset.

- `CALLS_DIR`
  - Directory to watch for call audio (default: `/home/peebs/calls`).

### Backfill

- `BACKFILL_LIMIT`
  - Max number of **unprocessed** calls to backfill on startup.
  - Default: `15`.
  - Hard max enforced in config: `50`.
  - Values > 50 are clamped to 50 with a warning.

Semantics:

- Backfill considers **all files** in `CALLS_DIR`.
- Filters out those already marked as “processed” (based on DB/metadata).
- Sorts remaining unprocessed files **newest → oldest**.
- Picks at most `BACKFILL_LIMIT` of those.
- Attempts to enqueue each into the queue.

### Queue / Worker Pool

- `JOB_QUEUE_SIZE`
  - Capacity of the in-memory job channel.
  - Default: `100`.
  - Hard max: e.g., `1024` (implementation detail).
  - Must be >= workers; invalid values fall back to defaults.

- `WORKER_COUNT`
  - Number of worker goroutines.
  - Default: `4`.
  - Minimum `1`; invalid values fall back to default.

### External Integrations

(Names may vary; keep in sync with actual code.)

- `GROUPME_BOT_ID`
- `GROUPME_ACCESS_TOKEN`
- Any transcription API keys / base URLs.
- DB DSN or path for storing transcriptions/metadata.

---

## 4. Job Model

### Job Fields (conceptual)

Each job represents processing a single call file:

- `Source` – `"backfill"` or `"watcher"` (may be extended).
- `FileName` – basename (e.g., `Glenwood-Pochuck_EMS_2025_11_27_19_58_13.mp3`).
- `FullPath` – absolute path to the file.
- `CreatedAt` – derived from filename or fs metadata when available.
- `Attempts` – for future retry policies (currently simple one-shot).

There is exactly **one** queue instance created in `main` and shared by all producers and workers.

---

## 5. Backfill Semantics

### Startup Sequence (Happy Path)

1. Config is loaded (limit, queue size, worker count, paths).
2. Queue is created with configured capacity.
3. Worker pool is started.
4. HTTP server is started (listening on `HTTP_PORT`).
5. Watcher is started on `CALLS_DIR`.
6. Backfill is launched in its own goroutine:
   - Scans `CALLS_DIR`.
   - Classifies files as **already processed** vs **unprocessed**.
   - Selects up to `BACKFILL_LIMIT` newest unprocessed files.
   - Attempts to enqueue each into the queue.

### Backfill Logging

On startup you should see logs like:

```text
starting backfill with limit=15 queue_size=100 workers=4
server listening on :8000
watching /home/peebs/calls for new files
backfill summary: total=393 unprocessed=389 selected=15 enqueued=15 dropped_full=0 other_errors=0 already_processed=4
queue stats: length=15 capacity=100 workers=4

Definitions:
	•	total – total files encountered in the directory.
	•	already_processed – files skipped because they’re already done.
	•	unprocessed – files that appear not yet processed.
	•	selected – min(unprocessed, BACKFILL_LIMIT).
	•	enqueued – jobs actually put onto the queue successfully.
	•	dropped_full – jobs that could not be enqueued because the queue stayed full after bounded retries.
	•	other_errors – any enqueue failures that were not “queue is full.”

If selected > 0 and enqueued == 0, there is a bug or misconfiguration and logs around enqueue will state why.

⸻

6. Queue Behavior

Enqueue

Producers (backfill, watcher, HTTP-triggered jobs) call queue.Enqueue(ctx, job):
	•	If channel has capacity:
	•	Job is pushed, returns nil.
	•	If channel is full:
	•	Implementation uses a bounded retry:
	•	Non-blocking initial attempt.
	•	Brief retries over a small window (e.g., up to ~5s).
	•	If still full after that window:
	•	Returns ErrQueueFull.
	•	Backfill increments dropped_full and logs once per backfill run.
	•	If ctx is cancelled:
	•	Returns ctx.Err().

Workers
	•	Number of workers = WORKER_COUNT.
	•	Each worker:
	•	Loops on the job channel.
	•	Wraps each job in a per-job context with timeout (e.g., 60s).
	•	Executes the processing pipeline.
	•	Logs detailed timing.

Shutdown:
	•	On SIGINT/SIGTERM, main cancels a root context.
	•	New enqueues stop.
	•	Workers finish in-flight jobs up to a grace period.
	•	Queue metrics are logged on exit.

⸻

7. Processing Pipeline

For each job (both backfill and watcher):
	1.	Decode / Input Prep
	•	Open the file, validate it’s a supported audio type.
	•	Normalize paths, extract metadata from filename if needed.
	2.	Transcription
	•	Call the configured transcription endpoint.
	•	Handle network errors, timeouts, non-200s.
	•	Store transcription into local DB/file-based store.
	3.	Notification
	•	Format a message (system + agency + summary + link).
	•	POST to GroupMe or other downstream channel.
	•	Update local state to mark file as “processed”.

Timing Logs

Each job log should include per-step timings, for example:

job_source=backfill file=Alamuchy-Green_EMS_2025_12_01_12_40_51.mp3 total=11.5s decode=0.2s transcribe=10.8s notify=0.3s status=success

These timings make it obvious where latency is coming from (transcription vs I/O vs webhook).

⸻

8. HTTP Endpoints

/api/transcriptions
	•	Returns HTML or JSON with recent transcriptions.
	•	Supports basic browsing of processed calls.

/healthz (Liveness)
	•	Always 200 if the process is running.
	•	No external dependencies.

/readyz (Readiness)
	•	200 only when:
	•	Queue is initialized.
	•	Worker pool is running.
	•	Any required DB/storage checks pass.
	•	Non-200 on startup failure or critical dependency failure.

/debug/queue (Debug / Metrics)
	•	Returns JSON similar to:

{
  "length": 5,
  "capacity": 100,
  "workers": 4,
  "last_backfill": {
    "total": 393,
    "unprocessed": 389,
    "selected": 15,
    "enqueued": 15,
    "dropped_full": 0,
    "other_errors": 0,
    "already_processed": 4
  },
  "processed_jobs": 123,
  "failed_jobs": 2
}

Use this to verify backfill and queue behavior without tailing logs.

⸻

9. Running & Operations

Local Run

go run main.go

Environment typically set via .env or systemd unit:
	•	Ensure CALLS_DIR exists and contains .mp3 files.
	•	Set transcription + GroupMe env vars before starting.

Common Checks
	•	On startup, look for:
	•	starting backfill with limit=...
	•	server listening on :8000
	•	watching /home/peebs/calls for new files
	•	A reasonable backfill summary (selected and enqueued > 0 when there are unprocessed files).
	•	To debug “stuck” conditions:
	•	Hit /debug/queue:
	•	If length == 0 and selected > 0 but enqueued == 0, investigate enqueue errors.
	•	If length is near capacity, consider increasing JOB_QUEUE_SIZE or reducing backfill/ingest rate.
	•	Check per-job logs for long transcribe durations.

⸻

10. Extension Guidelines (for future agents / Codex work)

When adding new capabilities (e.g., different alert channels, alternative transcription providers):
	•	Do not bypass the queue:
	•	All work must go through the same Queue API.
	•	Tag job sources:
	•	Use job.Source to differentiate backfill vs watcher vs API-triggered work, so logs remain readable.
	•	Keep new configuration in the same Config struct and document new env vars here.
	•	Add tests for:
	•	Backfill selection.
	•	Queue behavior under full conditions.
	•	New processing branches (e.g., new notifiers).

This document should be kept in sync whenever queue, backfill, or processing semantics change.

