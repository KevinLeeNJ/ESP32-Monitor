#!/usr/bin/env python3
"""Generate uncompressed LVGL music fonts.

Requires fonttools[woff] and lv_font_conv 1.5.3. The source font and its
SIL OFL 1.1 license are in client/frontend/public/fonts/. Building the
generated C sources requires a compiler with #embed support.

Glyph bitmaps and glyph descriptors are kept as .bin resources and
compiled in with #embed. The descriptor records use LVGL's 8-byte
LV_FONT_FMT_TXT_LARGE==0 layout (little-endian), which the generated C
checks at build time. Character-map tables stay in C: their cmaps carry
pointers, and the remaining data arrays are small.
"""
import argparse
import re
import struct
from pathlib import Path
import subprocess
import tempfile
from fontTools.ttLib import TTFont

root = Path(__file__).resolve().parents[3]
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--converter', required=True, type=Path)
args = parser.parse_args()
source = root / 'client/frontend/public/fonts/fusion-pixel-zh-hans-12px.woff2'
output = root / 'firmware/app/ui/fonts'
common = '0x20-0x7e,0xa0-0xff,0x2000-0x206f,0x2605-0x2606,0x266a-0x266b,0x3000-0x30ff,0x3130-0x318f,0x3400-0x9fff'
extra = '0xac00-0xd7a3,0xff00-0xffef'
# Split the 18px bitmap below LVGL's 20-bit bitmap-index limit.
fonts = [(18, 'monitor_music_18', common),
         (18, 'monitor_music_18_extra', extra),
         (12, 'monitor_music_12', common + ',' + extra)]
with tempfile.TemporaryDirectory() as tmp:
    font = TTFont(source)
    font.flavor = None
    ttf = Path(tmp) / 'fusion-pixel.ttf'
    font.save(ttf)
    for size, name, ranges in fonts:
        target = output / (name + '.c')
        subprocess.run(['node', str(args.converter.resolve(strict=True)), '--size', str(size),
            '--bpp', '1', '--format', 'lvgl', '--no-compress', '--no-kerning',
            '--font', str(ttf), '--range', ranges, '--lv-font-name', name,
            '--output', str(target)], check=True)
        text = target.read_text(encoding='utf-8')
        # Converter options contain temporary absolute paths; keep output reproducible.
        text = '\n'.join(line for line in text.split('\n') if not line.startswith(' * Opts:'))
        if name == 'monitor_music_18':
            text = text.replace('#ifndef MONITOR_MUSIC_18',
                                'LV_FONT_DECLARE(monitor_music_18_extra);\n\n#ifndef MONITOR_MUSIC_18', 1)
            text = text.replace('.fallback = NULL,', '.fallback = &monitor_music_18_extra,', 1)
        match = re.search(r'(static LV_ATTRIBUTE_LARGE_CONST const uint8_t glyph_bitmap\[\] = \{)(.*?)(\n\};)', text, re.S)
        if not match:
            raise ValueError(f"bitmap array missing in {name}")
        values = re.findall(r'0x([0-9a-fA-F]{1,2})\b', re.sub(r'/\*.*?\*/', '', match.group(2), flags=re.S))
        bitmap = bytes(int(value, 16) for value in values)
        (output / (name + '.bin')).write_bytes(bitmap)
        embed = f'\n#embed "{name}.bin"'
        text = text[:match.start(2)] + embed + text[match.end(2):]

        # Pack glyph_dsc into LVGL's 8-byte LARGE==0 layout: a 32-bit word
        # holding bitmap_index (low 20 bits) and adv_w (high 12 bits),
        # followed by box_w, box_h and signed offsets.
        match = re.search(r'static const lv_font_fmt_txt_glyph_dsc_t glyph_dsc\[\] = \{(.*?)\n\};', text, re.S)
        if not match:
            raise ValueError(f"glyph_dsc array missing in {name}")
        body = re.sub(r'/\*.*?\*/', '', match.group(1), flags=re.S)
        entries = re.findall(
            r'\{\.bitmap_index = (\d+), \.adv_w = (\d+), \.box_w = (\d+), '
            r'\.box_h = (\d+), \.ofs_x = (-?\d+), \.ofs_y = (-?\d+)\}', body)
        if len(entries) != body.count('{.bitmap_index'):
            raise ValueError(f"unparsed glyph entries in {name}")
        records = bytearray()
        for bitmap_index, adv_w, box_w, box_h, ofs_x, ofs_y in entries:
            values = tuple(int(value) for value in (bitmap_index, adv_w, box_w, box_h, ofs_x, ofs_y))
            if not (values[0] <= 0xFFFFF and values[1] <= 0xFFF and
                    0 <= values[2] <= 0xFF and 0 <= values[3] <= 0xFF and
                    -0x80 <= values[4] <= 0x7F and -0x80 <= values[5] <= 0x7F):
                raise ValueError(f"glyph entry out of range in {name}: {values}")
            word = values[0] | (values[1] << 20)
            records += struct.pack('<IBBbb', word, values[2], values[3], values[4], values[5])
        dsc_bin = name + '_glyph_dsc.bin'
        (output / dsc_bin).write_bytes(records)
        replacement = (
            '/* Glyph descriptors packed for LV_FONT_FMT_TXT_LARGE == 0. */\n'
            '_Static_assert(__BYTE_ORDER__ == __ORDER_LITTLE_ENDIAN__, "music font data is little-endian");\n'
            '_Static_assert(sizeof(lv_font_fmt_txt_glyph_dsc_t) == 8, "unexpected glyph descriptor layout");\n'
            'static const uint8_t glyph_dsc_bin[] __attribute__((aligned(4))) = {\n'
            f'#embed "{dsc_bin}"\n'
            '};')
        text = text[:match.start()] + replacement + text[match.end():]
        text = text.replace('.glyph_dsc = glyph_dsc,',
                            '.glyph_dsc = (const lv_font_fmt_txt_glyph_dsc_t *)(const void *)glyph_dsc_bin,', 1)
        target.write_text(text, encoding='utf-8', newline='\n')
