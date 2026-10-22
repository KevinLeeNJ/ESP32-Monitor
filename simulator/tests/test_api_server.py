"""Run with: python3 -m unittest discover -s simulator/tests -p test_api_server.py"""
import contextlib
import http.client
import importlib.util
import io
import json
from pathlib import Path
import threading
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("api_server", Path(__file__).resolve().parents[1] / "api_server.py")
api = importlib.util.module_from_spec(spec)
spec.loader.exec_module(api)


class ApiServerTests(unittest.TestCase):
    def setUp(self):
        self.server = api.SimulatorServer(("127.0.0.1", 0), 4)
        self.thread = threading.Thread(target=self.server.serve_forever, kwargs={"poll_interval": 0.01}, daemon=True)
        self.thread.start()

    def tearDown(self):
        self.server.shutdown()
        self.server.server_close()
        self.thread.join()

    def request(self, path, token=None, method="GET"):
        client = http.client.HTTPConnection("127.0.0.1", self.server.server_port, timeout=2)
        try:
            headers = {"Authorization": "Bearer " + token} if token is not None else {}
            client.request(method, path, headers=headers)
            response = client.getresponse()
            body = response.read()
            self.assertEqual(response.getheader("Content-Type"), "application/json")
            self.assertEqual(response.getheader("Cache-Control"), "no-store")
            self.assertEqual(int(response.getheader("Content-Length")), len(body))
            return response.status, json.loads(body), response.getheader("Allow")
        finally:
            client.close()

    def test_counts_and_firmware_contract(self):
        for num in range(5):
            self.server.num = num
            status, body, _ = self.request(api.SUMMARY_PATH, api.READ_TOKEN)
            self.assertEqual(status, 200)
            self.assertEqual(body["device_count"], num)
            self.assertEqual(len(body["devices"]), num)
            self.assertLessEqual(len(json.dumps(body).encode()), 4096)
            for device in body["devices"]:
                self.assertLessEqual(len(device["name"]), 8)
                self.assertLessEqual(len(device["uptime"]), 8)
                self.assertEqual(device["metric_count"], 3)
                self.assertEqual([m["name"] for m in device["metrics"]], ["CPU", "MEM", "DISK"])
                for metric in device["metrics"]:
                    self.assertIs(type(metric["percent"]), int)
                    self.assertTrue(0 <= metric["percent"] <= 100)
                    self.assertEqual(metric["value"], f"{metric['percent']}%")

    def test_authentication_and_routes(self):
        self.assertEqual(self.request(api.SUMMARY_PATH)[0], 401)
        self.assertEqual(self.request(api.SUMMARY_PATH, "f" * 64)[0], 401)
        self.assertEqual(self.request("/healthz")[:2], (200, {"status": "ok"}))
        self.assertEqual(self.request("/readyz")[:2], (200, {"status": "ready"}))
        self.assertEqual(self.request("/missing")[0], 404)
        self.assertEqual(self.request(api.SUMMARY_PATH, api.READ_TOKEN, "POST")[::2], (405, "GET"))

    def test_regenerates_metrics_and_bounds(self):
        self.server.num = 1
        with patch.object(api.random, "randint", side_effect=[0, 100, 42, 1, 99, 43]):
            first = self.request(api.SUMMARY_PATH, api.READ_TOKEN)[1]
            second = self.request(api.SUMMARY_PATH, api.READ_TOKEN)[1]
        self.assertNotEqual(first, second)
        self.assertEqual(first["devices"][0]["metrics"][0]["percent"], 0)
        self.assertEqual(first["devices"][0]["metrics"][1]["percent"], 100)

    def test_cli_validation(self):
        self.assertEqual(api.parse_args(["--num", "0", "--port", "8443"]).num, 0)
        for argv in (["--num", "5"], ["--num", "-1"], ["--port", "0"], ["--port", "65536"]):
            with contextlib.redirect_stderr(io.StringIO()), self.assertRaises(SystemExit):
                api.parse_args(argv)
        self.assertEqual(len(api.READ_TOKEN), 64)
        int(api.READ_TOKEN, 16)


if __name__ == "__main__":
    unittest.main()
