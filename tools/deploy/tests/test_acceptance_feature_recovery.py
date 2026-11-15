"""Pure fixture/receipt checks; no container, package or service operations."""
import copy
import hashlib
import io
import json
from pathlib import Path
import tarfile
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch
import uuid

import acceptance_feature as feature
import acceptance_feature_recovery as scenario
from contracts import ReleaseDeclaration, canonical_digest, digest, unit_selection_names
import recover
import services
from test_artifacts import elf
from test_contracts import fixture
from topology import parse_profile, parse_topology


class FeatureRecoveryAcceptanceTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name).resolve()
        project, profile, entry = fixture()
        self.version = "20261101T000000Z-" + "a" * 40
        manifest = {"project_id": project["id"], "instance_id": profile["instance_id"],
                    "instance": entry["name"], "environment": profile["environment"],
                    "architecture": "amd64", "version": self.version}
        self.map_path = "private-source-maps/" + self.version + "/app.map"
        files = {"manifest.json": json.dumps(manifest).encode(), "artisan": elf(),
                 "config/deploy.toml": b'[app.services.storage.disks.attachments]\ndriver="r2"\n',
                 "public/index.html": b"fixture", self.map_path: b"{}"}
        source = self.root / "input.tar.gz"
        with tarfile.open(source, "w:gz") as archive:
            for name, data in files.items():
                item = tarfile.TarInfo(name)
                item.size = len(data)
                archive.addfile(item, io.BytesIO(data))
        self.archive = self.root / "feature.tar.gz"
        with patch.object(feature, "read_json", return_value=profile):
            feature.feature_archive(source, self.archive, entry["host_id"], confined=True, copies=3)

    def test_confined_wildcard_fixture_has_valid_versioned_executable_and_generation(self):
        from test_contracts import fixture
        import upgrades
        profile = fixture()[1]
        with patch.object(feature, "read_json", return_value=profile):
            _, _, declaration, _ = feature.feature_archive(self.root / "input.tar.gz", self.archive,
                                                          profile["host"]["host_id"], confined=True)
        role = next(r for r in declaration.roles if r.common.name == "wildcard")
        self.assertEqual(role.common.confinement, "loopback")
        self.assertTrue(dict(role.listeners)["control"].wildcard)
        self.assertTrue(role.executable.package.endswith("-minimal"))
        self.assertIn(role.executable.path.removeprefix("/usr/bin/"), role.executable.package)
        compile(role.arguments[2].value, "wildcard-fixture", "exec")
        self.assertEqual(upgrades.feature_generation(declaration, {}), 16)

    def test_state_variants_remain_valid_and_preserve_payload_and_source(self):
        original = digest(self.archive)
        old = scenario.read_manifest(self.archive)
        for state in ("local-unprotected", "disposable"):
            with self.subTest(state=state):
                version = "20261102T000000Z-" + uuid.uuid4().hex
                path = self.root / (state + ".tar.gz")
                scenario.state_archive(self.archive, path, state, version)
                manifest = scenario.read_manifest(path)
                declaration = ReleaseDeclaration.parse(manifest["deployment"])
                self.assertEqual(manifest["version"], version)
                self.assertEqual(declaration.topology_sha256, manifest["topology_sha256"])
                self.assertEqual(manifest["deployment"]["profile"], old["deployment"]["profile"])
                for name in ("relay", feature.COPIES_ROLE):
                    role = next(role for role in manifest["deployment"]["topology"]["roles"] if role["name"] == name)
                    self.assertEqual(role["state"], {"kind": state, **({"acknowledged": True} if state == "local-unprotected" else {})})
                self.assertEqual(3, manifest["deployment"]["profile"]["placement"][feature.COPIES_ROLE])
                self.assertEqual(manifest["deployment"]["artifacts"], old["deployment"]["artifacts"])
                self.assertEqual(manifest["files"]["artisan"], old["files"]["artisan"])
                with tarfile.open(path, "r:gz") as archive:
                    self.assertNotIn(self.map_path, archive.getnames())
                    self.assertEqual(archive.extractfile("private-source-maps/" + version + "/app.map").read(), b"{}")
                    self.assertEqual(set(archive.getnames()), {"manifest.json", *manifest["files"]})
                    for member in declaration.inventory:
                        data = archive.extractfile(member.path).read()
                        self.assertEqual(len(data), member.size)
                        self.assertEqual(hashlib.sha256(data).hexdigest(), member.sha256)
                        self.assertEqual(manifest["files"][member.path], member.sha256)
                    self.assertTrue(archive.getmember("artisan").mode & 0o111)
        self.assertEqual(digest(self.archive), original)

    def inventory(self, protected):
        state = "disposable" if protected else "local-unprotected"
        manifest = scenario.state_archive(self.archive, self.root / (state + ".tar.gz"), state,
                                          "20261102T000000Z-" + uuid.uuid4().hex)
        # Coverage and ordering need placement only, without materializing executables.
        inventory = object.__new__(services.UnitInventory)
        inventory.topology = parse_topology(manifest["deployment"]["topology"])
        inventory.profile = parse_profile(manifest["deployment"]["profile"], inventory.topology)
        inventory.roles = {role.common.name: role for role in inventory.topology.roles}
        inventory.units = services.expanded_unit_names(manifest["instance"], inventory.topology, inventory.profile)
        inventory.resolution = SimpleNamespace(id=str(uuid.uuid4()))
        return inventory

    def receipt(self, protected, inventory):
        findings = [] if protected else [{"code": "service_state_unprotected", "role": "relay"}]
        coverage = {"supported_components": ["postgresql", "encrypted_configuration", "selected_deployment"],
                    "complete_instance_coverage": protected,
                    "service_state": [{"role": "relay", "kind": "disposable" if protected else "local-unprotected"},
                                      {"role": "socket-provider", "kind": "disposable"},
                                      {"role": "wildcard", "kind": "disposable"}],
                    "findings": findings}
        for identity in inventory.copies(feature.COPIES_ROLE):
            coverage["service_state"].append({"role": feature.COPIES_ROLE, "ordinal": identity.ordinal,
                "kind": "disposable" if protected else "local-unprotected",
                "persistent_state_paths": [services.ServicePaths.for_unit(identity, inventory.resolution.id).state]})
            if not protected:
                findings.append({"code": "service_state_unprotected", "role": feature.COPIES_ROLE, "ordinal": identity.ordinal})
        return {"status": "complete", "pgbackrest_label": "20261101-000000F",
                "database_point": {"pgbackrest_label": "20261101-000000F"}, "deployment_sha256": "a" * 64,
                "coverage": coverage, "key_archive": {"status": "complete", "object_key": "fixture/keys.age",
                    "deployment_sha256": "a" * 64, "coverage": {
                        **copy.deepcopy(coverage), "complete_instance_coverage": False,
                        "supported_components": ["encrypted_configuration", "selected_deployment"]}}}

    def test_receipt_assertion_distinguishes_success_and_complete_coverage(self):
        for protected in (True, False):
            inventory = self.inventory(protected)
            scenario.assert_coverage(self.receipt(protected, inventory), inventory, protected=protected)
        inventory = self.inventory(False)
        for mutate in (
                lambda r: r.update(status="failed"),
                lambda r: r["coverage"].update(complete_instance_coverage=True),
                lambda r: r["coverage"].update(findings=[]),
                lambda r: r["key_archive"]["coverage"].update(complete_instance_coverage=True),
                lambda r: r["key_archive"].update(status="failed"),
                lambda r: r["coverage"].update(supported_components=["postgresql"]),
                lambda r: r["key_archive"].update(deployment_sha256="b" * 64),
                lambda r: r["coverage"]["service_state"].pop(),
                lambda r: r["coverage"]["service_state"][-1].update(ordinal=2),
                lambda r: r["coverage"]["service_state"][-1].update(persistent_state_paths=[]),
                lambda r: r["key_archive"]["coverage"]["service_state"].pop()):
            receipt = self.receipt(False, inventory)
            mutate(receipt)
            with self.assertRaises(AssertionError):
                scenario.assert_coverage(receipt, inventory, protected=False)

    def test_recovery_enablement_excludes_every_copy_and_keeps_provider_first(self):
        inventory = self.inventory(True)
        enabled = scenario.recovery_units(inventory)
        self.assertEqual(("socket-provider", "relay"), unit_selection_names(enabled))
        disabled = set(inventory.units) - set(enabled)
        self.assertEqual({"http", "wildcard", "copies", "copies@2", "copies@3"},
                         set(unit_selection_names(sorted(disabled))))

    def test_replacement_binding_preserves_source_and_binds_new_host_operation(self):
        manifest = scenario.read_manifest(self.archive)
        metadata = {"profile": manifest["deployment"]["profile"],
                    "host_id": manifest["deployment"]["profile"]["host"]["host_id"],
                    "feature_recovery": {"deployment": {"fixture": "selected-descriptor"}}}
        original = copy.deepcopy(metadata)
        profile = copy.deepcopy(metadata["profile"])
        profile["host"]["host_id"] = str(uuid.uuid4())
        operation = str(uuid.uuid4())
        document = scenario.replacement_binding(metadata, profile["host"]["host_id"], operation)
        parsed = recover.replacement_binding(document, metadata, profile, operation, "takeover")
        self.assertEqual(parsed.deployment_sha256, canonical_digest(metadata["feature_recovery"]["deployment"]))
        self.assertEqual(parsed.target_host_id, profile["host"]["host_id"])
        self.assertEqual(parsed.operation_id, operation)
        self.assertEqual(parsed.dns_cutover, "external_pending")
        self.assertEqual(parsed.observations, ())
        self.assertEqual(parsed.listeners[0].replacement_addresses, scenario.TARGET_ADDRESSES)
        self.assertFalse(set(parsed.listeners[0].old_addresses) & set(scenario.TARGET_ADDRESSES))
        self.assertEqual(metadata, original)
        effective = recover.effective_profile(profile, parsed)
        self.assertEqual(effective["listeners"]["relay.control"]["dns"]["public_addresses"], list(scenario.TARGET_ADDRESSES))
        self.assertEqual(profile["listeners"], metadata["profile"]["listeners"])


if __name__ == "__main__":
    unittest.main()
