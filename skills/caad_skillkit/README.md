# CAAD Skillkit

Repo-local helper toolkit for CAAD development. All commands return JSON and never print secrets.

## Install

```bash
pip install -e skills/caad_skillkit
```

## Commands

```bash
caad env check OPENAI_API_KEY LOCALAI_BASE_URL LOCALAI_MODEL
caad env set OPENAI_API_KEY --from-keychain "OpenAI" --account "me@example.com"
caad localai readyz
caad localai models
caad localai chat-test --model tinyllama-1.1b-chat
caad ai metadata --input transcript.txt --pretty
caad ai rollup --input transcript.txt --pretty
caad ai transcribe --input audio.mp3 --out transcript.json
```

## Environment

- `AI_BACKEND` (default: localai)
- `LOCALAI_BASE_URL` (default: http://localhost:8080)
- `LOCALAI_MODEL` (default: tinyllama-1.1b-chat)
- `TRANSCRIBE_BACKEND` (default: openai)
- `OPENAI_STT_MODEL` (default: gpt-4o-transcribe)
- `AI_TIMEOUT_S` (default: 45)

## No Secrets Policy

- Secrets are never printed.
- All env values are trimmed of whitespace and CRLF.
- Errors are JSON to stderr with non-zero exit codes.
