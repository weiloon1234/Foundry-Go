"""Queue fences survive process restart, history pruning and recovery shortcuts."""
import copy
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch
import uuid

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "lib"))

import compatibility
from contracts import DeployError, now
from host import Host, Paths
import release
from test_contracts import fixture, registry
import test_release
from topology import instance_units


def progress(operation):
    return {'schema': 1, 'operation_id': operation, 'plan_token': 'a' * 64, 'action': 'cutover',
            'source': None, 'target': None, 'phase': 'stopping', 'outcome': 'needs_reconciliation',
            'started_at': now(), 'rollback_prohibited': False, 'drain_failed': False, 'hold': False,
            'queues': [], 'database': 'not_started', 'seed': 'not_started', 'error': None}


class CompatibilityTests(unittest.TestCase):
    setUp = test_release.ReleaseTests.setUp
    bundle = test_release.ReleaseTests.bundle
    deploy = test_release.ReleaseTests.deploy
    recovery_point = test_release.ReleaseTests.recovery_point
    reconcile_grants = test_release.ReleaseTests.reconcile_grants

    def install(self):
        archive, manifest = self.bundle()
        self.deploy(archive)
        state = self.host.load_state(self.entry)
        state['queue_compatibility'] = {'schema': 1, 'floor': 3, 'permission': 'admitted',
                                       'admitted': {manifest['version']: manifest['files']['artisan']}}
        self.host.save_state(self.entry, state)
        return manifest, state

    def test_boot_admission_uses_durable_identity_not_history(self):
        manifest, state = self.install()
        state['history'] = []
        state['last_attempt'] = None
        self.host.save_state(self.entry, state)
        fresh = Host(Paths(self.root))
        compatibility.startup_check(fresh, self.entry['name'], 'worker')
        state['queue_compatibility']['admitted'][manifest['version']] = 'f' * 64
        self.host.save_state(self.entry, state)
        with self.assertRaises(DeployError) as error:
            compatibility.startup_check(fresh, self.entry['name'], 'worker')
        self.assertEqual('queue_release_incompatible', error.exception.code)

    def test_persisted_layout_floor_cannot_be_cleared(self):
        _, state = self.install()
        for change in ('remove', 'lower'):
            candidate = copy.deepcopy(state)
            if change == 'remove':
                candidate.pop('queue_compatibility')
            else:
                candidate['queue_compatibility']['floor'] = None
            with self.assertRaises(DeployError):
                self.host.save_state(self.entry, candidate)

    def test_closed_gate_blocks_every_recovery_start_and_switch(self):
        manifest, state = self.install()
        state['queue_compatibility']['permission'] = 'closed'
        state['last_attempt'].update(role_recovery_pending=True, prior_background={'worker': 'active'})
        self.host.save_state(self.entry, state)
        self.host.commands.clear()
        for action in (
            lambda: release.restart(self.host, self.entry, state),
            lambda: release.resume_background_recovery(self.host, self.entry, state),
            lambda: release.restore_code(self.host, self.entry, state, manifest, None, 'healthy'),
            lambda: release.switch_to(self.host, self.entry, manifest['version']),
            lambda: compatibility.startup_check(self.host, self.entry['name'], 'http'),
        ):
            with self.assertRaises(DeployError) as error:
                action()
            self.assertEqual('queue_start_fenced', error.exception.code)
        self.assertFalse(any(args[:2] in (['systemctl', 'start'], ['systemctl', 'restart'])
                             for args, _ in self.host.commands))

    def test_rollback_rejects_unadmitted_code_before_side_effects(self):
        manifest, state = self.install()
        archive, other = self.bundle(2)
        staging = self.opt / 'releases' / '.stage-test'
        release.unpack_archive(archive, staging, self.entry, release.digest(archive))
        release.publish_release(self.host, self.entry, staging, other)
        self.host.commands.clear()
        with self.assertRaises(DeployError) as error:
            release.rollback(self.host, self.project, self.profile, other['version'],
                             manifest['version'], str(uuid.uuid4()))
        self.assertEqual('queue_release_incompatible', error.exception.code)
        self.assertEqual([], self.host.commands)
        self.assertEqual(manifest['version'], release.current_version(self.host, self.entry))

    def test_missing_or_malformed_state_never_admits_boot(self):
        self.install()
        path = self.host.paths.instance_state(self.entry['name']) / 'state.json'
        path.unlink()
        with self.assertRaises(DeployError):
            compatibility.startup_check(self.host, self.entry['name'], 'http')
        for value in ({}, {'schema': 1, 'floor': True, 'permission': 'admitted', 'admitted': {}},
                      {'schema': 1, 'floor': 3, 'permission': 'allow_everything', 'admitted': {}}):
            with self.assertRaises(DeployError):
                compatibility.validate(value)

    def test_containment_preserves_floor_and_operation_without_running_artisan(self):
        _, state = self.install()
        operation = str(uuid.uuid4())
        state['cutover'] = progress(operation)
        self.host.save_state(self.entry, state)
        self.host.commands.clear()
        with patch.object(compatibility, 'prove_stopped', return_value={}):
            result = release.containment_stop(self.host, self.entry, state)
        recorded = self.host.load_state(self.entry)
        self.assertTrue(result['contained'])
        self.assertEqual(3, recorded['queue_compatibility']['floor'])
        self.assertEqual('closed', recorded['queue_compatibility']['permission'])
        self.assertEqual(operation, recorded['cutover']['operation_id'])
        self.assertTrue(recorded['cutover']['hold'])
        self.assertTrue(all(args[0] == 'systemctl' and args[1] in ('stop', 'disable') for args, _ in self.host.commands))


class QuiescenceTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.host = Host(Paths(Path(temporary.name)))
        _, _, self.entry = fixture()
        self.group = '/system.slice/test.service'
        self.cgroup = self.host.paths.p('/sys/fs/cgroup' + self.group)
        self.cgroup.mkdir(parents=True)
        (self.cgroup / 'cgroup.procs').write_text('')
        self.values = dict(LoadState='loaded', ActiveState='inactive', SubState='dead', MainPID='0',
                           ControlPID='0', ControlGroup=self.group, Job='0', Result='success',
                           ExecMainCode='1', ExecMainStatus='0')
        self.run = patch.object(self.host, 'run', side_effect=self.observe).start()
        self.addCleanup(patch.stopall)

    def observe(self, args, **kwargs):
        if '--property=MainPID,ControlGroup' in args:
            return subprocess.CompletedProcess(args, 0, f'MainPID={os.getpid()}\nControlGroup=/system.slice/{args[2]}\n', '')
        return subprocess.CompletedProcess(args, 0, ''.join(f'{key}={value}\n' for key, value in self.values.items()), '')

    def test_main_pid_zero_is_not_descendant_exit_proof(self):
        compatibility.prove_stopped(self.host, self.entry)
        child = self.cgroup / 'child'
        child.mkdir()
        (child / 'cgroup.procs').write_text('123\n')
        with self.assertRaises(DeployError) as error:
            compatibility.prove_stopped(self.host, self.entry)
        self.assertEqual('cutover_stop_unproven', error.exception.code)

    def test_default_cgroup_matches_systemd_instance_slices(self):
        self.assertEqual(('system.slice', '/system.slice/app.service'), compatibility.default_cgroup('app.service'))
        self.assertEqual(('system-foundry\\x2dhttp.slice',
                          '/system.slice/system-foundry\\x2dhttp.slice/foundry-http@semaklah-production.service'),
                         compatibility.default_cgroup('foundry-http@semaklah-production.service'))

    def test_stopped_instance_units_prove_exit_in_their_template_slice(self):
        # Real systemd clears ControlGroup on stopped units and places instances in
        # system-<escaped template>.slice, not system.slice.
        units = instance_units(self.entry)
        self.assertTrue(all('@' in unit for unit in units.values()))
        self.values.update(ControlGroup='')
        observed = {}
        def observe(args, **kwargs):
            unit = args[2]
            observed[unit] = compatibility.default_cgroup(unit)
            values = {**self.values, 'Slice': self.slice or observed[unit][0]}
            return subprocess.CompletedProcess(args, 0, ''.join(f'{k}={v}\n' for k, v in values.items()), '')
        self.run.side_effect = observe
        self.slice = None
        compatibility.prove_stopped(self.host, self.entry)
        busy = self.host.paths.p('/sys/fs/cgroup' + observed[units['worker']][1])
        busy.mkdir(parents=True)
        (busy / 'cgroup.procs').write_text('123\n')
        with self.assertRaises(DeployError) as error:
            compatibility.prove_stopped(self.host, self.entry, roles=['worker'])
        self.assertEqual('cutover_stop_unproven', error.exception.code)
        (busy / 'cgroup.procs').write_text('')
        self.slice = 'system.slice'
        with self.assertRaises(DeployError) as error:
            compatibility.prove_stopped(self.host, self.entry, roles=['worker'])
        self.assertEqual('cutover_stop_unproven', error.exception.code)

    def test_tool_upgrade_waits_for_a_running_cutover_executor(self):
        state = self.host.load_state(self.entry)
        state['cutover'] = progress(str(uuid.uuid4()))
        for outcome, executor, allowed in [('needs_reconciliation', 'exited', True),
                                           ('needs_reconciliation', 'running', False),
                                           ('running', 'exited', False)]:
            with self.subTest(outcome=outcome, executor=executor):
                state['cutover']['outcome'] = outcome
                with patch.object(release, 'cutover_executor', return_value=executor):
                    if allowed:
                        compatibility.require_upgrade_idle(self.host, self.entry, state)
                    else:
                        with self.assertRaises(DeployError) as error:
                            compatibility.require_upgrade_idle(self.host, self.entry, state)
                        self.assertEqual('cutover_pending', error.exception.code)
        with self.assertRaises(DeployError):
            compatibility.require_mutation_idle(state)

    def test_pending_job_and_forced_termination_fail_closed(self):
        for key, value, code in [('Job', '99', 'cutover_stop_unproven'),
                                 ('ControlPID', '99', 'cutover_stop_unproven'),
                                 ('Result', 'timeout', 'cutover_drain_review')]:
            with self.subTest(key=key):
                prior = self.values[key]
                self.values[key] = value
                with self.assertRaises(DeployError) as error:
                    compatibility.prove_stopped(self.host, self.entry)
                self.assertEqual(code, error.exception.code)
                self.values[key] = prior

    def test_quiescence_requires_matching_closed_operation(self):
        state = self.host.load_state(self.entry)
        operation = str(uuid.uuid4())
        executor = self.host.paths.p('/sys/fs/cgroup/system.slice/' + release.cutover_unit(self.entry, operation))
        executor.mkdir(parents=True)
        (executor / 'cgroup.procs').write_text(str(os.getpid()) + '\n')
        state['cutover'] = progress(operation)
        boot = self.host.paths.p('/proc/sys/kernel/random/boot_id')
        boot.parent.mkdir(parents=True)
        boot.write_text(str(uuid.uuid4()))
        state['cutover']['drain'] = {'boot_id': boot.read_text(), 'roles': {
            role: {'invocation_id': '', 'complete': True} for role in instance_units(self.entry)}}
        state['queue_compatibility'] = {'schema': 1, 'floor': None, 'permission': 'closed', 'admitted': {}}
        self.host.save_state(self.entry, state)
        with patch.object(compatibility, 'require_no_clients'):
            release.require_cutover_quiescence(self.host, self.entry, operation)
            with self.assertRaises(DeployError):
                release.require_cutover_quiescence(self.host, self.entry, str(uuid.uuid4()))
            state['cutover']['drain_failed'] = True
            self.host.save_state(self.entry, state)
            with self.assertRaises(DeployError) as error:
                release.require_cutover_quiescence(self.host, self.entry, operation)
            self.assertEqual('cutover_drain_review', error.exception.code)
