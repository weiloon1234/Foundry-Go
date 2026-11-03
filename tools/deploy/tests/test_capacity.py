"""Persisted sizing, compatibility, ownership and interrupted capacity changes."""
import copy
import hashlib
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'lib'))
import capacity
import postgres
import redis_acl
import release
import upgrades
from contracts import DeployError
from host import Host, Paths
from test_contracts import fixture, registry
from test_postgres import LEGACY_CAPACITY


class SizingTests(unittest.TestCase):
    def test_exact_table(self):
        for memory, shared, cache, connections, work, maintenance, redis_mib in (
            (1536, '384MB', '768MB', 40, '4MB', '96MB', 192),
            (1950, '448MB', '960MB', 48, '5MB', '112MB', 192),
            (3900, '960MB', '1920MB', 97, '5MB', '240MB', 448),
            (7900, '1920MB', '3904MB', 197, '5MB', '480MB', 960),
            (32000, '8000MB', '16000MB', 200, '16MB', '1GB', 3968),
            (64000, '8GB', '32000MB', 200, '16MB', '1GB', 4096),
        ):
            with self.subTest(memory=memory):
                result = capacity.postgres(memory)
                self.assertEqual({key: result[key] for key in result if key != 'budget_mib'}, {
                    'shared_buffers': shared, 'effective_cache_size': cache, 'max_connections': connections,
                    'work_mem': work, 'maintenance_work_mem': maintenance})
                self.assertEqual(result['budget_mib'], capacity._mib(shared) + capacity._mib(maintenance) + connections * capacity._mib(work))
                self.assertEqual(capacity.redis(memory), {'maxmemory_bytes': redis_mib * capacity.MIB,
                                 'budget_bytes': redis_mib * 2 * capacity.MIB, 'maxclients': 256})
                self.assertTrue(capacity.postgres_valid(result))
                self.assertTrue(capacity.redis_valid(capacity.redis(memory)))

    def test_memory_and_container_limits(self):
        with tempfile.TemporaryDirectory() as root:
            host = Host(Paths(Path(root)))
            with self.assertRaisesRegex(DeployError, 'MemTotal'):
                capacity.memory_mib(host)
            host.owned_write(host.paths.p('/proc/meminfo'), 'MemTotal: 8388608 kB\n')
            self.assertEqual(capacity.memory_mib(host), 8192)
            for limit, expected in [('max', 8192), (str(1950 * capacity.MIB), 1950), (str(16384 * capacity.MIB), 8192), ('0', 0)]:
                host.owned_write(host.paths.p('/sys/fs/cgroup/memory.max'), limit)
                self.assertEqual(capacity.memory_mib(host), expected)
            for code in ('postgres_capacity', 'host_capacity'):
                with self.assertRaises(DeployError) as error:
                    capacity.allocation_memory(host, code)
                self.assertEqual(error.exception.code, code)
            host.owned_write(host.paths.p('/proc/meminfo'), 'not memory')
            with self.assertRaises(DeployError):
                capacity.memory_mib(host)

    def test_redis_effective_settings_use_the_recorded_capacity(self):
        recorded = capacity.redis(3900)
        observed = {"port": "0", "tls-port": str(redis_acl.REDIS_PORT), "bind": "127.0.0.1 ::1", "appendonly": "yes",
                    "appendfsync": "everysec", "save": "900 1 300 10 60 10000", "maxmemory-policy": "noeviction",
                    "maxmemory": str(448 * capacity.MIB), "maxclients": "256", "dir": redis_acl.REDIS_DATA,
                    "aclfile": redis_acl.REDIS_ACLFILE, "tls-auth-clients": "no", "stop-writes-on-bgsave-error": "yes"}
        persistence = "aof_enabled:1\naof_last_write_status:ok\nrdb_last_bgsave_status:ok\n"
        with patch.object(redis_acl, "_verify_connection"), patch.object(redis_acl, "_verify_admin"), \
                patch.object(redis_acl, "_redis", side_effect=[observed, persistence]):
            redis_acl._verify_runtime(object(), recorded)
        for key, value in (("maxmemory", str(128 * capacity.MIB)), ("appendonly", "no"), ("maxclients", "512")):
            with self.subTest(key=key), patch.object(redis_acl, "_verify_connection"), patch.object(redis_acl, "_verify_admin"), \
                    patch.object(redis_acl, "_redis", return_value={**observed, key: value}), self.assertRaises(DeployError) as error:
                redis_acl._verify_runtime(object(), recorded)
            self.assertEqual(error.exception.code, "redis_configuration")

    def test_legacy_and_bounds(self):
        self.assertTrue(capacity.postgres_valid(LEGACY_CAPACITY))
        self.assertTrue(postgres._allocation_valid(postgres._allocation(16, 55432, 'token', LEGACY_CAPACITY)))
        for key, value in [('max_connections', 201), ('max_connections', True), ('work_mem', '17MB'),
                           ('maintenance_work_mem', '1024MB'), ('shared_buffers', '9GB'), ('effective_cache_size', '1MB'),
                           ('budget_mib', 1), ('unexpected', 1)]:
            self.assertFalse(capacity.postgres_valid({**LEGACY_CAPACITY, key: value}), (key, value))
        old = {'maxmemory_bytes': 128 * capacity.MIB, 'budget_bytes': 256 * capacity.MIB, 'maxclients': 256}
        self.assertTrue(capacity.redis_valid(old))
        for key, value in [('maxmemory_bytes', 5 * 1024 * capacity.MIB), ('budget_bytes', 1), ('maxclients', 257), ('extra', 1)]:
            self.assertFalse(capacity.redis_valid({**old, key: value}))


class CapacityHost(Host):
    def __init__(self, root):
        super().__init__(Paths(root))
        self.calls = []
        self.invocations = {}
        self.effective = {}
        self.interrupt_restart = None
        self.unit_states = {}
        self.jobs = {}
        self.fail_restart = None

    def run(self, args, **kwargs):
        self.calls.append(args)
        if args == ['pg_lsclusters', '--no-header']:
            status = 'online' if self.unit_states.get('postgresql@16-foundry.service', 'active') == 'active' else 'down'
            output = f'16 foundry 55432 {status} foundry-postgres /var/lib/foundry/postgresql/16/foundry /log\n'
        elif args[:2] == ['systemctl', 'show']:
            unit = args[2]
            state = self.unit_states.get(unit, 'active')
            if unit.startswith('foundry-backup-'):
                output = 'LoadState=loaded\nMainPID=0\nActiveState=' + self.unit_states.get(unit, 'inactive') + '\n'
            elif 'InvocationID' in args[3]:
                output = 'ActiveState=' + state + '\nSubState=' + ('running' if state == 'active' else 'dead') + '\nJob=' + self.jobs.get(unit, '') + '\nInvocationID=' + self.invocations[unit] + '\n'
            else:
                output = 'LoadState=loaded\nActiveState=' + state + '\nFragmentPath=' + redis_acl.REDIS_UNIT_PATH + '\nMainPID=12\nUnitFileState=enabled\nDropInPaths=\n'
        elif args[:2] == ['systemctl', 'restart']:
            unit = args[2]
            if self.fail_restart == unit:
                self.unit_states[unit] = 'failed'
                raise DeployError('command_failed', 'Injected restart failure')
            self.unit_states[unit] = 'active'
            self.invocations[unit] = format(int(self.invocations[unit], 16) + 1, '032x')
            name = 'redis' if unit == redis_acl.REDIS_UNIT else 'postgresql'
            self.effective[name] = capacity._load(self)['plan']['sized'][name]
            if self.interrupt_restart == unit:
                self.interrupt_restart = None
                raise KeyboardInterrupt()
            output = ''
        else:
            raise AssertionError(args)
        return subprocess.CompletedProcess(args, 0, output, '')


class TransitionTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.host = CapacityHost(Path(temporary.name))
        self.project, self.profile, self.entry = fixture()
        self.registry = registry(self.entry)
        self.pg = dict(postgres._allocation(16, 55432, 'token', LEGACY_CAPACITY), state='ready')
        self.redis = {'owner': 'foundry', 'unit': redis_acl.REDIS_UNIT, 'port': redis_acl.REDIS_PORT,
                      'aclfile': redis_acl.REDIS_ACLFILE, 'config_path': redis_acl.REDIS_CONFIG,
                      'ownership_token': 'token', 'state': 'ready', 'capacity': capacity.redis(1024)}
        self.registry['shared'] = {'postgresql': self.pg, 'redis': self.redis}
        self.host.write_registry(self.registry)
        self.host.owned_write(self.host.paths.p('/proc/meminfo'), 'MemTotal: 4194304 kB\n')
        self.host.owned_write(self.host.paths.p('/etc/os-release'), 'ID=ubuntu\nVERSION_ID="24.04"\n')
        self.host.owned_write(self.host.paths.p('/usr/bin/pg_lsclusters'), '')
        for absolute, text in ((self.pg['config_path'] + '/foundry-owner.json', json.dumps({'host_id': self.registry['host_id'], 'ownership_token': 'token'})),
                               (self.pg['config_path'] + '/conf.d/foundry.conf', postgres._configuration(self.pg)),
                               (self.pg['config_path'] + '/pg_hba.conf', 'fixture hba'),
                               ('/etc/systemd/system/' + self.pg['unit'], postgres._unit(self.pg)),
                               (redis_acl.REDIS_CONFIG, redis_acl._redis_config('token', self.redis['capacity'])),
                               (redis_acl.REDIS_UNIT_PATH, 'fixture redis unit')):
            self.host.owned_write(self.host.paths.p(absolute), text)
        postgres._save_cluster(self.host, {'host_id': self.registry['host_id'], 'allocation': self.pg,
                                          'phase': 'ready', 'hba': {'phase': 'published', 'new': 'fixture hba'}})
        redis_acl._save_claim(self.host, {'host_id': self.registry['host_id'], 'ownership_token': 'token',
            'phase': 'ready', 'allocation': self.redis,
            'files': {path: hashlib.sha256(self.host.paths.p(path).read_bytes()).hexdigest()
                      for path in (redis_acl.REDIS_CONFIG, redis_acl.REDIS_UNIT_PATH)}})
        self.host.invocations = {allocation['unit']: '1' * 32 for allocation in (self.pg, self.redis)}
        self.host.effective = {'postgresql': copy.deepcopy(self.pg['capacity']), 'redis': copy.deepcopy(self.redis['capacity'])}
        self.state = {'current': {'version': 'running'}, 'status': 'healthy', 'last_attempt': None}
        self.start_patch(patch.object(self.host, 'load_state', side_effect=lambda entry: copy.deepcopy(self.state)))
        self.ready = self.start_patch(patch.object(release, 'ready', return_value=True))
        self.verify_pg = self.start_patch(patch.object(postgres, '_verify_settings', side_effect=lambda host, reg:
            self.assertEqual(reg['shared']['postgresql']['capacity'], host.effective['postgresql'])))
        self.verify_redis = self.start_patch(patch.object(redis_acl, '_verify_runtime', side_effect=lambda host, sized:
            self.assertEqual(sized, host.effective['redis'])))

    def start_patch(self, patcher):
        self.addCleanup(patcher.stop)
        return patcher.start()

    def plan(self):
        return capacity.plan(self.host)

    def apply(self):
        return capacity.apply(self.host, self.plan()['plan_token'])

    def test_plan_is_read_only_and_apply_commits_after_readiness(self):
        before = {path: path.read_bytes() for path in self.host.paths.root.rglob('*') if path.is_file()}
        plan = self.plan()
        self.assertEqual(before, {path: path.read_bytes() for path in self.host.paths.root.rglob('*') if path.is_file()})
        self.assertEqual(plan['current']['postgresql'], LEGACY_CAPACITY)
        self.assertEqual(plan['sized']['postgresql'], capacity.postgres(4096))
        self.assertEqual(plan['restart_units'], [self.pg['unit'], self.redis['unit']])
        def ready(host, entry):
            if capacity._load(host)['phase'] != 'committing':
                self.assertEqual(host.read_registry()['shared']['postgresql']['capacity'], LEGACY_CAPACITY)
            return True
        self.ready.side_effect = ready
        result = capacity.apply(self.host, plan['plan_token'])
        self.assertTrue(result['ok'])
        self.assertEqual(self.host.read_registry()['shared']['postgresql']['capacity'], plan['sized']['postgresql'])
        self.assertEqual(self.host.read_registry()['revision'], self.registry['revision'] + 1)
        self.assertEqual(capacity._load(self.host)['phase'], 'complete')
        count = len(self.host.calls)
        self.assertEqual(capacity.apply(self.host, plan['plan_token']), result)
        self.assertEqual(len(self.host.calls), count)

    def test_resize_preserves_archive_timeout_presence(self):
        for present in (True, False):
            with self.subTest(present=present):
                if not present:
                    registry = self.host.read_registry()
                    registry["shared"]["postgresql"].pop("archive_timeout")
                    self.host.write_registry(registry)
                    journal = postgres._cluster_journal(self.host, registry)
                    journal["allocation"].pop("archive_timeout")
                    postgres._save_cluster(self.host, journal)
                    self.host.owned_write(self.host.paths.p(self.pg["config_path"] + "/conf.d/foundry.conf"),
                                          postgres._configuration(registry["shared"]["postgresql"]))
                with patch.object(capacity, "memory_mib", return_value=4096 if present else 2048):
                    self.apply()
                content = self.host.paths.p(self.pg["config_path"] + "/conf.d/foundry.conf").read_text()
                self.assertEqual(present, "archive_timeout = '1min'\n" in content)
                self.assertEqual(present, "archive_timeout" in self.host.read_registry()["shared"]["postgresql"])

    def test_noop_does_not_publish_or_restart(self):
        self.apply()
        before = self.host.paths.registry.read_bytes()
        restarts = len([c for c in self.host.calls if c[:2] == ['systemctl', 'restart']])
        self.assertTrue(self.plan()['noop'])
        self.assertTrue(self.apply()['noop'])
        self.assertEqual(before, self.host.paths.registry.read_bytes())
        self.assertEqual(restarts, len([c for c in self.host.calls if c[:2] == ['systemctl', 'restart']]))

    def test_shrink_is_explicit(self):
        self.apply()
        before = self.host.read_registry()['shared']['postgresql']['capacity']
        with patch.object(capacity, 'memory_mib', return_value=1536):
            self.assertEqual(self.host.read_registry()['shared']['postgresql']['capacity'], before)
            result = self.apply()
        self.assertEqual(result['capacity'], {'postgresql': capacity.postgres(1536), 'redis': capacity.redis(1536)})

    def test_token_and_ram_drift_refuse_before_writes(self):
        token = self.plan()['plan_token']
        with patch.object(capacity, 'memory_mib', return_value=1950), self.assertRaises(DeployError) as error:
            capacity.apply(self.host, token)
        self.assertEqual(error.exception.code, 'plan_stale')
        self.assertIsNone(capacity._load(self.host))
        with self.assertRaises(DeployError):
            capacity.apply(self.host, None)

    def test_rehearsal_pending_recovery_and_mid_deploy_refused(self):
        self.state['last_attempt'] = {'outcome': 'running'}
        with self.assertRaises(DeployError) as error:
            self.apply()
        self.assertEqual(error.exception.code, 'reconciliation_required')
        self.state['last_attempt'] = None
        with patch('recover._load', return_value={'phase': 'timers'}), self.assertRaises(DeployError) as error:
            self.apply()
        self.assertEqual(error.exception.code, 'recovery_pending')
        self.entry['recovered_from'] = {'mode': 'rehearsal'}
        with patch.object(self.host, 'read_registry', return_value=self.registry), self.assertRaises(DeployError) as error:
            self.apply()
        self.assertEqual(error.exception.code, 'rehearsal_read_only')
        self.assertIsNone(capacity._load(self.host))

    def test_observed_new_invocation_skips_extra_restart_and_ram_stays_pinned(self):
        plan = self.plan()
        self.host.interrupt_restart = self.pg['unit']
        with self.assertRaises(KeyboardInterrupt):
            capacity.apply(self.host, plan['plan_token'])
        with patch.object(capacity, 'memory_mib', side_effect=AssertionError('resume recomputed memory')):
            result = self.apply()
        self.assertEqual(result['capacity'], plan['sized'])
        self.assertEqual(self.host.calls.count(['systemctl', 'restart', self.pg['unit']]), 1)

    def test_restart_intent_without_execution_is_reissued_by_tool(self):
        save = capacity._save
        def interrupt(host, journal):
            save(host, journal)
            if journal['services']['postgresql']['phase'] == 'restarting':
                raise KeyboardInterrupt()
        with patch.object(capacity, '_save', side_effect=interrupt), self.assertRaises(KeyboardInterrupt):
            self.apply()
        self.assertNotIn(['systemctl', 'restart', self.pg['unit']], self.host.calls)
        with patch.object(capacity, 'memory_mib', side_effect=AssertionError('resume recomputed RAM')):
            self.assertTrue(self.apply()['ok'])
        self.assertEqual(self.host.calls.count(['systemctl', 'restart', self.pg['unit']]), 1)

    def test_readiness_failure_keeps_old_allocations_and_blocks_mutations(self):
        self.ready.return_value = False
        with self.assertRaises(DeployError) as error:
            self.apply()
        self.assertEqual(error.exception.code, 'capacity_not_ready')
        self.assertEqual(self.host.read_registry()['shared']['postgresql']['capacity'], LEGACY_CAPACITY)
        with self.assertRaises(DeployError) as error:
            upgrades.check_client(self.host, {}, 'activate')
        self.assertEqual(error.exception.code, 'capacity_pending')
        self.ready.return_value = True
        self.assertTrue(self.apply()['ok'])

    def test_unowned_config_change_on_resume_refused(self):
        self.host.interrupt_restart = self.pg['unit']
        with self.assertRaises(KeyboardInterrupt):
            self.apply()
        self.host.owned_write(self.host.paths.p(redis_acl.REDIS_CONFIG), 'foreign')
        with self.assertRaises(DeployError) as error:
            self.apply()
        self.assertEqual(error.exception.code, 'redis_ownership')

    def test_mutation_waiting_on_host_lock_rechecks_pending_transition(self):
        self.host.interrupt_restart = self.pg['unit']
        with self.assertRaises(KeyboardInterrupt):
            self.apply()
        with self.assertRaises(DeployError) as error:
            with self.host.lock('host'):
                self.fail('A mutation entered while capacity is pending')
        self.assertEqual(error.exception.code, 'capacity_pending')

    def test_all_journal_phases_resume_without_extra_restarts(self):
        # Every subcase has its own host and real private/public journal files.
        for name, phase in ((None, 'services'), ('postgresql', 'publishing'), ('postgresql', 'verified'),
                            ('redis', 'publishing'), ('redis', 'verified'), (None, 'readiness'),
                            (None, 'committing'), (None, 'complete')):
            with self.subTest(service=name, phase=phase):
                case = TransitionTests()
                case.setUp()
                try:
                    save = capacity._save
                    interrupted = []
                    def crash(host, journal):
                        save(host, journal)
                        actual = journal['services'][name]['phase'] if name else journal['phase']
                        if actual == phase and not interrupted:
                            interrupted.append(True)
                            raise KeyboardInterrupt()
                    token = case.plan()['plan_token']
                    with patch.object(capacity, '_save', side_effect=crash), self.assertRaises(KeyboardInterrupt):
                        capacity.apply(case.host, token)
                    self.assertTrue(capacity.apply(case.host, token)['ok'])
                    for unit in case.host.invocations:
                        self.assertEqual(case.host.calls.count(['systemctl', 'restart', unit]), 1)
                    self.assertEqual(capacity._load(case.host)['phase'], 'complete')
                finally:
                    case.doCleanups()

    def test_partial_commit_resumes_each_write_without_restart_or_revision_drift(self):
        for owner, method in ((postgres, '_save_cluster'), (redis_acl, '_save_claim'), (None, 'write_registry')):
            with self.subTest(write=method):
                case = TransitionTests()
                case.setUp()
                try:
                    target = owner or case.host
                    original = getattr(target, method)
                    def crash(*args, **kwargs):
                        original(*args, **kwargs)
                        raise KeyboardInterrupt()
                    token = case.plan()['plan_token']
                    with patch.object(target, method, side_effect=crash), self.assertRaises(KeyboardInterrupt):
                        capacity.apply(case.host, token)
                    self.assertEqual(capacity._load(case.host)['phase'], 'committing')
                    self.assertTrue(capacity.apply(case.host, token)['ok'])
                    self.assertEqual(case.host.read_registry()['revision'], case.registry['revision'] + 1)
                    for unit in case.host.invocations:
                        self.assertEqual(case.host.calls.count(['systemctl', 'restart', unit]), 1)
                finally:
                    case.doCleanups()

    def test_server_wiring_and_confirmation(self):
        from test_manage import manager
        metadata = upgrades.protocol()
        request = {'project': self.project, 'profile': self.profile, 'protocol': metadata['protocol'],
                   'client': {'generation': metadata['generation']}}
        plan = manager.execute(self.host, 'capacity-plan', request)
        with self.assertRaises(DeployError) as error:
            manager.execute(self.host, 'capacity-apply', {**request, 'plan_token': plan['plan_token']})
        self.assertEqual(error.exception.code, 'confirmation_required')
        result = manager.execute(self.host, 'capacity-apply', {**request, 'plan_token': plan['plan_token'], 'confirm': True})
        self.assertTrue(result['ok'])
        self.assertIn('capacity-plan', upgrades.READ_ACTIONS)
        self.assertNotIn('capacity-apply', upgrades.READ_ACTIONS)

    def test_local_action_prints_plan_and_sends_token_after_confirmation(self):
        from test_local import cli
        from unittest.mock import MagicMock
        remote = MagicMock()
        remote.request.side_effect = [{'noop': False, 'plan_token': 'a' * 64}, {'ok': True}]
        with patch.dict('os.environ', {'ACTION': 'capacity'}), patch.object(cli, 'SSH', return_value=remote), \
                patch.object(cli, 'confirm') as confirm, patch.object(cli, 'display') as display:
            cli.deploy(self.project, self.profile, {})
        self.assertEqual(remote.request.call_args_list[0].args[0], 'capacity-plan')
        self.assertEqual(remote.request.call_args_list[1].args, ('capacity-apply', {
            'project': self.project, 'profile': self.profile, 'plan_token': 'a' * 64, 'confirm': True}))
        confirm.assert_called_once()
        self.assertEqual(display.call_count, 2)

    def test_readiness_list_preserves_never_deployed_and_stopped_instances(self):
        _, _, never = fixture(project_slug='never', host_id=self.registry['host_id'], port=8101)
        _, _, stopped = fixture(project_slug='stopped', host_id=self.registry['host_id'], port=8102)
        registered = registry(self.entry, never, stopped)
        registered['shared'] = self.registry['shared']
        self.host.write_registry(registered)
        states = {self.entry['profile']['instance_id']: self.state,
                  never['profile']['instance_id']: {'status': 'never_deployed', 'current': None, 'last_attempt': None},
                  stopped['profile']['instance_id']: {'status': 'stopped', 'current': {'version': 'stopped'}, 'last_attempt': None}}
        before = copy.deepcopy(states)
        with patch.object(self.host, 'load_state', side_effect=lambda entry: copy.deepcopy(states[entry['profile']['instance_id']])):
            plan = self.plan()
            self.assertEqual(plan['ready_instances'], [self.entry['profile']['instance_id']])
            self.ready.side_effect = lambda host, entry: entry['profile']['instance_id'] == self.entry['profile']['instance_id']
            self.assertTrue(capacity.apply(self.host, plan['plan_token'])['ok'])
        self.assertEqual(states, before)
        self.assertEqual(capacity._load(self.host)['plan']['ready_instances'], plan['ready_instances'])
        self.assertEqual(self.ready.call_count, 2)
        self.assertTrue(all(call.args[1]['name'] == self.entry['name'] for call in self.ready.call_args_list))
        capacity.require_idle(self.host)

    def test_rolled_back_instance_is_in_readiness_plan(self):
        self.state['status'] = 'rolled_back'
        self.assertEqual(self.plan()['ready_instances'], [self.entry['profile']['instance_id']])

    def test_new_apply_refuses_running_backup_before_journaling_or_restart(self):
        token = self.plan()['plan_token']
        for kind in ('full', 'diff'):
            unit = 'foundry-backup-' + kind + '-cluster.service'
            self.host.unit_states[unit] = 'active'
            with self.subTest(kind=kind), self.assertRaises(DeployError) as error:
                capacity.apply(self.host, token)
            self.assertEqual(error.exception.code, 'capacity_backup_running')
            self.assertIn('rerun ACTION=capacity', error.exception.detail)
            self.assertIsNone(capacity._load(self.host))
            self.assertFalse(any(call[:2] == ['systemctl', 'restart'] for call in self.host.calls))
            self.host.unit_states[unit] = 'inactive'
        self.assertTrue(capacity.apply(self.host, token)['ok'])

    def test_failed_or_inactive_service_resume_restarts_once_and_keeps_ownership_checks(self):
        for name, state in (('postgresql', 'failed'), ('postgresql', 'inactive'), ('redis', 'failed'), ('redis', 'inactive')):
            with self.subTest(service=name, state=state):
                case = TransitionTests()
                case.setUp()
                try:
                    unit = case.registry['shared'][name]['unit']
                    case.host.fail_restart = unit
                    with self.assertRaises(DeployError) as error:
                        case.apply()
                    self.assertEqual(error.exception.code, 'capacity_restart_failed')
                    self.assertIn('Rerun ACTION=capacity', error.exception.detail)
                    self.assertIn('ACTION=recover', error.exception.detail)
                    self.assertEqual(case.host.calls.count(['systemctl', 'restart', unit]), 1)
                    self.assertEqual(capacity._load(case.host)['services'][name]['phase'], 'restarting')
                    case.host.unit_states[unit] = state
                    case.host.fail_restart = None
                    with patch.object(capacity, 'memory_mib', side_effect=AssertionError('resume resized RAM')), \
                            patch.object(capacity, '_backups_idle', side_effect=AssertionError('resume checked backups')):
                        self.assertTrue(case.apply()['ok'])
                    self.assertEqual(case.host.calls.count(['systemctl', 'restart', unit]), 2)
                finally:
                    case.doCleanups()

    def test_pending_systemd_job_waits_without_restarting(self):
        self.host.interrupt_restart = self.pg['unit']
        with self.assertRaises(KeyboardInterrupt):
            self.apply()
        self.host.jobs[self.pg['unit']] = '42'
        before = list(self.host.calls)
        with self.assertRaises(DeployError) as error:
            self.apply()
        self.assertEqual(error.exception.code, 'capacity_restart_pending')
        self.assertIn('Wait and rerun ACTION=capacity', error.exception.detail)
        self.assertFalse(any(call[:2] == ['systemctl', 'restart'] for call in self.host.calls[len(before):]))
        self.host.jobs.clear()
        self.assertTrue(self.apply()['ok'])
        self.assertEqual(self.host.calls.count(['systemctl', 'restart', self.pg['unit']]), 1)

    def test_down_redis_with_foreign_unit_is_still_refused(self):
        self.host.fail_restart = self.redis['unit']
        with self.assertRaises(DeployError):
            self.apply()
        with patch.object(redis_acl, '_unit', return_value={'LoadState': 'loaded', 'ActiveState': 'failed',
                'FragmentPath': '/foreign.service', 'DropInPaths': ''}), self.assertRaises(DeployError) as error:
            self.apply()
        self.assertEqual(error.exception.code, 'redis_ownership')
