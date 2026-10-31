#!/bin/sh

PATH=/sbin:/bin:/usr/sbin:/usr/bin
export PATH
TS=/usr/local/tailscale-runtime/bin/tailscale
CONNECT=/usr/local/tailscale/connect.sh
LOCK=/tmp/kobo-tailscale-connect.lock
BOOT_PATTERN='/usr/local/tailscale/(on-boot|boot)\.sh'

case "${1-}" in
    logout) exec timeout 30 /usr/local/k-tailscale/helper logout ;;
    install|install-addon) exec timeout 600 /usr/local/k-tailscale/helper "$1" ;;
    restart) sync; exec /sbin/reboot ;;
    prepare) exec timeout 120 /bin/sh /usr/local/tailscale-ui/setup.sh ;;
    connect|signin)
        if [ ! -x "$TS" ] || [ ! -r "$CONNECT" ]; then
            echo 'The working Tailscale installation or connection helper is missing.'
            exit 2
        fi
        exec /bin/sh "$CONNECT" "$1"
        ;;
    disconnect|cancel|stop) ;;
    *) echo 'Unknown Tailscale action.'; exit 2 ;;
esac

# Recover an interrupted root transaction before a new preference change.
if [ -x /usr/local/k-tailscale/helper ]; then
    timeout 90 /usr/local/k-tailscale/helper recover-files || exit $?
fi

# This is the same lock used by the existing NickelMenu connection helper.
# Holding it prevents a different control from starting a new login mid-stop.
if ! mkdir "$LOCK" 2>/dev/null; then
    echo 'A connection attempt is still active. Wait for it to finish, then try again.'
    exit 3
fi
trap 'rmdir "$LOCK" 2>/dev/null' 0
if ! command -v pgrep >/dev/null 2>&1; then
    echo 'Cannot verify pending startup jobs: pgrep is unavailable.'
    exit 4
fi

# The original boot helper detaches its child. Wait for that pending startup
# before stopping the daemon, rather than declaring USB ready too early.
tries=0
while pgrep -f "$BOOT_PATTERN" >/dev/null 2>&1; do
    tries=$((tries + 1))
    if [ "$tries" -ge 30 ]; then
        echo 'Tailscale startup is still pending. Retry when it has finished.'
        exit 4
    fi
    sleep 0.2
done

if pidof tailscaled >/dev/null 2>&1; then
    if [ ! -x "$TS" ]; then
        echo 'The Tailscale command is missing.'
        exit 2
    fi
    timeout 4 "$TS" down 2>&1
    result=$?
    if [ "$result" -ne 0 ] && [ "$1" != stop ]; then
        echo "Disconnect failed (status $result)."
        exit "$result"
    fi
fi

if [ "$1" != stop ]; then
    echo 'Disconnected.'
    exit 0
fi

tries=0
while pidof tailscaled >/dev/null 2>&1; do
    for pid in $(pidof tailscaled); do
        case "$pid" in ''|*[!0-9]*) echo 'Cannot verify the daemon PID.'; exit 4 ;; esac
        if [ "$pid" -le 1 ] || [ "$pid" -eq "$$" ]; then
            echo 'Refusing an unsafe daemon PID.'
            exit 4
        fi
        kill -TERM "$pid" 2>/dev/null || true
    done
    tries=$((tries + 1))
    if [ "$tries" -ge 30 ]; then
        echo 'The daemon is still running. Retry Stop for USB before connecting the cable.'
        exit 4
    fi
    sleep 0.2
done
echo 'Ready for USB.'
exit 0
