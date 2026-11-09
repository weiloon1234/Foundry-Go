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
        prefix = self.inventory['connections'][0]['prefix']
        registry = self.host.read_registry()
        registry['tools']['generation'] = 5
        registry['instances'][self.profile['instance_id']]['redis_grants'] = {'keys': [prefix], 'channels': []}
        self.host.write_registry(registry)
        self.entry = registry['instances'][self.profile['instance_id']]
        self.events = []
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
        patch.object(release, 'cutover_executor', return_value='absent').start()
        patch.object(release, '_require_executor_alone').start()
        patch('upgrades._installed', return_value=self.root / 'tools').start()
        patch.object(release.backup, '_settings', return_value={}).start()
        patch.object(release.backup, '_repository_settings', return_value={}).start()
        patch.object(release, 'inspect_job_queues', side_effect=lambda *args: copy.deepcopy(self.inventory)).start()
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
        self.restore_calls = []
        def plan(host, registry, entry, request):
            self.assertEqual({'schema', 'operation_id', 'snapshot_id', 'inventory_sha256', 'scopes', 'identity_file'}, set(request))
            identity = Path(request['identity_file'])
            self.assertEqual(0o600, identity.stat().st_mode & 0o777)
            self.assertEqual('PRIVATE IDENTITY', identity.read_text())
            self.identity_file = identity
            source_request = {**request, 'operation_id': str(uuid.uuid4()), 'source_release': self.source['version'],
                              'configuration_revision': entry['configuration_revision']}
            return {'schema': 1, 'operation_id': self.restore_operation, 'snapshot': evidence(entry, source_request),
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
