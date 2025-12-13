import json
import os
from http.server import BaseHTTPRequestHandler, HTTPServer
from typing import Any, Dict, List

from .client import AlertsClient


class Handler(BaseHTTPRequestHandler):
    client = AlertsClient(os.getenv("ALERTS_API_BASE"))

    def do_POST(self):
        length = int(self.headers.get("Content-Length", 0))
        raw = self.rfile.read(length) if length else b"{}"
        try:
            payload = json.loads(raw.decode() or "{}")
        except Exception:
            self.send_error(400, "invalid json")
            return
        tool = payload.get("tool")
        args: Dict[str, Any] = payload.get("args", {})
        try:
            result = self.dispatch(tool, args)
        except Exception as exc:  # noqa: BLE001
            self.send_error(500, str(exc))
            return
        self._respond(result)

    def do_GET(self):
        if self.path.rstrip("/") == "/healthz":
            self._respond({"ok": True})
            return
        self.send_error(404, "not found")

    def dispatch(self, tool: str, args: Dict[str, Any]):
        if tool in ("alerts.status", "status"):
            return self.client.status()
        if tool in ("alerts.backfill", "backfill"):
            return self.client.backfill(int(args.get("since_minutes", 60)), args.get("stages", []))
        if tool in ("alerts.reprocess", "reprocess"):
            return self.client.reprocess(args.get("call_id"), args.get("stage"), bool(args.get("force", False)))
        if tool in ("alerts.enqueue", "enqueue"):
            return self.client.enqueue_stage(args.get("call_id"), args.get("stage"), args.get("params", {}))
        if tool in ("alerts.jobs", "jobs"):
            return {"jobs": self.client.jobs()}
        if tool in ("alerts.briefing", "briefing"):
            data = self.client.briefing_data()
            return {"briefing_data": data}
        if tool in ("alerts.anomalies", "anomalies"):
            return self.client.anomalies(int(args.get("since_minutes", 60)))
        if tool in ("alerts.tail_job", "tail_job"):
            job_id = int(args.get("job_id", 0))
            seconds = int(args.get("seconds", 30))
            return {"lines": self.client.tail_job(job_id, seconds)}
        raise ValueError(f"unknown tool {tool}")

    def _respond(self, payload: Dict[str, Any]):
        data = json.dumps(payload).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def log_message(self, format: str, *args: Any) -> None:  # noqa: A003
        return


def run_server():
    port = int(os.getenv("MCP_PORT", "8787"))
    srv = HTTPServer(("0.0.0.0", port), Handler)
    srv.serve_forever()


if __name__ == "__main__":
    run_server()
