#!/usr/bin/env python3
"""Collect licenses for the helper's actual Linux/ARM dependency graph."""
import argparse
import json
import os
from pathlib import Path
import subprocess


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root', required=True, type=Path)
    root = parser.parse_args().root.resolve()
    env = dict(os.environ, CGO_ENABLED='0', GOOS='linux', GOARCH='arm', GOARM='7')
    raw = subprocess.check_output(['go', 'list', '-deps', '-json', './cmd/k-tailscale-helper'],
                                  cwd=root/'helper', env=env, text=True)
    modules = {}
    decoder = json.JSONDecoder()
    while raw.strip():
        package, index = decoder.raw_decode(raw.lstrip())
        raw = raw.lstrip()[index:]
        module = package.get('Module')
        if module:
            modules[module['Path']] = module
    sections = ['k-tailscale\n'+(root/'LICENSE').read_text()]
    ca = root/'helper/ca'
    sections.append('Mozilla CA certificate bundle\n'+(ca/'README.md').read_text()+'\n'+(ca/'MPL-2.0.txt').read_text())
    goroot = Path(subprocess.check_output(['go', 'env', 'GOROOT'], text=True).strip())
    go_license = next((p for p in [goroot/'LICENSE', goroot.parent/'LICENSE'] if p.is_file()), None)
    if go_license is None:
        raise RuntimeError('Go distribution license is missing')
    sections.append('Go standard library\n'+go_license.read_text())
    for name, module in sorted(modules.items()):
        if module.get('Main'):
            continue
        directory = Path(module.get('Replace', module)['Dir'])
        licenses = sorted({p for pattern in ['LICENSE*', 'COPYING*', 'NOTICE*']
                           for p in directory.glob(pattern) if p.is_file()})
        if not licenses:
            raise RuntimeError('No dependency license found for '+name)
        heading = name+' '+module.get('Version', '')
        sections.append(heading+'\n'+'\n'.join(p.read_text() for p in licenses))
    target = root/'build/THIRD_PARTY_NOTICES.txt'
    target.write_text('\n\n'+'\n\n'.join(sections)+'\n')
    print('Collected notices for',len(modules)-1,'dependency modules')


if __name__ == '__main__':
    main()
