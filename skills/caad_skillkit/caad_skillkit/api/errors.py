from __future__ import annotations

from typing import Dict


def error_payload(message: str, code: str = "error") -> Dict[str, str]:
    return {"error": {"code": code, "message": message}}
