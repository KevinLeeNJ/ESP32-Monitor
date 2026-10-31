#!/bin/sh
# Bundle GTK/WebKit on the build host; system services and graphics remain host-provided.
set -eu
client_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
linuxdeploy=${LINUXDEPLOY:-linuxdeploy}
appimagetool=${APPIMAGETOOL:-appimagetool}
for tool in "$linuxdeploy" "$appimagetool" patchelf pkg-config python3 go npm rsync; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    printf 'Required build tool not found: %s\n' "$tool" >&2
    exit 1
  fi
done
linuxdeploy=$(command -v "$linuxdeploy")
appimagetool=$(command -v "$appimagetool")
case $(uname -m) in
  x86_64|aarch64) arch=$(uname -m) ;;
  *) printf '%s\n' 'Unsupported AppImage architecture.' >&2; exit 1 ;;
esac
webkit_dir="$(pkg-config --variable=libdir webkitgtk-6.0)/webkitgtk-6.0"
case "$webkit_dir" in /usr/*) ;; *) printf 'Unsupported WebKit directory: %s\n' "$webkit_dir" >&2; exit 1 ;; esac
[ -d "$webkit_dir/injected-bundle" ] || { printf 'WebKit helpers not found: %s\n' "$webkit_dir" >&2; exit 1; }
lgpl_text=/usr/share/common-licenses/LGPL-2.1
[ -f "$lgpl_text" ] || { printf 'LGPL-2.1 license text not found: %s\n' "$lgpl_text" >&2; exit 1; }
stage=$(mktemp -d "${TMPDIR:-/tmp}/esp32-appimage.XXXXXXXX")
trap 'rm -rf "$stage"' EXIT HUP INT TERM
appdir="$stage/ESP32-Monitor.AppDir"
mkdir -p "$appdir/usr/bin" "$appdir/usr/share/doc/esp32-monitor" "$appdir/usr/share/esp32-monitor" "$appdir$(dirname "$webkit_dir")" "$stage/tools"
cd "$client_dir/frontend"
[ -d node_modules ] || npm ci
npm run build
mkdir -p "$client_dir/internal/guiassets/assets"
rsync -a --delete "$client_dir/frontend/dist-web/" "$client_dir/internal/guiassets/assets/"
cd "$client_dir"
go build -trimpath -o "$appdir/usr/bin/monitor-client" ./cmd/monitor-client
go build -trimpath -o "$appdir/usr/bin/monitor-agent.bin" ./cmd/monitor-agent
cp "$client_dir/packaging/linux/appimage/AgentRun" "$appdir/usr/bin/monitor-agent"
cp -a "$webkit_dir" "$appdir$webkit_dir"
printf '%s\n' "$webkit_dir" > "$appdir/usr/share/esp32-monitor/webkit-helper-dir"
docdir="$appdir/usr/share/doc/esp32-monitor"
install -m 644 "$client_dir/packaging/linux/README.txt" "$docdir/README.txt"
install -m 644 "$client_dir/../LICENSE" "$docdir/LICENSE.txt"
install -m 644 "$client_dir/packaging/THIRD_PARTY_NOTICES.txt" "$docdir/THIRD_PARTY_NOTICES.txt"
install -m 644 "$client_dir/third_party/go-keyring/LICENSE" "$docdir/LICENSE-go-keyring.txt"
install -m 644 "$client_dir/frontend/public/fonts/LICENSE-fusion-pixel.txt" "$docdir/LICENSE-fusion-pixel.txt"
install -m 644 "$client_dir/frontend/public/fonts/LICENSE-unscii/unscii.html" "$docdir/LICENSE-unscii.html"
install -m 644 "$client_dir/frontend/public/icons/LICENSE" "$docdir/LICENSE-pixelarticons.txt"
install -m 644 "$lgpl_text" "$docdir/LICENSE-LGPL-2.1.txt"
# Tauri's plugin rewrites every /usr in WebKit, including /usr/bin/bwrap.
# Remove that one operation; apply a targeted relocation after deployment.
python3 - "$client_dir/third_party/tauri-linuxdeploy/linuxdeploy-plugin-gtk.sh" "$stage/tools/linuxdeploy-plugin-gtk.sh" <<'PY'
import pathlib, sys
source = pathlib.Path(sys.argv[1]).read_text()
gtk3 = 'DEPLOY_GTK_VERSION=3 # Force GTK3 for tauri apps'
if source.count(gtk3) != 1:
    raise SystemExit('Unexpected GTK plugin: GTK version override was not found exactly once')
source = source.replace(gtk3, 'DEPLOY_GTK_VERSION="${DEPLOY_GTK_VERSION:-0}"')
line = '''find "$APPDIR"/usr/lib* -name 'libwebkit*' -exec sed -i -e "s|/usr|././|g" '{}' \\;'''
if source.count(line) != 1:
    raise SystemExit('Unexpected GTK plugin: WebKit rewrite was not found exactly once')
path = pathlib.Path(sys.argv[2])
path.write_text(source.replace(line, ': # WebKit relocation is handled by the build script'))
path.chmod(0o755)
PY
gtk_lib="$(pkg-config --variable=libdir gtk4)/libgtk-4.so.1"
if command -v readelf >/dev/null 2>&1 && readelf -SW "$gtk_lib" | grep -q '\.relr\.dyn'; then
  export NO_STRIP=1
fi
PATH="$stage/tools:$PATH" DEPLOY_GTK_VERSION=4 APPIMAGE_EXTRACT_AND_RUN=1 "$linuxdeploy" --appdir "$appdir" \
  --deploy-deps-only "$appdir/usr/bin/monitor-client" --deploy-deps-only "$appdir$webkit_dir" \
  --desktop-file "$client_dir/packaging/linux/appimage/esp32-monitor.desktop" \
  --icon-file "$client_dir/assets/icons/app-icon-256.png" --icon-filename esp32-monitor \
  --custom-apprun "$client_dir/packaging/linux/appimage/AppRun" \
  --exclude-library 'libwayland*' --exclude-library 'libEGL*' --exclude-library 'libGL*' \
  --exclude-library 'libgbm*' --exclude-library 'libdrm*' --exclude-library 'libvulkan*' --plugin gtk
if ! grep -q 'GTK_PATH=.*gtk-4.0' "$appdir/apprun-hooks/linuxdeploy-plugin-gtk.sh"; then
  printf '%s\n' 'GTK plugin did not configure GTK 4.' >&2
  exit 1
fi
# The plugin's nested linuxdeploy invocation may add graphics libraries again.
python3 - "$appdir" "$webkit_dir" <<'PY'
import pathlib, sys
appdir = pathlib.Path(sys.argv[1])
libdir = appdir / 'usr/lib'
for pattern in ('libwayland*', 'libEGL*', 'libGL*', 'libgbm*', 'libdrm*', 'libvulkan*'):
    for path in libdir.glob(pattern):
        if path.is_file() or path.is_symlink():
            path.unlink()
webkit = libdir / 'libwebkitgtk-6.0.so.4'
data = webkit.read_bytes()
original = sys.argv[2].encode() + b'\0'
relocated = b'././' + pathlib.Path(sys.argv[2]).name.encode() + b'/'
if len(relocated) >= len(original) or original not in data:
    raise SystemExit('Cannot relocate WebKit helper path safely')
webkit.write_bytes(data.replace(original, relocated.ljust(len(original), b'\0')))
(appdir / sys.argv[2].lstrip('/')).rename(libdir / pathlib.Path(sys.argv[2]).name)
PY
patchelf --set-rpath '$ORIGIN' "$appdir/usr/lib/libwebkitgtk-6.0.so.4"
for helper in "$appdir/usr/lib/$(basename "$webkit_dir")"/WebKit*Process; do
  [ -f "$helper" ] && patchelf --set-rpath '$ORIGIN/..' "$helper"
done
injected="$appdir/usr/lib/$(basename "$webkit_dir")/injected-bundle/libwebkitgtkinjectedbundle.so"
relative=$(python3 -c 'import os,sys; print(os.path.relpath(sys.argv[1], sys.argv[2]))' "$appdir/usr/lib" "$(dirname "$injected")")
patchelf --set-rpath "\$ORIGIN/$relative:\$ORIGIN" "$injected"
# linuxdeploy wraps custom AppRun after hooks; we must save the host environment first.
cp "$client_dir/packaging/linux/appimage/AppRun" "$appdir/AppRun"
output="$client_dir/build/linux/ESP32-Monitor-$arch.AppImage"
ARCH="$arch" APPIMAGE_EXTRACT_AND_RUN=1 "$appimagetool" "$appdir" "$stage/ESP32-Monitor.AppImage"
mv -f "$stage/ESP32-Monitor.AppImage" "$output"
printf 'Built %s\n' "$output"
