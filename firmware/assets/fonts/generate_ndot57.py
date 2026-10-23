#!/usr/bin/env python3
"""Generate the monitor's 1bpp NDot57 fonts with MiSans CJK fallback glyphs.

Usage: python3 generate_ndot57.py --converter /path/to/lv_font_conv.js
"""

import argparse
from pathlib import Path
import subprocess
import tempfile


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--converter", required=True, type=Path)
    args = parser.parse_args()
    root = Path(__file__).resolve().parents[3]
    output = root / "firmware/app/ui/fonts"
    output.mkdir(parents=True, exist_ok=True)
    weather = "晴少云多阴天小雨中大雷阵雪夹冰雹雾霾沙尘暴"
    icons = root / "third_party/lvgl/scripts/built_in_font/FontAwesome5-Solid+Brands+Regular.woff"

    with tempfile.TemporaryDirectory() as workdir:
        work = Path(workdir)
        for size in (8, 12, 14, 20, 30):
            name = f"monitor_ndot57_{size}"
            command = [
                "node", str(args.converter.resolve(strict=True)),
                "--size", str(size), "--bpp", "1", "--format", "lvgl",
                "--no-compress",
                "--font", str(root / "firmware/fonts/Ndot57-Regular.otf"),
                "--range", "0x20-0x7e",
                "--font", str(Path.home() / "Library/Fonts/MISANS-REGULAR.OTF"),
                "--range", "0xb0", "--symbols", weather,
            ]
            if size == 14:
                command += ["--font", str(icons), "--range", "0xf021,0xf071,0xf1eb"]
            command += ["--lv-font-name", name, "--output", f"{name}.c"]
            subprocess.run(command, cwd=work, check=True)
            source = (work / f"{name}.c").read_text()
            header = (
                "/* Generated from firmware/fonts/Ndot57-Regular.otf and MiSans CJK.\n"
                " * 1bpp LVGL resource; regenerate with firmware/assets/fonts/generate_ndot57.py.\n"
                " */\n"
            )
            (output / f"{name}.c").write_text(header + source)


if __name__ == "__main__":
    main()
