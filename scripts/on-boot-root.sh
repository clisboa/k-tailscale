#!/bin/sh
PATH=/sbin:/bin:/usr/sbin:/usr/bin
export PATH
umask 077
cd / || exit 1
RUNTIME=/usr/local/tailscale-runtime
if [ "$(cat "$RUNTIME/ready" 2>/dev/null)" != storage-v1 ]; then
    # A pre-commit interruption retains the original boot configuration.
    [ -r "$RUNTIME/backup/on-boot.sh" ] || exit 1
    exec /bin/sh "$RUNTIME/backup/on-boot.sh"
fi
[ -x "$RUNTIME/bin/tailscaled" ] && [ -s "$RUNTIME/state/tailscaled.state" ] || exit 1
if [ ! -c /dev/net/tun ]; then
    mkdir -p /dev/net || exit 1
    mknod /dev/net/tun c 10 200 || exit 1
fi
if pidof tailscaled >/dev/null 2>&1; then exit 0; fi
mkdir -p /var/run/tailscale || exit 1
"$RUNTIME/bin/tailscaled" --statedir="$RUNTIME/state" --socket=/var/run/tailscale/tailscaled.sock </dev/null >/dev/null 2>&1 &
exit 0
