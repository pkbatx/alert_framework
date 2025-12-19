from __future__ import annotations

import hashlib
import json
from datetime import datetime, timedelta, timezone
from pathlib import Path
from typing import Any, Dict, List

from jsonschema import validate as jsonschema_validate
from jsonschema.exceptions import ValidationError

REPO_ROOT = Path(__file__).resolve().parents[1]
CONTRACTS_DIR = REPO_ROOT / "contracts"
RUNTIME_DIR = REPO_ROOT / "runtime"
CALLS_DIR = RUNTIME_DIR / "calls"


class StoreError(RuntimeError):
    pass


def _load_contract(name: str) -> Dict[str, Any]:
    path = CONTRACTS_DIR / f"{name}.schema.json"
    if not path.exists():
        raise StoreError(f"missing contract schema: {path}")
    try:
        return json.loads(path.read_text())
    except json.JSONDecodeError as err:
        raise StoreError(f"invalid schema JSON: {path}") from err


def _validate(payload: Dict[str, Any], schema_name: str) -> Dict[str, Any]:
    schema = _load_contract(schema_name)
    try:
        jsonschema_validate(payload, schema)
    except ValidationError as err:
        raise StoreError(f"{schema_name} validation failed: {err.message}") from err
    return payload


def ensure_runtime_layout() -> None:
    CALLS_DIR.mkdir(parents=True, exist_ok=True)


def call_id_from_audio(path: str) -> str:
    file_path = Path(path).expanduser().resolve()
    if not file_path.exists():
        raise StoreError(f"audio file not found: {file_path}")
    digest = hashlib.sha256()
    with file_path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def paths_for_call(call_id: str) -> Dict[str, str]:
    base_dir = CALLS_DIR / call_id
    audio_dir = base_dir / "audio"
    return {
        "audio": str(audio_dir),
        "transcript": str(base_dir / "transcript.json"),
        "metadata": str(base_dir / "metadata.json"),
        "rollup": str(base_dir / "rollup.json"),
        "call_record": str(base_dir / "call_record.json"),
    }


def _atomic_write_json(path: Path, payload: Dict[str, Any]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    tmp_path = path.with_suffix(path.suffix + ".tmp")
    tmp_path.write_text(json.dumps(payload, ensure_ascii=False))
    tmp_path.replace(path)


def write_call_record(call_record: Dict[str, Any]) -> Dict[str, Any]:
    validated = _validate(call_record, "call_record")
    call_id = validated["id"]
    call_dir = CALLS_DIR / call_id
    (call_dir / "audio").mkdir(parents=True, exist_ok=True)
    record_path = call_dir / "call_record.json"
    _atomic_write_json(record_path, validated)
    return validated


def _parse_ts(value: str) -> datetime | None:
    try:
        if value.endswith("Z"):
            value = value[:-1] + "+00:00"
        return datetime.fromisoformat(value)
    except ValueError:
        return None


def list_calls(limit: int = 100, since_hours: int = 24) -> List[Dict[str, Any]]:
    ensure_runtime_layout()
    cutoff = datetime.now(timezone.utc) - timedelta(hours=since_hours)
    records: List[Dict[str, Any]] = []
    for record_path in CALLS_DIR.glob("*/call_record.json"):
        try:
            payload = json.loads(record_path.read_text())
        except json.JSONDecodeError:
            continue
        ts_value = payload.get("ts", "")
        ts_parsed = _parse_ts(ts_value) if isinstance(ts_value, str) else None
        if ts_parsed and ts_parsed < cutoff:
            continue
        records.append(payload)
    records.sort(
        key=lambda item: _parse_ts(item.get("ts", "")) or datetime.min.replace(tzinfo=timezone.utc),
        reverse=True,
    )
    return records[:limit]


def _read_json_if_exists(path: Path) -> Dict[str, Any] | None:
    if not path.exists():
        return None
    try:
        return json.loads(path.read_text())
    except json.JSONDecodeError:
        return None


def read_call(call_id: str) -> Dict[str, Any]:
    base_paths = paths_for_call(call_id)
    record_path = Path(base_paths["call_record"])
    if not record_path.exists():
        raise StoreError(f"call record not found: {record_path}")
    call_record = json.loads(record_path.read_text())
    _validate(call_record, "call_record")
    return {
        "call_record": call_record,
        "transcript": _read_json_if_exists(Path(base_paths["transcript"])),
        "metadata": _read_json_if_exists(Path(base_paths["metadata"])),
        "rollup": _read_json_if_exists(Path(base_paths["rollup"])),
    }
