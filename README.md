# k-tailscale

Tailscale on a Kobo Libra Colour, with a native toolbar widget for connecting, signing in and installing updates. Inspired by [Relio](https://github.com/andronedev/relio).

**Supported:** Kobo Libra Colour (model 390), firmware **4.46.23852**, Qt 5.2.1, ARMv7 hard float. Other devices and firmware are refused by the installer. NickelMenu is optional.

## Install

Use the [guided installer](https://clisboa.github.io/k-tailscale/) in Chrome or Edge, or download `KoboRoot.tgz` from [Releases](https://github.com/clisboa/k-tailscale/releases/latest).

1. Connect the Kobo by USB and tap **Connect** on its screen.
2. Select its `KOBOeReader` drive in the installer and choose **Install k-tailscale**. For a manual install, copy `KoboRoot.tgz` into the drive's hidden `.kobo` folder.
3. Safely eject the drive, unplug, and let the Kobo restart. Wait for **Preparing Tailscale** to finish.
4. Open the nine-dot icon beside Search and tap **Sign in** or **Connect**. Scan the sign-in QR with your phone, or open the complete text link on your phone or computer.

Use one installer at a time. An existing `.kobo/KoboRoot.tgz` is a pending update: finish or resolve it before installing this addon. The browser checks model/firmware, download SHA256 and copied-file SHA256. It preserves uncertain failed writes for inspection; browser APIs cannot exclude simultaneous writers in desktop applications. It does not automatically eject the drive or upload books/device credentials.

The complete package includes a signature-verified official stable Tailscale release and the helper. Fresh installation needs roughly **120 MiB free on system storage**, in addition to the already installed addon. Setup checks the actual required space before copying. A new install asks you to sign in; an existing recognized installation preserves its identity and connection preference. Unknown/custom layouts are retained and reported.

When upgrading from the original books-storage installation, use its old **Stop for USB** once before step 1. Subsequent USB transfers need no stop button. The original onboard copy is retained for recovery but never copied over current root state again.

## Use and updates

- Tap the nine-dot toolbar icon to open **K-Tailscale**, a compact dropdown. The connected icon fills five dots in a **T**. Tap the icon again or tap outside to dismiss.
- **Sign in** explicitly requests interactive authentication, including when an old node key remains, and shows its QR and fallback link. **Connect** resumes a saved login; **Disconnect / Cancel** control the connection. The widget keeps the Kobo's reported name and shows its Tailscale IP only while connected. Status refreshes automatically. Disconnect retains the running daemon for status queries.
- **Sign out** ends the current login after an inline confirmation. **Cancel** or tapping outside leaves you signed in. After signing out, tap **Sign in** to obtain a fresh QR and fallback link; Disconnect keeps your login.
- The default name is `kobo-libra-colour`, explicitly set by the connection helper. The widget displays the hostname reported by Tailscale.
- Sign in uses a 15-second CLI wait for completing authentication; this is a timeout, not a delay before the QR. The QR appears as soon as Tailscale provides its URL and stays usable while you finish on your phone. Without a current link, a failed attempt shows its real diagnostic instead of a preparation acknowledgement. Normal saved-login Connect retains its 90-second connection timeout.
- When sign-in is required, a QR code opens the current login URL on your phone. The complete text URL stays visible; long links that cannot form a readable QR use text alone. QR generation runs locally in memory.
- Library sync works with a server reachable through your tailnet, such as BookOrbit or Calibre-Web. Configure that service separately.
- **Updates** expands below the connection controls and shows two independent channels. Tailscale follows the latest **official upstream stable** release directly. The toolbar addon follows this project's signed releases.
- Checks run automatically while the UI is visible and Wi-Fi is available, at most once every six hours, with a 30-minute retry after an unsuccessful check. **Check now** bypasses that interval. Checks never install software or turn Wi-Fi on.
- Tap **Install Tailscale update** or **Install addon update** explicitly. Installation requires Wi-Fi and at least 20% battery or charging. Addon updates require an explicit **Restart Kobo** afterward.

Tailscale updates verify official distribution signatures, validate both ARM executables and preflight disk space. The helper checkpoints quiescent state, swaps the complete executable directory, then checks the new daemon's LocalAPI/version. A failed start restores the previous pair and immutable state checkpoint. Disconnected preference is preserved. An intentionally stopped daemon stays stopped; its new version is validated on the next start. Durable journals recover interrupted activation or rollback.

Addon updates use an independent Ed25519 public key, a signed size/hash/version manifest and an exact file whitelist. Updates write only the addon's own system files; they never replace the runtime's Tailscale binaries or identity. Interrupted file replacement restores immutable backups. The current core helper stays active until successful setup after a restart promotes the new version.

All downloads, staging, state and recovery journals live on system storage. Books storage is used only for the initial USB installer and a closed setup report. Nickel normally turns Wi-Fi off during USB drive mode; the daemon reconnects when Wi-Fi returns if its saved preference is connected.

## Recovery and removal

An error appears in the panel with **Retry setup** when preparation fails. The closed report `.adds/tailscale-ui/migration-status.txt` is readable over USB. Active logs use private RAM storage under `/tmp/kobo-tailscale/`; private daemon state stays in `/usr/local/tailscale-runtime/state/`. Never share that state directory.

The plugin renames itself to `libtailscaleui.so.failsafe` for its first 20 seconds. If Nickel crashes before restoration, the toolbar remains disabled on the next boot. Reinstall the complete USB archive to retry. You can also create `.adds/tailscale-ui/disable-plugin` over USB and restart to disable the toolbar, then delete that marker and restart to re-enable it.

To remove the toolbar, copy `KoboRoot-uninstall.tgz` to `.kobo/KoboRoot.tgz`, safely eject and unplug. Removal retains Tailscale, its active boot helpers, identity and library configuration. The independent core helper is retained because the runtime needs it. Onboard documentation/reports may remain.

A killed connection/update action can leave a lock in RAM. Restart before retrying. An unexpected/custom system layout is refused rather than overwritten. System storage must have room for both the previous and candidate release plus download/checkpoint/reserve; the panel reports required/available space.

## Build and validation

A Linux Docker daemon, Go matching `helper/go.mod`, Node.js 24+, Python 3 and OpenSSL with Ed25519 support are required. Qt native tests need Qt 5 development packages, make, a C++11 compiler, procps, coreutils and zbar-tools (independent QR decoding). `tests/Dockerfile` supplies native dependencies. NickelTC GCC/Qt 5.2.1 is pinned in `scripts/Dockerfile`.

```sh
go -C helper test -race ./...
go -C helper vet ./...
make test
python3 -m unittest discover -s tests -p 'test_*.py' -v
node --test installer/drive.test.mjs
scripts/build.sh
scripts/build-helper.sh
mkdir -p build/upstream
build/k-tailscale-helper-host fetch-release --directory "$PWD/build/upstream" > build/upstream/artifact.json
python3 scripts/package.py
python3 scripts/verify-package.py dist/KoboRoot.tgz
python3 scripts/verify-package.py --kind update dist/k-tailscale-update.tgz
python3 scripts/verify-package.py --kind remove dist/KoboRoot-uninstall.tgz
python3 scripts/build-site.py
```

Archives have deterministic metadata, fixed ownership/modes and SHA256 sums. Tests cover native UI/processes, Unix sockets, signature verification, archive scope, installation, power-loss recovery and identity retention. USB installation, storage migration and the base compact widget have been confirmed on a Libra Colour. Interactive QR sign-in in v0.1.6 and upstream HTTPS checks without certificate errors in v0.1.7 have also been confirmed on the Kobo. Native previews use Qt 5.15; the production build targets Qt 5.2.1.

Version0.1.8 is the first public release at `clisboa/k-tailscale`. Earlier local builds through0.1.7 use a different addon-feed URL and need this complete USB installer once to adopt the public release feed.

The updater embeds a Mozilla TLS CA bundle so release checks and downloads work even when the Kobo system CA store is missing or outdated. Both release feeds share this trust store, while normal HTTPS hostname and expiry checks remain enabled. The CA snapshot is refreshed in addon releases independently of Tailscale binary updates; see `helper/ca/README.md` for provenance.

Tailscale's distribution-signature verification library and public signing-root bundle are pinned to v1.104.4. The latest upstream stable software remains independently downloadable; a future incompatible signing protocol/root rollover requires an addon update. See [RELEASING.md](RELEASING.md) for publishing/signing and [NOTICE](NOTICE) for attribution and licenses. This is an independent project, unaffiliated with Kobo or Tailscale.
