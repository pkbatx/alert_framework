import json
import os
from http.server import BaseHTTPRequestHandler, HTTPServer
from typing import Any, Dict

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
        if self.path.startswith("/tools/"):
            tool = self.path[len("/tools/") :]
            try:
                result = self.dispatch(tool, {})
            except Exception as exc:  # noqa: BLE001
                self.send_error(500, str(exc))
                return
            self._respond(result)
            return
        self.send_error(404, "not found")

    def dispatch(self, tool: str, args: Dict[str, Any]):
        if tool in ("alerts.status", "status"):
            return self.client.status()
        if tool in ("alerts.run_transcribe", "run_transcribe"):
            return self.client.run_transcribe(**args)
        if tool in ("alerts.run_enrich", "run_enrich"):
            return self.client.run_enrich(**args)
        if tool in ("alerts.run_publish", "run_publish"):
            return self.client.run_publish(**args)
        if tool in ("alerts.reprocess", "reprocess"):
            return self.client.reprocess(args.get("call_id"), args.get("stage"), args.get("force", False))
        if tool in ("alerts.jobs", "jobs"):
            return self.client.jobs()
        if tool in ("alerts.job_status", "job_status"):
            return self.client.job_status(args.get("job_id", ""))
        if tool in ("alerts.tail_job", "tail_job"):
            return {"lines": self.client.tail_job(args.get("job_id", ""), args.get("seconds", 30))}
        if tool in ("alerts.briefing", "briefing"):
            return self.client.briefing(args.get("since_minutes", 360), args.get("format", "bullets"))
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
