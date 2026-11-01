import hashlib
import importlib.util
import io
import json
from pathlib import Path
import tarfile
import tempfile
import unittest


PROJECT = Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location('tun_build', PROJECT/'scripts/build-tun.py')
BUILD = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(BUILD)


class TUNSourceBundle(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.source = self.root/'upstream.tar.bz2'
        self.source.write_bytes(b'complete upstream source fixture')
        for name in BUILD.SOURCE_FILES:
            path = self.root/name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text('public build input: '+name+'\n')
        (self.root/'helper/kernels/clara-hd.json').write_text(json.dumps({
            'source_sha256': hashlib.sha256(self.source.read_bytes()).hexdigest()}))
        self.bundle = self.root/'source.tar.gz'

    def test_complete_bundle_has_reproducible_bytes_and_verifiable_inputs(self):
        BUILD.source_bundle(self.root, self.source, self.bundle)
        first = self.bundle.read_bytes()
        BUILD.verify_source_bundle(self.root, self.bundle)
        BUILD.source_bundle(self.root, self.source, self.bundle)
        self.assertEqual(first, self.bundle.read_bytes())

    def test_changed_upstream_and_build_recipe_are_rejected(self):
        BUILD.source_bundle(self.root, self.source, self.bundle)
        self.source.write_bytes(b'changed upstream')
        BUILD.source_bundle(self.root, self.source, self.bundle)
        with self.assertRaisesRegex(ValueError, 'differs from pinned'):
            BUILD.verify_source_bundle(self.root, self.bundle)
        self.source.write_bytes(b'complete upstream source fixture')
        BUILD.source_bundle(self.root, self.source, self.bundle)
        (self.root/'kernel/Dockerfile').write_text('changed recipe')
        with self.assertRaisesRegex(ValueError, 'differs from pinned'):
            BUILD.verify_source_bundle(self.root, self.bundle)

    def test_unexpected_file_is_not_accepted_as_corresponding_source(self):
        with tarfile.open(self.bundle, 'w:gz') as archive:
            info = tarfile.TarInfo('unexpected-file')
            info.size = 4
            archive.addfile(info, io.BytesIO(b'nope'))
        with self.assertRaisesRegex(ValueError, 'Unexpected'):
            BUILD.verify_source_bundle(self.root, self.bundle)


if __name__ == '__main__':
    unittest.main()
