from __future__ import annotations

from pathlib import Path
from typing import Any, Dict

from jsonschema import validate as jsonschema_validate
from jsonschema.exceptions import ValidationError

from caad_skillkit.ai.clients import localai_transcribe, openai_transcribe
from caad_skillkit.ai.schemas import get_schema
from caad_skillkit.config import get_env, load_settings


class TranscribeError(RuntimeError):
    pass


def transcribe_audio(path: str) -> Dict[str, Any]:
    settings = load_settings()
    backend = (get_env("TRANSCRIBE_BACKEND") or settings.transcribe_backend).lower()
    file_path = Path(path).expanduser().resolve()
    if not file_path.exists():
        raise TranscribeError(f"file not found: {file_path}")

    if backend == "localai":
        base_url = get_env("LOCALAI_BASE_URL") or settings.localai_base_url
        model = get_env("OPENAI_STT_MODEL") or settings.openai_stt_model
        raw = localai_transcribe(base_url, model, str(file_path))
        text = raw.get("text", "") if isinstance(raw, dict) else ""
    elif backend == "openai":
        api_key = get_env("OPENAI_API_KEY")
        if not api_key:
            raise TranscribeError("OPENAI_API_KEY missing")
        model = get_env("OPENAI_STT_MODEL") or settings.openai_stt_model
        raw = openai_transcribe(api_key, model, str(file_path))
        text = raw.get("text", "") if isinstance(raw, dict) else ""
    else:
        raise TranscribeError(f"unsupported TRANSCRIBE_BACKEND {backend}")

    payload = {
        "text": text,
        "language": raw.get("language", "") if isinstance(raw, dict) else "",
        "confidence": None,
        "duration_s": None,
        "segments": [],
    }
    try:
        jsonschema_validate(payload, get_schema("transcription"))
    except ValidationError as err:
        raise TranscribeError(f"transcription schema validation failed: {err.message}") from err
    return payload
