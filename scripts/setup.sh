#!/bin/sh
PATH=/sbin:/bin:/usr/sbin:/usr/bin
export PATH
umask 077
cd / || exit 1
RUNTIME=/usr/local/tailscale-runtime
UI=/usr/local/tailscale-ui
CORE=/usr/local/k-tailscale
SOURCE=/mnt/onboard/tailscale

# The proven legacy migrator owns only the old books-storage layout.
if [ ! -s "$RUNTIME/ready" ] && [ ! -f "$RUNTIME/installer-owner" ] && [ -s "$SOURCE/tailscaled.state" ]; then
    pending="$UI/legacy-migration-pending"
    if [ -e "$RUNTIME" ] && [ ! -f "$pending" ]; then
        echo 'An incomplete system setup was retained. Its saved identity was not replaced.'
        exit 4
    fi
    if [ -f "$pending" ] && [ -s "$RUNTIME/state/tailscaled.state" ] && ! cmp -s "$RUNTIME/state/tailscaled.state" "$SOURCE/tailscaled.state"; then
        echo 'System state changed during legacy recovery. Both copies were retained.'
        exit 4
    fi
    : > "$pending" || exit 4
    sync || exit 4
    result=$(/bin/sh "$UI/migrate.sh" 2>&1)
    code=$?
    if [ "$code" -ne 0 ]; then printf '%s\n' "$result"; exit "$code"; fi
    rm -f "$pending" || exit 4
    sync || exit 4
fi
# Recover an interrupted addon file transaction through the independent old
# core before selecting a possibly half-written release-version candidate.
if [ -x "$CORE/helper" ]; then
    "$CORE/helper" recover-files || exit $?
fi
version=$(cat "$UI/release-version" 2>/dev/null)
[ "${#version}" -le 32 ] && printf '%s\n' "$version" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$' || { echo 'The release version is missing or invalid.'; exit 2; }
helper="$CORE/releases/$version/helper"
[ -x "$helper" ] || { echo 'The release helper is missing.'; exit 2; }
exec "$helper" prepare
