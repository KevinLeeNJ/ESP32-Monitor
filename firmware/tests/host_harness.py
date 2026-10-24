"""Compile production functions with explicit deterministic SDK boundaries."""
from pathlib import Path
import re
import subprocess
import tempfile

REPO = Path(__file__).resolve().parents[2]


def function(source, name):
    # Ignore braces in strings and comments without changing byte offsets.
    masked = re.sub(r'//[^\n]*|/\*[\s\S]*?\*/|"(?:\\.|[^"\\])*"|\'(?:\\.|[^\'\\])*\'',
                    lambda m: ' ' * len(m[0]), source)
    for match in re.finditer(r'^[^\n;{}]*\b' + re.escape(name) + r'\s*\(', masked, re.M):
        brace = masked.find('{', match.end())
        semicolon = masked.find(';', match.end())
        if brace < 0 or (semicolon >= 0 and semicolon < brace):
            continue
        depth, end = 1, brace + 1
        while depth:
            depth += (masked[end] == '{') - (masked[end] == '}')
            end += 1
        return source[match.start():end]
    raise AssertionError(f'Production function missing: {name}')


def defines(source, names):
    return '\n'.join(re.search(r'^#define ' + re.escape(name) + r'\b[^\n]*', source, re.M)[0]
                     for name in names)


def run(code, compiler='cc', *, cpp=False, cjson=None, libraries=()):
    with tempfile.TemporaryDirectory(prefix='monitor-regression-') as directory:
        unit = Path(directory) / ('test.cc' if cpp else 'test.c')
        unit.write_text(code)
        binary = Path(directory) / 'test'
        cmd = [compiler, '-std=c++17' if cpp else '-std=c11', '-Wall', '-Wextra', '-Werror', '-Wno-unused-parameter',
               '-I'+str(REPO/'firmware/tests/fakes'),
               '-I'+str(REPO/'firmware/include'),
               '-I'+str(REPO/'firmware/components/firmware'),
               '-I'+str(REPO/'third_party'), '-I'+str(REPO/'third_party/lvgl')]
        if cjson:
            cmd += ['-I'+str(cjson), str(Path(cjson)/'cJSON.c')]
        subprocess.run(cmd + [str(unit), '-lm', *libraries, '-o', str(binary)], check=True, timeout=60)
        subprocess.run([str(binary)], check=True, timeout=10)
