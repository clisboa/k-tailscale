#!/bin/sh
PATH=/sbin:/bin:/usr/sbin:/usr/bin
export PATH
umask 077
cd / || exit 1
RUNTIME=/usr/local/tailscale-runtime
LOGDIR=/tmp/kobo-tailscale
LOCK=/tmp/kobo-tailscale-connect.lock
[ "$(cat "$RUNTIME/ready" 2>/dev/null)" = storage-v1 ] || { echo 'System-storage preparation is incomplete.'; exit 2; }
if [ -x /usr/local/k-tailscale/helper ]; then
    timeout 90 /usr/local/k-tailscale/helper recover-files || exit $?
fi
mkdir "$LOCK" 2>/dev/null || { echo 'Another Tailscale action is active.'; exit 3; }
trap 'rmdir "$LOCK" 2>/dev/null' 0
trap 'exit 143' HUP INT TERM
mkdir -p "$LOGDIR" || exit 1
# Keep a bounded, per-attempt log in RAM. The toolbar reads status from LocalAPI.
exec > "$LOGDIR/auth.log" 2>&1
if ! pidof tailscaled >/dev/null 2>&1; then
    /bin/sh /usr/local/tailscale/boot.sh || exit 1
fi
tries=0
while [ ! -S /var/run/tailscale/tailscaled.sock ]; do
    tries=$((tries+1));[ "$tries" -lt 60 ] || { echo 'Tailscale did not start.'; exit 4; }
    sleep 0.1
done
tries=0
while ! awk '$1 ~ /^wlan/ && $2 == "00000000" && $3 != "00000000" { ready=1 } END { exit !ready }' /proc/net/route; do
    tries=$((tries+1));[ "$tries" -lt 60 ] || { echo 'Connect Wi-Fi, then retry Connect.'; exit 4; }
    sleep 1
done
"$RUNTIME/bin/tailscale" up --accept-dns=false --hostname=kobo-libra-colour --timeout=90s
result=$?
if [ "$result" -ne 0 ]; then echo "Tailscale Connect failed (status $result)."; fi
exit "$result"
