#!/bin/sh
# Run ownership-sensitive regressions as root only inside disposable Linux.
set -eu
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
mkdir -p "$ROOT/build"
test_dir=$(mktemp -d "$ROOT/build/root-test.XXXXXX")
image=
cleanup() {
  if [ -n "$image" ]; then docker image rm "$image" >/dev/null 2>&1 || true; fi
  rm -rf "$test_dir"
}
trap cleanup 0
CGO_ENABLED=0 GOOS=linux go -C "$ROOT/helper" test -c -o "$test_dir/helper.test"
cp -R "$ROOT/helper" "$test_dir/helper"
cp -R "$ROOT/scripts" "$test_dir/scripts"
printf '%s\n' 'FROM debian:bookworm-slim' 'COPY helper.test /helper.test' \
  'COPY helper /src/helper' 'COPY scripts /src/scripts' 'WORKDIR /src/helper' \
  'ENTRYPOINT ["/helper.test", "-test.v"]' > "$test_dir/Dockerfile"
image=$(docker build --quiet --network none "$test_dir")
docker run --rm --network none --user 0:0 --read-only \
  --tmpfs /tmp:rw,exec,nosuid,size=256m "$image"
