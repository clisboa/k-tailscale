#!/bin/sh
set -eu
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
docker build --platform linux/amd64 -t kobo-tailscale-ui-arm -f "$ROOT/scripts/Dockerfile" "$ROOT"
container=$(docker create --platform linux/amd64 kobo-tailscale-ui-arm /bin/true)
trap 'docker rm -f "$container" >/dev/null 2>&1 || true' 0
mkdir -p "$ROOT/build"
docker cp "$container:/src/build/libtailscaleui.so" "$ROOT/build/libtailscaleui.so"
echo "$ROOT/build/libtailscaleui.so"
