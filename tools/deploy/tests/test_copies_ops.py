"""Operations over expanded inventories without lifting the profile count gate."""
from dataclasses import replace
import os
from pathlib import Path
import sys
import tempfile
import time
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "lib"))
import admission
import backup
import contracts as c
import copy_enablement
import feature_lifecycle
import feature_maintenance
import feature_recovery
from host import Host, Paths
import journal
import listeners
import operation_selection
import recover
import report
import services
import topology
from test_host_capabilities import INSTANCE, selection
from test_services import inventory_fixture


def expanded_inventory(count=2):
    """Use expansion and immutable descriptors directly while profiles remain gated."""
    inventory, entry, _ = inventory_fixture()
    original = dict(inventory.specs)
    inventory.profile = replace(inventory.profile, placement=(("http", 1), ("relay", count), ("worker", 1)))
    expanded = topology.expand_placement(inventory.topology, INSTANCE, inventory.profile.placement)
    inventory.units = services.expanded_unit_names(entry["name"], inventory.topology, inventory.profile)
    inventory.specs = {key: replace(original[c.UnitIdentity(INSTANCE, key.role, 1)], identity=key) for key in expanded}
    inventory.deployment = replace(inventory.deployment, units=tuple(inventory.specs.values()),
                                   profile_sha256=c.canonical_digest(inventory.profile.projection()))
    endpoints = listeners.allocate_private(INSTANCE, listeners.private_listeners(inventory.topology, inventory.profile), (), (), first=18000, last=18150)
    inventory.resolution = replace(inventory.resolution, endpoints=endpoints,
                                   deployment_sha256=c.canonical_digest(c.record_document(inventory.deployment)))
    entry.update(profile=inventory.profile.projection(), topology=inventory.topology.projection())
    return inventory, entry


class CopyOperationsTests(unittest.TestCase):
    def setUp(self):
        self.inventory, self.entry = expanded_inventory()
        self.http = self.inventory.identity("http")
        self.first, self.second = self.inventory.copies("relay")
        self.worker = self.inventory.identity("worker")
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.host = Host(Paths(Path(self.temp.name)))

    def test_copy_selection_round_trip_and_legacy_bytes(self):
        keys = (self.http, self.first, self.second, self.worker)
        self.assertEqual(c.unit_selection_names(keys), ("http", "relay", "relay@2", "worker"))
        self.assertEqual(c.unit_selection_identities(INSTANCE, c.unit_selection_names(keys)), keys)
        legacy = selection()
        self.assertEqual(c.record_document(c.DeploymentSelection.parse(legacy)), legacy)
        selected = replace(c.DeploymentSelection.parse(selection()), enabled_roles=c.unit_selection_names(keys), maintenance_holds=("relay@2",))
        self.assertEqual(c.DeploymentSelection.parse(c.record_document(selected)), selected)
        for invalid in (["relay@1"], ["relay@02"], ["relay@129"], ["relay@0"], ["relay@2.x"], ["relay", "relay"], [True]):
            with self.subTest(invalid=invalid), self.assertRaises(c.DeployError):
                c.unit_selection_identities(INSTANCE, invalid)
        with self.assertRaises(c.DeployError):
            c.unit_selection_names((self.first, c.UnitIdentity("10000000-0000-4000-8000-000000000001", "relay", 2)))

    def test_selection_supports_all_256_units(self):
        keys = tuple(c.UnitIdentity(INSTANCE, role, ordinal) for role in ("one", "two") for ordinal in range(1, 129))
        self.assertEqual(c.unit_selection_identities(INSTANCE, c.unit_selection_names(keys)), keys)

    def test_public_selector_validation_and_journal_scope(self):
        for action, role, ordinal in (("status", "relay", 1), ("logs", None, 2), ("logs", "all", 2),
                                      ("stop", "relay", True), ("restart", "relay", 0), ("logs", "relay", "2")):
            with self.subTest(action=action, role=role, ordinal=ordinal), self.assertRaises(c.DeployError):
                operation_selection.validate(action, role, ordinal)
        with patch.object(operation_selection, "selected_units", return_value={key: self.inventory.units[key] for key in self.inventory.copies("relay")}):
            one = journal.arguments(self.entry, "relay", 20, ordinal=2)
            self.assertIn(self.inventory.units[self.second], one)
            self.assertNotIn(self.inventory.units[self.first], one)
            all_copies = journal.arguments(self.entry, "relay", 20, follow=True)
            self.assertIn(self.inventory.units[self.first], all_copies)
            self.assertIn(self.inventory.units[self.second], all_copies)
            self.assertIn("--follow", all_copies)
            with self.assertRaises(c.DeployError):
                journal.arguments(self.entry, "relay", 20, ordinal=3)

    def test_cli_rejects_copy_before_opening_ssh(self):
        from test_local import cli
        for action, env in (("status", {"COPY": "1", "ROLE": "relay"}),
                            ("logs", {"COPY": "2"}), ("stop", {"COPY": "-1", "ROLE": "relay"}),
                            ("restart", {"COPY": "129", "ROLE": "relay"})):
            with self.subTest(action=action, env=env), patch.dict(os.environ, env, clear=True), self.assertRaises(cli.LocalError):
                cli.copy_parameter(action)
        with patch.dict(os.environ, {"COPY": "2", "ROLE": "relay"}, clear=True):
            self.assertEqual(cli.copy_parameter("logs"), 2)

    def test_cli_other_commands_reject_copy_before_their_effects(self):
        from test_local import cli
        for command in ("bootstrap", "release", "rehearse"):
            with self.subTest(command=command), patch.dict(os.environ, {}, clear=True), \
                 patch.object(sys, "argv", ["cli", "--app-root", self.temp.name, "--role", "relay", "--copy", "2", command]), \
                 patch.object(cli, "ROOT", cli.ROOT), patch.object(cli, "resolve_app_root", return_value=Path(self.temp.name)), \
                 patch.object(cli, "local_settings") as settings, self.assertRaises(cli.LocalError):
                cli.main()
            settings.assert_not_called()


    def test_manage_rejects_copy_for_every_other_action_before_host_reads(self):
        from test_manage import manager
        from unittest.mock import Mock
        host = Mock()
        for action in set(manager.ACTIONS) - {"stop", "restart", "logs"}:
            with self.subTest(action=action), self.assertRaises(c.DeployError) as error:
                manager.execute(host, action, {"role": "relay", "copy": 1})
            self.assertEqual(error.exception.code, "invalid_copy")
        self.assertEqual(host.mock_calls, [])


    def test_manage_forwards_copy_to_lifecycle_owner(self):
        from test_manage import manager
        from test_contracts import fixture
        project, profile, entry = fixture()
        with patch.object(self.host, "read_registry", return_value={}), \
             patch.object(self.host, "require_identity", return_value=({}, entry)), \
             patch("upgrades.check_client"), patch("upgrades.check_target_capabilities"), \
             patch.object(operation_selection, "units", return_value={self.second: self.inventory.units[self.second]}), \
             patch.object(feature_lifecycle, "is_feature", return_value=True), \
             patch.object(feature_lifecycle, "request_operation", return_value={"ok": True, "status": "degraded"}) as operation:
            result = manager.execute(self.host, "stop", {"project": project, "profile": profile, "role": "relay", "copy": 2})
        self.assertEqual(result["status"], "degraded")
        self.assertEqual(operation.call_args.kwargs["copy"], 2)
        self.assertEqual(operation.call_args.kwargs["role"], "relay")

    def test_rehearsal_expands_every_safe_dependency_copy(self):
        raw = self.inventory.topology.projection()
        for role in raw["roles"]:
            if role["name"] == "http":
                role["requires"] = ["relay"]
            elif role["name"] == "relay":
                role["requires"] = []
                role["recovery_rehearsal_safe"] = True
        self.inventory.topology = topology.parse_topology(raw)
        self.inventory.roles = {role.common.name: role for role in self.inventory.topology.roles}
        selected = recover.rehearsal_roles(self.inventory)
        self.assertEqual(set(selected), {self.first, self.second, self.http})
        self.assertLess(selected.index(self.second), selected.index(self.http))


    def test_hold_markers_are_independent_and_up_honors_deliberate_disable(self):
        feature_maintenance.publish_markers(self.host, self.inventory, {self.first, self.second})
        paths = [self.host.paths.p(feature_maintenance.marker_path(key)) for key in (self.first, self.second)]
        self.assertNotEqual(*paths)
        self.assertTrue(all(path.exists() for path in paths))
        saved = {"holds": ["relay", "relay@2"], "enabled": ["http", "relay", "relay@2"], "running": ["http", "relay", "relay@2"]}
        with patch.object(feature_maintenance, "record", return_value=saved), patch.object(feature_maintenance, "live", return_value={"down": True}):
            holds, running, intent = feature_maintenance.selection(self.host, self.entry, self.inventory,
                    {self.http, self.first}, {self.http}, "up")
        self.assertEqual(holds, set())
        self.assertEqual(running, {self.http, self.first})
        self.assertNotIn("relay@2", intent["enabled"])
        feature_maintenance.clear_markers(self.host, self.inventory, {self.second})
        self.assertFalse(paths[0].exists())
        self.assertTrue(paths[1].exists())

    def test_unknown_maintenance_store_preserves_each_marker(self):
        feature_maintenance.publish_markers(self.host, self.inventory, {self.second})
        with patch.object(feature_maintenance, "record", return_value=None), patch.object(feature_maintenance, "live", return_value=None):
            holds, running, intent = feature_maintenance.selection(self.host, self.entry, self.inventory,
                        {self.http, self.first, self.second}, {self.http, self.first}, None)
        self.assertIn(self.second, holds)
        self.assertNotIn(self.second, running)
        self.assertIn("relay@2", intent["holds"])

    def test_removed_enablement_retains_false_and_missing_never_enables(self):
        previous, _ = expanded_inventory(1)
        self.entry["deployment_selection"] = selection()
        for recorded in (None, False, True):
            if recorded is not None:
                copy_enablement.record_removed_copy_enablement(self.host, self.entry, self.second, recorded)
            with patch.object(feature_lifecycle, "load", return_value=(None, None, previous, None)), \
                 patch.object(services, "enabled_units", return_value=(self.http, self.first)), \
                 patch.object(feature_lifecycle, "configuration_hold_running", return_value={self.http, self.first}):
                enabled, running, removed = feature_lifecycle.transition_enablement(self.host, self.entry, self.inventory, restoring=True)
            self.assertEqual(self.second in enabled, recorded is True)
            self.assertEqual(self.second in running, recorded is True)
            plan = copy_enablement.restoration_plan(self.host, self.entry, previous, self.inventory)
            self.assertEqual(plan, [{"role": "relay", "ordinal": 2, "enabled": recorded is True,
                                     "evidence": "missing" if recorded is None else "recorded"}])
        copy_enablement.record_removed_copy_enablement(self.host, self.entry, self.worker, False)
        self.assertEqual(copy_enablement.removed_enablement(self.host, self.entry), {self.second: True, self.worker: False})
        with self.assertRaises(c.DeployError):
            copy_enablement.record_removed_copy_enablement(self.host, self.entry, self.second, 1)

    def test_removed_history_can_outlive_one_placement_unit_limit(self):
        retired = {c.UnitIdentity(INSTANCE, f"retired{number}", 1): number % 2 == 0 for number in range(c.MAX_UNITS + 1)}
        for identity, enabled in retired.items():
            copy_enablement.record_removed_copy_enablement(self.host, self.entry, identity, enabled)
        copy_enablement.record_removed_copy_enablement(self.host, self.entry, self.second, False)
        self.assertEqual(copy_enablement.removed_enablement(self.host, self.entry), {**retired, self.second: False})


    def test_backup_coverage_is_per_copy_and_singleton_shape_is_unchanged(self):
        singleton, _, _ = inventory_fixture()
        expected = {"role": "relay", "kind": singleton.roles["relay"].state.kind}
        if expected["kind"] == "external":
            expected["recovery_owner"] = singleton.roles["relay"].state.recovery_owner
        self.assertEqual(backup.service_coverage(singleton)["service_state"], [expected])
        states = backup.service_coverage(self.inventory)["service_state"]
        self.assertEqual([row["ordinal"] for row in states], [1, 2])
        self.assertNotEqual(states[0]["persistent_state_paths"], states[1]["persistent_state_paths"])
        self.assertTrue(states[0]["persistent_state_paths"][0].endswith("/relay"))
        self.assertTrue(states[1]["persistent_state_paths"][0].endswith("/relay@2"))

    def test_recovery_restores_exact_copies_and_orders_all_prerequisites(self):
        selected = replace(c.DeploymentSelection.parse(selection()), enabled_roles=("http", "relay", "relay@2", "worker"), maintenance_holds=("relay@2",))
        enabled, held, running = recover.takeover_selection(self.inventory, {"selection": c.record_document(selected)})
        self.assertEqual(set(enabled), set(self.inventory.units))
        self.assertEqual(set(held), {self.second, self.worker})
        self.assertEqual(running, (self.http, self.first))
        self.assertLess(enabled.index(self.first), enabled.index(self.worker))
        self.assertLess(enabled.index(self.second), enabled.index(self.worker))
        intent = {"mode": "takeover", "recovery_enabled_roles": list(c.unit_selection_names(enabled)),
                  "recovery_maintenance_holds": list(c.unit_selection_names(held)), "recovery_roles": list(c.unit_selection_names(running))}
        self.assertEqual(feature_recovery.selected_roles(intent, self.inventory), (set(enabled), set(held), set(running)))
        intent["recovery_roles"].append("relay@2")
        with self.assertRaises(c.DeployError):
            feature_recovery.selected_roles(intent, self.inventory)

    def test_report_groups_copies_and_never_probes_a_hold(self):
        observations = {key: {"unit": unit} for key, unit in self.inventory.units.items()}
        result = report.services_document(observations)
        self.assertEqual(result["http"], {"unit": self.inventory.units[self.http]})
        self.assertEqual([row["ordinal"] for row in result["relay"]["copies"]], [1, 2])
        with patch.object(report, "role_service", return_value={"MainPID": "0", "ActiveState": "inactive"}), \
             patch.object(services, "probe") as probe:
            row = report.copy_runtime(self.host, self.inventory, self.second, {"held": True}, time.monotonic() + 1)
        self.assertEqual(row["readiness"], {"status": "held", "ready": None})
        self.assertEqual(row["admission"], "held")
        probe.assert_not_called()

    def test_report_copy_paths_listeners_and_grants_do_not_collapse(self):
        observations = {key: {"unit": unit, "held": key == self.second, "enabled": True} for key, unit in self.inventory.units.items()}
        network = {"listeners": [], "private_listeners": [{"role": "relay", "ordinal": n, "port": 18000 + n, "transport": "tcp"} for n in (1, 2)],
                   "confinement": [{"role": "relay", "ordinal": n, "state": "verified"} for n in (1, 2)]}
        self.inventory.resolution = replace(self.inventory.resolution, unix_endpoints=tuple(
            c.UnixEndpoint.for_listener(INSTANCE, "relay", "socket", "relay", ordinal=n) for n in (1, 2)))
        with patch.object(report, "copy_runtime", return_value={"readiness": {"status": "held", "ready": None}}):
            report.copy_report_details(self.host, self.entry, observations, (None, None, self.inventory, None), network, [], time.monotonic() + 1)
        self.assertEqual(observations[self.first]["listeners"][0]["port"], 18001)
        self.assertEqual(observations[self.second]["listeners"][0]["port"], 18002)
        self.assertNotEqual(observations[self.first]["persistent_state_paths"], observations[self.second]["persistent_state_paths"])
        self.assertEqual(observations[self.second]["confinement"]["ordinal"], 2)
        first_socket, = [row for row in observations[self.first]["listeners"] if row["transport"] == "unix"]
        second_socket, = [row for row in observations[self.second]["listeners"] if row["transport"] == "unix"]
        self.assertEqual((first_socket["ordinal"], second_socket["ordinal"]), (1, 2))
        self.assertNotEqual(first_socket["path"], second_socket["path"])



from test_feature_wiring import FeatureWiringFixture


class CopyRollbackHeldTests(FeatureWiringFixture):
    def test_missing_removed_evidence_remains_degraded_even_under_maintenance(self):
        import uuid
        self.build_archive(worker=True, independent=True)
        target = self.version
        plan = self.request("bootstrap-plan", topology=self.topology.projection())
        self.request("bootstrap-apply", topology=self.topology.projection(), plan_token=plan["plan_token"], confirm=True)
        self.request("activate", archive=str(self.archive), archive_sha256=c.digest(self.archive))
        current_profile = dict(self.profile)
        self.build_archive(independent=True, version="20261115T000000Z-abcdef12")
        self.profile = current_profile
        result = self.request("activate", archive=str(self.archive), archive_sha256=c.digest(self.archive))
        self.profile = result["profile_update"]
        _, entry = self.host.require_identity(self.project, self.profile)
        store = admission.AdmissionStore(self.host, self.profile["instance_id"])
        (store.durable / "removed-enablement.json").unlink()
        # Exercise the supported built-in stop policy under the real transition owner.
        def stopped(topology):
            return {role.common.name for role in topology.roles} - {"http"}
        with patch.object(feature_maintenance, "stopped_roles", side_effect=stopped):
            self.request("down", confirm=True)
            arguments = {"target": target, "expected_current": self.version, "operation_id": str(uuid.uuid4())}
            plan = self.request("release-plan", **arguments)
            self.assertEqual(plan["restored_copies"], [{"role": "worker", "ordinal": 1, "enabled": False, "evidence": "missing"}])
            result = self.request("rollback", **arguments, plan_token=plan["plan_token"])
        self.assertEqual(result["status"], "degraded")
        self.profile = result["profile_update"]
        _, entry = self.host.require_identity(self.project, self.profile)
        _, selected, inventory, _ = feature_lifecycle.load(self.host, entry)
        self.assertIn("worker", selected.maintenance_holds)
        self.assertNotIn(inventory.units[inventory.identity("worker")], self.host.enabled)


if __name__ == "__main__":
    unittest.main()
