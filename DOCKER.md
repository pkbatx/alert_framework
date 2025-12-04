# Docker usage

This project ships with a multi-stage Docker build that creates a minimal runtime image with `ffmpeg` available for audio preprocessing. The container is designed to be configured entirely through environment variables so it can be launched with `docker run` or compose without extra files.

## Build

```bash
docker build -t alert-framework .
```

## Run with environment variables

The image exposes port `8000` by default. Override settings with `-e` flags and mount a volume for persistent data:

```bash
docker run --rm \
  -p 8000:8000 \
  -v $(pwd)/data:/data \
  -e HTTP_PORT=8000 \
  -e CALLS_DIR=/data/calls \
  -e WORK_DIR=/data/work \
  -e DB_PATH=/data/work/transcriptions.db \
  -e GROUPME_BOT_ID="your-bot-id" \
  -e GROUPME_ACCESS_TOKEN="your-groupme-token" \
  -e MAPBOX_TOKEN="your-mapbox-token" \
  alert-framework
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

To customize the prefix used in webhook listen URLs, set `PUBLIC_BASE_URL` to your public domain/path, or `EXTERNAL_LISTEN_BASE_URL` if the audio files live elsewhere.

The entrypoint ensures `CALLS_DIR`, `WORK_DIR`, and `DB_PATH` are initialized before the application starts, making the container easy to run with only environment variables and a mounted data volume.
