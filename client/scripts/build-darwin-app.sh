#!/bin/sh
# Build the macOS application bundle: Wails GUI + Agent + media dylib.
set -eu
client_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
app_name="ESP32 Monitor"
build_dir="$client_dir/build"
bundle="$build_dir/$app_name.app"
contents="$bundle/Contents"

mkdir -p "$build_dir"

cd "$client_dir/frontend"
if [ ! -d node_modules ]; then
  npm install
fi
npm run build

mkdir -p "$client_dir/internal/guiassets/assets"
rsync -a --delete "$client_dir/frontend/dist-web/" "$client_dir/internal/guiassets/assets/"

cd "$client_dir"
go build -trimpath -o "$build_dir/monitor-client" ./cmd/monitor-client
go build -trimpath -o "$build_dir/monitor-agent" ./cmd/monitor-agent
./scripts/build-nowplaying-macos.sh

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
cp "$client_dir/assets/icons/app-icon.icns" "$contents/Resources/app-icon.icns"

xattr -cr "$bundle"
codesign --force --deep --sign - "$bundle"
codesign --verify --deep --strict "$bundle"

rm -rf "$build_dir/$app_name.app"
mv "$bundle" "$build_dir/"
rm -rf "$stage"
xattr -cr "$build_dir/$app_name.app" 2>/dev/null || true
echo "Built $build_dir/$app_name.app"
