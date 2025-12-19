#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

if [ -f "$ROOT/.env" ]; then
  set -a
  # shellcheck disable=SC1091
  . "$ROOT/.env"
  set +a
fi

CLI="python $ROOT/skills/caad_skillkit/caad_skillkit/cli.py"

$CLI env check OPENAI_API_KEY LOCALAI_BASE_URL LOCALAI_MODEL AI_BACKEND
$CLI localai readyz
$CLI localai models

if [ -z "${LOCALAI_MODEL:-}" ]; then
  echo '{"ok":false,"error":{"code":"missing_localai_model","message":"LOCALAI_MODEL is not set"}}' >&2
  exit 1
fi

$CLI localai chat-test --model "$LOCALAI_MODEL"

TMPDIR="$(mktemp -d)"
trap 'rm -rf "$TMPDIR"' EXIT

cat > "$TMPDIR/transcription.json" <<'EOF'
{"text":"stub","language":"en","confidence":null,"duration_s":null,"segments":[]}
EOF

cat > "$TMPDIR/metadata.json" <<'EOF'
{"call_type":"unknown","location":"unknown","notes":"stub","tags":[]}
EOF

cat > "$TMPDIR/rollup.json" <<'EOF'
{"title":"stub","summary":"stub","evidence":[],"confidence":"low"}
EOF

cat > "$TMPDIR/call_record.json" <<'EOF'
{"id":"stub","ts":"1970-01-01T00:00:00Z","source":"stub","audio_path":"runtime/calls/stub/audio/input.wav","transcript_path":"runtime/calls/stub/transcript.json","metadata_path":"runtime/calls/stub/metadata.json","rollup_path":"runtime/calls/stub/rollup.json","status":"stub"}
EOF

$CLI validate transcription --input "$TMPDIR/transcription.json"
$CLI validate metadata --input "$TMPDIR/metadata.json"
$CLI validate rollup --input "$TMPDIR/rollup.json"
$CLI validate call_record --input "$TMPDIR/call_record.json"
