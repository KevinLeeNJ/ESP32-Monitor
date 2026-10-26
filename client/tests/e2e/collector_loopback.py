#!/usr/bin/env python3
"""Real GUI API -> Agent -> Telegraf -> protected loopback -> Server -> SQLite.

Uses isolated binaries/state, ephemeral loopback ports and temporary credentials.
Requires Telegraf 1.40.2. Never starts/stops the user's existing Telegraf service.
"""
import argparse
import json
import os
from pathlib import Path
import platform
import re
import secrets
import shutil
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

ROOT = Path(__file__).resolve().parents[3]
HTTP = build_opener(ProxyHandler({}))


def require(ok, message):
    if not ok:
        raise AssertionError(message)


def wait(predicate, message, timeout=45):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        try:
            value = predicate()
            if value:
                return value
        except (URLError, OSError, json.JSONDecodeError, sqlite3.OperationalError):
            pass
        time.sleep(0.15)
    raise AssertionError(message)


def request(base, path, token=None, data=None, method="GET", origin=None):
    headers = {"Content-Type": "application/json"}
    if token:
        headers["Authorization"] = "Bearer " + token
    if origin:
        headers["Origin"] = origin
    body = None if data is None else json.dumps(data).encode()
    req = Request(base + path, data=body, headers=headers, method=method)
    try:
        response = HTTP.open(req, timeout=100)
    except HTTPError as error:
        response = error
    with response:
        raw = response.read()
        return response.code, json.loads(raw) if raw else None


def stop(process):
    if process and process.poll() is None:
        process.send_signal(signal.SIGTERM)
        try:
            process.wait(timeout=15)
        except subprocess.TimeoutExpired:
            process.kill()
            process.wait()
            raise AssertionError("managed process did not stop")
        require(process.returncode == 0, "process shutdown failed")


def alive(pid):
    try:
        os.kill(pid, 0)
        return True
    except ProcessLookupError:
        return False


def run(args, temp):
    telegraf = shutil.which(args.telegraf)
    require(telegraf, "Telegraf executable missing")
    version = subprocess.check_output([telegraf, "version"], text=True).strip()
    require(version.split()[1] == "1.40.2", "requires pinned Telegraf 1.40.2")
    print(f"Real {version}, {platform.system()} {platform.machine()}", flush=True)
    agent_bin, server_bin = temp / "monitor-agent", temp / "monitor-server"
    subprocess.run(["go", "build", "-o", str(agent_bin), "./cmd/monitor-agent"], cwd=ROOT / "client", check=True)
    subprocess.run(["go", "build", "-o", str(server_bin), "./cmd/monitor-server"], cwd=ROOT / "server", check=True)
    with socket.socket() as reserve:
        reserve.bind(("127.0.0.1", 0))
        server_port = reserve.getsockname()[1]
    server_base = f"http://127.0.0.1:{server_port}"
    write_token, read_token = secrets.token_hex(32), secrets.token_hex(32)
    database, config = temp / "monitor.db", temp / "server.yaml"
    config.write_text(f"""listen: 127.0.0.1:{server_port}
public_base_url: {server_base}
database: {json.dumps(str(database))}
online_timeout_seconds: 45
metric_ttl_seconds: 45
history_retention_hours: 24
read_token: {read_token}
agents:
  - id: collector-e2e
    write_token: {write_token}
panel:
  devices:
    - id: collector-e2e
      name: LOCAL
      disk_mount_path: /
      uptime_source:
        measurement: system
        tags: {{}}
        field: uptime
""")
    config.chmod(0o600)
    token_file = temp / "upload-token"
    token_file.write_text(write_token)
    token_file.chmod(0o600)
    state = temp / "collector"
    state.mkdir(mode=0o700)
    records, lock = [], threading.Lock()

    class Relay(BaseHTTPRequestHandler):
        def log_message(self, *unused):
            pass

        def do_POST(self):
            body = self.rfile.read(int(self.headers["Content-Length"]))
            data = json.loads(body)
            code, _ = request(server_base, self.path, write_token, data, "POST")
            with lock:
                records.append({"code": code, "batch": data})
            self.send_response(code)
            self.end_headers()

    relay = ThreadingHTTPServer(("127.0.0.1", 0), Relay)
    relay_thread = threading.Thread(target=relay.serve_forever, daemon=True)
    relay_thread.start()
    log = open(temp / "processes.log", "w+")
    server, agent = None, None
    managed_pid = None
    report = {"telegraf": version, "platform": platform.platform(), "checks": []}

    def passed(message):
        report["checks"].append(message)
        print("PASS: " + message, flush=True)

    def audit_generated_config():
        paths = list(state.glob("telegraf-*.conf"))
        require(len(paths) == 1, "expected one active Telegraf configuration")
        # Audit with logging enabled so quiet mode cannot hide a warning.
        text = re.sub(r"(?m)^(\s*)quiet\s*=\s*true\s*$", r"\1quiet = false", paths[0].read_text())
        audit = temp / "audit.conf"
        audit.write_text(text)
        audit.chmod(0o600)
        checked = subprocess.run([telegraf, "config", "check", "--config", str(audit),
                                  "--strict-env-handling"], capture_output=True, text=True)
        require(checked.returncode == 0, "Telegraf config check rejected the generated configuration")
        require(not re.search(r"deprecat|will (?:change|be removed)|new default", checked.stderr, re.I),
                "generated configuration emitted a deprecation/default-change warning")
        audit.unlink()

    try:
        server = subprocess.Popen([str(server_bin), "serve", "--config", str(config)], stdout=log, stderr=log)
        wait(lambda: request(server_base, "/readyz")[0] == 200, "Server not ready")
        command = [str(agent_bin), "--collector", "--collector-api", "127.0.0.1:0",
                   "--collector-state-dir", str(state), "--telegraf-executable", telegraf,
                   "--collector-upload-url", f"http://127.0.0.1:{relay.server_port}/v1/ingest/telegraf",
                   "--collector-upload-token-file", str(token_file)]
        agent = subprocess.Popen(command, stdout=log, stderr=log)
        descriptor = wait(lambda: json.loads((state / "collector-api.json").read_text()), "Agent API not ready")
        base, native_token = descriptor["base_url"], descriptor["token"]

        def api(path, data=None, method="GET"):
            return request(base, "/v1/collector/" + path, native_token, data, method)

        require(request(base, "/v1/collector/status")[0] == 401, "API accepted missing credential")
        require(request(base, "/v1/collector/status", native_token, origin="https://example.org")[0] == 401,
                "API accepted web origin")
        passed("native bridge API rejects missing credential and browser origins")
        status, caps = api("capabilities")
        require(status == 200 and len(caps["categories"]) == 6, "six category capability contract missing")
        metrics = {metric["id"]: metric for category in caps["categories"]
                   for source in category["sources"] for metric in source["metrics"]}
        for identity in ["mem/mem/used_percent", "cpu/cpu/usage_active", "system/system/uptime", "net/net/bytes_recv"]:
            require(identity in metrics and metrics[identity]["objects"], "actual probe field/objects missing: " + identity)
        require(not metrics["net/net/bytes_recv"]["upload_supported"], "unsupported network upload advertised")
        passed("frontend capability API contains real CPU/memory/network/uptime fields and observed objects")
        for _ in range(3):
            scan_code, rescanned = api("rescan", method="POST")
            total_objects = [obj for category in rescanned["categories"] for source in category["sources"]
                             for metric in source["metrics"] if metric["id"] == "cpu/cpu/usage_active"
                             for obj in metric["objects"] if obj.get("cpu") == "cpu-total"]
            require(scan_code == 200 and total_objects, "regular CPU probe omitted cpu-total")
        passed("repeated real capability rescans retain cpu-total despite the short once-mode sampling issue")

        first = {"interval_seconds": 5, "selections": [{"id": "mem/mem/used_percent", "upload": True}]}
        code, initial = api("config", first, "PUT")
        require(code == 200 and initial["pid"] > 0 and initial["revision"] == 1, "first apply failed: " + str(initial))
        managed_pid = initial["pid"]
        audit_generated_config()
        wait(lambda: api("status")[1]["last_upload_at"], "initial real upload not confirmed")

        def rows():
            with sqlite3.connect(database) as db:
                return db.execute("SELECT measurement,field,sampled_at,value,tags_json FROM metric_history").fetchall()

        wait(lambda: any(row[:2] == ("mem", "used_percent") for row in rows()), "memory not persisted")
        require(all(row[:2] == ("mem", "used_percent") for row in rows()), "unselected fields uploaded")
        passed("real Telegraf memory sample reaches Agent loopback, HTTP 204 and Server SQLite")
        bad = {"interval_seconds": 5, "selections": [{"id": "net/net/bytes_recv", "upload": True}]}
        require(api("config", bad, "PUT")[0] == 422 and api("status")[1]["pid"] == managed_pid,
                "unsupported upload changed the running collector")
        passed("invalid upload selection leaves the active process/configuration intact")
        total = next(obj for obj in metrics["cpu/cpu/usage_active"]["objects"] if obj.get("cpu") == "cpu-total")
        new = {"interval_seconds": 5, "selections": [
            {"id": "cpu/cpu/usage_active", "objects": [total], "upload": True},
            {"id": "system/system/uptime", "upload": True},
            {"id": "net/net/bytes_recv", "upload": False},
        ]}
        code, changed = api("config", new, "PUT")
        require(code == 200 and changed["revision"] == 2 and changed["pid"] != managed_pid,
                "new configuration not committed: " + str(changed))
        require(not alive(managed_pid), "previous managed Telegraf still running")
        managed_pid = changed["pid"]
        audit_generated_config()
        passed("generated memory and CPU/network/system configs pass Telegraf check without deprecation/default-change warnings")
        wait(lambda: {r[0] for r in rows()} >= {"mem", "cpu", "system"}, "new metrics not persisted")
        cutoff = len(records)
        # Wait across another gather, then assert old/unselected/local-only fields
        # cannot appear in uploads after the completed configuration switch.
        wait(lambda: len(records) > cutoff, "no continued uploads after switch", timeout=15)
        for record in records[cutoff:]:
            require(record["code"] == 204, "upstream did not accept new metrics")
            for sample in record["batch"]["metrics"]:
                expected = {"cpu": "usage_active", "system": "uptime"}
                require(sample["name"] in expected and set(sample["fields"]) == {expected[sample["name"]]},
                        "old/unselected/local-only fields escaped")
                require("host" not in sample["tags"], "hostname escaped")
                if sample["name"] == "cpu":
                    require(sample["tags"] == total, "unselected CPU core escaped")
        preview = api("status")[1]["preview"]["metrics"]
        require(any(s["name"] == "net" for s in preview), "local-only network preview absent")
        for record in records:
            for sample in record["batch"]["metrics"]:
                for field, value in sample["fields"].items():
                    match = [r for r in rows() if r[0] == sample["name"] and r[1] == field
                             and r[2] == sample["timestamp"] and json.loads(r[4]) == sample["tags"]]
                    require(match and abs(match[0][3] - value) < 1e-6, "HTTP sample differs from SQLite")
        passed("configuration restarts Telegraf; new CPU/uptime fields persist with exact sample values")
        passed("CPU object/field selection is enforced; local-only network remains in preview")
        summary = request(server_base, "/v1/devices/summary", read_token)[1]
        require(summary["device_count"] == 1 and summary["devices"][0]["uptime"] != "--", "uptime projection absent")
        require(summary["devices"][0]["metrics"][0]["value"] != "--", "CPU projection absent")
        passed("Server device summary projects the newly enabled CPU and uptime")

        # Actual runtime failure after successful discovery: persist must fail
        # after a live candidate starts, so the old valid configuration is restored.
        persisted = state / "collector.json"
        saved = persisted.read_bytes()
        persisted.unlink()
        persisted.mkdir()
        try:
            candidate = {"interval_seconds": 6, "selections": new["selections"]}
            failure_code, failure = api("config", candidate, "PUT")
            require(failure_code == 422 and failure.get("error_code") == "config_persist_failed",
                    "expected a post-start persistence failure: " + str(failure))
            rolled = api("status")[1]
            require(rolled["state"] == "healthy" and rolled["revision"] == 2 and rolled["config"] == new,
                    "live rollback failed")
            require(not alive(managed_pid), "failed candidate left the old process running")
            managed_pid = rolled["pid"]
        finally:
            persisted.rmdir()
            persisted.write_bytes(saved)
            persisted.chmod(0o600)
        passed("post-start persistence failure rolls back to the prior live config and revision")
        snapshot_text = json.dumps(api("status")[1]) + json.dumps(caps)
        generated = "".join(path.read_text() for path in state.glob("telegraf-*.conf"))
        require(write_token not in snapshot_text + generated and native_token not in snapshot_text + generated,
                "credential escaped into frontend data or generated Telegraf config")
        require(len(list(state.glob("telegraf-*.conf"))) == 1, "stale temporary configurations remain")
        passed("frontend DTOs and Telegraf TOML exclude Server/native bridge credentials")
        stop(agent)
        agent = None
        require(not alive(managed_pid), "Telegraf survived Agent exit")
        require(not list(state.glob("telegraf-*.conf")), "temporary TOML survived clean exit")
        agent = subprocess.Popen(command, stdout=log, stderr=log)
        descriptor = wait(lambda: json.loads((state / "collector-api.json").read_text()), "restored Agent API not ready")
        base, native_token = descriptor["base_url"], descriptor["token"]
        restored = api("status")[1]
        require(restored["revision"] == 2 and restored["config"] == new and restored["state"] == "healthy",
                "restart did not restore committed config")
        managed_pid = restored["pid"]
        passed("Agent shutdown reaps Telegraf; restart restores the committed structured configuration")
        disabled = {"interval_seconds": 10, "selections": []}
        code, final = api("config", disabled, "PUT")
        require(code == 200 and final["pid"] == 0 and final["state"] == "disabled" and not alive(managed_pid),
                "disable did not stop Telegraf")
        passed("disabling all metrics stops the managed collector")
        report["capability_metric_count"] = len(metrics)
        report["sqlite_field_count"] = len(rows())
        report["http_upload_count"] = len(records)
        report["categories"] = [{"id": category["id"], "sources": [
            {"id": source["id"], "state": source["state"], "reason": source.get("reason"),
             "metric_count": len(source["metrics"])} for source in category["sources"]]} for category in caps["categories"]]
        if args.evidence:
            evidence = Path(args.evidence)
            evidence.parent.mkdir(parents=True, exist_ok=True)
            evidence.write_text(json.dumps(report, indent=2, ensure_ascii=False) + "\n")
    finally:
        stop(agent)
        stop(server)
        relay.shutdown()
        relay.server_close()
        log.close()


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--telegraf", default="telegraf")
    parser.add_argument("--evidence", help="redacted JSON report path")
    arguments = parser.parse_args()
    with tempfile.TemporaryDirectory(prefix="esp32-collector-e2e-") as directory:
        run(arguments, Path(directory))
