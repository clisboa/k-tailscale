#!/usr/bin/env python3
"""Build the static GitHub Pages installer from a verified complete archive."""
import argparse
import hashlib
import json
from pathlib import Path
import shutil
import subprocess
import sys
import tarfile

from package_scope import PAYLOAD, release_version


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root', type=Path, default=Path(__file__).resolve().parents[1])
    parser.add_argument('--output', type=Path)
    args = parser.parse_args()
    root = args.root.resolve()
    package = root/'dist/KoboRoot.tgz'
    subprocess.run([sys.executable, str(Path(__file__).with_name('verify-package.py')),
                    '--helper', str(root/'build/k-tailscale-helper-host'), str(package)], check=True)
    output = args.output or root/'dist/site'
    output.mkdir(parents=True, exist_ok=True)
    for name in ['index.html', 'style.css', 'drive.mjs', 'main.mjs']:
        shutil.copyfile(root/'installer'/name, output/name)
    shutil.copytree(root/'installer/images', output/'images', dirs_exist_ok=True,
                    ignore=shutil.ignore_patterns('README.md'))
    shutil.copyfile(package, output/'KoboRoot.tgz')
    with tarfile.open(package) as archive:
        upstream = json.loads(archive.extractfile(PAYLOAD+'release.json').read())
    metadata = {'version': release_version((root/'VERSION').read_text()), 'tailscale': upstream['version'],
                'size': package.stat().st_size, 'sha256': hashlib.sha256(package.read_bytes()).hexdigest(),
                'file': 'KoboRoot.tgz'}
    (output/'install.json').write_text(json.dumps(metadata, sort_keys=True)+'\n')
    print(output)


if __name__ == '__main__':
    main()
