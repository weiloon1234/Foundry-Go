"""Upgrade harness boundaries without Docker or service mutations."""
import hashlib
import io
import json
from pathlib import Path
import subprocess
import tarfile
import tempfile
import unittest
from unittest.mock import patch

import acceptance_upgrade as upgrade


class UpgradeHarnessTests(unittest.TestCase):
    def test_invoke_keeps_published_and_candidate_entrypoints_separate(self):
        success = subprocess.CompletedProcess([], 0, '{"plan_token":"reviewed"}', '')
        with patch.object(upgrade, 'run', return_value=success) as run:
            self.assertEqual({'plan_token': 'reviewed'}, upgrade.invoke('tools-plan', {}, tools=Path('/candidate')))
            self.assertEqual('/candidate/server/manage.py', run.call_args.args[0][2])
            upgrade.invoke('cutover-plan', {'client': {'generation': 5}})
            self.assertEqual([upgrade.LAUNCHER, 'cutover-plan'], run.call_args.args[0])
            self.assertEqual(5, json.loads(run.call_args.kwargs['input'])['client']['generation'])
        refused = subprocess.CompletedProcess([], 1, '{"error":{"code":"host_upgrade_required"}}', '')
        with patch.object(upgrade, 'run', return_value=refused):
            upgrade.invoke('restart', {}, error='host_upgrade_required')
            with self.assertRaises(AssertionError):
                upgrade.invoke('restart', {})

    def test_readiness_variant_preserves_binary_and_rebinds_manifest_hash(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            source, destination = root / 'healthy.tar.gz', root / 'failed.tar.gz'
            binary, config = b'published binary', b'[app.fixture]\nreadiness = "healthy"\n'
            manifest = {'files': {'artisan': hashlib.sha256(binary).hexdigest(),
                                  'config/deploy.toml': hashlib.sha256(config).hexdigest()}}
            with tarfile.open(source, 'w:gz') as archive:
                for name, payload in {'artisan': binary, 'config/deploy.toml': config,
                                      'manifest.json': json.dumps(manifest).encode()}.items():
                    member = tarfile.TarInfo(name)
                    member.size = len(payload)
                    archive.addfile(member, io.BytesIO(payload))
            before = source.read_bytes()
            upgrade.failing_readiness(source, destination)
            self.assertEqual(before, source.read_bytes())
            with tarfile.open(destination) as archive:
                self.assertEqual(binary, archive.extractfile('artisan').read())
                configured = archive.extractfile('config/deploy.toml').read()
                self.assertIn(b'readiness = "fail"', configured)
                recorded = json.load(archive.extractfile('manifest.json'))
                self.assertEqual(hashlib.sha256(configured).hexdigest(), recorded['files']['config/deploy.toml'])
                self.assertEqual(manifest['files']['artisan'], recorded['files']['artisan'])
            with self.assertRaises(FileExistsError):
                upgrade.failing_readiness(source, destination)
