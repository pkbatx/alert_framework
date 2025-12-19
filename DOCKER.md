# Docker usage

This project ships with a multi-stage Docker build that creates a minimal runtime image with `ffmpeg` available for audio preprocessing. The production topology runs the API, worker, and Next.js UI as separate services using Docker Compose.

## Production Compose

```bash
docker compose up -d --build
```

This brings up:

- `api` on `http://localhost:8000`
- `worker` (no ports, handles watcher + queue)
- `web` on `http://localhost:3000`

### Optional dev compose

```bash
docker compose -f docker-compose.dev.yml up --build
```

### Key environment variables

- `HTTP_PORT`: HTTP listen port (with or without leading `:`). Defaults to `:8000`.
- `CALLS_DIR`: Directory where new audio recordings appear. Defaults to `/data/calls` inside the container.
- `WORK_DIR`: Directory for generated artifacts and SQLite database. Defaults to `/data/work`.
- `DB_PATH`: SQLite path; defaults to `$WORK_DIR/transcriptions.db` when not provided.
- `GROUPME_BOT_ID` / `GROUPME_ACCESS_TOKEN`: GroupMe credentials for sending alerts.
- `MAPBOX_TOKEN`: Optional Mapbox access token for mapping features.
- `AUDIO_FILTER_ENABLED`: Toggle audio preprocessing (`true` by default).
- `FFMPEG_BIN`: Override the `ffmpeg` binary name/path if needed.
- `PUBLIC_BASE_URL`: Base URL (scheme, host, and optional path prefix) used when building preview links and webhook payloads. Set this to your public domain when running behind a reverse proxy.
- `EXTERNAL_LISTEN_BASE_URL`: Optional override for direct audio links sent to webhooks. Use this if audio files are hosted at a different domain or CDN; otherwise `PUBLIC_BASE_URL` (or `http://localhost:HTTP_PORT`) is used.
- `ALERT_MODE`: Service role (`api`, `worker`, `all`). Compose sets this for the production split.
- `STRICT_CONFIG`: Fail fast on config errors (set to `true` in production compose).
- `IN_DOCKER`: Enables Docker-specific safeguards when set to `true`.

To customize the prefix used in webhook listen URLs, set `PUBLIC_BASE_URL` to your public domain/path, or `EXTERNAL_LISTEN_BASE_URL` if the audio files live elsewhere.

Compose mounts `./data` to `/data`, `./runtime/calls` to `/data/calls`, and `./config` to `/app/config`. Ensure these host directories exist and are writable before starting the stack.
