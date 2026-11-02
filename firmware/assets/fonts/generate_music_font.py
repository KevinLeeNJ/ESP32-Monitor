#!/usr/bin/env python3
"""Generate the dynamically loaded LVGL .bin music font.

Requires fonttools[woff] and lv_font_conv 1.5.3. Source and SIL OFL 1.1
license are in client/frontend/public/fonts/.
"""
import argparse
from pathlib import Path
import subprocess
import tempfile
from fontTools.ttLib import TTFont

root = Path(__file__).resolve().parents[3]
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--converter', required=True, type=Path)
parser.add_argument('--output', type=Path, default=root / 'firmware/assets/fonts/music-cjk.bin')
args = parser.parse_args()
source = root / 'client/frontend/public/fonts/fusion-pixel-zh-hans-12px.woff2'
target = args.output
with tempfile.TemporaryDirectory() as tmp:
    font = TTFont(source)
    font.flavor = None
    ttf = Path(tmp) / 'fusion-pixel.ttf'
    font.save(ttf)
    subprocess.run(['node', str(args.converter.resolve(strict=True)), '--size', '14',
        '--bpp', '1', '--format', 'bin', '--no-compress', '--no-kerning', '--font', str(ttf),
        '--range', '0x20-0x7e,0x3000-0x30ff,0x3130-0x318f,0x3400-0x9fff,0xac00-0xd7a3,0xff00-0xffef',
        '--output', str(target)], check=True)
