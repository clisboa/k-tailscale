"""Shared, explicit public package scope. No live device files are inputs."""
import re

LIB = 'usr/local/Kobo/imageformats/libtailscaleui.so'
STATE = 'mnt/onboard/.adds/tailscale-ui/'
PAYLOAD = 'mnt/onboard/.adds/k-tailscale/install/'
UI = 'usr/local/tailscale-ui/'
TUN = 'mnt/onboard/.adds/k-tailscale/kernel/clara-hd/tun.ko'
TUN_LICENSE = 'mnt/onboard/.adds/k-tailscale/kernel/clara-hd/COPYING'
HELPERS = ['control.sh', 'migrate.sh', 'on-boot-root.sh', 'connect-root.sh',
           'login-root.sh', 'diagnose-root.sh', 'setup.sh', 'runtime-boot.sh']
VERSION_PATTERN = re.compile(r'(0|[1-9][0-9]{0,5})\.(0|[1-9][0-9]{0,5})\.(0|[1-9][0-9]{0,5})\Z')


def release_version(text):
    value = text.strip()
    if not VERSION_PATTERN.fullmatch(value):
        raise ValueError('Invalid project release version')
    return value


def core(version):
    return 'usr/local/k-tailscale/releases/'+version+'/'


def expected(version, kind):
    if kind == 'remove':
        return {LIB: 0o755, STATE+'uninstall': 0o644}
    entries = {LIB: 0o755, UI+'release-version': 0o644,
               core(version)+'helper': 0o755, core(version)+'THIRD_PARTY_NOTICES.txt': 0o644}
    entries.update({UI+name: 0o755 for name in HELPERS})
    if kind == 'install':
        entries.update({STATE+name: 0o644 for name in ['README.txt', 'LICENSE', 'NOTICE']})
        entries.update({PAYLOAD+'release.tgz': 0o644, PAYLOAD+'release.json': 0o644, TUN: 0o644, TUN_LICENSE: 0o644})
    return entries
