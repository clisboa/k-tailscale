#!/bin/sh
PATH=/sbin:/bin:/usr/sbin:/usr/bin
export PATH
cd / || exit 1
echo 'Tailscale storage: /usr/local/tailscale-runtime'
if [ -r /usr/local/tailscale-runtime/ready ]; then echo 'System storage is active.'; else echo 'System-storage migration is incomplete.'; fi
/usr/local/tailscale-runtime/bin/tailscale status 2>&1
exit 0
