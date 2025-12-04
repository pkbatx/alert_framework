# Alert Framework

A Go-based automation service that watches a directory of new radio call recordings, pushes real-time FYI alerts to GroupMe, transcribes audio with OpenAI, stores structured metadata in SQLite, and serves a polished single-page dashboard for monitoring activity.

## Highlights

- Watches `CALLS_DIR` for new audio files and enqueues a deterministic ingest + transcription workflow.
- Sends two-stage GroupMe notifications (initial alert + cleaned transcript) using the shared formatting engine.
- Persists all call context, transcripts, tags, locations, and regen history inside a local SQLite database.
- Embedded frontend (vanilla JS + Plotly + Mapbox) provides filtering, waveform playback, hotspot maps, and transcript utilities.
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

Use `.env.example` as a starting point; it documents every variable that ships with the service.

## Development Workflow

- `go run .` launches the watcher, queue, webhook sender, and HTTP server in one process.
- Static assets are embedded at build time (`go:embed static/*`), so changes require rebuilding the binary.
- File watcher (fsnotify) only reacts to new `.mp3` writes/moves; use the UI controls to re-run transcription or formatting for existing calls.
- The UI defaults to the last 24 hours of activity and polls the `/api/calls` endpoint every few seconds. Use the summary filters to inspect volume, tags, and hotspots.

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
