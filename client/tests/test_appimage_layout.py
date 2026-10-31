#!/usr/bin/env python3
"""Verify the AppImage uses host libraries and preserves the launch environment."""

import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile

image = Path(sys.argv[1]).resolve()
with tempfile.TemporaryDirectory(prefix="esp32-appimage-test-") as tmp:
    root = Path(tmp)
    subprocess.run([str(image), "--appimage-extract"], cwd=root, check=True, stdout=subprocess.DEVNULL)
    appdir = root / "squashfs-root"
    for name in ("AppRun", "usr/bin/monitor-client", "usr/bin/monitor-agent", "esp32-monitor.desktop", "esp32-monitor.png"):
        assert (appdir / name).exists(), name
    assert not list(appdir.rglob("*.so*")), "AppImage must use host libraries"
    dependencies = subprocess.check_output(["ldd", str(appdir / "usr/bin/monitor-client")], text=True)
    for name in ("libgtk-4.so.1", "libwebkitgtk-6.0.so.4"):
        assert any(name in line and "not found" not in line and str(appdir) not in line for line in dependencies.splitlines()), name
    assert "not found" not in dependencies, dependencies
    docdir = appdir / "usr/share/doc/esp32-monitor"
    for name in ("README.txt", "LICENSE.txt", "THIRD_PARTY_NOTICES.txt", "LICENSE-go-keyring.txt", "LICENSE-fusion-pixel.txt", "LICENSE-unscii.html", "LICENSE-pixelarticons.txt"):
        assert (docdir / name).is_file(), name

    fixture = root / "fixture"
    (fixture / "usr/bin").mkdir(parents=True)
    shutil.copy(appdir / "AppRun", fixture / "AppRun")
    client = fixture / "usr/bin/monitor-client"
    client.write_text('#!/bin/sh\nprintf "%s\\n%s\\n%s\\n%s\\n" "${GDK_BACKEND-unset}" "${LD_LIBRARY_PATH-unset}" "${ESP32_MONITOR_SKIP_DESKTOP_INTEGRATION-unset}" "$1"\n')
    client.chmod(0o755)
    env = os.environ.copy()
    env.update(DISPLAY=":1", LD_LIBRARY_PATH="/host")
    env.pop("ESP32_MONITOR_SKIP_DESKTOP_INTEGRATION", None)
    for backend in (None, "wayland"):
        if backend is None:
            env.pop("GDK_BACKEND", None)
        else:
            env["GDK_BACKEND"] = backend
        result = subprocess.check_output([str(fixture / "AppRun"), "argument"], text=True, env=env).splitlines()
        assert result == [backend or "unset", "/host", "unset", "argument"], result

print("AppImage host libraries and launch environment: OK")
