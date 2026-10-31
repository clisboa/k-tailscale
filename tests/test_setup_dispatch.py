import os
from pathlib import Path
import subprocess
import tempfile
import unittest

PROJECT = Path(__file__).resolve().parents[1]


class SetupDispatch(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='kt-setup-')
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.runtime = self.root/'runtime'
        self.ui = self.root/'ui'
        self.core = self.root/'core'
        self.source = self.root/'source'
        self.calls = self.root/'calls'
        for directory in [self.ui, self.core/'releases/0.1.0', self.source]:
            directory.mkdir(parents=True)
        (self.ui/'release-version').write_text('0.1.0\n')
        self.executable(self.core/'releases/0.1.0/helper', '#!/bin/sh\necho "helper:$1" >> '+str(self.calls)+'\n')
        self.executable(self.ui/'migrate.sh', '#!/bin/sh\necho migrate >> '+str(self.calls)+'\nmkdir -p '+str(self.runtime)+'\necho storage-v1 > '+str(self.runtime/'ready')+'\n')

    @staticmethod
    def executable(path, content):
        path.write_text(content)
        path.chmod(0o755)

    def run_setup(self):
        source = (PROJECT/'scripts/setup.sh').read_text()
        for original, replacement in [('/usr/local/tailscale-runtime', self.runtime),
                                      ('/usr/local/tailscale-ui', self.ui),
                                      ('/usr/local/k-tailscale', self.core),
                                      ('/mnt/onboard/tailscale', self.source)]:
            source = source.replace(original,str(replacement))
        script = self.root/'setup.sh'
        script.write_text(source)
        return subprocess.run(['/bin/sh',str(script)], capture_output=True,text=True,timeout=8)

    def test_fresh_and_current_setup_use_versioned_helper(self):
        result = self.run_setup()
        self.assertEqual(result.returncode,0,result.stdout+result.stderr)
        self.assertEqual(self.calls.read_text(),'helper:prepare\n')
        self.runtime.mkdir()
        (self.runtime/'ready').write_text('storage-v1\n')
        (self.source/'tailscaled.state').write_text('stale-state')
        self.calls.unlink()
        self.assertEqual(self.run_setup().returncode,0)
        self.assertEqual(self.calls.read_text(),'helper:prepare\n')

    def test_legacy_setup_migrates_before_preparing(self):
        (self.source/'tailscaled.state').write_text('fixture-identity')
        result=self.run_setup()
        self.assertEqual(result.returncode,0,result.stdout+result.stderr)
        self.assertEqual(self.calls.read_text(),'migrate\nhelper:prepare\n')

    def test_unknown_uncommitted_system_state_is_never_replaced(self):
        (self.runtime/'state').mkdir(parents=True)
        (self.runtime/'state/tailscaled.state').write_text('newer-system-identity')
        (self.source/'tailscaled.state').write_text('older-books-identity')
        result=self.run_setup()
        self.assertNotEqual(result.returncode,0)
        self.assertFalse(self.calls.exists())
        self.assertEqual((self.runtime/'state/tailscaled.state').read_text(),'newer-system-identity')

    def test_independent_core_recovers_before_selecting_candidate(self):
        self.executable(self.core/'helper', '#!/bin/sh\necho "core:$1" >> '+str(self.calls)+'\n')
        result=self.run_setup()
        self.assertEqual(result.returncode,0,result.stdout+result.stderr)
        self.assertEqual(self.calls.read_text(),'core:recover-files\nhelper:prepare\n')

    def test_invalid_version_never_runs_a_helper(self):
        (self.ui/'release-version').write_text('../bad\n')
        self.assertNotEqual(self.run_setup().returncode,0)
        self.assertFalse(self.calls.exists())


if __name__ == '__main__':
    unittest.main()
