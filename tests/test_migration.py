import json
import os
from pathlib import Path
import shutil
import signal
import socket
import subprocess
import sys
import tempfile
import time
import unittest

PROJECT = Path(__file__).resolve().parents[1]


@unittest.skipUnless(sys.platform.startswith('linux'), 'Requires Linux process and filesystem semantics')
class MigrationTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='tailui-migrate-')
        self.root = Path(self.temp.name)
        self.onboard = self.root/'onboard'
        self.source = self.onboard/'tailscale'
        self.system = self.root/'system/tailscale'
        self.runtime = self.root/'system/tailscale-runtime'
        self.payload = self.root/'system/tailscale-ui'
        self.usrbin = self.root/'usrbin'
        self.bin = self.root/'commands'
        self.lock = self.root/'connect.lock'
        self.pidfile = self.root/'daemon.pid'
        self.started = self.root/'started'
        self.mounts = self.root/'mounts'
        self.free = self.root/'free'
        for p in [self.source, self.system, self.payload, self.usrbin, self.bin]:
            p.mkdir(parents=True)
        self.mounts.write_text('/dev/test '+str(self.onboard)+' vfat rw 0 0\n')
        self.free.write_text('1000000')
        self.state = b'{"identity":"fixture-node","WantRunning":true}\n'
        (self.source/'tailscaled.state').write_bytes(self.state)
        (self.source/'certs').mkdir()
        (self.source/'certs/fixture').write_bytes(b'preserved-certificate')
        self.executable(self.source/'tailscale', '#!/bin/sh\nexit 0\n')
        daemon = '#!'+sys.executable+'\nimport os,pathlib,signal,time\n'
        daemon += 'pathlib.Path('+repr(str(self.pidfile))+').write_text(str(os.getpid()))\n'
        daemon += 'pathlib.Path('+repr(str(self.started))+').write_text(__file__)\n'
        daemon += 'signal.signal(signal.SIGTERM,lambda *args:exit(0))\nwhile True:time.sleep(.02)\n'
        self.executable(self.source/'tailscaled', daemon)
        self.oldboot = '#!/bin/sh\n"'+str(self.source/'tailscaled')+'" </dev/null >/dev/null 2>&1 &\n'
        self.executable(self.system/'on-boot.sh', self.oldboot)
        self.executable(self.system/'boot.sh', '#!/bin/sh\n/bin/sh "'+str(self.system/'on-boot.sh')+'"\n')
        for name in ['tailscale', 'tailscaled']:
            (self.usrbin/name).symlink_to(self.source/name)
        self.rootboot = '#!/bin/sh\ncd /\npidof tailscaled >/dev/null && exit 0\n"'+str(self.runtime/'bin/tailscaled')+'" --statedir="'+str(self.runtime/'state')+'" </dev/null >/dev/null 2>&1 &\n'
        self.executable(self.payload/'on-boot-root.sh', self.rootboot)
        for name in ['connect-root.sh', 'login-root.sh', 'diagnose-root.sh']:
            self.executable(self.payload/name, '#!/bin/sh\nexit 0\n')
        menu = self.onboard/'.adds/nm/tailscale'
        menu.parent.mkdir(parents=True)
        self.menu = menu
        self.oldmenu = ('menu_item:main:Tailscale - Connect:cmd_spawn:quiet:sh /mnt/onboard/.adds/kobo-tailscale/connect.sh\n'
                        'menu_item:main:Tailscale - Status:cmd_output:9999:/mnt/onboard/tailscale/tailscale status\n'
                        'menu_item:main:Tailscale - Stop for USB:cmd_output:9999:old-stop\n'
                        'menu_item:main:My custom item:cmd_spawn:quiet:/custom/tool\n')
        self.oldmenu=self.oldmenu.replace('/mnt/onboard',str(self.onboard))
        menu.write_text(self.oldmenu)
        self.executable(self.bin/'df', '#!/bin/sh\necho "Filesystem 1024-blocks Used Available Capacity Mounted"\necho "test 2000000 1000000 $(cat '+str(self.free)+') 50% /"\n')
        discovery = '#!'+sys.executable+'\nimport os,pathlib,subprocess,sys\ntry:\n'
        discovery += ' p=int(pathlib.Path('+repr(str(self.pidfile))+').read_text());os.kill(p,0)\n'
        discovery += ' s=subprocess.check_output(["ps","-p",str(p),"-o","stat="],text=True).strip()\n'
        discovery += ' if not s or s.startswith("Z"):sys.exit(1)\n print(p)\nexcept (OSError,ValueError,subprocess.CalledProcessError):sys.exit(1)\n'
        self.executable(self.bin/'pidof', discovery)
        self.executable(self.bin/'cp', '#!/bin/sh\n[ ! -f '+str(self.root/'fail-copy')+' ] || exit 9\nexec /bin/cp "$@"\n')
        self.executable(self.bin/'ln', '#!/bin/sh\n[ ! -f '+str(self.root/'fail-link')+' ] || exit 9\nexec /bin/ln "$@"\n')
        self.env = dict(os.environ, PATH=str(self.bin)+':'+os.environ['PATH'])

    def tearDown(self):
        if self.pidfile.exists():
            try:
                os.kill(int(self.pidfile.read_text()), signal.SIGKILL)
            except (ProcessLookupError, ValueError):
                pass
        self.temp.cleanup()

    @staticmethod
    def executable(path, text):
        path.write_text(text)
        path.chmod(0o755)

    def script(self):
        source = PROJECT/'scripts/migrate.sh'
        self.assertTrue(source.exists(), 'Storage migration is not implemented')
        text = source.read_text()
        for original, replacement in [('/usr/local/tailscale-runtime',self.runtime),
                                      ('/usr/local/tailscale-ui',self.payload),
                                      ('/usr/local/tailscale',self.system),
                                      ('/usr/bin/tailscale',self.usrbin/'tailscale'),
                                      ('/mnt/onboard',self.onboard),
                                      ('/tmp/kobo-tailscale-connect.lock',self.lock),
                                      ('/proc/mounts',self.mounts)]:
            text = text.replace(original,str(replacement))
        text = text.replace('PATH=/sbin:/bin:/usr/sbin:/usr/bin','PATH='+self.env['PATH'])
        target = self.root/'migrate.sh'
        target.write_text(text)
        return target

    def run_migration(self):
        return subprocess.run(['/bin/sh',str(self.script())],env=self.env,capture_output=True,text=True,timeout=15)

    def running(self):
        return subprocess.run([str(self.bin/'pidof'),'tailscaled'],capture_output=True).returncode == 0

    def start_daemon(self):
        subprocess.run(['/bin/sh',str(self.system/'boot.sh')],env=self.env,check=True)
        for _ in range(100):
            if self.running():
                return int(self.pidfile.read_text())
            time.sleep(.01)
        self.fail('Fixture daemon failed to start')

    def assert_original(self):
        self.assertEqual((self.system/'on-boot.sh').read_text(),self.oldboot)
        self.assertEqual(os.readlink(self.usrbin/'tailscaled'),str(self.source/'tailscaled'))
        self.assertEqual(self.menu.read_text(),self.oldmenu)
        self.assertEqual((self.source/'tailscaled.state').read_bytes(),self.state)
        self.assertFalse((self.runtime/'ready').exists())

    def test_preserves_identity_preferences_and_restarts_from_root(self):
        for wanted in [True,False]:
            with self.subTest(wanted=wanted):
                if self.runtime.exists():
                    shutil.rmtree(self.runtime)
                    (self.system/'on-boot.sh').write_text(self.oldboot)
                    for name in ['connect','login','diagnose']:
                        (self.system/(name+'.sh')).unlink()
                    self.menu.write_text(self.oldmenu)
                    for name in ['tailscale','tailscaled']:
                        (self.usrbin/name).unlink();(self.usrbin/name).symlink_to(self.source/name)
                self.state=json.dumps({'identity':'fixture-node','WantRunning':wanted}).encode()
                (self.source/'tailscaled.state').write_bytes(self.state)
                oldpid=self.start_daemon()
                result=self.run_migration()
                self.assertEqual(result.returncode,0,result.stdout+result.stderr)
                self.assertEqual((self.runtime/'state/tailscaled.state').read_bytes(),self.state)
                self.assertEqual((self.runtime/'state/certs/fixture').read_bytes(),b'preserved-certificate')
                self.assertEqual((self.runtime/'state/tailscaled.state').stat().st_mode&0o777,0o600)
                self.assertTrue(self.running())
                self.assertNotEqual(int(self.pidfile.read_text()),oldpid)
                self.assertEqual(self.started.read_text(),str(self.runtime/'bin/tailscaled'))
                self.assertEqual(os.readlink(self.usrbin/'tailscaled'),str(self.runtime/'bin/tailscaled'))
                self.assertIn('My custom item',self.menu.read_text())
                self.assertNotIn('Stop for USB',self.menu.read_text())
                self.assertIn(str(self.runtime/'bin/tailscale'),self.menu.read_text())
                os.kill(int(self.pidfile.read_text()),signal.SIGTERM);time.sleep(.1)

    def test_previously_stopped_stays_stopped(self):
        result=self.run_migration()
        self.assertEqual(result.returncode,0,result.stdout+result.stderr)
        self.assertFalse(self.running())
        self.assertTrue((self.runtime/'ready').exists())

    def test_insufficient_space_changes_nothing(self):
        pid=self.start_daemon();self.free.write_text('1')
        result=self.run_migration()
        self.assertNotEqual(result.returncode,0)
        self.assertIn('space',result.stdout.lower())
        self.assert_original();self.assertEqual(int(self.pidfile.read_text()),pid);self.assertTrue(self.running())

    def test_missing_identity_changes_nothing(self):
        (self.source/'tailscaled.state').unlink()
        result=self.run_migration()
        self.assertNotEqual(result.returncode,0)
        self.assertFalse((self.runtime/'ready').exists())
        self.assertEqual((self.system/'on-boot.sh').read_text(),self.oldboot)

    def test_busy_lock_is_not_stolen(self):
        self.lock.mkdir();result=self.run_migration()
        self.assertNotEqual(result.returncode,0)
        self.assertTrue(self.lock.exists());self.assert_original()

    def test_copy_failure_rolls_back_and_restarts_legacy(self):
        self.start_daemon();(self.root/'fail-copy').touch()
        result=self.run_migration()
        self.assertNotEqual(result.returncode,0)
        self.assert_original();self.assertTrue(self.running())
        self.assertFalse(self.lock.exists())

    def test_activation_failure_restores_boot_and_menu(self):
        self.start_daemon();(self.root/'fail-link').touch()
        result=self.run_migration()
        self.assertNotEqual(result.returncode,0)
        self.assert_original();self.assertTrue(self.running())
        self.assertFalse(self.lock.exists())

    def test_idempotence_never_recopies_stale_onboard_state(self):
        self.assertEqual(self.run_migration().returncode,0)
        fresh=b'new-root-state-and-preferences'
        (self.runtime/'state/tailscaled.state').write_bytes(fresh)
        result=self.run_migration()
        self.assertEqual(result.returncode,0,result.stdout+result.stderr)
        self.assertEqual((self.runtime/'state/tailscaled.state').read_bytes(),fresh)
        shutil.move(self.onboard,self.root/'usb-exported')
        self.assertEqual(self.run_migration().returncode,0)
        self.assertEqual((self.runtime/'state/tailscaled.state').read_bytes(),fresh)

    def test_invalid_active_runtime_never_overwrites_new_identity(self):
        self.assertEqual(self.run_migration().returncode,0)
        fresh=b'new-active-node-state'
        (self.runtime/'state/tailscaled.state').write_bytes(fresh)
        (self.usrbin/'tailscaled').unlink()
        result=self.run_migration()
        self.assertNotEqual(result.returncode,0)
        self.assertEqual((self.runtime/'state/tailscaled.state').read_bytes(),fresh)

    def test_changed_boot_is_reported_without_overwriting_identity(self):
        self.assertEqual(self.run_migration().returncode,0)
        fresh=b'current-root-identity'
        (self.runtime/'state/tailscaled.state').write_bytes(fresh)
        (self.system/'on-boot.sh').write_text(self.oldboot)
        result=self.run_migration()
        self.assertNotEqual(result.returncode,0)
        self.assertEqual((self.runtime/'state/tailscaled.state').read_bytes(),fresh)

    def test_restart_failure_reports_committed_storage_separately(self):
        self.start_daemon()
        self.executable(self.source/'tailscaled','#!/bin/sh\nexit 1\n')
        result=self.run_migration()
        self.assertEqual(result.returncode,0,result.stdout+result.stderr)
        self.assertIn('startup failed',result.stdout)
        self.assertTrue((self.runtime/'ready').exists())
        self.assertEqual((self.runtime/'state/tailscaled.state').read_bytes(),self.state)

    def test_root_boot_works_with_onboard_absent(self):
        self.assertEqual(self.run_migration().returncode,0)
        shutil.move(self.onboard,self.root/'usb-exported')
        subprocess.run(['/bin/sh',str(self.system/'boot.sh')],env=self.env,check=True)
        for _ in range(100):
            if self.running():break
            time.sleep(.01)
        self.assertTrue(self.running())
        self.assertEqual(self.started.read_text(),str(self.runtime/'bin/tailscaled'))

    def test_recovers_owned_interrupted_activation(self):
        self.assertEqual(self.run_migration().returncode,0)
        (self.runtime/'ready').unlink()
        result=self.run_migration()
        self.assertEqual(result.returncode,0,result.stdout+result.stderr)
        self.assertEqual((self.runtime/'state/tailscaled.state').read_bytes(),self.state)

    def test_recovers_owned_interrupted_staging(self):
        stage=Path(str(self.runtime)+'.new');stage.mkdir()
        (stage/'owner').write_text('storage-v1\n')
        (stage/'partial').write_text('incomplete')
        result=self.run_migration()
        self.assertEqual(result.returncode,0,result.stdout+result.stderr)
        self.assertFalse(stage.exists())

    def test_interrupted_rollback_keeps_immutable_backups_for_retry(self):
        for index,label in enumerate(['boot','cli','daemon','menu']):
            with self.subTest(after=label):
                if index:
                    self.tearDown();self.setUp()
                self.assertEqual(self.run_migration().returncode,0)
                (self.runtime/'ready').unlink()
                target={'boot':self.system/'on-boot.sh','cli':self.usrbin/'tailscale',
                        'daemon':self.usrbin/'tailscaled','menu':self.menu}[label]
                interrupt=self.root/'interrupt-at';interrupt.write_text(str(target))
                wrapper='''#!/bin/sh
first=$1
/bin/mv "$@" || exit $?
for last; do :; done
if [ -f '{flag}' ] && [ "$last" = "$(cat '{flag}')" ]; then
    case "$*" in
        *'/backup/'*|*'.restore'*) rm -f '{flag}'; kill -KILL "$PPID" ;;
    esac
fi
exit 0
'''.format(flag=interrupt)
                self.executable(self.bin/'mv',wrapper)
                interrupted=self.run_migration()
                self.assertNotEqual(interrupted.returncode,0)
                self.assertFalse(interrupt.exists(),'The intended rollback step was not reached')
                self.lock.rmdir()  # Reboot clears the old transaction's /tmp lock.
                recovered=self.run_migration()
                self.assertEqual(recovered.returncode,0,recovered.stdout+recovered.stderr)
                self.assertEqual((self.runtime/'state/tailscaled.state').read_bytes(),self.state)
                self.assertIn('My custom item',self.menu.read_text())

    def test_unknown_runtime_is_never_removed(self):
        self.runtime.mkdir();(self.runtime/'custom').write_text('keep')
        result=self.run_migration()
        self.assertNotEqual(result.returncode,0)
        self.assertEqual((self.runtime/'custom').read_text(),'keep')

    def root_script(self,name):
        text=(PROJECT/'scripts'/name).read_text()
        self.assertNotIn('/mnt/onboard',text)
        for original,replacement in [('/usr/local/tailscale-runtime',self.runtime),
                                      ('/usr/local/tailscale',self.system),
                                      ('/var/run/tailscale',self.root/'run'),
                                      ('/tmp/kobo-tailscale-connect.lock',self.lock),
                                      ('/tmp/kobo-tailscale',self.root/'logs'),
                                      ('/tmp/tailscaled.log',self.root/'daemon.log'),
                                      ('/dev/net',self.root/'dev/net'),
                                      ('/proc/net/route',self.root/'routes')]:
            text=text.replace(original,str(replacement))
        text=text.replace('PATH=/sbin:/bin:/usr/sbin:/usr/bin','PATH='+self.env['PATH'])
        path=self.root/name;path.write_text(text);return path

    def test_actual_root_boot_has_no_books_partition_dependency(self):
        self.assertEqual(self.run_migration().returncode,0)
        shutil.move(self.onboard,self.root/'usb-exported')
        self.executable(self.bin/'mknod','#!/bin/sh\ntouch "$1"\n')
        result=subprocess.run(['/bin/sh',str(self.root_script('on-boot-root.sh'))],env=self.env,capture_output=True,text=True,timeout=3)
        self.assertEqual(result.returncode,0,result.stdout+result.stderr)
        for _ in range(100):
            if self.running():break
            time.sleep(.01)
        self.assertTrue(self.running())
        self.assertEqual(self.started.read_text(),str(self.runtime/'bin/tailscaled'))

    def test_actual_connect_runs_with_books_partition_absent(self):
        self.assertEqual(self.run_migration().returncode,0)
        shutil.move(self.onboard,self.root/'usb-exported')
        (self.root/'routes').write_text('wlan0 00000000 0100000A 0003\n')
        (self.root/'run').mkdir()
        server=socket.socket(socket.AF_UNIX,socket.SOCK_STREAM)
        self.addCleanup(server.close)
        server.bind(str(self.root/'run/tailscaled.sock'))
        result=subprocess.run(['/bin/sh',str(self.root_script('connect-root.sh'))],env=self.env,capture_output=True,text=True,timeout=4)
        self.assertEqual(result.returncode,0,result.stdout+result.stderr)
        self.assertFalse(self.lock.exists())
        self.assertTrue((self.root/'logs/auth.log').exists())


if __name__ == '__main__':
    unittest.main()
