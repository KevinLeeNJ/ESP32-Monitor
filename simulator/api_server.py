#!/usr/bin/env python3
"""Devices API simulator using only Python's standard library.

Run: python3 simulator/api_server.py --num 4 --port 18080
ESP32 Base URL: http://<Mac LAN IP>:18080 (no /v1/devices/summary suffix).
Metrics change on every successful summary request. Restart with --num 0..4
on the same port to check device removal/recovery without reconfiguring ESP32.
"""

import argparse
import hmac
import ipaddress
import json
import random
import re
import shutil
import subprocess
import sys
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import urlsplit

# Public, fixed development credential; exactly 64 hex characters like Server.
READ_TOKEN = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
DEVICE_NAMES = ("NAS", "PI", "PC", "ROUTER")
SUMMARY_PATH = "/v1/devices/summary"


def format_uptime(seconds):
    if seconds >= 86400:
        return f"{seconds // 86400}d {seconds // 3600 % 24}h"
    if seconds >= 3600:
        return f"{seconds // 3600}h {seconds // 60 % 60}m"
    return f"{seconds // 60}m {seconds % 60}s" if seconds >= 60 else f"{seconds}s"


class SimulatorServer(ThreadingHTTPServer):
    daemon_threads = True
    allow_reuse_address = True

    def __init__(self, address, num):
        if not 0 <= num <= len(DEVICE_NAMES):
            raise ValueError("num must be between 0 and 4")
        self.num = num
        self.started = time.monotonic()
        super().__init__(address, SimulatorHandler)

    def summary(self):
        elapsed = int(time.monotonic() - self.started)
        devices = []
        for index, name in enumerate(DEVICE_NAMES[:self.num]):
            metrics = []
            for metric_name in ("CPU", "MEM", "DISK"):
                percent = random.randint(0, 100)
                metrics.append({"name": metric_name, "value": f"{percent}%", "percent": percent})
            devices.append({
                "name": name,
                "uptime": format_uptime((index + 1) * 86400 + 4 * 3600 + elapsed),
                "metric_count": 3,
                "metrics": metrics,
            })
        return {"device_count": self.num, "devices": devices}


class SimulatorHandler(BaseHTTPRequestHandler):
    # Close each connection, matching firmware's one-request client lifetime.
    protocol_version = "HTTP/1.0"

    def setup(self):
        super().setup()
        self.connection.settimeout(10)

    def send_json(self, status, body, allow=None, head=False):
        payload = json.dumps(body, separators=(",", ":"), ensure_ascii=True).encode("ascii")
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Cache-Control", "no-store")
        self.send_header("Content-Length", str(len(payload)))
        self.send_header("Connection", "close")
        if allow:
            self.send_header("Allow", allow)
        self.end_headers()
        if not head:
            self.wfile.write(payload)

    def handle_api(self, head=False):
        path = urlsplit(self.path).path
        if path not in (SUMMARY_PATH, "/healthz", "/readyz"):
            self.send_json(404, {"error": "not_found"}, head=head)
            return
        if self.command != "GET":
            self.send_json(405, {"error": "method_not_allowed"}, allow="GET", head=head)
            return
        if path in ("/healthz", "/readyz"):
            self.send_json(200, {"status": "ok" if path == "/healthz" else "ready"})
            return
        headers = self.headers.get_all("Authorization", [])
        parts = headers[0].split() if len(headers) == 1 else []
        if (len(parts) != 2 or parts[0].lower() != "bearer" or
                not hmac.compare_digest(parts[1].encode("utf-8"), READ_TOKEN.encode("ascii"))):
            self.send_json(401, {"error": "unauthorized"})
            return
        snapshot = self.server.summary()
        self.send_json(200, snapshot)
        values = "; ".join(
            f"{d['name']} " + " ".join(f"{m['name']}={m['value']}" for m in d["metrics"])
            for d in snapshot["devices"]
        )
        print(f"  devices={snapshot['device_count']} {values}", flush=True)

    def do_GET(self):
        self.handle_api()

    def do_HEAD(self):
        self.handle_api(head=True)

    def do_POST(self):
        self.handle_api()

    do_PUT = do_POST
    do_DELETE = do_POST
    do_PATCH = do_POST
    do_OPTIONS = do_POST

    def log_message(self, fmt, *args):
        # Log paths/status only. Never echo request headers or tokens.
        print(f"[{self.log_date_time_string()}] {self.client_address[0]} {fmt % args}", flush=True)


def lan_addresses():
    """Best-effort local interface lookup; sends no discovery traffic."""
    command = shutil.which("ifconfig") or ("/sbin/ifconfig" if sys.platform == "darwin" else None)
    args = [command] if command else ([shutil.which("ip"), "-o", "-4", "addr", "show"] if shutil.which("ip") else None)
    if not args:
        return []
    try:
        output = subprocess.run(args, capture_output=True, text=True, timeout=2, check=True).stdout
    except (OSError, subprocess.SubprocessError):
        return []
    addresses = set()
    for value in re.findall(r"\binet\s+(\d+\.\d+\.\d+\.\d+)", output):
        address = ipaddress.ip_address(value)
        if not address.is_loopback and not address.is_link_local and not address.is_unspecified:
            addresses.add(value)
    return sorted(addresses, key=lambda value: (not ipaddress.ip_address(value).is_private, value))


def parse_args(argv=None):
    parser = argparse.ArgumentParser(
        description="ESP32 Devices API simulator (plain HTTP, no extra dependencies)",
        epilog="Restart on the same port with a different --num to test online-device changes. Ctrl-C stops the server.",
    )
    parser.add_argument("--num", type=int, choices=range(5), default=4, help="online devices, 0..4 (default: 4)")
    parser.add_argument("--host", default="0.0.0.0", help="listen address (default: all IPv4 interfaces)")
    parser.add_argument("--port", type=int, default=18080, help="HTTP port (default: 18080)")
    args = parser.parse_args(argv)
    if not 1 <= args.port <= 65535:
        parser.error("--port must be between 1 and 65535")
    return args


def main(argv=None):
    args = parse_args(argv)
    try:
        server = SimulatorServer((args.host, args.port), args.num)
    except OSError as exc:
        print(f"Cannot listen on {args.host}:{args.port}: {exc}", file=sys.stderr)
        return 1
    with server:
        print(f"Devices API simulator: {args.num} online devices", flush=True)
        print(f"Listening: http://{args.host}:{server.server_port}", flush=True)
        candidates = lan_addresses() if args.host == "0.0.0.0" else [args.host]
        for host in candidates:
            print(f"ESP32 API Base URL: http://{host}:{server.server_port}", flush=True)
        if not candidates:
            print(f"ESP32 API Base URL: http://<Mac LAN IP>:{server.server_port}", flush=True)
        print(f"Fixed read Token: {READ_TOKEN}", flush=True)
        print("Use the Base URL without a path. This fixed-token HTTP service is for LAN debugging.", flush=True)
        print("GET /healthz (public); GET /v1/devices/summary (Bearer token). Ctrl-C to stop.", flush=True)
        try:
            server.serve_forever()
        except KeyboardInterrupt:
            print("\nSimulator stopped.", flush=True)
    return 0


if __name__ == "__main__":
    sys.exit(main())
