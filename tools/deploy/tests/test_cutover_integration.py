"""Real consumer artisan -> M4 wire contract; opt in with a built fixture and local Redis."""
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest
import uuid

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'lib'))
import cutover_contracts as contract
from contracts import DeployError
from host import Host, Paths
import release
from test_contracts import fixture


class ConsumerArtisanTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        binary = os.environ.get('FOUNDRY_TEST_CUTOVER_ARTISAN')
        address = os.environ.get('FOUNDRY_TEST_REDIS_ADDR')
        if not binary or not address:
            message = 'Set FOUNDRY_TEST_CUTOVER_ARTISAN and FOUNDRY_TEST_REDIS_ADDR for the real cutover boundary test'
            if os.environ.get('FOUNDRY_TEST_CUTOVER_REQUIRED') == '1':
                raise AssertionError(message)
            raise unittest.SkipTest(message)
        cls.binary = Path(binary).resolve(strict=True)
        host, port = address.rsplit(':', 1)
        if host not in ('127.0.0.1', 'localhost') or not port.isdigit():
            raise AssertionError('The cutover integration test accepts only loopback Redis')
        cls.redis_host, cls.redis_port = host, int(port)
        if not shutil.which('redis-cli'):
            raise AssertionError('redis-cli is required for exact-key fixture setup and cleanup')

    def setUp(self):
        temporary = tempfile.TemporaryDirectory(dir=Path(tempfile.gettempdir()).resolve())
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.host = Host(Paths(self.root / 'host'))
        _, _, self.entry = fixture()
        self.candidate = self.root / 'release'
        (self.candidate / 'config').mkdir(parents=True)
        shutil.copy2(self.binary, self.candidate / 'artisan')
        self.environment = 'cutover-' + uuid.uuid4().hex
        self.settings = self.candidate / 'config/deploy.toml'
        self.settings.write_text(f'''
[app.services.namespace]
application = "cutover-consumer"
environment = "{self.environment}"
[app.services.redis]
default = "default"
[app.services.redis.connections.default]
host = "unreachable.invalid"
port = 1
tls = "disable"
[app.services.jobs]
default = "delivery"
[app.services.jobs.connections.delivery]
driver = "redis"
redis = "default"
default_queue = "notifications"
''')
        connections = self.root / 'connections.json'
        connections.write_text(json.dumps({'default': {
            'host': self.redis_host, 'port': self.redis_port, 'tls': 'disable',
            'user': os.environ.get('FOUNDRY_TEST_REDIS_USER', ''),
            'password': os.environ.get('FOUNDRY_TEST_REDIS_PASSWORD', ''),
        }}))
        connections.chmod(0o600)
        self.env_file = self.host.paths.instance_config(self.entry['name']) / 'app.env'
        self.env_file.parent.mkdir(parents=True)
        self.env_file.write_text(f'BOOTSTRAP__APP__SERVICES__REDIS__CONNECTIONS_FILE={connections}\n')
        self.env_file.chmod(0o600)
        self.host.paths.data(self.entry['name']).mkdir(parents=True)
        self.calls = []
        self.host.run = self.run_artisan
        self.owned_keys = []
        self.addCleanup(self.cleanup_keys)
        self.assertEqual('PONG', self.redis('PING'))

    def run_artisan(self, args, *, env, timeout, check, cwd):
        # macOS has no Linux service UID. Only remove runuser; execute the exact
        # artisan argv, settings paths, cwd and environment built by release.app.
        self.assertEqual(['runuser', '-u', self.entry['name'], '--'], args[:4])
        self.assertEqual(str(self.candidate / 'artisan'), args[4])
        self.calls.append(args)
        environment = {key: value for key, value in os.environ.items() if not key.startswith('BOOTSTRAP_')}
        result = subprocess.run(args[4:], env={**environment, **env}, cwd=cwd,
                                timeout=timeout, capture_output=True, text=True)
        if result.returncode == 0:
            self.assertEqual('', result.stderr, 'Restricted artisan wrote lifecycle diagnostics')
        if check and result.returncode:
            raise AssertionError(f'Consumer artisan failed with exit {result.returncode}')
        return result

    def redis(self, *args):
        environment = {**os.environ, 'REDISCLI_AUTH': os.environ.get('FOUNDRY_TEST_REDIS_PASSWORD', '')}
        command = ['redis-cli', '-h', self.redis_host, '-p', str(self.redis_port), '--json']
        if os.environ.get('FOUNDRY_TEST_REDIS_USER'):
            command += ['--user', os.environ['FOUNDRY_TEST_REDIS_USER']]
        # Keys and any private settings stay out of argv, including exact cleanup.
        command += ['-x', 'EVAL', 'return redis.call(unpack(cjson.decode(ARGV[1])))', '0']
        result = subprocess.run(command, input=json.dumps(args), env=environment,
                                capture_output=True, text=True, timeout=10, check=True)
        value = json.loads(result.stdout)
        self.assertFalse(isinstance(value, dict) and 'error' in value, 'Redis fixture command failed')
        return value

    def cleanup_keys(self):
        if self.owned_keys:
            self.redis('DEL', *self.owned_keys)

    def inspect(self, **options):
        return release.inspect_job_queues(self.host, self.entry, self.candidate, **options)

    def migrate(self):
        result = release.app(self.host, self.entry, self.candidate, 'jobs', extra=(
            'migrate-layout', '--connection', 'delivery', '--queue', 'notifications', '--confirm', '--format', 'json'))
        value = json.loads(result.stdout)
        self.assertEqual({'queue', 'migrated'}, set(value))
        self.assertEqual('notifications', value['queue'])
        return value['migrated']

    def test_real_inventory_layout_migration_and_blocked_stdout(self):
        value = self.inspect()
        connection = value['connections'][0]
        self.assertEqual('delivery', connection['connection'])
        self.assertEqual({'application': 'cutover-consumer.jobs.delivery', 'environment': self.environment}, connection['namespace'])
        self.assertEqual(contract.sha(['localhost', self.redis_port, 0, os.environ.get('FOUNDRY_TEST_REDIS_USER', ''), False]), connection['storage_id'])
        queue = connection['queues'][0]
        self.assertEqual(('notifications', 'absent', True), (queue['queue'], queue['layout'], queue['eligible']))
        self.assertEqual(1, len(value['connections']))
        self.assertEqual(1, len(connection['queues']))
        self.assertTrue(all(':' + self.environment + ':' in key for key in queue['keys']))
        self.owned_keys = queue['keys']  # Register exact cleanup before the first write.
        self.assertEqual(0, self.redis('EXISTS', *self.owned_keys))
        identity = contract.inventory_identity(value)
        self.migrate()  # Let Go initialize the real policy; do not duplicate its defaults in Python.
        metadata = next(key for key in self.owned_keys if key.endswith(':metadata'))
        self.redis('HSET', metadata, 'layout', '2')
        self.assertEqual('2', self.inspect()['connections'][0]['queues'][0]['layout'])
        self.assertIs(True, self.migrate())
        self.assertIs(False, self.migrate())
        migrated = self.inspect()
        self.assertEqual('3', migrated['connections'][0]['queues'][0]['layout'])
        self.assertEqual(identity, contract.inventory_identity(migrated))
        self.redis('HSET', metadata, 'layout', '1')
        blocked = self.inspect(allow_blocked=True)
        self.assertFalse(contract.applicable(blocked))
        self.assertIn('layout_1_requires_upgrade', [b['code'] for b in blocked['blockers']])
        with self.assertRaises(DeployError):
            self.inspect()
        self.assertIn('--format', self.calls[0])
        self.assertEqual(str(self.settings), self.calls[0][self.calls[0].index('--config') + 1])
        self.assertEqual(str(self.env_file), self.calls[0][self.calls[0].index('--env-file') + 1])

    def test_real_no_redis_inventory(self):
        self.settings.write_text('[app.services.namespace]\napplication = "cutover-consumer"\nenvironment = "' + self.environment + '"\n')
        self.env_file.write_text('')
        value = self.inspect()
        self.assertEqual([], value['connections'])
        self.assertEqual([], value['blockers'])
        self.assertTrue(contract.applicable(value))
