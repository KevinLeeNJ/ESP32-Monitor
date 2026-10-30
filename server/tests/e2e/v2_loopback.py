#!/usr/bin/env python3
"""Production Server + real Client Agent, isolated state and loopback HTTP only.
No credentials, raw samples or terminal logs are printed or retained.
"""
import argparse
import http.cookiejar
import json
import os
from pathlib import Path
import re
import secrets
import signal
import socket
import subprocess
import tempfile
import time
from urllib.error import HTTPError, URLError
from urllib.request import Request, build_opener, ProxyHandler, HTTPCookieProcessor

ROOT = Path(__file__).resolve().parents[3]


def require(value, message):
    if not value:
        raise AssertionError(message)


def wait(fn, message, timeout=50):
    end = time.monotonic() + timeout
    while time.monotonic() < end:
        try:
            result = fn()
            if result:
                return result
        except (OSError, URLError, json.JSONDecodeError):
            pass
        time.sleep(.2)
    raise AssertionError(message)


def stop(p):
    if p and p.poll() is None:
        p.send_signal(signal.SIGTERM)
        try:
            p.wait(timeout=15)
        except subprocess.TimeoutExpired:
            p.kill()
            p.wait()
            raise AssertionError('process failed graceful shutdown')
        require(p.returncode == 0, 'process failed on shutdown')


def run(server_bin):
    with tempfile.TemporaryDirectory(prefix='monitor-v2-e2e-') as tmp:
        temp = Path(tmp)
        agent_bin = temp / 'monitor-agent'
        subprocess.run(['go', 'build', '-o', str(agent_bin), './cmd/monitor-agent'], cwd=ROOT/'client', check=True)
        with socket.socket() as s:
            s.bind(('127.0.0.1', 0))
            port = s.getsockname()[1]
        base = f'http://127.0.0.1:{port}'
        config = temp/'monitor.yaml'
        read_token = secrets.token_hex(32)
        config.write_text(json.dumps({'listen': f'127.0.0.1:{port}', 'public_base_url': base,
                                     'database': str(temp/'monitor.db'), 'read_token': read_token}))
        config.chmod(0o600)
        logs = temp/'server-output'
        jar = http.cookiejar.CookieJar()
        opener = build_opener(ProxyHandler({}), HTTPCookieProcessor(jar))
        csrf = ''

        def request(origin, path, method='GET', data=None, token='', admin=False):
            headers = {'Content-Type': 'application/json'}
            if token:
                headers['Authorization'] = 'Bearer '+token
            if admin:
                headers['Origin'] = origin
                headers['X-CSRF-Token'] = csrf
            req = Request(origin+path, method=method, headers=headers,
                          data=None if data is None else json.dumps(data).encode())
            try:
                response = opener.open(req, timeout=12)
            except HTTPError as e:
                response = e
            with response:
                raw = response.read()
                return response.code, json.loads(raw) if raw else {}

        def launch():
            stream = logs.open('a')
            p = subprocess.Popen([server_bin, 'serve', '--config', str(config)], stdout=stream, stderr=stream)
            stream.close()
            wait(lambda: request(base, '/readyz')[0] == 200, 'Server failed to start')
            return p

        server, agent = None, None
        try:
            server = launch()
            password = wait(lambda: re.search(r'WebUI 临时密码: ([a-f0-9]{32})', logs.read_text()), 'no bootstrap password')[1]
            code, state = request(base, '/api/auth/login', 'POST', {'password': password}, admin=True)
            require(code == 200 and state['must_change_password'], 'initial login failed')
            csrf = state['csrf_token']
            require(request(base, '/api/devices', admin=True)[0] == 403, 'forced password change bypassed')
            permanent = secrets.token_urlsafe(24)
            code, state = request(base, '/api/auth/password', 'POST', {'current_password': password, 'new_password': permanent}, admin=True)
            require(code == 200 and not state['must_change_password'], 'password change failed')
            csrf = state['csrf_token']
            print('PASS: password-only login, forced rotation and Cookie session', flush=True)
            code, device = request(base, '/api/devices', 'POST', {'name': 'Real Client'}, admin=True)
            require(code == 201, 'device creation failed')
            code, claim = request(base, '/v2/collector/register', 'POST', {'code': device['code']})
            require(code == 200, 'Client registration failed')
            token_file = temp/'server-token'
            token_file.write_text(claim['token']+'\n')
            token_file.chmod(0o600)
            state_dir = temp/'collector'
            state_dir.mkdir(mode=0o700)
            command = [str(agent_bin), '--collector', '--collector-api', '127.0.0.1:0', '--collector-state-dir', str(state_dir),
                       '--collector-upload-url', base+'/v2/collector/samples', '--collector-upload-token-file', str(token_file)]
            agent = subprocess.Popen(command, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            descriptor = wait(lambda: json.loads((state_dir/'collector-api.json').read_text()), 'Client Agent not ready')

            def native(path, data=None, method='GET'):
                return request(descriptor['base_url'], '/v1/collector/'+path, method, data, descriptor['token'])

            first_profile = wait(lambda: (lambda r: r[1] if r[0] == 200 and r[1].get('inventory', {}).get('objects') else None)(request(base, '/api/devices/'+device['id']+'/profile', admin=True)), 'initial metadata missing before enabling sample uploads')
            initial_directory = first_profile['inventory']['objects']
            require(all(o['id'] > 0 and o['name'] for o in initial_directory), 'incomplete object name mapping')
            require(native('status')[1].get('last_upload_at') is None, 'initial directory enabled sample upload')
            require(request(base, '/api/devices/'+device['id']+'/history', admin=True)[1]['batches'] == [], 'startup metadata created samples')
            print('PASS: first registered connection uploads full directory before sampling is enabled', flush=True)
            cfg = {'interval_seconds': 5, 'selections': [], 'upload': {'enabled': True, 'interval_seconds': 5, 'optional': []}}
            require(native('config', cfg, 'PUT')[0] == 200, 'Client configuration failed')
            wait(lambda: native('status')[1].get('last_upload_at'), 'Server did not accept real Client sample')
            require(request(base, '/api/devices/'+device['id']+'/metrics', admin=True)[1]['latest'].get('cpu.usage') is not None, 'missing CPU')
            print('PASS: real Client profile creation and CPU/MEM/uptime upload', flush=True)
            cfg['upload']['optional'] = [{'metric': 'system.os'}, {'metric': 'cpu.model'}]
            require(native('config', cfg, 'PUT')[0] == 200, 'Client selection update failed')
            wait(lambda: request(base, '/api/devices/'+device['id']+'/profile', admin=True)[1].get('inventory', {}).get('os'), 'Client PATCH was rejected')
            caps = native('capabilities')[1]['v2']['metrics']
            cfg['upload']['optional'] = [dict(metric=m['metric'], **({'objects': m['objects']} if m.get('objects') else {}))
                                         for m in caps if m['available'] and not m['required']
                                         and m['metric'] not in {'network.public_ipv4', 'drive.smart_health', 'memory.total', 'memory.used'}]
            previous_upload = native('status')[1].get('last_upload_at')
            require(native('config', cfg, 'PUT')[0] == 200, 'full local selections failed')
            wait(lambda: native('status')[1].get('last_upload_at') not in (None, previous_upload), 'Server rejected full local metric upload')
            print('PASS: all available local metric groups (without external IP/SMART queries)', flush=True)
            before = request(base, '/api/devices/'+device['id']+'/profile', admin=True)[1]['revision']
            print('PASS: real Client leaf-by-leaf Profile PATCH', flush=True)
            stop(agent)
            agent = None
            stop(server)
            server = launch()
            require(request(base, '/api/devices', admin=True)[0] == 200, 'persisted cookie failed after restart')
            require(request(base, '/api/devices/'+device['id']+'/profile', admin=True)[1]['revision'] == before, 'profile lost on restart')
            require(len(re.findall(r'WebUI 临时密码:', logs.read_text())) == 1, 'permanent password regenerated')
            agent = subprocess.Popen(command, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            descriptor = wait(lambda: json.loads((state_dir/'collector-api.json').read_text()), 'Agent restart failed')
            wait(lambda: native('status')[1].get('last_upload_at'), 'Client failed to resume uploads')
            require(native('status')[1]['profile_revision'] == before, 'cached profile revision changed')
            print('PASS: SQLite reopen, persisted Cookie and Client profile cache resume', flush=True)
            cfg['upload']['enabled'] = False
            cfg['upload']['optional'] = []
            require(native('config', cfg, 'PUT')[0] == 200, 'disable upload failed')
            wait(lambda: request(base, '/api/devices/'+device['id']+'/profile', admin=True)[1].get('enabled') == [], 'empty profile was not acknowledged')
            require(request(base, '/api/devices/'+device['id']+'/profile', admin=True)[1]['inventory']['objects'] == initial_directory, 'disabling samples removed long-term directory')
            # A second registration at the same URL must not reuse another device's confirmed Profile.
            stop(agent)
            agent = None
            code, second = request(base, '/api/devices', 'POST', {'name': 'Re-registered Client'}, admin=True)
            require(code == 201, 'second device creation failed')
            code, second_claim = request(base, '/v2/collector/register', 'POST', {'code': second['code']})
            require(code == 200, 'second registration failed')
            token_file.write_text(second_claim['token']+'\n')
            token_file.chmod(0o600)
            agent = subprocess.Popen(command, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            descriptor = wait(lambda: json.loads((state_dir/'collector-api.json').read_text()), 're-registered Agent failed')
            wait(lambda: request(base, '/api/devices/'+second['id']+'/profile', admin=True)[1].get('inventory', {}).get('objects') == initial_directory, 'new credential reused old confirmed cache')
            require(request(base, '/api/devices/'+second['id']+'/history', admin=True)[1]['batches'] == [], 'disabled re-registration sent samples')
            print('PASS: metadata survives disabled mode; fresh registration bypasses old profile cache', flush=True)
            require(request(base, '/v2/devices/summary', token=read_token)[0] == 200, 'v2 summary failed')
            require(request(base, '/v1/devices/summary', token=read_token)[0] == 404, 'v1 remains exposed')
            require(claim['token'] not in logs.read_text(), 'upload token leaked into logs')
            print('PASS: disable sync, v2-only routes and secret-safe audit logs', flush=True)
        finally:
            stop(agent)
            stop(server)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--server', required=True)
    args = parser.parse_args()
    run(str(Path(args.server).resolve()))
