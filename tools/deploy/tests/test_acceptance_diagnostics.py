"""Failure logs expose actionable state without Redis key or client data."""
import contextlib
import io
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import Mock, patch

import acceptance_diagnostics as diagnostics


class ActivationDiagnosticsTests(unittest.TestCase):
    def test_artisan_failure_retries_only_config_check_and_redacts_stderr(self):
        import release
        from contracts import DeployError
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory).resolve()
            (root / 'app.env').touch()
            for command in ('config:check', 'migrate'):
                with self.subTest(command=command):
                    host = Mock()
                    host.paths.instance_config.return_value = root
                    host.paths.data.return_value = root
                    host.run.side_effect = [DeployError('command_failed', 'withheld'),
                        subprocess.CompletedProcess([], 1, 'secret stdout',
                            'invalid: schedule declarations require scheduler to be enabled\nsecret token\n')]
                    output = io.StringIO()
                    try:
                        release.app(host, {'name': 'abc-acceptance'}, root, command)
                    except DeployError as error:
                        with contextlib.redirect_stdout(output):
                            diagnostics.artisan_failure(error)
                    value = json.loads(output.getvalue().split(': ', 1)[1])
                    self.assertEqual(command, value['command'])
                    self.assertEqual(command == 'config:check', value['reinvoked'])
                    self.assertEqual(2 if command == 'config:check' else 1, host.run.call_count)
                    self.assertNotIn('secret', output.getvalue())
                    if command == 'config:check':
                        self.assertIn('schedule declarations require scheduler to be enabled', value['stderr']['lines'])
                        self.assertEqual(10, host.run.call_args.kwargs['timeout'])
                        self.assertFalse(host.run.call_args.kwargs['check'])
        sample = diagnostics.artisan_stderr(('private value\n' * 1000))
        self.assertTrue(sample['truncated'])
        self.assertLessEqual(len(sample['lines']), 8)
        self.assertNotIn('private', json.dumps(sample))

    def test_phase_wrapper_preserves_arguments_and_exit_status(self):
        with tempfile.TemporaryDirectory() as directory:
            script = Path(directory) / 'phase.py'
            script.write_text('import sys\nprint(sys.argv[1])\nraise SystemExit(19)\n')
            result = subprocess.run([os.sys.executable, '-B', str(Path(__file__).with_name('acceptance_phase.py')),
                                     str(script), 'retained-argument'], capture_output=True, text=True, timeout=5)
            self.assertEqual(19, result.returncode)
            self.assertEqual('retained-argument\n', result.stdout)

    def test_unexpected_failed_units_include_result_and_exit_status(self):
        from acceptance_host import check_failed_units
        host = Mock()
        host.run.return_value.stdout = 'ActiveState=failed\nResult=exit-code\nExecMainStatus=1\nPrivate=secret\n'
        check_failed_units(host, ['systemd-modules-load.service', 'getty@tty1.service'])
        host.run.assert_not_called()
        with self.assertRaises(AssertionError) as caught:
            check_failed_units(host, ['owned-executor.service', 'getty@tty1.service'])
        details = json.loads(str(caught.exception).split(': ', 1)[1])
        self.assertEqual({'owned-executor.service': {
            'ActiveState': 'failed', 'Result': 'exit-code', 'ExecMainStatus': '1'}}, details)
        self.assertNotIn('secret', str(caught.exception))
        self.assertEqual('owned-executor.service', host.run.call_args.args[0][2])
        host.run.side_effect = RuntimeError('secret command output')
        with self.assertRaisesRegex(AssertionError, 'owned-executor.service.*unavailable'):
            check_failed_units(host, ['owned-executor.service'])

    def test_readiness_sample_keeps_probe_shape_and_redacts_other_content(self):
        connection = Mock()
        response = connection.getresponse.return_value
        response.status = 200
        response.read.return_value = b'{"status":"ready"}\n'
        with patch.object(diagnostics.http.client, 'HTTPConnection', return_value=connection):
            sample = diagnostics.readiness_sample({'allocations': {'http_port': 18000}})
        self.assertEqual({'status': 200, 'body': {'status': 'ready'}, 'sample_bytes': 19}, sample)
        connection.request.assert_called_once_with('GET', '/health/ready', headers={'Connection': 'close'})
        response.read.assert_called_once_with(4097)
        connection.close.assert_called_once()
        self.assertEqual({'ready': True}, diagnostics.probe_body(b'{"ready":true}'))
        for payload in (b'<html>secret</html>', b'{"status":"secret","token":"secret"}', b'secret' * 1000):
            self.assertNotIn('secret', str(diagnostics.probe_body(payload)))

    def test_journal_retains_recent_lifecycle_and_exit_evidence_without_requests_or_secrets(self):
        records = [{'__REALTIME_TIMESTAMP': '123', 'MESSAGE': json.dumps({
            'msg': 'application ready', 'level': 'INFO', 'token': 'secret'})} for _ in range(35)]
        records += [{'MESSAGE': json.dumps({'msg': 'HTTP request completed', 'path': 'secret'})},
                    {'MESSAGE': json.dumps({'msg': 'private secret failure', 'diagnostic': {'secret': 'token'}})},
                    {'MESSAGE': 'unit: Main process exited, code=exited, status=1/FAILURE'}]
        value = diagnostics.journal_summary('\n'.join(json.dumps(item) for item in records))
        self.assertEqual(10, len(value))
        self.assertEqual('application ready', value[0]['message'])
        self.assertEqual('process exited: code=exited, status=1', value[-1]['message'])
        self.assertNotIn('secret', json.dumps(value))
        self.assertNotIn('HTTP request', json.dumps(value))
        lifecycle = diagnostics.journal_summary('\n'.join(json.dumps({'MESSAGE': message}) for message in (
            'Started secret description.', 'owned.service: Deactivated successfully.',
            json.dumps({'msg': ['secret']}))))
        self.assertEqual(['unit started', 'unit deactivated successfully', '<redacted message>'],
                         [event['message'] for event in lifecycle])
        host = Mock()
        host.run.return_value.stdout = ''
        with patch.object(diagnostics, 'instance_units', return_value={'http': 'owned.service'}):
            self.assertEqual({'http': []}, diagnostics.role_journals(host, {}))
        args = host.run.call_args.args[0]
        self.assertIn('--lines=10', args)
        self.assertTrue(any(arg.startswith('--grep=') for arg in args))
        self.assertEqual(5, host.run.call_args.kwargs['timeout'])

    def test_runner_logs_diagnostics_and_preserves_activation_exit_status(self):
        runner = Path(__file__).with_name('run_acceptance.sh').read_text()
        functions = runner[runner.index('phase_on() {'):runner.index('# Refresh before foreign')]
        docker = '''
docker() {
  case "$*" in
    *acceptance_diagnostics.py*) echo 'redacted ACL summary'; return "$DIAGNOSTIC_STATUS" ;;
    *) echo 'activation failed'; return 37 ;;
  esac
}
'''
        for diagnostic_status in (0, 11):
            with self.subTest(diagnostic_status=diagnostic_status), tempfile.TemporaryDirectory() as directory:
                result = subprocess.run(['bash', '-c', 'set -e\n' + docker + functions + '\nphase_on host activation driver.py'],
                                        env={**os.environ, 'logs': directory, 'env_file': 'unused',
                                             'DIAGNOSTIC_STATUS': str(diagnostic_status)},
                                        capture_output=True, text=True, timeout=5)
                self.assertEqual(37, result.returncode)
                log = (Path(directory) / 'activation.log').read_text()
                self.assertIn('activation failed', log)
                self.assertIn('redacted ACL summary', log)
                self.assertEqual(diagnostic_status != 0, 'collector unavailable' in log)

    def test_instance_state_and_acl_counts_are_redacted_and_scoped(self):
        entry = {'name': 'abc-acceptance', 'allocations': {'redis_user': 'instance-user'}}
        host = Mock()
        host.read_registry.return_value = {'instances': {'id': entry}}
        host.load_state.return_value = {'last_attempt': {
            'action': 'deploy', 'phase': 'validation', 'error': 'invalid_json', 'outcome': 'failed',
            'private': 'secret-state'}}
        rows = [
            {'username': 'instance-user', 'reason': 'key', 'context': 'lua', 'count': 2,
             'object': 'secret-key', 'client-info': 'secret-client'},
            {'username': 'instance-user', 'reason': 'key', 'context': 'lua', 'count': 1},
            {'username': 'foreign-user', 'reason': 'auth', 'context': 'toplevel', 'count': 9},
            {'username': 'instance-user', 'reason': 'secret-reason', 'context': 'secret-context', 'count': 1},
        ]
        output = io.StringIO()
        with patch.object(diagnostics.redis_acl, '_redis', return_value=rows) as redis, contextlib.redirect_stdout(output):
            diagnostics.emit_diagnostics(host)
        redis.assert_called_once_with(host, 'ACL', 'LOG', '128')
        text = output.getvalue()
        self.assertNotIn('secret', text)
        self.assertNotIn('foreign-user', text)
        self.assertNotIn('instance-user', text)
        value = json.loads(text.split(': ', 1)[1])
        self.assertEqual({'action': 'deploy', 'phase': 'validation', 'error': 'invalid_json', 'outcome': 'failed'}, value['last_attempt'])
        self.assertEqual([{'reason': 'key', 'context': 'lua', 'count': 3},
                          {'reason': 'other', 'context': 'other', 'count': 1}], value['recent_acl_log'])

    def test_unavailable_diagnostics_never_expose_exception_details(self):
        host = Mock()
        host.read_registry.return_value = {'instances': {'id': {
            'name': 'abc-acceptance', 'allocations': {'redis_user': 'instance-user'}}}}
        host.load_state.side_effect = RuntimeError('secret-state')
        output = io.StringIO()
        with patch.object(diagnostics.redis_acl, '_redis', side_effect=RuntimeError('secret-redis')), contextlib.redirect_stdout(output):
            diagnostics.emit_diagnostics(host)
        self.assertNotIn('secret', output.getvalue())
        self.assertIn('"last_attempt": "unavailable"', output.getvalue())
        self.assertIn('"recent_acl_log": "unavailable"', output.getvalue())
        self.assertEqual('<redacted>', diagnostics.safe_label('private value\n'))
