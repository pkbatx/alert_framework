from __future__ import annotations

import uuid


def request_id_middleware(app):
    @app.middleware("http")
    async def add_request_id(request, call_next):
        request_id = request.headers.get("X-Request-Id") or str(uuid.uuid4())
        response = await call_next(request)
        response.headers["X-Request-Id"] = request_id
        return response
