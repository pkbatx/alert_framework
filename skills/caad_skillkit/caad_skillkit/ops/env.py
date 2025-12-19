from __future__ import annotations

import os
import re
import subprocess
from pathlib import Path
from typing import Dict, List, Optional


def _trim(value: str) -> str:
    return value.strip()


def env_check(keys: List[str]) -> List[Dict[str, object]]:
    results = []
    for key in keys:
        raw = os.getenv(key)
        if raw is None:
            results.append(
                {
                    "key": key,
                    "present": False,
                    "length": 0,
                    "leading_whitespace": False,
                    "trailing_whitespace": False,
                    "contains_newline": False,
                }
            )
            continue
        results.append(
            {
                "key": key,
                "present": True,
                "length": len(raw.strip()),
                "leading_whitespace": bool(re.match(r"^\s", raw)),
                "trailing_whitespace": bool(re.search(r"\s$", raw)),
                "contains_newline": "\n" in raw or "\r" in raw,
            }
        )
    return results


def _keychain_get(service: str, account: Optional[str] = None) -> str:
    cmd = ["security", "find-generic-password", "-w", "-s", service]
    if account:
        cmd += ["-a", account]
    proc = subprocess.run(cmd, capture_output=True, text=True, check=False)
    if proc.returncode != 0:
        raise RuntimeError(proc.stderr.strip() or "keychain lookup failed")
    return proc.stdout.strip()


def env_write(env_path: Path, key: str, value: str) -> Dict[str, object]:
    cleaned = _trim(value)
    lines = env_path.read_text().splitlines() if env_path.exists() else []
    updated = False
    new_lines = []
    for line in lines:
        if not line.strip() or line.strip().startswith("#") or "=" not in line:
            new_lines.append(line)
            continue
        k, _ = line.split("=", 1)
        if k.strip() == key:
            new_lines.append(f"{key}={cleaned}")
            updated = True
        else:
            new_lines.append(line)
    if not updated:
        new_lines.append(f"{key}={cleaned}")
    env_path.write_text("\n".join(new_lines) + "\n")
    os.environ[key] = cleaned
    return {
        "present": True,
        "length": len(cleaned),
        "leading_whitespace": False,
        "trailing_whitespace": False,
        "contains_newline": False,
    }


def env_write_from_keychain(env_path: Path, key: str, service: str, account: Optional[str] = None) -> Dict[str, object]:
    value = _keychain_get(service, account)
    return env_write(env_path, key, value)
