import json
from unittest import TestCase, mock

from mcp_server.client import AlertsClient


class AlertsClientTests(TestCase):
    def setUp(self):
        self.client = AlertsClient("http://example.com")

    @mock.patch("urllib.request.urlopen")
    def test_status_calls_ops(self, urlopen):
        urlopen.return_value.__enter__.return_value.read.return_value = json.dumps({"ok": True}).encode()
        resp = self.client.status()
        self.assertTrue(resp["ok"])
        called_url = urlopen.call_args[0][0].full_url
        self.assertIn("/ops/status", called_url)

    @mock.patch("urllib.request.urlopen")
    def test_run_transcribe_payload(self, urlopen):
        urlopen.return_value.__enter__.return_value.read.return_value = json.dumps({"job_id": "123", "accepted": 2}).encode()
        resp = self.client.run_transcribe(since_minutes=5, limit=10)
        self.assertEqual(resp["job_id"], "123")
        data = json.loads(urlopen.call_args[0][0].data.decode())
        self.assertEqual(data["since_minutes"], 5)

    @mock.patch("urllib.request.urlopen")
    def test_briefing_merges_status_and_calls(self, urlopen):
        status_resp = mock.MagicMock()
        status_resp.read.return_value = json.dumps({"queue": {}}).encode()
        calls_resp = mock.MagicMock()
        calls_resp.read.return_value = json.dumps({"calls": [{"tags": ["fire"], "location": "Main"}]}).encode()
        urlopen.side_effect = [mock.MagicMock(__enter__=lambda s: status_resp, __exit__=mock.Mock()),
                               mock.MagicMock(__enter__=lambda s: calls_resp, __exit__=mock.Mock())]
        briefing = self.client.briefing()
        self.assertIn("summary", briefing)
        self.assertIn("Calls", briefing["summary"])

