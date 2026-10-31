#!/usr/bin/env python3
"""Verify exact addon scope, ABI, deterministic metadata and upstream authenticity."""
import argparse
import base64
import gzip
import hashlib
import io
import json
from pathlib import Path
import re
import struct
import subprocess
import tarfile
import tempfile

from package_scope import LIB, STATE, PAYLOAD, UI, core, expected, release_version

LIMIT = 128 * 1024 * 1024
SECRETS = re.compile(rb'tskey-[a-z]+-[A-Za-z0-9_-]+|(?:privkey|nodekey|machinekey|authkey):[a-zA-Z0-9]+|/api/v1/kobo/[a-fA-F0-9]{24,}|/a/[a-fA-F0-9]{16,}')


def arm_elf(payload, plugin):
    if len(payload) < 52 or payload[:7] != b'\x7fELF\x01\x01\x01':
        raise ValueError('Executable is not a 32-bit little-endian ELF')
    kind_id, machine = struct.unpack_from('<HH', payload, 16)
    if machine != 40 or kind_id != (3 if plugin else 2):
        raise ValueError('Unexpected executable architecture or type')
    if plugin:
        if not struct.unpack_from('<I', payload, 36)[0] & 0x400:
            raise ValueError('Plugin must use ARM hard-float')
        for marker in (b'QTMETADATA', b'qt_plugin_query_metadata', b'qt_plugin_instance', b'tailscale-ui'):
            if marker not in payload:
                raise ValueError('Missing Qt plugin metadata')
    elif b'kobo-tailscale/helper' not in payload:
        raise ValueError('Missing runtime helper build identity')


def upstream_proof(entries, verifier):
    metadata = json.loads(entries[PAYLOAD+'release.json'])
    archive = entries[PAYLOAD+'release.tgz']
    if len(archive) != metadata['size'] or hashlib.sha256(archive).hexdigest() != metadata['sha256']:
        raise ValueError('Bundled upstream archive differs from its public manifest')
    proof = metadata['proof']
    for name in ['keys_signature', 'package_signature']:
        if len(base64.b64decode(proof[name], validate=True)) != 64:
            raise ValueError('Invalid upstream signature size')
    keys = base64.b64decode(proof['keys'], validate=True)
    if not keys or len(keys) > 1024*1024 or b'PRIVATE KEY' in keys:
        raise ValueError('Invalid public signing bundle')
    with tempfile.TemporaryDirectory(prefix='kt-package-proof-') as folder:
        folder = Path(folder)
        (folder/'release.tgz').write_bytes(archive)
        (folder/'release.json').write_bytes(entries[PAYLOAD+'release.json'])
        result = subprocess.run([str(verifier), 'verify-payload', '--archive', str(folder/'release.tgz'),
                                 '--manifest', str(folder/'release.json')], capture_output=True, text=True, timeout=90)
        if result.returncode:
            raise ValueError('Official Tailscale signature verification failed: '+result.stderr[:400])
        verified = json.loads(result.stdout)
        for field in ['version', 'expanded', 'files']:
            if verified[field] != metadata[field]:
                raise ValueError('Upstream manifest size/version mismatch')


def verify(path, kind, verifier):
    with gzip.open(path, 'rb') as compressed:
        data = compressed.read(LIMIT+1)
    if len(data) > LIMIT:
        raise ValueError('Archive exceeds the size limit')
    entries = {}
    members = {}
    with tarfile.open(fileobj=io.BytesIO(data), mode='r:') as archive:
        for member in archive:
            name = member.name
            if name in entries or not member.isfile():
                raise ValueError('Duplicate path or unexpected member type')
            if member.uid or member.gid or member.mtime or member.uname or member.gname:
                raise ValueError('Archive metadata is not deterministic')
            payload = archive.extractfile(member).read(LIMIT+1)
            if len(payload) != member.size or len(payload) > LIMIT:
                raise ValueError('Invalid member size')
            if SECRETS.search(payload):
                raise ValueError('Credential-like data found in package')
            entries[name] = payload
            members[name] = member
    version = '' if kind == 'remove' else release_version(entries[UI+'release-version'].decode())
    scope = expected(version, kind)
    if set(entries) != set(scope):
        raise ValueError('Unexpected or missing archive paths')
    if any(members[name].mode != mode for name, mode in scope.items()):
        raise ValueError('Unexpected file permissions')
    arm_elf(entries[LIB], True)
    if kind == 'remove':
        if entries[STATE+'uninstall']:
            raise ValueError('Uninstall marker must be empty')
        return
    arm_elf(entries[core(version)+'helper'], False)
    if kind == 'install':
        upstream_proof(entries, verifier)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--kind', choices=['install', 'remove', 'update'], default='install')
    parser.add_argument('--helper', type=Path, default=Path(__file__).resolve().parents[1]/'build/k-tailscale-helper-host')
    parser.add_argument('path', type=Path)
    args = parser.parse_args()
    try:
        verify(args.path, args.kind, args.helper)
    except (OSError, EOFError, tarfile.TarError, ValueError, KeyError, TypeError, subprocess.SubprocessError) as error:
        parser.exit(1, 'Package verification failed: '+str(error)+'\n')
    print('Verified '+args.kind+' package: '+str(args.path))


if __name__ == '__main__':
    main()
