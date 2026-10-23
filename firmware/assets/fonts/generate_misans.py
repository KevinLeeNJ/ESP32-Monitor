#!/usr/bin/env python3
"""Regenerate the screen's MiSans C resources with lv_font_conv 1.5.3.

Usage: python3 generate_misans.py --font /path/to/MiSans-Regular.otf
       --converter /path/to/lv_font_conv/lv_font_conv.js

MiSans font software is supplied separately by Xiaomi:
https://hyperos.mi.com/font/download/
Generated glyphs are embedded in this application, not a standalone font package.
"""

import argparse
from pathlib import Path
import subprocess
import tempfile


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--font", required=True, type=Path)
    parser.add_argument("--converter", required=True, type=Path)
    args = parser.parse_args()
    root = Path(__file__).resolve().parents[3]
    output = root / "firmware/app/ui/fonts"
    output.mkdir(parents=True, exist_ok=True)
    icons = root / "third_party/lvgl/scripts/built_in_font/FontAwesome5-Solid+Brands+Regular.woff"
    weather = "晴少云多阴天小雨中大雷阵雪夹冰雹雾霾沙尘暴"

    with tempfile.TemporaryDirectory() as workdir:
        work = Path(workdir)
        (work / "MiSans-Regular.otf").symlink_to(args.font.resolve(strict=True))
        (work / "FontAwesome.woff").symlink_to(icons)
        for size in (8, 12, 14, 20, 30):
            name = f"monitor_misans_{size}"
            command = [
                "node", str(args.converter.resolve(strict=True)),
                "--size", str(size), "--bpp", "1", "--format", "lvgl",
                "--no-compress", "--font", "MiSans-Regular.otf",
                "--range", "0x20-0x7e,0xb0", "--symbols", weather,
            ]
            if size == 14:
                command += ["--font", "FontAwesome.woff", "--range", "0xf021,0xf071,0xf1eb"]
            command += ["--lv-font-name", name, "--output", f"{name}.c"]
            subprocess.run(command, cwd=work, check=True)
            source = (work / f"{name}.c").read_text()
            attribution = (
                "/* This application uses MiSans Regular.\n"
                " * Copyright 2020-2023 Beijing Xiaomi Mobile Software Co.,Ltd.\n"
                " * Font source: https://hyperos.mi.com/font/\n"
                " * Regenerate with firmware/assets/fonts/generate_misans.py.\n"
                " */\n"
            )
            if size == 14:
                attribution += (
                    "/* Status icons: Font Awesome 5, Fonticons, Inc. (SIL OFL 1.1).\n"
                    " * See third_party/lvgl/scripts/built_in_font/font_license/FontAwesome5/LICENSE.txt.\n"
                    " */\n"
                )
            (output / f"{name}.c").write_text(attribution + source)


if __name__ == "__main__":
    main()
