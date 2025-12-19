from __future__ import annotations

from typing import Any, Dict

from jsonschema import validate as jsonschema_validate
from jsonschema.exceptions import ValidationError

from caad_skillkit.ai.clients import localai_chat, openai_chat
from caad_skillkit.ai.jsonio import parse_first_json_object
from caad_skillkit.ai.schemas import METADATA_SCHEMA, ROLLUP_SCHEMA
from caad_skillkit.config import get_env, load_settings


class SkillError(RuntimeError):
    pass


def _validate(payload: Dict[str, Any], schema: Dict[str, Any]) -> Dict[str, Any]:
    try:
        jsonschema_validate(payload, schema)
    except ValidationError as err:
        raise SkillError(f"schema validation failed: {err.message}") from err
    return payload


def _backend() -> str:
    return (get_env("AI_BACKEND") or load_settings().ai_backend).lower()


def build_metadata(text: str) -> Dict[str, Any]:
    backend = _backend()
    if backend == "stub":
        payload = {
            "call_type": "unknown",
            "location": "unknown",
            "notes": "stub output",
            "tags": [],
        }
        return _validate(payload, METADATA_SCHEMA)

    model = get_env("LOCALAI_MODEL") or load_settings().localai_model
    if backend == "localai":
        base_url = get_env("LOCALAI_BASE_URL") or load_settings().localai_base_url
        system_prompt = "Return a single JSON object with keys call_type, location, notes, tags. No extra keys."
        content = localai_chat(base_url, model, system_prompt, text)
    elif backend == "openai":
        base_url = get_env("OPENAI_BASE_URL") or "https://api.openai.com"
        api_key = get_env("OPENAI_API_KEY")
        if not api_key:
            raise SkillError("OPENAI_API_KEY missing")
        system_prompt = "Return a single JSON object with keys call_type, location, notes, tags. No extra keys."
        content = openai_chat(base_url, api_key, model, system_prompt, text)
    else:
        raise SkillError(f"unsupported AI_BACKEND {backend}")

    payload = parse_first_json_object(content)
    return _validate(payload, METADATA_SCHEMA)


def build_rollup(text: str) -> Dict[str, Any]:
    backend = _backend()
    if backend == "stub":
        payload = {
            "title": "stub rollup",
            "summary": "stub summary",
            "evidence": [],
            "confidence": "low",
        }
        return _validate(payload, ROLLUP_SCHEMA)

    model = get_env("LOCALAI_MODEL") or load_settings().localai_model
    if backend == "localai":
        base_url = get_env("LOCALAI_BASE_URL") or load_settings().localai_base_url
        system_prompt = "Return a single JSON object with keys title, summary, evidence, confidence. No extra keys."
        content = localai_chat(base_url, model, system_prompt, text)
    elif backend == "openai":
        base_url = get_env("OPENAI_BASE_URL") or "https://api.openai.com"
        api_key = get_env("OPENAI_API_KEY")
        if not api_key:
            raise SkillError("OPENAI_API_KEY missing")
        system_prompt = "Return a single JSON object with keys title, summary, evidence, confidence. No extra keys."
        content = openai_chat(base_url, api_key, model, system_prompt, text)
    else:
        raise SkillError(f"unsupported AI_BACKEND {backend}")

    payload = parse_first_json_object(content)
    return _validate(payload, ROLLUP_SCHEMA)
