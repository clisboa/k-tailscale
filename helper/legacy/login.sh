#!/bin/sh
if [ -r /tmp/kobo-tailscale/auth.log ]; then
    tail -n 14 /tmp/kobo-tailscale/auth.log
else
    echo 'Choose Connect to generate a login link if sign-in is needed.'
fi
exit 0
