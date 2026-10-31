#!/bin/sh
PATH=/sbin:/bin:/usr/sbin:/usr/bin
export PATH
umask 077
cd / || exit 1
case "${1-connect}" in
    connect) set --; wait_timeout=90s ;;
    signin) set -- --force-reauth; wait_timeout=15s ;;
    *) echo 'Unknown Tailscale connection action.'; exit 2 ;;
esac
RUNTIME=/usr/local/tailscale-runtime
LOGDIR=/tmp/kobo-tailscale
LOCK=/tmp/kobo-tailscale-connect.lock
[ "$(cat "$RUNTIME/ready" 2>/dev/null)" = storage-v1 ] || { echo 'System-storage preparation is incomplete.'; exit 2; }
if [ -x /usr/local/k-tailscale/helper ]; then
    recovery=$(timeout 90 /usr/local/k-tailscale/helper recover-files 2>&1)
    result=$?
    if [ "$result" -ne 0 ]; then printf '%s\nPreparation failed (status %s).\n' "$recovery" "$result"; exit "$result"; fi
fi
mkdir "$LOCK" 2>/dev/null || { echo 'Another Tailscale action is active.'; exit 3; }
trap 'rmdir "$LOCK" 2>/dev/null' 0
trap 'exit 143' HUP INT TERM
mkdir -p "$LOGDIR" || exit 1
exec 3>&1
exec > "$LOGDIR/auth.log" 2>&1
finish() {
    result=$?
    trap - 0
    if [ "$result" -ne 0 ]; then tail -c 4096 "$LOGDIR/auth.log" | tail -n 14 >&3; fi
    rmdir "$LOCK" 2>/dev/null
    exit "$result"
}
trap finish 0
trap 'exit 143' HUP INT TERM
# The daemon publishes the live auth URL through LocalAPI. Failed attempts also
# return their real, bounded diagnostic to the caller instead of setup's ACK.
if ! pidof tailscaled >/dev/null 2>&1; then
    /bin/sh /usr/local/tailscale/boot.sh 3>&- || exit 1
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
"$RUNTIME/bin/tailscale" up "$@" --accept-dns=false --hostname=kobo-libra-colour --timeout="$wait_timeout" 3>&-
result=$?
if [ "$result" -ne 0 ]; then echo "Tailscale Connect failed (status $result)."; fi
exit "$result"
