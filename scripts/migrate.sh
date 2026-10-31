#!/bin/sh
# Migrate this installation without parsing or changing its private state.
PATH=/sbin:/bin:/usr/sbin:/usr/bin
export PATH
umask 077
cd / || exit 1
SOURCE=/mnt/onboard/tailscale
RUNTIME=/usr/local/tailscale-runtime
BASE=/usr/local/tailscale
PAYLOAD=/usr/local/tailscale-ui
MENU=/mnt/onboard/.adds/nm/tailscale
CLI_LINK=/usr/bin/tailscale
DAEMON_LINK=/usr/bin/tailscaled
LOCK=/tmp/kobo-tailscale-connect.lock
BOOT_PATTERN='/usr/local/tailscale/(on-boot|boot)\.sh'
STAGE=$RUNTIME.new

fail() { echo "$1"; exit 4; }
valid_runtime() {
    [ "$(cat "$RUNTIME/ready" 2>/dev/null)" = storage-v1 ] &&
    [ -x "$RUNTIME/bin/tailscale" ] && [ -x "$RUNTIME/bin/tailscaled" ] &&
    [ -s "$RUNTIME/state/tailscaled.state" ] &&
    [ "$(readlink "$CLI_LINK")" = "$RUNTIME/bin/tailscale" ] &&
    [ "$(readlink "$DAEMON_LINK")" = "$RUNTIME/bin/tailscaled" ] || return 1
    cmp -s "$BASE/on-boot.sh" "$PAYLOAD/on-boot-root.sh" || return 1
    for name in connect login diagnose; do
        cmp -s "$BASE/$name.sh" "$PAYLOAD/$name-root.sh" || return 1
    done
    return 0
}
[ ! -L "$RUNTIME" ] && [ ! -L "$STAGE" ] || fail 'Unexpected runtime link; no changes made.'
if [ -s "$RUNTIME/ready" ]; then
    valid_runtime || fail 'The system Tailscale runtime needs repair; its saved state was not replaced.'
    echo 'Tailscale already uses system storage.'
    exit 0
fi
awk '$2 == "/mnt/onboard" { mounted=1 } END { exit !mounted }' /proc/mounts || fail 'Books storage is not mounted; no changes made.'
[ -x "$SOURCE/tailscale" ] && [ -x "$SOURCE/tailscaled" ] && [ -s "$SOURCE/tailscaled.state" ] || fail 'The existing binaries or saved identity are missing; no changes made.'
[ -z "$(find "$SOURCE" -type l -print)" ] || fail 'Unexpected links in the existing state directory; no changes made.'
[ -f "$BASE/on-boot.sh" ] && [ -f "$BASE/boot.sh" ] || fail 'The existing boot setup is missing; no changes made.'
for name in on-boot-root connect-root login-root diagnose-root; do
    [ -r "$PAYLOAD/$name.sh" ] || fail 'A migration helper is missing; no changes made.'
done
for command in pgrep cmp df du cp mv readlink; do
    command -v "$command" >/dev/null 2>&1 || fail 'A required migration command is unavailable.'
done
mkdir "$LOCK" 2>/dev/null || fail 'A connection is still active; wait and restart the Kobo to retry migration.'
was_running=0
stopped=0
activated=0
runtime_created=0
committed=0
stage_created=0
restore_copy() {
    restore_source=$1
    restore_target=$2
    restore_kind=$3
    rm -f "$restore_target.restore" || return 1
    if [ "$restore_kind" = link ]; then
        cp -P "$restore_source" "$restore_target.restore" || return 1
    else
        cp -p "$restore_source" "$restore_target.restore" || return 1
    fi
    mv -f "$restore_target.restore" "$restore_target"
}
rollback() {
    restored=1
    for name in on-boot.sh; do
        [ ! -e "$RUNTIME/backup/$name" ] || restore_copy "$RUNTIME/backup/$name" "$BASE/$name" file || restored=0
    done
    [ ! -L "$RUNTIME/backup/cli-link" ] || restore_copy "$RUNTIME/backup/cli-link" "$CLI_LINK" link || restored=0
    [ ! -L "$RUNTIME/backup/daemon-link" ] || restore_copy "$RUNTIME/backup/daemon-link" "$DAEMON_LINK" link || restored=0
    [ ! -f "$RUNTIME/backup/menu" ] || restore_copy "$RUNTIME/backup/menu" "$MENU" file || restored=0
    rm -f "$BASE/on-boot.sh.new" "$CLI_LINK.new" "$DAEMON_LINK.new" "$MENU.new"
    for name in connect login diagnose; do rm -f "$BASE/$name.sh"; done
    sync || restored=0
    [ "$restored" = 1 ]
}
finish() {
    result=$?
    trap - 0
    if [ "$committed" != 1 ]; then
        safe=1
        if [ "$activated" = 1 ]; then rollback || safe=0; fi
        if [ "$stage_created" = 1 ]; then rm -rf "$STAGE"; fi
        if [ "$runtime_created" = 1 ] && [ "$safe" = 1 ]; then rm -rf "$RUNTIME"; fi
        if [ "$was_running" = 1 ] && [ "$stopped" = 1 ] && [ "$safe" = 1 ]; then
            /bin/sh "$BASE/boot.sh" >/dev/null 2>&1
            tries=0
            while ! pidof tailscaled >/dev/null 2>&1 && [ "$tries" -lt 30 ]; do tries=$((tries+1));sleep 0.1;done
        fi
        if [ "$safe" != 1 ]; then echo 'Configuration rollback needs attention; backups remain in the system runtime.'; fi
    fi
    rmdir "$LOCK" 2>/dev/null || true
    exit "$result"
}
trap finish 0
trap 'exit 143' HUP INT TERM
# Only recover directories identified as this installer's interrupted work.
if [ -e "$RUNTIME" ]; then
    [ "$(cat "$RUNTIME/owner" 2>/dev/null)" = storage-v1 ] &&
    [ -f "$RUNTIME/backup/on-boot.sh" ] && [ -L "$RUNTIME/backup/cli-link" ] &&
    [ -L "$RUNTIME/backup/daemon-link" ] || fail 'An unknown runtime exists; it was not changed.'
    for name in connect login diagnose; do
        [ ! -e "$BASE/$name.sh" ] || cmp -s "$BASE/$name.sh" "$PAYLOAD/$name-root.sh" || fail 'A custom helper changed during recovery; it was not removed.'
    done
    activated=1
    runtime_created=1
    rollback || fail 'Cannot recover the interrupted activation; backups were retained.'
    activated=0
    rm -rf "$RUNTIME" || fail 'Cannot clear the recovered runtime.'
    runtime_created=0
fi
if [ -e "$STAGE" ]; then
    [ "$(cat "$STAGE/owner" 2>/dev/null)" = storage-v1 ] || fail 'An unknown staging directory exists; it was not changed.'
    rm -rf "$STAGE" || fail 'Cannot clear the interrupted staging directory.'
fi
grep -F "$SOURCE" "$BASE/on-boot.sh" >/dev/null || fail 'Unrecognized boot setup; no changes made.'
[ "$(readlink "$CLI_LINK")" = "$SOURCE/tailscale" ] && [ "$(readlink "$DAEMON_LINK")" = "$SOURCE/tailscaled" ] || fail 'Unrecognized binary links; no changes made.'
for name in connect login diagnose; do
    [ ! -e "$BASE/$name.sh" ] || fail 'A custom system helper already exists; no changes made.'
done
needed=$(du -sk "$SOURCE" | awk '{print $1}')
available=$(df -Pk "$BASE" | awk 'NR == 2 {print $4}')
case "$needed:$available" in *[!0-9:]*|:*|*:) fail 'Cannot verify free system space; no changes made.' ;; esac
needed=$((needed + 16384))
[ "$available" -ge "$needed" ] || fail "Not enough system space: need $needed KiB, available $available KiB. No changes made."
tries=0
while pgrep -f "$BOOT_PATTERN" >/dev/null 2>&1; do
    tries=$((tries+1));[ "$tries" -lt 30 ] || fail 'Startup is still pending; migration was not activated.'
    sleep 0.2
done
if pidof tailscaled >/dev/null 2>&1; then was_running=1; fi
tries=0
while pidof tailscaled >/dev/null 2>&1; do
    for pid in $(pidof tailscaled); do
        case "$pid" in ''|*[!0-9]*) fail 'Cannot verify daemon PID; migration stopped.' ;; esac
        [ "$pid" -gt 1 ] && [ "$pid" -ne "$$" ] || fail 'Unsafe daemon PID; migration stopped.'
        kill -TERM "$pid" 2>/dev/null || true
    done
    tries=$((tries+1));[ "$tries" -lt 30 ] || fail 'Daemon did not stop; migration was not activated.'
    sleep 0.2
done
stopped=1
mkdir "$STAGE" || fail 'Cannot stage the system runtime.'
stage_created=1
echo storage-v1 > "$STAGE/owner" || fail 'Cannot identify the staged runtime.'
mkdir "$STAGE/bin" "$STAGE/state" "$STAGE/backup" || fail 'Cannot stage the runtime directories.'
cp "$SOURCE/tailscale" "$SOURCE/tailscaled" "$STAGE/bin/" || fail 'Binary copy failed; original setup retained.'
for item in "$SOURCE"/* "$SOURCE"/.[!.]* "$SOURCE"/..?*; do
    [ -e "$item" ] || continue
    case "${item##*/}" in tailscale|tailscaled) continue ;; esac
    cp -R "$item" "$STAGE/state/" || fail 'State copy failed; original setup retained.'
done
find "$SOURCE" -type f > "$STAGE/files" || fail 'Cannot verify the state copy.'
while IFS= read -r file; do
    relative=${file#"$SOURCE"/}
    case "$relative" in tailscale|tailscaled) copy="$STAGE/bin/$relative" ;; *) copy="$STAGE/state/$relative" ;; esac
    cmp -s "$file" "$copy" || fail 'Copy verification failed; original setup retained.'
done < "$STAGE/files"
rm -f "$STAGE/files"
chmod 755 "$STAGE/bin/tailscale" "$STAGE/bin/tailscaled" || fail 'Cannot set binary permissions.'
find "$STAGE/state" -type f -exec chmod 600 {} \; || fail 'Cannot protect saved state.'
find "$STAGE/state" -type d -exec chmod 700 {} \; || fail 'Cannot protect state directories.'
cp -p "$BASE/on-boot.sh" "$STAGE/backup/on-boot.sh" || fail 'Cannot preserve the boot configuration.'
cp -P "$CLI_LINK" "$STAGE/backup/cli-link" || fail 'Cannot preserve the CLI link.'
cp -P "$DAEMON_LINK" "$STAGE/backup/daemon-link" || fail 'Cannot preserve the daemon link.'
if [ -f "$MENU" ]; then cp "$MENU" "$STAGE/backup/menu" || fail 'Cannot preserve the owned menu file.'; fi
echo storage-v1 > "$STAGE/owner"
mv "$STAGE" "$RUNTIME" || fail 'Cannot activate the staged runtime.'
stage_created=0
runtime_created=1
# Backups and their final directory must be durable before changing boot paths.
sync || fail 'Cannot checkpoint the staged runtime; original setup retained.'
activated=1
cp "$PAYLOAD/on-boot-root.sh" "$BASE/on-boot.sh.new" && chmod 755 "$BASE/on-boot.sh.new" && mv -f "$BASE/on-boot.sh.new" "$BASE/on-boot.sh" || fail 'Boot activation failed; restoring the original setup.'
for name in connect login diagnose; do
    cp "$PAYLOAD/$name-root.sh" "$BASE/$name.sh" && chmod 755 "$BASE/$name.sh" || fail 'System helper activation failed; restoring the original setup.'
done
ln -s "$RUNTIME/bin/tailscale" "$CLI_LINK.new" && mv -f "$CLI_LINK.new" "$CLI_LINK" || fail 'CLI link activation failed; restoring the original setup.'
ln -s "$RUNTIME/bin/tailscaled" "$DAEMON_LINK.new" && mv -f "$DAEMON_LINK.new" "$DAEMON_LINK" || fail 'Daemon link activation failed; restoring the original setup.'
if [ -f "$MENU" ]; then
    sed -e 's|/mnt/onboard/tailscale/|/usr/local/tailscale-runtime/bin/|g' \
        -e 's|/mnt/onboard/.adds/kobo-tailscale/connect.sh|/usr/local/tailscale/connect.sh|g' \
        -e 's|/mnt/onboard/.adds/kobo-tailscale/show-login.sh|/usr/local/tailscale/login.sh|g' \
        -e 's|/mnt/onboard/.adds/kobo-tailscale/diagnose.sh|/usr/local/tailscale/diagnose.sh|g' \
        -e '/:Tailscale - Stop for USB:/d' -e '/:Tailscale - Install:/d' -e '/:Tailscale - Install ONCE:/d' \
        "$MENU" > "$MENU.new" && mv -f "$MENU.new" "$MENU" || fail 'Menu activation failed; restoring the original setup.'
fi
sync
echo storage-v1 > "$RUNTIME/ready" || fail 'Cannot record runtime activation.'
sync
committed=1
if [ "$was_running" = 1 ]; then
    /bin/sh "$BASE/boot.sh" >/dev/null 2>&1
    tries=0
    while ! pidof tailscaled >/dev/null 2>&1 && [ "$tries" -lt 30 ]; do tries=$((tries+1));sleep 0.1;done
    if ! pidof tailscaled >/dev/null 2>&1; then
        echo 'Storage migration complete. Daemon startup failed; tap Connect to retry.'
        exit 0
    fi
fi
echo 'Tailscale now uses system storage. The existing identity and connection preference were preserved.'
exit 0
