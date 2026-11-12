"""Upgrade harness boundaries without Docker or service mutations."""
import json
from pathlib import Path
import subprocess
import tempfile
from contextlib import ExitStack
import unittest
from unittest.mock import Mock, patch

import acceptance_upgrade as upgrade


class UpgradeHarnessTests(unittest.TestCase):
    def test_invoke_keeps_published_and_candidate_entrypoints_separate(self):
        success = subprocess.CompletedProcess([], 0, '{"plan_token":"reviewed"}', '')
        with patch.object(upgrade, 'run', return_value=success) as run:
            self.assertEqual({'plan_token': 'reviewed'}, upgrade.invoke('tools-plan', {}, tools=Path('/candidate')))
            self.assertEqual('/candidate/server/manage.py', run.call_args.args[0][2])
            upgrade.invoke('activate', {'client': {'generation': 1}})
            self.assertEqual([upgrade.LAUNCHER, 'activate'], run.call_args.args[0])
            self.assertEqual(1, json.loads(run.call_args.kwargs['input'])['client']['generation'])
        refused = subprocess.CompletedProcess([], 1, '{"error":{"code":"host_upgrade_required"}}', '')
        with patch.object(upgrade, 'run', return_value=refused):
            upgrade.invoke('restart', {}, error='host_upgrade_required')
            with self.assertRaises(AssertionError):
                upgrade.invoke('restart', {})

    def test_scenario_requires_replacement_or_noop_matching_bundle_digests(self):
        for changed in (False, True):
            with self.subTest(changed=changed), tempfile.TemporaryDirectory() as temporary, ExitStack() as stack:
                active = Path(temporary) / ('b' * 64)
                active.mkdir()
                old_tools = Path('/baseline')
                old_digest = 'a' * 64 if changed else active.name
                baseline = {'tools': {'generation': 9, 'bundle_sha256': old_digest,
                                      'active_path': str(active)},
                            'instances': {'instance': {'client_generation': 1}}}
                installed = {**baseline, 'tools': {**baseline['tools'], 'bundle_sha256': active.name}}
                host = Mock()
                host.read_registry.side_effect = [baseline, installed]
                host.paths.p.return_value = active
                host.require_identity.return_value = ({}, {})
                host.load_state.return_value = {'current': {'version': 'next'}}
                stack.enter_context(patch.object(upgrade, 'fixture', return_value=(host, {}, {'instance_id': 'instance'}, {})))
                for name in ('prove_running', 'check_report'):
                    stack.enter_context(patch.object(upgrade, name))
                stack.enter_context(patch.object(upgrade, 'protocol', return_value={'generation': 9}))
                stack.enter_context(patch.object(upgrade, 'tool_bundle', side_effect=lambda path:
                    (None, None, old_digest if path == old_tools else active.name)))
                stack.enter_context(patch.object(upgrade, 'request', return_value={}))
                stack.enter_context(patch.object(upgrade, 'instance_units', return_value={}))
                stack.enter_context(patch.object(upgrade, 'second_archive', return_value=(Path('/next'), 'next')))
                stack.enter_context(patch.object(upgrade, 'digest', return_value='archive-digest'))
                calls = []
                def invoke(action, payload, **kwargs):
                    calls.append(action)
                    if action == 'tools-plan':
                        if len(calls) == 1:
                            return {'plan_token': 'token', 'upgrade': ({'from_digest': old_digest,
                                'generation': 9, 'removed_paths': []} if changed else None),
                                'prune_bundles': [old_digest] if changed else [], 'prune_upgrade_journals': []}
                        return {'upgrade': None, 'prune_bundles': []}
                    if action == 'tools-apply':
                        return {'outcome': 'succeeded' if changed else 'unchanged',
                                'removed_bundles': [old_digest] if changed else [], 'removed_upgrade_journals': []}
                    if action == 'activate':
                        leftover = active.parent / ('f' * 64)
                        (leftover / 'prune-fixture.txt').unlink()
                        leftover.rmdir()
                        return {'removed_bundles': [leftover.name]}
                    return {}
                stack.enter_context(patch.object(upgrade, 'invoke', side_effect=invoke))
                upgrade.upgrade(Path('/archive'), old_tools)
                self.assertEqual(['tools-plan', 'tools-apply', 'tools-plan', 'activate', 'report'], calls)

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
