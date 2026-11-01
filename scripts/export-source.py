#!/usr/bin/env python3
"""Export only public source, without private workspace history or build files."""
import argparse
import pathlib
import shutil

ROOT = pathlib.Path(__file__).resolve().parents[1]
DIRECTORIES = ['src','helper','scripts','tests','installer','.github','kernel']
FILES = ['VERSION','README.md','RELEASING.md','LICENSE','NOTICE','Makefile','.gitignore','.gitattributes']


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('destination',type=pathlib.Path)
    args = parser.parse_args()
    destination = args.destination.resolve()
    if destination.exists():
        parser.error('Export destination must not exist')
    destination.mkdir(parents=True)
    for directory in DIRECTORIES:
        shutil.copytree(ROOT/directory,destination/directory,
                        ignore=shutil.ignore_patterns('__pycache__','*.pyc','*.test'))
    for name in FILES:
        shutil.copy2(ROOT/name,destination/name)
    for path in destination.rglob('*'):
        if path.is_file() and b'-----BEGIN PRIVATE KEY-----\n' in path.read_bytes():
            raise ValueError('Private key unexpectedly present in source export: '+str(path))
    print(destination)

if __name__ == '__main__':
    main()
