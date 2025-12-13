import json
import os
import time
import urllib.parse
import urllib.request
from typing import Any, Dict, List, Optional

DEFAULT_BASE = os.getenv("ALERTS_API_BASE", "http://localhost:8000")


def _request(method: str, path: str, payload: Optional[Dict[str, Any]] = None, stream: bool = False):
    url = urllib.parse.urljoin(DEFAULT_BASE, path)
    data = None
    headers = {"Content-Type": "application/json"}
    if payload is not None:
        data = json.dumps(payload).encode()
    req = urllib.request.Request(url, data=data, method=method, headers=headers)
    return urllib.request.urlopen(req)


class AlertsClient:
    def __init__(self, base: Optional[str] = None):
        self.base = base or DEFAULT_BASE

    def status(self) -> Dict[str, Any]:
        with _request("GET", f"{self.base}/ops/status") as resp:
            return json.loads(resp.read())

    def run_transcribe(self, **kwargs) -> Dict[str, Any]:
        return self._post("/ops/transcribe/run", kwargs)

    def run_enrich(self, **kwargs) -> Dict[str, Any]:
        return self._post("/ops/enrich/run", kwargs)

    def run_publish(self, **kwargs) -> Dict[str, Any]:
        return self._post("/ops/publish/run", kwargs)

    def reprocess(self, call_id: str, stage: str, force: bool = False) -> Dict[str, Any]:
        return self._post("/ops/reprocess", {"call_id": call_id, "stage": stage, "force": force})

    def jobs(self) -> Dict[str, Any]:
        with _request("GET", f"{self.base}/ops/jobs") as resp:
            return json.loads(resp.read())

    def job_status(self, job_id: str) -> Dict[str, Any]:
        with _request("GET", f"{self.base}/ops/jobs/{job_id}") as resp:
            return json.loads(resp.read())

    def tail_job(self, job_id: str, seconds: int = 30) -> List[str]:
        end = time.time() + seconds
        url = f"{self.base}/ops/jobs/{job_id}/logs"
        req = urllib.request.Request(url, method="GET")
        lines: List[str] = []
        with urllib.request.urlopen(req, timeout=seconds) as resp:
            for raw in resp:
                try:
                    text = raw.decode()
                except Exception:
                    continue
                if text.strip():
                    lines.append(text.strip())
                if time.time() >= end:
                    break
        return lines

    def briefing(self, since_minutes: int = 360, format: str = "bullets") -> Dict[str, Any]:
        status = self.status()
        calls = self._get_calls(since_minutes)
        summary = self._summarize(calls, format)
        return {"status": status, "calls": calls, "summary": summary}

    def _get_calls(self, since_minutes: int) -> List[Dict[str, Any]]:
        qs = urllib.parse.urlencode({"since_minutes": since_minutes, "limit": 200})
        with _request("GET", f"{self.base}/api/calls?{qs}") as resp:
            data = json.loads(resp.read())
        return data.get("calls", [])

    def _summarize(self, calls: List[Dict[str, Any]], fmt: str) -> str:
        if not calls:
            return "No calls in window."
        tags: Dict[str, int] = {}
        towns: Dict[str, int] = {}
        for c in calls:
            for tag in c.get("tags", []):
                tags[tag] = tags.get(tag, 0) + 1
            loc = (c.get("location") or "").strip()
            if loc:
                towns[loc] = towns.get(loc, 0) + 1
        tag_top = sorted(tags.items(), key=lambda kv: kv[1], reverse=True)[:3]
        town_top = sorted(towns.items(), key=lambda kv: kv[1], reverse=True)[:3]
        lines = [
            f"Calls: {len(calls)} in last window",
            "Top tags: " + ", ".join([f"{t} ({n})" for t, n in tag_top]) if tag_top else "Top tags: none",
            "Top locations: " + ", ".join([f"{t} ({n})" for t, n in town_top]) if town_top else "Top locations: none",
        ]
        return "\n".join(lines)

    def _post(self, path: str, payload: Dict[str, Any]) -> Dict[str, Any]:
        with _request("POST", f"{self.base}{path}", payload) as resp:
            return json.loads(resp.read())

