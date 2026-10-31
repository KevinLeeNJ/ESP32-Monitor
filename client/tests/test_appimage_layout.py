#!/usr/bin/env python3
"""Verify the built AppImage's WebKit sandbox paths and launcher."""

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
    libdir = appdir / "usr/lib"
    webkit = (libdir / "libwebkitgtk-6.0.so.4").read_bytes()
    assert b"././webkitgtk-6.0/\0" in webkit
    for name in ("WebKitWebProcess", "WebKitNetworkProcess", "injected-bundle/libwebkitgtkinjectedbundle.so"):
        assert (libdir / "webkitgtk-6.0" / name).is_file(), name

    docdir = appdir / "usr/share/doc/esp32-monitor"
    for name in (
        "LICENSE.txt",
        "THIRD_PARTY_NOTICES.txt",
        "LICENSE-go-keyring.txt",
        "LICENSE-fusion-pixel.txt",
        "LICENSE-unscii.html",
        "LICENSE-pixelarticons.txt",
        "LICENSE-LGPL-2.1.txt",
    ):
        assert (docdir / name).is_file(), name
    assert "MIT License" in (docdir / "LICENSE.txt").read_text()

    # Exercise AppRun without launching the GUI or touching the user's settings.
    fixture = root / "fixture"
    (fixture / "apprun-hooks").mkdir(parents=True)
    (fixture / "usr/bin").mkdir(parents=True)
    (fixture / "usr/lib").mkdir(parents=True)
    (fixture / "test-bin").mkdir()
    (fixture / "usr/share/esp32-monitor").mkdir(parents=True)
    shutil.copy(appdir / "AppRun", fixture / "AppRun")
    (fixture / "apprun-hooks/linuxdeploy-plugin-gtk.sh").write_text(":\n")
    (fixture / "usr/share/esp32-monitor/webkit-helper-dir").write_text("/usr/lib/x86_64-linux-gnu/webkitgtk-6.0\n")
    client = fixture / "usr/bin/monitor-client"
    client.write_text('#!/bin/sh\nprintf "%s\\n%s\\n" "$PWD" "$WEBKIT_INJECTED_BUNDLE_PATH"\n')
    client.chmod(0o755)
    ldd = fixture / "test-bin/ldd"
    ldd.write_text('''#!/bin/sh
case "$TEST_LDD:$1" in
  missing:*WebKitWebProcess) echo 'libexample.so.1 => not found' ;;
  version:*WebKitNetworkProcess) echo "libc.so.6: version 'GLIBC_9.99' not found"; exit 1 ;;
esac
''')
    ldd.chmod(0o755)
    env = os.environ.copy()
    env["PATH"] = str(fixture / "test-bin") + os.pathsep + env["PATH"]
    env["DISPLAY"] = env["WAYLAND_DISPLAY"] = ""
    env["TEST_LDD"] = "ok"
    result = subprocess.check_output([str(fixture / "AppRun")], text=True, env=env).splitlines()
    assert result == [str(fixture / "usr/lib"), str(fixture / "usr/lib/webkitgtk-6.0/injected-bundle")], result
    for scenario, binary, detail in (
        ("missing", "WebKitWebProcess", "libexample.so.1 => not found"),
        ("version", "WebKitNetworkProcess", "GLIBC_9.99"),
    ):
        env["TEST_LDD"] = scenario
        result = subprocess.run([str(fixture / "AppRun")], text=True, capture_output=True, env=env)
        assert result.returncode != 0 and not result.stdout, result
        assert binary in result.stderr and detail in result.stderr, result.stderr
    dialog = fixture / "test-bin/zenity"
    dialog.write_text('#!/bin/sh\nprintf "%s\\n%s\\n" "$*" "$LD_LIBRARY_PATH" > "$TEST_DIALOG"\n')
    dialog.chmod(0o755)
    env.update(TEST_LDD="missing", DISPLAY=":test", TEST_DIALOG=str(root / "dialog"), LD_LIBRARY_PATH="/host-libs")
    result = subprocess.run([str(fixture / "AppRun")], text=True, capture_output=True, env=env)
    assert result.returncode != 0
    text = (root / "dialog").read_text()
    assert "libexample.so.1 => not found" in text and text.rstrip().endswith("/host-libs"), text

print("AppImage WebKit sandbox layout, launcher and dependency diagnostics: OK")
