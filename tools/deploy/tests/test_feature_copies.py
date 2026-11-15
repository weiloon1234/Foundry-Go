"""Reviewed numbered-copy transitions through the real lifecycle without systemd."""
from pathlib import Path
import sys
import unittest
from unittest.mock import patch
import uuid

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "lib"))

import admission
import contracts as c
import feature_lifecycle as lifecycle
import feature_runtime
import operations
import services
import shared_directories
from test_feature_wiring import FeatureWiringFixture


class FeatureCopiesTests(FeatureWiringFixture):
    def deploy_copies(self, count=3, *, independent=False):
        self.build_archive(copies=count, independent=independent, application_content=b"unchanged application")
        plan = self.request("bootstrap-plan", topology=self.topology.projection())
        self.request("bootstrap-apply", topology=self.topology.projection(), plan_token=plan["plan_token"], confirm=True)
        self.request("activate", archive=str(self.archive), archive_sha256=c.digest(self.archive))
        _, entry = self.host.require_identity(self.project, self.profile)
        return entry, lifecycle.load(self.host, entry)[2]

    def review_copies(self, count, version):
        current_profile = dict(self.profile)
        self.build_archive(copies=count, application_content=b"unchanged application", version=version)
        self.profile = current_profile
        request = {"archive": str(self.archive), "archive_sha256": c.digest(self.archive),
                   "operation_id": str(uuid.uuid4())}
        return request, self.request("release-plan", **request)

    def apply_copies(self, request, review):
        result = self.request("activate", **request, plan_token=review["plan_token"])
        self.profile = result["profile_update"]
        _, entry = self.host.require_identity(self.project, self.profile)
        return entry, lifecycle.load(self.host, entry)[2]

    def removed_invocation(self):
        entry, inventory = self.deploy_copies()
        removed = inventory.identity("relay", 3)
        unit = inventory.units[removed]
        process = self.host.running[unit][0]
        launch = lifecycle.load_resolution(self.host, entry, inventory.resolution.id)[1]
        pin = c.HelperInvocation(admission.boot_id(self.host), removed, process,
                                self.host.read_registry()["tools"]["bundle_sha256"],
                                c.canonical_digest(c.record_document(inventory.specs[removed])),
                                inventory.resolution.id, 1, None)
        request, review = self.review_copies(2, "20261115T000000Z-abcdef12")
        self.apply_copies(request, review)
        self.host.owned_write(self.host.paths.p("/proc/self/cgroup"), "0::/system.slice/" + unit)
        return c.record_document(pin), launch

    def test_cleanup_accepts_deselected_removed_copy(self):
        pin, launch = self.removed_invocation()
        with patch.object(admission, "AdmissionStore", side_effect=AssertionError("cleanup read current admission")):
            self.assertEqual(0, feature_runtime.run("cleanup", pin, launch))

    def test_deselected_start_hooks_and_invalid_cleanup_still_refuse(self):
        pin, launch = self.removed_invocation()
        for action in ("admit", "prepare", "running"):
            with self.subTest(action=action), self.assertRaises(c.DeployError) as raised:
                feature_runtime.run(action, pin, launch)
            self.assertEqual("feature_runtime_selection", raised.exception.code)
        for field, value in (("unit_spec_sha256", "a" * 64), ("resolution_id", str(uuid.uuid4()))):
            with self.subTest(field=field), self.assertRaises(c.DeployError) as raised:
                feature_runtime.run("cleanup", {**pin, field: value}, launch)
            self.assertEqual("feature_runtime_selection", raised.exception.code)
        self.host.owned_write(self.host.paths.p("/proc/self/cgroup"), "0::/system.slice/unrelated.service")
        with self.assertRaises(c.DeployError) as raised:
            feature_runtime.run("cleanup", pin, launch)
        self.assertEqual("feature_runtime_identity", raised.exception.code)

    def test_shrink_records_failed_removed_unit_before_reset_and_leaves_it_inactive(self):
        _, inventory = self.deploy_copies()
        removed = inventory.identity("relay", 3)
        unit = inventory.units[removed]
        self.host.failed.add("unrelated.service")
        request, review = self.review_copies(2, "20261115T000000Z-abcdef12")
        command = self.host.run
        resets = []
        def failed_stop(args, **kwargs):
            if args[:2] == ["systemctl", "reset-failed"]:
                self.assertEqual([unit], args[2:])
                record = c.read_json(operations.directory(self.host, request["operation_id"]) /
                                     ("removed-unit-" + removed.record_key + ".json"))
                self.assertEqual((unit, "loaded", "failed"),
                                 tuple(record[key] for key in ("Id", "LoadState", "ActiveState")))
                self.assertEqual(("0", "0", "", "0"),
                                 tuple(record[key] for key in ("MainPID", "ControlPID", "ControlGroup", "Job")))
                self.assertTrue(self.host.paths.p("/etc/systemd/system/" + unit).is_file())
                store = admission.AdmissionStore(self.host, removed.instance_id)
                self.assertTrue(c.read_json(store.durable / "removed-enablement.json")[removed.record_key])
                closure = c.ClosureEvidence.parse(c.read_json(store.runtime / "boot.json"))
                self.assertIn(removed, closure.units)
                resets.append(unit)
            result = command(args, **kwargs)
            if args == ["systemctl", "stop", unit]:
                self.host.failed.add(unit)
            return result
        with patch.object(self.host, "run", side_effect=failed_stop):
            self.apply_copies(request, review)
        self.assertEqual([unit], resets)
        proof = services.stopped_unit(self.host, unit)
        self.assertEqual(("not-found", "inactive"), (proof["LoadState"], proof["ActiveState"]))
        self.assertEqual({"unrelated.service"}, self.host.failed)

    def test_shrink_retains_unit_when_failed_state_does_not_clear(self):
        _, inventory = self.deploy_copies()
        removed = inventory.identity("relay", 3)
        unit = inventory.units[removed]
        request, review = self.review_copies(2, "20261115T000000Z-abcdef12")
        command = self.host.run
        def failed_reset(args, **kwargs):
            result = command(args, **kwargs)
            if args in (["systemctl", "stop", unit], ["systemctl", "reset-failed", unit]):
                self.host.failed.add(unit)
            return result
        with patch.object(self.host, "run", side_effect=failed_reset), self.assertRaises(c.DeployError) as raised:
            self.apply_copies(request, review)
        self.assertEqual("feature_stop_unverified", raised.exception.code)
        self.assertTrue(self.host.paths.p("/etc/systemd/system/" + unit).is_file())
        self.assertTrue(self.host.paths.p(services.ServicePaths.for_unit(removed, inventory.resolution.id).base).exists())
        self.assertIn(unit, self.host.enabled)

    def test_regrow_disabled_copy_stops_excluded_running_dependent(self):
        _, inventory = self.deploy_copies(2)
        second = inventory.identity("relay", 2)
        dependent = inventory.identity("http")
        self.host.run(["systemctl", "stop", inventory.units[second]])
        self.host.run(["systemctl", "disable", inventory.units[second]])
        request, review = self.review_copies(1, "20261115T000000Z-abcdef12")
        _, shrunk = self.apply_copies(request, review)
        self.assertIn(shrunk.units[dependent], self.host.running)
        request, review = self.review_copies(2, "20261116T000000Z-abcdef12")
        self.assertNotIn(dependent.record_key, review["running_roles"])
        self.assertNotIn(dependent.record_key, review["enabled_roles"])
        self.assertIn(dependent.record_key, review["unit_changes"])
        stopped = services.stopped_unit
        proofs = []
        def prove_exit(host, unit):
            stopped(host, unit)
            proofs.append(unit)
        with patch.object(services, "stopped_unit", side_effect=prove_exit):
            entry, grown = self.apply_copies(request, review)
        self.assertIn(grown.units[dependent], proofs)
        self.assertNotIn(grown.units[dependent], self.host.running)
        self.assertNotIn(grown.units[dependent], self.host.enabled)
        self.assertNotIn(grown.units[second], self.host.enabled)
        self.assertNotIn(grown.units[second], self.host.running)
        evidence = c.ClosureEvidence.parse(c.read_json(admission.AdmissionStore(
            self.host, grown.profile.instance_id).runtime / "boot.json"))
        self.assertIn(dependent, evidence.units)

    def test_grow_and_shrink_preserve_survivors_and_remove_only_proven_stopped_views(self):
        entry, initial = self.deploy_copies()
        processes = {identity: self.host.running[unit][0] for identity, unit in initial.units.items()}
        originals = dict(initial.specs)
        shared = self.host.paths.p(c.shared_directory_path(initial.profile.instance_id, "kept"))
        shared.mkdir(parents=True, exist_ok=True)
        shared_file = shared / "kept"
        shared_file.write_text("shared cache")
        state_files = {}
        for identity in initial.copies("relay"):
            paths = services.ServicePaths.for_unit(identity, initial.resolution.id)
            state = self.host.paths.p(paths.state) / "data" / "copy-owned"
            state.parent.mkdir(parents=True, exist_ok=True)
            state.write_text(identity.record_key)
            state_files[identity] = state
        request, grow = self.review_copies(4, "20261115T000000Z-abcdef12")
        self.assertEqual(grow["added_units"], ["relay@4"])
        self.assertEqual(grow["unit_changes"], ["relay@4"])
        self.assertEqual(set(grow["retained_units"]), {identity.record_key for identity in initial.units})
        with patch.object(shared_directories, "discard") as discard:
            entry, grown = self.apply_copies(request, grow)
        discard.assert_not_called()
        for identity in initial.units:
            self.assertEqual(grown.specs[identity], originals[identity])
            self.assertEqual(self.host.running[grown.units[identity]][0], processes[identity])
        self.assertTrue(set(initial.resolution.endpoints) <= set(grown.resolution.endpoints))
        removed = (grown.identity("relay", 3), grown.identity("relay", 4))
        removed_views = {identity: self.host.paths.p(services.ServicePaths.for_unit(
            identity, self.host.running[grown.units[identity]][1].resolution.id).base) for identity in removed}
        request, shrink = self.review_copies(2, "20261116T000000Z-abcdef12")
        self.assertEqual(shrink["removed_units"], ["relay@3", "relay@4"])
        self.assertEqual(shrink["unit_changes"], ["relay@3", "relay@4"])
        recorded = []
        record = lifecycle.record_removed_copy_enablement
        def after_proof(host, selected_entry, identity, enabled):
            self.assertNotIn(grown.units[identity], host.running)
            self.assertTrue(host.paths.p("/etc/systemd/system/" + grown.units[identity]).exists())
            self.assertTrue(removed_views[identity].exists())
            store = admission.AdmissionStore(host, identity.instance_id)
            evidence = c.ClosureEvidence.parse(c.read_json(store.runtime / "boot.json"))
            self.assertIn(identity, evidence.units)
            recorded.append(identity)
            record(host, selected_entry, identity, enabled)
        with patch.object(lifecycle, "record_removed_copy_enablement", side_effect=after_proof), \
                patch.object(shared_directories, "discard") as discard:
            entry, shrunk = self.apply_copies(request, shrink)
        discard.assert_not_called()
        self.assertEqual(set(recorded), set(removed))
        self.assertEqual(tuple(identity.ordinal for identity in shrunk.copies("relay")), (1, 2))
        for identity in shrunk.units:
            self.assertEqual(shrunk.specs[identity], originals[identity])
            self.assertEqual(self.host.running[shrunk.units[identity]][0], processes[identity])
        for identity in removed:
            self.assertNotIn(grown.units[identity], self.host.enabled)
            self.assertFalse(self.host.paths.p("/etc/systemd/system/" + grown.units[identity]).exists())
            self.assertFalse(removed_views[identity].exists())
            self.assertEqual("inactive", services.stopped_unit(self.host, grown.units[identity])["ActiveState"])
            self.assertNotIn(["systemctl", "reset-failed", grown.units[identity]], self.host.commands)
        for identity, path in state_files.items():
            self.assertEqual(path.read_text(), identity.record_key)
        self.assertEqual(shared_file.read_text(), "shared cache")
        self.assertEqual(c.DeploymentSelection.parse(entry["deployment_selection"]).minimum_generation, 18)

    def test_failed_removed_copy_exit_does_not_record_disable_or_unlink(self):
        _, inventory = self.deploy_copies()
        removed = inventory.identity("relay", 3)
        request, review = self.review_copies(2, "20261115T000000Z-abcdef12")
        stopped = services.stopped_unit
        def require_exit(host, unit):
            if unit == inventory.units[removed]:
                raise c.DeployError("feature_stop_unverified", "A copy still owns a child process.")
            return stopped(host, unit)
        with patch.object(services, "stopped_unit", side_effect=require_exit), \
                patch.object(lifecycle, "record_removed_copy_enablement") as record, self.assertRaises(c.DeployError):
            self.apply_copies(request, review)
        record.assert_not_called()
        self.assertIn(inventory.units[removed], self.host.enabled)
        self.assertTrue(self.host.paths.p("/etc/systemd/system/" + inventory.units[removed]).exists())
        self.assertTrue(self.host.paths.p(services.ServicePaths.for_unit(removed, inventory.resolution.id).base).exists())
        self.assertNotIn(["systemctl", "reset-failed", inventory.units[removed]], self.host.commands)

    def test_plain_stop_then_restart_enables_and_verifies_every_copy(self):
        entry, inventory = self.deploy_copies(2)
        self.assertEqual(self.request("stop")["status"], "stopped")
        self.assertFalse(self.host.enabled & set(inventory.units.values()))
        self.assertFalse(self.host.running)
        _, entry = self.host.require_identity(self.project, self.profile)
        self.assertEqual(lifecycle.load(self.host, entry)[1].enabled_roles, ())

        self.host.commands.clear()
        with patch.object(services, "wait_ready_set", wraps=services.wait_ready_set) as readiness:
            result = self.request("restart")
        self.assertEqual(result["status"], "healthy")
        self.assertEqual(set(services.enabled_units(self.host, inventory.units)), set(inventory.units))
        self.assertEqual(set(self.host.running), set(inventory.units.values()))
        self.assertTrue(any(set(call.args[2]) == set(inventory.units) for call in readiness.call_args_list))
        starts = [command[3:] for command in self.host.commands if command[:3] == ["systemctl", "start", "--no-block"]]
        self.assertEqual(starts, [[inventory.units[name] for name in inventory.copies("relay")],
                                  [inventory.units[inventory.identity("http")]]])
        _, entry = self.host.require_identity(self.project, self.profile)
        store, selection, inventory, _ = lifecycle.load(self.host, entry)
        self.assertEqual(set(selection.enabled_roles), set(c.unit_selection_names(inventory.units)))
        self.assertEqual(selection.admission.disposition, c.AdmissionDisposition.COMMITTED)
        self.assertEqual(self.host.load_state(entry)["status"], "healthy")
        store.require_cleanup_clear()

    def test_plain_restart_after_stop_retains_maintenance_holds(self):
        import feature_maintenance
        entry, inventory = self.deploy_copies(2, independent=True)
        self.request("down", confirm=True)
        self.request("stop")
        result = self.request("restart")
        self.assertEqual(result["status"], "healthy")
        self.assertTrue(self.host.maintenance_down)
        self.assertEqual(set(services.enabled_units(self.host, inventory.units)), set(inventory.units))
        self.assertEqual(set(self.host.running), {inventory.units[inventory.identity("http")]})
        _, entry = self.host.require_identity(self.project, self.profile)
        _, selection, inventory, _ = lifecycle.load(self.host, entry)
        self.assertEqual(set(selection.maintenance_holds), set(c.unit_selection_names(inventory.copies("relay"))))
        for identity in inventory.copies("relay"):
            self.assertTrue(self.host.paths.p(feature_maintenance.marker_path(identity)).is_file())
        self.request("up", confirm=True)
        self.assertEqual(set(self.host.running), set(inventory.units.values()))

    def test_named_restart_refuses_hold_and_up_preserves_disabled_copy(self):
        entry, inventory = self.deploy_copies(2, independent=True)
        first, second = inventory.copies("relay")
        self.request("down", confirm=True)
        self.request("stop", role="relay", copy=2)
        with self.assertRaises(c.DeployError) as error:
            self.request("restart", role="relay", copy=2)
        self.assertEqual(error.exception.code, "service_held")
        self.assertNotIn(inventory.units[second], self.host.enabled)
        self.assertNotIn(inventory.units[second], self.host.running)
        self.request("up", confirm=True)
        self.assertIn(inventory.units[first], self.host.running)
        self.assertNotIn(inventory.units[second], self.host.enabled)
        self.assertNotIn(inventory.units[second], self.host.running)
        self.request("restart", role="relay", copy=2)
        self.assertEqual(set(services.enabled_units(self.host, inventory.units)), set(inventory.units))
        self.assertEqual(set(self.host.running), set(inventory.units.values()))

    def test_copy_selector_stops_restarts_one_copy_and_preserves_siblings(self):
        entry, inventory = self.deploy_copies(2, independent=True)
        first, second = inventory.copies("relay")
        original = self.host.running[inventory.units[first]]
        http = self.host.running[inventory.units[inventory.identity("http")]]
        result = self.request("stop", role="relay", copy=2)
        self.assertEqual(result["status"], "degraded")
        self.assertNotIn(inventory.units[second], self.host.enabled)
        self.assertNotIn(inventory.units[second], self.host.running)
        self.assertEqual(self.host.running[inventory.units[first]], original)
        self.request("restart", role="relay", copy=2)
        self.assertIn(inventory.units[second], self.host.running)
        self.assertIn(inventory.units[second], self.host.enabled)
        self.assertEqual(self.host.running[inventory.units[first]], original)
        self.assertEqual(self.host.running[inventory.units[inventory.identity("http")]], http)

    def test_public_copy_operations_preserve_sibling_route_and_allocation(self):
        import edge
        self.build_archive(copies=2, independent=True, public=True)
        self.configure_firewall()
        plan = self.request("bootstrap-plan", topology=self.topology.projection())
        self.request("bootstrap-apply", topology=self.topology.projection(), plan_token=plan["plan_token"], confirm=True)
        self.request("activate", archive=str(self.archive), archive_sha256=c.digest(self.archive))
        _, entry = self.host.require_identity(self.project, self.profile)
        inventory = lifecycle.load(self.host, entry)[2]
        first, second = inventory.copies("relay")
        original = self.host.running[inventory.units[first]]
        endpoint = next(item for item in inventory.resolution.endpoints if (item.role, item.ordinal) == (first.role, first.ordinal))
        directory = self.host.paths.p(edge.RUNTIME + "/" + inventory.profile.instance_id)
        first_route = (directory / "stream-relay.api.conf").read_bytes()
        second_route = directory / "stream-relay.api@2.conf"
        self.request("stop", role="relay", copy=2)
        self.assertFalse(second_route.exists())
        self.assertEqual((directory / "stream-relay.api.conf").read_bytes(), first_route)
        self.request("restart", role="relay", copy=2)
        self.assertTrue(second_route.exists())
        self.assertEqual((directory / "stream-relay.api.conf").read_bytes(), first_route)
        _, entry = self.host.require_identity(self.project, self.profile)
        inventory = lifecycle.load(self.host, entry)[2]
        self.assertIn(endpoint, inventory.resolution.endpoints)
        self.assertEqual(self.host.running[inventory.units[first]], original)
        self.assertIn(inventory.units[second], self.host.running)

    def test_named_stop_refuses_enabled_dependents(self):
        _, inventory = self.deploy_copies(2)
        with self.assertRaises(c.DeployError) as caught:
            self.request("stop", role="relay", copy=2)
        self.assertEqual(caught.exception.code, "service_dependents")
        self.assertIn(inventory.units[inventory.identity("relay", 2)], self.host.running)

    def test_copy_consumer_growth_backup_and_rollback_keep_private_snapshots(self):
        import backup
        import runtime_config
        self.build_archive(copies=2, independent=True, copies_binding=True, application_content=b"same")
        initial = self.version
        private = {"FOUNDRY_SERVICE_SECRET__COPY__RELAY__" + str(n) + "__COPY_TOKEN": "copy-private-" + str(n) for n in (1, 2, 3)}
        plan = self.request("bootstrap-plan", topology=self.topology.projection())
        self.request("bootstrap-apply", topology=self.topology.projection(), plan_token=plan["plan_token"], confirm=True,
                     secrets={"configuration": private})
        self.request("activate", archive=str(self.archive), archive_sha256=c.digest(self.archive))
        _, entry = self.host.require_identity(self.project, self.profile)
        inventory = lifecycle.load(self.host, entry)[2]
        private_paths = runtime_config.service_secret_snapshot(self.host, entry)
        first = inventory.identity("relay")
        original = self.host.running[inventory.units[first]]
        http = self.host.running[inventory.units[inventory.identity("http")]]
        current_profile = self.profile.copy()
        self.build_archive(copies=3, independent=True, copies_binding=True, application_content=b"same", version="20261115T000000Z-abcdef12")
        self.profile = current_profile
        request = {"archive": str(self.archive), "archive_sha256": c.digest(self.archive), "operation_id": str(uuid.uuid4())}
        review = self.request("release-plan", **request)
        self.assertIn("http", review["changed_units"])
        self.assertIn("relay", review["retained_units"])
        entry, inventory = self.apply_copies(request, review)
        self.assertEqual(self.host.running[inventory.units[first]], original)
        self.assertNotEqual(self.host.running[inventory.units[inventory.identity("http")]], http)
        binding, = inventory.specs[inventory.identity("http")].bindings.copies
        self.assertEqual([item.ordinal for item in binding.copies], [1, 2, 3])
        _, _, _, launch = lifecycle.load(self.host, entry)
        self.assertEqual(set(launch["secret_paths"]), {"relay", "relay@2", "relay@3"})
        for item in binding.copies:
            key = c.UnitIdentity(inventory.profile.instance_id, "relay", item.ordinal).record_key
            self.assertEqual(item.secret_file, launch["secret_paths"][key]["TOKEN_FILE"])
        directory = self.host.paths.instance_config(entry["name"])
        files = {"instances/" + entry["name"] + "/" + path.relative_to(directory).as_posix(): path
                 for path in runtime_config.service_secret_snapshot_paths(self.host, entry)}
        with patch.object(backup, "instance_paths", return_value={"configuration": str(directory)}):
            snapshot = backup._feature_snapshot(self.host, entry, files)
        self.assertEqual(set(snapshot["secret_paths"]), set(launch["secret_paths"]))
        rollback = {"target": initial, "expected_current": inventory.deployment.release, "operation_id": str(uuid.uuid4())}
        review = self.request("release-plan", **rollback)
        result = self.request("rollback", **rollback, plan_token=review["plan_token"])
        self.profile = result["profile_update"]
        _, entry = self.host.require_identity(self.project, self.profile)
        inventory = lifecycle.load(self.host, entry)[2]
        self.assertEqual(len(inventory.copies("relay")), 2)
        self.assertEqual(runtime_config.service_secret_snapshot(self.host, entry), private_paths)
        rollback = {"target": "20261115T000000Z-abcdef12", "expected_current": initial, "operation_id": str(uuid.uuid4())}
        review = self.request("release-plan", **rollback)
        self.assertEqual(review["restored_copies"], [{"role": "relay", "ordinal": 3, "enabled": True, "evidence": "recorded"}])
        result = self.request("rollback", **rollback, plan_token=review["plan_token"])
        self.profile = result["profile_update"]
        _, entry = self.host.require_identity(self.project, self.profile)
        inventory = lifecycle.load(self.host, entry)[2]
        self.assertIn(inventory.units[inventory.identity("relay", 3)], self.host.enabled)

    def test_admission_refuses_unplaced_copy_of_a_declared_role(self):
        entry, inventory = self.deploy_copies(2)
        store, selection, _, _ = lifecycle.load(self.host, entry)
        from dataclasses import replace
        invalid = replace(selection, enabled_roles=selection.enabled_roles + ("relay@3",))
        with self.assertRaises(c.DeployError) as caught:
            store._matching(invalid, inventory.resolution)
        self.assertEqual(caught.exception.code, "admission_selection")

    def test_runtime_cleanup_never_follows_a_copy_symlink_into_state(self):
        _, inventory = self.deploy_copies()
        identity = inventory.identity("relay", 3)
        paths = services.ServicePaths.for_unit(identity, str(uuid.uuid4()))
        target = self.host.paths.p(paths.state) / "data" / "keep"
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_text("persistent")
        base = self.host.paths.p(paths.base)
        base.parent.mkdir(parents=True)
        base.symlink_to(target.parent, target_is_directory=True)
        with self.assertRaises(c.DeployError):
            lifecycle.remove_copy_runtime(self.host, identity)
        self.assertEqual(target.read_text(), "persistent")


if __name__ == "__main__":
    unittest.main()
