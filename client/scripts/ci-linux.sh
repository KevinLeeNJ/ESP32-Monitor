#!/bin/sh
# Linux client CI entry point. Runs static checks, short tests, the AppImage
# build, baseline/layout verification and a startup smoke, all inside the
# pinned Ubuntu 24.04 environment. Only Docker is required on the runner; this
# script has no GitHub-specific dependency.
set -eu
client_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
repo_root=$(CDPATH= cd -- "$client_dir/.." && pwd)
image=${BUILD_IMAGE:-esp32-monitor-linux-build:24.04}
image_dir="$client_dir/packaging/linux/build-env"
cache="$client_dir/build/cache"
arch=$(uname -m)
appimage="build/linux/ESP32-Monitor-$arch.AppImage"

command -v docker >/dev/null 2>&1 || {
  printf '%s\n' 'Docker is required for the Linux CI pipeline.' >&2
  exit 1
}

if [ "${FORCE_IMAGE_BUILD:-0}" = "1" ] || ! docker image inspect "$image" >/dev/null 2>&1; then
  docker build -t "$image" "$image_dir"
fi

mkdir -p "$cache/go-mod" "$cache/go-build" "$cache/npm" "$client_dir/build/linux"

run_in_env() {
  docker run --rm \
    --user "$(id -u):$(id -g)" \
    --env HOME=/tmp \
    --env GOCACHE=/cache/go-build \
    --env GOMODCACHE=/cache/go-mod \
    --env npm_config_cache=/cache/npm \
    --env APPIMAGE_BUILD_ENV="$image" \
    --volume "$repo_root:/src" \
    --volume "$cache:/cache" \
    --workdir /src/client \
    "$image" "$@"
}

step() { printf '\n== %s ==\n' "$1"; }

step 'go vet'
run_in_env go vet ./...

step 'go test -short'
run_in_env go test -short ./...

step 'frontend install'
# Always install from the lockfile: an existing node_modules may be partial.
run_in_env sh -c 'cd frontend && npm ci'

step 'frontend test'
run_in_env sh -c 'cd frontend && npm test'

step 'appimage build'
# Remove the previous artifact so verification can never pass on a stale file.
rm -f "$client_dir/$appimage"
run_in_env /src/client/scripts/build-linux-appimage.sh

step 'baseline check'
run_in_env /src/client/scripts/check-linux-baseline.sh "$appimage"

step 'layout check'
run_in_env python3 tests/test_appimage_layout.py "$appimage"

step 'startup smoke'
run_in_env /src/client/scripts/smoke-linux-appimage.sh "$appimage"

step 'artifact'
ls -lh "$client_dir/$appimage"
cat "$client_dir/build/linux/build-info.json"
