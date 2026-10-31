#!/usr/bin/env python3
"""Decode rendered login images with an independent implementation."""
from pathlib import Path
import subprocess
import sys
names = sys.argv[1:] or ['plain', 'query', 'small', 'changed', 'widget']
for name in names:
    image = Path('build') / f'qr-{name}.png'
    expected = image.with_suffix('.txt').read_bytes()
    result = subprocess.run(['zbarimg', '--quiet', '--raw', str(image)], capture_output=True, check=True)
    if result.stdout != expected + b'\n':
        raise AssertionError(f'QR payload mismatch: {name}')
print('Independent ZBar decoding matches all complete login URL payloads.')
