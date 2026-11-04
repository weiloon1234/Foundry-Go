"""Tool upgrade fault coverage uses an isolated filesystem, never host services."""
import copy
from contextlib import nullcontext
import json
import os
import subprocess
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch
import uuid

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "lib"))
from contracts import DeployError, HOST_PATHS
from host import Paths
import upgrades


class UpgradeHost:
    def __init__(self, root, registry):
        self.paths = Paths(root)
        self.registry = copy.deepcopy(registry)

    def read_registry(self, missing=False):
        return copy.deepcopy(self.registry)

    def write_registry(self, registry):
        self.registry = copy.deepcopy(registry)

    def check_control_directory(self, directory):
        return directory

    def lock(self, name):
        return nullcontext()


class UpgradeTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name).resolve()
        self.source = self.root / "source"
        for name in upgrades.REQUIRED_FILES:
            path = self.source / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text("# test bundle\n")
        self.metadata = {"protocol": {"major": 2, "minor": 0}, "generation": 1,
                         "minimum_host_generation": 1, "minimum_client_generation": 1,
                         "framework_version": "v0.1.0"}
        (self.source / "protocol.json").write_text(json.dumps(self.metadata))
        (self.source / "server/launcher.py").write_text(
            (Path(__file__).resolve().parents[1] / "server/launcher.py").read_text())
        self.digest = upgrades.tool_bundle(self.source)[2]
        self.host = UpgradeHost(self.root / "host", {
            "host_id": str(uuid.uuid4()), "revision": 1, "instances": {}, "updated_at": "2026-11-01T00:00:00Z",
            "tools": upgrades._active_record(self.source, self.digest, None)})

    def install(self, **kwargs):
        return upgrades.install_tools(self.host, self.host.read_registry(), self.source, **kwargs)

    def test_fresh_install_is_immutable_and_unchanged_retry_keeps_revision(self):
        self.assertEqual("succeeded", self.install()["outcome"])
        destination = self.host.paths.tools / "tools" / self.digest
        self.assertTrue((destination / "manifest.json").is_file())
        self.assertEqual("tools/" + self.digest, os.readlink(self.host.paths.tools / "current"))
        self.assertEqual(0o644, (destination / "lib/contracts.py").stat().st_mode & 0o777)
        launcher = self.host.paths.p(HOST_PATHS["launcher"])
        self.assertEqual(0o755, launcher.stat().st_mode & 0o777)
        self.assertNotIn("@TOOLS_ROOT@", launcher.read_text())
        before = self.host.read_registry()
        self.assertEqual("unchanged", self.install()["outcome"])
        self.assertEqual(before, self.host.read_registry())

    def test_generation_two_without_recovery_upgrades_through_normal_plan_and_apply(self):
        (self.source / "lib/recover.py").unlink()
        self.metadata["generation"] = 2
        (self.source / "protocol.json").write_text(json.dumps(self.metadata))
        previous = upgrades.tool_bundle(self.source)[2]
        self.host.registry["tools"] = upgrades._active_record(self.source, previous, None)
        self.install()
        self.metadata["generation"] = 3
        (self.source / "protocol.json").write_text(json.dumps(self.metadata))
        with self.assertRaises(DeployError) as error:
            upgrades.plan_upgrade(self.host, self.source)
        self.assertEqual(error.exception.code, "tools_incomplete")
        (self.source / "lib/recover.py").write_text("# recovery implementation\n")
        plan = upgrades.plan_upgrade(self.host, self.source)
        self.assertIn("lib/recover.py", plan["upgrade"]["changed_paths"])
        result = upgrades.apply_upgrade(self.host, self.source, plan["plan_token"], str(uuid.uuid4()))
        self.assertEqual(result["outcome"], "succeeded")
        self.assertEqual(self.host.read_registry()["tools"]["generation"], 3)
        self.assertTrue((self.host.paths.tools / "tools" / previous).is_dir())

    def test_bundle_is_traversable_under_a_hardened_umask(self):
        previous = os.umask(0o077)
        try:
            self.assertEqual("succeeded", self.install()["outcome"])
        finally:
            os.umask(previous)
        destination = self.host.paths.tools / "tools" / self.digest
        directories = [self.host.paths.tools, destination.parent, destination,
                       *[path for path in destination.rglob("*") if path.is_dir()]]
        for directory in directories:
            self.assertEqual(0o755, directory.stat().st_mode & 0o777, directory)

    def test_bundle_digest_binds_paths_content_and_excludes_bytecode(self):
        (self.source / "lib/__pycache__").mkdir()
        (self.source / "lib/__pycache__/untrusted.pyc").write_bytes(b"ignored")
        self.assertEqual(self.digest, upgrades.tool_bundle(self.source)[2])
        (self.source / "lib/additional.py").write_text("# new file\n")
        self.assertNotEqual(self.digest, upgrades.tool_bundle(self.source)[2])

    def test_incomplete_integration_bundle_refuses_before_install(self):
        for name in ("lib/backup.py", "lib/cloudflare-ranges.json",
                     "server/templates/foundry-worker@.service",
                     "server/templates/foundry-backup.timer"):
            with self.subTest(name=name):
                path = self.source / name
                content = path.read_bytes()
                path.unlink()
                try:
                    with self.assertRaises(DeployError) as caught:
                        self.install()
                    self.assertEqual(caught.exception.code, "tools_incomplete")
                    self.assertFalse(self.host.paths.tools.exists())
                finally:
                    path.write_bytes(content)

    def test_partial_copy_resumes_only_same_digest(self):
        original = upgrades.atomic_write

        def interrupted(path, data, mode=0o644):
            if str(path).endswith("lib/host.py"):
                raise OSError("injected interruption")
            return original(path, data, mode)

        with patch.object(upgrades, "atomic_write", side_effect=interrupted):
            with self.assertRaises(OSError):
                self.install()
        operation = self.host.registry["tools"]["upgrade_operation"]
        self.assertIsNotNone(operation)
        self.assertEqual(self.host.registry["host_id"], upgrades.hello(self.host)["host_id"])
        self.assertEqual("succeeded", self.install(allow_upgrade=True, operation_id=operation)["outcome"])
        self.assertIsNone(self.host.registry["tools"]["upgrade_operation"])

    def test_selection_interruption_stays_gated_and_resumes_forward(self):
        self.install()
        (self.source / "lib/new_module.py").write_text("# new implementation\n")
        plan = upgrades.plan_upgrade(self.host, self.source)
        operation = str(uuid.uuid4())
        with patch.object(upgrades, "_select", side_effect=OSError("injected interruption")):
            with self.assertRaises(OSError):
                upgrades.apply_upgrade(self.host, self.source, plan["plan_token"], operation)
        request = {"protocol": self.metadata["protocol"], "client": {"generation": 1}}
        with self.assertRaises(DeployError) as caught:
            upgrades.check_client(self.host, request, "activate")
        self.assertEqual("tools_upgrade_pending", caught.exception.code)
        result = upgrades.apply_upgrade(self.host, self.source, plan["plan_token"], operation)
        self.assertEqual("succeeded", result["outcome"])
        self.assertTrue((self.host.paths.tools / "tools" / self.digest).is_dir())
        self.assertIsNone(self.host.registry["tools"]["upgrade_operation"])
        self.assertEqual("unchanged", upgrades.apply_upgrade(self.host, self.source, plan["plan_token"], operation)["outcome"])

    def test_completed_receipt_with_pending_registry_resumes(self):
        original = self.host.write_registry

        def interrupted(registry):
            if registry["tools"]["upgrade_operation"] is None:
                raise OSError("injected final publication failure")
            original(registry)

        with patch.object(self.host, "write_registry", side_effect=interrupted):
            with self.assertRaises(OSError):
                self.install()
        operation = self.host.registry["tools"]["upgrade_operation"]
        self.assertEqual("unchanged", self.install(allow_upgrade=True, operation_id=operation)["outcome"])
        self.assertIsNone(self.host.registry["tools"]["upgrade_operation"])

    def test_pending_upgrade_rejects_another_target_digest(self):
        with patch.object(upgrades, "_select", side_effect=OSError("injected interruption")):
            with self.assertRaises(OSError):
                self.install()
        (self.source / "lib/new_module.py").write_text("# different candidate\n")
        with self.assertRaises(DeployError) as caught:
            self.install(allow_upgrade=True)
        self.assertEqual("tools_upgrade_pending", caught.exception.code)

    def test_installed_file_drift_and_mode_drift_refuse(self):
        self.install()
        path = self.host.paths.tools / "tools" / self.digest / "lib/host.py"
        path.chmod(0o600)
        with self.assertRaises(DeployError) as caught:
            self.install()
        self.assertEqual("tools_ownership", caught.exception.code)
        path.chmod(0o644)
        path.write_text("# changed installed bytes\n")
        with self.assertRaises(DeployError) as caught:
            self.install()
        self.assertEqual("tools_version_conflict", caught.exception.code)

    def test_archive_reload_effect_and_token_bind_the_shared_comparison(self):
        import backup
        import postgres
        self.install()
        (self.source / "lib/new_module.py").write_text("# change\n")
        cluster = {"unit": "foundry-postgresql.service", "archive_command": backup._archive_command(),
                   "archive_timeout": postgres.ARCHIVE_TIMEOUT, "config_path": "/etc/postgresql/16/foundry"}
        hba = self.host.paths.p(cluster["config_path"]) / "pg_hba.conf"
        hba.parent.mkdir(parents=True)
        hba.write_text(postgres._hba(self.host.registry))
        journal = {"hba": {"phase": "published"}}
        journal_patch = patch.object(postgres, "_cluster_journal", return_value=journal)
        journal_patch.start()
        self.addCleanup(journal_patch.stop)
        self.host.registry["shared"] = {"postgresql": cluster}
        self.assertEqual([], upgrades.plan_upgrade(self.host, self.source)["upgrade"]["service_effects"])
        self.host.registry["shared"]["backup"] = {"configured": True}
        self.assertEqual([], upgrades.plan_upgrade(self.host, self.source)["upgrade"]["service_effects"])
        for changed in (dict(cluster, archive_command="old"), {k: v for k, v in cluster.items() if k != "archive_timeout"}):
            self.host.registry["shared"]["postgresql"] = changed
            plan = upgrades.plan_upgrade(self.host, self.source)
            self.assertEqual([{"service": "postgresql", "unit": cluster["unit"], "action": "reload"}],
                             plan["upgrade"]["service_effects"])
            with patch.object(upgrades, "install_tools", return_value={"outcome": "succeeded"}) as install:
                upgrades.apply_upgrade(self.host, self.source, plan["plan_token"], str(uuid.uuid4()))
                install.assert_called_once()
            self.host.registry["shared"]["postgresql"] = cluster
            with self.assertRaises(DeployError) as caught:
                upgrades.apply_upgrade(self.host, self.source, plan["plan_token"], str(uuid.uuid4()))
            self.assertEqual("plan_changed", caught.exception.code)

    def test_hba_reload_effect_is_bound_to_plan_token(self):
        import backup
        import postgres
        self.install()
        (self.source / "lib/new_module.py").write_text("# change\n")
        cluster = {"unit": "postgresql@16-foundry.service", "config_path": "/etc/postgresql/16/foundry",
                   "archive_command": backup._archive_command(), "archive_timeout": postgres.ARCHIVE_TIMEOUT}
        self.host.registry["shared"] = {"postgresql": cluster, "backup": {"configured": True}}
        path = self.host.paths.p(cluster["config_path"]) / "pg_hba.conf"
        path.parent.mkdir(parents=True)
        content = postgres._hba(self.host.registry)
        journal = {"hba": {"phase": "published"}}
        with patch.object(postgres, "_cluster_journal", return_value=journal):
            for phase, current in (("published", "old hba"), ("publishing", content)):
                journal["hba"]["phase"] = phase
                path.write_text(current)
                plan = upgrades.plan_upgrade(self.host, self.source)
                self.assertEqual([{"service": "postgresql", "unit": cluster["unit"], "action": "reload"}],
                                 plan["upgrade"]["service_effects"])
                with patch.object(upgrades, "install_tools", return_value={"outcome": "succeeded"}) as install:
                    upgrades.apply_upgrade(self.host, self.source, plan["plan_token"], str(uuid.uuid4()))
                    install.assert_called_once()
                path.write_text(content)
                journal["hba"]["phase"] = "published"
                self.assertEqual([], upgrades.plan_upgrade(self.host, self.source)["upgrade"]["service_effects"])
                with self.assertRaises(DeployError) as caught:
                    upgrades.apply_upgrade(self.host, self.source, plan["plan_token"], str(uuid.uuid4()))
                self.assertEqual("plan_changed", caught.exception.code)

    def test_plan_revision_drift_is_rejected(self):
        self.install()
        (self.source / "lib/new_module.py").write_text("# change\n")
        plan = upgrades.plan_upgrade(self.host, self.source)
        self.host.registry["revision"] += 1
        with self.assertRaises(DeployError) as caught:
            upgrades.apply_upgrade(self.host, self.source, plan["plan_token"], str(uuid.uuid4()))
        self.assertEqual("plan_changed", caught.exception.code)

    def test_protocol_errors_allow_minimal_hello(self):
        self.install()
        for generation, code in [(2, "host_upgrade_required"), (0, "client_too_old")]:
            request = {"protocol": {"major": 2, "minor": 0}, "client": {"generation": generation}}
            with self.assertRaises(DeployError) as caught:
                upgrades.check_client(self.host, request, "activate")
            self.assertEqual(code, caught.exception.code)
            upgrades.check_client(self.host, request, "hello")
        self.assertEqual({"minimum": 1, "maximum": 1}, upgrades.hello(self.host)["client_generation"])

    def test_retired_bundle_cannot_be_selected_again(self):
        self.install()
        change = self.source / "lib/new_module.py"
        change.write_text("# change\n")
        self.install(allow_upgrade=True)
        change.unlink()
        with self.assertRaises(DeployError) as caught:
            self.install(allow_upgrade=True)
        self.assertEqual("tools_downgrade", caught.exception.code)

    def test_upgrade_stays_pending_until_new_bundle_rebinds_backup_commands(self):
        self.install()
        self.host.registry["shared"] = {"backup": {"owner": "foundry"},
                                         "postgresql": {"unit": "foundry-postgresql.service"}}
        (self.source / "lib/new_module.py").write_text("# updated bundle\n")
        operation = str(uuid.uuid4())
        calls = []
        def command(args, **kwargs):
            calls.append(args)
            if len(calls) == 1:
                raise DeployError("backup_rebind_failed", "loaded command is old")
            return subprocess.CompletedProcess(args, 0, "", "")
        with patch.object(self.host, "run", side_effect=command, create=True):
            with self.assertRaises(DeployError):
                self.install(allow_upgrade=True, operation_id=operation)
            self.assertEqual(self.host.registry["tools"]["upgrade_operation"], operation)
            result = self.install(allow_upgrade=True, operation_id=operation)
        self.assertEqual(result["outcome"], "succeeded")
        self.assertEqual(calls[0], calls[1])
        self.assertEqual(calls[0][-1], "rebind")
        self.assertIn(self.host.registry["tools"]["bundle_sha256"], calls[0][-2])
        self.assertIsNone(self.host.registry["tools"]["upgrade_operation"])


if __name__ == "__main__":
    unittest.main()
