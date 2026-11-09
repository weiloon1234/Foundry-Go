"""M4 orchestration with M1/M3 contract fakes; no servers or installed tools."""
import copy
import json
import os
from pathlib import Path
import subprocess
import sys
import unittest
from unittest.mock import patch
import uuid

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'lib'))
import compatibility
import cutover_contracts as contract
from contracts import DeployError, digest, now
import release
import test_release
from topology import instance_units

executor_alone = release._require_executor_alone
outstanding_executor = release.cutover_executor
real_prove_stopped = compatibility.prove_stopped


def inspection(entry, layout='2'):
    prefix = f"foundry:jobs:v1:{entry['allocations']['namespace']['application']}:{entry['environment']}:"
    return {'schema': 1, 'supported_layout': 3, 'coverage': 'declared', 'blockers': [], 'connections': [{
        'connection': 'delivery', 'redis_connection': 'primary',
        'storage_id': contract.sha(['localhost', 56379, 0, entry['allocations']['redis_user'], True]),
        'namespace': copy.deepcopy(entry['allocations']['namespace']), 'prefix': prefix, 'policy_id': 'b' * 64,
        'queues': [{'queue': 'default', 'base': prefix + 'queue:' + 'a' * 64,
                    'keys': [prefix + 'queue:' + 'a' * 64 + ':metadata'], 'layout': layout, 'eligible': True}]}]}


def evidence(entry, request):
    return {'schema': 1, 'status': 'complete', 'snapshot_id': request['snapshot_id'],
            'operation_id': request['operation_id'], 'host_id': entry['host_id'], 'project_id': entry['project_id'],
            'instance_id': entry['profile']['instance_id'], 'source_release': request['source_release'],
            'configuration_revision': request['configuration_revision'], 'inventory_sha256': request['inventory_sha256'],
            'scope_sha256': contract.sha(request['scopes']), 'captured_at': now(), 'completed_at': now(),
            'key_count': 1, 'encrypted_bytes': 123, 'sha256': 'c' * 64,
            'object_key': f"redis/{entry['profile']['instance_id']}/{request['snapshot_id']}.json.age"}


class CutoverTests(unittest.TestCase):
    bundle = test_release.ReleaseTests.bundle
    deploy = test_release.ReleaseTests.deploy
    recovery_point = test_release.ReleaseTests.recovery_point
    reconcile_grants = test_release.ReleaseTests.reconcile_grants

    def setUp(self):
        test_release.ReleaseTests.setUp(self)
        archive, self.source = self.bundle()
        self.deploy(archive)
        self.archive, self.target = self.bundle(2)
        self.inventory = inspection(self.entry)
        self.boot_path = self.host.paths.p('/proc/sys/kernel/random/boot_id')
        self.boot_path.parent.mkdir(parents=True)
        self.boot_path.write_text(str(uuid.uuid4()))
        prefix = self.inventory['connections'][0]['prefix']
        registry = self.host.read_registry()
        registry['tools']['generation'] = 5
        registry['instances'][self.profile['instance_id']]['redis_grants'] = {'keys': [prefix], 'channels': []}
        self.host.write_registry(registry)
        self.entry = registry['instances'][self.profile['instance_id']]
        self.events = []
        self.invocations = {unit: uuid.uuid4().hex for unit in instance_units(self.entry).values()}
        self.executor_invocation = uuid.uuid4().hex
        self.executor_running = False
        self.executor_observation = {}
        self.fail_at = None
        self.unknown_at = None
        self.interrupt_at = None
        self.snapshot_calls = []
        self.run = self.host.run
        self.host.run = self.command
        patch.object(release, 'services', side_effect=lambda host, entry: {r: {'ActiveState': host.role_states.get(u, 'inactive'), 'executable_matches': True} for r, u in instance_units(entry).items()}).start()
        patch.object(compatibility, 'require_guards').start()
        patch.object(compatibility, 'prove_stopped', side_effect=self.stopped).start()
        patch.object(compatibility, 'require_no_clients').start()
        patch.object(release, 'cutover_executor', side_effect=lambda *args: 'running' if self.executor_running else 'absent').start()
        execute = release.execute_cutover
        def supervised(*args):
            self.executor_running = True
            self.executor_invocation = uuid.uuid4().hex
            try:
                return execute(*args)
            finally:
                self.executor_running = False
        patch.object(release, 'execute_cutover', side_effect=supervised).start()
        patch.object(release, '_require_executor_alone').start()
        patch('upgrades._installed', return_value=self.root / 'tools').start()
        patch.object(release.backup, '_settings', return_value={}).start()
        patch.object(release.backup, '_repository_settings', return_value={}).start()
        patch.object(release, 'inspect_job_queues', side_effect=lambda *args, **kwargs: copy.deepcopy(self.inventory)).start()
        patch.object(release, 'release_redis_inventory', return_value={'patterns': [prefix + '*'], 'channels': []}).start()
        patch.object(contract.redis_acl, 'inspect', return_value={'owner': 'foundry', 'state': 'ready'}).start()
        patch.object(contract.redis_acl, '_redis', side_effect=self.scan).start()
        self.snapshot = type('Snapshot', (), {})()
        self.snapshot.capture = self.capture
        patch.object(release, '_snapshot_module', return_value=self.snapshot).start()
        self.addCleanup(patch.stopall)
        self.host.commands.clear()

    def scan(self, host, *args):
        return ['0', [key for c in self.inventory['connections'] for q in c['queues'] for key in q['keys']]]

    def stopped(self, host, entry, **kwargs):
        if any(host.role_states.get(unit) == 'active' for unit in instance_units(entry).values()):
            raise DeployError('cutover_stop_unproven', 'Still active')
        return {}

    def capture(self, host, registry, entry, request):
        release.require_cutover_quiescence(host, entry, request['operation_id'])
        self.events.append('snapshot')
        self.snapshot_calls.append(copy.deepcopy(request))
        if self.fail_at == 'snapshot':
            raise DeployError('redis_snapshot_upload', 'Injected upload failure')
        return evidence(entry, request)

    def command(self, args, **kwargs):
        args = list(map(str, args))
        if '--property=InvocationID' in args:
            value = (self.executor_invocation if args[2].startswith('foundry-cutover-') else
                     self.invocations[args[2]] if self.host.role_states.get(args[2]) == 'active' else '')
            return subprocess.CompletedProcess(args, 0, value + '\n', '')
        if args[:2] == ['systemctl', 'show'] and args[2].startswith('foundry-cutover-'):
            operation = args[2].removeprefix('foundry-cutover-' + self.profile['instance_id'] + '-').removesuffix('.service')
            value = {'LoadState': 'loaded', 'ActiveState': 'active' if self.executor_running else 'inactive',
                     'SubState': 'running' if self.executor_running else 'dead',
                     'MainPID': str(os.getpid()) if self.executor_running else '0', 'ControlPID': '0', 'Job': '0',
                     'ControlGroup': '/system.slice/' + args[2], 'InvocationID': self.executor_invocation,
                     'Transient': 'yes', 'Description': f"Foundry cutover {self.profile['instance_id']} {operation}"}
            value.update(self.executor_observation)
            return subprocess.CompletedProcess(args, 0, ''.join(f'{key}={item}\n' for key, item in value.items() if item is not None), '')
        event = next((item for item in ('config:check', 'deploy:storage-probe', 'migrate:status', 'migrate-layout', 'migrate', 'db:seed') if item in args), None)
        if args[:2] in (['systemctl', 'stop'], ['systemctl', 'restart'], ['systemctl', 'start']):
            event = args[1]
        if event:
            self.events.append(event)
        if self.fail_at == event and event is not None:
            raise DeployError('injected_failure', 'Injected command failure')
        if self.interrupt_at == event and event is not None:
            raise KeyboardInterrupt()
        if 'migrate-layout' in args:
            queue = args[args.index('--queue') + 1]
            for connection in self.inventory['connections']:
                for target in connection['queues']:
                    if target['queue'] == queue:
                        target['layout'] = '3'
            if self.unknown_at == event:
                raise DeployError('command_unavailable', 'Response lost after mutation')
            return subprocess.CompletedProcess(args, 0, json.dumps({'queue': queue, 'migrated': False}), '')
        if event == 'migrate':
            self.host.pending = False
        result = self.run(args, **kwargs)
        if self.unknown_at == event and event is not None:
            raise DeployError('command_unavailable', 'Response lost after mutation')
        return result

    def plan(self):
        return release.plan_cutover(self.host, self.project, self.profile, self.archive, digest(self.archive))

    def apply(self):
        self.plan_result = self.plan()
        self.operation = str(uuid.uuid4())
        return release.apply_cutover(self.host, self.project, self.profile, self.plan_result['plan_token'], self.operation)

    def execute(self):
        return release.execute_cutover(self.host, self.profile['instance_id'], self.operation)

    def state(self):
        return self.host.load_state(self.entry)

    def test_exact_order_and_snapshot_before_migration_even_when_migrated_false(self):
        self.host.pending = True
        self.apply()
        self.assertNotIn('stop', self.events)
        self.assertIn('config:check', self.events)
        self.assertEqual('closed', self.state()['queue_compatibility']['permission'])
        result = self.execute()
        self.assertTrue(result['ok'])
        for a, b in [('stop', 'snapshot'), ('snapshot', 'migrate-layout'), ('migrate-layout', 'migrate'), ('migrate', 'db:seed'), ('db:seed', 'restart')]:
            self.assertLess(self.events.index(a), self.events.index(b), (a, b, self.events))
        self.assertEqual(3, self.state()['queue_compatibility']['floor'])
        self.assertTrue(self.state()['cutover']['rollback_prohibited'])
        self.assertEqual(self.target['version'], release.current_version(self.host, self.entry))
        count = len(self.events)
        replay = release.apply_cutover(self.host, self.project, self.profile, self.plan_result['plan_token'], self.operation)
        self.assertEqual(result, replay)
        self.assertEqual(count, len(self.events))

    def test_preboundary_snapshot_failure_restores_only_original_enabled_roles(self):
        self.host.enabled_roles.discard(instance_units(self.entry)['scheduler'])
        self.apply()
        self.fail_at = 'snapshot'
        with self.assertRaises(DeployError):
            self.execute()
        self.assertNotIn('migrate-layout', self.events)
        self.assertEqual('failed', self.state()['cutover']['outcome'])
        self.assertEqual(self.source['version'], release.current_version(self.host, self.entry))
        self.assertNotIn(instance_units(self.entry)['scheduler'], self.host.enabled_roles)
        self.assertFalse(self.state()['cutover']['rollback_prohibited'])

    def test_lost_migration_response_resumes_same_target_without_recapture(self):
        self.apply()
        self.unknown_at = 'migrate-layout'
        with self.assertRaises(DeployError):
            self.execute()
        self.assertEqual('closed', self.state()['queue_compatibility']['permission'])
        self.assertTrue(self.state()['cutover']['rollback_prohibited'])
        self.assertNotIn('restart', self.events)
        self.unknown_at = None
        release.apply_cutover(self.host, self.project, self.profile, self.plan_result['plan_token'], self.operation, resume=True)
        self.assertTrue(self.execute()['ok'])
        self.assertEqual(1, len(self.snapshot_calls))
        self.assertEqual(2, self.events.count('migrate-layout'))

    def test_partial_queue_progress_does_not_repeat_confirmed_work(self):
        second = copy.deepcopy(self.inventory['connections'][0]['queues'][0])
        second.update(queue='second', base=second['base'][:-64] + 'b' * 64)
        second['keys'] = [second['base'] + ':metadata']
        self.inventory['connections'][0]['queues'].append(second)
        self.apply()
        original = self.host.run
        calls = 0
        def fail_second(args, **kwargs):
            nonlocal calls
            if 'migrate-layout' in args:
                calls += 1
                if calls == 2:
                    raise DeployError('command_unavailable', 'Unknown second outcome')
            return original(args, **kwargs)
        self.host.run = fail_second
        with self.assertRaises(DeployError):
            self.execute()
        self.assertEqual(['confirmed', 'dispatched_or_unknown'], [q['status'] for q in self.state()['cutover']['queues']])
        self.host.run = original
        release.apply_cutover(self.host, self.project, self.profile, self.plan_result['plan_token'], self.operation, resume=True)
        self.execute()
        self.assertEqual(2, self.events.count('migrate-layout'))

    def test_seeder_uncertainty_never_reruns_or_restores_old_code(self):
        self.apply()
        self.unknown_at = 'db:seed'
        with self.assertRaises(DeployError):
            self.execute()
        self.unknown_at = None
        release.apply_cutover(self.host, self.project, self.profile, self.plan_result['plan_token'], self.operation, resume=True)
        with self.assertRaises(DeployError) as error:
            self.execute()
        self.assertEqual('seed_reconciliation', error.exception.code)
        self.assertEqual(1, self.events.count('db:seed'))
        self.assertNotIn('restart', self.events)

    def test_database_uncertainty_is_reconciled_from_status(self):
        self.host.pending = True
        self.apply()
        self.unknown_at = 'migrate'
        with self.assertRaises(DeployError):
            self.execute()
        self.unknown_at = None
        release.apply_cutover(self.host, self.project, self.profile, self.plan_result['plan_token'], self.operation, resume=True)
        self.execute()
        self.assertEqual(1, self.events.count('migrate'))

    def test_health_failure_keeps_candidate_selected_and_fenced(self):
        self.apply()
        self.health.return_value = False
        with self.assertRaises(DeployError):
            self.execute()
        self.assertEqual(self.target['version'], release.current_version(self.host, self.entry))
        self.assertEqual('closed', self.state()['queue_compatibility']['permission'])
        self.assertTrue(all(self.host.role_states.get(u) == 'inactive' for u in instance_units(self.entry).values()))

    def test_forced_drain_requires_review_and_never_migrates(self):
        self.apply()
        with patch.object(compatibility, 'prove_stopped', side_effect=DeployError('cutover_drain_review', 'Killed worker')):
            with self.assertRaises(DeployError):
                self.execute()
        self.assertTrue(self.state()['cutover']['drain_failed'])
        self.assertNotIn('snapshot', self.events)
        self.assertNotIn('restart', self.events)
        with self.assertRaises(DeployError):
            release.apply_cutover(self.host, self.project, self.profile, self.plan_result['plan_token'], self.operation, resume=True)

    def test_config_drift_rejects_before_stop_and_uuid_intent_cannot_change(self):
        plan = self.plan()
        self.env_file.write_text(self.env_file.read_text() + '\nPUBLIC_CHANGE=yes\n')
        with self.assertRaises(DeployError) as error:
            release.apply_cutover(self.host, self.project, self.profile, plan['plan_token'], str(uuid.uuid4()))
        self.assertEqual('cutover_plan_changed', error.exception.code)
        self.assertNotIn('stop', self.events)

    def test_interruption_before_dispatch_keeps_persistent_fence(self):
        self.apply()
        self.interrupt_at = 'migrate-layout'
        with self.assertRaises(KeyboardInterrupt):
            self.execute()
        self.assertEqual('dispatched_or_unknown', self.state()['cutover']['queues'][0]['status'])
        self.assertEqual(3, self.state()['queue_compatibility']['floor'])
        with self.assertRaises(DeployError):
            compatibility.require_current(self.host, self.entry)

    def test_ordinary_activation_requires_explicit_cutover(self):
        with self.assertRaises(DeployError) as error:
            self.deploy(self.archive)
        self.assertEqual('queue_cutover_required', error.exception.code)
        self.assertNotIn('stop', self.events)

    def test_all_layout_three_still_blocks_old_fallback(self):
        self.inventory['connections'][0]['queues'][0]['layout'] = '3'
        self.apply()
        self.fail_at = 'snapshot'
        with self.assertRaises(DeployError):
            self.execute()
        self.assertTrue(self.state()['cutover']['rollback_prohibited'])
        self.assertNotIn('restart', self.events)

    def test_no_redis_skips_snapshot_and_migration(self):
        self.inventory['connections'] = []
        self.entry['redis_grants']['keys'] = []
        registry = self.host.read_registry()
        registry['instances'][self.profile['instance_id']] = self.entry
        self.host.write_registry(registry)
        with patch.object(release, 'release_redis_inventory', return_value={'patterns': [], 'channels': []}):
            self.apply()
            self.execute()
        self.assertEqual([], self.snapshot_calls)
        self.assertNotIn('migrate-layout', self.events)
        self.assertIsNone(self.state()['queue_compatibility']['floor'])

    def test_acceptance_recovers_on_either_side_of_atomic_state_write(self):
        self.plan_result = self.plan()
        self.operation = str(uuid.uuid4())
        save = self.host.save_state
        before = self.state()
        for written in (False, True):
            with self.subTest(written=written):
                def interrupted(entry, state):
                    if 'cutover' in state:
                        if written:
                            save(entry, state)
                        raise KeyboardInterrupt()
                    save(entry, state)
                with patch.object(self.host, 'save_state', side_effect=interrupted):
                    with self.assertRaises(KeyboardInterrupt):
                        release.apply_cutover(self.host, self.project, self.profile, self.plan_result['plan_token'], self.operation)
                record_path = contract.private_path(self.host, self.entry, 'operation', self.operation)
                record = json.loads(record_path.read_text())
                self.assertFalse(record['accepted'])
                self.assertNotIn('stop', self.events)
                release.apply_cutover(self.host, self.project, self.profile, self.plan_result['plan_token'], self.operation, resume=True)
                self.assertEqual('closed', self.state()['queue_compatibility']['permission'])
                self.assertTrue(json.loads(record_path.read_text())['accepted'])
                # Isolate the second simulated crash without exercising state deletion through runtime APIs.
                state_path = self.host.paths.instance_state(self.entry['name']) / 'state.json'
                state_path.write_text(json.dumps(before))
                record_path.unlink()

    def test_lost_terminal_receipt_never_reexecutes_or_duplicates_history(self):
        self.apply()
        self.execute()
        path = contract.private_path(self.host, self.entry, 'operation', self.operation)
        record = json.loads(path.read_text())
        expected = record.pop('result')
        contract.save_private(self.host, path, record)
        before_events, before_history = list(self.events), self.state()['history']
        result = release.apply_cutover(self.host, self.project, self.profile, self.plan_result['plan_token'], self.operation, resume=True)
        self.assertEqual(expected, result)
        self.assertEqual(before_events, self.events)
        self.assertEqual(before_history, self.state()['history'])

    def test_receipt_publication_failure_preserves_healthy_durable_completion(self):
        self.apply()
        save = contract.save_private
        def fail_receipt(host, path, value):
            if value.get('result'):
                raise OSError('Injected receipt publication failure')
            save(host, path, value)
        with patch.object(contract, 'save_private', side_effect=fail_receipt):
            with self.assertRaises(DeployError):
                self.execute()
        self.assertEqual('succeeded', self.state()['cutover']['outcome'])
        self.assertEqual('admitted', self.state()['queue_compatibility']['permission'])
        before = list(self.events)
        result = release.apply_cutover(self.host, self.project, self.profile, self.plan_result['plan_token'], self.operation, resume=True)
        self.assertTrue(result['ok'])
        self.assertEqual(before, self.events)

    def test_wrong_snapshot_evidence_never_dispatches_queue_mutation(self):
        self.apply()
        original = self.snapshot.capture
        def wrong(*args):
            result = original(*args)
            result['snapshot_id'] = str(uuid.uuid4())
            return result
        self.snapshot.capture = wrong
        with self.assertRaises(DeployError):
            self.execute()
        self.assertNotIn('migrate-layout', self.events)
        self.assertEqual('failed', self.state()['cutover']['outcome'])

    def restore_plan(self):
        self.restore_operation, snapshot_id = str(uuid.uuid4()), str(uuid.uuid4())
        snapshot_request = {'operation_id': str(uuid.uuid4()), 'snapshot_id': snapshot_id,
                            'source_release': self.source['version'], 'configuration_revision': self.entry['configuration_revision'],
                            'inventory_sha256': contract.inventory_identity(self.inventory), 'scopes': contract.scopes(self.inventory)}
        bound_evidence = evidence(self.entry, snapshot_request)
        contract.save_private(self.host, contract.private_path(self.host, self.entry, 'snapshot', snapshot_id),
                              {'inventory': copy.deepcopy(self.inventory), 'evidence': bound_evidence})
        self.restore_calls = []
        def plan(host, registry, entry, request):
            self.assertEqual({'schema', 'operation_id', 'snapshot_id', 'inventory_sha256', 'scopes', 'identity_file'}, set(request))
            identity = Path(request['identity_file'])
            self.assertEqual(0o600, identity.stat().st_mode & 0o777)
            self.assertEqual(self.restore_operation, identity.parent.name)
            self.assertEqual('restore.identity', identity.name)
            self.assertEqual(0o700, identity.parent.stat().st_mode & 0o777)
            self.assertEqual('PRIVATE IDENTITY', identity.read_text())
            self.identity_file = identity
            return {'schema': 1, 'operation_id': self.restore_operation, 'snapshot': bound_evidence,
                    'scope_sha256': contract.sha(request['scopes']), 'restore_keys': 1, 'remove_keys': 1,
                    'plan_token': 'f' * 64, 'warning': 'restore_may_replay_external_side_effects'}
        def restore(kind, host, registry, entry, request):
            self.assertEqual({'schema', 'operation_id', 'plan_token', 'confirm', 'safety_snapshot'}, set(request))
            release.require_cutover_quiescence(host, entry, request['operation_id'])
            self.restore_calls.append(kind)
            self.events.append('redis-restore')
            if self.unknown_at == 'redis-restore':
                raise DeployError('redis_restore_reconciliation', 'Lost restore result')
            return {'schema': 1, 'status': 'complete', 'operation_id': self.restore_operation,
                    'snapshot_id': snapshot_id, 'safety_snapshot_id': request['safety_snapshot']['snapshot_id'],
                    'scope_sha256': request['safety_snapshot']['scope_sha256'], 'completed_at': now(),
                    'restored_keys': 1, 'removed_keys': 1}
        self.snapshot.plan_restore = plan
        self.snapshot.apply_restore = lambda *args: restore('apply', *args)
        self.snapshot.resume_restore = lambda *args: restore('resume', *args)
        result = release.plan_redis_restore(self.host, self.project, self.profile, self.restore_operation, snapshot_id, 'PRIVATE IDENTITY')
        self.assertFalse(self.identity_file.exists())
        return result

    def test_restore_delegates_with_safety_snapshot_and_keeps_startup_closed(self):
        plan = self.restore_plan()
        self.assertNotIn('stop', self.events)
        release.apply_redis_restore(self.host, self.project, self.profile, plan['plan_token'], self.restore_operation)
        result = release.execute_cutover(self.host, self.profile['instance_id'], self.restore_operation)
        self.assertTrue(result['ok'])
        self.assertLess(self.events.index('stop'), self.events.index('snapshot'))
        self.assertLess(self.events.index('snapshot'), self.events.index('redis-restore'))
        self.assertNotIn('restart', self.events)
        self.assertEqual(['apply'], self.restore_calls)
        self.assertEqual('closed', self.state()['queue_compatibility']['permission'])
        self.assertEqual(3, self.state()['queue_compatibility']['floor'])
        replay = release.apply_redis_restore(self.host, self.project, self.profile, plan['plan_token'], self.restore_operation)
        self.assertEqual(result, replay)
        self.assertEqual(['apply'], self.restore_calls)

    def test_restore_unknown_result_uses_resume_and_original_safety_snapshot(self):
        self.apply()
        self.unknown_at = 'migrate-layout'
        with self.assertRaises(DeployError):
            self.execute()
        plan = self.restore_plan()
        release.apply_redis_restore(self.host, self.project, self.profile, plan['plan_token'], self.restore_operation)
        self.unknown_at = 'redis-restore'
        with self.assertRaises(DeployError):
            release.execute_cutover(self.host, self.profile['instance_id'], self.restore_operation)
        count = len(self.snapshot_calls)
        self.unknown_at = None
        release.apply_redis_restore(self.host, self.project, self.profile, plan['plan_token'], self.restore_operation, resume=True)
        release.execute_cutover(self.host, self.profile['instance_id'], self.restore_operation)
        self.assertEqual(count, len(self.snapshot_calls))
        self.assertEqual(['apply', 'resume'], self.restore_calls)
        original = json.loads(contract.private_path(self.host, self.entry, 'operation', self.operation).read_text())
        self.assertEqual('superseded', original['result']['outcome'])

    def test_executor_cgroup_refuses_a_surviving_root_child(self):
        operation = str(uuid.uuid4())
        group = '/system.slice/' + release.cutover_unit(self.entry, operation)
        root = self.host.paths.p('/sys/fs/cgroup' + group)
        root.mkdir(parents=True)
        (root / 'cgroup.procs').write_text(str(os.getpid()) + '\n')
        result = subprocess.CompletedProcess([], 0, f'MainPID={os.getpid()}\nControlGroup={group}\n', '')
        with patch.object(self.host, 'run', return_value=result):
            executor_alone(self.host, self.entry, operation)
            child = root / 'child'
            child.mkdir()
            (child / 'cgroup.procs').write_text('987654\n')
            with self.assertRaises(DeployError) as error:
                executor_alone(self.host, self.entry, operation)
            self.assertEqual('cutover_executor_unknown', error.exception.code)

    def test_containment_hold_never_recovers_old_roles_on_late_submission(self):
        self.apply()
        state = self.state()
        release.containment_stop(self.host, self.entry, state)
        with self.assertRaises(DeployError):
            self.execute()
        self.assertNotIn('restart', self.events)
        self.assertNotIn('snapshot', self.events)
        self.assertTrue(self.state()['cutover']['hold'])
        self.assertEqual('closed', self.state()['queue_compatibility']['permission'])

    def test_missing_gate_and_inconsistent_dispatch_fail_closed(self):
        self.apply()
        state = self.state()
        state['cutover']['queues'][0]['status'] = 'dispatched_or_unknown'
        with self.assertRaises(DeployError):
            self.host.save_state(self.entry, state)
        state.pop('queue_compatibility')
        path = self.host.paths.instance_state(self.entry['name']) / 'state.json'
        path.write_text(json.dumps(state))
        with self.assertRaises(DeployError) as error:
            compatibility.startup_check(self.host, self.entry['name'], 'worker')
        self.assertEqual('queue_compatibility_missing', error.exception.code)

    def test_worker_selection_validates_candidate_before_downtime(self):
        def remove_selection(manifest):
            config = self.root / 'source-3/config/deploy.toml'
            config.write_text('[project]\n')
            manifest['files']['config/deploy.toml'] = digest(config)
        archive, _ = self.bundle(3, mutate=remove_selection)
        with self.assertRaises(DeployError) as error:
            release.plan_cutover(self.host, self.project, self.profile, archive, digest(archive))
        self.assertEqual('worker_configuration', error.exception.code)
        self.assertNotIn('stop', self.events)
        plan = self.plan()
        with patch.object(release, 'worker_connection', side_effect=DeployError('worker_configuration', 'Changed selection')):
            with self.assertRaises(DeployError):
                release.apply_cutover(self.host, self.project, self.profile, plan['plan_token'], str(uuid.uuid4()))
        self.assertNotIn('cutover', self.state())
        self.assertNotIn('stop', self.events)

    def test_worker_environment_is_prepared_before_start_admission(self):
        self.apply()
        write, permissions = self.host.owned_write, []
        def capture(path, *args, **kwargs):
            if path.name == 'worker.env':
                permissions.append(self.state()['queue_compatibility']['permission'])
            return write(path, *args, **kwargs)
        with patch.object(self.host, 'owned_write', side_effect=capture):
            self.execute()
        self.assertEqual('closed', permissions[0])

    def test_forward_recovery_binds_replacement_and_retains_prior_evidence(self):
        self.apply()
        self.health.return_value = False
        with self.assertRaises(DeployError):
            self.execute()
        self.assertEqual(self.target, self.state()['current'])
        release.resolve_cutover_drain(self.host, self.project, self.profile, self.operation,
                                      'owner@example.test', 'Reviewed readiness failure and interrupted work')
        prior_operation, prior_progress = self.operation, self.state()['cutover']
        archive, replacement = self.bundle(3)
        plan = release.plan_cutover(self.host, self.project, self.profile, archive, digest(archive), forward=True)
        self.assertEqual(prior_operation, plan['forward_recovery']['operation_id'])
        operation = str(uuid.uuid4())
        with self.assertRaises(DeployError) as error:
            release.apply_cutover(self.host, self.project, self.profile, plan['plan_token'], operation)
        self.assertEqual('cutover_forward_confirmation', error.exception.code)
        self.health.return_value = True
        release.apply_cutover(self.host, self.project, self.profile, plan['plan_token'], operation, forward=True)
        self.assertEqual(3, self.state()['queue_compatibility']['floor'])
        result = release.execute_cutover(self.host, self.profile['instance_id'], operation)
        self.assertTrue(result['ok'])
        self.assertEqual(replacement, self.state()['current'])
        prior = json.loads(contract.private_path(self.host, self.entry, 'operation', prior_operation).read_text())
        self.assertEqual(prior_progress, prior['superseded_progress'])
        self.assertEqual('superseded', prior['result']['outcome'])
        binding = release._load_snapshot_binding(self.host, self.entry, prior_progress['snapshot']['snapshot_id'])
        self.assertEqual(prior_progress['snapshot'], binding['evidence'])

    def test_failed_health_restore_then_forward_activation_has_no_state_drift(self):
        self.apply()
        self.health.return_value = False
        with self.assertRaises(DeployError):
            self.execute()
        self.assertEqual(self.target, self.state()['current'])
        release.resolve_cutover_drain(self.host, self.project, self.profile, self.operation,
                                      'owner@example.test', 'Reviewed readiness failure before restore')
        plan = self.restore_plan()
        release.apply_redis_restore(self.host, self.project, self.profile, plan['plan_token'], self.restore_operation)
        release.execute_cutover(self.host, self.profile['instance_id'], self.restore_operation)
        self.inventory['connections'][0]['queues'][0]['layout'] = '2'
        archive, replacement = self.bundle(3)
        forward = release.plan_cutover(self.host, self.project, self.profile, archive, digest(archive))
        self.health.return_value = True
        operation = str(uuid.uuid4())
        release.apply_cutover(self.host, self.project, self.profile, forward['plan_token'], operation)
        release.execute_cutover(self.host, self.profile['instance_id'], operation)
        self.assertEqual(replacement, self.state()['current'])
        self.assertEqual('healthy', self.state()['status'])

    def test_restore_uses_bound_scope_when_live_inspection_is_damaged(self):
        self.apply()
        self.unknown_at = 'migrate-layout'
        with self.assertRaises(DeployError):
            self.execute()
        with patch.object(release, 'inspect_job_queues', side_effect=DeployError('cutover_inventory_blocked', 'Damaged values')):
            self.restore_plan()
        self.assertFalse(self.identity_file.exists())

    def test_recovery_restores_running_set_and_preserves_distinct_enablement(self):
        units = instance_units(self.entry)
        self.host.enabled_roles.discard(units['worker'])  # disabled, but running
        self.host.role_states[units['scheduler']] = 'inactive'  # enabled, but not running
        enabled = set(self.host.enabled_roles)
        self.apply()
        self.fail_at = 'snapshot'
        with self.assertRaises(DeployError):
            self.execute()
        self.assertEqual('active', self.host.role_states[units['worker']])
        self.assertEqual('inactive', self.host.role_states[units['scheduler']])
        self.assertEqual(enabled, self.host.enabled_roles)
        self.assertEqual('degraded', self.state()['status'])

    def test_no_redis_cutover_keeps_ordinary_rollback(self):
        self.test_no_redis_skips_snapshot_and_migration()
        with patch.object(release, 'release_redis_inventory', return_value={'patterns': [], 'channels': []}):
            result = release.rollback(self.host, self.project, self.profile, self.source['version'],
                                      self.target['version'], str(uuid.uuid4()))
        self.assertTrue(result['ok'])
        self.assertEqual(self.source, self.state()['current'])

    def test_blocked_plan_shows_logical_inventory_and_cannot_apply(self):
        self.inventory['blockers'] = [{'code': 'invalid_queue_state', 'connection': 'delivery', 'queue': 'default'}]
        self.inventory['connections'][0]['queues'][0].update(layout='unknown', eligible=False)
        plan = self.plan()
        self.assertFalse(plan['applicable'])
        self.assertIsNone(plan['plan_token'])
        self.assertEqual(self.inventory['blockers'], plan['blockers'])
        self.assertEqual('unknown', plan['queues'][0]['layout'])
        self.assertNotIn(self.inventory['connections'][0]['prefix'], json.dumps(plan))
        with self.assertRaises(DeployError):
            release.apply_cutover(self.host, self.project, self.profile, plan['plan_token'], str(uuid.uuid4()))
        self.assertNotIn('stop', self.events)

    def test_terminal_stop_reports_stopped_and_restart(self):
        self.apply()
        self.execute()
        result = release.containment_stop(self.host, self.entry, self.state())
        self.assertEqual('stopped', self.state()['status'])
        self.assertEqual('restart', result['next_action'])
        self.assertEqual('restart', release.cutover_status(self.host, self.entry)['next_action'])
        self.assertEqual('admitted', self.state()['queue_compatibility']['permission'])

    def test_boot_change_during_unresolved_drain_requires_review(self):
        self.apply()
        self.interrupt_at = 'stop'
        with self.assertRaises(KeyboardInterrupt):
            self.execute()
        self.boot_path.write_text(str(uuid.uuid4()))
        self.interrupt_at = None
        with self.assertRaises(DeployError) as error:
            release.apply_cutover(self.host, self.project, self.profile, self.plan_result['plan_token'], self.operation, resume=True)
        self.assertEqual('cutover_drain_review', error.exception.code)
        self.assertTrue(self.state()['cutover']['drain_failed'])
        self.assertNotIn('snapshot', self.events)

    def test_restore_dispatch_gap_calls_module_resume_without_bypass(self):
        plan = self.restore_plan()
        release.apply_redis_restore(self.host, self.project, self.profile, plan['plan_token'], self.restore_operation)
        with patch.object(self.snapshot, 'apply_restore', side_effect=KeyboardInterrupt()):
            with self.assertRaises(KeyboardInterrupt):
                release.execute_cutover(self.host, self.profile['instance_id'], self.restore_operation)
        release.apply_redis_restore(self.host, self.project, self.profile, plan['plan_token'], self.restore_operation, resume=True)
        release.execute_cutover(self.host, self.profile['instance_id'], self.restore_operation)
        self.assertEqual(['resume'], self.restore_calls)

    def test_real_m3_intake_accepts_m4_operation_directory_and_cleans_identity(self):
        import redis_snapshot
        plan = self.restore_plan()
        operation = str(uuid.uuid4())
        self.snapshot.plan_restore = redis_snapshot.plan_restore
        with patch.object(redis_snapshot, '_ownership'), \
                patch.object(redis_snapshot, '_restore_paths', side_effect=DeployError('redis_restore_boundary_reached', 'After real identity validation')) as boundary:
            with self.assertRaises(DeployError) as error:
                release.plan_redis_restore(self.host, self.project, self.profile, operation,
                                           plan['snapshot']['snapshot_id'], 'PRIVATE IDENTITY')
        self.assertEqual('redis_restore_boundary_reached', error.exception.code)
        boundary.assert_called_once()
        private = contract.private_path(self.host, self.entry, 'restore', operation).parent
        self.assertFalse((private / 'operations' / operation / 'restore.identity').exists())

    def test_invocation_change_before_completed_drain_requires_review(self):
        self.apply()
        self.invocations[instance_units(self.entry)['worker']] = uuid.uuid4().hex
        with self.assertRaises(DeployError) as error:
            self.execute()
        self.assertEqual('cutover_drain_review', error.exception.code)
        self.assertTrue(self.state()['cutover']['drain_failed'])
        self.assertNotIn('snapshot', self.events)

    def test_per_role_drain_completion_survives_interruption(self):
        self.apply()
        stopped = self.stopped
        def interrupt_worker(host, entry, *, roles=None):
            if roles == ['worker']:
                raise KeyboardInterrupt()
            return stopped(host, entry, roles=roles)
        with patch.object(compatibility, 'prove_stopped', side_effect=interrupt_worker):
            with self.assertRaises(KeyboardInterrupt):
                self.execute()
        recorded = self.state()['cutover']['drain']['roles']
        self.assertTrue(recorded['http']['complete'])
        self.assertFalse(recorded['worker']['complete'])
        self.boot_path.write_text(str(uuid.uuid4()))
        with self.assertRaises(DeployError):
            release.apply_cutover(self.host, self.project, self.profile, self.plan_result['plan_token'], self.operation, resume=True)
        self.assertTrue(self.state()['cutover']['drain_failed'])

    def test_blocked_storage_coverage_preserves_safe_plan_without_private_keys(self):
        with patch.object(contract.redis_acl, '_redis', return_value=['0', ['private-orphan-key']]):
            plan = self.plan()
        self.assertFalse(plan['applicable'])
        self.assertEqual('cutover_unknown_queue', plan['blockers'][0]['code'])
        self.assertNotIn('private-orphan-key', json.dumps(plan))

    def test_switch_publication_uncertainty_records_observed_candidate(self):
        self.apply()
        switch = release._switch_pointer
        def uncertain(*args):
            switch(*args)
            raise OSError('Response lost after selection')
        with patch.object(release, '_switch_pointer', side_effect=uncertain):
            with self.assertRaises(DeployError):
                self.execute()
        self.assertEqual(self.target, self.state()['current'])
        self.assertEqual('closed', self.state()['queue_compatibility']['permission'])

    def systemd_exit(self, *, status='203', code='1', pid='123', started='99', after_start=False, result='exit-code'):
        original = self.host.run
        def run(args, **options):
            if any(str(arg).startswith('--property=LoadState,ActiveState,SubState,MainPID,ControlPID,ControlGroup,Slice') for arg in args):
                values = dict(LoadState='loaded', ActiveState='failed', SubState='failed', MainPID='0',
                              ControlPID='0', ControlGroup='', Slice=compatibility.default_cgroup(args[2])[0], Job='0', Result=result,
                              ExecMainCode=code, ExecMainStatus=status, ExecMainPID=pid,
                              ExecMainStartTimestampMonotonic=started, InvocationID=self.invocations[args[2]])
                if after_start and 'restart' not in self.events:
                    values.update(Result='success', ExecMainStatus='0')
                return subprocess.CompletedProcess(args, 0, ''.join(f'{k}={v}\n' for k, v in values.items()), '')
            return original(args, **options)
        return patch.object(self.host, 'run', side_effect=run)

    def test_hard_death_after_switch_restore_then_new_plan(self):
        self.apply()
        switch = release._switch_pointer
        def die(*args):
            switch(*args)
            os._exit(87)  # No exception handler or finally can publish state.
        child = os.fork()
        if child == 0:
            with patch.object(release, '_switch_pointer', side_effect=die):
                try:
                    self.execute()
                finally:
                    os._exit(88)
        _, status = os.waitpid(child, 0)
        self.assertEqual(87, os.waitstatus_to_exitcode(status))
        self.assertEqual(self.target['version'], release.current_version(self.host, self.entry))
        self.assertEqual(self.source, self.state()['current'])
        plan = self.restore_plan()
        self.assertEqual(self.target, self.state()['current'])
        release.apply_redis_restore(self.host, self.project, self.profile, plan['plan_token'], self.restore_operation)
        release.execute_cutover(self.host, self.profile['instance_id'], self.restore_operation)
        self.assertEqual(self.target, self.state()['current'])
        archive, _ = self.bundle(3)
        self.assertTrue(release.plan_cutover(self.host, self.project, self.profile, archive, digest(archive))['applicable'])

    def test_reboot_guard_refusals_after_completed_drain_allow_resume(self):
        self.reboot_guard_refusal('exit-code')

    def test_reboot_start_limit_after_completed_drain_allows_resume(self):
        self.reboot_guard_refusal('start-limit-hit')

    def test_missing_candidate_history_cannot_grant_reboot_exemption(self):
        self.apply()
        self.interrupt_at = 'migrate-layout'
        with self.assertRaises(KeyboardInterrupt):
            self.execute()
        state = self.state()
        state['cutover'].pop('candidate_start')
        self.host.save_state(self.entry, state)
        self.boot_path.write_text(str(uuid.uuid4()))
        with self.systemd_exit(status='0', code='0', pid='0', started='0', result='start-limit-hit'):
            with self.assertRaises(DeployError) as error:
                real_prove_stopped(self.host, self.entry)
        self.assertEqual('cutover_drain_review', error.exception.code)

    def reboot_guard_refusal(self, result):
        self.apply()
        self.interrupt_at = 'migrate-layout'
        with self.assertRaises(KeyboardInterrupt):
            self.execute()
        self.assertTrue(all(v['complete'] for v in self.state()['cutover']['drain']['roles'].values()))
        self.boot_path.write_text(str(uuid.uuid4()))
        self.interrupt_at = None
        with self.systemd_exit(status='0', code='0', pid='0', started='0', result=result), \
                patch.object(compatibility, 'prove_stopped', side_effect=real_prove_stopped):
            release.apply_cutover(self.host, self.project, self.profile, self.plan_result['plan_token'], self.operation, resume=True)
            self.assertTrue(self.execute()['ok'])
        self.assertFalse(self.state()['cutover']['drain_failed'])

    def test_completed_cutover_is_history_after_ordinary_deploy_and_restore(self):
        self.apply()
        self.execute()
        archive, current = self.bundle(3)
        self.deploy(archive)
        self.assertEqual(current, self.state()['current'])
        self.assertEqual(self.target['version'], self.state()['cutover']['target'])
        release.containment_stop(self.host, self.entry, self.state())
        plan = self.restore_plan()
        saved = json.loads(contract.private_path(self.host, self.entry, 'restore', self.restore_operation).read_text())
        self.assertEqual(current, saved['source'])
        self.assertEqual(current, saved['target'])
        release.apply_redis_restore(self.host, self.project, self.profile, plan['plan_token'], self.restore_operation)
        release.execute_cutover(self.host, self.profile['instance_id'], self.restore_operation)
        self.assertEqual(current, self.state()['current'])

    def test_candidate_killed_during_readiness_then_guard_retry_requires_review(self):
        self.apply()
        def killed(*args, **kwargs):
            unit = instance_units(self.entry)['http']
            self.host.role_states[unit] = 'failed'
            self.invocations[unit] = uuid.uuid4().hex  # A later, guard-refused invocation hides the kill.
            return False
        self.health.side_effect = killed
        with self.systemd_exit(status='0', code='0', pid='0', started='0', after_start=True), \
                patch.object(compatibility, 'prove_stopped', side_effect=real_prove_stopped):
            with self.assertRaises(DeployError):
                self.execute()
            failed = self.state()['cutover']['candidate_start']
            self.assertEqual('failed', failed['outcome'])
            self.assertTrue(self.state()['cutover']['drain_failed'])
            self.assertEqual('closed', self.state()['queue_compatibility']['permission'])
            archive, replacement = self.bundle(3)
            with self.assertRaises(DeployError) as error:
                release.plan_cutover(self.host, self.project, self.profile, archive, digest(archive), forward=True)
            self.assertEqual('cutover_drain_review', error.exception.code)
            with self.assertRaises(DeployError) as error:
                release.apply_cutover(self.host, self.project, self.profile, self.plan_result['plan_token'], self.operation, resume=True)
            self.assertEqual('cutover_drain_review', error.exception.code)
            reviewed = release.resolve_cutover_drain(self.host, self.project, self.profile, self.operation,
                                                   'owner@example.test', 'Reviewed killed candidate and external effects')
            self.assertEqual(failed, reviewed['resolution']['candidate_start'])
            plan = release.plan_cutover(self.host, self.project, self.profile, archive, digest(archive), forward=True)
            self.health.side_effect = None
            self.health.return_value = True
            operation = str(uuid.uuid4())
            release.apply_cutover(self.host, self.project, self.profile, plan['plan_token'], operation, forward=True)
            self.assertTrue(release.execute_cutover(self.host, self.profile['instance_id'], operation)['ok'])
            self.assertEqual(replacement, self.state()['current'])

    def test_interrupted_candidate_attempt_requires_review_even_after_clean_stop(self):
        self.apply()
        self.interrupt_at = 'restart'
        with self.assertRaises(KeyboardInterrupt):
            self.execute()
        self.assertEqual('dispatched_or_unknown', self.state()['cutover']['candidate_start']['outcome'])
        self.interrupt_at = None
        with self.assertRaises(DeployError) as error:
            release.apply_cutover(self.host, self.project, self.profile, self.plan_result['plan_token'], self.operation, resume=True)
        self.assertEqual('cutover_drain_review', error.exception.code)
        self.assertTrue(self.state()['cutover']['drain_failed'])
        self.assertEqual('closed', self.state()['queue_compatibility']['permission'])

    def test_missing_start_history_after_executor_death_requires_review_despite_clean_containment(self):
        self.apply()
        def die_during_readiness(*args, **kwargs):
            path = self.host.paths.instance_state(self.entry['name']) / 'state.json'
            state = json.loads(path.read_text())
            state['cutover'].pop('candidate_start')  # Simulate lost startup evidence, bypassing validation.
            path.write_text(json.dumps(state))
            os._exit(87)
        child = os.fork()
        if child == 0:
            self.health.side_effect = die_during_readiness
            try:
                self.execute()
            finally:
                os._exit(88)
        _, status = os.waitpid(child, 0)
        self.assertEqual(87, os.waitstatus_to_exitcode(status))
        for connection in self.inventory['connections']:
            for queue in connection['queues']:
                queue['layout'] = '3'  # The child migrated the fixture's queue before startup.
        with self.assertRaises(DeployError) as error:
            compatibility.startup_check(self.host, self.entry['name'], 'http')
        self.assertEqual('cutover_drain_review', error.exception.code)
        contained = release.containment_stop(self.host, self.entry, self.state())
        self.assertEqual('cutover-drain-resolve', contained['next_action'])
        attempt = self.state()['cutover']['candidate_start']
        self.assertEqual('history_unknown', attempt['outcome'])
        self.assertTrue(self.state()['cutover']['drain_failed'])
        with self.assertRaises(DeployError) as error:
            release.apply_cutover(self.host, self.project, self.profile, self.plan_result['plan_token'], self.operation, resume=True)
        self.assertEqual('cutover_drain_review', error.exception.code)
        archive, _ = self.bundle(3)
        with self.assertRaises(DeployError) as error:
            release.plan_cutover(self.host, self.project, self.profile, archive, digest(archive), forward=True)
        self.assertEqual('cutover_drain_review', error.exception.code)
        reviewed = release.resolve_cutover_drain(self.host, self.project, self.profile, self.operation,
                                               'owner@example.test', 'Reviewed lost startup history and interrupted work')
        self.assertEqual(attempt, reviewed['resolution']['candidate_start'])
        release.apply_cutover(self.host, self.project, self.profile, self.plan_result['plan_token'], self.operation, resume=True)
        self.assertTrue(self.execute()['ok'])

    def test_interrupted_readiness_stop_then_resolution_without_failed_resume(self):
        self.apply()
        self.health.side_effect = KeyboardInterrupt()
        with self.assertRaises(KeyboardInterrupt):
            self.execute()
        self.assertTrue(self.state()['cutover']['drain_failed'])  # The original executor has exited.
        attempt = self.state()['cutover']['candidate_start']
        self.assertEqual('dispatched_or_unknown', attempt['outcome'])
        contained = release.containment_stop(self.host, self.entry, self.state())
        self.assertEqual('cutover-drain-resolve', contained['next_action'])
        reviewed = release.resolve_cutover_drain(self.host, self.project, self.profile, self.operation,
                                               'owner@example.test', 'Reviewed interrupted readiness after containment')
        self.assertEqual(attempt, reviewed['resolution']['candidate_start'])
        self.health.side_effect = None
        release.apply_cutover(self.host, self.project, self.profile, self.plan_result['plan_token'], self.operation, resume=True)
        self.assertTrue(self.execute()['ok'])

    def interrupt_candidate_readiness(self):
        self.apply()
        self.health.side_effect = KeyboardInterrupt()
        with self.assertRaises(KeyboardInterrupt):
            self.execute()
        self.health.side_effect = None

    def test_reboot_during_candidate_readiness_fences_enabled_roles_until_review_and_resume(self):
        self.interrupt_candidate_readiness()
        persisted = json.loads((self.host.paths.instance_state(self.entry['name']) / 'state.json').read_text())
        attempt = persisted['cutover']['candidate_start']
        self.assertEqual('admitted', persisted['queue_compatibility']['permission'])
        self.assertEqual('dispatched_or_unknown', attempt['outcome'])
        self.assertEqual(set(instance_units(self.entry).values()), self.host.enabled_roles)
        self.boot_path.write_text(str(uuid.uuid4()))
        for role in instance_units(self.entry):
            with self.assertRaises(DeployError) as error:
                compatibility.startup_check(self.host, self.entry['name'], role)
            self.assertEqual('cutover_drain_review', error.exception.code)
        self.assertTrue(self.state()['cutover']['drain_failed'])
        self.assertEqual('closed', self.state()['queue_compatibility']['permission'])
        contained = release.containment_stop(self.host, self.entry, self.state())
        self.assertEqual('cutover-drain-resolve', contained['next_action'])
        reviewed = release.resolve_cutover_drain(self.host, self.project, self.profile, self.operation,
                                               'owner@example.test', 'Reviewed candidate work interrupted by reboot')
        self.assertEqual(attempt, reviewed['resolution']['candidate_start'])
        with self.assertRaises(DeployError):
            compatibility.startup_check(self.host, self.entry['name'], 'http')
        release.apply_cutover(self.host, self.project, self.profile, self.plan_result['plan_token'], self.operation, resume=True)
        self.assertTrue(self.execute()['ok'])
        current = self.state()['cutover']['candidate_start']
        self.assertEqual(self.boot_path.read_text(), current['boot_id'])
        self.assertNotEqual(attempt['executor_invocation_id'], current['executor_invocation_id'])
        self.assertEqual('healthy', current['outcome'])
        self.boot_path.write_text(str(uuid.uuid4()))
        compatibility.startup_check(self.host, self.entry['name'], 'http')

    def test_unresolved_admission_requires_same_boot_and_live_executor_invocation(self):
        self.apply()
        def check_admission(*args, **kwargs):
            compatibility.startup_check(self.host, self.entry['name'], 'http')
            invocation = self.executor_invocation
            for replacement in (uuid.uuid4().hex, ''):
                self.executor_invocation = replacement
                with self.assertRaises(DeployError) as error:
                    compatibility.startup_check(self.host, self.entry['name'], 'http')
                self.assertEqual('cutover_drain_review', error.exception.code)
            self.executor_invocation = invocation
            self.executor_running = False
            with self.assertRaises(DeployError) as error:
                compatibility.startup_check(self.host, self.entry['name'], 'http')
            self.assertEqual('cutover_drain_review', error.exception.code)
            self.executor_running = True
            raise KeyboardInterrupt()
        self.health.side_effect = check_admission
        with self.assertRaises(KeyboardInterrupt):
            self.execute()

    def test_unresolved_admission_requires_strict_systemd_executor_liveness(self):
        self.apply()
        def check_admission(*args, **kwargs):
            compatibility.startup_check(self.host, self.entry['name'], 'http')
            self.assertFalse(self.state()['cutover']['drain_failed'])
            observations = [
                {'ActiveState': 'deactivating', 'MainPID': '0'},
                {'ActiveState': 'activating'},
                {'ActiveState': 'reloading'},
                {'ActiveState': None},
                {'SubState': 'dead'},
                {'SubState': None},
                {'MainPID': '0'},
                {'MainPID': None},
                {'MainPID': 'invalid'},
                {'InvocationID': None},
                {'InvocationID': uuid.uuid4().hex},
                {'LoadState': 'not-found'},
            ]
            for observation in observations:
                with self.subTest(observation=observation):
                    self.executor_observation = observation
                    for role in instance_units(self.entry):
                        with self.assertRaises(DeployError) as error:
                            compatibility.startup_check(self.host, self.entry['name'], role)
                        self.assertEqual('cutover_drain_review', error.exception.code)
                    state = self.state()
                    self.assertTrue(state['cutover']['drain_failed'])
                    self.assertEqual('closed', state['queue_compatibility']['permission'])
            self.executor_observation = {}
            compatibility.startup_check(self.host, self.entry['name'], 'http')
            return True
        self.health.side_effect = check_admission
        self.assertTrue(self.execute()['ok'])

    def test_deactivating_executor_fences_admission_until_resolution_and_explicit_resume(self):
        self.apply()
        def executor_dying(*args, **kwargs):
            self.executor_observation = {'ActiveState': 'deactivating', 'MainPID': '0'}
            # Pending children still count as outstanding work, but cannot authorize startup.
            self.assertEqual('running', outstanding_executor(self.host, self.entry, self.operation))
            with self.assertRaises(DeployError) as error:
                compatibility.startup_check(self.host, self.entry['name'], 'http')
            self.assertEqual('cutover_drain_review', error.exception.code)
            self.assertTrue(self.state()['cutover']['drain_failed'])
            raise KeyboardInterrupt()
        self.health.side_effect = executor_dying
        with self.assertRaises(KeyboardInterrupt):
            self.execute()
        self.health.side_effect = None
        self.executor_observation = {}  # Executor and remaining work have now exited.
        contained = release.containment_stop(self.host, self.entry, self.state())
        self.assertEqual('cutover-drain-resolve', contained['next_action'])
        attempt = self.state()['cutover']['candidate_start']
        with self.assertRaises(DeployError) as error:
            release.apply_cutover(self.host, self.project, self.profile, self.plan_result['plan_token'], self.operation, resume=True)
        self.assertEqual('cutover_drain_review', error.exception.code)
        reviewed = release.resolve_cutover_drain(self.host, self.project, self.profile, self.operation,
                                               'owner@example.test', 'Reviewed work after executor death during readiness')
        self.assertEqual(attempt, reviewed['resolution']['candidate_start'])
        self.assertEqual('closed', self.state()['queue_compatibility']['permission'])
        with self.assertRaises(DeployError):
            compatibility.startup_check(self.host, self.entry['name'], 'http')
        release.apply_cutover(self.host, self.project, self.profile, self.plan_result['plan_token'], self.operation, resume=True)
        self.assertTrue(self.execute()['ok'])
        self.assertNotEqual(attempt['executor_invocation_id'], self.state()['cutover']['candidate_start']['executor_invocation_id'])

    def test_missing_or_invalid_attempt_boot_evidence_refuses_guard_and_allows_review(self):
        self.interrupt_candidate_readiness()
        path = self.host.paths.instance_state(self.entry['name']) / 'state.json'
        original = json.loads(path.read_text())
        for boot in (None, 'invalid-boot'):
            with self.subTest(boot=boot):
                state = copy.deepcopy(original)
                if boot is None:
                    state['cutover']['candidate_start'].pop('boot_id')
                else:
                    state['cutover']['candidate_start']['boot_id'] = boot
                path.write_text(json.dumps(state))
                with self.assertRaises(DeployError) as error:
                    compatibility.startup_check(self.host, self.entry['name'], 'http')
                self.assertEqual('cutover_drain_review', error.exception.code)
                self.assertTrue(self.state()['cutover']['drain_failed'])
                release.containment_stop(self.host, self.entry, self.state())
                release.resolve_cutover_drain(self.host, self.project, self.profile, self.operation,
                                              'owner@example.test', 'Reviewed missing or invalid attempt boot evidence')
                self.assertEqual('closed', self.state()['queue_compatibility']['permission'])

    def test_invalid_start_history_can_be_reviewed_and_later_loss_needs_fresh_review(self):
        self.apply()
        self.interrupt_at = 'restart'
        with self.assertRaises(KeyboardInterrupt):
            self.execute()
        path = self.host.paths.instance_state(self.entry['name']) / 'state.json'
        state = json.loads(path.read_text())
        state['cutover']['candidate_start'] = {'outcome': 'healthy', 'id': 'invalid'}
        path.write_text(json.dumps(state))
        release.containment_stop(self.host, self.entry, self.state())
        attempt = self.state()['cutover']['candidate_start']
        self.assertEqual('history_unknown', attempt['outcome'])
        self.assertNotIn('invalid', json.dumps(attempt))
        release.resolve_cutover_drain(self.host, self.project, self.profile, self.operation,
                                      'owner@example.test', 'Reviewed malformed startup history')
        release.apply_cutover(self.host, self.project, self.profile, self.plan_result['plan_token'], self.operation, resume=True)
        with self.assertRaises(KeyboardInterrupt):
            self.execute()
        state = json.loads(path.read_text())
        state['cutover']['candidate_start'] = {'outcome': 'healthy', 'id': 'invalid'}
        path.write_text(json.dumps(state))
        with self.assertRaises(DeployError) as error:
            release.apply_cutover(self.host, self.project, self.profile, self.plan_result['plan_token'], self.operation, resume=True)
        self.assertEqual('cutover_drain_review', error.exception.code)
        self.assertNotEqual(attempt, self.state()['cutover']['candidate_start'])
        self.assertTrue(self.state()['cutover']['drain_failed'])

    def test_missing_history_at_migration_dispatch_is_unknown_before_clean_stop(self):
        self.apply()
        self.interrupt_at = 'migrate-layout'
        with self.assertRaises(KeyboardInterrupt):
            self.execute()
        path = self.host.paths.instance_state(self.entry['name']) / 'state.json'
        state = json.loads(path.read_text())
        state['cutover'].pop('candidate_start')
        path.write_text(json.dumps(state))
        with self.assertRaises(DeployError) as error:
            release.apply_cutover(self.host, self.project, self.profile, self.plan_result['plan_token'], self.operation, resume=True)
        self.assertEqual('cutover_drain_review', error.exception.code)
        persisted = json.loads(path.read_text())
        self.assertTrue(persisted['cutover']['drain_failed'])
        self.assertEqual('history_unknown', persisted['cutover']['candidate_start']['outcome'])
        self.assertEqual('closed', persisted['queue_compatibility']['permission'])

    def test_failed_candidate_pre_start_requires_resolution_before_forward_recovery(self):
        self.apply()
        self.fail_at = 'restart'
        with self.systemd_exit(status='0', code='0', pid='0', started='0', after_start=True), \
                patch.object(compatibility, 'prove_stopped', side_effect=real_prove_stopped):
            with self.assertRaises(DeployError):
                self.execute()
            self.assertTrue(self.state()['cutover']['drain_failed'])
            release.resolve_cutover_drain(self.host, self.project, self.profile, self.operation,
                                          'owner@example.test', 'Reviewed candidate pre-start failure')
            archive, replacement = self.bundle(3)
            plan = release.plan_cutover(self.host, self.project, self.profile, archive, digest(archive), forward=True)
            self.fail_at = None
            operation = str(uuid.uuid4())
            release.apply_cutover(self.host, self.project, self.profile, plan['plan_token'], operation, forward=True)
            self.assertTrue(release.execute_cutover(self.host, self.profile['instance_id'], operation)['ok'])
        self.assertEqual(replacement, self.state()['current'])

    def test_ambiguous_candidate_exit_requires_audited_resolution_then_forward(self):
        self.apply()
        self.fail_at = 'restart'
        with self.systemd_exit(status='1', after_start=True), patch.object(compatibility, 'prove_stopped', side_effect=real_prove_stopped):
            with self.assertRaises(DeployError):
                self.execute()
            self.assertTrue(self.state()['cutover']['drain_failed'])
            result = release.resolve_cutover_drain(self.host, self.project, self.profile, self.operation,
                                                   'owner@example.test', 'Lead checked startup failure before application admission')
            self.assertEqual('owner@example.test', result['resolution']['reviewed_by'])
            self.assertEqual('closed', self.state()['queue_compatibility']['permission'])
            self.assertFalse(self.state()['cutover']['drain_failed'])
            self.assertEqual(result, release.resolve_cutover_drain(self.host, self.project, self.profile, self.operation,
                                                   'owner@example.test', 'Lead checked startup failure before application admission'))
            unit = instance_units(self.entry)['worker']
            invocation = self.invocations[unit]
            self.invocations[unit] = uuid.uuid4().hex
            with self.assertRaises(DeployError) as error:
                real_prove_stopped(self.host, self.entry)
            self.assertEqual('cutover_drain_review', error.exception.code)
            self.invocations[unit] = invocation
            archive, _ = self.bundle(3)
            plan = release.plan_cutover(self.host, self.project, self.profile, archive, digest(archive), forward=True)
            self.fail_at = None
            operation = str(uuid.uuid4())
            release.apply_cutover(self.host, self.project, self.profile, plan['plan_token'], operation, forward=True)
            self.assertTrue(release.execute_cutover(self.host, self.profile['instance_id'], operation)['ok'])

    def test_queue_published_during_shutdown_uses_reviewed_abort_replan(self):
        self.apply()
        queue = copy.deepcopy(self.inventory['connections'][0]['queues'][0])
        queue.update(queue='late', base=queue['base'][:-64] + 'b' * 64)
        queue['keys'] = [queue['base'] + ':metadata']
        original = self.scan
        def published(host, *args):
            return ['0', original(host, *args)[1] + queue['keys']]
        with patch.object(contract.redis_acl, '_redis', side_effect=published):
            with self.assertRaises(DeployError) as error:
                self.execute()
            self.assertEqual('cutover_unknown_queue', error.exception.code)
        self.assertNotIn('snapshot', self.events)
        self.assertNotIn('migrate-layout', self.events)
        prior = self.operation
        self.inventory['connections'][0]['queues'].append(queue)
        archive, replacement = self.bundle(3)
        plan = release.plan_cutover(self.host, self.project, self.profile, archive, digest(archive), abort=True,
                                    reviewed_by='owner@example.test', reason='Declare the queue published by the draining request')
        self.assertEqual(2, len(plan['queues']))
        operation = str(uuid.uuid4())
        with self.assertRaises(DeployError):
            release.apply_cutover(self.host, self.project, self.profile, plan['plan_token'], operation)
        release.apply_cutover(self.host, self.project, self.profile, plan['plan_token'], operation, abort=True)
        self.assertEqual('closed', self.state()['queue_compatibility']['permission'])
        self.assertEqual(prior, self.state()['cutover']['supersedes_operation'])
        self.assertTrue(release.execute_cutover(self.host, self.profile['instance_id'], operation)['ok'])
        self.assertEqual(replacement, self.state()['current'])
        old = json.loads(contract.private_path(self.host, self.entry, 'operation', prior).read_text())
        self.assertEqual('superseded', old['result']['outcome'])
        self.assertEqual(2, len(self.snapshot_calls[0]['scopes'][0]['keys']))

    def test_abort_replan_refuses_snapshot_or_migration_dispatch(self):
        self.apply()
        self.interrupt_at = 'migrate-layout'
        with self.assertRaises(KeyboardInterrupt):
            self.execute()
        archive, _ = self.bundle(3)
        with self.assertRaises(DeployError) as error:
            release.plan_cutover(self.host, self.project, self.profile, archive, digest(archive), abort=True,
                                reviewed_by='owner@example.test', reason='Must refuse')
        self.assertEqual('cutover_abort_unsafe', error.exception.code)

    def test_drain_resolution_never_substitutes_for_process_exit(self):
        self.apply()
        state = self.state()
        state['cutover']['drain_failed'] = True
        self.host.save_state(self.entry, state)
        with self.assertRaises(DeployError) as error:
            release.resolve_cutover_drain(self.host, self.project, self.profile, self.operation,
                                          'owner@example.test', 'Cannot approve a live process')
        self.assertEqual('cutover_stop_unproven', error.exception.code)
        self.assertTrue(self.state()['cutover']['drain_failed'])
        self.assertNotIn('drain_resolutions', self.state()['cutover'])

    def test_snapshot_dispatch_marker_survives_stopping_phase_on_retry(self):
        self.apply()
        with patch.object(self.snapshot, 'capture', side_effect=KeyboardInterrupt()):
            with self.assertRaises(KeyboardInterrupt):
                self.execute()
        state = self.state()
        release._cutover_stop(self.host, self.entry, state)
        self.assertEqual('stopping', self.state()['cutover']['phase'])
        self.assertTrue(self.state()['cutover']['snapshot_started'])
        archive, _ = self.bundle(3)
        with self.assertRaises(DeployError) as error:
            release.plan_cutover(self.host, self.project, self.profile, archive, digest(archive), abort=True,
                                reviewed_by='owner@example.test', reason='Cannot bypass uncertain capture')
        self.assertEqual('cutover_abort_unsafe', error.exception.code)


class InventoryContractTests(unittest.TestCase):
    def setUp(self):
        from test_contracts import fixture
        self.entry = fixture()[2]
        self.value = inspection(self.entry)

    def test_layout_observation_does_not_change_immutable_identity(self):
        identity = contract.inventory_identity(self.value)
        self.value['connections'][0]['queues'][0]['layout'] = '3'
        self.assertEqual(identity, contract.inventory_identity(self.value))
        self.assertEqual(self.value, contract.inventory(json.dumps(self.value)))

    def test_exit_one_blockers_are_displayable_but_never_applicable(self):
        self.value['blockers'] = [{'code': 'invalid_queue_state', 'connection': 'delivery', 'queue': 'default'}]
        self.value['connections'][0]['queues'][0].update(layout='unknown', eligible=False)
        payload = json.dumps(self.value)
        displayed = contract.inventory(payload, 1, allow_blocked=True)
        self.assertEqual(self.value, displayed)
        self.assertFalse(contract.applicable(displayed))
        with self.assertRaises(DeployError):
            contract.inventory(payload, 1)

    def test_unknown_fields_bad_states_and_ineligible_sources_refuse(self):
        cases = []
        for change in (lambda v: v.update(schema=True), lambda v: v.update(unknown=1),
                       lambda v: v['connections'][0]['queues'][0].update(layout='1', eligible=False),
                       lambda v: v['connections'][0]['queues'][0].update(keys=['foreign']),
                       lambda v: v.update(blockers=[{'code': 'redis_unavailable', 'connection': 'delivery', 'queue': None}])):
            value = copy.deepcopy(self.value)
            change(value)
            cases.append(value)
        for value in cases:
            with self.assertRaises(DeployError):
                contract.inventory(json.dumps(value))

    def test_alias_conflicts_refuse_and_unknown_storage_keys_block(self):
        other = copy.deepcopy(self.value['connections'][0])
        other['connection'] = 'other'
        other['policy_id'] = 'f' * 64
        self.value['connections'].append(other)
        with self.assertRaises(DeployError):
            contract.targets(self.value)
        self.value['connections'].pop()
        self.entry['redis_grants'] = {'keys': [self.value['connections'][0]['prefix']]}
        registry = {'instances': {self.entry['profile']['instance_id']: self.entry}}
        with patch.object(contract.redis_acl, 'inspect', return_value={'owner': 'foundry', 'state': 'ready'}), \
             patch.object(contract.redis_acl, '_redis', return_value=['0', ['unknown-key']]):
            with self.assertRaises(DeployError) as error:
                contract.coverage(None, registry, self.entry, self.value)
        self.assertEqual('cutover_unknown_queue', error.exception.code)
