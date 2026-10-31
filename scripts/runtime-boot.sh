#!/bin/sh
PATH=/sbin:/bin:/usr/sbin:/usr/bin
export PATH
umask 077
cd / || exit 1
if [ ! -c /dev/net/tun ]; then
    mkdir -p /dev/net || exit 1
    mknod /dev/net/tun c 10 200 2>/dev/null || [ -c /dev/net/tun ] || exit 1
fi
exec timeout 60 /usr/local/k-tailscale/helper service-start
