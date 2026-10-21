#!/usr/bin/env python3
"""Opt-in real Telegraf/Server loopback acceptance test (Python stdlib, Unix).

The recording relay forwards actual HTTP requests to the production binary.
Only its first two responses are injected (503, 429) to exercise Agent retries.
All credentials, databases and raw logs live in an automatically removed tempdir.
"""

import argparse
import copy
import http.client
import json
import math
import os
from pathlib import Path
import platform
import pty
import re
import select
import signal
import socket
import sqlite3
import subprocess
import tempfile
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.error import HTTPError, URLError
from urllib.request import Request, build_opener, ProxyHandler


def require(condition, message):
    if not condition:
        raise AssertionError(message)


def wait_for(predicate, timeout, message):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        if predicate():
            return
        time.sleep(0.25)
    raise AssertionError(message)


def stop(process):
    if process is not None and process.poll() is None:
        process.send_signal(signal.SIGTERM)
        try:
            process.wait(timeout=12)
        except subprocess.TimeoutExpired:
            process.kill()
            process.wait()
            raise AssertionError("process failed graceful shutdown")
        require(process.returncode == 0, "process exited unsuccessfully")


def configure(binary, path, base, database):
    # Exercise the real interactive wizard; never print its credential output.
    master, slave = pty.openpty()
    process = subprocess.Popen(
        [binary, "configure", "--config", str(path)],
        stdin=slave, stdout=slave, stderr=slave,
    )
    os.close(slave)
    output = bytearray()
    try:
        answers = [base.removeprefix("http://"), str(database), base,
                   "y", "local-mac", "MAC", "y", "/", "1", "n", "s", "y"]
        os.write(master, ("\n".join(answers) + "\n").encode())
        deadline = time.monotonic() + 15
        while time.monotonic() < deadline:
            if select.select([master], [], [], 0.1)[0]:
                try:
                    data = os.read(master, 65536)
                except OSError:
                    break
                if not data:
                    break
                output.extend(data)
            if process.poll() is not None:
                break
        process.wait(timeout=2)
        require(process.returncode == 0 and path.exists(), "configuration wizard failed")
    finally:
        os.close(master)
        if process.poll() is None:
            process.kill()
            process.wait()
    text = path.read_text()
    read = re.search(r"read_token: ([0-9a-f]{64})", text).group(1)
    write = re.search(r"write_token: ([0-9a-f]{64})", text).group(1)
    require(read != write and path.stat().st_mode & 0o777 == 0o600,
            "wizard credentials or file permissions invalid")
    return read, write


def run(args, temporary):
    binary = str(Path(args.server).resolve())
    evidence = Path(args.evidence).resolve()
    evidence.mkdir(parents=True, exist_ok=True)
    version = subprocess.check_output([args.telegraf, "version"], text=True).strip()
    print(f"Testing {version} on {platform.system()} {platform.machine()}", flush=True)
    with socket.socket() as reservation:
        reservation.bind(("127.0.0.1", 0))
        port = reservation.getsockname()[1]
    base = f"http://127.0.0.1:{port}"
    config = temporary / "monitor.yaml"
    database = temporary / "monitor.db"
    read_token, write_token = configure(binary, config, base, database)
    subprocess.run([binary, "check-config", "--config", str(config)],
                   check=True, stdout=subprocess.DEVNULL)
    opener = build_opener(ProxyHandler({}))

    def request(path, token=None, body=None):
        headers = {"Content-Type": "application/json"}
        if token:
            headers["Authorization"] = "Bearer " + token
        req = Request(base + path, data=body, headers=headers)
        try:
            with opener.open(req, timeout=5) as response:
                return response.status, response.read(), response.headers
        except HTTPError as error:
            return error.code, error.read(), error.headers

    server_log = open(temporary / "server.log", "w+")
    agent_log = open(temporary / "telegraf.log", "w+")
    server = agent = relay = None
    records = []
    lock = threading.Lock()

    def start_server():
        process = subprocess.Popen([binary, "serve", "--config", str(config)],
                                   stdout=server_log, stderr=server_log)
        def ready():
            require(process.poll() is None, "server stopped during startup")
            try:
                return request("/readyz")[0] == 200
            except URLError:
                return False
        wait_for(ready, 15, "server not ready")
        return process

    class Relay(BaseHTTPRequestHandler):
        def log_message(self, *_):
            pass

        def do_POST(self):
            body = self.rfile.read(int(self.headers["Content-Length"]))
            with lock:
                index = len(records)
            code = [503, 429][index] if index < 2 else None
            if code is None:
                connection = http.client.HTTPConnection("127.0.0.1", port, timeout=6)
                try:
                    connection.request("POST", self.path, body, {
                        "Authorization": self.headers.get("Authorization", ""),
                        "Content-Type": self.headers.get("Content-Type", ""),
                        "Content-Encoding": self.headers.get("Content-Encoding", "identity"),
                    })
                    response = connection.getresponse()
                    code = response.status
                    response.read()
                finally:
                    connection.close()
            with lock:
                records.append({"status": code, "batch": json.loads(body),
                                "body_bytes": len(body), "at": time.time()})
            self.send_response(code)
            self.send_header("Content-Length", "0")
            self.end_headers()

    try:
        server = start_server()
        require(json.loads(request("/v1/devices/summary", read_token)[1]) ==
                {"device_count": 0, "devices": []}, "unreported device visible")
        relay = ThreadingHTTPServer(("127.0.0.1", 0), Relay)
        thread = threading.Thread(target=relay.serve_forever, daemon=True)
        thread.start()
        toml = f'''[agent]
  interval = "10s"
  round_interval = true
  flush_interval = "10s"
  collection_jitter = "0s"
  flush_jitter = "1s"
  metric_batch_size = 100
  metric_buffer_limit = 1000
  hostname = "e2e-mac"

[[inputs.cpu]]
  percpu = false
  totalcpu = true
  collect_cpu_time = false
  report_active = true
[[inputs.mem]]
[[inputs.disk]]
  mount_points = ["/"]
[[inputs.system]]
  include = ["uptime"]
[[outputs.http]]
  url = "http://127.0.0.1:{relay.server_port}/v1/ingest/telegraf"
  method = "POST"
  timeout = "5s"
  data_format = "json"
  json_timestamp_units = "1s"
  use_batch_format = true
  content_encoding = "identity"
  non_retryable_statuscodes = [400, 401, 403, 404, 413, 415, 422]
  [outputs.http.headers]
    Content-Type = "application/json"
    Authorization = "Bearer {write_token}"
'''
        agent_config = temporary / "telegraf.conf"
        agent_config.write_text(toml)
        agent_config.chmod(0o600)
        agent = subprocess.Popen([args.telegraf, "--config", str(agent_config)],
                                 stdout=agent_log, stderr=agent_log)

        def uploaded():
            require(agent.poll() is None, "Telegraf exited before uploading")
            with lock:
                return sum(r["status"] == 204 for r in records) >= 2
        print("Collecting real metrics; first responses inject 503 then 429...", flush=True)
        wait_for(uploaded, 95, "no two successful uploads after retries")
        print("503/429 recovered; checking production Server restart...", flush=True)
        before_restart = json.loads(request("/v1/devices/summary", read_token)[1])
        require(before_restart["device_count"] == 1, "device absent before restart")
        stop(server)
        server = start_server()
        require(json.loads(request("/v1/devices/summary", read_token)[1])["device_count"] == 1,
                "restart lost persisted device")
        with lock:
            success_count = sum(r["status"] == 204 for r in records)

        def resumed():
            with lock:
                return sum(r["status"] == 204 for r in records) > success_count
        wait_for(resumed, 30, "uploads did not resume after server restart")
        stop(agent)
        agent = None
        with lock:
            observed = copy.deepcopy(records)
        require([r["status"] for r in observed[:2]] == [503, 429], "fault injection missing")
        require(all(r["status"] == 204 for r in observed[2:]), "unexpected server rejection")
        accepted = [r for r in observed if r["status"] == 204]
        captured = accepted[0]["batch"]
        names = {m["name"] for r in accepted for m in r["batch"]["metrics"]}
        require(names == {"cpu", "mem", "disk", "system"}, "unexpected real measurements")
        require(request("/v1/devices/summary")[0] == 401, "anonymous read permitted")
        require(request("/v1/devices/summary", write_token)[0] == 403, "write token read permitted")
        payload = json.dumps(captured).encode()
        require(request("/v1/ingest/telegraf", "0" * 64, payload)[0] == 401, "bad token accepted")
        require(request("/v1/ingest/telegraf", read_token, payload)[0] == 403, "read token upload permitted")

        with sqlite3.connect(f"file:{database}?mode=ro", uri=True) as db:
            require(db.execute("pragma journal_mode").fetchone()[0] == "wal", "WAL disabled")
            latest = db.execute("select measurement,tags_json,field,sampled_at,value from metric_latest").fetchall()
            history = db.execute("select measurement,tags_json,field,sampled_at,value from metric_history").fetchall()
            last_seen = db.execute("select last_seen_at from devices where device_id='local-mac'").fetchone()[0]
        stored = {(m, t, f, s): v for m, t, f, s, v in history}
        checked = 0
        # Also check the failed batches: Telegraf must resend their original samples.
        for record in observed:
            for metric in record["batch"]["metrics"]:
                tags = {k: v for k, v in metric["tags"].items() if k != "host"}
                tags_json = json.dumps(tags, sort_keys=True, separators=(",", ":"))
                for field, value in metric["fields"].items():
                    if not isinstance(value, (int, float)) or isinstance(value, bool):
                        continue
                    key = (metric["name"], tags_json, field, metric["timestamp"])
                    require(key in stored and stored[key] == value, "captured field missing or changed in SQLite")
                    checked += 1
        require(all("host" not in json.loads(row[1]) for row in latest), "host tag persisted")
        status, body, headers = request("/v1/devices/summary", read_token)
        summary = json.loads(body)
        require(status == 200 and len(body) <= 4096 and headers["Cache-Control"] == "no-store",
                "summary HTTP contract mismatch")
        device = summary["devices"][0]
        require(summary["device_count"] == 1 and device["metric_count"] == 3, "card count mismatch")
        for label, measurement, field, tags in [
            ("CPU", "cpu", "usage_active", {"cpu": "cpu-total"}),
            ("MEM", "mem", "used_percent", {}),
            ("DISK", "disk", "used_percent", {"path": "/"}),
        ]:
            candidates = [row for row in latest if row[0] == measurement and row[2] == field
                          and all(json.loads(row[1]).get(k) == v for k, v in tags.items())]
            require(len(candidates) == 1, "real metric source absent or ambiguous")
            raw = candidates[0][4]
            rounded = math.floor(raw + 0.5)
            item = device["metrics"][["CPU", "MEM", "DISK"].index(label)]
            require(item == {"name": label, "value": f"{rounded}%", "percent": max(0, min(100, rounded))},
                    "summary value differs from actual uploaded value")
        uptime = [row[4] for row in latest if row[0] == "system" and row[2] == "uptime"]
        require(len(uptime) == 1 and uptime[0] >= 0 and uptime[0] == int(uptime[0]), "invalid actual uptime")
        seconds = int(uptime[0])
        expected_uptime = (f"{seconds // 86400}d {seconds // 3600 % 24}h" if seconds >= 86400 else
                           f"{seconds // 3600}h {seconds // 60 % 60}m" if seconds >= 3600 else
                           f"{seconds // 60}m {seconds % 60}s" if seconds >= 60 else f"{seconds}s")
        require(device["uptime"] == expected_uptime, "uptime formatting mismatch")
        print("Actual payload → SQLite → CPU/MEM/DISK and uptime match; waiting real 45s expiry...", flush=True)
        wait_for(lambda: time.time() > last_seen + 46, 48, "offline wait timed out")
        offline = json.loads(request("/v1/devices/summary", read_token)[1])
        require(offline == {"device_count": 0, "devices": []}, "offline device not removed")
        # New Agent process, same configuration: verify card reappears using fresh metrics.
        with lock:
            success_count = sum(r["status"] == 204 for r in records)
        agent = subprocess.Popen([args.telegraf, "--config", str(agent_config)],
                                 stdout=agent_log, stderr=agent_log)
        wait_for(resumed, 30, "restarted Agent did not upload")
        require(json.loads(request("/v1/devices/summary", read_token)[1])["device_count"] == 1,
                "restarted Agent card did not reappear")
        stop(agent)
        agent = None
        stop(server)
        server = None
        for handle in [server_log, agent_log]:
            handle.flush()
            handle.seek(0)
            logs = handle.read()
            require(read_token not in logs and write_token not in logs, "credential leaked in process logs")

        sample = copy.deepcopy(captured)
        for metric in sample["metrics"]:
            if "device" in metric["tags"]:
                metric["tags"]["device"] = "redacted-device"
        outputs = {
            "macos-real-batch.json": sample,
            "summary.json": summary,
            "offline-summary.json": offline,
            "result.json": {
                "telegraf": version, "platform": platform.system(), "architecture": platform.machine(),
                "completed_at_utc": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
                "requests_before_offline": [{"status": r["status"], "body_bytes": r["body_bytes"],
                                             "metrics": len(r["batch"]["metrics"])} for r in observed],
                "measurements": sorted(names), "history_rows": len(history), "latest_rows": len(latest),
                "numeric_field_checks_including_retries": checked, "summary_bytes": len(body),
                "online_timeout_seconds": 45, "metric_ttl_seconds": 45,
                "checks": ["wizard_generated_distinct_tokens_0600", "initial_empty", "real_http_json_ingest",
                           "503_and_429_buffered_retry", "all_captured_numeric_fields_persisted_unchanged",
                           "host_tag_removed", "wal", "cpu_mem_disk_values_and_uptime_match_sqlite",
                           "summary_size_and_cache_headers", "401_403_token_isolation",
                           "server_process_restart_persistence", "real_clock_offline_removal",
                           "agent_restart_reappearance", "sigterm_exit_zero", "no_tokens_in_process_logs"],
                "redaction": "fixed test hostname e2e-mac; disk device tag replaced; no credentials or raw logs",
            },
        }
        for name, value in outputs.items():
            (evidence / name).write_text(json.dumps(value, indent=2, sort_keys=True) + "\n")
        (evidence / "telegraf.redacted.conf").write_text(toml.replace(write_token, "<UPLOAD_TOKEN>"))
        print(f"PASS: {len(outputs['result.json']['checks'])} checks; evidence in {evidence}", flush=True)
    finally:
        try:
            stop(agent)
        finally:
            try:
                stop(server)
            finally:
                if relay is not None:
                    relay.shutdown()
                    relay.server_close()
                server_log.close()
                agent_log.close()


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--server", required=True, help="built monitor-server binary")
    parser.add_argument("--telegraf", default="telegraf")
    parser.add_argument("--evidence", required=True, help="sanitized evidence output directory")
    arguments = parser.parse_args()
    with tempfile.TemporaryDirectory(prefix="esp32-monitor-telegraf-") as directory:
        run(arguments, Path(directory))
