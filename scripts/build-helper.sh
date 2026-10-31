#!/bin/sh
set -eu
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
# The migrator copies this exact known generation before Go adopts its schema.
cmp "$ROOT/scripts/connect-root.sh" "$ROOT/helper/legacy/connect-v3.sh" || { echo 'Update the recognized migration template before building.' >&2; exit 2; }
version=$(cat "$ROOT/VERSION")
printf '%s\n' "$version" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$' || exit 2
mkdir -p "$ROOT/build"
cd "$ROOT/helper"
go build -trimpath -buildvcs=false -ldflags "-s -w -X kobo-tailscale/helper.BuildVersion=$version" -o "$ROOT/build/k-tailscale-helper-host" ./cmd/k-tailscale-helper
CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -trimpath -buildvcs=false -ldflags "-s -w -X kobo-tailscale/helper.BuildVersion=$version" -o "$ROOT/build/k-tailscale-helper" ./cmd/k-tailscale-helper
python3 "$ROOT/scripts/dependency-notices.py" --root "$ROOT"
echo "$ROOT/build/k-tailscale-helper"
