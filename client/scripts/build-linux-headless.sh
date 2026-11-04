#!/bin/sh
# Build the standalone Linux collector without desktop dependencies.
set -eu
client_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
arch=${GOARCH:-$(go env GOARCH)}
case "$arch" in amd64|arm64) ;; *) printf 'Unsupported architecture: %s\n' "$arch" >&2; exit 1 ;; esac
stage=$(mktemp -d "${TMPDIR:-/tmp}/esp32-headless.XXXXXXXX")
trap 'rm -rf "$stage"' EXIT HUP INT TERM
mkdir -p "$client_dir/build/linux-headless"
cd "$client_dir"
GOCACHE="$stage/go-build" CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -trimpath -o "$stage/monitor-headless" ./cmd/monitor-headless
install -m 755 "$stage/monitor-headless" "$client_dir/build/linux-headless/monitor-headless-linux-$arch"
printf 'Built %s\n' "$client_dir/build/linux-headless/monitor-headless-linux-$arch"
