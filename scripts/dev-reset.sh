#!/usr/bin/env bash
set -euo pipefail

kill_port() {
  local port=$1
  if command -v lsof >/dev/null 2>&1; then
    local pids
    pids=$(lsof -ti tcp:"${port}" || true)
    if [ -n "${pids}" ]; then
      kill ${pids} || true
    fi
  elif command -v fuser >/dev/null 2>&1; then
    fuser -k "${port}"/tcp || true
  fi
}

kill_port 3000
kill_port 8000

rm -rf web/.next web/node_modules/.cache

make dev
