#!/bin/sh
# Launch the AppImage on a virtual display and assert it reaches client_start,
# opens a WebKit view and stays alive for the observation window.
set -eu
image=${1:?usage: smoke-linux-appimage.sh <AppImage>}
seconds=${SMOKE_SECONDS:-20}
work=$(mktemp -d "${TMPDIR:-/tmp}/esp32-smoke.XXXXXXXX")
trap 'rm -rf "$work"' EXIT HUP INT TERM

cat > "$work/run.sh" <<'EOF'
#!/bin/sh
"$1" >"$2" 2>&1 &
pid=$!
sleep "$3"
if ! kill -0 "$pid" 2>/dev/null; then
  printf '%s\n' client_exited_early >"$4"
  exit 1
fi
pgrep -f WebKitWebProcess >"$5" || true
kill "$pid" 2>/dev/null || true
wait "$pid" 2>/dev/null || true
EOF
chmod 0755 "$work/run.sh"

export XDG_CONFIG_HOME="$work/config"
export XDG_DATA_HOME="$work/data"
export APPIMAGE_EXTRACT_AND_RUN=1
export ESP32_MONITOR_SKIP_DESKTOP_INTEGRATION=1
export GDK_BACKEND=x11 LIBGL_ALWAYS_SOFTWARE=1
# Unprivileged user namespaces are unavailable in many CI containers. This
# weakens the WebKit sandbox for the smoke run only.
export WEBKIT_DISABLE_SANDBOX_THIS_IS_DANGEROUS=1

status=0
xvfb-run -a dbus-run-session -- "$work/run.sh" \
  "$image" "$work/stdout.log" "$seconds" "$work/early" "$work/webkit" || status=$?

log="$XDG_CONFIG_HOME/ESP32-Monitor/logs/client.log"
[ "$status" -eq 0 ] || { printf 'smoke: client did not stay alive\n' >&2; cat "$work/stdout.log" >&2; exit 1; }
[ -f "$log" ] || { printf 'smoke: missing %s\n' "$log" >&2; exit 1; }
grep -q 'client_start' "$log" || { printf 'smoke: client_start not logged\n' >&2; cat "$log" >&2; exit 1; }
[ -s "$work/webkit" ] || { printf 'smoke: WebKitWebProcess not observed\n' >&2; cat "$log" >&2; exit 1; }

printf 'smoke: client_start logged, WebKit view created, ran %ss\n' "$seconds"
