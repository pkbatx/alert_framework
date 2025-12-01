# Backfill helper

This command is a one-time helper that scans your calls directory and enqueues any missing or failed transcriptions through the running service API.

```
go run ./cmd/backfill_helper --help
```

## Common flags

- `--service` — Base URL for the running service (defaults to `SERVICE_BASE_URL` or `http://localhost:$HTTP_PORT`).
- `--calls-dir` — Path to the directory with audio files.
- `--db` — Path to the SQLite DB used by the main service.
- `--limit` — Cap how many pending items are enqueued in one run.
- `--concurrency` — Number of simultaneous enqueue requests.
- `--dry-run` — Print the plan without sending any requests.
- `--requeue-stale` — Automatically requeue in-flight jobs older than the provided duration.

The helper skips files already marked `done`, leaves active `queued`/`processing` jobs alone (unless they are stale), and retries entries with an `error` status.
