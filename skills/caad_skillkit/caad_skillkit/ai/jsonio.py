from __future__ import annotations

import json
import re
from typing import Optional

CODE_FENCE_RE = re.compile(r"```(?:json)?|```", re.IGNORECASE)


def strip_code_fences(text: str) -> str:
    cleaned = CODE_FENCE_RE.sub("", text)
    return cleaned.strip()


def extract_first_json_object(text: str) -> Optional[str]:
    start = text.find("{")
    if start == -1:
        return None
    depth = 0
    for idx in range(start, len(text)):
        ch = text[idx]
        if ch == "{":
            depth += 1
        elif ch == "}":
            depth -= 1
            if depth == 0:
                return text[start : idx + 1]
    return None


def parse_first_json_object(text: str) -> dict:
    cleaned = strip_code_fences(text)
    obj_text = extract_first_json_object(cleaned)
    if not obj_text:
        raise ValueError("no JSON object found")
    remainder = cleaned.replace(obj_text, "", 1)
    if "{" in remainder or "}" in remainder:
        raise ValueError("multiple JSON objects found")
    return json.loads(obj_text)
