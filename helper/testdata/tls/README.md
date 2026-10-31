Public TLS certificate chains captured on2026-10-31 using openssl s_client
-connect HOST:443 -servername HOST -showcerts, for pkgs.tailscale.com and
github.com. These are unmodified public leaf/intermediate certificates, with no
private keys. Offline tests verify against the production CA pool using a fixed
time inside the leaf validity interval so routine certificate rotation does not
break the deterministic test. Opt-in live tests also cover current server chains.
