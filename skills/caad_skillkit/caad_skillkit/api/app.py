from __future__ import annotations

import json
import os
from pathlib import Path
from typing import Any, Dict, List

import requests
from jsonschema import validate as jsonschema_validate
from jsonschema.exceptions import ValidationError

from caad_skillkit.ai.schemas import get_schema
from caad_skillkit.api.errors import error_payload
from caad_skillkit.api.middleware import request_id_middleware
from core import store


def create_app():
    try:
        from fastapi import FastAPI, HTTPException, Query
        from fastapi.responses import JSONResponse
    except Exception as err:
        raise RuntimeError(f"FastAPI not available: {err}") from err

    app = FastAPI()
    request_id_middleware(app)

    def _validate(schema_name: str, payload: Dict[str, Any]) -> None:
        try:
            jsonschema_validate(payload, get_schema(schema_name))
        except ValidationError as err:
            raise HTTPException(
                status_code=500,
                detail=error_payload(f"{schema_name} validation failed: {err.message}", "schema_invalid"),
            ) from err

    def _read_json(path: Path, schema_name: str) -> Dict[str, Any]:
        if not path.exists():
            raise HTTPException(status_code=404, detail=error_payload("not found", "not_found"))
        try:
            payload = json.loads(path.read_text())
        except json.JSONDecodeError as err:
            raise HTTPException(status_code=500, detail=error_payload("invalid JSON", "invalid_json")) from err
        _validate(schema_name, payload)
        return payload

    @app.get("/healthz")
    def healthz():
        return {"ok": True}

    @app.get("/readyz")
    def readyz():
        store_ok = True
        try:
            store.ensure_runtime_layout()
        except Exception:
            store_ok = False
        payload = {"ok": store_ok, "store": store_ok}
        localai_base = (os.getenv("LOCALAI_BASE_URL") or "").rstrip("/")
        if localai_base:
            localai_ok = False
            try:
                resp = requests.get(f"{localai_base}/readyz", timeout=2)
                localai_ok = resp.status_code == 200
            except requests.RequestException:
                localai_ok = False
            payload["localai"] = localai_ok
        return payload

    @app.get("/calls")
    def list_calls(
        since_hours: int = Query(24, ge=1),
        limit: int = Query(100, ge=1, le=1000),
    ) -> List[Dict[str, Any]]:
        try:
            calls = store.list_calls(limit=limit, since_hours=since_hours)
        except Exception as err:
            raise HTTPException(status_code=500, detail=error_payload(str(err), "store_error")) from err
        for call in calls:
            _validate("call_record", call)
        return calls

    @app.get("/calls/{call_id}")
    def get_call(call_id: str) -> Dict[str, Any]:
        paths = store.paths_for_call(call_id)
        return _read_json(Path(paths["call_record"]), "call_record")

    @app.get("/calls/{call_id}/transcript")
    def get_transcript(call_id: str) -> Dict[str, Any]:
        paths = store.paths_for_call(call_id)
        return _read_json(Path(paths["transcript"]), "transcription")

    @app.get("/calls/{call_id}/metadata")
    def get_metadata(call_id: str) -> Dict[str, Any]:
        paths = store.paths_for_call(call_id)
        return _read_json(Path(paths["metadata"]), "metadata")

    @app.get("/calls/{call_id}/rollup")
    def get_rollup(call_id: str) -> Dict[str, Any]:
        paths = store.paths_for_call(call_id)
        return _read_json(Path(paths["rollup"]), "rollup")

    @app.exception_handler(Exception)
    async def handle_error(_, exc):
        return JSONResponse(status_code=500, content=error_payload(str(exc)))

    @app.exception_handler(HTTPException)
    async def handle_http_error(_, exc):
        detail = exc.detail
        if isinstance(detail, dict) and "error" in detail:
            payload = detail
        else:
            payload = error_payload(str(detail), "http_error")
        return JSONResponse(status_code=exc.status_code, content=payload)

    return app
