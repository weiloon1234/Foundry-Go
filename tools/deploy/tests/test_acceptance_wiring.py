"""Acceptance entrypoint selection without Docker or host mutations."""
import importlib.util
from contextlib import ExitStack
import json
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import Mock, patch

import acceptance_host
import acceptance_nginx
import build_acceptance
from test_contracts import fixture


class AcceptanceWiringTests(unittest.TestCase):
    def test_upgrade_precedes_ordinary_host_exercises(self):
        runner = Path(__file__).with_name('run_acceptance.sh').read_text()
        phases = runner[runner.index('phase foreign-baseline '):runner.index('# Set before invocation:')]
        ordinary = ['foreign-baseline', 'host', 'roles', 'capacity', 'backup', 'nginx', 'edge']
        for enabled, expected in ((0, ordinary), (1, [ordinary[0], 'upgrade-prepare', 'upgrade',
                                                    *ordinary[1:]])):
            with self.subTest(upgrade=enabled):
                result = subprocess.run(['bash', '-c',
                    'set -eu\nphase() { echo "$*"; }\nmode=foreign\nupgrade=' + str(enabled) + '\n' + phases],
                    capture_output=True, text=True, check=True, timeout=5)
                calls = [line.split() for line in result.stdout.splitlines()]
                self.assertEqual(expected, [call[0] for call in calls])
                for call in calls:
                    if call[0] == 'host':
                        self.assertEqual(bool(enabled), '--after-upgrade' in call)
                    if call[0] == 'upgrade-prepare':
                        self.assertIn('--prepare-upgrade', call)
                        self.assertIn('--tools-root', call)
                    if '/verification/old-deploy' in call:
                        self.assertIn(call[0], ('upgrade-prepare', 'upgrade'))

    def test_upgrade_preparation_activates_only_first_instance_via_installed_launcher(self):
        import acceptance_backup
        with tempfile.TemporaryDirectory() as directory, ExitStack() as stack:
            root = Path(directory)
            (root / 'server').mkdir()
            (root / 'server/manage.py').touch()
            (root / 'protocol.json').touch()
            stack.enter_context(patch.object(acceptance_host.sys, 'argv', ['acceptance_host.py',
                '--archive', str(root / 'input.tar.gz'), '--prepare-upgrade', '--tools-root', str(root)]))
            stack.enter_context(patch.object(acceptance_host, 'WORK', root / 'work'))
            instances = [fixture()[:2] for _ in range(3)]
            stack.enter_context(patch.object(acceptance_host, 'fixtures', return_value=instances))
            host = Mock()
            host.read_registry.return_value = None
            stack.enter_context(patch.object(acceptance_host, 'Host', return_value=host))
            for name in ('guard', 'atomic_write'):
                stack.enter_context(patch.object(acceptance_host, name))
            stack.enter_context(patch.object(acceptance_host, 'archive_contract', return_value=({}, {})))
            stack.enter_context(patch.object(acceptance_host, 'bootstrap_secrets',
                                           return_value={'app_storage': {'disks': {}}}))
            stack.enter_context(patch.object(acceptance_backup, 's3_settings', return_value={'app_bucket': 'owned'}))
            stack.enter_context(patch.object(acceptance_backup, 'rclone'))
            stack.enter_context(patch.object(acceptance_host, 'archive_for', return_value=root / 'bound.tar.gz'))
            stack.enter_context(patch.object(acceptance_host, 'digest', return_value='checksum'))
            prepare = stack.enter_context(patch.object(acceptance_host, 'prepare'))
            action = stack.enter_context(patch.object(acceptance_host, 'action'))
            renewal = stack.enter_context(patch.object(acceptance_host, 'certificate_renewal'))
            isolation = stack.enter_context(patch.object(acceptance_host, 'isolation'))
            stack.enter_context(patch("upgrades.protocol_metadata", return_value={"client": {"generation": 6}}))
            acceptance_host.main()
            prepare.assert_called_once()
            self.assertEqual(instances[0], prepare.call_args.args[1])
            self.assertEqual(root.resolve(), prepare.call_args.kwargs['tools'])
            action.assert_called_once()
            self.assertEqual('activate', action.call_args.args[0])
            self.assertEqual(str(root / 'bound.tar.gz'), action.call_args.args[1]['archive'])
            renewal.assert_not_called()
            isolation.assert_not_called()

    def test_nginx_readiness_uses_the_deploy_body_contract(self):
        for body, expected in (({"ready": True}, True), ({"status": "ready"}, True),
                               ({"ready": "true"}, False), ({"status": "ready", "x": 1}, False),
                               ({"status": "starting"}, False)):
            with self.subTest(body=body), patch.object(acceptance_nginx, 'https_response',
                    return_value=(200, {acceptance_nginx.HEADER: 'owned'}, json.dumps(body).encode())):
                if expected:
                    self.assertEqual('owned', acceptance_nginx.http_readiness({'name': 'owned'}))
                else:
                    with self.assertRaises(AssertionError):
                        acceptance_nginx.http_readiness({'name': 'owned'})

    def test_bootstrap_uses_explicit_bundle(self):
        project, profile, _ = fixture()
        replies = [
            subprocess.CompletedProcess([], 0, json.dumps({'ok': True, 'plan_token': 'reviewed'})),
            subprocess.CompletedProcess([], 0, json.dumps({'ok': True, 'provisioning': 'ready'})),
        ]
        with patch.object(acceptance_host, 'fixture_topology', return_value={
                'schema': 1, 'roles': [{'name': 'http'}]}), \
                patch.object(acceptance_host, 'run', side_effect=replies) as run, \
                patch('upgrades.protocol_metadata', return_value={'client': {'generation': 6}}):
            acceptance_host.prepare(None, (project, profile), {'configuration': {}},
                                    tools=Path('/verification/old-deploy'))
        self.assertEqual([call.args[0][2] for call in run.call_args_list],
                         ['bootstrap-plan', 'bootstrap-apply'])
        for call in run.call_args_list:
            self.assertEqual(call.args[0][1], '/verification/old-deploy/server/manage.py')
        self.assertEqual(json.loads(run.call_args_list[1].kwargs['input'])['plan_token'], 'reviewed')
        self.assertEqual(6, json.loads(run.call_args_list[1].kwargs['input'])['client']['generation'])

    def test_builder_refuses_existing_archive_path_file_before_docker(self):
        with tempfile.TemporaryDirectory() as temporary:
            destination = Path(temporary) / 'archive.txt'
            destination.write_text('retained')
            with patch.object(importlib.util, 'spec_from_file_location') as load, \
                    self.assertRaisesRegex(ValueError, 'never overwritten'):
                build_acceptance.build_fixture('arm64', destination)
            load.assert_not_called()
            self.assertEqual(destination.read_text(), 'retained')

    def test_builder_refuses_dangling_archive_path_symlink(self):
        with tempfile.TemporaryDirectory() as temporary:
            destination = Path(temporary) / 'archive.txt'
            target = Path(temporary) / 'missing'
            destination.symlink_to(target)
            with self.assertRaisesRegex(ValueError, 'never overwritten'):
                build_acceptance.build_fixture('arm64', destination)
            self.assertFalse(target.exists())

    def test_builder_requires_existing_parent_before_docker(self):
        with tempfile.TemporaryDirectory() as temporary:
            destination = Path(temporary) / 'missing/archive.txt'
            with self.assertRaisesRegex(ValueError, 'existing parent'):
                build_acceptance.build_fixture('arm64', destination)


if __name__ == '__main__':
    unittest.main()
