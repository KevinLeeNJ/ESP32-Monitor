#!/bin/sh
# Build the macOS application bundle: Wails GUI + Agent + media dylib.
set -eu
version=0.1.0
skip_tests=0
while [ "$#" -gt 0 ]; do
  case "$1" in
    --version) [ "$#" -ge 2 ] || { echo 'Missing version' >&2; exit 1; }; version=$2; shift 2 ;;
    --skip-tests) skip_tests=1; shift ;;
    *) echo "Unknown argument: $1" >&2; exit 1 ;;
  esac
done
printf '%s\n' "$version" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$' || { echo 'Version must look like 1.2.3' >&2; exit 1; }
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

node_modules_created=0
[ -d "$client_dir/frontend/node_modules" ] || node_modules_created=1
stage=""
cleanup() {
  [ -z "$stage" ] || rm -rf "$stage"
  rm -rf "$client_dir/frontend/dist-web" "$client_dir/internal/guiassets/assets"
  mkdir -p "$client_dir/internal/guiassets/assets"
  printf '%s\n' 'Frontend has not been built.' > "$client_dir/internal/guiassets/assets/index.html"
  rm -f "$build_dir/monitor-client" "$build_dir/monitor-agent" "$client_dir/bin/libmonitor-media.dylib"
  if [ "$node_modules_created" = 1 ]; then rm -rf "$client_dir/frontend/node_modules"; fi
}
trap cleanup EXIT HUP INT TERM
mkdir -p "$build_dir"

cd "$client_dir/frontend"
if [ ! -d node_modules ]; then
  npm ci
fi
npm run build

mkdir -p "$client_dir/internal/guiassets/assets"
rsync -a --delete "$client_dir/frontend/dist-web/" "$client_dir/internal/guiassets/assets/"

cd "$client_dir"
if [ "$skip_tests" = 0 ]; then
  echo 'Running Go tests and vet'
  "$GO" test ./cmd/monitor-agent ./cmd/monitor-client ./cmd/sim-esp32 ./cmd/sim-server ./internal/...
  "$GO" vet ./cmd/monitor-agent ./cmd/monitor-client ./cmd/sim-esp32 ./cmd/sim-server ./internal/...
fi
"$GO" build -trimpath -ldflags "$ldflags" -o "$build_dir/monitor-client" ./cmd/monitor-client
"$GO" build -trimpath -ldflags "$ldflags" -o "$build_dir/monitor-agent" ./cmd/monitor-agent
./scripts/build-nowplaying-macos.sh

for binary in "$build_dir/monitor-client" "$build_dir/monitor-agent" "$client_dir/bin/libmonitor-media.dylib"; do
  minos=$(vtool -show-build "$binary" 2>/dev/null | awk '/minos/{print $2; exit}')
  printf '%s: macOS %s\n' "$(basename "$binary")" "${minos:-unknown}"
done

# Assemble and sign outside the iCloud-synced tree: the fileprovider daemon
# re-adds FinderInfo/fpfs xattrs to bundles under Documents, which trips
# codesign --verify. Package the staged bundle directly into the DMG.
stage=$(mktemp -d)
bundle="$stage/$app_name.app"
contents="$bundle/Contents"
mkdir -p "$contents/MacOS" "$contents/Resources"
cp "$build_dir/monitor-client" "$contents/MacOS/monitor-client"
cp "$build_dir/monitor-agent" "$contents/MacOS/monitor-agent"
cp "$client_dir/bin/libmonitor-media.dylib" "$contents/MacOS/libmonitor-media.dylib"
cp "$client_dir/packaging/darwin/Info.plist" "$contents/Info.plist"
/usr/libexec/PlistBuddy -c "Set :LSMinimumSystemVersion $macos_target" "$contents/Info.plist"
/usr/libexec/PlistBuddy -c "Set :CFBundleShortVersionString $version" "$contents/Info.plist"
/usr/libexec/PlistBuddy -c "Set :CFBundleVersion $version" "$contents/Info.plist"
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

arch=$("$GO" env GOARCH)
dmg="$build_dir/ESP32-Monitor-$version-darwin-$arch.dmg"
ln -s /Applications "$stage/Applications"
rm -f "$dmg"
hdiutil create -volname "$app_name" -srcfolder "$stage" -format UDZO "$dmg"
hdiutil verify "$dmg"

rm -rf "$stage"
rm -rf "$build_dir/$app_name.app"
echo "Built DMG: $dmg"
echo "DMG: $dmg"
