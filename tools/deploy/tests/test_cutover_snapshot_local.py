"""Real local Redis/age recovery; systemd, managed-host ACL observations and object transport are fixtures."""
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import unittest
from unittest.mock import patch
import uuid

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'lib'))
import backup
from contracts import DeployError, REDIS_ACLFILE
import cutover_contracts as contract
import redis_acl
import redis_snapshot
import release
import test_cutover


class LocalSnapshotRoundTripTests(unittest.TestCase):
    def test_capture_apply_lost_response_resume(self):
        missing = [tool for tool in ('redis-server', 'redis-cli', 'redis-check-rdb', 'age', 'age-keygen') if not shutil.which(tool)]
        if missing:
            self.skipTest('Local recovery tools absent: ' + ', '.join(missing))
        address = os.environ.get('FOUNDRY_TEST_REDIS_ADDR', '127.0.0.1:6379')
        hostname, port = address.rsplit(':', 1)
        self.assertIn(hostname, ('127.0.0.1', 'localhost'))
        scenario = test_cutover.CutoverTests()
        self.addCleanup(scenario.doCleanups)
        scenario.setUp()
        host, entry = scenario.host, scenario.entry
        connection = scenario.inventory['connections'][0]
        application = entry['allocations']['namespace']['application'] + '.recovery-' + uuid.uuid4().hex
        prefix = f"foundry:jobs:v1:{application}:{entry['environment']}:"
        connection['namespace']['application'] = application
        connection['prefix'] = prefix
        queue = connection['queues'][0]
        queue['base'] = prefix + 'queue:' + 'a' * 64
        keys = queue['keys'] = sorted(queue['base'] + ':' + suffix for suffix in ('metadata', 'ready', 'records'))
        registry = host.read_registry()
        entry['redis_grants'] = {'keys': [prefix], 'channels': []}
        registry['instances'][scenario.profile['instance_id']] = entry
        host.write_registry(registry)
        password = scenario.secret
        policy = redis_acl._policy(entry, password, [prefix + '*'], [])
        redis_acl._save_state(host, entry, {'instance_id': scenario.profile['instance_id'], 'committed': policy})
        host.owned_write(host.paths.p(REDIS_ACLFILE), redis_acl._acl_line(entry, password) + '\n', mode=0o640)

        def redis(*args, input=None):
            command = ['redis-cli', '-h', hostname, '-p', port, '--json']
            if os.environ.get('FOUNDRY_TEST_REDIS_USER'):
                command += ['--user', os.environ['FOUNDRY_TEST_REDIS_USER']]
            if args[0] == 'EVAL':
                command += ['-x', *args]
            else:
                command += ['-x', 'EVAL', 'return redis.call(unpack(cjson.decode(ARGV[1])))', '0']
                input = json.dumps(args)
            result = subprocess.run(command, input=input, text=True, capture_output=True, timeout=10,
                                    env={**os.environ, 'REDISCLI_AUTH': os.environ.get('FOUNDRY_TEST_REDIS_PASSWORD', '')})
            self.assertEqual(0, result.returncode, 'Local Redis command failed')
            value = json.loads(result.stdout)
            self.assertFalse(isinstance(value, dict) and 'error' in value, 'Local Redis rejected command')
            return value

        self.assertEqual('PONG', redis('PING'))
        self.assertEqual(0, redis('EXISTS', *keys))
        self.addCleanup(lambda: redis('DEL', *keys))
        redis('HSET', keys[0], 'field', 'original')
        redis('SET', keys[1], 'expires', 'PX', '120000')
        expiry = redis('PEXPIRETIME', keys[1])
        identity = scenario.root / 'test-age.identity'
        subprocess.run(['age-keygen', '-o', str(identity)], capture_output=True, check=True, timeout=10)
        recipient = subprocess.run(['age-keygen', '-y', str(identity)], capture_output=True, text=True, check=True, timeout=10).stdout.strip()
        repository = scenario.root / 'object-repository'
        repository.mkdir()
        original_run = host.run
        commands = []
        def run(args, **options):
            commands.append(args)
            if args[0] == 'prlimit':
                args = args[args.index('--') + 1:]  # Linux sandbox wrapper; use native tools on macOS.
            if args[0] in ('age', 'redis-check-rdb'):
                result = subprocess.run(args, capture_output=True, text=True, timeout=options.get('timeout', 60))
                if result.returncode:
                    raise DeployError('command_failed', 'Local recovery tool failed')
                return result
            return original_run(args, **options)

        def transport(host, args):
            def local(value):
                return repository / value.removeprefix('backup:') if value.startswith('backup:') else Path(value)
            if args[0] == 'copyto':
                source, target = map(local, args[1:3])
                target.parent.mkdir(parents=True, exist_ok=True)
                if '--immutable' in args and target.exists():
                    self.assertEqual(source.read_bytes(), target.read_bytes())
                else:
                    shutil.copyfile(source, target)
            elif args[0] == 'check':
                name = args[args.index('--include') + 1].lstrip('/')
                self.assertEqual((local(args[1]) / name).read_bytes(), (local(args[2]) / name).read_bytes())
            else:
                self.fail('Unexpected local object transport operation')

        lose_response = [False]
        def admin(host, *args, **options):
            if args[:2] == ('ACL', 'GETUSER'):
                return {'flags': ['on'], 'passwords': [hashlib.sha256(password.encode()).hexdigest()],
                        'keys': '~' + prefix + '*', 'channels': '', 'selectors': [],
                        'commands': '-@all ' + ' '.join('+' + item for item in sorted(redis_acl.REDIS_COMMANDS))}
            value = redis(*args, **options)
            if lose_response[0] and args[:2] == ('EVAL', redis_snapshot.RESTORE_SCRIPT):
                lose_response[0] = False
                raise DeployError('command_unavailable', 'Lost response after actual Redis restore')
            return value

        with patch.object(host, 'run', side_effect=run), \
                patch.object(release, 'release_redis_inventory', return_value={'patterns': [prefix + '*'], 'channels': []}), \
                patch.object(release, '_snapshot_module', return_value=redis_snapshot), \
                patch.object(redis_acl, '_redis', side_effect=admin), \
                patch.object(backup, '_settings', return_value={'age_recipients': [recipient]}), \
                patch.object(backup, '_repository_settings', return_value={'endpoint': 'https://local.invalid', 'region': 'auto', 'bucket': 'test'}), \
                patch.object(backup, '_rclone', side_effect=transport):
            scenario.apply()
            scenario.unknown_at = 'migrate-layout'
            with self.assertRaises(DeployError):
                scenario.execute()
            captured = scenario.state()['cutover']['snapshot']
            self.assertEqual(2, captured['key_count'])
            redis('HSET', keys[0], 'field', 'changed')
            redis('SET', keys[2], 'extra')
            operation = str(uuid.uuid4())
            plan = release.plan_redis_restore(host, scenario.project, scenario.profile, operation,
                                             captured['snapshot_id'], identity.read_text())
            self.assertEqual((2, 1), (plan['restore_keys'], plan['remove_keys']))
            release.apply_redis_restore(host, scenario.project, scenario.profile, plan['plan_token'], operation)
            lose_response[0] = True
            with self.assertRaises(DeployError):
                release.execute_cutover(host, scenario.profile['instance_id'], operation)
            safety = scenario.state()['cutover']['safety_snapshot']
            release.apply_redis_restore(host, scenario.project, scenario.profile, plan['plan_token'], operation, resume=True)
            self.assertTrue(release.execute_cutover(host, scenario.profile['instance_id'], operation)['ok'])
            self.assertEqual(safety, scenario.state()['cutover']['safety_snapshot'])
            self.assertEqual('closed', scenario.state()['queue_compatibility']['permission'])
            self.assertEqual('original', redis('HGET', keys[0], 'field'))
            self.assertEqual('expires', redis('GET', keys[1]))
            self.assertEqual(expiry, redis('PEXPIRETIME', keys[1]))
            self.assertEqual(0, redis('EXISTS', keys[2]))
            self.assertTrue(any('redis-check-rdb' in args for args in commands))
            self.assertTrue(any(args[0] == 'age' for args in commands))
