from __future__ import annotations

import json
import time
from typing import Any, Dict, Optional

import requests

from caad_skillkit.config import get_env

DEFAULT_TIMEOUT = 45


def _timeout() -> int:
    return int(get_env("AI_TIMEOUT_S", str(DEFAULT_TIMEOUT)) or DEFAULT_TIMEOUT)


def _post(url: str, payload: Dict[str, Any], headers: Dict[str, str], timeout_s: int, retries: int = 1) -> requests.Response:
    last_err: Optional[Exception] = None
    for attempt in range(retries + 1):
        try:
            return requests.post(url, headers=headers, json=payload, timeout=timeout_s)
        except requests.RequestException as err:
            last_err = err
            if attempt >= retries:
                raise
            time.sleep(0.5)
    raise last_err  # type: ignore[misc]


def localai_chat(base_url: str, model: str, system_prompt: str, user_text: str) -> str:
    url = base_url.rstrip("/") + "/v1/chat/completions"
    payload = {
        "model": model,
        "temperature": 0,
        "messages": [
            {"role": "system", "content": system_prompt},
            {"role": "user", "content": user_text},
        ],
    }
    headers = {"Content-Type": "application/json"}
    resp = _post(url, payload, headers, _timeout(), retries=1)
    resp.raise_for_status()
    data = resp.json()
    return data["choices"][0]["message"]["content"]


def openai_chat(base_url: str, api_key: str, model: str, system_prompt: str, user_text: str) -> str:
    url = base_url.rstrip("/") + "/v1/chat/completions"
    payload = {
        "model": model,
        "temperature": 0,
        "messages": [
            {"role": "system", "content": system_prompt},
            {"role": "user", "content": user_text},
        ],
    }
    headers = {"Content-Type": "application/json", "Authorization": f"Bearer {api_key}"}
    resp = _post(url, payload, headers, _timeout(), retries=1)
    resp.raise_for_status()
    data = resp.json()
    return data["choices"][0]["message"]["content"]


def localai_transcribe(base_url: str, model: str, file_path: str) -> Dict[str, Any]:
    url = base_url.rstrip("/") + "/v1/audio/transcriptions"
    headers: Dict[str, str] = {}
    data = {"model": model}
    with open(file_path, "rb") as handle:
        files = {"file": (file_path, handle, "application/octet-stream")}
        resp = requests.post(url, data=data, files=files, headers=headers, timeout=_timeout())
    resp.raise_for_status()
    return resp.json()


def openai_transcribe(api_key: str, model: str, file_path: str) -> Dict[str, Any]:
    try:
        from openai import OpenAI
    except Exception as err:  # pragma: no cover - import guard
        raise RuntimeError(f"openai SDK unavailable: {err}") from err

    client = OpenAI(api_key=api_key)
    with open(file_path, "rb") as handle:
        result = client.audio.transcriptions.create(
            model=model,
            file=handle,
            response_format="verbose_json",
        )
    raw = json.loads(result.model_dump_json())
    return raw
