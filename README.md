# Alert Framework (Control plane + Data plane)

This repo now separates operational concerns into a **deterministic Go data plane** and a **typed MCP control plane**. The data plane keeps ingest/alerts running without MCP; the MCP layer orchestrates workflows, backfills, and diagnostics by calling the `/ops` and `/api` surfaces exposed by the Go service.

## Layout
- `cmd/alert-framework/`: thin entrypoint wiring config and the app
- `internal/app`: composition root for data-plane wiring
- `internal/config`: environment/.env loading with sane defaults
- `internal/store`: SQLite models + migrations and job/call helpers
- `internal/jobs`: unified job model, idempotency, worker pool, SSE-friendly logs
- `internal/pipeline`: deterministic stage implementations (INGEST → PUBLISH)
- `internal/watch`: fsnotify watcher that enqueues ingest jobs
- `internal/httpapi`: `/ops/*` and `/api/*` routers for UI + MCP
- `internal/notify|nlp|geo|metrics|events`: integration hooks and observability stubs
- `mcp_server/`: MCP sidecar that exposes typed tools backed by `/ops` + `/api`

## Running locally

```bash
docker compose up --build
```

Environment defaults are tuned for local operation. Core ingest/alert behavior continues to run even if MCP is offline.

## Data plane responsibilities
- Watch `CALLS_DIR` for new audio files and create immutable call artifacts under `WORK_DIR`
- Execute deterministic stages via jobs: `INGEST`, `PREPROCESS`, `ALERT_INITIAL`, `TRANSCRIBE`, `NORMALIZE`, `ENRICH`, `PUBLISH`
- Persist calls, jobs, and logs in SQLite
- Expose stable APIs:
  - `/api/*` for dashboard consumption
  - `/ops/*` for safe operations, backfills, and streaming logs
- Emit structured logs and counters for observability

## Control plane (MCP) responsibilities
- Provide typed tools that compose `/ops` + `/api` endpoints into workflows: status, backlog analysis, backfill, reprocess, briefing, anomaly detection, clustering, diagnostics
- Never require direct DB/file access; all evidence flows through the data-plane APIs
- All tools are idempotent and evidence-first (no secrets logged)

## Example operator workflows
- **Cold start:** bring up Docker Compose, confirm `/ops/health`, tail watcher events, and enqueue an ingest job for a known file.
- **Backfill window:** use `alerts.backfill` MCP tool to enqueue stages for a time window. Jobs are idempotent by `(call_id, stage)`.
- **Daily briefing:** `alerts.briefing` pulls `/ops/briefing-data` and optionally summarizes via LLM with evidence of `call_id`s.
- **Reprocess:** `alerts.reprocess` enqueues a specific stage for a `call_id` with an optional `force` flag.
- **Diagnose:** `alerts.diagnose` inspects `/ops/status`, recent jobs, and call metadata to suggest next steps.

## Ops surface
- `GET /ops/status` – counters plus recent calls/jobs
- `POST /ops/jobs/enqueue` – enqueue any stage
- `GET /ops/jobs` – list persisted jobs
- `GET /ops/jobs/{id}` – job detail
- `GET /ops/jobs/{id}/logs` – streamable logs (buffered for SSE)
- `POST /ops/reprocess` – convenience wrapper for a single call + stage
- `POST /ops/backfill` – enqueue windowed work (idempotent)
- `GET /ops/briefing-data` – structured aggregates for MCP
- `GET /ops/anomalies` – deterministic spike detection stub
- `GET /ops/health` – DB connectivity check

## Testing
- Go unit tests cover pipeline stage behavior, job idempotency, and `/ops` API surfaces
- Python tests (under `mcp_server/`) validate MCP wrappers with mocked HTTP responses

## Guardrails
- `ENABLE_DANGEROUS_OPS` defaults to false; set explicitly when running destructive tasks
- MCP logs never include secrets; control plane only uses data-plane HTTP APIs
- Deterministic stages keep outputs repeatable and idempotent across replays
