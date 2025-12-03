#!/usr/bin/env sh
set -e

# Ensure working directories exist for bind mounts or defaults
mkdir -p "${CALLS_DIR:-/data/calls}" "${WORK_DIR:-/data/work}"

# If DB_PATH is not provided, align it with WORK_DIR so persisted volumes work
if [ -z "${DB_PATH}" ]; then
  export DB_PATH="${WORK_DIR:-/data/work}/transcriptions.db"
fi

exec "$@"
