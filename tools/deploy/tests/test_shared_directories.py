"""Shared cache declarations, delivery and mount refusal boundaries (no mounts)."""
from dataclasses import replace
import copy
import errno
import os
from pathlib import Path
import stat
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


def shared_inventory(*, writers=("http",), readers=("relay",)):
    old, entry, _ = inventory_fixture()
    raw = old.topology.projection()
    raw["shared_directories"] = {"render-cache": {"state": "disposable", "max_bytes": 1048576,
                                                "writers": list(writers), "readers": list(readers)}}
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
                self.assertEqual(unit.count("PrivateUsers=yes\n"), 1)
                self.assertIn(directive + '="' + path + '"', unit)
                self.assertIn(("BindPaths" if role == "http" else "BindReadOnlyPaths") + '="' + path + '"', unit)
            else:
                self.assertNotIn("PrivateUsers=", unit)
                self.assertNotIn(path, unit)
            if role == "relay":
                self.assertIn('Environment="RENDER_CACHE=' + path + '"', unit)
        self.assertNotIn("render-cache", c.record_document(inventory.specs["worker"].bindings))

    def test_user_namespace_policy_covers_both_access_modes_and_role_kinds(self):
        for writers, readers in ((["http"], ["relay"]), (["relay"], ["http"])):
            inventory, entry = shared_inventory(writers=writers, readers=readers)
            for name in ("http", "relay"):
                with self.subTest(writer=writers[0], role=name):
                    unit = services.render_unit(inventory, name, entry, DIGEST)
                    self.assertEqual(unit.count("PrivateUsers=yes\n"), 1)
                    self.assertIn("CapabilityBoundingSet=\n", unit)
                    self.assertIn("ExecStartPre=+", unit)
                    self.assertNotIn("ExecStart=+", unit)
            self.assertNotIn("PrivateUsers=", services.render_unit(inventory, "worker", entry, DIGEST))
        old, entry, _ = inventory_fixture()
        for name in old.roles:
            self.assertNotIn("PrivateUsers=", services.render_unit(old, name, entry, DIGEST))

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

    def test_user_namespace_keeps_cgroup_ip_filters_and_privileged_preparation(self):
        import recovery_rehearsal
        inventory, entry = shared_inventory()
        inventory.roles["relay"] = replace(inventory.roles["relay"], recovery_rehearsal_safe=True)
        with patch.object(recovery_rehearsal, "active", return_value=True):
            unit = services.render_unit(inventory, "relay", entry, DIGEST)
        self.assertIn("PrivateUsers=yes\n", unit)
        self.assertIn(recovery_rehearsal.FILTER, unit)
        self.assertIn("ExecStartPre=+", unit)
        self.assertIn("ExecCondition=+", unit)


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
            parent = self.path.parent.lstat()
            self.assertEqual((parent.st_uid, parent.st_gid, stat.S_IMODE(parent.st_mode)),
                             (self.uid, self.gid, 0o700))
        with patch.object(shared, "_mount", return_value=self.record), patch.object(self.host, "run") as run, \
                patch.object(os, "statvfs", return_value=self.capacity), patch.object(os, "fstatvfs", return_value=self.capacity):
            self.prepare()
            run.assert_not_called()

    def test_preparation_tightens_systemd_parent_before_mounting(self):
        self.path.parent.mkdir(parents=True)
        self.path.parent.chmod(0o755)
        def mount(args, **kwargs):
            self.assertEqual(args[:3], ["mount", "-t", "tmpfs"])
            self.assertEqual(stat.S_IMODE(self.path.parent.stat().st_mode), 0o700)
            return subprocess.CompletedProcess(args, 0)
        with patch.object(shared, "_mount", side_effect=[None, self.record]), \
                patch.object(self.host, "run", side_effect=mount) as run, \
                patch.object(os, "fstatvfs", return_value=self.capacity):
            self.prepare()
        run.assert_called_once()
        self.assertEqual(stat.S_IMODE(self.path.parent.stat().st_mode), 0o700)

    def test_discard_tightens_empty_systemd_parent_after_refused_admission(self):
        self.path.parent.mkdir(parents=True)
        self.path.parent.chmod(0o755)
        with patch.object(self.host, "run") as run:
            shared.discard(self.host, self.topology, INSTANCE, self.uid, self.gid)
        run.assert_not_called()
        self.assertEqual(stat.S_IMODE(self.path.parent.stat().st_mode), 0o700)
        self.assertFalse(self.path.exists())

    def test_discard_tightens_parent_before_unmounting(self):
        self.path.mkdir(parents=True, mode=0o700)
        self.path.parent.chmod(0o755)
        def unmount(args, **kwargs):
            self.assertEqual(args, ["umount", str(self.path)])
            self.assertEqual(stat.S_IMODE(self.path.parent.stat().st_mode), 0o700)
            return subprocess.CompletedProcess(args, 0)
        with patch.object(shared, "_mount", side_effect=[self.record, None]), \
                patch.object(self.host, "run", side_effect=unmount) as run:
            shared.discard(self.host, self.topology, INSTANCE, self.uid, self.gid)
        run.assert_called_once()
        self.assertFalse(self.path.exists())

    def test_tightening_is_reverified_before_any_mount_operation(self):
        self.path.parent.mkdir(parents=True)
        self.path.parent.chmod(0o755)
        for action in (self.prepare, lambda: shared.discard(self.host, self.topology, INSTANCE, self.uid, self.gid)):
            with patch.object(os, "fchmod"), patch.object(self.host, "run") as run, self.assertRaises(c.DeployError):
                action()
            run.assert_not_called()

    def test_parent_symlink_and_foreign_owner_are_not_adopted(self):
        self.path.parent.parent.mkdir(parents=True)
        self.path.parent.symlink_to(self.temp.name)
        actions = (self.prepare, lambda: shared.discard(self.host, self.topology, INSTANCE, self.uid, self.gid))
        for action in actions:
            with patch.object(self.host, "run") as run, patch.object(os, "fchmod") as chmod, self.assertRaises(c.DeployError):
                action()
            run.assert_not_called()
            chmod.assert_not_called()
        self.path.parent.unlink()
        self.path.parent.mkdir(mode=0o700)
        for uid, gid in ((self.uid + 1, self.gid), (self.uid, self.gid + 1)):
            info = SimpleNamespace(st_uid=uid, st_gid=gid, st_mode=stat.S_IFDIR | 0o755)
            for action in actions:
                with self.subTest(uid=uid, gid=gid), patch.object(os, "fstat", return_value=info), \
                        patch.object(os, "fchmod") as chmod, patch.object(self.host, "run") as run, self.assertRaises(c.DeployError):
                    action()
                run.assert_not_called()
                chmod.assert_not_called()

    def test_other_parent_modes_and_file_type_are_refused(self):
        self.path.parent.mkdir(parents=True)
        for mode in (0o775, 0o750, 0o711):
            self.path.parent.chmod(mode)
            for action in (self.prepare, lambda: shared.discard(self.host, self.topology, INSTANCE, self.uid, self.gid)):
                with self.subTest(mode=mode), patch.object(os, "fchmod") as chmod, \
                        patch.object(self.host, "run") as run, self.assertRaises(c.DeployError):
                    action()
                run.assert_not_called()
                chmod.assert_not_called()
            report, = shared.report(self.host, self.topology, INSTANCE, self.uid, self.gid)
            self.assertEqual(report["finding"], "shared_directory_unverified")
            self.assertEqual(stat.S_IMODE(self.path.parent.stat().st_mode), mode)
        self.path.parent.rmdir()
        self.path.parent.write_text("not a directory")
        for action in (self.prepare, lambda: shared.discard(self.host, self.topology, INSTANCE, self.uid, self.gid)):
            with patch.object(self.host, "run") as run, self.assertRaises(c.DeployError):
                action()
            run.assert_not_called()
        self.assertEqual(self.path.parent.read_text(), "not a directory")

    def test_empty_systemd_parent_reports_not_prepared_without_modification(self):
        self.path.parent.mkdir(parents=True)
        self.path.parent.chmod(0o755)
        with patch.object(shared, "_mount", return_value=None) as mount, patch.object(os, "fchmod") as chmod:
            report, = shared.report(self.host, self.topology, INSTANCE, self.uid, self.gid)
        mount.assert_called_once_with(self.host, self.path.parent)
        chmod.assert_not_called()
        self.assertEqual(report["status"], "not_prepared")
        self.assertNotIn("finding", report)
        self.assertNotIn("used_bytes", report)
        self.assertEqual(stat.S_IMODE(self.path.parent.stat().st_mode), 0o755)
        self.assertEqual(list(self.path.parent.iterdir()), [])

    def test_empty_systemd_parent_with_a_mount_is_unverified(self):
        self.path.parent.mkdir(parents=True)
        self.path.parent.chmod(0o755)
        with patch.object(shared, "_mount", return_value={"fstype": "tmpfs"}), patch.object(os, "fchmod") as chmod:
            report, = shared.report(self.host, self.topology, INSTANCE, self.uid, self.gid)
        chmod.assert_not_called()
        self.assertEqual(report["finding"], "shared_directory_unverified")
        self.assertEqual(stat.S_IMODE(self.path.parent.stat().st_mode), 0o755)

    def test_systemd_parent_with_content_is_reported_unverified(self):
        self.path.mkdir(parents=True, mode=0o700)
        self.path.parent.chmod(0o755)
        (self.path / "keep").write_text("retained")
        with patch.object(self.host, "run") as run:
            report, = shared.report(self.host, self.topology, INSTANCE, self.uid, self.gid)
            self.assertEqual(report["finding"], "shared_directory_unverified")
        run.assert_not_called()
        self.assertEqual(stat.S_IMODE(self.path.parent.stat().st_mode), 0o755)
        self.assertEqual((self.path / "keep").read_text(), "retained")

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
        self.path.parent.chmod(0o700)
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

    def test_unprivileged_report_keeps_coverage_without_claiming_usage(self):
        self.path.mkdir(parents=True, mode=0o700)
        self.path.parent.chmod(0o700)
        with patch.object(os, "open", side_effect=PermissionError(errno.EACCES, "root-only parent")), \
                patch.object(self.host, "run") as run:
            report, = shared.report(self.host, self.topology, INSTANCE, self.uid, self.gid)
        run.assert_not_called()
        self.assertEqual(report["finding"], "shared_directory_unverified")
        self.assertEqual(report["state"], "disposable")
        self.assertFalse(report["backed_up"] or report["restored"])
        self.assertNotIn("used_bytes", report)

    def test_discard_busy_mount_retains_files_and_replays_empty_cleanup(self):
        self.path.mkdir(parents=True, mode=0o700)
        self.path.parent.chmod(0o700)
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

    def test_discard_after_reboot_does_not_create_a_parent(self):
        with patch.object(self.host, "run") as run:
            shared.discard(self.host, self.topology, INSTANCE, self.uid, self.gid)
        run.assert_not_called()
        self.assertFalse(self.path.parent.exists())


class ProcfsAcceptanceTests(unittest.TestCase):
    def setUp(self):
        import acceptance_feature
        self.scope = {"__name__": "procfs_acceptance_test"}
        exec(compile(acceptance_feature.PROCFS_PROBE, "procfs-acceptance", "exec"), self.scope)
        self.probe = acceptance_feature.procfs_targets(42, "/run/foundry/cache", "/var/lib/foundry/apps/example")

    def test_host_path_requires_eacces_for_both_modes(self):
        with patch.object(os, "open", side_effect=PermissionError(errno.EACCES, "denied")) as opened:
            self.scope["assert_host_denied"]("/run/foundry/cache")
        self.assertEqual([call.args for call in opened.call_args_list],
                         [("/run/foundry/cache/frame.txt", mode) for mode in (os.O_RDONLY, os.O_WRONLY)])
        for code in (errno.ENOENT, errno.ESRCH, errno.EROFS, errno.EPERM):
            with self.subTest(code=code), patch.object(os, "open", side_effect=OSError(code, "wrong denial")), \
                    self.assertRaises(AssertionError):
                self.scope["assert_host_denied"]("/run/foundry/cache")
        for sequence in ([11], [PermissionError(errno.EACCES, "denied"), 11]):
            with patch.object(os, "open", side_effect=sequence), patch.object(os, "close") as closed, \
                    self.assertRaises(AssertionError):
                self.scope["assert_host_denied"]("/run/foundry/cache")
            closed.assert_called_once_with(11)

    def test_probe_keeps_procfs_cwd_traversal_and_checks_both_open_modes(self):
        self.assertEqual(self.probe["paths"], ["/proc/42/root/run/foundry/cache/frame.txt",
            "/proc/42/cwd/../../../../../run/foundry/cache/frame.txt"])
        self.assertEqual(self.probe["links"], ["/proc/42/root", "/proc/42/cwd", "/proc/42/fd/1"])
        with patch.object(os, "open", side_effect=PermissionError(errno.EACCES, "denied")) as opened, \
                patch.object(os, "readlink", side_effect=PermissionError(errno.EPERM, "denied")) as readlink:
            self.scope["assert_denied"](self.probe)
        self.assertEqual([(call.args[0], call.args[1]) for call in opened.call_args_list],
                         [(path, mode) for path in self.probe["paths"] for mode in (os.O_RDONLY, os.O_WRONLY)])
        self.assertEqual([call.args[0] for call in readlink.call_args_list], self.probe["links"])

    def test_missing_paths_read_only_mounts_and_success_never_prove_denial(self):
        for code in (errno.ENOENT, errno.ESRCH, errno.EROFS):
            with self.subTest(code=code), patch.object(os, "open", side_effect=OSError(code, "not permission")), \
                    self.assertRaises(AssertionError):
                self.scope["assert_denied"](self.probe)
        for sequence in ([11], [PermissionError(errno.EACCES, "denied"), 11]):
            with patch.object(os, "open", side_effect=sequence), patch.object(os, "close") as closed, \
                    self.assertRaises(AssertionError):
                self.scope["assert_denied"](self.probe)
            closed.assert_called_once_with(11)
        with patch.object(os, "open", side_effect=PermissionError(errno.EACCES, "denied")), \
                patch.object(os, "readlink", return_value="/"), self.assertRaises(AssertionError):
            self.scope["assert_denied"](self.probe)


if __name__ == "__main__":
    unittest.main()
