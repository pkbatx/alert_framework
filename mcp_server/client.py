import json
import os
import time
import urllib.parse
import urllib.request
from typing import Any, Dict, List, Optional

DEFAULT_BASE = os.getenv("ALERTS_API_BASE", "http://localhost:8080")


def _request(method: str, url: str, payload: Optional[Dict[str, Any]] = None):
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

    def backfill(self, since_minutes: int, stages: List[str]) -> Dict[str, Any]:
        return self._post("/ops/backfill", {"since_minutes": since_minutes, "stages": stages})

    def reprocess(self, call_id: str, stage: str, force: bool = False) -> Dict[str, Any]:
        return self._post("/ops/reprocess", {"call_id": call_id, "stage": stage, "force": force})

    def enqueue_stage(self, call_id: str, stage: str, params: Optional[Dict[str, Any]] = None) -> Dict[str, Any]:
        return self._post("/ops/jobs/enqueue", {"call_id": call_id, "stage": stage, "params": params or {}})

    def jobs(self) -> List[Dict[str, Any]]:
        with _request("GET", f"{self.base}/ops/jobs") as resp:
            return json.loads(resp.read())

    def job_status(self, job_id: int) -> Dict[str, Any]:
        with _request("GET", f"{self.base}/ops/jobs/{job_id}") as resp:
            return json.loads(resp.read())

    def tail_job(self, job_id: int, seconds: int = 30) -> List[str]:
        end = time.time() + seconds
        req = urllib.request.Request(f"{self.base}/ops/jobs/{job_id}/logs", method="GET")
        lines: List[str] = []
        with urllib.request.urlopen(req, timeout=seconds) as resp:
            for raw in resp:
                text = raw.decode(errors="ignore").strip()
                if text:
                    lines.append(text)
                if time.time() >= end:
                    break
        return lines

    def briefing_data(self) -> Dict[str, Any]:
        with _request("GET", f"{self.base}/ops/briefing-data") as resp:
            return json.loads(resp.read())

    def anomalies(self, since_minutes: int) -> Dict[str, Any]:
        qs = urllib.parse.urlencode({"since_minutes": since_minutes})
        with _request("GET", f"{self.base}/ops/anomalies?{qs}") as resp:
            return json.loads(resp.read())

    def calls(self, limit: int = 100) -> Dict[str, Any]:
        qs = urllib.parse.urlencode({"limit": limit})
        with _request("GET", f"{self.base}/api/calls?{qs}") as resp:
            return json.loads(resp.read())

    def _post(self, path: str, payload: Dict[str, Any]) -> Dict[str, Any]:
        with _request("POST", f"{self.base}{path}", payload) as resp:
            return json.loads(resp.read())
