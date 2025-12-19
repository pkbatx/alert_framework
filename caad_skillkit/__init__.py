from __future__ import annotations

import pkgutil
from pathlib import Path

__path__ = pkgutil.extend_path(__path__, __name__)  # type: ignore[name-defined]

_skillkit_root = Path(__file__).resolve().parent.parent / "skills" / "caad_skillkit" / "caad_skillkit"
if _skillkit_root.exists():
    __path__.append(str(_skillkit_root))
