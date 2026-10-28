#!/usr/bin/env python3
"""Real Agent Core/Native Collector -> local v2 Server (cmd/sim-server).

Uses isolated binaries, loopback ports and temporary credentials. The Server
validates the v2 profile/sample contract; this script asserts the Agent creates
the profile once, uploads required samples, PATCHes selection changes, reuses
the cached profile across restarts and never contacts a v1 ingest endpoint.
"""
import argparse
import json
import os
from pathlib import Path
import platform
import secrets
import signal
import socket
import subprocess
import tempfile
import threading
import time
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
        except (URLError, OSError, json.JSONDecodeError):
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


def stop(process, sig=signal.SIGTERM):
    if process and process.poll() is None:
        process.send_signal(sig)
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


class Logs:
    def __init__(self, stream):
        self.lines = []
        self.lock = threading.Lock()
        self.thread = threading.Thread(target=self._read, args=(stream,), daemon=True)
        self.thread.start()

    def _read(self, stream):
        for line in stream:
            with self.lock:
                self.lines.append(line.rstrip("\n"))

    def events(self, event, op=None):
        with self.lock:
            lines = list(self.lines)
        result = []
        for line in lines:
            try:
                entry = json.loads(line)
            except json.JSONDecodeError:
                continue
            if entry.get("event") == event and (op is None or entry.get("op") == op):
                result.append(entry)
        return result

    def contains(self, fragment):
        with self.lock:
            return any(fragment in line for line in self.lines)


def run(args, temp):
    print(f"Core Collector (gopsutil), {platform.system()} {platform.machine()}", flush=True)
    agent_bin, server_bin = temp / "monitor-agent", temp / "sim-server"
    subprocess.run(["go", "build", "-o", str(agent_bin), "./cmd/monitor-agent"], cwd=ROOT / "client", check=True)
    subprocess.run(["go", "build", "-o", str(server_bin), "./cmd/sim-server"], cwd=ROOT / "client", check=True)
    with socket.socket() as reserve:
        reserve.bind(("127.0.0.1", 0))
        server_port = reserve.getsockname()[1]
    server_base = f"http://127.0.0.1:{server_port}"
    state = temp / "collector"
    state.mkdir(mode=0o700)
    token_file = temp / "upload-token"
    report = {"collector": "gopsutil", "platform": platform.platform(), "checks": []}

    def passed(message):
        report["checks"].append(message)
        print("PASS: " + message, flush=True)

    server = subprocess.Popen([str(server_bin), "-listen", f"127.0.0.1:{server_port}"],
                              stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
    logs = Logs(server.stdout)
    agent = None
    agent_pid = None
    try:
        wait(lambda: request(server_base, "/api/devices")[0] == 200, "sim-server not ready")
        code, device = request(server_base, "/api/devices", data={"name": "Collector E2E"}, method="POST")
        require(code == 201 and device.get("code"), "device creation failed: " + str(device))
        code, registered = request(server_base, "/v2/collector/register", data={"code": device["code"]}, method="POST")
        require(code == 200 and len(registered.get("token", "")) == 64, "registration failed: " + str(registered))
        token_file.write_text(registered["token"] + "\n")
        token_file.chmod(0o600)
        passed("v2 verification-code registration issues a private bearer token")

        command = [str(agent_bin), "--collector", "--collector-api", "127.0.0.1:0",
                   "--collector-state-dir", str(state),
                   "--collector-upload-url", server_base + "/v2/collector/samples",
                   "--collector-upload-token-file", str(token_file)]
        agent = subprocess.Popen(command, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        descriptor = wait(lambda: json.loads((state / "collector-api.json").read_text()), "Agent API not ready")
        base, native_token = descriptor["base_url"], descriptor["token"]

        def api(path, data=None, method="GET"):
            return request(base, "/v1/collector/" + path, native_token, data, method)

        require(request(base, "/v1/collector/status")[0] == 401, "API accepted missing credential")
        require(request(base, "/v1/collector/status", native_token, origin="https://example.org")[0] == 401,
                "API accepted web origin")
        passed("native bridge API rejects missing credential and browser origins")

        status, caps = api("capabilities")
        require(status == 200 and caps.get("v2"), "v2 capabilities missing: " + str(caps))
        metrics = {metric["metric"]: metric for metric in caps["v2"]["metrics"]}
        for key in ["cpu.usage", "memory.usage", "system.uptime"]:
            require(metrics.get(key, {}).get("required") and metrics[key]["available"], "required metric missing: " + key)
        require(any(metric["metric"] == "system.os" and metric["available"] for metric in caps["v2"]["metrics"]),
                "fixed OS metric unavailable")
        passed("collector capabilities expose the v2 semantic metric contract")

        config = {"interval_seconds": 5, "selections": [],
                  "upload": {"enabled": True, "interval_seconds": 5, "optional": []}}
        code, initial = api("config", config, "PUT")
        require(code == 200 and initial["pid"] > 0 and initial["revision"] == 1, "first apply failed: " + str(initial))
        agent_pid = initial["pid"]
        wait(lambda: api("status")[1].get("last_upload_at"), "initial v2 upload not confirmed")
        current = api("status")[1]
        require(current.get("profile_revision", 0) >= 1, "profile revision missing")
        sample = current.get("v2_sample") or {}
        require(all(sample.get(key) is not None for key in ("c", "m", "u")), "required sample fields missing: " + str(sample))
        require(len(logs.events("profile", "put create")) == 1, "profile was not created exactly once")
        require(logs.events("samples", "accept"), "no accepted v2 sample batch")
        passed("required CPU/memory/uptime sample reaches the v2 Server")

        config["upload"]["optional"] = [{"metric": "system.os"}]
        code, changed = api("config", config, "PUT")
        require(code == 200 and changed["revision"] == 2 and changed["pid"] == agent_pid,
                "selection change failed: " + str(changed))
        committed = changed["config"]
        wait(lambda: logs.events("profile", "patch"), "profile PATCH not observed")
        require(not logs.events("profile", "put create")[1:], "fixed profile re-PUT after selection change")
        wait(lambda: len(logs.events("samples", "accept")) >= 2, "continued samples after PATCH")
        passed("selection changes PATCH only the changed profile fields")

        profile_events = len(logs.events("profile"))
        revisions = api("status")[1]["profile_revision"]
        stop(agent)
        agent = None
        require(not alive(agent_pid), "Agent survived shutdown")
        agent = subprocess.Popen(command, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        descriptor = wait(lambda: json.loads((state / "collector-api.json").read_text()), "restored Agent API not ready")
        base, native_token = descriptor["base_url"], descriptor["token"]
        restored = api("status")[1]
        require(restored["revision"] == 2 and restored["config"] == committed and restored["state"] == "healthy",
                "restart did not restore committed config: " + str(restored))
        agent_pid = restored["pid"]
        wait(lambda: api("status")[1].get("last_upload_at"), "restored Agent did not resume uploads")
        require(len(logs.events("profile")) == profile_events,
                "cached profile was retransmitted after Agent restart")
        require(api("status")[1]["profile_revision"] == revisions, "profile revision changed on restart")
        passed("Agent restart reuses the confirmed v2 profile revision")

        disabled = {"interval_seconds": 10, "selections": [],
                    "upload": {"enabled": False, "interval_seconds": 10, "optional": []}}
        code, final = api("config", disabled, "PUT")
        require(code == 200 and final["pid"] == 0 and final["state"] == "disabled",
                "disabling upload did not stop collection: " + str(final))
        wait(lambda: len(logs.events("profile", "patch")) >= 2, "empty profile PATCH not observed")
        passed("disabling remote upload stops collection after the empty profile is acknowledged")

        devices = request(server_base, "/api/devices")[1]["devices"]
        require(devices and devices[0]["registered"] and devices[0].get("last_seen_at"),
                "registered device was not marked online: " + str(devices))
        require(not logs.contains("/v1/ingest/telegraf"), "v1 ingest endpoint was contacted")
        unexpected = [entry for entry in logs.events("reject")
                      if not (entry.get("status") == 404 and entry.get("error") == "profile_required")]
        require(not unexpected, "Server rejected a v2 request: " + str(unexpected))
        require(not logs.contains(registered["token"]), "bearer token leaked into Server logs")
        passed("no v1 ingest traffic; Server accepted the full v2 exchange")

        stop(agent)
        agent = None
        require(not alive(agent_pid), "Agent survived shutdown")
        require(not (state / "collector-api.json").exists(), "collector API descriptor survived shutdown")
        report["profile_events"] = len(logs.events("profile"))
        report["sample_batches"] = len(logs.events("samples", "accept"))
        if args.evidence:
            evidence = Path(args.evidence)
            evidence.parent.mkdir(parents=True, exist_ok=True)
            evidence.write_text(json.dumps(report, indent=2, ensure_ascii=False) + "\n")
    finally:
        stop(agent)
        stop(server, signal.SIGINT)


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--evidence", help="redacted JSON report path")
    arguments = parser.parse_args()
    with tempfile.TemporaryDirectory(prefix="esp32-collector-v2-e2e-") as directory:
        run(arguments, Path(directory))
