import json
import unittest
from unittest.mock import patch
from threading import Thread
from http.server import ThreadingHTTPServer
from urllib.request import Request, urlopen
from urllib.error import HTTPError
import server_monitor as m


class CollectorTests(unittest.TestCase):
    def test_redaction(self):
        raw = "Authorization: Bearer private-jwt X-Monitor-Token: collector-secret ?key=private-key&ok=1 sk-abcdefghijklmnop"
        result = m.redact(raw)
        for secret in ["private-jwt", "collector-secret", "private-key", "sk-abcdefghijklmnop"]:
            self.assertNotIn(secret, result)
        self.assertIn("&ok=1", result)

    def test_release_validation(self):
        for value in ["{}", "broken", json.dumps({"container": "x;rm", "health_url": "http://127.0.0.1/health"}), json.dumps({"container": "app", "health_url": "http://example.com/health"})]:
            with patch.object(m, "read_text", return_value=value):
                self.assertIsNone(m.active_release())
        expected = {"container": "sub2api-release-blue", "health_url": "http://127.0.0.1:19093/health"}
        with patch.object(m, "read_text", return_value=json.dumps(expected)):
            self.assertEqual(expected, m.active_release())

    def test_logs_follow_active_release(self):
        with patch.object(m, "active_release", return_value={"container": "new-app"}), patch.object(m, "run", return_value={"stdout": "hello", "stderr": "", "ok": True, "code": 0}) as run:
            m.logs_for("sub2api", 10000)
            self.assertEqual(["docker", "logs", "--tail", "200", "new-app"], run.call_args.args[0])

    def test_token_required_and_summary_is_cached(self):
        server = ThreadingHTTPServer(("127.0.0.1", 0), m.Handler)
        thread = Thread(target=server.serve_forever, daemon=True)
        thread.start()
        url = "http://127.0.0.1:%d/api/summary" % server.server_port
        try:
            with patch.object(m, "TOKEN", "x" * 32), patch.object(m, "LATEST_SUMMARY", {"generated_at": "2026-09-12T00:00:00Z"}), patch.object(m, "collect_summary") as collect:
                with self.assertRaises(HTTPError) as exc:
                    urlopen(url)
                self.assertEqual(401, exc.exception.code)
                exc.exception.close()
                with self.assertRaises(HTTPError) as exc:
                    urlopen(Request(url, headers={"X-Monitor-Token": "wrong"}))
                self.assertEqual(401, exc.exception.code)
                exc.exception.close()
                for _ in range(2):
                    with urlopen(Request(url, headers={"X-Monitor-Token": "x" * 32})) as response:
                        self.assertIn("samples", json.load(response))
                collect.assert_not_called()
        finally:
            server.shutdown()
            server.server_close()
            thread.join()


if __name__ == "__main__":
    unittest.main()
