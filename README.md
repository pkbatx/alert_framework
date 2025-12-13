# Alert Framework

A Go-based automation service that watches a directory of new radio call recordings, pushes real-time FYI alerts to GroupMe, transcribes audio with OpenAI, stores structured metadata in SQLite, and serves a polished single-page dashboard for monitoring activity.

## Highlights

- Watches `CALLS_DIR` for new audio files and enqueues a deterministic ingest + transcription workflow.
- Sends two-stage GroupMe notifications (initial alert + cleaned transcript) using the shared formatting engine.
- Persists all call context, transcripts, tags, locations, and regen history inside a local SQLite database.
- Embedded frontend (vanilla JS + Plotly + Mapbox) provides filtering, waveform playback, hotspot maps, and transcript utilities.
- Sussex-focused metadata inference verifies street-level addresses with Mapbox after (optional) LLM-backed cleanup, keeping Lakeland EMS calls pinned to Andover Township unless transcripts clearly say otherwise.
- Ships with Docker support, optional audio preprocessing via `ffmpeg`, and zero external dependencies beyond OpenAI + GroupMe.

See `agents.md` for the authoritative description of each agent (watcher, queue manager, workers, UI, etc.).

## Repository Layout

```
alert_framework/
├── config/            # Environment + runtime configuration helpers and tests
├── formatting/        # Alert copy helpers (GroupMe templates, location formatting, incident helpers)
├── metrics/           # Lightweight in-process metrics + debug endpoints
├── queue/             # Job definitions, in-memory queue, and worker orchestration
├── static/            # Embedded frontend (HTML/CSS/JS) plus UI tests
├── docker/            # Helper assets referenced by the Dockerfile
├── DOCKER.md          # Container build/run instructions
├── agents.md          # High-level system/agent documentation
├── main.go            # CLI entrypoint wiring watchers, HTTP server, queue, and workers together
└── README.md          # You are here
```

Legacy backfill helpers are intentionally removed to keep the runtime focused on live monitoring.

## Getting Started

1. **Install prerequisites**  
   - Go 1.21+ (toolchain target in `go.mod`)  
   - Node 20+ for the tiny UI test suite  
   - `ffmpeg` available on `PATH`

2. **Create a configuration file**
   ```bash
   cp .env.example .env
   # edit .env with your keys, bot IDs, and desired paths
   ```

3. **Create the working directories referenced by `.env`**
   ```bash
   mkdir -p ./data/calls ./data/work
   ```

4. **Run the service locally**
   ```bash
   go run .
   ```
   The HTTP UI defaults to `http://localhost:8000` and immediately starts watching `CALLS_DIR` for new `.mp3` drops.

5. **Drop an `.mp3` into `CALLS_DIR`** to watch the ingest → alert → transcription flow in action.

## Configuration

All settings are sourced from environment variables (or `.env`). Common options:

| Variable | Purpose | Default |
| --- | --- | --- |
| `HTTP_PORT` | HTTP listen address (accepts `:8000` or `8000`) | `:8000` |
| `CALLS_DIR` | Directory to watch for new recordings | `/data/calls` |
| `WORK_DIR` | Workspace for derived artifacts and the SQLite DB | `/data/work` |
| `DB_PATH` | Explicit SQLite path (falls back to `$WORK_DIR/transcriptions.db`) | `""` |
| `OPENAI_API_KEY` | API key used for transcription + cleanup requests | none |
| `GROUPME_BOT_ID` / `GROUPME_ACCESS_TOKEN` | Credentials for sending alerts | none |
| `MAPBOX_TOKEN` | Enables hotspot and density overlays in the UI | none |
| `PUBLIC_BASE_URL` | External base URL for webhook/listen links | empty (derived from host/port) |
| `EXTERNAL_LISTEN_BASE_URL` | Override for direct audio links (CDN, S3, etc.) | empty |
| `AUDIO_FILTER_ENABLED` | Toggle `ffmpeg` preprocessing | `true` |
| `FFMPEG_BIN` | Executable name/path when a custom build is required | `ffmpeg` |
| `WORKER_COUNT` | Concurrent transcription workers | `4` |
| `JOB_QUEUE_SIZE` | Bounded queue capacity (auto clamped between 1 and 1024) | `100` |
| `JOB_TIMEOUT_SEC` | Max seconds a worker may hold a job | `60` |
| `DEV_UI` | Enables extra UI traces/tooling when truthy | `false` |
| `NLP_CONFIG_PATH` | Path to the GPT-5.1 prompt template file | `config/config.yaml` |

Use `.env.example` as a starting point; it documents every variable that ships with the service.

### Prompt customization

`POST /api/settings` accepts the same payload returned from `GET /api/settings`. Update `cleanup_prompt` to tweak transcript normalization rules, and `metadata_prompt` to fine tune the Sussex-specific metadata extractor. Leaving either field empty automatically falls back to the hardened defaults defined in `main.go`.

The metadata prompt powers a two-stage location flow:

1. Deterministic parsing + regex extraction attempts to geocode an address strictly inside the Sussex bounding box.
2. If that fails, the metadata prompt runs once against the normalized transcript (after the OpenAI transcription step completes). The JSON response is geocoded with Mapbox and only accepted when the coordinates fall within Sussex County (Andover Township bias). The result is cached per filename so subsequent UI loads avoid extra API calls.

#### config/config.yaml

`config/config.yaml` (JSON syntax, still valid YAML 1.2) hosts runtime-reloadable templates for the GPT-5.1 refinement pipeline. Edit this file to adjust:

- `cleanup_prompt`, `metadata_prompt`, and `address_prompt`
- `refinement_temperature`, summary/cleanup style, and Sussex bias mode
- Mapbox bounding box overrides

The backend watches this file and reloads templates at runtime—no restart required. Use `NLP_CONFIG_PATH` to point at alternate locations per environment.

## Development Workflow

- `go run .` launches the watcher, queue, webhook sender, and HTTP server in one process.
- Static assets are embedded at build time (`go:embed static/*`), so changes require rebuilding the binary.
- File watcher (fsnotify) only reacts to new `.mp3` writes/moves; use the UI controls to re-run transcription or formatting for existing calls.
- The UI defaults to the last 24 hours of activity and polls the `/api/calls` endpoint every few seconds. Use the summary filters to inspect volume, tags, and hotspots.

## Ops API

Operators can trigger reprocessing and inspect pipeline health via a minimal control surface mounted under `/ops` on the existing HTTP server.

- `GET /ops/status` – health snapshot including config summary, queue depth, pipeline counters, and DB status.
- `POST /ops/transcribe/run` – enqueue transcription jobs for existing calls. Body: `{"call_ids": ["file.mp3"], "since_minutes": 60, "limit": 200, "force": false}`.
- `POST /ops/enrich/run` – enqueue metadata enrichment for matching calls.
- `POST /ops/publish/run` – enqueue publish jobs; optional `destination` of `groupme`, `ui`, or `both`.
- `POST /ops/reprocess` – target a single call id with `{ "call_id": "file.mp3", "stage": "transcribe"|"enrich"|"publish", "force": false }`.
- `GET /ops/jobs` – list recent ops jobs; `GET /ops/jobs/{id}` for details.
- `GET /ops/jobs/{id}/logs` – SSE stream of per-job log lines.
- `POST /ops/reset` – drain in-memory queue and mark in-flight work failed (only when `ENABLE_DANGEROUS_OPS=1`).

All endpoints are idempotent where possible and avoid returning tokens or secrets.

## MCP Sidecar

An optional MCP companion exposes friendly tools that wrap the Ops API without touching files or the database directly.

Run locally with Docker Compose:

```bash
docker compose up --build
```

The `mcp` service listens on port 8787 and proxies to the Go app on port 8000. Tools include:

- `alerts.status()`
- `alerts.run_transcribe(...)`
- `alerts.run_enrich(...)`
- `alerts.run_publish(..., destination="both")`
- `alerts.reprocess(call_id, stage, force=False)`
- `alerts.jobs()` / `alerts.job_status(job_id)`
- `alerts.tail_job(job_id, seconds=30)`
- `alerts.briefing(since_minutes=360, format="bullets")`

Sample prompts:

- "What's the current backlog?"
- "Transcribe last 60 minutes"
- "Generate a 6-hour briefing"
- "Reprocess call <id> transcribe + publish"

## Docker

A multi-stage image is provided (see `DOCKER.md`). In short:

```bash
docker build -t alert-framework .
docker run --rm \
  -p 8000:8000 \
  -v $(pwd)/data:/data \
  --env-file .env \
  alert-framework
```

Mount a persistent volume for `/data` (or whichever directories you place in `CALLS_DIR`/`WORK_DIR`) to retain recordings and the SQLite database.

## Testing

```bash
go test ./...
npm test
```

The Go tests cover configuration, formatting, and queue helpers, while the Node tests validate small UI utilities. Extend these test suites before shipping new enhancements.

## Release Checklist

- [ ] Populate `.env` or runtime environment with production keys (OpenAI, GroupMe, Mapbox) and unique bot IDs.
- [ ] Configure `PUBLIC_BASE_URL` (and optionally `EXTERNAL_LISTEN_BASE_URL`) so preview and download links resolve externally.
- [ ] Set up persistent storage for `CALLS_DIR` and `WORK_DIR`, plus log shipping if required.
- [ ] Verify `ffmpeg` availability (or set `FFMPEG_BIN`) on the target host/container.
- [ ] Pin `JOB_QUEUE_SIZE`, `WORKER_COUNT`, and `JOB_TIMEOUT_SEC` to match the expected ingest volume.

With those cosmetics in place the repository is ready for a public GitHub release without touching the underlying runtime behavior.
