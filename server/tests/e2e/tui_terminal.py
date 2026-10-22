#!/usr/bin/env python3
"""Opt-in local PTY tests for the real Charm UI (macOS/Linux, no network).

Install tests/e2e/requirements-tui.txt in a venv, then run after building:
python3 tests/e2e/tui_terminal.py --server bin/monitor-server
All configurations and random tokens stay in an automatically removed temp dir.
No terminal transcript or credentials are printed or retained.
"""
import argparse
import errno
import fcntl
import os
from pathlib import Path
import pty
import re
import select
import signal
import struct
import subprocess
import tempfile
import termios
import time

import pyte

DOWN = b"\x1b[B"
ENTER = b"\r"
ESC = b"\x1b"
REPLACE = b"\x01\x0b"  # beginning of field, kill remainder


class Terminal:
    def __init__(self, server, config, rows=32, cols=100):
        self.pid, self.fd = pty.fork()
        if self.pid == 0:
            os.environ["TERM"] = "xterm-256color"
            os.environ.pop("NO_COLOR", None)
            os.execv(server, [server, "configure", "--config", str(config)])
        self.screen = pyte.Screen(cols, rows)
        self.stream = pyte.ByteStream(self.screen)
        self.full = b""
        self.status = None
        try:
            self.resize(rows, cols)
            self.expect("编辑基础配置")
        except BaseException:
            self.close()
            raise

    def resize(self, rows, cols):
        self.screen.resize(lines=rows, columns=cols)
        fcntl.ioctl(self.fd, termios.TIOCSWINSZ, struct.pack("HHHH", rows, cols, 0, 0))
        os.kill(self.pid, signal.SIGWINCH)

    def pump(self, wait=0.08):
        if select.select([self.fd], [], [], wait)[0]:
            try:
                data = os.read(self.fd, 65536)
            except OSError as exc:
                if exc.errno == errno.EIO:
                    return False
                raise
            self.stream.feed(data)
            self.full += data
            # Answer cursor position probes if the terminal engine requests one.
            if b"\x1b[6n" in data:
                os.write(self.fd, b"\x1b[1;1R")
            return bool(data)
        return True

    def send(self, keys):
        self.pump()
        os.write(self.fd, keys.encode() if isinstance(keys, str) else keys)

    def expect(self, text, timeout=8, focused=False):
        deadline = time.monotonic() + timeout
        while True:
            lines = self.screen.display
            if any(text in line and (not focused or "┃" in line) for line in lines):
                return
            if time.monotonic() >= deadline or not self.pump():
                # Never dump the terminal: it may contain deliberately revealed tokens.
                raise AssertionError(f"terminal did not reach: {text}")

    def menu(self, index):
        self.send(DOWN * index + ENTER)

    def input(self, text, next_title):
        self.send(REPLACE + text.encode() + ENTER)
        fields = {"监听地址", "SQLite 路径", "对外 Base URL", "卡片名称", "磁盘挂载点", "展示顺序"}
        self.expect(next_title, focused=next_title in fields)

    def yes(self, next_title):
        self.send("y")
        self.expect(next_title)

    def finish(self):
        deadline = time.monotonic() + 8
        while time.monotonic() < deadline:
            self.pump()
            pid, status = os.waitpid(self.pid, os.WNOHANG)
            if pid:
                self.status = status
                assert os.waitstatus_to_exitcode(status) == 0, "configure did not exit successfully"
                assert b"\x1b[?25h" in self.full, "cursor was not restored"
                assert b"\x1b[?2004l" in self.full, "bracketed-paste mode was not restored"
                return
        raise AssertionError("configure did not exit")

    def close(self):
        if self.status is None:
            try:
                os.kill(self.pid, signal.SIGKILL)
                os.waitpid(self.pid, 0)
            except ProcessLookupError:
                pass
        os.close(self.fd)


def run(server):
    with tempfile.TemporaryDirectory(prefix="monitor-charm-pty-") as root:
        path = Path(root) / "monitor.yaml"
        t = Terminal(server, path)
        try:
            # Actual arrow navigation and inline validation, then Tab / Shift+Tab.
            t.menu(0)
            t.expect("监听地址")
            t.input("bad", "listen must be host:port")
            assert not path.exists()
            t.input("127.0.0.1:18080", "SQLite 路径")
            t.send(b"\x1b[Z")
            t.expect("监听地址")
            t.send(b"\t")
            t.expect("SQLite 路径")
            t.input(str(Path(root) / "monitor.db"), "对外 Base URL")
            t.input("https://monitor.example.com:1443", "修改暂存于内存")
            assert not path.exists(), "editing wrote before Save"
            t.menu(1)
            t.expect("客户端 ID")
            t.input("bad id", "客户端 ID 格式无效")
            t.input("nas", "每台设备固定显示")
            t.send(ENTER)
            t.expect("卡片名称")
            t.input("NAS", "磁盘挂载点")
            t.input("/vol1", "展示顺序")
            t.input("1", "修改暂存于内存")
            t.menu(8)
            t.expect("确认保存配置")
            # Enter must choose the default safe answer (return).
            t.send(ENTER)
            t.expect("修改暂存于内存")
            assert not path.exists()
            t.menu(8)
            t.expect("确认保存配置")
            t.yes("配置已保存")
            t.finish()
            assert path.stat().st_mode & 0o777 == 0o600
            assert "disk_mount_path: /vol1" in path.read_text()
            checked = subprocess.run([server, "check-config", "--config", str(path)], capture_output=True)
            assert checked.returncode == 0
            print("PASS: create, arrow/Tab/Shift+Tab navigation, validation recovery, safe save, permissions")
        finally:
            t.close()
        before = path.read_bytes()
        # Persisted format need not expose the tokens in any menu.
        tokens = re.findall(rb"(?:read_token|write_token):\s*([a-f0-9]{64})", before)
        assert len(tokens) == 2
        t = Terminal(server, path, rows=24, cols=80)
        try:
            assert all(token not in t.full for token in tokens)
            t.menu(4)
            t.expect("选择客户端")
            assert all(token not in t.full for token in tokens)
            t.send(DOWN + ENTER)
            t.expect(tokens[1].decode())
            t.send(ENTER)
            t.expect("修改暂存于内存")
            t.menu(6)
            t.expect(tokens[0].decode())
            t.send(ENTER)
            t.expect("修改暂存于内存")
            assert all(token.decode() not in "\n".join(t.screen.display) for token in tokens)
            t.menu(2)
            t.expect("选择客户端")
            t.send(DOWN + ENTER)
            t.expect("每台设备固定显示")
            t.send(ENTER)
            t.expect("卡片名称")
            t.input("HOME", "磁盘挂载点")
            t.input("/data", "展示顺序")
            t.input("1", "修改暂存于内存")
            t.menu(5)
            t.expect("选择客户端")
            t.send(DOWN + ENTER)
            t.expect("旧 Token 失效")
            t.yes("修改暂存于内存")
            # Check resize and Escape back from another form.
            t.resize(20, 60)
            t.menu(0)
            t.expect("监听地址")
            t.send(ESC)
            t.expect("当前操作未应用")
            # Ctrl+C on main menu opens unsaved-change confirmation.
            t.send(b"\x03")
            t.expect("放弃所有未保存修改")
            t.send("n")
            t.expect("修改暂存于内存")
            t.send(ESC)
            t.expect("放弃所有未保存修改")
            t.yes("已取消")
            t.finish()
            assert before == path.read_bytes(), "cancelled edit or rotation changed the file"
            print("PASS: 80x24/60x20 terminal, explicit token reveal, edit, rotation, resize, Escape/Ctrl+C cancellation")
        finally:
            t.close()
        t = Terminal(server, path)
        try:
            t.menu(3)
            t.expect("选择客户端")
            t.send(DOWN + ENTER)
            t.expect("删除客户端 nas")
            t.send(ENTER)
            t.expect("修改暂存于内存")
            t.menu(3)
            t.expect("选择客户端")
            t.send(DOWN + ENTER)
            t.expect("删除客户端 nas")
            t.yes("修改暂存于内存")
            t.menu(8)
            t.expect("确认保存配置")
            t.yes("配置已保存")
            t.finish()
            assert "id: nas" not in path.read_text(), "deleted device retained"
            assert b"read_token: " + tokens[0] in path.read_bytes(), "deletion rotated panel token"
            checked = subprocess.run([server, "check-config", "--config", str(path)], capture_output=True)
            assert checked.returncode == 0
            print("PASS: deletion default rejection, explicit confirmation, save and config reload")
        finally:
            t.close()
    print("PASS: local terminal tests complete; temporary configs and tokens removed")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--server", required=True)
    args = parser.parse_args()
    run(str(Path(args.server).resolve()))
