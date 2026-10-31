"""Exercise the real root connection script at its external process boundary."""
import json
import os
from pathlib import Path
import socket
import subprocess
import sys
import tempfile
import unittest

SOURCE = Path(__file__).resolve().parents[1] / 'scripts/connect-root.sh'


@unittest.skipUnless(sys.platform.startswith('linux'), 'Kobo process semantics')
class ConnectTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(prefix='tailui-connect-')
        self.root = Path(self.tmp.name)
        self.addCleanup(self.tmp.cleanup)
        self.runtime = self.root/'runtime'
        self.commands = self.root/'commands'
        (self.runtime/'bin').mkdir(parents=True)
        self.commands.mkdir()
        (self.runtime/'ready').write_text('storage-v1\n')
        (self.root/'routes').write_text('wlan0 00000000 0100000A 0003\n')
        self.core = self.root/'core'
        self.executable(self.core, '#!/bin/sh\n[ ! -f "'+str(self.root/'recover-fail')+'" ] || { echo "recovery refused"; exit 6; }\necho \'{"message":"Action completed."}\'\n')
        self.executable(self.commands/'pidof', '#!/bin/sh\necho 321\n')
        self.calls = self.root/'calls.json'
        self.issued = self.root/'issued-link'
        self.executable(self.runtime/'bin/tailscale', '#!'+sys.executable+'\n'+
            'import json,pathlib,sys\n'+
            'args=sys.argv[1:]\npathlib.Path('+repr(str(self.calls))+').write_text(json.dumps(args))\n'+
            'if pathlib.Path('+repr(str(self.root/'cli-fail'))+').exists():\n print("fixture authentication timeout",file=sys.stderr);sys.exit(7)\n'+
            'if "--force-reauth" in args:\n pathlib.Path('+repr(str(self.issued))+').write_text("https://login.tailscale.com/a/fake-new-login")\n print("To authenticate, visit: https://login.tailscale.com/a/fake-new-login")\n')
        (self.root/'run').mkdir()
        server = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        self.addCleanup(server.close)
        server.bind(str(self.root/'run/tailscaled.sock'))
        text = SOURCE.read_text()
        for original, replacement in [('/usr/local/k-tailscale/helper',self.core),
            ('/usr/local/tailscale-runtime',self.runtime),('/usr/local/tailscale',self.root/'hooks'),
            ('/tmp/kobo-tailscale-connect.lock',self.root/'lock'),('/tmp/kobo-tailscale',self.root/'logs'),
            ('/var/run/tailscale',self.root/'run'),('/proc/net/route',self.root/'routes')]:
            text = text.replace(original,str(replacement))
        text = text.replace('PATH=/sbin:/bin:/usr/sbin:/usr/bin','PATH='+str(self.commands)+':'+os.environ['PATH'])
        self.script = self.root/'connect.sh'
        self.script.write_text(text)

    @staticmethod
    def executable(path,text):
        path.write_text(text)
        path.chmod(0o755)

    def run_action(self,verb):
        return subprocess.run(['/bin/sh',str(self.script),verb],text=True,capture_output=True,timeout=4)

    def test_real_cli_failure_reaches_ui_instead_of_recovery_ack(self):
        (self.root/'cli-fail').touch()
        result = self.run_action('signin')
        self.assertEqual(result.returncode,7,result.stdout+result.stderr)
        self.assertIn('fixture authentication timeout',result.stdout+result.stderr)
        self.assertNotIn('Action completed',result.stdout+result.stderr)
        self.assertIn('fixture authentication timeout',(self.root/'logs/auth.log').read_text())
        self.assertFalse((self.root/'lock').exists())

    def test_error_is_delivered_before_releasing_shared_log_lock(self):
        # Force the valid interleaving where a new owner truncates on release.
        self.executable(self.commands/'rmdir', '#!/bin/sh\n/bin/rmdir "$1" || exit $?\nprintf "next attempt log\\n" > "'+str(self.root/'logs/auth.log')+'"\n')
        (self.root/'cli-fail').touch()
        result=self.run_action('signin')
        self.assertEqual(result.returncode,7)
        self.assertIn('fixture authentication timeout',result.stdout+result.stderr)
        self.assertNotIn('next attempt log',result.stdout+result.stderr)

    def test_retained_key_signin_requests_interactive_authentication(self):
        result = self.run_action('signin')
        self.assertEqual(result.returncode,0,result.stdout+result.stderr)
        self.assertTrue(self.issued.exists(),'Sign in did not request a new interactive login')
        self.assertEqual(json.loads(self.calls.read_text()),['up','--force-reauth','--accept-dns=false','--hostname=kobo-libra-colour','--timeout=15s'])
        self.assertNotIn('Action completed',result.stdout+result.stderr)

    def test_saved_login_connect_does_not_force_authentication(self):
        result = self.run_action('connect')
        self.assertEqual(result.returncode,0,result.stdout+result.stderr)
        self.assertFalse(self.issued.exists())
        self.assertEqual(json.loads(self.calls.read_text()),['up','--accept-dns=false','--hostname=kobo-libra-colour','--timeout=90s'])

    def test_recovery_failure_is_not_silenced(self):
        (self.root/'recover-fail').touch()
        result = self.run_action('signin')
        self.assertEqual(result.returncode,6)
        self.assertIn('recovery refused',result.stdout+result.stderr)
        self.assertFalse(self.calls.exists())

    def test_bad_verb_has_no_authentication_side_effect(self):
        result = self.run_action('erase')
        self.assertNotEqual(result.returncode,0)
        self.assertFalse(self.calls.exists())


if __name__ == '__main__':
    unittest.main()
