#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

COMPOSE="docker compose"
SERVICES="$($COMPOSE config --services)"
START_SERVICES=("api")

if echo "$SERVICES" | grep -qx "localai"; then
  START_SERVICES+=("localai")
fi

$COMPOSE up -d --build "${START_SERVICES[@]}"

API_PORT="${CAAD_API_PORT:-8000}"
for _ in $(seq 1 30); do
  if curl -fsS "http://127.0.0.1:$API_PORT/healthz" >/dev/null 2>&1; then
    break
  fi
  sleep 0.5
done

curl -fsS "http://127.0.0.1:$API_PORT/healthz" >/dev/null
curl -fsS "http://127.0.0.1:$API_PORT/calls?since_hours=24&limit=10" | python3 -m json.tool >/dev/null

if echo "$SERVICES" | grep -qx "localai"; then
  for _ in $(seq 1 30); do
    if curl -fsS "http://127.0.0.1:8080/readyz" >/dev/null 2>&1; then
      break
    fi
    sleep 0.5
  done
  curl -fsS "http://127.0.0.1:8080/readyz" >/dev/null
fi
