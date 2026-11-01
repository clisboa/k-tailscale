# Optional Clara HD TUN module

Kobo Clara HD firmware 4.38.23852 uses kernel `4.1.15-00136-g12655eaaef89`
with `CONFIG_TUN` disabled. Its configuration is preserved in
`clara-hd.config`. The helper loads this optional module only if TUN is
unavailable on the exact supported kernel. Libra Colour uses its built-in TUN.

The Clara factory configuration also disables `CONFIG_NETFILTER`. The helper
selects Tailscale's `--netfilter-mode=off` for that pinned kernel, and the generated
Clara connection command supplies the same setting. This avoids unsupported
iptables/nftables setup without replacing the kernel. The addon configures an
endpoint connection, not subnet routing or exit-node service; Libra's setting is
unchanged. See https://tailscale.com/docs/reference/netfilter-modes.

The module persists on books storage. Its hash is pinned in the system helper;
it is copied and checked in private RAM before ordinary `insmod`. Kernel ABI
checks stay enabled. A complete USB install supplies the module; addon-only
updates preserve it. No kernel image is installed or flashed.

The source is Kobo's published Linux 4.1.15 tree:
[official source archive](https://github.com/kobolabs/Kobo-Reader/blob/master/hw/imx6sll-clara/kernel.tar.bz2).
`helper/kernels/clara-hd.json` pins its SHA256, factory configuration, toolchain,
module output hash, version magic and module-layout checksum. The only relevant
configuration changes enable TUN as a module and reproduce the existing kernel
release string. The public source lacks the later `CONFIG_TOUCHSCREEN_FTS`
option; that affects an unrelated built-in touchscreen driver. The module build
was compared against 25 factory modules: all 423 shared symbol checksums match.
The device's ordinary module loader performs the remaining checks.

Build on a computer with Python 3.11+ and Docker:

```sh
scripts/build-tun.sh
```

The pinned NickelTC image supplies ARM GCC 4.9.4; signed Debian archive packages
supply the old kernel build's `bc` and `lzop` tools. The build fixes its user,
host, version and timestamp for reproducibility. A kernel image is generated
only as a build intermediate for symbol versions and is never packaged.

The TUN driver and underlying kernel source are licensed under GPL version 2;
see `COPYING`. Every binary release containing the module also supplies
`k-tailscale-tun-source.tar.gz` with the complete unmodified upstream source,
factory configuration, manifest and build scripts. Extract that bundle and run
the same command to reproduce `build/tun.ko` and its corresponding source bundle. The supplied `kernel.tar.bz2` is used directly; no second source download is needed.
