"""Upgrade harness boundaries without Docker or service mutations."""
import json
from pathlib import Path
import subprocess
import unittest
from unittest.mock import Mock, patch

import acceptance_upgrade as upgrade


class UpgradeHarnessTests(unittest.TestCase):
    def test_invoke_keeps_published_and_candidate_entrypoints_separate(self):
        success = subprocess.CompletedProcess([], 0, '{"plan_token":"reviewed"}', '')
        with patch.object(upgrade, 'run', return_value=success) as run:
            self.assertEqual({'plan_token': 'reviewed'}, upgrade.invoke('tools-plan', {}, tools=Path('/candidate')))
            self.assertEqual('/candidate/server/manage.py', run.call_args.args[0][2])
            upgrade.invoke('activate', {'client': {'generation': 6}})
            self.assertEqual([upgrade.LAUNCHER, 'activate'], run.call_args.args[0])
            self.assertEqual(6, json.loads(run.call_args.kwargs['input'])['client']['generation'])
        refused = subprocess.CompletedProcess([], 1, '{"error":{"code":"host_upgrade_required"}}', '')
        with patch.object(upgrade, 'run', return_value=refused):
            upgrade.invoke('restart', {}, error='host_upgrade_required')
            with self.assertRaises(AssertionError):
                upgrade.invoke('restart', {})

    def test_report_accepts_only_the_host_phases_container_failures(self):
        host = Mock()
        for failed in ([], ['systemd-modules-load.service'], ['getty@tty1.service']):
            report = {'host': {'failed_units': failed},
                      'problems': [{'code': 'units_failed'}] if failed else []}
            upgrade.check_report(host, report)
        with patch('acceptance_diagnostics.unit_failure_details', return_value={}):
            with self.assertRaises(AssertionError):
                upgrade.check_report(host, {'host': {'failed_units': ['foundry-http@fixture.service']},
                                            'problems': [{'code': 'units_failed'}]})
        with self.assertRaises(AssertionError):
            upgrade.check_report(host, {'host': {'failed_units': []}, 'problems': [{'code': 'backup_stale'}]})
