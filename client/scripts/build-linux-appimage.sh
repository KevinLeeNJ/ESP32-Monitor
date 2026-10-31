#!/bin/sh
# Build an AppImage using the host's GTK4 and WebKitGTK runtime libraries.
set -eu
client_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
appimagetool=${APPIMAGETOOL:-appimagetool}
for tool in "$appimagetool" pkg-config go npm rsync; do
  command -v "$tool" >/dev/null 2>&1 || { printf 'Required build tool not found: %s\n' "$tool" >&2; exit 1; }
done
case $(uname -m) in
  x86_64|aarch64) arch=$(uname -m) ;;
  *) printf '%s\n' 'Unsupported AppImage architecture.' >&2; exit 1 ;;
esac
pkg-config --exists gtk4 webkitgtk-6.0
stage=$(mktemp -d "${TMPDIR:-/tmp}/esp32-appimage.XXXXXXXX")
trap 'rm -rf "$stage"' EXIT HUP INT TERM
appdir="$stage/ESP32-Monitor.AppDir"
mkdir -p "$appdir/usr/bin" "$appdir/usr/share/doc/esp32-monitor" "$client_dir/build/linux"
cd "$client_dir/frontend"
[ -d node_modules ] || npm ci
npm run build
mkdir -p "$client_dir/internal/guiassets/assets"
rsync -a --delete "$client_dir/frontend/dist-web/" "$client_dir/internal/guiassets/assets/"
cd "$client_dir"
go build -trimpath -o "$appdir/usr/bin/monitor-client" ./cmd/monitor-client
go build -trimpath -o "$appdir/usr/bin/monitor-agent" ./cmd/monitor-agent
install -m 755 "$client_dir/packaging/linux/appimage/AppRun" "$appdir/AppRun"
install -m 644 "$client_dir/packaging/linux/appimage/esp32-monitor.desktop" "$appdir/esp32-monitor.desktop"
install -m 644 "$client_dir/assets/icons/app-icon-256.png" "$appdir/esp32-monitor.png"
docdir="$appdir/usr/share/doc/esp32-monitor"
install -m 644 "$client_dir/packaging/linux/README.txt" "$docdir/README.txt"
install -m 644 "$client_dir/../LICENSE" "$docdir/LICENSE.txt"
install -m 644 "$client_dir/packaging/THIRD_PARTY_NOTICES.txt" "$docdir/THIRD_PARTY_NOTICES.txt"
install -m 644 "$client_dir/third_party/go-keyring/LICENSE" "$docdir/LICENSE-go-keyring.txt"
install -m 644 "$client_dir/frontend/public/fonts/LICENSE-fusion-pixel.txt" "$docdir/LICENSE-fusion-pixel.txt"
install -m 644 "$client_dir/frontend/public/fonts/LICENSE-unscii/unscii.html" "$docdir/LICENSE-unscii.html"
install -m 644 "$client_dir/frontend/public/icons/LICENSE" "$docdir/LICENSE-pixelarticons.txt"
output="$client_dir/build/linux/ESP32-Monitor-$arch.AppImage"
ARCH="$arch" APPIMAGE_EXTRACT_AND_RUN=1 "$appimagetool" "$appdir" "$stage/ESP32-Monitor.AppImage"
mv -f "$stage/ESP32-Monitor.AppImage" "$output"
printf 'Built %s\n' "$output"
