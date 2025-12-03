#!/usr/bin/env sh
set -eu

CALLS_DIR=${CALLS_DIR:-/data/calls}
WORK_DIR=${WORK_DIR:-/data/work}
DB_PATH=${DB_PATH:-${WORK_DIR}/transcriptions.db}
HTTP_PORT=${HTTP_PORT:-:8000}

for var in OPENAI_API_KEY GROUPME_BOT_ID GROUPME_ACCESS_TOKEN; do
  eval "value=\${${var}:-}"
  if [ -z "${value}" ]; then
    echo "Environment variable ${var} is required" >&2
    exit 1
  fi
done

mkdir -p "${CALLS_DIR}" "${WORK_DIR}" "$(dirname "${DB_PATH}")"
mkdir -p /data/last24 /data/tmp /alert_framework_data/work
[ -f "${DB_PATH}" ] || touch "${DB_PATH}"

export CALLS_DIR WORK_DIR DB_PATH HTTP_PORT

exec /app/alert_framework "$@"
