from __future__ import annotations

import subprocess
from pathlib import Path
from typing import List, Optional


def find_compose_file(start: Optional[Path] = None) -> Path:
    current = (start or Path.cwd()).resolve()
    for parent in [current, *current.parents]:
        for name in ("docker-compose.yml", "compose.yml"):
            candidate = parent / name
            if candidate.exists():
                return candidate
    raise FileNotFoundError("compose file not found")


def run_compose(compose_file: Path, args: List[str], timeout_s: int = 30) -> subprocess.CompletedProcess:
    cmd = ["docker", "compose", "-f", str(compose_file)] + args
    return subprocess.run(cmd, capture_output=True, text=True, timeout=timeout_s, check=False)
