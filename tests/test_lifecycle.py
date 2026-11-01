import os
from pathlib import Path
import signal
import subprocess
import sys
import tempfile
import time
import unittest


SOURCE = Path(__file__).resolve().parents[1] / 'scripts/control.sh'


class LifecycleTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(prefix='tailui-life-')
        self.root = Path(self.tmp.name)
        self.children = []
        self.bin = self.root / 'bin'
        self.bin.mkdir()
        self.pidfile = self.root / 'daemon.pid'
        self.lock = self.root / 'connection.lock'
        self.client = self.root / 'runtime/bin/tailscale'
        self.client.parent.mkdir(parents=True)
        self.helper = self.root / 'hooks/connect.sh'
        self.helper.parent.mkdir(parents=True)
        self.hooks = self.root / 'hooks'
        self.hooks.mkdir(exist_ok=True)
        self.write_executable(self.client, '#!/bin/sh\n[ "$1" = down ] || exit 9\n[ ! -f ' + str(self.root / 'fail') + ' ] || { echo "CLI failure"; exit 7; }\necho disconnected > ' + str(self.root / 'preference') + '\n')
        self.write_executable(self.helper, '#!/bin/sh\nmkdir ' + str(self.lock) + ' 2>/dev/null || exit 3\ntrap "rmdir ' + str(self.lock) + '" 0\necho up >> ' + str(self.root / 'requests') + '\nsleep 0.5\n')
        discovery = '''#!{python}
import os,pathlib,subprocess,sys
if sys.argv[1:] != ['tailscaled']:sys.exit(2)
try:
 p=int(pathlib.Path({pidfile!r}).read_text());os.kill(p,0)
 state=subprocess.check_output(['ps','-p',str(p),'-o','stat='],text=True).strip()
 if not state or state.startswith('Z'):sys.exit(1)
 print(p)
except (OSError,ValueError,subprocess.CalledProcessError):sys.exit(1)
'''.format(python=sys.executable, pidfile=str(self.pidfile))
        self.write_executable(self.bin / 'pidof', discovery)
        self.daemon_code = self.root / 'daemon.py'
        self.daemon_code.write_text('import signal,time,sys\nif len(sys.argv)>1:signal.signal(signal.SIGTERM,signal.SIG_IGN)\nwhile True:time.sleep(0.05)\n')

    def tearDown(self):
        for child in self.children:
            if child.poll() is None:
                os.killpg(child.pid, signal.SIGKILL)
            child.wait()
        if self.pidfile.exists():
            try:os.kill(int(self.pidfile.read_text()), signal.SIGKILL)
            except ProcessLookupError:pass
        self.tmp.cleanup()

    @staticmethod
    def write_executable(path, text):
        path.write_text(text)
        path.chmod(0o755)

    def script(self):
        self.assertTrue(SOURCE.exists(), 'Lifecycle helper has not been implemented')
        text = SOURCE.read_text().replace('/usr/bin/tailscale', str(self.client))
        text = text.replace('/usr/local/k-tailscale/helper', str(self.root / 'core-helper'))
        text = text.replace('/usr/local/tailscale-runtime', str(self.root / 'runtime'))
        text = text.replace('/usr/local/tailscale-ui', str(self.root / 'ui'))
        text = text.replace('/tmp/kobo-tailscale-connect.lock', str(self.lock))
        text = text.replace('/usr/local/tailscale', str(self.hooks))
        text = text.replace('PATH=/sbin:/bin:/usr/sbin:/usr/bin', 'PATH=' + str(self.bin) + ':' + os.environ['PATH'])
        target = self.root / 'control.sh'
        target.write_text(text)
        return target

    def run_action(self, action):
        return subprocess.run(['/bin/sh', str(self.script()), action], capture_output=True, text=True, timeout=12)

    def start_daemon(self, ignore=False):
        args = [sys.executable, str(self.daemon_code)] + (['ignore'] if ignore else [])
        child = subprocess.Popen(args, start_new_session=True)
        self.children.append(child)
        self.pidfile.write_text(str(child.pid))
        time.sleep(0.08)
        return child

    def test_duplicate_connect_starts_one_login(self):
        script = self.script()
        first = subprocess.Popen(['/bin/sh', str(script), 'connect'])
        time.sleep(0.1)
        second = subprocess.run(['/bin/sh', str(script), 'connect'], capture_output=True, timeout=3)
        self.assertEqual(first.wait(timeout=3), 0)
        self.assertNotEqual(second.returncode, 0)
        self.assertEqual((self.root / 'requests').read_text().splitlines(), ['up'])

    def test_connection_action_forwards_exact_intent(self):
        self.write_executable(self.helper, '#!/bin/sh\necho "$1" > "'+str(self.root/'intent')+'"\n')
        for verb in ['connect', 'signin']:
            with self.subTest(verb=verb):
                result=self.run_action(verb)
                self.assertEqual(result.returncode,0,result.stdout+result.stderr)
                self.assertEqual((self.root/'intent').read_text().strip(),verb)

    def test_cold_start_reaches_recovery_before_ram_cli_exists(self):
        self.client.unlink()
        self.write_executable(self.helper, '#!/bin/sh\necho "runtime recovery reached"\n')
        for verb in ['connect', 'signin']:
            with self.subTest(verb=verb):
                result=self.run_action(verb)
                self.assertEqual(result.returncode,0,result.stdout+result.stderr)
                self.assertIn('runtime recovery reached',result.stdout)

    def test_disconnect_preserves_running_daemon(self):
        daemon = self.start_daemon()
        result = self.run_action('disconnect')
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual((self.root / 'preference').read_text().strip(), 'disconnected')
        self.assertIsNone(daemon.poll())

    def test_logout_uses_serialized_core_action_instead_of_down(self):
        self.write_executable(self.root / 'core-helper', '#!/bin/sh\necho "$1" >> "' + str(self.root / 'core-calls') + '"\n')
        result = self.run_action('logout')
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual((self.root / 'core-calls').read_text().splitlines(), ['logout'])
        self.assertFalse((self.root / 'preference').exists())

    def test_logout_core_error_is_not_success(self):
        self.write_executable(self.root / 'core-helper', '#!/bin/sh\necho "logout refused"; exit 7\n')
        result = self.run_action('logout')
        self.assertEqual(result.returncode, 7)
        self.assertIn('logout refused', result.stdout)

    def test_failed_down_is_reported(self):
        self.start_daemon();(self.root / 'fail').touch()
        result = self.run_action('disconnect')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('CLI failure', result.stdout)

    def test_busy_connection_prevents_usb_ready(self):
        daemon = self.start_daemon();self.lock.mkdir()
        result = self.run_action('stop')
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn('Ready for USB', result.stdout)
        self.assertIsNone(daemon.poll())

    def test_stop_waits_for_daemon_exit(self):
        daemon = self.start_daemon()
        result = self.run_action('stop')
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIsNotNone(daemon.poll())
        self.assertIn('Ready for USB', result.stdout)
        self.assertFalse(self.lock.exists())

    def test_unresponsive_daemon_never_reports_usb_ready(self):
        self.start_daemon(ignore=True)
        result = self.run_action('stop')
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn('Ready for USB', result.stdout)

    def test_stop_waits_for_pending_boot_job(self):
        worker = self.hooks / 'on-boot.sh'
        self.write_executable(worker, '#!/bin/sh\nsleep 0.4\n' + sys.executable + ' ' + str(self.daemon_code) + ' &\necho $! > ' + str(self.pidfile) + '\n')
        child = subprocess.Popen(['/bin/sh', str(worker)], start_new_session=True)
        self.children.append(child)
        time.sleep(0.05)
        result = self.run_action('stop')
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertTrue(self.pidfile.exists(), 'Stop returned before the pending boot worker finished')
        discovery = subprocess.run([str(self.bin / 'pidof'), 'tailscaled'], capture_output=True)
        self.assertNotEqual(discovery.returncode, 0)

    def test_unknown_action_has_no_side_effect(self):
        result = self.run_action('erase')
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse((self.root / 'preference').exists())

    def test_prepare_dispatches_without_requiring_new_runtime_binaries(self):
        ui=self.root/'ui';ui.mkdir()
        self.write_executable(ui/'setup.sh','#!/bin/sh\necho setup-prepared\n')
        self.client.unlink()
        result=self.run_action('prepare')
        self.assertEqual(result.returncode,0,result.stdout+result.stderr)
        self.assertIn('setup-prepared',result.stdout)


if __name__ == '__main__':
    unittest.main()
