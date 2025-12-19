from __future__ import annotations

import requests

from caad_skillkit.config import get_env, load_settings


def _base_url() -> str:
    return get_env("LOCALAI_BASE_URL") or load_settings().localai_base_url


def localai_readyz() -> dict:
    url = _base_url().rstrip("/") + "/readyz"
    resp = requests.get(url, timeout=5)
    return {"url": url, "status": resp.status_code, "ok": resp.ok, "body": resp.text[:2000]}


def localai_models() -> dict:
    url = _base_url().rstrip("/") + "/v1/models"
    resp = requests.get(url, timeout=10)
    body = resp.json() if resp.ok else {"error": resp.text[:2000]}
    return {"url": url, "status": resp.status_code, "ok": resp.ok, "body": body}


def localai_chat_test(model: str) -> dict:
    url = _base_url().rstrip("/") + "/v1/chat/completions"
    payload = {
        "model": model,
        "temperature": 0,
        "messages": [
            {"role": "system", "content": "Respond with the single word ok."},
            {"role": "user", "content": "ping"},
        ],
    }
    resp = requests.post(url, json=payload, timeout=10)
    content = ""
    if resp.ok:
        data = resp.json()
        content = data["choices"][0]["message"]["content"]
    return {"url": url, "status": resp.status_code, "ok": resp.ok, "content": content}
