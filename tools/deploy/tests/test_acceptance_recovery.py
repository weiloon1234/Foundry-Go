"""Hostless checks for recovery artifact construction and launcher responses."""
import hashlib
import io
import json
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import Mock, patch
import tarfile
from test_contracts import fixture

import acceptance_recovery as recovery


class RecoveryArtifactTests(unittest.TestCase):
    def test_incident_proves_stop_refusal_before_cutover_and_stop_success_after_forward(self):
        suite = Mock()
        suite.project, suite.profile, suite.entry = {}, {}, {}
        suite.host.read_registry.side_effect = [
            {'tools': {'bundle_sha256': 'old'}}, {'tools': {'bundle_sha256': 'current'}}]
        suite.state.return_value = {'queue_compatibility': {'floor': 3},
                                    'cutover': {'operation_id': 'failed-operation'}}
        suite.wait_operation.return_value = {
            'queue_compatibility': {'permission': 'closed'},
            'cutover': {'error': 'cutover_stop_unproven', 'snapshot_started': False}}
        suite.foreign.return_value = {'unrelated': 'unchanged'}
        suite.candidate.return_value = (Path('/candidate.tar.gz'), 'candidate')
        suite.apply.side_effect = ['failed-operation', 'successor']
        suite.healthy.return_value = {'cutover': {'supersedes_operation': 'failed-operation'}}
        suite.call.return_value = {'contained': True, 'status': 'stopped'}
        events = Mock()
        events.attach_mock(suite.call, 'call')
        events.attach_mock(suite.plan, 'plan')
        events.attach_mock(suite.healthy, 'healthy')
        with patch.object(recovery, 'invoke', return_value={'upgrade': {}, 'plan_token': 'upgrade'}) as invoke, \
                patch.object(recovery, 'tool_bundle', side_effect=[(None, None, 'old'), (None, None, 'current')]), \
                patch.object(recovery, 'digest', return_value='checksum'), \
                patch.object(recovery, 'prove_running'), \
                patch.object(recovery, 'acknowledge_incident_executor', return_value={'unit': 'owned'}) as acknowledge, \
                patch.object(recovery.contract, 'private_path'), \
                patch.object(recovery, 'read_json', return_value={'result': True}):
            events.attach_mock(invoke, 'invoke')
            recovery.Recovery.incident(suite, Path('/old-tools'))
        actions = [(call[0], call.args[0]) for call in events.mock_calls]
        self.assertEqual([
            ('invoke', 'stop'), ('call', 'restart'), ('plan', suite.candidate.return_value),
            ('invoke', 'tools-plan'), ('invoke', 'tools-apply'), ('invoke', 'cutover-abort-plan'),
            ('plan', suite.candidate.return_value), ('healthy', 'successor'),
            ('call', 'stop'), ('call', 'restart'),
        ], actions)
        self.assertEqual('cutover_stop_unproven', invoke.call_args_list[0].kwargs['error'])
        self.assertEqual('cutover_abort_unsafe', invoke.call_args_list[-1].kwargs['error'])
        suite.wait_operation.assert_called_once_with('failed-operation', 'needs_reconciliation', timeout=270)
        self.assertEqual('stopped', suite.record.call_args.kwargs['upgraded_stop'])
        acknowledge.assert_called_once_with(suite.host, suite.entry, 'failed-operation', 'successor')

    def test_acknowledges_only_exited_superseded_executor_after_successful_recovery(self):
        _, _, entry = fixture()
        operation, successor = '40000000-0000-4000-8000-000000000001', '40000000-0000-4000-8000-000000000002'
        unit = recovery.release.cutover_unit(entry, operation)
        archived = {'result': {'outcome': 'superseded', 'replacement_operation': successor},
                    'superseded_progress': {'error': 'cutover_stop_unproven'}}
        host = Mock()
        progress = {'operation_id': successor, 'outcome': 'succeeded', 'supersedes_operation': operation}
        host.load_state.return_value = {'cutover': progress}
        show = subprocess.CompletedProcess([], 0, 'ActiveState=failed\nResult=exit-code\nExecMainStatus=1\n')
        host.run.side_effect = [show, subprocess.CompletedProcess([], 0, '')]
        with patch.object(recovery, 'read_json', return_value=archived), \
                patch.object(recovery.contract, 'private_path'), \
                patch.object(recovery.release, 'cutover_executor', side_effect=['exited', 'absent']):
            evidence = recovery.acknowledge_incident_executor(host, entry, operation, successor)
        self.assertEqual({'unit': unit, 'ActiveState': 'failed', 'Result': 'exit-code', 'ExecMainStatus': '1'}, evidence)
        self.assertEqual(['systemctl', 'reset-failed', unit], host.run.call_args_list[-1].args[0])
        self.assertEqual(2, host.run.call_count)
        host.run.reset_mock()
        # Neither an unfinished successor nor a mismatched failure may be acknowledged.
        with patch.object(recovery, 'read_json', return_value=archived), \
                patch.object(recovery.contract, 'private_path'), \
                patch.object(recovery.release, 'cutover_executor', return_value='exited'), \
                patch.object(recovery, 'unit_failure_details', return_value={
                    'ActiveState': 'failed', 'Result': 'signal', 'ExecMainStatus': '9'}):
            for outcome in ('running', 'succeeded'):
                progress['outcome'] = outcome
                with self.assertRaises(AssertionError):
                    recovery.acknowledge_incident_executor(host, entry, operation, successor)
                host.run.assert_not_called()

    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)

    def archive(self, config):
        path = self.root / 'source.tar.gz'
        payloads = {'config/deploy.toml': config, 'artisan': b'real-binary-placeholder-for-archive-only-test',
                    'public/index.html': b'<p>fixture</p>'}
        manifest = {'version': 'retained-version', 'files': {
            name: hashlib.sha256(data).hexdigest() for name, data in payloads.items()}}
        payloads['manifest.json'] = json.dumps(manifest).encode()
        with tarfile.open(path, 'x:gz') as archive:
            for name, data in payloads.items():
                member = tarfile.TarInfo(name)
                member.size = len(data)
                member.mode = 0o755 if name == 'artisan' else 0o644
                archive.addfile(member, io.BytesIO(data))
        return path

    def test_variants_preserve_binary_and_source_and_rehash_configuration(self):
        source = self.archive(b'[fixture]\nreadiness = "healthy"\n')
        original = source.read_bytes()
        for mode in ('fail', 'hold'):
            output = self.root / (mode + '.tar.gz')
            recovery.rewrite_readiness(source, output, mode)
            with tarfile.open(output, 'r:gz') as archive:
                manifest = json.load(archive.extractfile('manifest.json'))
                for name, expected in manifest['files'].items():
                    self.assertEqual(expected, hashlib.sha256(archive.extractfile(name).read()).hexdigest())
                self.assertIn(('readiness = "' + mode + '"').encode(), archive.extractfile('config/deploy.toml').read())
                self.assertEqual(0o755, archive.getmember('artisan').mode)
                self.assertEqual(b'<p>fixture</p>', archive.extractfile('public/index.html').read())
            self.assertEqual(original, source.read_bytes())

    def test_missing_or_ambiguous_fixture_declaration_refuses_before_publication(self):
        for config in (b'[app]\n', b'readiness = "healthy"\nreadiness = "hold"\n'):
            source = self.archive(config)
            output = self.root / 'invalid.tar.gz'
            with self.assertRaises(ValueError):
                recovery.rewrite_readiness(source, output, 'fail')
            self.assertFalse(output.exists())
            source.unlink()

    def test_plans_need_not_have_ok_field_and_refusals_need_exact_code(self):
        with patch.object(recovery, 'run', return_value=subprocess.CompletedProcess([], 0, '{"plan_token":"token"}', '')):
            self.assertEqual({'plan_token': 'token'}, recovery.invoke('cutover-plan', {}))
        refused = subprocess.CompletedProcess([], 1, '{"error":{"code":"cutover_abort_unsafe"}}', '')
        with patch.object(recovery, 'run', return_value=refused):
            recovery.invoke('cutover-abort-plan', {}, error='cutover_abort_unsafe')
            with self.assertRaises(AssertionError):
                recovery.invoke('cutover-abort-plan', {}, error='command_failed')

    def test_hash_comparison_ignores_redis_field_order_but_rejects_duplicate_fields(self):
        self.assertEqual(recovery.hash_fields(['a', '1', 'b', '2']), recovery.hash_fields(['b', '2', 'a', '1']))
        for value in (['a'], ['a', '1', 'a', '2'], [1, 'one']):
            with self.assertRaises(AssertionError):
                recovery.hash_fields(value)
