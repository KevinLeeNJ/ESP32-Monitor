"""Small cross-platform helpers shared by the host firmware regressions.

CMake passes the compiler as argv[1]. Outside CMake the platform default is
used, and every temporary executable gets the host suffix (test.exe on
Windows) so subprocess can execute the artifact it just produced.
"""
import os
import subprocess
from pathlib import Path


def cc(preferred=None):
    return preferred or os.environ.get("CC") or ("gcc" if os.name == "nt" else "cc")


def cxx(preferred=None):
    return preferred or os.environ.get("CXX") or ("g++" if os.name == "nt" else "c++")


def binary(directory, name="test"):
    suffix = ".exe" if os.name == "nt" else ""
    return Path(directory) / (name + suffix)


def winsock_libraries():
    return ["-lws2_32"] if os.name == "nt" else []


def run(path, **kwargs):
    subprocess.run([str(path)], check=True, **kwargs)
