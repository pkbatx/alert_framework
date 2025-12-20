from __future__ import annotations

import json
from pathlib import Path
from typing import Any, Dict

DEFAULT_METADATA_SCHEMA: Dict[str, Any] = {
    "type": "object",
    "additionalProperties": False,
    "required": ["call_type", "location", "notes", "tags"],
    "properties": {
        "call_type": {"type": "string"},
        "location": {"type": "string"},
        "notes": {"type": "string"},
        "tags": {"type": "array", "items": {"type": "string"}},
    },
}

DEFAULT_ROLLUP_SCHEMA: Dict[str, Any] = {
    "type": "object",
    "additionalProperties": False,
    "required": ["title", "summary", "evidence", "confidence"],
    "properties": {
        "title": {"type": "string"},
        "summary": {"type": "string"},
        "evidence": {"type": "array", "items": {"type": "string"}},
        "confidence": {"type": "string", "enum": ["low", "medium", "high"]},
    },
}

DEFAULT_TRANSCRIPTION_SCHEMA: Dict[str, Any] = {
    "type": "object",
    "additionalProperties": False,
    "required": ["text", "language", "confidence", "duration_s", "segments"],
    "properties": {
        "text": {"type": "string"},
        "language": {"type": "string"},
        "confidence": {"type": ["number", "null"]},
        "duration_s": {"type": ["number", "null"]},
        "segments": {
            "type": "array",
            "items": {
                "type": "object",
                "additionalProperties": False,
                "required": ["start", "end", "text"],
                "properties": {
                    "start": {"type": "number"},
                    "end": {"type": "number"},
                    "text": {"type": "string"},
                },
            },
        },
    },
}

DEFAULT_CALL_RECORD_SCHEMA: Dict[str, Any] = {
    "type": "object",
    "additionalProperties": False,
    "required": [
        "id",
        "ts",
        "source",
        "original_filename",
        "stored_audio_path",
        "transcript_path",
        "metadata_path",
        "rollup_path",
        "status",
    ],
    "properties": {
        "id": {"type": "string"},
        "ts": {"type": "string"},
        "source": {"type": "string"},
        "original_filename": {"type": "string"},
        "stored_audio_path": {"type": "string"},
        "transcript_path": {"type": "string"},
        "metadata_path": {"type": "string"},
        "rollup_path": {"type": "string"},
        "status": {"type": "string"},
        "error": {"type": "string"},
    },
}


def _find_contracts_dir() -> Path | None:
    current = Path(__file__).resolve()
    for parent in current.parents:
        candidate = parent / "contracts"
        if candidate.is_dir():
            return candidate
    return None


def _load_contract(name: str, fallback: Dict[str, Any]) -> Dict[str, Any]:
    contracts_dir = _find_contracts_dir()
    if not contracts_dir:
        return fallback
    schema_path = contracts_dir / f"{name}.schema.json"
    if not schema_path.exists():
        return fallback
    try:
        return json.loads(schema_path.read_text())
    except json.JSONDecodeError:
        return fallback


METADATA_SCHEMA = _load_contract("metadata", DEFAULT_METADATA_SCHEMA)
ROLLUP_SCHEMA = _load_contract("rollup", DEFAULT_ROLLUP_SCHEMA)
TRANSCRIPTION_SCHEMA = _load_contract("transcription", DEFAULT_TRANSCRIPTION_SCHEMA)
CALL_RECORD_SCHEMA = _load_contract("call_record", DEFAULT_CALL_RECORD_SCHEMA)

SCHEMA_MAP = {
    "metadata": METADATA_SCHEMA,
    "rollup": ROLLUP_SCHEMA,
    "transcription": TRANSCRIPTION_SCHEMA,
    "call_record": CALL_RECORD_SCHEMA,
}


def get_schema(name: str) -> Dict[str, Any]:
    key = name.lower()
    if key not in SCHEMA_MAP:
        raise KeyError(f"unknown schema: {name}")
    return SCHEMA_MAP[key]
