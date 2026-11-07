#!/usr/bin/env python3
"""Compress a web asset for embedding in the configuration AP firmware.

gzip records a timestamp by default; mtime=0 keeps the embedded bytes
reproducible across builds so flash images only change with the page.
"""
import gzip
import sys
from pathlib import Path

source, target = (Path(arg) for arg in sys.argv[1:3])
target.parent.mkdir(parents=True, exist_ok=True)
target.write_bytes(gzip.compress(source.read_bytes(), compresslevel=9, mtime=0))
