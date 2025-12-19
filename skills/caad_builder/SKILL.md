---
name: caad_builder
description: Repo-local workspace skill that provides executable CAAD helpers (transcription, metadata/rollup JSON analysis, LocalAI/Docker Compose ops, env key hygiene, FastAPI scaffolding). Use for deterministic glue work and structured diagnostics without exposing secrets.
---

# CAAD Builder Skill

Provide executable helpers for CAAD development. Always return JSON only, never log secrets, and fail fast with structured errors.

## Entry Points

- Python module: `skills/caad_builder/caad_builder.py`
- CLI: `python skills/caad_builder/caad_builder.py <command> [args]`

## Commands

- `transcribe-audio --file-path <path>`
- `analyze-metadata --text <string>`
- `analyze-rollup --text <string>`
- `start-localai [--compose-file <path>]`
- `check-localai-health`
- `list-compose-services [--compose-file <path>]`
- `triage-service --service-name <name> [--compose-file <path>]`
- `ensure-env-key --key <KEY> [--value <value>] [--keychain-service <service>]`
- `scaffold-fastapi-component --name <name> --type <route|dependency|service>`

## Notes

- Read `.env` from repo root when present, but never print secrets.
- Trim whitespace and newlines from all environment variables automatically.
- Use timeouts for HTTP and subprocess calls.
- Return JSON only (success or structured error).
