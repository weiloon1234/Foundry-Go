"""Maintenance policy transitions use live enablement, holds and shared state."""
from unittest.mock import patch
from types import SimpleNamespace
import uuid

import test_feature_wiring as wiring
import contracts as c
import feature_lifecycle as lifecycle
import feature_maintenance
import services


class MaintenanceTransitionTests(wiring.FeatureWiringFixture):
    def held(self, *, public=False):
        self.build_archive(independent=True, public=public)
        if public:
            wiring.FeatureWiringTests.configure_firewall(self)
        plan = self.request('bootstrap-plan', topology=self.topology.projection())
        self.request('bootstrap-apply', topology=self.topology.projection(), plan_token=plan['plan_token'], confirm=True)
        self.request('activate', archive=str(self.archive), archive_sha256=c.digest(self.archive))
        self.original = self.version
        self.request('down', confirm=True)
        _, entry = self.host.require_identity(self.project, self.profile)
        _, selected, inventory, _ = lifecycle.load(self.host, entry)
        self.assertEqual(selected.maintenance_holds, ('relay',))
        self.marker = self.host.paths.p(feature_maintenance.marker_path(inventory.specs['relay'].identity))
        self.assertTrue(self.marker.exists())
        return entry, inventory

    def rollback(self):
        operation = str(uuid.uuid4())
        plan = self.request('release-plan', target=self.original, expected_current=self.version, operation_id=operation)
        return self.request('rollback', target=self.original, expected_current=self.version, operation_id=operation,
                            plan_token=plan['plan_token'], confirm=True)

    def test_restore_drill_after_external_up_never_starts_or_exposes_held_roles(self):
        self.held(public=True)
        self.host.maintenance_down = False
        before = dict(self.host.running), list(self.host.started), dict(self.host.firewall_live)
        with patch.object(wiring.provision.backup, 'restore_drill', return_value={'status': 'complete'}) as drill:
            self.request('restore-drill', confirm=True, backup_id=str(uuid.uuid4()))
        drill.assert_called_once()
        self.assertEqual((self.host.running, self.host.started, self.host.firewall_live), before)
        self.assertTrue(self.marker.exists())

    def test_stop_after_external_up_does_not_start_held_roles_first(self):
        self.held(public=True)
        self.host.maintenance_down = False
        before = list(self.host.started)
        self.request('stop')
        self.assertEqual(self.host.started, before)
        self.assertFalse(self.host.running)

    def test_restore_drill_can_reconcile_external_down_without_starts(self):
        _, inventory = self.held(public=True)
        self.request('up', confirm=True)
        self.host.maintenance_down = True
        before = list(self.host.started)
        with patch.object(wiring.provision.backup, 'restore_drill', return_value={'status': 'complete'}):
            self.request('restore-drill', confirm=True, backup_id=str(uuid.uuid4()))
        self.assertEqual(self.host.started, before)
        self.assertTrue(self.marker.exists())
        self.assertNotIn(inventory.units['relay'], self.host.running)

    def test_no_start_down_executor_keeps_hold_after_continue_policy_change(self):
        self.held(public=True)
        self.build_archive(independent=True, public=True, maintenance='continue', version='20261115T000000Z-abcdef12')
        with patch.object(feature_maintenance, 'live', return_value=None):
            self.request('activate', archive=str(self.archive), archive_sha256=c.digest(self.archive))
        before = dict(self.host.running), list(self.host.started), dict(self.host.firewall_live)
        with patch.object(wiring.provision.backup, 'restore_drill', return_value={'status': 'complete'}):
            self.request('restore-drill', confirm=True, backup_id=str(uuid.uuid4()))
        # The pinned executor must retain the restriction as well as the initial
        # observation: even down can otherwise release a changed-policy hold.
        lifecycle.request_operation(self.host, 'down', self.project, self.profile,
                                    maintenance_reconcile=True, maintenance_no_start=True)
        self.assertEqual((self.host.running, self.host.started, self.host.firewall_live), before)
        self.assertTrue(self.marker.exists())

    def test_public_held_role_allows_deploy_and_rollback_review(self):
        self.held(public=True)
        self.build_archive(independent=True, public=True, version='20261115T000000Z-abcdef12')
        self.request('activate', archive=str(self.archive), archive_sha256=c.digest(self.archive))
        self.rollback()
        _, entry = self.host.require_identity(self.project, self.profile)
        _, selected, inventory, _ = lifecycle.load(self.host, entry)
        self.assertEqual(selected.maintenance_holds, ('relay',))
        self.assertIn(inventory.units['relay'], self.host.enabled)
        self.assertNotIn(inventory.units['relay'], self.host.running)
        self.assertTrue(self.marker.exists())

    def test_continue_policy_restores_enabled_held_copy_and_proves_readiness(self):
        self.held()
        self.build_archive(independent=True, maintenance='continue', version='20261115T000000Z-abcdef12')
        with patch.object(services, 'wait_ready_set', wraps=services.wait_ready_set) as ready:
            self.request('activate', archive=str(self.archive), archive_sha256=c.digest(self.archive))
        _, entry = self.host.require_identity(self.project, self.profile)
        _, selected, inventory, _ = lifecycle.load(self.host, entry)
        self.assertFalse(selected.maintenance_holds)
        self.assertFalse(self.marker.exists())
        self.assertIn(inventory.units['relay'], self.host.running)
        self.assertTrue(any('relay' in call.args[2] for call in ready.call_args_list))
        self.request('up', confirm=True)
        self.assertIn(inventory.units['relay'], self.host.running)

    def test_failed_policy_release_restores_hold_before_cleanup_completes(self):
        self.held()
        self.build_archive(independent=True, maintenance='continue', version='20261115T000000Z-abcdef12')
        with patch.object(services, 'wait_ready_set', return_value=False), self.assertRaises(c.DeployError):
            self.request('activate', archive=str(self.archive), archive_sha256=c.digest(self.archive))
        _, entry = self.host.require_identity(self.project, self.profile)
        _, selected, inventory, _ = lifecycle.load(self.host, entry)
        self.assertIn('relay', selected.maintenance_holds)
        self.assertIn('relay', feature_maintenance.record(self.host, entry)['holds'])
        self.assertTrue(self.marker.exists())
        self.assertNotIn(inventory.units['relay'], self.host.running)

    def test_policy_release_honors_disable(self):
        _, inventory = self.held()
        self.host.run(['systemctl', 'disable', inventory.units['relay']])
        self.build_archive(independent=True, maintenance='continue', version='20261115T000000Z-abcdef12')
        self.request('activate', archive=str(self.archive), archive_sha256=c.digest(self.archive))
        self.request('up', confirm=True)
        self.assertNotIn(inventory.units['relay'], self.host.running)
        self.assertNotIn(inventory.units['relay'], self.host.enabled)

    def test_unavailable_store_preserves_hold_across_reviewed_policy_change(self):
        self.held()
        self.build_archive(independent=True, maintenance='continue', version='20261115T000000Z-abcdef12')
        with patch.object(feature_maintenance, 'live', return_value=None):
            self.request('activate', archive=str(self.archive), archive_sha256=c.digest(self.archive))
            _, entry = self.host.require_identity(self.project, self.profile)
            _, selected, inventory, _ = lifecycle.load(self.host, entry)
            self.assertEqual(selected.maintenance_holds, ('relay',))
            self.assertEqual(feature_maintenance.record(self.host, entry)['holds'], ['relay'])
            self.assertTrue(self.marker.exists())
            self.assertNotIn(inventory.units['relay'], self.host.running)
        # A later observation reconciles the changed policy and restores the copy.
        self.request('status')
        self.assertIn(inventory.units['relay'], self.host.running)
        self.assertFalse(self.marker.exists())

    def test_retired_copy_hold_does_not_repeatedly_change_shared_maintenance(self):
        entry, inventory = self.held()
        store, selected, _, launch = lifecycle.load(self.host, entry)
        current = SimpleNamespace(topology=inventory.topology, resolution=inventory.resolution,
                                  roles={'http': inventory.roles['http']})
        with patch.object(lifecycle, 'load', return_value=(store, selected, current, launch)), \
                patch.object(lifecycle, 'request_operation') as request:
            feature_maintenance.reconcile(self.host, self.project, self.profile)
        request.assert_not_called()
        self.assertTrue(self.marker.exists())
