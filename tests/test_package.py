import gzip
import hashlib
import io
from pathlib import Path
import struct
import subprocess
import sys
import tarfile
import tempfile
import unittest
import base64
import json
import shutil

PROJECT = Path(__file__).resolve().parents[1]
LIB = 'usr/local/Kobo/imageformats/libtailscaleui.so'
CONTROL = 'usr/local/tailscale-ui/control.sh'
HELPERS = ['control.sh', 'migrate.sh', 'on-boot-root.sh', 'connect-root.sh', 'login-root.sh', 'diagnose-root.sh', 'setup.sh', 'runtime-boot.sh']
STATE = 'mnt/onboard/.adds/tailscale-ui/'
CORE = 'usr/local/k-tailscale/releases/0.1.0/'
PAYLOAD = 'mnt/onboard/.adds/k-tailscale/install/'


def elf():
    data = bytearray(80)
    data[:7] = b'\x7fELF\x01\x01\x01'
    struct.pack_into('<HHI', data, 16, 3, 40, 1)
    struct.pack_into('<I', data, 36, 0x05000400)
    return bytes(data) + b'QTMETADATA qt_plugin_query_metadata qt_plugin_instance tailscale-ui'


class Packages(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        (self.root / 'build').mkdir()
        (self.root / 'scripts').mkdir()
        (self.root / 'build/libtailscaleui.so').write_bytes(elf())
        (self.root/'VERSION').write_text('0.1.0\n')
        helper=bytearray(elf()+b'kobo-tailscale/helper')
        struct.pack_into('<H',helper,16,2)
        (self.root/'build/k-tailscale-helper').write_bytes(helper)
        (self.root/'build/THIRD_PARTY_NOTICES.txt').write_text('Fixture dependency notices\n')
        upstream=self.root/'build/upstream';upstream.mkdir()
        with tarfile.open(upstream/'verified-1.104.4.tgz','w:gz') as archive:
            for name in ['tailscale','tailscaled']:
                info=tarfile.TarInfo('tailscale_1.104.4_arm/'+name);info.size=len(helper);info.mode=0o755
                archive.addfile(info,io.BytesIO(helper))
        proof={'keys':base64.b64encode(b'fixture signing bundle').decode(),
               'keys_signature':base64.b64encode(bytes(64)).decode(),
               'package_signature':base64.b64encode(bytes(64)).decode()}
        metadata={'path':str(upstream/'verified-1.104.4.tgz'),'version':'1.104.4',
                  'expanded':len(helper)*2,'files':{n:len(helper) for n in ['tailscale','tailscaled']},'proof':proof}
        (upstream/'artifact.json').write_text(json.dumps(metadata))
        # The package layer depends on a host verifier; its actual crypto is
        # exercised by the helper's signed-server and real upstream tests.
        self.verifier=self.root/'build/k-tailscale-helper-host'
        self.verifier.write_text("#!/bin/sh\ncat <<'RESULT'\n"+json.dumps(metadata)+"\nRESULT\n")
        self.verifier.chmod(0o755)
        for name in HELPERS:
            (self.root / 'scripts' / name).write_text('#!/bin/sh\necho Ready\n')
        for name in ('README.md', 'LICENSE', 'NOTICE'):
            (self.root / name).write_text(name + '\n')
        self.dist = self.root / 'dist'

    def package(self):
        subprocess.run([sys.executable, str(PROJECT / 'scripts/package.py'),
                        '--root', str(self.root), '--output', str(self.dist)], check=True,
                       stdout=subprocess.PIPE, stderr=subprocess.PIPE)

    def verify(self, name='KoboRoot.tgz', kind='install'):
        return subprocess.run([sys.executable, str(PROJECT / 'scripts/verify-package.py'),
                               '--helper',str(self.verifier),'--kind', kind, str(self.dist / name)],
                              capture_output=True, text=True)

    def rewrite(self, change):
        path = self.dist / 'KoboRoot.tgz'
        with tarfile.open(path, 'r:gz') as archive:
            entries = [(info, archive.extractfile(info).read()) for info in archive.getmembers()]
        entries = change(entries)
        with tarfile.open(path, 'w:gz') as archive:
            for info, data in entries:
                archive.addfile(info, io.BytesIO(data) if info.isfile() else None)

    def test_deterministic_contents_permissions_and_checksums(self):
        self.package()
        first = {p.name: p.read_bytes() for p in self.dist.iterdir()}
        self.package()
        self.assertEqual(first, {p.name: p.read_bytes() for p in self.dist.iterdir()})
        with tarfile.open(self.dist / 'KoboRoot.tgz') as archive:
            members = archive.getmembers()
            expected={LIB,STATE+'README.txt',STATE+'LICENSE',STATE+'NOTICE'}
            expected.update('usr/local/tailscale-ui/'+name for name in HELPERS)
            expected.update({'usr/local/tailscale-ui/release-version',CORE+'helper',CORE+'THIRD_PARTY_NOTICES.txt',PAYLOAD+'release.tgz',PAYLOAD+'release.json'})
            self.assertEqual({m.name for m in members}, expected)
            self.assertTrue(all(m.isfile() and m.uid == m.gid == m.mtime == 0 for m in members))
            self.assertEqual({m.name: m.mode for m in members}[LIB], 0o755)
            self.assertEqual({m.name: m.mode for m in members}[CONTROL], 0o755)
        for line in (self.dist / 'SHA256SUMS').read_text().splitlines():
            digest, name = line.split('  ')
            self.assertEqual(digest, hashlib.sha256((self.dist/name).read_bytes()).hexdigest())
        self.assertEqual(self.verify().returncode, 0)
        with tarfile.open(self.dist/'KoboRoot.tgz') as archive:
            for member in archive.getmembers():
                self.assertNotIn(str(self.root).encode(),archive.extractfile(member).read())
        with tarfile.open(self.dist/'k-tailscale-update.tgz') as archive:
            self.assertTrue(all(name.startswith('usr/') for name in archive.getnames()))
            self.assertNotIn(PAYLOAD+'release.tgz',archive.getnames())
        self.assertEqual(self.verify('k-tailscale-update.tgz','update').returncode,0)

    def test_removal_only_installs_cleanup_plugin_and_marker(self):
        self.package()
        with tarfile.open(self.dist / 'KoboRoot-uninstall.tgz') as archive:
            self.assertEqual(set(archive.getnames()), {LIB, STATE+'uninstall'})
            self.assertEqual(archive.extractfile(STATE+'uninstall').read(), b'')
        self.assertEqual(self.verify('KoboRoot-uninstall.tgz', 'remove').returncode, 0)

    def test_installer_site_uses_verified_archive_and_public_metadata(self):
        self.package()
        shutil.copytree(PROJECT/'installer',self.root/'installer')
        result=subprocess.run([sys.executable,str(PROJECT/'scripts/build-site.py'),'--root',str(self.root)],capture_output=True,text=True)
        self.assertEqual(result.returncode,0,result.stdout+result.stderr)
        site=self.dist/'site';metadata=json.loads((site/'install.json').read_text())
        self.assertEqual(metadata['sha256'],hashlib.sha256((site/'KoboRoot.tgz').read_bytes()).hexdigest())
        self.assertEqual(metadata['version'],'0.1.0')
        self.assertEqual(metadata['tailscale'],'1.104.4')
        self.assertNotIn(str(self.root),json.dumps(metadata))
        self.assertFalse(list(site.glob('*.test.*')))

    def test_rejects_path_traversal(self):
        self.package()
        def change(entries):
            entries[0][0].name = '../etc/passwd'
            return entries
        self.rewrite(change)
        self.assertNotEqual(self.verify().returncode, 0)

    def test_rejects_links_and_wrong_permissions(self):
        self.package()
        def change(entries):
            entries[0][0].type = tarfile.SYMTYPE
            entries[0][0].linkname = '/etc/passwd'
            return entries
        self.rewrite(change)
        self.assertNotEqual(self.verify().returncode, 0)
        self.package()
        self.rewrite(lambda entries: [(self.wrong_mode(m), d) for m, d in entries])
        self.assertNotEqual(self.verify().returncode, 0)

    @staticmethod
    def wrong_mode(info):
        if info.name == CONTROL:
            info.mode = 0o777
        return info

    def test_rejects_duplicates_credentials_and_wrong_architecture(self):
        self.package()
        self.rewrite(lambda entries: entries + [entries[0]])
        self.assertNotEqual(self.verify().returncode, 0)
        (self.root / 'scripts/control.sh').write_text('tskey-auth-secret-test-fixture')
        self.package()
        self.assertNotEqual(self.verify().returncode, 0)
        (self.root / 'scripts/control.sh').write_text('#!/bin/sh\n')
        (self.root / 'build/libtailscaleui.so').write_bytes(b'not ARM ELF')
        self.package()
        self.assertNotEqual(self.verify().returncode, 0)


if __name__ == '__main__':
    unittest.main()
