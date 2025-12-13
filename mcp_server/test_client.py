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
    def test_reprocess_payload(self, urlopen):
        urlopen.return_value.__enter__.return_value.read.return_value = json.dumps({"id": 1}).encode()
        resp = self.client.reprocess("abc", "INGEST", False)
        self.assertEqual(resp["id"], 1)
        data = json.loads(urlopen.call_args[0][0].data.decode())
        self.assertEqual(data["call_id"], "abc")

    @mock.patch("urllib.request.urlopen")
    def test_briefing_data(self, urlopen):
        urlopen.return_value.__enter__.return_value.read.return_value = json.dumps({"total_calls": 1}).encode()
        data = self.client.briefing_data()
        self.assertEqual(data["total_calls"], 1)
