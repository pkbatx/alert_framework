from __future__ import annotations

import json
import os
import sys
from pathlib import Path
from typing import Any, Dict

_SKILLKIT_ROOT = Path(__file__).resolve().parents[1]
_REPO_ROOT = None
for parent in Path(__file__).resolve().parents:
    if (parent / "contracts").is_dir():
        _REPO_ROOT = parent
        break
for path in [_SKILLKIT_ROOT, _REPO_ROOT]:
    if path and str(path) not in sys.path:
        sys.path.insert(0, str(path))

import click
from jsonschema import validate as jsonschema_validate
from jsonschema.exceptions import ValidationError

from caad_skillkit.ai.rollups import SkillError, build_metadata, build_rollup
from caad_skillkit.ai.schemas import get_schema
from caad_skillkit.ai.transcribe import TranscribeError, transcribe_audio
from caad_skillkit.config import load_env_file
from caad_skillkit.ops.env import env_check, env_write, env_write_from_keychain
from caad_skillkit.ops.smoke import localai_chat_test, localai_models, localai_readyz
from core import store as core_store
from worker import ingest as ingest_worker
from caad_skillkit.docs.generator import docs_all, docs_boundaries, docs_readme, docs_structure, docs_validate


def emit_ok(data: Any, pretty: bool = False) -> None:
    payload = {"ok": True, "data": data}
    if pretty:
        sys.stdout.write(json.dumps(payload, indent=2, ensure_ascii=False))
    else:
        sys.stdout.write(json.dumps(payload, ensure_ascii=False))
    sys.stdout.write("\n")


def emit_err(code: str, message: str, details: Dict[str, Any] | None = None) -> None:
    payload: Dict[str, Any] = {"ok": False, "error": {"code": code, "message": message}}
    if details:
        payload["error"]["details"] = details
    sys.stderr.write(json.dumps(payload, ensure_ascii=False))
    sys.stderr.write("\n")
    raise SystemExit(1)


def load_repo_env() -> None:
    root = Path.cwd()
    load_env_file(root / ".env")


@click.group()
def cli() -> None:
    """CAAD skillkit CLI."""


@cli.group()
def env() -> None:
    """Environment helpers."""


@env.command("check")
@click.argument("keys", nargs=-1, required=True)
def env_check_cmd(keys: tuple[str, ...]) -> None:
    load_repo_env()
    emit_ok(env_check(list(keys)))


@env.command("set")
@click.argument("key")
@click.option("--value", "value", help="Value to set")
@click.option("--from-keychain", "keychain", help="macOS Keychain service name")
@click.option("--account", "account", help="macOS Keychain account")
def env_set_cmd(key: str, value: str | None, keychain: str | None, account: str | None) -> None:
    load_repo_env()
    env_path = Path.cwd() / ".env"
    try:
        if keychain:
            result = env_write_from_keychain(env_path, key, keychain, account)
        elif value is not None:
            result = env_write(env_path, key, value)
        else:
            emit_err("missing_value", "Provide --value or --from-keychain")
        emit_ok(result)
    except Exception as err:
        emit_err("env_write_failed", str(err))


@cli.group()
def localai() -> None:
    """LocalAI helpers."""


@localai.command("readyz")
def localai_readyz_cmd() -> None:
    load_repo_env()
    try:
        emit_ok(localai_readyz())
    except Exception as err:
        emit_err("localai_readyz_failed", str(err))


@localai.command("models")
def localai_models_cmd() -> None:
    load_repo_env()
    try:
        emit_ok(localai_models())
    except Exception as err:
        emit_err("localai_models_failed", str(err))


@localai.command("chat-test")
@click.option("--model", "model", required=True)
def localai_chat_test_cmd(model: str) -> None:
    load_repo_env()
    try:
        emit_ok(localai_chat_test(model))
    except Exception as err:
        emit_err("localai_chat_failed", str(err))


@cli.group()
def ai() -> None:
    """AI helpers."""


@ai.command("metadata")
@click.option("--input", "input_path", required=True, type=click.Path(exists=True, dir_okay=False))
@click.option("--pretty", is_flag=True, default=False)
def ai_metadata_cmd(input_path: str, pretty: bool) -> None:
    load_repo_env()
    text = Path(input_path).read_text()
    try:
        result = build_metadata(text)
    except SkillError as err:
        emit_err("metadata_failed", str(err))
    emit_ok(result, pretty=pretty)


@ai.command("rollup")
@click.option("--input", "input_path", required=True, type=click.Path(exists=True, dir_okay=False))
@click.option("--pretty", is_flag=True, default=False)
def ai_rollup_cmd(input_path: str, pretty: bool) -> None:
    load_repo_env()
    text = Path(input_path).read_text()
    try:
        result = build_rollup(text)
    except SkillError as err:
        emit_err("rollup_failed", str(err))
    emit_ok(result, pretty=pretty)


@ai.command("transcribe")
@click.option("--input", "input_path", required=True, type=click.Path(exists=True, dir_okay=False))
@click.option("--out", "output_path", required=True, type=click.Path(dir_okay=False))
@click.option("--backend", "backend", type=click.Choice(["openai", "localai"], case_sensitive=False))
def ai_transcribe_cmd(input_path: str, output_path: str, backend: str | None) -> None:
    load_repo_env()
    if backend:
        os.environ["TRANSCRIBE_BACKEND"] = backend
    try:
        result = transcribe_audio(input_path)
    except TranscribeError as err:
        emit_err("transcribe_failed", str(err))
    Path(output_path).write_text(json.dumps(result, ensure_ascii=False))
    emit_ok({"output": str(Path(output_path).resolve())})


@cli.command("validate")
@click.argument("schema_type", type=click.Choice(["metadata", "rollup", "transcription", "call_record"]))
@click.option("--input", "input_path", required=True, type=click.Path(exists=True, dir_okay=False))
def validate_cmd(schema_type: str, input_path: str) -> None:
    load_repo_env()
    payload = json.loads(Path(input_path).read_text())
    try:
        jsonschema_validate(payload, get_schema(schema_type))
    except ValidationError as err:
        emit_err("schema_validation_failed", err.message, {"schema": schema_type})
    emit_ok({"schema": schema_type, "valid": True})


@cli.group()
def store() -> None:
    """Artifact store helpers."""


@store.command("list")
@click.option("--since-hours", "since_hours", type=int, default=24, show_default=True)
@click.option("--limit", "limit", type=int, default=100, show_default=True)
def store_list_cmd(since_hours: int, limit: int) -> None:
    load_repo_env()
    try:
        emit_ok(core_store.list_calls(limit=limit, since_hours=since_hours))
    except Exception as err:
        emit_err("store_list_failed", str(err))


@store.command("show")
@click.argument("call_id")
def store_show_cmd(call_id: str) -> None:
    load_repo_env()
    try:
        emit_ok(core_store.read_call(call_id))
    except Exception as err:
        emit_err("store_show_failed", str(err))


@store.command("paths")
@click.argument("call_id")
def store_paths_cmd(call_id: str) -> None:
    load_repo_env()
    emit_ok(core_store.paths_for_call(call_id))


@cli.group()
def worker() -> None:
    """Ingestion worker helpers."""


@worker.command("run")
def worker_run_cmd() -> None:
    load_repo_env()
    ingest_worker.run_watcher()


@worker.command("process")
@click.option("--input", "input_path", required=True, type=click.Path(exists=True, dir_okay=False))
def worker_process_cmd(input_path: str) -> None:
    load_repo_env()
    record = ingest_worker.process_file(Path(input_path), Path(os.getenv("CALLS_DIR") or "runtime/inbox"))
    sys.stdout.write(json.dumps(record, ensure_ascii=False))
    sys.stdout.write("\n")


@cli.group()
def docs() -> None:
    """Documentation helpers."""


def _resolve_write(write: bool) -> bool:
    return write


@docs.command("structure")
@click.option("--dry-run/--write", "write", default=False)
def docs_structure_cmd(write: bool) -> None:
    try:
        result = docs_structure(_resolve_write(write))
        emit_ok({"changes": [str(change.path) for change in result if change.changed]})
    except Exception as err:
        emit_err("docs_structure_failed", str(err))


@docs.command("readme")
@click.option("--dry-run/--write", "write", default=False)
def docs_readme_cmd(write: bool) -> None:
    try:
        result = docs_readme(_resolve_write(write))
        emit_ok({"changes": [str(change.path) for change in result if change.changed]})
    except Exception as err:
        emit_err("docs_readme_failed", str(err))


@docs.command("boundaries")
@click.option("--dry-run/--write", "write", default=False)
def docs_boundaries_cmd(write: bool) -> None:
    try:
        result = docs_boundaries(_resolve_write(write))
        emit_ok({"changes": [str(change.path) for change in result if change.changed]})
    except Exception as err:
        emit_err("docs_boundaries_failed", str(err))


@docs.command("validate")
def docs_validate_cmd() -> None:
    try:
        emit_ok(docs_validate())
    except Exception as err:
        emit_err("docs_validate_failed", str(err))


@docs.command("all")
@click.option("--dry-run/--write", "write", default=False)
def docs_all_cmd(write: bool) -> None:
    try:
        emit_ok(docs_all(_resolve_write(write)))
    except Exception as err:
        emit_err("docs_all_failed", str(err))


def main() -> None:
    try:
        cli()
    except SystemExit:
        raise
    except Exception as err:
        emit_err("unexpected_error", str(err))


if __name__ == "__main__":
    main()
