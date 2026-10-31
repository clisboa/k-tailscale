# Updater TLS CA bundle

`cacert.pem` is the unchanged Mozilla root CA store converted to PEM by curl.
It is embedded in the helper and used only by the HTTPS updater client, shared
by Tailscale and addon release checks/downloads. It does not modify the Kobo's
system trust store. Distribution-signature keys in `helper/roots` and the addon
signing public key remain separate from this TLS trust store.

- Source: https://curl.se/ca/cacert.pem
- Published checksum: https://curl.se/ca/cacert.pem.sha256
- Extraction information: https://curl.se/docs/caextract.html
- Mozilla certificate data as of: 2026-10-20 13:32:40 GMT
- Retrieved and checksum-verified: 2026-10-31
- SHA256 of this complete PEM file: `796f1389bfea13c0ed78f663b765baaff54a508e5483786bd6ece73c5e3e3cbe`
- Size: 200116 bytes; 118 certificates.
- License: Mozilla Public License 2.0, included in `MPL-2.0.txt`.
- License source: https://www.mozilla.org/MPL/2.0/

The SHA256 in the PEM header identifies Mozilla's source data; the complete-file
digest above is the one checked against curl's published checksum. Refresh the
bundle as a reviewed source change before addon releases (see `RELEASING.md`).
There is no runtime trust-anchor download, and updating the Tailscale binary
alone does not refresh this helper's CA bundle. The original Mozilla source is
linked in the preserved PEM header. Public source releases include this file;
installed THIRD_PARTY_NOTICES.txt includes this provenance and the full MPL.
