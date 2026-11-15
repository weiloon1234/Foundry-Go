"""Shared cache declarations, delivery and mount refusal boundaries (no mounts)."""
from dataclasses import replace
import copy
import os
from pathlib import Path
import subprocess
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "lib"))
import backup
import contracts as c
from host import Host, Paths
import services
import shared_directories as shared
import topology as t
import upgrades
from test_services import inventory_fixture
from test_host_capabilities import INSTANCE, DIGEST, CURRENT


def shared_inventory():
    old, entry, _ = inventory_fixture()
    raw = old.topology.projection()
    raw["shared_directories"] = {"render-cache": {"state": "disposable", "max_bytes": 1048576,
                                                "writers": ["http"], "readers": ["relay"]}}
    raw["roles"][1]["arguments"] = [{"shared_directory_path": "render-cache"}]
    raw["roles"][1]["environment"] = {"RENDER_CACHE": {"shared_directory_path": "render-cache"}}
    topology = t.parse_topology(raw)
    units = tuple(replace(old.specs[role.common.name], declaration_sha256=c.canonical_digest(document),
                          arguments=(c.shared_directory_path(INSTANCE, "render-cache"),) if role.common.name == "relay" else (),
                          bindings=replace(old.specs[role.common.name].bindings,
                                           shared_directories=shared.bindings(topology, INSTANCE, role.common.name)))
                  for role, document in zip(topology.roles, raw["roles"]))
    deployment = replace(old.deployment, topology_sha256=topology.sha256, units=units)
    resolution = replace(old.resolution, deployment_sha256=c.canonical_digest(c.record_document(deployment)))
    return services.UnitInventory(old.name, topology, old.profile, deployment, resolution), entry


class SharedDeclarationTests(unittest.TestCase):
    def test_typed_roundtrip_access_and_generation(self):
        inventory, _ = shared_inventory()
        self.assertEqual(c.Deployment.parse(c.record_document(inventory.deployment)), inventory.deployment)
        self.assertEqual(inventory.specs["http"].bindings.shared_directories[0].access, "read-write")
        self.assertEqual(inventory.specs["relay"].bindings.shared_directories[0].access, "read-only")
        self.assertEqual(inventory.specs["worker"].bindings.shared_directories, ())
        self.assertNotIn("shared_directories", c.record_document(inventory.specs["worker"].bindings))
        self.assertEqual(upgrades.feature_generation(inventory.topology, {}), 16)
        with self.assertRaises(c.DeployError):
            upgrades.check_feature_generation({"tools": {"generation": 15}}, inventory.topology, {}, client_generation=16)
        with self.assertRaises(c.DeployError):
            upgrades.check_feature_generation({"tools": {"generation": 16}}, inventory.topology, {}, client_generation=15)
        self.assertEqual(upgrades.feature_generation(t.parse_topology({"schema": 1, "roles": [{"name": "http"}]}),
                                                    {"deployment_selection": {"minimum_generation": 16}}), 16)

    def test_reject_invalid_state_limit_roles_and_ungranted_references(self):
        inventory, _ = shared_inventory()
        valid = inventory.topology.projection()
        for value in (0, -1, True, 65537, "1048576", (1 << 40) + 65536):
            raw = copy.deepcopy(valid)
            raw["shared_directories"]["render-cache"]["max_bytes"] = value
            with self.subTest(value=value), self.assertRaises(c.DeployError):
                t.parse_topology(raw)
        for change in ({"state": "durable"}, {"writers": []}, {"writers": ["missing"]},
                       {"readers": ["http"]}, {"readers": ["relay", "relay"]}, {"unknown": 1}):
            raw = copy.deepcopy(valid)
            raw["shared_directories"]["render-cache"].update(change)
            with self.subTest(change=change), self.assertRaises(c.DeployError):
                t.parse_topology(raw)
        raw = copy.deepcopy(valid)
        raw["shared_directories"]["render-cache"]["readers"] = []
        with self.assertRaises(c.DeployError):
            t.parse_topology(raw)
        raw = copy.deepcopy(valid)
        raw["roles"][0].setdefault("bindings", {})["render-cache"] = {"executable": {"package": "jq", "path": "/usr/bin/jq"}}
        with self.assertRaises(c.DeployError):
            t.parse_topology(raw)

    def test_actual_unit_views_for_app_reader_and_unlisted_role(self):
        inventory, entry = shared_inventory()
        path = c.shared_directory_path(INSTANCE, "render-cache")
        for role, directive in (("http", "ReadWritePaths"), ("relay", "ReadOnlyPaths"), ("worker", None)):
            unit = services.render_unit(inventory, role, entry, DIGEST)
            self.assertIn('TemporaryFileSystem="' + c.shared_directory_root(INSTANCE) + ':ro"', unit)
            self.assertIn("ProtectSystem=strict", unit)
            if directive:
                self.assertIn(directive + '="' + path + '"', unit)
                self.assertIn(("BindPaths" if role == "http" else "BindReadOnlyPaths") + '="' + path + '"', unit)
            else:
                self.assertNotIn(path, unit)
            if role == "relay":
                self.assertIn('Environment="RENDER_CACHE=' + path + '"', unit)
        self.assertNotIn("render-cache", c.record_document(inventory.specs["worker"].bindings))

    def test_backup_excludes_disposable_files_and_inventory_rejects_tampering(self):
        inventory, _ = shared_inventory()
        coverage = backup.service_coverage(inventory)
        directory, = coverage["shared_directories"]
        self.assertEqual(directory["state"], "disposable")
        self.assertFalse(directory["backed_up"] or directory["restored"])
        self.assertNotIn("shared_directories", coverage["supported_components"])
        spec = inventory.specs["relay"]
        bad = replace(spec, bindings=replace(spec.bindings, shared_directories=()))
        deployment = replace(inventory.deployment, units=tuple(bad if u == spec else u for u in inventory.deployment.units))
        resolution = replace(inventory.resolution, deployment_sha256=c.canonical_digest(c.record_document(deployment)))
        with self.assertRaises(c.DeployError):
            services.UnitInventory(inventory.name, inventory.topology, inventory.profile, deployment, resolution)


class SharedPreparationTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.host = Host(Paths(Path(self.temp.name)))
        self.topology = shared_inventory()[0].topology
        self.path = self.host.paths.p(c.shared_directory_path(INSTANCE, "render-cache"))
        self.uid, self.gid = os.geteuid(), os.getegid()
        self.record = {"target": str(self.path), "source": shared._source(INSTANCE, "render-cache"),
                       "fstype": "tmpfs", "options": "rw,nosuid,nodev,noexec"}
        self.capacity = SimpleNamespace(f_blocks=256, f_frsize=4096, f_files=256, f_bfree=255)

    def prepare(self):
        shared.prepare(self.host, self.topology, INSTANCE, self.uid, self.gid)

    def test_mount_create_verify_and_idempotent_reuse(self):
        with patch.object(shared, "_mount", side_effect=[None, self.record]), \
                patch.object(self.host, "run", return_value=subprocess.CompletedProcess([], 0)), \
                patch.object(os, "fstatvfs", return_value=self.capacity):
            self.prepare()
            command = self.host.run.call_args.args[0]
            self.assertEqual(command[:3], ["mount", "-t", "tmpfs"])
            self.assertIn("size=1048576,nr_inodes=256", command[4])
        with patch.object(shared, "_mount", return_value=self.record), patch.object(self.host, "run") as run, \
                patch.object(os, "statvfs", return_value=self.capacity), patch.object(os, "fstatvfs", return_value=self.capacity):
            self.prepare()
            run.assert_not_called()

    def test_symlink_occupied_and_foreign_mount_refuse(self):
        self.path.parent.mkdir(parents=True)
        self.path.symlink_to(self.temp.name)
        with self.assertRaises(c.DeployError), patch.object(self.host, "run") as run:
            self.prepare()
        run.assert_not_called()
        self.path.unlink()
        self.path.mkdir(mode=0o700)
        (self.path / "keep").write_text("do not overlay")
        with patch.object(shared, "_mount", return_value=None), self.assertRaises(c.DeployError):
            self.prepare()
        self.assertEqual((self.path / "keep").read_text(), "do not overlay")
        for record in ({**self.record, "source": "foreign"}, {**self.record, "fstype": "ext4"},
                       {**self.record, "options": "rw,nodev,nosuid"}):
            with patch.object(shared, "_mount", return_value=record), self.assertRaises(c.DeployError):
                self.prepare()

    def test_report_checks_actual_limits_and_permissions(self):
        self.path.mkdir(parents=True, mode=0o700)
        with patch.object(shared, "_mount", return_value=self.record), patch.object(os, "fstatvfs", return_value=self.capacity):
            report, = shared.report(self.host, self.topology, INSTANCE, self.uid, self.gid)
            self.assertEqual((report["status"], report["used_bytes"]), ("verified", 4096))
            self.path.chmod(0o755)
            report, = shared.report(self.host, self.topology, INSTANCE, self.uid, self.gid)
            self.assertEqual(report["finding"], "shared_directory_unverified")
            self.path.chmod(0o700)
            self.capacity.f_blocks *= 2
            report, = shared.report(self.host, self.topology, INSTANCE, self.uid, self.gid)
            self.assertEqual(report["finding"], "shared_directory_unverified")

    def test_discard_busy_mount_retains_files_and_replays_empty_cleanup(self):
        self.path.mkdir(parents=True, mode=0o700)
        (self.path / "keep").write_text("retained")
        with patch.object(shared, "_mount", return_value=self.record), \
                patch.object(self.host, "run", side_effect=c.DeployError("busy", "busy")), self.assertRaises(c.DeployError):
            shared.discard(self.host, self.topology, INSTANCE, self.uid, self.gid)
        self.assertTrue((self.path / "keep").exists())
        (self.path / "keep").unlink()
        with patch.object(shared, "_mount", return_value=None):
            shared.discard(self.host, self.topology, INSTANCE, self.uid, self.gid)
            shared.discard(self.host, self.topology, INSTANCE, self.uid, self.gid)
        self.assertFalse(self.path.exists())


if __name__ == "__main__":
    unittest.main()
