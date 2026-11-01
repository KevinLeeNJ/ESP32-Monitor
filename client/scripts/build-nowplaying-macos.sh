#!/bin/sh
set -eu
client_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
macos_target=${MACOSX_DEPLOYMENT_TARGET:-13.0}
mkdir -p "$client_dir/bin"
xcrun clang -mmacosx-version-min="$macos_target" \
  -dynamiclib -fobjc-arc -fblocks -Wall -Wextra \
  -Wno-unused-parameter -framework Foundation -framework AppKit \
  "$client_dir/native/nowplaying/mediaremote.m" \
  -o "$client_dir/bin/libmonitor-media.dylib"
