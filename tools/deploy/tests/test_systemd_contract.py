"""Real manager/kernel contracts. Opt in as root with FOUNDRY_REAL_SYSTEMD=1.

Only probe-owned runtime units are changed. No Redis, app archive, or Go build is
needed. The in-memory state and role binding isolate deployment metadata; command
results, process inspection, cgroups, and all property parsers remain real.
"""
import os
from pathlib import Path
import signal
import socket
import sys
import tempfile
import time
import unittest
from unittest.mock import patch
import uuid

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'lib'))
import backup
import capacity
import compatibility
from contracts import DeployError
from host import Host
import packages
import redis_acl
import release
import report


TEMPLATES = Path(__file__).resolve().parents[1] / 'server/templates'
SCRIPT = Path(__file__).resolve()
EXIT_PROPERTIES = ('LoadState,ActiveState,SubState,MainPID,ControlPID,ControlGroup,Slice,Job,'
                   'Result,ExecMainCode,ExecMainStatus,ExecMainPID,'
                   'ExecMainStartTimestampMonotonic,InvocationID')


class ProbeHost(Host):
    def __init__(self):
        super().__init__()
        self.state = {}

    def load_state(self, entry):
        return self.state


def probe(mode, directory):
    """Small real processes with file barriers instead of timing-dependent sleeps."""
    root = Path(directory)
    if mode == 'restart':
        counter = root / 'starts'
        count = int(counter.read_text()) + 1 if counter.exists() else 1
        counter.write_text(str(count))
        if count == 1:
            raise SystemExit(23)
    if mode == 'wait':
        (root / 'waiting').touch()
        while not (root / 'continue').exists():
            time.sleep(.02)
        return
    if mode == 'log':
        print(root.name, flush=True)
        (root / 'logged').touch()
        return
    if mode == 'forking':
        read_fd, write_fd = os.pipe()
        if os.fork():
            os.close(write_fd)
            os.read(read_fd, 1)
            return
        os.close(read_fd)
        os.setsid()
        (root / 'daemon.pid').write_text(str(os.getpid()))
        os.write(write_fd, b'1')
        os.close(write_fd)
    if mode == 'notify':
        address = os.environ['NOTIFY_SOCKET']
        with socket.socket(socket.AF_UNIX, socket.SOCK_DGRAM) as connection:
            connection.sendto(b'READY=1', '\0' + address[1:] if address.startswith('@') else address)
    if mode == 'sigint':
        def interrupt(signum, frame):
            (root / 'interrupted').touch()
            raise SystemExit(130)
        signal.signal(signal.SIGINT, interrupt)
    if mode in ('tree', 'stubborn', 'mixed'):
        child = os.fork()
        name = 'parent' if child else 'child'
        def terminate(signum, frame):
            (root / (name + '.term')).touch()
            if mode == 'stubborn' or mode == 'mixed' and name == 'child':
                return
            raise SystemExit(130)
        signal.signal(signal.SIGTERM, terminate)
        (root / (name + '.pid')).write_text(str(os.getpid()))
    else:
        (root / 'ready').touch()
    while True:
        signal.pause()


def executor_probe(instance_id, operation_id, directory):
    root = Path(directory)
    host = Host()
    entry = {'profile': {'instance_id': instance_id}}
    release._require_executor_alone(host, entry, operation_id)
    child = os.fork()
    if child == 0:
        while True:
            signal.pause()
    try:
        try:
            release._require_executor_alone(host, entry, operation_id)
        except DeployError as error:
            if error.code != 'cutover_executor_unknown':
                raise
        else:
            raise AssertionError('A surviving executor child was accepted as quiescent')
    finally:
        os.kill(child, signal.SIGTERM)
        os.waitpid(child, 0)
    release._require_executor_alone(host, entry, operation_id)
    stopping = []
    def terminate(signum, frame):
        stopping.append(time.monotonic() + 2)
    signal.signal(signal.SIGTERM, terminate)
    (root / 'executor-ready').touch()
    while not (root / 'executor-exit').exists() and (not stopping or time.monotonic() < stopping[0]):
        time.sleep(.02)


@unittest.skipUnless(os.environ.get('FOUNDRY_REAL_SYSTEMD') == '1',
                     'requires FOUNDRY_REAL_SYSTEMD=1 and sudo on a systemd host')
class SystemdContractTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        if os.geteuid() != 0:
            raise AssertionError('FOUNDRY_REAL_SYSTEMD=1 requires root via sudo')
        if not Path('/run/systemd/system').is_dir() or not Path('/run/systemd/private').is_socket():
            raise AssertionError('Opted-in contract tests require the real system manager')
        if not Path('/sys/fs/cgroup/cgroup.controllers').is_file():
            raise AssertionError('Contract tests require the deployment cgroup-v2 environment')
        if Path('/proc/1/exe').resolve().name != 'systemd':
            raise AssertionError('Contract tests require systemd as PID 1')

    def setUp(self):
        self.host = ProbeHost()
        self.prefix = 'foundry-contract-probe-' + uuid.uuid4().hex[:12]
        self.units, self.files, self.slices = [], [], set()
        temporary = tempfile.TemporaryDirectory(prefix=self.prefix + '-', dir='/run')
        self.root = Path(temporary.name)
        self.addCleanup(temporary.cleanup)
        self.addCleanup(self.assert_no_probe_files)
        self.addCleanup(self.cleanup_units)

    def assert_no_probe_files(self):
        remaining = sorted(str(path) for path in Path('/run/systemd/system').rglob(self.prefix + '*'))
        remaining += [str(path) for unit in self.units
                      for path in (Path('/run/systemd/system') / unit,
                                   Path('/var/lib/systemd/timers') / ('stamp-' + unit))
                      if path.exists() or path.is_symlink()]
        self.assertEqual([], remaining, 'Probe-owned unit files, enablement links or timer stamps remain')

    def cleanup_units(self):
        # Register ownership before start/enable. Never use unit globs or reset
        # the host's failed-unit list. Cleanup failures must fail this test too.
        for unit in reversed(self.units):
            self.host.run(['systemctl', 'disable', '--runtime', unit], check=False, timeout=10)
            self.host.run(['systemctl', 'stop', unit], check=False, timeout=10)
            value = self.show(unit, 'MainPID,ControlPID,ActiveState')
            self.assertEqual('0', value.get('MainPID', '0'), unit)
            self.assertEqual('0', value.get('ControlPID', '0'), unit)
            self.assertIn(value['ActiveState'], ('inactive', 'failed'), unit)
            if unit.endswith('.timer'):
                self.host.run(['systemctl', 'clean', '--what=state', unit], timeout=5)
            self.host.run(['systemctl', 'reset-failed', unit], check=False, timeout=5)
        for path in reversed(self.files):
            path.unlink()
            if path.parent.name.endswith('.d'):
                path.parent.rmdir()
        self.host.run(['systemctl', 'daemon-reload'], timeout=10)
        for unit in self.slices:
            self.host.run(['systemctl', 'stop', unit], check=False, timeout=5)

    def tearDown(self):
        result = self._outcome.result
        if any(test is self or getattr(test, 'test_case', None) is self
               for test, _ in result.failures + result.errors):
            for unit in self.units:
                print(unit, self.show(unit), file=sys.stderr)
            if self.units:
                logs = self.host.run(['journalctl', *[arg for unit in self.units for arg in ('--unit', unit)],
                                      '--lines=80', '--no-pager', '--output=short-iso'], check=False, timeout=5)
                print(logs.stdout, file=sys.stderr)

    def show(self, unit, properties=EXIT_PROPERTIES):
        result = self.host.run(['systemctl', 'show', unit, '--property=' + properties, '--no-pager'],
                               check=False, timeout=5)
        return dict(line.split('=', 1) for line in result.stdout.splitlines() if '=' in line)

    def wait_for(self, check, *, timeout=5):
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            value = check()
            if value:
                return value
            time.sleep(.02)
        self.fail(f'Probe did not reach its barrier within {timeout:.1f} seconds: ' + self.prefix)

    def write_unit(self, name, content, *, instance=None):
        path = Path('/run/systemd/system') / name
        with path.open('x') as stream:
            self.files.append(path)
            stream.write(content)
        unit = instance or name
        self.units.append(unit)
        if '@' in unit:
            self.slices.add(compatibility.default_cgroup(unit)[0])
        self.host.run(['systemctl', 'daemon-reload'], timeout=10)
        return unit

    def command(self, mode, directory=None):
        return f'/usr/bin/python3 -I {SCRIPT} --probe {mode} {directory or self.root}'

    def service(self, suffix, *, command='/usr/bin/sleep infinity', options='', template=False):
        name = self.prefix + '-' + suffix
        unit = name + ('@instance-one.service' if template else '.service')
        return self.write_unit(name + ('@.service' if template else '.service'),
                               '[Unit]\nDescription=Foundry contract probe\n'
                               '[Service]\nType=exec\nRestart=no\nTimeoutStopSec=1s\n'
                               'KillMode=control-group\nSuccessExitStatus=130\n'
                               f'ExecStart={command}\n{options}\n'
                               '[Install]\nWantedBy=multi-user.target\n',
                               instance=unit)

    def prove(self, unit, *, review=False):
        # Bind the arbitrary probe name, never fake systemctl or kernel evidence.
        with patch.object(compatibility, 'selected_units', return_value={'http': unit}):
            return compatibility.prove_stopped(self.host, {}, review=review)['http']

    def assert_code(self, code, action):
        with self.assertRaises(DeployError) as raised:
            action()
        self.assertEqual(code, raised.exception.code)

    def test_template_and_plain_cgroups_running_and_stopped(self):
        self.assertGreater(capacity.memory_mib(self.host), 0)
        for template in (False, True):
            with self.subTest(template=template):
                unit = self.service('placement', template=template)
                self.host.run(['systemctl', 'start', unit])
                live = release.service(self.host, unit)
                self.assertEqual(('loaded', 'active', 'running', '0'),
                                 tuple(live[key] for key in ('LoadState', 'ActiveState', 'SubState', 'NRestarts')))
                self.assertTrue(release._matches_executable(live, Path('/usr/bin/sleep')))
                expected_slice, expected_group = compatibility.default_cgroup(unit)
                self.assertEqual(expected_group, live['ControlGroup'])
                self.assertEqual(expected_slice, self.show(unit)['Slice'])
                self.assertIn(live['MainPID'], Path('/sys/fs/cgroup' + expected_group, 'cgroup.procs').read_text().split())
                self.assert_code('cutover_stop_unproven', lambda: self.prove(unit))
                self.assert_code('cutover_stop_unproven', lambda: compatibility._empty_cgroup(self.host, expected_group))
                invocation = release._role_invocation(self.host, unit)
                self.assertRegex(invocation, r'^[a-f0-9]{32}$')
                self.assertTrue(capacity._running_invocation(capacity._unit_state(self.host, unit)))
                self.assertEqual(live['MainPID'], packages._service(self.host, unit)['MainPID'])
                self.host.run(['systemctl', 'restart', unit])
                self.assertNotEqual(invocation, release._role_invocation(self.host, unit))
                with patch.object(release, 'selected_units', return_value={'http': unit}):
                    release.stop(self.host, {}, role='http')
                stopped = self.prove(unit)
                self.assertEqual(('inactive', 'dead', '0', '0', '', 'success'),
                                 tuple(stopped[key] for key in ('ActiveState', 'SubState', 'MainPID',
                                                               'ControlPID', 'ControlGroup', 'Result')))
                self.assertIn(stopped['Job'], ('', '0'))
                if template:
                    # Mutation proof against real observed properties. v0.15.0
                    # required system.slice when a stopped ControlGroup was empty.
                    with patch.object(compatibility, 'default_cgroup',
                                      return_value=('system.slice', '/system.slice/' + unit)):
                        self.assert_code('cutover_stop_unproven', lambda: self.prove(unit))

    def test_prefix_escaping_matches_systemd_escape(self):
        for prefix in ('foundry-http', '.probe', 'a/b', 'a_b.c:9', 'caf\u00e9', r'a\x2db'):
            with self.subTest(prefix=prefix):
                escaped = self.host.run(['systemd-escape', '--', prefix]).stdout.strip()
                self.assertEqual(escaped, compatibility._escape_unit_prefix(prefix))

    def test_loaded_fragment_dropins_reload_and_enablement(self):
        unit = self.service('metadata', template=True)
        # Inactive units can be garbage-collected and freshly loaded by show.
        # Keep this unit active so the edit really differs from a retained load.
        self.host.run(['systemctl', 'start', unit])
        properties = 'LoadState,ActiveState,FragmentPath,MainPID,UnitFileState,DropInPaths,NeedDaemonReload'
        initial = self.show(unit, properties)
        self.assertEqual(str(self.files[0]), initial['FragmentPath'])
        self.assertEqual(('', 'no', 'disabled'),
                         tuple(initial[key] for key in ('DropInPaths', 'NeedDaemonReload', 'UnitFileState')))
        with patch.object(redis_acl, 'REDIS_UNIT', unit):
            observed = redis_acl._unit(self.host)
        self.assertEqual(initial['FragmentPath'], observed['FragmentPath'])
        # Set a distinctly later mtime: no dependence on filesystem timestamp resolution.
        path = self.files[0]
        path.write_text(path.read_text().replace('Description=Foundry contract probe',
                                                'Description=Foundry contract edited'))
        stamp = path.stat().st_mtime + 2
        os.utime(path, (stamp, stamp))
        self.wait_for(lambda: self.show(unit, properties)['NeedDaemonReload'] == 'yes')
        self.assertEqual('Foundry contract probe', self.show(unit, 'Description')['Description'])
        dropin = Path('/run/systemd/system') / (unit + '.d') / 'probe.conf'
        dropin.parent.mkdir()
        with dropin.open('x') as stream:
            self.files.append(dropin)
            stream.write('[Service]\nEnvironment=FOUNDRY_CONTRACT_PROBE=1\n')
        self.host.run(['systemctl', 'daemon-reload'])
        self.assertEqual(str(dropin), self.show(unit, properties)['DropInPaths'])
        self.assertEqual('no', self.show(unit, properties)['NeedDaemonReload'])
        self.assertEqual('Foundry contract edited', self.show(unit, 'Description')['Description'])
        self.host.run(['systemctl', 'enable', '--runtime', unit])
        self.assertEqual('enabled-runtime', self.host.run(['systemctl', 'is-enabled', unit]).stdout.strip())
        with patch.object(release, 'instance_units', return_value={'http': unit}):
            self.assertEqual(['http'], release.enabled_roles(self.host, {}))
        self.host.run(['systemctl', 'disable', '--runtime', unit])
        self.assertNotEqual(0, self.host.run(['systemctl', 'is-enabled', unit], check=False).returncode)
        with patch.object(release, 'instance_units', return_value={'http': unit}):
            self.assertEqual([], release.enabled_roles(self.host, {}))
        missing = self.prefix + '-missing.service'
        self.assertEqual('not-found', packages._service(self.host, missing)['LoadState'])
        masked = self.prefix + '-masked.service'
        mask = Path('/run/systemd/system') / masked
        mask.symlink_to('/dev/null')
        self.files.append(mask)
        self.host.run(['systemctl', 'daemon-reload'])
        self.assertEqual('masked', packages._service(self.host, masked)['LoadState'])

    def test_prestart_refusal_never_runs_main_and_start_limit_is_visible(self):
        unit = self.write_unit(self.prefix + '-guard@.service',
                               '[Unit]\nStartLimitIntervalSec=60\nStartLimitBurst=2\n'
                               '[Service]\nType=exec\nUser=nobody\nRestart=no\n'
                               f'ExecStartPre=+/usr/bin/touch {self.root}/pre-root\n'
                               'ExecStartPre=+/usr/bin/false\n'
                               f'ExecStart={self.command("log")}\n',
                               instance=self.prefix + '-guard@one.service')
        for attempt in range(3):
            marker = self.root / 'pre-root'
            marker.unlink(missing_ok=True)
            self.assertNotEqual(0, self.host.run(['systemctl', 'start', unit], check=False).returncode)
            value = self.prove(unit, review=True)
            self.assertEqual(('failed', 'failed'), tuple(value[key] for key in ('ActiveState', 'SubState')))
            # A rate-limited start can retain the previous exit-code result.
            # Prove refusal by the absence of a new pre-start execution instead.
            self.assertIn(value['Result'], ('exit-code',) if attempt < 2 else ('exit-code', 'start-limit-hit'))
            for key in ('MainPID', 'ControlPID', 'ExecMainPID', 'ExecMainCode',
                        'ExecMainStatus', 'ExecMainStartTimestampMonotonic'):
                self.assertEqual('0', value[key], key)
            self.assertFalse((self.root / 'logged').exists())
            self.assertEqual(attempt < 2, marker.exists())
            self.assert_code('cutover_drain_review', lambda: self.prove(unit))
        self.assertIn(unit, report.failed_units(self.host, [], time.monotonic() + 5))
        self.host.run(['systemctl', 'reset-failed', unit])
        self.assertEqual('inactive', self.show(unit)['ActiveState'])

    def test_pending_control_process_and_oneshot_completion(self):
        unit = self.service('pending', command='/usr/bin/true',
                            options=f'ExecStartPre={self.command("wait")}\nRemainAfterExit=yes')
        self.host.run(['systemctl', 'start', '--no-block', unit])
        self.wait_for(lambda: (self.root / 'waiting').exists())
        value = self.show(unit)
        self.assertEqual('activating', value['ActiveState'])
        self.assertGreater(int(value['ControlPID']), 0)
        self.assertNotIn(value['Job'], ('', '0'))
        self.assert_code('capacity_restart_pending', lambda: capacity._unit_state(self.host, unit))
        self.assert_code('cutover_stop_unproven', lambda: self.prove(unit))
        (self.root / 'continue').touch()
        self.wait_for(lambda: self.show(unit)['SubState'] == 'exited')
        self.assertEqual('active', self.show(unit)['ActiveState'])
        backup._stop_drill(self.host, unit)
        self.prove(unit)
        oneshot = self.write_unit(self.prefix + '-oneshot.service',
                                  '[Service]\nType=oneshot\nRemainAfterExit=yes\nExecStart=/usr/bin/true\n')
        self.assertEqual('static', self.show(oneshot, 'UnitFileState')['UnitFileState'])
        self.assertEqual('0', self.show(oneshot)['ExecMainStartTimestampMonotonic'])
        self.host.run(['systemctl', 'start', oneshot])
        value = self.show(oneshot)
        self.assertEqual(('active', 'exited', 'success', '1', '0', '0'),
                         tuple(value[key] for key in ('ActiveState', 'SubState', 'Result',
                                                     'ExecMainCode', 'ExecMainStatus', 'MainPID')))
        self.assertGreater(int(value['ExecMainStartTimestampMonotonic']), 0)
        backup._stop_drill(self.host, oneshot)

    def test_failed_main_and_clean_exit_130(self):
        unit = self.service('exit', command='/usr/bin/sh -c "exit 23"')
        self.host.run(['systemctl', 'start', unit], check=False)
        self.wait_for(lambda: self.show(unit)['ActiveState'] == 'failed')
        value = self.prove(unit, review=True)
        self.assertEqual(('exit-code', '1', '23'),
                         tuple(value[key] for key in ('Result', 'ExecMainCode', 'ExecMainStatus')))
        self.assertGreater(int(value['ExecMainPID']), 0)
        self.assertGreater(int(value['ExecMainStartTimestampMonotonic']), 0)
        self.assert_code('cutover_drain_review', lambda: self.prove(unit))
        clean = self.service('clean', command='/usr/bin/sh -c "exit 130"', options='RemainAfterExit=yes')
        self.host.run(['systemctl', 'start', clean])
        self.wait_for(lambda: self.show(clean)['SubState'] == 'exited')
        self.assertEqual('success', self.show(clean)['Result'])
        self.assertEqual('130', self.show(clean)['ExecMainStatus'])
        self.host.run(['systemctl', 'stop', clean])
        self.prove(clean)

    def test_automatic_restart_counter_and_invocation(self):
        unit = self.service('restart', command=self.command('restart'),
                            options='Restart=on-failure\nRestartSec=50ms')
        self.host.run(['systemctl', 'start', unit])
        self.wait_for(lambda: (self.root / 'ready').exists())
        value = release.service(self.host, unit)
        self.assertEqual(('active', 'running', '1'),
                         tuple(value[key] for key in ('ActiveState', 'SubState', 'NRestarts')))
        self.assertEqual('2', (self.root / 'starts').read_text())
        self.assertTrue(capacity._running_invocation(capacity._unit_state(self.host, unit)))
        self.host.run(['systemctl', 'stop', unit])
        self.prove(unit)

    def test_explicit_interrupt_signal(self):
        unit = self.service('interrupt', command=self.command('sigint'), options='KillSignal=SIGINT')
        self.host.run(['systemctl', 'start', unit])
        self.wait_for(lambda: (self.root / 'ready').exists())
        self.host.run(['systemctl', 'stop', unit])
        self.assertTrue((self.root / 'interrupted').exists())
        self.prove(unit)

    def test_stop_owns_children_and_timeout_requires_review(self):
        for mode in ('tree', 'stubborn', 'mixed'):
            with self.subTest(mode=mode):
                for path in self.root.iterdir():
                    path.unlink()
                unit = self.service(mode, command=self.command(mode),
                                    options='TimeoutStopSec=300ms\n' + ('KillMode=mixed\n' if mode == 'mixed' else ''))
                self.host.run(['systemctl', 'start', unit])
                self.wait_for(lambda: all((self.root / (name + '.pid')).is_file()
                                          and (self.root / (name + '.pid')).read_text().isdigit()
                                          for name in ('parent', 'child')))
                pids = [int((self.root / (name + '.pid')).read_text()) for name in ('parent', 'child')]
                self.host.run(['systemctl', 'stop', unit])
                value = self.prove(unit, review=True)
                self.assertTrue((self.root / 'parent.term').exists())
                self.assertEqual(mode != 'mixed', (self.root / 'child.term').exists())
                for pid in pids:
                    self.wait_for(lambda pid=pid: not Path(f'/proc/{pid}').exists())
                if mode == 'stubborn':
                    self.assertEqual(('timeout', '2', '9'),
                                     tuple(value[key] for key in ('Result', 'ExecMainCode', 'ExecMainStatus')))
                    self.assert_code('cutover_drain_review', lambda: self.prove(unit))
                if mode == 'tree':
                    self.prove(unit)

    def test_transient_executor_identity_liveness_and_descendants(self):
        entry = {'profile': {'instance_id': str(uuid.uuid4())}}
        operation = str(uuid.uuid4())
        unit = release.cutover_unit(entry, operation)
        self.assertEqual('absent', release.cutover_executor(self.host, entry, operation))
        self.units.append(unit)
        runner = self.root / 'server/cutover_runner.py'
        runner.parent.mkdir()
        runner.write_text('import runpy, sys\n'
                          f'runpy.run_path({str(SCRIPT)!r})["executor_probe"]('
                          f'sys.argv[1], sys.argv[2], {str(self.root)!r})\n')
        plan = {'tools_path': str(self.root)}
        release._submit_cutover(self.host, entry, plan, operation)
        self.wait_for(lambda: (self.root / 'executor-ready').exists())
        self.assertEqual('running', release.cutover_executor(self.host, entry, operation))
        invocation = release._role_invocation(self.host, unit)
        self.assertTrue(release.cutover_executor_admits_start(self.host, entry, operation, invocation))
        self.assertFalse(release.cutover_executor_admits_start(self.host, entry, operation, '0' * 32))
        properties = self.show(unit, 'Transient,Description,KillMode,RemainAfterExit,ControlGroup')
        self.assertEqual('yes', properties['Transient'])
        self.assertEqual('control-group', properties['KillMode'])
        self.assertEqual('yes', properties['RemainAfterExit'])
        self.assertEqual('/system.slice/' + unit, properties['ControlGroup'])
        release._submit_cutover(self.host, entry, plan, operation)
        self.assertEqual(invocation, release._role_invocation(self.host, unit))
        (self.root / 'executor-exit').touch()
        self.wait_for(lambda: release.cutover_executor(self.host, entry, operation) == 'exited')
        self.assertEqual('exited', self.show(unit)['SubState'])
        self.assertFalse(release.cutover_executor_admits_start(self.host, entry, operation, invocation))
        self.host.run(['systemctl', 'stop', unit])
        self.host.run(['systemctl', 'reset-failed', unit], check=False)
        (self.root / 'executor-ready').unlink()
        (self.root / 'executor-exit').unlink()
        release._submit_cutover(self.host, entry, plan, operation)
        self.wait_for(lambda: (self.root / 'executor-ready').exists())
        self.assertNotEqual(invocation, release._role_invocation(self.host, unit))
        invocation = release._role_invocation(self.host, unit)
        self.host.run(['systemctl', 'stop', '--no-block', unit])
        self.wait_for(lambda: self.show(unit)['ActiveState'] == 'deactivating')
        self.assertEqual('running', release.cutover_executor(self.host, entry, operation))
        self.assertFalse(release.cutover_executor_admits_start(self.host, entry, operation, invocation))
        (self.root / 'executor-exit').touch()
        self.wait_for(lambda: release.cutover_executor(self.host, entry, operation) != 'running')

    def test_notify_and_forking_service_readiness(self):
        for mode in ('notify', 'forking'):
            with self.subTest(mode=mode):
                unit = self.write_unit(self.prefix + '-' + mode + '.service',
                                       f'[Service]\nType={mode}\nExecStart={self.command(mode)}\n'
                                       'TimeoutStartSec=5s\nTimeoutStopSec=1s\n'
                                       + (f'PIDFile={self.root}/daemon.pid\n' if mode == 'forking' else ''))
                self.host.run(['systemctl', 'start', unit])
                self.assertTrue(capacity._running_invocation(capacity._unit_state(self.host, unit)))
                self.assertGreater(int(packages._service(self.host, unit)['MainPID']), 0)
                self.host.run(['systemctl', 'stop', unit])
                self.prove(unit)

    def test_timer_templates_execstart_and_journal_selection(self):
        for kind in ('backup', 'certbot-renew'):
            with self.subTest(kind=kind):
                name = self.prefix + '-' + kind
                service = name + '.service'
                timer = name + '.timer'
                content = (TEMPLATES / ('foundry-' + kind + '.timer')).read_text()
                content = content.replace('foundry-backup-@TASK@-@IDENTITY@.service', service)
                content = content.replace('@TASK@', 'full').replace('@IDENTITY@', 'contract')
                content = content.replace('@CALENDAR@', 'Sun *-*-* 02:00:00 UTC').replace('@JITTER_SECONDS@', '0')
                # Keep the production calendar/persistence syntax; an additional
                # monotonic trigger exercises dispatch without waiting for a date.
                # Zero random delay does not override the default 1-minute accuracy.
                content += '\n[Timer]\nOnActiveSec=200ms\nRandomizedDelaySec=0\nAccuracySec=1ms\n'
                directory = self.root / (name + '-message')
                directory.mkdir()
                probe_command = self.command('log', directory)
                self.write_unit(service, '[Service]\nType=oneshot\nStandardOutput=journal\n'
                                f'ExecStart={probe_command}\n')
                self.write_unit(timer, content)
                command = self.host.run(['systemctl', 'show', service, '--property=ExecStart', '--value']).stdout
                self.assertIn(probe_command, command)
                selected = self.host.run(['systemctl', 'show', timer, '--property=Unit', '--value']).stdout.strip()
                self.assertEqual(service, selected)
                self.host.run(['systemctl', 'enable', '--runtime', '--now', timer])
                self.assertEqual(0, self.host.run(['systemctl', 'is-active', '--quiet', timer]).returncode)
                self.assertEqual(0, self.host.run(['systemctl', 'is-enabled', timer]).returncode)
                deadline = time.monotonic() + 12
                self.wait_for(lambda: (directory / 'logged').exists(), timeout=12)
                def journal():
                    return self.host.run(['journalctl', '--unit', service, '--lines=100', '--no-pager',
                                          '--output=short-iso'], timeout=5).stdout
                self.wait_for(lambda: directory.name in journal(), timeout=max(0, deadline - time.monotonic()))
                self.assertRegex(journal(), r'\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d')


if __name__ == '__main__':
    if len(sys.argv) == 4 and sys.argv[1] == '--probe':
        probe(sys.argv[2], sys.argv[3])
    else:
        unittest.main()
