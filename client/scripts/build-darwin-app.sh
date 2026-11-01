#!/bin/sh
# Build the macOS application bundle: Wails GUI + Agent + media dylib.
set -eu
client_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
app_name="ESP32 Monitor"
build_dir="$client_dir/build"
bundle="$build_dir/$app_name.app"
contents="$bundle/Contents"

# Keep every compiler invocation on macOS 13.0: Wails v3 supports macOS 12,
# but current Go runtimes (including Homebrew's bottle) require 13.0.
# Override with MACOSX_DEPLOYMENT_TARGET when using an older Go toolchain.
GO=${GO:-go}
macos_target=${MACOSX_DEPLOYMENT_TARGET:-13.0}
export MACOSX_DEPLOYMENT_TARGET="$macos_target"
cgo_min="-mmacosx-version-min=$macos_target"
export CGO_CFLAGS="${CGO_CFLAGS:-} $cgo_min"
export CGO_CXXFLAGS="${CGO_CXXFLAGS:-} $cgo_min"
export CGO_LDFLAGS="${CGO_LDFLAGS:-} $cgo_min"
ldflags="-extldflags=$cgo_min"

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

# The Go runtime is also compiled for a minimum macOS. Homebrew's Go bottle is
# newer than the upstream floor, so warn instead of silently shipping an
# artifact that claims to run on an older release than its runtime supports.
probe=$(mktemp -d)
printf 'module deployment-probe\n\ngo 1.21\n' > "$probe/go.mod"
printf 'package main\nimport "C"\nfunc main(){}\n' > "$probe/main.go"
probe_output=$(cd "$probe" && "$GO" build -o "$probe/probe" . 2>&1 || true)
rm -rf "$probe"
go_floor=$(printf '%s\n' "$probe_output" | sed -n "s/.*was built for newer 'macOS' version (\([0-9][0-9.]*\)).*/\1/p" | head -n 1)
if [ -n "$go_floor" ] && version_gt "$go_floor" "$macos_target"; then
  printf 'warning: %s runtime objects target macOS %s, newer than %s.\n' "$("$GO" version | awk '{print $3}')" "$go_floor" "$macos_target" >&2
  printf 'warning: the app may not launch on macOS %s; use an upstream Go toolchain via GO=/path/to/go.\n' "$macos_target" >&2
fi

mkdir -p "$build_dir"

cd "$client_dir/frontend"
if [ ! -d node_modules ]; then
  npm install
fi
npm run build

mkdir -p "$client_dir/internal/guiassets/assets"
rsync -a --delete "$client_dir/frontend/dist-web/" "$client_dir/internal/guiassets/assets/"

cd "$client_dir"
"$GO" build -trimpath -ldflags "$ldflags" -o "$build_dir/monitor-client" ./cmd/monitor-client
"$GO" build -trimpath -ldflags "$ldflags" -o "$build_dir/monitor-agent" ./cmd/monitor-agent
./scripts/build-nowplaying-macos.sh

for binary in "$build_dir/monitor-client" "$build_dir/monitor-agent" "$client_dir/bin/libmonitor-media.dylib"; do
  minos=$(vtool -show-build "$binary" 2>/dev/null | awk '/minos/{print $2; exit}')
  printf '%s: macOS %s\n' "$(basename "$binary")" "${minos:-unknown}"
done

# Assemble and sign outside the iCloud-synced tree: the fileprovider daemon
# re-adds FinderInfo/fpfs xattrs to bundles under Documents, which trips
# codesign --verify. Move the finished bundle into build/ afterwards.
stage=$(mktemp -d)
bundle="$stage/$app_name.app"
contents="$bundle/Contents"
mkdir -p "$contents/MacOS" "$contents/Resources"
cp "$build_dir/monitor-client" "$contents/MacOS/monitor-client"
cp "$build_dir/monitor-agent" "$contents/MacOS/monitor-agent"
cp "$client_dir/bin/libmonitor-media.dylib" "$contents/MacOS/libmonitor-media.dylib"
cp "$client_dir/packaging/darwin/Info.plist" "$contents/Info.plist"
/usr/libexec/PlistBuddy -c "Set :LSMinimumSystemVersion $macos_target" "$contents/Info.plist"
cp "$client_dir/assets/icons/app-icon.icns" "$contents/Resources/app-icon.icns"
cp "$client_dir/../LICENSE" "$contents/Resources/LICENSE.txt"
cp "$client_dir/packaging/THIRD_PARTY_NOTICES.txt" "$contents/Resources/THIRD_PARTY_NOTICES.txt"
cp "$client_dir/third_party/go-keyring/LICENSE" "$contents/Resources/LICENSE-go-keyring.txt"
cp "$client_dir/frontend/public/fonts/LICENSE-fusion-pixel.txt" "$contents/Resources/LICENSE-fusion-pixel.txt"
cp "$client_dir/frontend/public/fonts/LICENSE-unscii/unscii.html" "$contents/Resources/LICENSE-unscii.html"
cp "$client_dir/frontend/public/icons/LICENSE" "$contents/Resources/LICENSE-pixelarticons.txt"

xattr -cr "$bundle"
codesign --force --deep --sign - "$bundle"
codesign --verify --deep --strict "$bundle"

rm -rf "$build_dir/$app_name.app"
mv "$bundle" "$build_dir/"
rm -rf "$stage"
xattr -cr "$build_dir/$app_name.app" 2>/dev/null || true
echo "Built $build_dir/$app_name.app"
