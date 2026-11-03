#!/bin/sh
# Assert the AppImage matches the documented runtime baseline: Ubuntu 24.04
# glibc, host-provided GTK4/WebKitGTK 6.0 and no bundled shared libraries.
set -eu
input=${1:?usage: check-linux-baseline.sh <AppImage|AppDir>}
case "$input" in
  /*) ;;
  *) input="$PWD/$input" ;;
esac
baseline=${MAX_GLIBC:-2.39}

version_gt() { # $1 > $2
  awk -v a="$1" -v b="$2" 'BEGIN {
    n = split(a, A, "."); m = split(b, B, ".")
    for (i = 1; i <= 3; i++) {
      x = (i <= n ? A[i] : 0) + 0; y = (i <= m ? B[i] : 0) + 0
      if (x > y) exit 0
      if (x < y) exit 1
    }
    exit 1
  }'
}

fail() {
  printf 'baseline check failed: %s\n' "$*" >&2
  exit 1
}

case "$input" in
  *.AppImage)
    tmp=$(mktemp -d "${TMPDIR:-/tmp}/esp32-baseline.XXXXXXXX")
    trap 'rm -rf "$tmp"' EXIT HUP INT TERM
    ( cd "$tmp" && "$input" --appimage-extract >/dev/null )
    root="$tmp/squashfs-root"
    ;;
  *)
    root=$input
    ;;
esac

for rel in usr/bin/monitor-client usr/bin/monitor-agent; do
  [ -f "$root/$rel" ] || fail "missing $rel"
  max=$(objdump -T "$root/$rel" | grep -o 'GLIBC_[0-9.]*' | sed 's/GLIBC_//' | sort -V | tail -n 1)
  [ -n "$max" ] || fail "no glibc symbols found in $rel"
  version_gt "$max" "$baseline" && fail "$rel requires glibc $max, baseline is $baseline"
  printf '%s: max glibc %s\n' "$rel" "$max"
done

needed=$(readelf -d "$root/usr/bin/monitor-client" | grep NEEDED)
for lib in libgtk-4.so.1 libwebkitgtk-6.0.so.4; do
  printf '%s\n' "$needed" | grep -q "$lib" || fail "$lib is not a runtime dependency"
done
printf '%s\n' 'monitor-client: host GTK4 and WebKitGTK 6.0'

if find "$root" -type f -name '*.so*' | grep -q .; then
  fail "bundled shared libraries found"
fi
printf '%s\n' 'no bundled shared libraries'

info="$root/usr/share/doc/esp32-monitor/build-info.json"
[ -f "$info" ] || fail "missing build-info.json"
printf '%s\n' 'build-info.json:'
cat "$info"
