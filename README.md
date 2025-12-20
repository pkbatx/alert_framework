# Alert Framework

A Go-based automation service that watches a directory of new radio call recordings, pushes real-time FYI alerts to GroupMe, transcribes audio with OpenAI, stores structured metadata in SQLite, and serves a polished single-page dashboard for monitoring activity.

## Highlights

- Watches `CALLS_DIR` for new audio files and enqueues a deterministic ingest + transcription workflow.
- Sends two-stage GroupMe notifications (initial alert + cleaned transcript) using the shared formatting engine.
- Persists all call context, transcripts, tags, locations, and regen history inside a local SQLite database.
- Embedded frontend (vanilla JS + Plotly + Mapbox) remains for fallback, while the Next.js CAD console lives in `web/`.
- Sussex-focused metadata inference verifies street-level addresses with Mapbox after (optional) LLM-backed cleanup, keeping Lakeland EMS calls pinned to Andover Township unless transcripts clearly say otherwise.
- Call rollups cluster recent geo-resolved calls into incident summaries for the CAD console.
- Ships with Docker support, optional audio preprocessing via `ffmpeg`, and zero external dependencies beyond OpenAI + GroupMe.

See `agents.md` for the authoritative description of each agent (watcher, queue manager, workers, UI, etc.).

## Repository Layout

```
alert_framework/
├── config/            # Environment + runtime configuration helpers and tests
├── formatting/        # Alert copy helpers (GroupMe templates, location formatting, incident helpers)
├── metrics/           # Lightweight in-process metrics + debug endpoints
├── queue/             # Job definitions, in-memory queue, and worker orchestration
├── web/               # Next.js 14 CAD console (dev server on :3000)
├── static/            # Embedded frontend (HTML/CSS/JS) plus UI tests
├── scripts/           # Dev helper scripts
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
   mkdir -p ./runtime/calls ./runtime/work
   ```

4. **Run the service locally**
   ```bash
   make dev
   ```
   The API listens on `http://localhost:8000`, while the CAD console runs at `http://localhost:3000`.

5. **Drop an `.mp3` into `CALLS_DIR`** to watch the ingest → alert → transcription flow in action.

## Configuration

All settings are sourced from environment variables (or `.env`). Common options:

| Variable | Purpose | Default |
| --- | --- | --- |
| `HTTP_PORT` | HTTP listen address (accepts `:8000` or `8000`) | `:8000` |
| `CALLS_DIR` | Directory to watch for new recordings | `./runtime/calls` |
| `WORK_DIR` | Workspace for derived artifacts and the SQLite DB | `./runtime/work` |
| `DB_PATH` | Explicit SQLite path (falls back to `$WORK_DIR/transcriptions.db`) | `""` |
| `CONFIG_PATH` | YAML/JSON config path | `config/config.yaml` |
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
| `API_BASE_URL` | Web proxy upstream base URL | `http://localhost:8000` |
| `ENABLE_ADMIN_ACTIONS` | Enable admin-only mutating endpoints | `false` |
| `ADMIN_TOKEN` | Token required for admin actions | empty |
| `ALERT_MODE` | Service role (`api`, `worker`, `all`) | `all` |
| `STRICT_CONFIG` | Fail fast on config errors | `false` |
| `IN_DOCKER` | Enables Docker-specific safeguards | `false` |

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

- `make dev` launches the watcher, queue, webhook sender, and API, plus the Next.js UI on port 3000.
- `go run .` still runs everything in one process if you prefer the embedded UI.
- `make reset` clears Next.js caches and stops rogue listeners on ports 3000/8000.
- File watcher (fsnotify) only reacts to new `.mp3` writes/moves; use the UI controls to re-run transcription or formatting for existing calls.
- The UI defaults to the last 24 hours of activity and reads from the `/api/calls` proxy endpoint on the same origin.

## Docker

A multi-stage image is provided (see `DOCKER.md`). In short:

```bash
docker compose up -d --build
```

Mount a persistent volume for `/data` (or whichever directories you place in `CALLS_DIR`/`WORK_DIR`) to retain recordings and the SQLite database.

## Testing

```bash
go test ./...
npm test
```

## Acceptance Checklist

```bash
docker compose up -d --build
curl -sS http://localhost:3000/api/calls?window=24h | head -c 200
curl -sS http://localhost:3000/api/health
```

- Web console loads calls or shows “No calls in the last 24h”.
- Browser Network calls `/api/calls` (same-origin), not `:8000` directly.
- No mutating actions are visible in the default UI.

The Go tests cover configuration, formatting, and queue helpers, while the Node tests validate small UI utilities. Extend these test suites before shipping new enhancements.

## Release Checklist

- [ ] Populate `.env` or runtime environment with production keys (OpenAI, GroupMe, Mapbox) and unique bot IDs.
- [ ] Configure `PUBLIC_BASE_URL` (and optionally `EXTERNAL_LISTEN_BASE_URL`) so preview and download links resolve externally.
- [ ] Set up persistent storage for `CALLS_DIR` and `WORK_DIR`, plus log shipping if required.
- [ ] Verify `ffmpeg` availability (or set `FFMPEG_BIN`) on the target host/container.
- [ ] Pin `JOB_QUEUE_SIZE`, `WORKER_COUNT`, and `JOB_TIMEOUT_SEC` to match the expected ingest volume.

With those cosmetics in place the repository is ready for a public GitHub release without touching the underlying runtime behavior.

<!-- CAAD_DOCS_BEGIN -->
## CAAD System Overview (managed)

Current system behavior (Phases 0–2):

- Polling worker ingests audio from `CALLS_DIR` and writes call artifacts
- File-based artifact store under `runtime/calls/<call_id>/`
- Contracts in `contracts/` define every JSON artifact
- Skillkit (`caad` CLI) provides AI, validation, and doc tooling

There is no UI or database in the Phase 0–2 pipeline; legacy Go/Next assets remain but are inactive.
See `STRUCTURE.md` for boundary-level details.
<!-- CAAD_DOCS_END -->
