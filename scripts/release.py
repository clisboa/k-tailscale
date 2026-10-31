#!/usr/bin/env python3
"""Verify local release artifacts and sign the independent addon manifest."""
import argparse
import hashlib
import json
import pathlib
import subprocess
import tempfile
from package_scope import release_version

ROOT = pathlib.Path(__file__).resolve().parents[1]
REPOSITORY = 'clisboa/k-tailscale'


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--key', required=True, type=pathlib.Path,
                        help='private Ed25519 PEM outside the source tree')
    args = parser.parse_args()
    key = args.key.resolve()
    if key == ROOT or ROOT in key.parents:
        parser.error('Keep private signing material outside the source tree')
    if key.stat().st_mode & 0o077:
        parser.error('Signing key must have private file permissions (0600)')
    version = release_version((ROOT/'VERSION').read_text())
    for kind, name in [('install','KoboRoot.tgz'), ('update','k-tailscale-update.tgz'),
                       ('remove','KoboRoot-uninstall.tgz')]:
        subprocess.run(['python3', str(ROOT/'scripts/verify-package.py'),
                        '--kind',kind,str(ROOT/'dist'/name)], check=True)
    public = subprocess.check_output(['openssl','pkey','-in',str(key),'-pubout'])
    if public.strip() != (ROOT/'helper/addon-public.pem').read_bytes().strip():
        parser.error('Signing key does not match the embedded public key')
    archive = ROOT/'dist/k-tailscale-update.tgz'
    manifest = {'schema':1,'version':version,'min_helper':'0.1.0',
                'size':archive.stat().st_size,
                'sha256':hashlib.sha256(archive.read_bytes()).hexdigest(),
                'url':f'https://github.com/{REPOSITORY}/releases/download/v{version}/k-tailscale-update.tgz'}
    output = ROOT/'dist/update.json'
    output.write_text(json.dumps(manifest,sort_keys=True,separators=(',',':'))+'\n')
    signature = ROOT/'dist/update.json.sig'
    subprocess.run(['openssl','pkeyutl','-sign','-rawin','-inkey',str(key),
                    '-in',str(output),'-out',str(signature)], check=True)
    with tempfile.TemporaryDirectory(prefix='k-tailscale-signature-') as tmp:
        pub = pathlib.Path(tmp)/'public.pem'
        pub.write_bytes(public)
        subprocess.run(['openssl','pkeyutl','-verify','-rawin','-pubin',
                        '-inkey',str(pub),'-in',str(output),'-sigfile',str(signature)],check=True)
    subprocess.run(['python3', str(ROOT/'scripts/build-site.py')], check=True)
    print('Verified and signed local release '+version)

if __name__ == '__main__':
    main()
