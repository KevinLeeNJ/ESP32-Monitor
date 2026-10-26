#!/bin/sh
set -eu
client_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
mkdir -p "$client_dir/bin"
xcrun clang -dynamiclib -fobjc-arc -fblocks -Wall -Wextra \
  -Wno-unused-parameter -framework Foundation -framework AppKit \
  "$client_dir/native/nowplaying/mediaremote.m" \
  -o "$client_dir/bin/libmonitor-media.dylib"
