from __future__ import annotations

import os
from dataclasses import dataclass
from pathlib import Path
from typing import Optional

DEFAULT_AI_BACKEND = "localai"
DEFAULT_LOCALAI_BASE_URL = "http://localhost:8080"
DEFAULT_LOCALAI_MODEL = "tinyllama-1.1b-chat"
DEFAULT_TRANSCRIBE_BACKEND = "openai"
DEFAULT_OPENAI_STT_MODEL = "gpt-4o-transcribe"
DEFAULT_AI_TIMEOUT_S = 45


def normalize_env(value: Optional[str]) -> Optional[str]:
    if value is None:
        return None
    trimmed = value.strip()
    if len(trimmed) >= 2 and trimmed[0] == trimmed[-1] and trimmed[0] in ("'", '"'):
        trimmed = trimmed[1:-1].strip()
    return trimmed


def get_env(key: str, default: Optional[str] = None) -> Optional[str]:
    return normalize_env(os.getenv(key)) or default


def load_env_file(path: Path) -> None:
    if not path.exists():
        return
    for raw in path.read_text().splitlines():
        line = raw.strip()
        if not line or line.startswith("#") or "=" not in line:
            continue
        key, value = line.split("=", 1)
        key = key.strip()
        if key and key not in os.environ:
            os.environ[key] = normalize_env(value) or ""


@dataclass(frozen=True)
class Settings:
    ai_backend: str
    localai_base_url: str
    localai_model: str
    transcribe_backend: str
    openai_stt_model: str
    ai_timeout_s: int


def load_settings() -> Settings:
    load_env_file(Path.cwd() / ".env")
    return Settings(
        ai_backend=get_env("AI_BACKEND", DEFAULT_AI_BACKEND) or DEFAULT_AI_BACKEND,
        localai_base_url=get_env("LOCALAI_BASE_URL", DEFAULT_LOCALAI_BASE_URL) or DEFAULT_LOCALAI_BASE_URL,
        localai_model=get_env("LOCALAI_MODEL", DEFAULT_LOCALAI_MODEL) or DEFAULT_LOCALAI_MODEL,
        transcribe_backend=get_env("TRANSCRIBE_BACKEND", DEFAULT_TRANSCRIBE_BACKEND) or DEFAULT_TRANSCRIBE_BACKEND,
        openai_stt_model=get_env("OPENAI_STT_MODEL", DEFAULT_OPENAI_STT_MODEL) or DEFAULT_OPENAI_STT_MODEL,
        ai_timeout_s=int(get_env("AI_TIMEOUT_S", str(DEFAULT_AI_TIMEOUT_S)) or DEFAULT_AI_TIMEOUT_S),
    )
