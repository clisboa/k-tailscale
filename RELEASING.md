# Releasing

The repository is `clisboa/k-tailscale`. GitHub Pages serves the static installer; GitHub Releases serves the three archives, checksums and signed addon manifest.

Changes to the installer page and its shared widget previews on `main` deploy
through the **Installer page** workflow. It downloads the existing `v$(cat VERSION)`
installer, checks its published SHA256 and verifies its package scope and official
Tailscale signatures before rebuilding the static site. It does not create a
release, change its archives, or use the addon signing key. The workflow can also
be run manually. Keep `VERSION` at a published release for page-only deployments;
an unreleased version is refused by the release download. Pages and release
deployments share a concurrency group to avoid simultaneous publication.

1. Update `VERSION` with a numeric three-part version and run all tests in README.
2. Build the Qt library and host/ARM helper, fetch the current official stable Tailscale artifact with `fetch-release`, and build/verify all archives.
3. Run `python3 scripts/release.py --key /secure/location/addon-signing-key.pem` to sign `dist/update.json`, verify its signature and build `dist/site`. The key must be private (0600), outside the repository, and match `helper/addon-public.pem`.
4. Inspect the installer and native panel previews. Test USB setup and both update paths on a supported Kobo, including disconnected state, low battery, failure recovery and a reboot.
5. Publish a matching `vX.Y.Z` tag. The release workflow verifies the tag, repeats tests/build/signing, creates the release and deploys Pages.

Configure Pages to use GitHub Actions and add the private Ed25519 PEM as the repository/environment Actions secret `ADDON_SIGNING_KEY`. Keep a separate offline backup. Never commit the private key, runtime state, authentication links, tokens, device backups or local work/cache files. The public key is intentionally committed and embedded.

The initial USB archive bundles current official Tailscale, whose signature is checked during build and again during setup. Later addon archives omit that payload. Updating the addon never implicitly updates Tailscale. A release that raises `min_helper` beyond an installed helper requires the complete USB installer.

The current addon trust key has no online rollover mechanism. To replace it, distribute a reviewed complete USB installer with the new public key; a compromised project key must not authorize its own replacement. Upstream Tailscale trust roots are separate and come from its pinned official verification library.

The test and release workflows run `scripts/test-linux-root.sh` in addition to ordinary Go race/vet checks. This builds the helper tests and runs the full binary as root in a disposable Linux container with networking disabled, a read-only root filesystem and a writable temporary test directory. The factory ownership regressions in `helper/ownership_linux_test.go` reproduce the official firmware metadata without changing the host filesystem; ordinary non-root runs skip those cases.

Before an addon release, refresh the embedded updater TLS roots from curl's
Mozilla bundle at https://curl.se/ca/cacert.pem and fetch its published checksum
at https://curl.se/ca/cacert.pem.sha256 using verified HTTPS on the build host.
Compare the complete file's SHA256 before replacing `helper/ca/cacert.pem`;
review the CA additions/removals, retain the PEM header, and update the date,
certificate count, size and digest in `helper/ca/README.md`. Keep the full MPL2.0
license and verify the generated THIRD_PARTY_NOTICES.txt contains it. Commit the
bundle update as a source change; do not fetch trust anchors on the Kobo.
Run the offline empty-system-store regression in Linux and explicitly enable
`KTAILSCALE_TEST_PUBLIC_TLS=1 go test -run '^TestUpdaterPublicTLS$' -v` for a live
release check without system CAs. Normal tests do not contact public services.
The CA snapshot is delivered in addon updates independently of Tailscale binary
updates; a Tailscale-only update does not refresh these TLS roots.

## Clara kernel dependency

Run `scripts/build-tun.sh` before packaging. It verifies Kobo's pinned source,
uses the pinned NickelTC toolchain and factory configuration, and requires the
34 KiB module to reproduce the manifest hash. The full USB package includes the
public module and GPL license; addon-only updates retain the installed driver.
The system helper authenticates RAM copies and uses normal kernel ABI checks.

Publish `dist/k-tailscale-tun-source.tar.gz` alongside every release containing
the module. It contains all corresponding source and the exact configuration
and recipe. `scripts/release.py` verifies the complete source-bundle scope and
input hashes before signing, and `SHA256SUMS` covers this additional asset. Do
not include a built kernel image in any installer or release. The release
workflow builds and publishes this asset automatically.
