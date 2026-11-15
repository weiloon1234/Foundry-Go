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
import services
import shared_directories
from test_feature_wiring import FeatureWiringFixture


class FeatureCopiesTests(FeatureWiringFixture):
    def deploy_copies(self, count=3):
        self.build_archive(copies=count, application_content=b"unchanged application")
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
        for identity, path in state_files.items():
            self.assertEqual(path.read_text(), identity.record_key)
        self.assertEqual(shared_file.read_text(), "shared cache")
        self.assertEqual(c.DeploymentSelection.parse(entry["deployment_selection"]).minimum_generation, 17)

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
