from __future__ import annotations

from caad_skillkit.api.errors import error_payload
from caad_skillkit.api.health import health_payload
from caad_skillkit.api.middleware import request_id_middleware


def create_app():
    try:
        from fastapi import FastAPI
        from fastapi.responses import JSONResponse
    except Exception as err:
        raise RuntimeError(f"FastAPI not available: {err}") from err

    app = FastAPI()
    request_id_middleware(app)

    @app.get("/healthz")
    def healthz():
        return health_payload(True)

    @app.get("/readyz")
    def readyz():
        return health_payload(True)

    @app.exception_handler(Exception)
    async def handle_error(_, exc):
        return JSONResponse(status_code=500, content=error_payload(str(exc)))

    return app
