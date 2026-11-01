#!/usr/bin/env python3
"""Reproduce the optional Clara TUN module and its complete source bundle."""
import argparse
import gzip
import hashlib
import json
from pathlib import Path
import shutil
import subprocess
import tarfile
import tempfile
import urllib.request

ROOT = Path(__file__).resolve().parents[1]
SOURCE_FILES = ['kernel/clara-hd.config', 'kernel/COPYING', 'kernel/README.md',
                'kernel/Dockerfile', 'scripts/build-tun.py', 'scripts/build-tun.sh',
                'helper/kernels/clara-hd.json']


def sha(path):
    with path.open('rb') as source:
        return hashlib.file_digest(source, 'sha256').hexdigest()


def source_bundle(root, archive, output):
    with output.open('wb') as target:
        with gzip.GzipFile(filename='', fileobj=target, mode='wb', mtime=0) as gz:
            with tarfile.open(fileobj=gz, mode='w', format=tarfile.USTAR_FORMAT) as tar:
                for name, path in [('kernel.tar.bz2', archive)]+[(p, root/p) for p in SOURCE_FILES]:
                    info = tarfile.TarInfo(name)
                    info.size = path.stat().st_size
                    info.mode = 0o755 if name.startswith('scripts/') else 0o644
                    info.uid = info.gid = info.mtime = 0
                    with path.open('rb') as source:
                        tar.addfile(info, source)


def verify_source_bundle(root, bundle):
    spec = json.loads((root/'helper/kernels/clara-hd.json').read_text())
    expected = {'kernel.tar.bz2': spec['source_sha256']}
    expected.update({name: sha(root/name) for name in SOURCE_FILES})
    seen = set()
    total = 0
    with tarfile.open(bundle, 'r:gz') as archive:
        for info in archive:
            if not info.isfile() or info.name not in expected or info.name in seen:
                raise ValueError('Unexpected corresponding-source archive entry')
            total += info.size
            if info.size < 0 or total > 140*1024*1024:
                raise ValueError('Corresponding-source archive exceeds size limit')
            mode = 0o755 if info.name.startswith('scripts/') else 0o644
            if info.mode != mode or info.uid or info.gid or info.mtime or info.uname or info.gname:
                raise ValueError('Unexpected corresponding-source archive metadata')
            with archive.extractfile(info) as source:
                actual = hashlib.file_digest(source, 'sha256').hexdigest()
            if actual != expected[info.name]:
                raise ValueError('Corresponding source differs from pinned build inputs: '+info.name)
            seen.add(info.name)
    if seen != set(expected):
        raise ValueError('Corresponding-source archive is incomplete')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root', type=Path, default=ROOT)
    args = parser.parse_args()
    root = args.root.resolve()
    spec = json.loads((root/'helper/kernels/clara-hd.json').read_text())
    if sha(root/'kernel/clara-hd.config') != spec['config_sha256']:
        raise ValueError('Factory kernel configuration SHA256 mismatch')
    dockerfile = (root/'kernel/Dockerfile').read_text()
    if not dockerfile.startswith('FROM '+spec['toolchain_image']+'\n'):
        raise ValueError('Kernel toolchain differs from its pinned image')
    cache = root/'build/upstream'
    cache.mkdir(parents=True, exist_ok=True)
    archive = cache/'clara-kernel-source.tar.bz2'
    bundled = root/'kernel.tar.bz2'
    if not archive.exists() and bundled.exists():
        if sha(bundled) != spec['source_sha256']:
            raise ValueError('Bundled corresponding kernel source SHA256 mismatch')
        shutil.copyfile(bundled, archive)
    if not archive.exists():
        with tempfile.NamedTemporaryFile(dir=cache, prefix='.kernel-download-', delete=False) as temp:
            staged = Path(temp.name)
            try:
                with urllib.request.urlopen(spec['source_url'], timeout=120) as response:
                    total = 0
                    while chunk := response.read(1024*1024):
                        total += len(chunk)
                        if total > 128*1024*1024:
                            raise ValueError('Kernel source exceeds download limit')
                        temp.write(chunk)
                temp.close()
                if sha(staged) != spec['source_sha256']:
                    raise ValueError('Official kernel source SHA256 mismatch')
                staged.rename(archive)
            finally:
                staged.unlink(missing_ok=True)
    if sha(archive) != spec['source_sha256']:
        raise ValueError('Cached kernel source SHA256 mismatch')
    with tempfile.TemporaryDirectory(dir=root/'build', prefix='tun-build-') as folder:
        context = Path(folder)
        shutil.copyfile(archive, context/'kernel.tar.bz2')
        shutil.copyfile(root/'kernel/clara-hd.config', context/'clara-hd.config')
        shutil.copyfile(root/'kernel/Dockerfile', context/'Dockerfile')
        subprocess.run(['docker', 'build', '--platform', 'linux/amd64', '-t',
                        'k-tailscale-clara-tun', str(context)], check=True)
    container = subprocess.check_output(['docker', 'create', '--platform', 'linux/amd64',
                                         'k-tailscale-clara-tun', '/bin/true'], text=True).strip()
    try:
        subprocess.run(['docker', 'cp', container+':/output/tun.ko', str(root/'build/tun.ko')], check=True)
    finally:
        subprocess.run(['docker', 'rm', '-f', container], check=True, stdout=subprocess.DEVNULL)
    module = root/'build/tun.ko'
    if module.stat().st_size != spec['size'] or sha(module) != spec['sha256']:
        raise ValueError('Rebuilt TUN module does not reproduce the pinned output')
    source_bundle(root, archive, root/'build/k-tailscale-tun-source.tar.gz')
    verify_source_bundle(root, root/'build/k-tailscale-tun-source.tar.gz')
    print('Verified reproducible TUN module: '+str(module))


if __name__ == '__main__':
    main()
