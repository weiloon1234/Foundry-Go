"""Generation-5 bridge and retryable cleanup boundaries; no host services."""
import copy
import importlib.util
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch
import uuid

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'lib'))
from contracts import DeployError, atomic_json, instance_paths, read_json
from host import Host, Paths
import release
import state_cleanup
import upgrades
from test_contracts import fixture, registry
import test_release

TOOLS = Path(__file__).resolve().parents[1]
STUBS = ('lib/compatibility.py', 'lib/cutover_contracts.py', 'server/start_check.py', 'server/cutover_runner.py')


class CleanupTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name).resolve()
        self.host = Host(Paths(self.root))
        self.project, self.profile, self.entry = fixture()
        self.host.write_registry(registry(self.entry))
        self.state = self.host.load_state(self.entry)
        self.state.update(cutover={'outcome': 'succeeded', 'hold': False, 'drain_failed': False},
                          queue_compatibility={'floor': 3, 'permission': 'admitted', 'admitted': {'a': '1', 'b': '2'}})
        self.path = self.host.paths.instance_state(self.entry['name']) / 'state.json'
        atomic_json(self.path, self.state)
        self.private = self.host.paths.p(instance_paths(self.entry['name'])['private'])
        self.private.mkdir(parents=True)
        self.records = [self.private / ('cutover-' + kind + '-' + str(uuid.uuid4()) + '.json')
                        for kind in ('plan', 'operation', 'snapshot')]
        for path in self.records:
            path.write_text('{}')
        self.unit = 'foundry-cutover-' + self.profile['instance_id'] + '-' + str(uuid.uuid4()) + '.service'
        self.other_unit = 'foundry-cutover-' + str(uuid.uuid4()) + '-' + str(uuid.uuid4()) + '.service'
        self.failed = {self.unit, self.other_unit, 'foreign.service'}
        self.exited_unit = 'foundry-cutover-' + self.profile['instance_id'] + '-' + str(uuid.uuid4()) + '.service'
        self.other_exited_unit = 'foundry-cutover-' + str(uuid.uuid4()) + '-' + str(uuid.uuid4()) + '.service'
        self.exited = set()
        self.observations = {}
        self.commands = []
        self.run = patch.object(self.host, 'run', side_effect=self.command).start()
        self.addCleanup(patch.stopall)

    def command(self, args, **kwargs):
        self.commands.append(args)
        output = ''
        if args[:2] == ['systemctl', 'list-units']:
            output = ''.join(unit + ' loaded failed failed description\n' for unit in sorted(self.failed))
            output += ''.join(unit + ' loaded active exited description\n' for unit in sorted(self.exited))
        elif args[:2] == ['systemctl', 'show']:
            observed = {'ActiveState': 'active' if args[2] in self.exited else 'failed',
                        'SubState': 'exited' if args[2] in self.exited else 'failed',
                        'Result': 'success' if args[2] in self.exited else 'exit-code',
                        'MainPID': '0', 'ControlPID': '0', 'Job': ''}
            observed.update(self.observations.get(args[2], {}))
            output = ''.join(key + '=' + value + '\n' for key, value in observed.items())
        elif args[:2] == ['systemctl', 'reset-failed']:
            self.failed.discard(args[2])
        elif args[:2] == ['systemctl', 'stop']:
            self.exited.discard(args[2])
        return subprocess.CompletedProcess(args, 0, output, '')

    def test_mutating_load_strips_only_owned_records_and_exact_units_idempotently(self):
        _, _, other_entry = fixture(project_slug='xyz', host_id=self.profile['host']['host_id'], port=8101)
        self.host.write_registry(registry(self.entry, other_entry))
        other_state = self.host.load_state(other_entry)
        other_state.update(cutover=self.state['cutover'], queue_compatibility=self.state['queue_compatibility'])
        other_path = self.host.paths.instance_state(other_entry['name']) / 'state.json'
        atomic_json(other_path, other_state)
        other_before = other_path.read_bytes()
        other = self.host.paths.p(instance_paths(other_entry['name'])['private'])
        other.mkdir(parents=True)
        sentinel = other / self.records[0].name
        sentinel.write_text('foreign')
        untouched = [self.private / 'backup.json', self.private / 'cutover-restore-other.json',
                     self.root / 'off-host-snapshot.json.age']
        for path in untouched:
            path.write_text('retain')
        state = self.host.load_state(self.entry, mutating=True)
        self.assertNotIn('cutover', state)
        self.assertNotIn('queue_compatibility', read_json(self.path))
        self.assertFalse(any(path.exists() for path in self.records))
        self.assertEqual('foreign', sentinel.read_text())
        self.assertEqual(other_before, other_path.read_bytes())
        self.assertTrue(all(path.read_text() == 'retain' for path in untouched))
        self.assertEqual({self.other_unit, 'foreign.service'}, self.failed)
        self.assertEqual([['systemctl', 'reset-failed', self.unit]],
                         [args for args in self.commands if args[1] == 'reset-failed'])
        before, commands = self.path.read_bytes(), list(self.commands)
        self.host.load_state(self.entry, mutating=True)
        self.assertEqual(before, self.path.read_bytes())
        self.assertEqual(commands, self.commands)

    def test_ooo_executors_allow_upgrade_and_cleanup_exact_names(self):
        self.exited.update((self.exited_unit, self.other_exited_unit))
        before = self.path.read_bytes()
        state_cleanup.check_upgrade(self.host, self.entry)
        self.assertEqual(before, self.path.read_bytes())
        self.assertTrue(all(path.exists() for path in self.records))
        self.assertFalse(any(args[1] in ('reset-failed', 'stop') for args in self.commands))
        self.host.load_state(self.entry, mutating=True)
        mutations = [args for args in self.commands if args[1] in ('reset-failed', 'stop')]
        self.assertCountEqual([['systemctl', 'reset-failed', self.unit],
                               ['systemctl', 'stop', self.exited_unit]], mutations)
        self.assertEqual({self.other_unit, 'foreign.service'}, self.failed)
        self.assertEqual({self.other_exited_unit}, self.exited)
        self.assertNotIn('cutover', read_json(self.path))
        self.assertNotIn('queue_compatibility', read_json(self.path))
        self.assertFalse(any(path.exists() for path in self.records))
        before, commands = self.path.read_bytes(), list(self.commands)
        self.host.load_state(self.entry, mutating=True)
        self.assertEqual(before, self.path.read_bytes())
        self.assertEqual(commands, self.commands)

    def test_show_properties_override_stale_list_columns(self):
        self.exited.add(self.exited_unit)
        self.observations[self.exited_unit] = {'ActiveState': 'inactive', 'SubState': 'dead'}
        state_cleanup.check_upgrade(self.host, self.entry)
        self.host.load_state(self.entry, mutating=True)
        self.assertFalse(any(args[1] == 'stop' for args in self.commands))

    def test_unfinished_executor_properties_refuse_before_any_cleanup(self):
        self.exited.add(self.exited_unit)
        for unit in (self.unit, self.exited_unit):
            for changes in ({'ActiveState': 'active', 'SubState': 'running'}, {'MainPID': '42'},
                            {'ControlPID': '43'}, {'Job': '123'},
                            {'ActiveState': 'activating'}, {'ActiveState': 'deactivating'}):
                with self.subTest(unit=unit, changes=changes):
                    self.observations = {unit: changes}
                    before = self.path.read_bytes()
                    for action in (lambda: state_cleanup.check_upgrade(self.host, self.entry),
                                   lambda: self.host.load_state(self.entry, mutating=True)):
                        with self.assertRaises(DeployError) as caught:
                            action()
                        self.assertEqual('state_cleanup_pending', caught.exception.code)
                    self.assertEqual(before, self.path.read_bytes())
                    self.assertTrue(all(path.exists() for path in self.records))
                    self.assertFalse(any(args[1] in ('stop', 'reset-failed') for args in self.commands))

    def test_crash_after_stopping_exited_executor_retries_before_state_publication(self):
        self.exited.add(self.exited_unit)
        def interrupt(args, **kwargs):
            result = self.command(args, **kwargs)
            if args[1] == 'stop':
                raise KeyboardInterrupt()
            return result
        self.run.side_effect = interrupt
        with self.assertRaises(KeyboardInterrupt):
            self.host.load_state(self.entry, mutating=True)
        self.assertNotIn(self.exited_unit, self.exited)
        self.assertIn('cutover', read_json(self.path))
        self.assertTrue(all(path.exists() for path in self.records))
        self.run.side_effect = self.command
        self.host.load_state(self.entry, mutating=True)
        self.assertNotIn(self.unit, self.failed)
        self.assertNotIn('cutover', read_json(self.path))
        self.assertEqual([['systemctl', 'stop', self.exited_unit]],
                         [args for args in self.commands if args[1] == 'stop'])

    def test_read_only_load_status_and_report_leave_state_files_and_units_unchanged(self):
        spec = importlib.util.spec_from_file_location('cleanup_manager', TOOLS / 'server/manage.py')
        manager = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(manager)
        before = self.path.read_bytes()
        with patch.object(release, 'services', return_value={}):
            status = manager.execute(self.host, 'status', {'project': self.project, 'profile': self.profile})
        self.assertNotIn('cutover', status)
        self.assertNotIn('cutover', status['state'])
        self.assertNotIn('queue_compatibility', status['state'])
        self.assertEqual(self.state['cutover'], self.host.load_state(self.entry)['cutover'])
        with patch('report.collect_report', side_effect=lambda host: host.load_state(self.entry)):
            manager.execute(self.host, 'report', {})
        self.assertEqual(before, self.path.read_bytes())
        self.assertTrue(all(path.exists() for path in self.records))
        self.run.assert_not_called()

    def test_save_strips_existing_state(self):
        self.host.save_state(self.entry, self.state)
        self.assertNotIn('cutover', read_json(self.path))
        self.assertNotIn(self.unit, self.failed)

    def test_save_with_fields_omitted_still_cleans_persisted_state(self):
        self.records[0].unlink()
        self.records[1].unlink()
        self.records[2].unlink()
        incoming = copy.deepcopy(self.state)
        state_cleanup.strip_fields(incoming)
        self.host.save_state(self.entry, incoming)
        self.assertNotIn(self.unit, self.failed)
        self.assertNotIn('cutover', read_json(self.path))

    def test_crash_midway_through_private_deletion_retries_from_persisted_state(self):
        unlink = Path.unlink
        count = 0
        def interrupt(path, *args, **kwargs):
            nonlocal count
            if path in self.records:
                count += 1
                if count == 2:
                    raise KeyboardInterrupt()
            return unlink(path, *args, **kwargs)
        with patch.object(Path, 'unlink', interrupt), self.assertRaises(KeyboardInterrupt):
            self.host.load_state(self.entry, mutating=True)
        self.assertIn('cutover', read_json(self.path))
        self.assertEqual(2, sum(path.exists() for path in self.records))
        self.host.load_state(self.entry, mutating=True)
        self.assertFalse(any(path.exists() for path in self.records))
        self.assertNotIn('queue_compatibility', read_json(self.path))

    def test_crash_before_state_publication_retries_with_no_private_records(self):
        with patch('host.atomic_json', side_effect=KeyboardInterrupt), self.assertRaises(KeyboardInterrupt):
            self.host.load_state(self.entry, mutating=True)
        self.assertIn('cutover', read_json(self.path))
        self.assertFalse(any(path.exists() for path in self.records))
        self.host.load_state(self.entry, mutating=True)
        self.assertNotIn('cutover', read_json(self.path))

    def test_reset_failure_retains_retry_trigger_and_records(self):
        def fail(args, **kwargs):
            if args[1] == 'reset-failed':
                raise DeployError('command_failed', 'injected')
            return self.command(args, **kwargs)
        self.run.side_effect = fail
        with self.assertRaises(DeployError):
            self.host.load_state(self.entry, mutating=True)
        self.assertIn('cutover', read_json(self.path))
        self.assertTrue(all(path.exists() for path in self.records))
        self.run.side_effect = self.command
        self.host.load_state(self.entry, mutating=True)
        self.assertNotIn('cutover', read_json(self.path))

    def test_unfinished_operation_or_live_executor_blocks_upgrade_without_mutation(self):
        for progress in ({'outcome': 'running'}, {'outcome': 'needs_reconciliation'},
                         {'outcome': 'succeeded', 'hold': True}, {'outcome': 'succeeded', 'drain_failed': True}):
            state = {**self.state, 'cutover': progress}
            atomic_json(self.path, state)
            with self.assertRaises(DeployError) as caught:
                state_cleanup.check_upgrade(self.host, self.entry)
            self.assertEqual('state_cleanup_pending', caught.exception.code)
            self.assertEqual(state, read_json(self.path))
        atomic_json(self.path, self.state)
        self.run.return_value = subprocess.CompletedProcess([], 0, self.unit + ' loaded active running description\n', '')
        self.run.side_effect = None
        with self.assertRaises(DeployError):
            state_cleanup.check_upgrade(self.host, self.entry)
        self.assertTrue(all(path.exists() for path in self.records))

    def test_symlink_records_refuse_without_deleting_target(self):
        target = self.root / 'foreign'
        target.write_text('retain')
        self.records[0].unlink()
        self.records[0].symlink_to(target)
        with self.assertRaises(DeployError):
            self.host.load_state(self.entry, mutating=True)
        self.assertEqual('retain', target.read_text())
        self.assertIn('cutover', read_json(self.path))

    def test_prune_has_no_cutover_or_plan_protection(self):
        releases = self.host.paths.opt(self.entry['name']) / 'releases'
        releases.mkdir(parents=True)
        versions = []
        for sequence in range(1, 5):
            _, manifest = test_release.ReleaseTests.bundle(self, sequence)
            version = manifest['version']
            versions.append(version)
            (releases / version).mkdir()
            atomic_json(releases / version / 'manifest.json', manifest)
        self.entry['profile']['retention']['server_releases'] = 1
        self.state['cutover'].update(source=versions[0], target=versions[1])
        self.records[0].write_text('{"target":{"version":"' + versions[2] + '"}}')
        release.prune(self.host, self.entry, self.state)
        self.assertEqual([versions[-1]], [path.name for path in releases.iterdir()])


class BridgeTests(unittest.TestCase):
    def test_bundle_contains_all_four_bridge_files_and_start_check_exits_zero(self):
        source, files, _ = upgrades.tool_bundle(TOOLS)
        self.assertTrue(set(STUBS) <= {path.relative_to(source).as_posix() for path in files})
        result = subprocess.run([sys.executable, '-I', str(TOOLS / 'server/start_check.py'), 'instance', 'http'],
                                capture_output=True, timeout=10)
        self.assertEqual((0, b'', b''), (result.returncode, result.stdout, result.stderr))
        for role in ('http', 'scheduler', 'worker'):
            self.assertNotIn('ExecStartPre=', (TOOLS / 'server/templates' / ('foundry-' + role + '@.service')).read_text())
        self.assertEqual(6, upgrades.protocol()['generation'])
        self.assertEqual(1, upgrades.protocol()['minimum_client_generation'])
