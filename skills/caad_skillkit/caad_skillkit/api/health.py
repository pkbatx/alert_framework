from __future__ import annotations

from typing import Dict


def health_payload(ok: bool = True) -> Dict[str, object]:
    return {"ok": ok}
