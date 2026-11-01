#!/usr/bin/env python3
"""Build deterministic Kobo archives from explicitly named addon files only."""
import argparse
import gzip
import hashlib
import io
from pathlib import Path
import tarfile
import json
import subprocess
import shutil

from package_scope import LIB, STATE, PAYLOAD, UI, HELPERS, TUN, TUN_LICENSE, core, release_version



def archive(path, entries):
    with path.open('wb') as output:
        with gzip.GzipFile(filename='', mode='wb', fileobj=output, mtime=0) as compressed:
            with tarfile.open(fileobj=compressed, mode='w', format=tarfile.USTAR_FORMAT) as tar:
                for name, mode, data in sorted(entries):
                    member = tarfile.TarInfo(name)
                    member.size = len(data)
                    member.mode = mode
                    member.uid = member.gid = member.mtime = 0
                    member.uname = member.gname = ''
                    tar.addfile(member, io.BytesIO(data))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root', type=Path, default=Path(__file__).resolve().parents[1])
    parser.add_argument('--output', type=Path)
    parser.add_argument('--upstream', type=Path, help='Verified public artifact JSON from fetch-release')
    args = parser.parse_args()
    root = args.root.resolve()
    output = args.output or root / 'dist'
    output.mkdir(parents=True, exist_ok=True)
    version = release_version((root/'VERSION').read_text())
    manifest = args.upstream or root/'build/upstream/artifact.json'
    upstream = json.loads(manifest.read_text())
    artifact = Path(upstream['path'])
    verifier = root/'build/k-tailscale-helper-host'
    result = subprocess.run([str(verifier), 'verify-payload', '--manifest', str(manifest),
                             '--archive', str(artifact)], capture_output=True, text=True, timeout=90)
    if result.returncode:
        raise ValueError('Official Tailscale signature verification failed: '+result.stderr[:500])
    verified = json.loads(result.stdout)
    data = artifact.read_bytes()
    public = {key: verified[key] for key in ['version', 'expanded', 'files', 'proof']}
    public.update(size=len(data), sha256=hashlib.sha256(data).hexdigest())
    module = root/'build/tun.ko'
    subprocess.run([str(verifier),'verify-tun-module','--module',str(module)],check=True,
                   capture_output=True,text=True,timeout=30)
    library = (root / 'build/libtailscaleui.so').read_bytes()
    install = [(LIB, 0o755, library)]
    for name in HELPERS:
        install.append((UI+name,0o755,(root/'scripts'/name).read_bytes()))
    install.extend([(UI+'release-version',0o644,(version+'\n').encode()),
                    (core(version)+'helper',0o755,(root/'build/k-tailscale-helper').read_bytes()),
                    (core(version)+'THIRD_PARTY_NOTICES.txt',0o644,(root/'build/THIRD_PARTY_NOTICES.txt').read_bytes())])
    update = list(install)
    install.extend([(PAYLOAD+'release.tgz',0o644,data),
                    (PAYLOAD+'release.json',0o644,(json.dumps(public,sort_keys=True)+'\n').encode())])
    install.extend([(TUN,0o644,module.read_bytes()),(TUN_LICENSE,0o644,(root/'kernel/COPYING').read_bytes())])
    for source, target in [('README.md', 'README.txt'), ('LICENSE', 'LICENSE'), ('NOTICE', 'NOTICE')]:
        install.append((STATE + target, 0o644, (root / source).read_bytes()))
    archive(output / 'KoboRoot.tgz', install)
    archive(output / 'k-tailscale-update.tgz', update)
    archive(output / 'KoboRoot-uninstall.tgz', [(LIB, 0o755, library), (STATE+'uninstall', 0o644, b'')])
    shutil.copyfile(root/'build/k-tailscale-tun-source.tar.gz',output/'k-tailscale-tun-source.tar.gz')
    names = ['KoboRoot.tgz', 'KoboRoot-uninstall.tgz', 'k-tailscale-update.tgz', 'k-tailscale-tun-source.tar.gz']
    (output / 'SHA256SUMS').write_text(''.join(
        hashlib.sha256((output / name).read_bytes()).hexdigest() + '  ' + name + '\n' for name in names))
    print(output)


if __name__ == '__main__':
    main()
