#!/usr/bin/env bash
set -euo pipefail

GIT_SHA=$(git rev-parse --short HEAD 2>/dev/null || echo dev)
BUILD_TIME=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

export NEXT_PUBLIC_BUILD_SHA=${NEXT_PUBLIC_BUILD_SHA:-$GIT_SHA}
export NEXT_PUBLIC_BUILD_TIME=${NEXT_PUBLIC_BUILD_TIME:-$BUILD_TIME}

trap 'kill 0' SIGINT SIGTERM EXIT

go run . &
( cd web && corepack enable && pnpm dev -p 3000 ) &

wait
