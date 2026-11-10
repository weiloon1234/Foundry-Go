"""Recovery handoff retains prepared files and acknowledged instance secrets."""
from contextlib import ExitStack
import io
import json
from pathlib import Path
import subprocess
import tarfile
import tempfile
import unittest
from unittest.mock import Mock, patch

import acceptance_backup
import acceptance_capacity
import acceptance_host as host_phase
from contracts import DeployError
from test_contracts import fixture
import upgrades


class PreparedHostTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name).resolve()
        work = patch.object(host_phase, 'WORK', self.root)
        work.start()
        self.addCleanup(work.stop)

    def archive(self):
        topology = host_phase.validate_topology({'schema': 1, 'roles': [{'name': 'http'}, {'name': 'scheduler'}]})
        path = self.root / 'app.tar.gz'
        with tarfile.open(path, 'w:gz') as archive:
            for name, data in {'manifest.json': json.dumps({'topology_sha256': topology['sha256']}).encode(),
                               'config/deploy.toml': b'[app.services.storage.disks.attachments]\ndriver="r2"\n'}.items():
                member = tarfile.TarInfo(name)
                member.size = len(data)
                archive.addfile(member, io.BytesIO(data))
        return path

    def test_topology_reuse_is_explicit_and_requires_identical_regular_file(self):
        archive = self.archive()
        expected = host_phase.archive_contract(archive)
        path = self.root / 'topology.json'
        before = path.read_bytes(), path.stat().st_ino
        with self.assertRaises(FileExistsError):
            host_phase.archive_contract(archive)
        self.assertEqual(expected, host_phase.archive_contract(archive, reuse=True))
        self.assertEqual(before, (path.read_bytes(), path.stat().st_ino))
        path.write_text('{}')
        with self.assertRaisesRegex(AssertionError, 'Prepared topology differs'):
            host_phase.archive_contract(archive, reuse=True)
        self.assertEqual('{}', path.read_text())
        path.unlink()
        with self.assertRaises((FileNotFoundError, DeployError)):
            host_phase.archive_contract(archive, reuse=True)
        target = self.root / 'other.json'
        target.write_bytes(before[0])
        path.symlink_to(target)
        with self.assertRaises(DeployError):
            host_phase.archive_contract(archive, reuse=True)

    def test_age_identity_reuse_never_generates_or_replaces_a_key(self):
        identity = self.root / 'age-identity.txt'
        identity.write_text('owned test identity')
        settings = dict(app_bucket='app', backup_bucket='backup', endpoint='https://s3',
                        access_key='test', secret_key='test')
        with patch.object(acceptance_backup, 's3_settings', return_value=settings), \
                patch.object(acceptance_backup, 'rclone'), \
                patch.object(host_phase, 'run', return_value=subprocess.CompletedProcess([], 0, 'age1owned\n')) as run:
            private = host_phase.bootstrap_secrets({'attachments': {'driver': 'r2'}}, reuse_identity=True)
            self.assertEqual(['age1owned'], private['age_recipients'])
            run.assert_called_once_with(['age-keygen', '-y', str(identity)])
            self.assertEqual('owned test identity', identity.read_text())
            identity.unlink()
            with self.assertRaises((AssertionError, DeployError)):
                host_phase.bootstrap_secrets({'attachments': {'driver': 'r2'}}, reuse_identity=True)
            self.assertEqual(1, run.call_count)

    def test_handoff_bootstraps_existing_instance_without_resubmitting_secrets(self):
        instances = [fixture()[:2] for _ in range(3)]
        prepared = {'name': 'abc-production'}
        host = Mock()
        host.read_registry.return_value = {'host_id': host_phase.HOST_ID, 'tools': {'bundle_sha256': 'current'}}
        host.require_identity.return_value = ({}, prepared)
        host.load_state.return_value = {'status': 'healthy', 'current': {'version': 'upgraded'}}
        host.paths.instance_config.return_value = self.root
        (self.root / 'owned').write_text('disposable acceptance fixture\n')
        (self.root / 'bootstrap-secrets.json').write_text(json.dumps({'age_recipients': ['age1owned']}))
        private = {'age_recipients': ['age1owned'], 'app_storage': {'disks': {}},
                   'configuration': {'APP_TEST': 'new-instance-only'}}
        with ExitStack() as stack:
            stack.enter_context(patch.object(host_phase.sys, 'argv', ['acceptance_host.py', '--archive', 'input', '--after-upgrade']))
            stack.enter_context(patch.object(host_phase, 'Host', return_value=host))
            stack.enter_context(patch.object(host_phase, 'fixtures', return_value=instances))
            for name in ('guard', 'atomic_write'):
                stack.enter_context(patch.object(host_phase, name))
            stack.enter_context(patch.object(upgrades, 'tool_bundle', return_value=(None, None, 'current')))
            archive = stack.enter_context(patch.object(host_phase, 'archive_contract', return_value=({}, {})))
            secrets = stack.enter_context(patch.object(host_phase, 'bootstrap_secrets', return_value=private))
            stack.enter_context(patch.object(acceptance_backup, 's3_settings', return_value={'app_bucket': 'owned'}))
            stack.enter_context(patch.object(acceptance_backup, 'rclone'))
            prepare = stack.enter_context(patch.object(host_phase, 'prepare'))
            stack.enter_context(patch.object(acceptance_capacity.capacity, 'memory_mib', return_value=4096))
            stack.enter_context(patch.object(acceptance_capacity, 'verify_settings', side_effect=RuntimeError('end of bootstrap')))
            with self.assertRaisesRegex(RuntimeError, 'end of bootstrap'):
                host_phase.main()
            archive.assert_called_once_with(Path('input'), reuse=True)
            secrets.assert_called_once_with({}, reuse_identity=True)
            self.assertEqual(3, prepare.call_count)
            self.assertIsNone(prepare.call_args_list[0].args[2])
            self.assertTrue(all(call.args[2] == private for call in prepare.call_args_list[1:]))
            private['age_recipients'] = ['age1wrong']
            prepare.reset_mock()
            with self.assertRaisesRegex(AssertionError, 'does not match'):
                host_phase.main()
            prepare.assert_not_called()
