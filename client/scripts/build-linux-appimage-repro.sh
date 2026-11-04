#!/bin/sh
# Build the AppImage inside the pinned Ubuntu 24.04 environment so the artifact
# keeps the documented glibc/GTK/WebKit baseline regardless of the host.
set -eu
client_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
# The AppImage bundles the repository-level LICENSE, so the whole repo is
# mounted and the build runs from client/.
repo_root=$(CDPATH= cd -- "$client_dir/.." && pwd)
image=${BUILD_IMAGE:-esp32-monitor-linux-build:24.04}

command -v docker >/dev/null 2>&1 || {
  printf '%s\n' 'Docker is required for the reproducible build; use build-appimage-linux for a host build.' >&2
  exit 1
}

if [ "${FORCE_IMAGE_BUILD:-0}" = "1" ] || ! docker image inspect "$image" >/dev/null 2>&1; then
  docker build -t "$image" "$client_dir/packaging/linux/build-env"
fi

# Caches and outputs live in the bind mount, so they must be owned by the
# caller. The container runs as the caller's UID to keep it that way.
cache=$(mktemp -d "${TMPDIR:-/tmp}/esp32-appimage-cache.XXXXXXXX")
trap 'rm -rf "$cache"' EXIT HUP INT TERM
mkdir -p "$cache/go-mod" "$cache/go-build" "$cache/npm" "$client_dir/build/linux"

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
  "$image" \
  /src/client/scripts/build-linux-appimage.sh "$@"
