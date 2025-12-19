#!/usr/bin/env python3
from __future__ import annotations

import json
import os
import re
import subprocess
import sys
import time
from pathlib import Path
from typing import Any, Dict, List, Optional, Tuple

import requests
import typer
from jsonschema import ValidationError as JSONSchemaError
from jsonschema import validate as jsonschema_validate
from pydantic import BaseModel, Field

app = typer.Typer(add_completion=False)

DEFAULT_TIMEOUT = 30
HEALTH_TIMEOUT = 5
LOG_TRUNCATE = 4000


def emit_ok(data: Any) -> None:
    payload = {"ok": True, "data": data}
    sys.stdout.write(json.dumps(payload, ensure_ascii=False))
    sys.stdout.write("\n")


def emit_err(code: str, message: str, details: Optional[Dict[str, Any]] = None, status: Optional[int] = None) -> None:
    error: Dict[str, Any] = {"code": code, "message": message}
    if details:
        error["details"] = details
    if status is not None:
        error["status"] = status
    payload = {"ok": False, "error": error}
    sys.stdout.write(json.dumps(payload, ensure_ascii=False))
    sys.stdout.write("\n")
    raise typer.Exit(code=1)


def trim_env(value: Optional[str]) -> Optional[str]:
    if value is None:
        return None
    return value.strip()


def get_env(key: str, default: Optional[str] = None) -> Optional[str]:
    return trim_env(os.getenv(key)) or default


def find_repo_root() -> Path:
    current = Path.cwd().resolve()
    for parent in [current, *current.parents]:
        if (parent / ".git").exists() or (parent / "go.mod").exists() or (parent / "package.json").exists():
            return parent
    return current


def load_dotenv(root: Path) -> None:
    env_path = root / ".env"
    if not env_path.exists():
        return
    for line in env_path.read_text().splitlines():
        line = line.strip()
        if not line or line.startswith("#") or "=" not in line:
            continue
        key, value = line.split("=", 1)
        key = key.strip()
        if key and key not in os.environ:
            os.environ[key] = value.strip()


def ensure_repo_env_loaded() -> Path:
    root = find_repo_root()
    load_dotenv(root)
    return root


def build_base_url(base: str, suffix: str) -> str:
    base = base.rstrip("/")
    if base.endswith("/v1"):
        return f"{base}{suffix}"
    return f"{base}/v1{suffix}"


def extract_first_json_object(text: str) -> Optional[str]:
    start = text.find("{")
    if start == -1:
        return None
    depth = 0
    for idx in range(start, len(text)):
        char = text[idx]
        if char == "{":
            depth += 1
        elif char == "}":
            depth -= 1
            if depth == 0:
                return text[start : idx + 1]
    return None


class Segment(BaseModel):
    start: float
    end: float
    text: str


class TranscriptionResult(BaseModel):
    text: str
    language: str
    confidence: Optional[float] = None
    duration_s: Optional[float] = None
    segments: List[Segment] = Field(default_factory=list)


def resolve_transcribe_backend() -> Tuple[str, str, str]:
    backend = get_env("TRANSCRIBE_BACKEND")
    if backend:
        backend = backend.lower()
    if backend in ("openai", "localai"):
        pass
    elif get_env("LOCALAI_BASE_URL"):
        backend = "localai"
    elif get_env("OPENAI_API_KEY"):
        backend = "openai"
    else:
        emit_err("missing_backend", "No transcription backend configured", {"hint": "Set TRANSCRIBE_BACKEND or OPENAI_API_KEY/LOCALAI_BASE_URL."})
    if backend == "localai":
        base_url = get_env("LOCALAI_BASE_URL")
        if not base_url:
            emit_err("missing_localai_url", "LOCALAI_BASE_URL is required for localai backend")
        api_key = get_env("LOCALAI_API_KEY") or ""
        return backend, base_url, api_key
    base_url = get_env("OPENAI_BASE_URL") or get_env("OPENAI_API_BASE") or "https://api.openai.com"
    api_key = get_env("OPENAI_API_KEY")
    if not api_key:
        emit_err("missing_openai_key", "OPENAI_API_KEY is required for openai backend")
    return backend, base_url, api_key


def parse_segments(data: Any) -> List[Segment]:
    segments = []
    raw = data if isinstance(data, list) else []
    for item in raw:
        if not isinstance(item, dict):
            continue
        try:
            segments.append(Segment(start=float(item.get("start", 0)), end=float(item.get("end", 0)), text=str(item.get("text", ""))))
        except (TypeError, ValueError):
            continue
    return segments


@app.command("transcribe-audio")
def transcribe_audio(file_path: str = typer.Option(..., "--file-path")) -> None:
    ensure_repo_env_loaded()
    path = Path(file_path).expanduser().resolve()
    if not path.exists() or not path.is_file():
        emit_err("file_not_found", "Audio file not found", {"file_path": str(path)})
    backend, base_url, api_key = resolve_transcribe_backend()
    model = get_env("TRANSCRIBE_MODEL") or get_env("OPENAI_AUDIO_MODEL") or "whisper-1"
    language = get_env("TRANSCRIBE_LANGUAGE")
    endpoint = build_base_url(base_url, "/audio/transcriptions")

    headers: Dict[str, str] = {}
    if api_key:
        headers["Authorization"] = f"Bearer {api_key}"

    data: Dict[str, Any] = {
        "model": model,
        "response_format": "verbose_json",
    }
    if language:
        data["language"] = language

    files = {"file": (path.name, path.open("rb"), "application/octet-stream")}
    try:
        response = requests.post(endpoint, headers=headers, data=data, files=files, timeout=DEFAULT_TIMEOUT)
    except requests.RequestException as err:
        emit_err("transcribe_request_failed", "Transcription request failed", {"error": str(err)})
    finally:
        files["file"][1].close()

    if response.status_code >= 400:
        body = response.text[:LOG_TRUNCATE]
        emit_err("transcribe_failed", "Transcription failed", {"status": response.status_code, "body": body}, status=response.status_code)

    try:
        payload = response.json()
    except ValueError as err:
        emit_err("transcribe_invalid_response", "Transcription response was not JSON", {"error": str(err)})
    result = TranscriptionResult(
        text=str(payload.get("text", "")),
        language=str(payload.get("language", "")) if payload.get("language") is not None else "",
        confidence=payload.get("confidence"),
        duration_s=payload.get("duration"),
        segments=parse_segments(payload.get("segments")),
    )
    emit_ok(result.model_dump())


def resolve_llm_config(prefix: str) -> Tuple[str, str, str]:
    base_url = (
        get_env(f"{prefix}_LLM_BASE_URL")
        or get_env("ROLLUP_LLM_BASE_URL")
        or get_env("OPENAI_BASE_URL")
        or get_env("OPENAI_API_BASE")
        or get_env("LOCALAI_BASE_URL")
    )
    if not base_url:
        emit_err("missing_llm_url", "No LLM base URL configured", {"hint": "Set OPENAI_BASE_URL, OPENAI_API_BASE, or LOCALAI_BASE_URL."})
    model = get_env(f"{prefix}_LLM_MODEL") or get_env("ROLLUP_LLM_MODEL") or "gpt-4o-mini"
    api_key = get_env("OPENAI_API_KEY") or get_env("LOCALAI_API_KEY") or ""
    return base_url, model, api_key


def call_chat_completion(base_url: str, model: str, api_key: str, system_prompt: str, user_text: str) -> str:
    endpoint = build_base_url(base_url, "/chat/completions")
    headers: Dict[str, str] = {"Content-Type": "application/json"}
    if api_key:
        headers["Authorization"] = f"Bearer {api_key}"
    payload = {
        "model": model,
        "messages": [
            {"role": "system", "content": system_prompt},
            {"role": "user", "content": user_text},
        ],
        "temperature": 0,
    }
    try:
        response = requests.post(endpoint, headers=headers, json=payload, timeout=DEFAULT_TIMEOUT)
    except requests.RequestException as err:
        emit_err("llm_request_failed", "LLM request failed", {"error": str(err)})
    if response.status_code >= 400:
        body = response.text[:LOG_TRUNCATE]
        emit_err("llm_failed", "LLM call failed", {"status": response.status_code, "body": body}, status=response.status_code)
    try:
        data = response.json()
    except ValueError as err:
        emit_err("llm_invalid_response", "LLM response was not JSON", {"error": str(err)})
    try:
        return data["choices"][0]["message"]["content"]
    except (KeyError, IndexError, TypeError):
        emit_err("llm_invalid_response", "LLM response missing content")


def validate_json(content: str, schema: Dict[str, Any]) -> Dict[str, Any]:
    obj_text = extract_first_json_object(content)
    if not obj_text:
        emit_err("json_missing", "No JSON object found in LLM output")
    remainder = content.replace(obj_text, "", 1)
    if "{" in remainder or "}" in remainder:
        emit_err("json_multiple", "Multiple JSON objects found in LLM output")
    try:
        payload = json.loads(obj_text)
    except json.JSONDecodeError as err:
        emit_err("json_parse_failed", "Failed to parse JSON", {"error": str(err)})
    try:
        jsonschema_validate(payload, schema)
    except JSONSchemaError as err:
        emit_err("json_schema_failed", "JSON did not match schema", {"error": str(err)})
    return payload


@app.command("analyze-metadata")
def analyze_metadata(text: str = typer.Option(..., "--text")) -> None:
    ensure_repo_env_loaded()
    base_url, model, api_key = resolve_llm_config("METADATA")
    system_prompt = (
        "Return a single JSON object only. Do not include prose, markdown, or multiple objects. "
        "If unsure, return an empty JSON object {}."
    )
    content = call_chat_completion(base_url, model, api_key, system_prompt, text)
    schema = {"type": "object"}
    payload = validate_json(content, schema)
    emit_ok(payload)


@app.command("analyze-rollup")
def analyze_rollup(text: str = typer.Option(..., "--text")) -> None:
    ensure_repo_env_loaded()
    base_url, model, api_key = resolve_llm_config("ROLLUP")
    system_prompt = (
        "Return a single JSON object only with keys: title, summary, evidence, merge_suggestion, confidence. "
        "No prose, no markdown, no additional keys."
    )
    content = call_chat_completion(base_url, model, api_key, system_prompt, text)
    schema = {
        "type": "object",
        "additionalProperties": False,
        "required": ["title", "summary", "evidence", "merge_suggestion", "confidence"],
        "properties": {
            "title": {"type": "string"},
            "summary": {"type": "string"},
            "evidence": {"type": "array", "items": {"type": "string"}},
            "merge_suggestion": {"type": "string", "enum": ["merge", "keep"]},
            "confidence": {"type": "string", "enum": ["low", "medium", "high"]},
        },
    }
    payload = validate_json(content, schema)
    emit_ok(payload)


def locate_compose_file(explicit: Optional[str] = None) -> Path:
    if explicit:
        path = Path(explicit).expanduser().resolve()
        if not path.exists():
            emit_err("compose_not_found", "Compose file not found", {"compose_file": str(path)})
        return path
    current = Path.cwd().resolve()
    for parent in [current, *current.parents]:
        for name in ("docker-compose.yml", "compose.yml"):
            candidate = parent / name
            if candidate.exists():
                return candidate
    emit_err("compose_not_found", "Unable to locate docker-compose.yml or compose.yml")
    return Path("docker-compose.yml")


def run_compose(args: List[str], compose_file: Path, timeout: int = DEFAULT_TIMEOUT) -> subprocess.CompletedProcess:
    command = ["docker", "compose", "-f", str(compose_file)] + args
    try:
        return subprocess.run(command, capture_output=True, text=True, timeout=timeout, check=False)
    except (subprocess.SubprocessError, FileNotFoundError) as err:
        emit_err("compose_failed", "docker compose invocation failed", {"error": str(err)})


def parse_ps_output(raw: str) -> List[Dict[str, Any]]:
    services = []
    for line in raw.splitlines():
        line = line.strip()
        if not line:
            continue
        try:
            item = json.loads(line)
        except json.JSONDecodeError:
            continue
        ports = []
        for pub in item.get("Publishers") or []:
            ports.append(
                {
                    "url": pub.get("URL"),
                    "published": pub.get("PublishedPort"),
                    "target": pub.get("TargetPort"),
                    "protocol": pub.get("Protocol"),
                }
            )
        status = item.get("Status", "") or ""
        health = "unknown"
        if "(healthy)" in status:
            health = "healthy"
        elif "(unhealthy)" in status:
            health = "unhealthy"
        services.append(
            {
                "service": item.get("Service"),
                "state": item.get("State"),
                "status": status,
                "health": health,
                "ports": ports,
            }
        )
    return services


@app.command("list-compose-services")
def list_compose_services(compose_file: Optional[str] = typer.Option(None, "--compose-file")) -> None:
    ensure_repo_env_loaded()
    compose_path = locate_compose_file(compose_file)
    proc = run_compose(["ps", "--format", "json"], compose_path, timeout=HEALTH_TIMEOUT)
    if proc.returncode != 0:
        emit_err("compose_ps_failed", "docker compose ps failed", {"stderr": proc.stderr.strip()})
    emit_ok(parse_ps_output(proc.stdout))


@app.command("start-localai")
def start_localai(compose_file: Optional[str] = typer.Option(None, "--compose-file")) -> None:
    ensure_repo_env_loaded()
    compose_path = locate_compose_file(compose_file)
    services_proc = run_compose(["config", "--services"], compose_path, timeout=HEALTH_TIMEOUT)
    if services_proc.returncode != 0:
        emit_err("compose_services_failed", "docker compose config failed", {"stderr": services_proc.stderr.strip()})
    services = [line.strip() for line in services_proc.stdout.splitlines() if line.strip()]
    if "localai" not in services:
        emit_err("service_not_found", "localai service not found", {"services": services})

    up_proc = run_compose(["up", "-d", "localai"], compose_path, timeout=DEFAULT_TIMEOUT)
    if up_proc.returncode != 0:
        emit_err("compose_up_failed", "docker compose up failed", {"stderr": up_proc.stderr.strip()})

    ps_proc = run_compose(["ps", "--format", "json", "localai"], compose_path, timeout=HEALTH_TIMEOUT)
    if ps_proc.returncode != 0:
        emit_err("compose_ps_failed", "docker compose ps failed", {"stderr": ps_proc.stderr.strip()})
    status = parse_ps_output(ps_proc.stdout)
    emit_ok({"compose_file": str(compose_path), "service": "localai", "status": status})


@app.command("check-localai-health")
def check_localai_health() -> None:
    ensure_repo_env_loaded()
    base_url = get_env("LOCALAI_BASE_URL") or get_env("LLM_BASE_URL") or "http://localhost:8080"
    url = base_url.rstrip("/") + "/readyz"
    try:
        response = requests.get(url, timeout=HEALTH_TIMEOUT)
    except requests.RequestException as err:
        emit_err("localai_unreachable", "LocalAI unreachable", {"error": str(err), "url": url})
    body = response.text[:LOG_TRUNCATE]
    emit_ok({"url": url, "status": response.status_code, "ok": response.ok, "body": body})


def classify_logs(logs: str) -> List[str]:
    patterns = {
        "missing_env": ["not set", "missing", "OPENAI_API_KEY", "API key"],
        "permission_denied": ["permission denied", "read-only file system"],
        "port_bind": ["address already in use", "bind"],
        "model_not_found": ["model not found", "no such model"],
    }
    hits = []
    lowered = logs.lower()
    for name, tokens in patterns.items():
        if any(token.lower() in lowered for token in tokens):
            hits.append(name)
    return hits


@app.command("triage-service")
def triage_service(
    service_name: str = typer.Option(..., "--service-name"),
    compose_file: Optional[str] = typer.Option(None, "--compose-file"),
) -> None:
    ensure_repo_env_loaded()
    compose_path = locate_compose_file(compose_file)
    ps_proc = run_compose(["ps", "--format", "json", service_name], compose_path, timeout=HEALTH_TIMEOUT)
    if ps_proc.returncode != 0:
        emit_err("compose_ps_failed", "docker compose ps failed", {"stderr": ps_proc.stderr.strip()})
    status = parse_ps_output(ps_proc.stdout)

    logs_proc = run_compose(["logs", "--tail", "200", service_name], compose_path, timeout=HEALTH_TIMEOUT)
    logs = logs_proc.stdout[-LOG_TRUNCATE:] if logs_proc.stdout else ""
    issues = classify_logs(logs)

    emit_ok({
        "service": service_name,
        "status": status,
        "issues": issues,
        "logs": logs,
    })


@app.command("ensure-env-key")
def ensure_env_key(
    key: str = typer.Option(..., "--key"),
    value: Optional[str] = typer.Option(None, "--value"),
    keychain_service: Optional[str] = typer.Option(None, "--keychain-service"),
) -> None:
    root = ensure_repo_env_loaded()
    env_path = root / ".env"

    source_value = value
    if source_value is None and keychain_service:
        try:
            proc = subprocess.run(
                ["security", "find-generic-password", "-w", "-s", keychain_service],
                capture_output=True,
                text=True,
                timeout=HEALTH_TIMEOUT,
                check=False,
            )
        except (subprocess.SubprocessError, FileNotFoundError) as err:
            emit_err("keychain_failed", "Keychain lookup failed", {"error": str(err)})
        if proc.returncode != 0:
            emit_err("keychain_failed", "Keychain lookup failed", {"stderr": proc.stderr.strip()})
        source_value = proc.stdout

    if source_value is None:
        source_value = get_env(key)

    if source_value is None:
        emit_err("missing_value", "No value available to set", {"key": key})

    leading_ws = bool(re.match(r"^\s", source_value))
    trailing_ws = bool(re.search(r"\s$", source_value))
    contains_newline = "\n" in source_value or "\r" in source_value

    cleaned = source_value.strip()
    length = len(cleaned)

    lines = env_path.read_text().splitlines() if env_path.exists() else []
    updated = False
    new_lines = []
    for line in lines:
        if not line.strip() or line.strip().startswith("#") or "=" not in line:
            new_lines.append(line)
            continue
        existing_key, _ = line.split("=", 1)
        if existing_key.strip() == key:
            new_lines.append(f"{key}={cleaned}")
            updated = True
        else:
            new_lines.append(line)
    if not updated:
        new_lines.append(f"{key}={cleaned}")

    env_path.write_text("\n".join(new_lines) + "\n")
    os.environ[key] = cleaned

    emit_ok({
        "present": True,
        "length": length,
        "leading_whitespace": leading_ws,
        "trailing_whitespace": trailing_ws,
        "contains_newline": contains_newline,
    })


def find_fastapi_root(root: Path) -> Optional[Path]:
    for candidate in root.rglob("main.py"):
        try:
            text = candidate.read_text()
        except OSError:
            continue
        if "FastAPI" in text:
            return candidate.parent
    return None


@app.command("scaffold-fastapi-component")
def scaffold_fastapi_component(
    name: str = typer.Option(..., "--name"),
    component_type: str = typer.Option(..., "--type"),
) -> None:
    root = ensure_repo_env_loaded()
    fastapi_root = find_fastapi_root(root)
    if not fastapi_root:
        emit_err("fastapi_not_found", "FastAPI project not found in repo")

    component_type = component_type.lower()
    if component_type not in ("route", "dependency", "service"):
        emit_err("invalid_type", "Type must be route, dependency, or service")

    target_dir_map = {
        "route": ["routes", "routers"],
        "dependency": ["dependencies"],
        "service": ["services"],
    }
    target_dir = None
    for candidate in target_dir_map[component_type]:
        if (fastapi_root / candidate).exists():
            target_dir = fastapi_root / candidate
            break
    if target_dir is None:
        emit_err("layout_missing", "Expected FastAPI layout directory not found", {"fastapi_root": str(fastapi_root), "expected": target_dir_map[component_type]})

    filename = f"{name}.py"
    file_path = target_dir / filename
    if file_path.exists():
        emit_err("file_exists", "Target file already exists", {"file_path": str(file_path)})

    if component_type == "route":
        content = (
            "from fastapi import APIRouter\n\n"
            f"router = APIRouter(prefix='/{name}', tags=['{name}'])\n\n"
            "@router.get('/')\n"
            "def list_items():\n"
            "    return {'status': 'ok'}\n"
        )
    elif component_type == "dependency":
        content = (
            "def get_dependency():\n"
            "    return {'status': 'ok'}\n"
        )
    else:
        content = (
            "class Service:\n"
            "    def health(self):\n"
            "        return {'status': 'ok'}\n"
        )

    file_path.write_text(content)

    emit_ok({
        "file_path": str(file_path),
        "lines": len(content.splitlines()),
        "component_type": component_type,
    })


if __name__ == "__main__":
    app()
