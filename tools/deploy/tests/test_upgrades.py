"""Tool upgrade fault coverage uses an isolated filesystem, never host services."""
import copy
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
from host import Host, Paths
import upgrades


class UpgradeHost(Host):
    def __init__(self, root, registry):
        super().__init__(Paths(root))
        self.registry = copy.deepcopy(registry)

    def read_registry(self, missing=False):
        return copy.deepcopy(self.registry)

    def write_registry(self, registry):
        self.registry = copy.deepcopy(registry)

    def run(self, args, **kwargs):
        if len(args) > 3 and args[3] == 'prune':
            assert args[2] == str(self.paths.tools / 'tools' / self.registry['tools']['bundle_sha256'] / 'lib/upgrades.py')
            assert kwargs['pass_fds'] == (self.host_lock_fd,)
            return subprocess.CompletedProcess(args, 0, json.dumps(upgrades._prune_bundles(self)), '')
        raise AssertionError('Unexpected host command')


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
        self.metadata = {"protocol": {"major": 2, "minor": 0}, "generation": 9,
                         "minimum_host_generation": 9, "minimum_client_generation": 1,
                         "framework_version": "v0.23.2"}
        (self.source / "protocol.json").write_text(json.dumps(self.metadata))
        (self.source / "server/launcher.py").write_text(
            (Path(__file__).resolve().parents[1] / "server/launcher.py").read_text())
        self.digest = upgrades.tool_bundle(self.source)[2]
        self.host = UpgradeHost(self.root / "host", {
            "host_id": str(uuid.uuid4()), "revision": 1, "instances": {}, "updated_at": "2026-11-01T00:00:00Z",
            "tools": upgrades._active_record(self.source, self.digest, None)})

    def install(self, **kwargs):
        with self.host.lock("host"):
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
        self.assertFalse((self.host.paths.tools / "tools" / self.digest).exists())
        self.assertEqual([self.digest], result["removed_bundles"])
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

    def test_newer_client_refuses_host_but_allows_hello(self):
        self.install()
        for generation, code in [(10, "host_upgrade_required"), (0, "client_too_old")]:
            request = {"protocol": {"major": 2, "minor": 0}, "client": {"generation": generation}}
            with self.assertRaises(DeployError) as caught:
                upgrades.check_client(self.host, request, "activate")
            self.assertEqual(code, caught.exception.code)
            upgrades.check_client(self.host, request, "hello")
        self.assertEqual({"minimum": 1, "maximum": 9}, upgrades.hello(self.host)["client_generation"])

    def test_generation_nine_upgrades_to_current(self):
        self.assert_upgrade_to_current()

    def test_generation_ten_upgrades_to_current(self):
        self.metadata["generation"] = 10
        (self.source / "protocol.json").write_text(json.dumps(self.metadata))
        self.host.registry["tools"] = upgrades._active_record(self.source, upgrades.tool_bundle(self.source)[2], None)
        self.assert_upgrade_to_current()

    def assert_upgrade_to_current(self):
        actual = Path(__file__).resolve().parents[1]
        metadata = upgrades.protocol(actual)
        self.assertGreater(metadata["generation"], 10)
        self.assertEqual(9, metadata["minimum_host_generation"])
        self.assertEqual(1, metadata["minimum_client_generation"])
        _, files, _ = upgrades.tool_bundle(actual)
        self.assertIn(actual / "lib/maintenance.py", files)
        self.install()
        self.metadata['generation'] = metadata['generation']
        (self.source / 'protocol.json').write_text(json.dumps(self.metadata))
        (self.source / "lib/maintenance.py").write_text("# current module\n")
        plan = upgrades.plan_upgrade(self.host, self.source)
        result = upgrades.apply_upgrade(self.host, self.source, plan["plan_token"], str(uuid.uuid4()))
        self.assertEqual("succeeded", result["outcome"])
        self.assertEqual(metadata["generation"], self.host.read_registry()["tools"]["generation"])
        self.assertEqual({"minimum": 1, "maximum": metadata["generation"]}, upgrades.hello(self.host)["client_generation"])
        for old_generation in (9, 10):
            self.metadata["generation"] = old_generation
            (self.source / "protocol.json").write_text(json.dumps(self.metadata))
            with self.assertRaises(DeployError) as caught:
                upgrades.plan_upgrade(self.host, self.source)
            self.assertEqual(caught.exception.code, "tools_downgrade")

    def test_generation_eight_refuses_planning_and_installation_before_writes(self):
        self.install()
        self.host.registry['tools']['generation'] = 8
        before = self.host.read_registry()
        for action in (lambda: upgrades.plan_upgrade(self.host, self.source),
                       lambda: self.install(allow_upgrade=True)):
            with self.assertRaises(DeployError) as caught:
                action()
            self.assertEqual('tools_upgrade_floor', caught.exception.code)
            self.assertIn('must already run generation 9 tools', caught.exception.detail)
            self.assertEqual(before, self.host.read_registry())

    def test_candidate_python_inventory_contains_only_runtime_modules(self):
        source = Path(__file__).resolve().parents[1]
        _, files, _ = upgrades.tool_bundle(source)
        expected = {name for name in upgrades.REQUIRED_FILES if name.endswith('.py')}
        self.assertEqual(expected, {path.relative_to(source).as_posix() for path in files if path.suffix == '.py'})

    def leftover(self, digest="b" * 64):
        path = self.host.paths.tools / "tools" / digest
        path.mkdir()
        (path / "old.py").write_text("# retired implementation\n")
        return path

    def test_upgrade_plan_lists_and_apply_prunes_all_superseded_bundles(self):
        self.install()
        leftover = self.leftover()
        (self.source / "lib/new_module.py").write_text("# new implementation\n")
        plan = upgrades.plan_upgrade(self.host, self.source)
        expected = sorted([self.digest, leftover.name])
        self.assertEqual(expected, plan["prune_bundles"])
        result = upgrades.apply_upgrade(self.host, self.source, plan["plan_token"], str(uuid.uuid4()))
        self.assertEqual(expected, result["removed_bundles"])
        self.assertEqual([result["bundle_sha256"]],
                         [path.name for path in leftover.parent.iterdir()])

    def test_unchanged_install_prunes_leftovers_and_rerun_is_idempotent(self):
        self.install()
        leftover = self.leftover()
        plan = upgrades.plan_upgrade(self.host, self.source)
        self.assertIsNone(plan["upgrade"])
        self.assertEqual([leftover.name], plan["prune_bundles"])
        before = self.host.read_registry()
        result = self.install()
        self.assertEqual("unchanged", result["outcome"])
        self.assertEqual([leftover.name], result["removed_bundles"])
        self.assertEqual([], self.install()["removed_bundles"])
        self.assertEqual(before, self.host.read_registry())
        self.assertEqual([], upgrades.plan_upgrade(self.host, self.source)["prune_bundles"])

    def test_pending_registry_or_unclaimed_journal_blocks_prune(self):
        self.install()
        leftover = self.leftover()
        operation = str(uuid.uuid4())
        for claimed in (True, False):
            with self.subTest(claimed=claimed):
                self.host.registry["tools"]["upgrade_operation"] = operation if claimed else None
                journal = self.host.paths.p(HOST_PATHS["upgrades"]) / operation / "intent.json"
                upgrades.atomic_json(journal, {"phase": "staged", "bundle_sha256": leftover.name})
                with self.host.lock("host"), self.assertRaises(DeployError) as caught:
                    upgrades._prune_bundles(self.host)
                self.assertEqual("tools_upgrade_pending", caught.exception.code)
                self.assertTrue(leftover.is_dir())
                self.assertTrue((leftover.parent / self.digest).is_dir())
                self.assertEqual([], upgrades.plan_upgrade(self.host, self.source)["prune_bundles"])

    def test_unsafe_entries_refuse_plans_and_pruning_before_any_removal(self):
        self.install()
        leftover = self.leftover("0" * 64)
        outside = self.root / "outside"
        outside.mkdir()
        marker = outside / "keep"
        marker.write_text("keep")
        directory = leftover.parent
        for kind in ("name", "uppercase", "file", "symlink", "nested_symlink"):
            with self.subTest(kind=kind):
                path = directory / ("unexpected" if kind == "name" else "C" * 64 if kind == "uppercase" else "c" * 64)
                if kind in {"name", "uppercase", "nested_symlink"}:
                    path.mkdir()
                    if kind == "nested_symlink":
                        (path / "escape").symlink_to(outside, target_is_directory=True)
                elif kind == "file":
                    path.write_text("unexpected")
                else:
                    path.symlink_to(outside, target_is_directory=True)
                try:
                    with self.assertRaises(DeployError) as caught:
                        upgrades.plan_upgrade(self.host, self.source)
                    self.assertEqual("unsafe_path", caught.exception.code)
                    with self.host.lock("host"), self.assertRaises(DeployError) as caught:
                        upgrades._prune_bundles(self.host)
                    self.assertEqual("unsafe_path", caught.exception.code)
                    self.assertTrue(leftover.is_dir())
                    self.assertEqual("keep", marker.read_text())
                finally:
                    if kind == "nested_symlink":
                        (path / "escape").unlink()
                    if path.is_symlink() or path.is_file():
                        path.unlink()
                    else:
                        path.rmdir()

    def test_interrupted_prune_retries_after_completed_upgrade(self):
        self.install()
        (self.source / "lib/new_module.py").write_text("# new implementation\n")
        def interrupted(path):
            (path / "manifest.json").unlink()
            raise OSError("injected partial removal")
        operation = str(uuid.uuid4())
        plan = upgrades.plan_upgrade(self.host, self.source)
        with patch.object(upgrades.shutil, "rmtree", side_effect=interrupted), self.assertRaises(OSError):
            upgrades.apply_upgrade(self.host, self.source, plan["plan_token"], operation)
        self.assertIsNone(self.host.registry["tools"]["upgrade_operation"])
        intent = self.host.paths.p(HOST_PATHS["upgrades"]) / operation / "intent.json"
        self.assertEqual("complete", upgrades.read_json(intent)["phase"])
        self.assertTrue((self.host.paths.tools / "tools" / self.digest).is_dir())
        result = upgrades.apply_upgrade(self.host, self.source, plan["plan_token"], operation)
        self.assertEqual([self.digest], result["removed_bundles"])
        self.assertEqual([], self.install()["removed_bundles"])

    def test_installation_and_pruning_require_host_lock(self):
        with self.assertRaises(DeployError) as caught:
            upgrades.install_tools(self.host, self.host.read_registry(), self.source)
        self.assertEqual("tools_lock_required", caught.exception.code)
        self.assertFalse(self.host.paths.tools.exists())
        self.install()
        self.leftover()
        with self.assertRaises(DeployError) as caught:
            upgrades._prune_bundles(self.host)
        self.assertEqual("tools_lock_required", caught.exception.code)

    def test_prune_inventory_drift_invalidates_plan(self):
        self.install()
        plan = upgrades.plan_upgrade(self.host, self.source)
        leftover = self.leftover()
        with self.assertRaises(DeployError) as caught:
            upgrades.apply_upgrade(self.host, self.source, plan["plan_token"], str(uuid.uuid4()))
        self.assertEqual("plan_changed", caught.exception.code)
        self.assertTrue(leftover.is_dir())

    def test_pointer_disagreement_never_removes_either_bundle(self):
        self.install()
        leftover = self.leftover()
        pointer = self.host.paths.tools / "current"
        pointer.unlink()
        pointer.symlink_to("tools/" + leftover.name)
        with self.host.lock("host"), self.assertRaises(DeployError) as caught:
            upgrades._prune_bundles(self.host)
        self.assertEqual("tools_pointer_conflict", caught.exception.code)
        self.assertTrue(leftover.is_dir())
        self.assertTrue((leftover.parent / self.digest).is_dir())

    def test_retired_bundle_cannot_be_selected_again(self):
        self.install()
        change = self.source / "lib/new_module.py"
        change.write_text("# change\n")
        self.install(allow_upgrade=True)
        change.unlink()
        with self.assertRaises(DeployError) as caught:
            self.install(allow_upgrade=True)
        self.assertEqual("tools_downgrade", caught.exception.code)

    def test_pruned_history_rejects_every_retired_bundle_and_replays_active(self):
        first = self.install()
        source = self.source / 'lib/change.py'
        source.write_text('# B\n')
        second = self.install(allow_upgrade=True)
        source.write_text('# C\n')
        operation = str(uuid.uuid4())
        plan = upgrades.plan_upgrade(self.host, self.source)
        third = upgrades.apply_upgrade(self.host, self.source, plan['plan_token'], operation)
        journals = self.host.paths.p(HOST_PATHS['upgrades'])
        self.assertEqual([operation], [path.name for path in journals.iterdir()])
        self.assertEqual([second['operation_id']], third['removed_upgrade_journals'])
        ledger = self.host.paths.p(HOST_PATHS['retired_tools'])
        self.assertEqual({'schema': 1, 'digests': sorted([first['bundle_sha256'], second['bundle_sha256']])},
                         upgrades.read_json(ledger))
        self.assertEqual(0o600, ledger.stat().st_mode & 0o777)
        self.assertEqual(os.geteuid(), ledger.stat().st_uid)
        for content in (None, '# B\n'):
            if content is None:
                source.unlink()
            else:
                source.write_text(content)
            with self.assertRaises(DeployError) as caught:
                self.install(allow_upgrade=True)
            self.assertEqual('tools_downgrade', caught.exception.code)
        source.write_text('# C\n')
        before = self.host.read_registry()
        intent = (journals / operation / 'intent.json').read_bytes()
        replay = upgrades.apply_upgrade(self.host, self.source, plan['plan_token'], operation)
        self.assertEqual('unchanged', replay['outcome'])
        self.assertEqual([], replay['removed_upgrade_journals'])
        self.assertEqual(before, self.host.read_registry())
        self.assertEqual(intent, (journals / operation / 'intent.json').read_bytes())

    def test_ledger_write_precedes_deletion_and_interruption_converges(self):
        first = self.install()
        journals = self.host.paths.p(HOST_PATHS['upgrades'])
        intent = journals / first['operation_id'] / 'intent.json'
        original_intent = intent.read_bytes()
        (self.source / 'lib/change.py').write_text('# B\n')
        operation = str(uuid.uuid4())
        plan = upgrades.plan_upgrade(self.host, self.source)
        def crash(path):
            self.assertIn(first['bundle_sha256'], upgrades._retired_bundles(self.host))
            self.assertEqual(original_intent, intent.read_bytes())
            raise OSError('crash after durable ledger write')
        with patch.object(upgrades.shutil, 'rmtree', side_effect=crash), self.assertRaises(OSError):
            upgrades.apply_upgrade(self.host, self.source, plan['plan_token'], operation)
        result = upgrades.apply_upgrade(self.host, self.source, plan['plan_token'], operation)
        self.assertEqual('unchanged', result['outcome'])
        self.assertEqual([first['operation_id']], result['removed_upgrade_journals'])
        self.assertEqual([operation], [path.name for path in journals.iterdir()])
        self.assertEqual([first['bundle_sha256']], upgrades._retired_bundles(self.host))

    def test_retry_syncs_identical_ledger_after_interrupted_directory_sync(self):
        first = self.install()
        (self.source / 'lib/change.py').write_text('# B\n')
        operation = str(uuid.uuid4())
        plan = upgrades.plan_upgrade(self.host, self.source)
        ledger = self.host.paths.p(HOST_PATHS['retired_tools'])
        parent = ledger.parent.stat()
        fsync = os.fsync
        synced = []
        def sync(descriptor, fail=False):
            info = os.fstat(descriptor)
            if (info.st_dev, info.st_ino) == (parent.st_dev, parent.st_ino):
                if fail:
                    raise OSError('crash after ledger rename before directory sync')
                synced.append(True)
            return fsync(descriptor)
        with patch.object(upgrades.os, 'fsync', side_effect=lambda fd: sync(fd, fail=True)), self.assertRaises(OSError):
            upgrades.apply_upgrade(self.host, self.source, plan['plan_token'], operation)
        self.assertEqual([first['bundle_sha256']], upgrades._retired_bundles(self.host))
        remove = upgrades.shutil.rmtree
        def delete(path):
            self.assertTrue(synced, 'identical ledger must be durable before deletion')
            return remove(path)
        with patch.object(upgrades.os, 'fsync', side_effect=sync), \
                patch.object(upgrades.shutil, 'rmtree', side_effect=delete):
            result = upgrades.apply_upgrade(self.host, self.source, plan['plan_token'], operation)
        self.assertEqual([first['operation_id']], result['removed_upgrade_journals'])
        self.assertEqual('unchanged', result['outcome'])

    def test_invalid_retirement_ledger_fails_closed_before_pruning(self):
        self.install()
        leftover = self.leftover()
        ledger = self.host.paths.p(HOST_PATHS['retired_tools'])
        for value in ('{', '{}', '{"schema":true,"digests":[]}',
                      json.dumps({'schema': 1, 'digests': ['B' * 64]}),
                      json.dumps({'schema': 1, 'digests': ['b' * 64, 'a' * 64]}),
                      json.dumps({'schema': 1, 'digests': ['a' * 64] * 2})):
            with self.subTest(value=value):
                upgrades.atomic_write(ledger, value, 0o600)
                for action in (lambda: upgrades.plan_upgrade(self.host, self.source), self.install):
                    with self.assertRaises(DeployError) as caught:
                        action()
                    self.assertEqual('invalid_retired_tools', caught.exception.code)
                for prune in (lambda: upgrades._prune_bundles(self.host),
                              lambda: upgrades.prune_tools(self.host, self.digest)):
                    with self.host.lock('host'), self.assertRaises(DeployError) as caught:
                        prune()
                    self.assertEqual('invalid_retired_tools', caught.exception.code)
                self.assertTrue(leftover.exists())
        upgrades.atomic_json(ledger, {'schema': 1, 'digests': []}, 0o644)
        with self.assertRaises(DeployError) as caught:
            self.install()
        self.assertEqual('invalid_retired_tools', caught.exception.code)

    def test_unsafe_journal_paths_refuse_before_ledger_write_or_bundle_removal(self):
        installed = self.install()
        leftover = self.leftover()
        directory = self.host.paths.p(HOST_PATHS['upgrades'])
        journal = directory / installed['operation_id']
        outside = self.root / 'outside'
        outside.mkdir()
        for name, target in (('escape', outside), ('intent-link.json', journal / 'intent.json')):
            path = journal / name
            path.symlink_to(target)
            try:
                with self.host.lock('host'), self.assertRaises(DeployError) as caught:
                    upgrades._prune_bundles(self.host)
                self.assertEqual('unsafe_path', caught.exception.code)
                self.assertTrue(leftover.exists())
                self.assertFalse(self.host.paths.p(HOST_PATHS['retired_tools']).exists())
            finally:
                path.unlink()
        bad = directory / 'not-an-operation'
        bad.mkdir()
        with self.host.lock('host'), self.assertRaises(DeployError) as caught:
            upgrades._prune_bundles(self.host)
        self.assertEqual('unsafe_path', caught.exception.code)
        self.assertTrue(leftover.exists())

    def test_journal_deletion_resumes_after_rename_or_partial_rmtree(self):
        first = self.install()
        source = self.source / 'lib/change.py'
        for failure in ('rename', 'rmtree'):
            with self.subTest(failure=failure):
                previous = first
                source.write_text('# ' + failure + '\n')
                operation = str(uuid.uuid4())
                plan = upgrades.plan_upgrade(self.host, self.source)
                area = self.host.paths.p(HOST_PATHS['upgrade_deletions'])
                original_rename, original_remove = os.rename, upgrades.shutil.rmtree
                def rename(old, new):
                    original_rename(old, new)
                    if Path(new).parent == area:
                        raise OSError('crash after rename before parent sync')
                def remove(path):
                    if path.parent == area:
                        (path / 'intent.json').unlink()
                        raise OSError('crash inside rmtree after intent removal')
                    return original_remove(path)
                with patch.object(upgrades.os, 'rename', side_effect=rename if failure == 'rename' else original_rename), \
                        patch.object(upgrades.shutil, 'rmtree', side_effect=remove if failure == 'rmtree' else original_remove), \
                        self.assertRaises(OSError):
                    upgrades.apply_upgrade(self.host, self.source, plan['plan_token'], operation)
                queued = area / previous['operation_id']
                self.assertTrue(queued.is_dir())
                self.assertEqual(failure == 'rename', (queued / 'intent.json').exists())
                self.assertEqual(0o700, area.stat().st_mode & 0o777)
                self.assertEqual(os.geteuid(), area.stat().st_uid)
                self.assertFalse((self.host.paths.p(HOST_PATHS['upgrades']) / queued.name).exists())
                reviewed = upgrades.plan_upgrade(self.host, self.source)
                self.assertIsNone(reviewed['upgrade'])
                self.assertEqual([queued.name], reviewed['prune_upgrade_journals'])
                self.assertFalse(upgrades._upgrade_pending(self.host, self.host.read_registry()))
                # Apply and upgrade-resume share this owner. Keep the original
                # operation/token to prove replay does not require a new plan.
                first = upgrades.apply_upgrade(self.host, self.source, plan['plan_token'], operation)
                self.assertEqual('unchanged', first['outcome'])
                self.assertEqual([queued.name], first['removed_upgrade_journals'])
                self.assertEqual([], list(area.iterdir()))
                self.assertEqual([operation], [p.name for p in self.host.paths.p(HOST_PATHS['upgrades']).iterdir()])
                self.assertEqual([], upgrades.plan_upgrade(self.host, self.source)['prune_upgrade_journals'])
                self.assertEqual([], self.install()['removed_upgrade_journals'])

    def test_plan_rejects_separate_deletion_filesystem_without_mutation(self):
        self.install()
        (self.source / 'lib/change.py').write_text('# candidate\n')
        area = self.host.paths.p(HOST_PATHS['upgrade_deletions'])
        journals = self.host.paths.p(HOST_PATHS['upgrades'])
        original_stat = Path.stat
        def separate_filesystem(path, *args, **kwargs):
            info = original_stat(path, *args, **kwargs)
            if path == journals or journals in path.parents:
                fields = list(info)
                fields[2] = info.st_dev + 1
                return os.stat_result(fields)
            return info
        def files():
            return {path.relative_to(self.host.paths.root):
                    (path.lstat().st_mode, os.readlink(path) if path.is_symlink()
                     else path.read_bytes() if path.is_file() else None)
                    for path in self.host.paths.root.rglob('*')}
        for exists in (False, True):
            with self.subTest(deletion_area_exists=exists):
                if exists:
                    area.mkdir(mode=0o700)
                before, registry = files(), self.host.read_registry()
                reviewed = upgrades.plan_upgrade(self.host, self.source)
                with patch.object(Path, 'stat', separate_filesystem), \
                        patch.object(upgrades, '_select') as select:
                    for action in (lambda: upgrades.plan_upgrade(self.host, self.source),
                                   lambda: upgrades.tools_upgrade(self.host, registry, self.source),
                                   lambda: upgrades.apply_upgrade(self.host, self.source, reviewed['plan_token'], str(uuid.uuid4()))):
                        with self.assertRaises(DeployError) as caught:
                            action()
                        self.assertEqual('unsafe_path', caught.exception.code)
                        self.assertIn("share the journals' filesystem", caught.exception.detail)
                    select.assert_not_called()
                self.assertEqual(before, files())
                self.assertEqual(registry, self.host.read_registry())

    def test_plan_keeps_normal_layout_read_only_before_and_after_first_use(self):
        installed = self.install()
        area = self.host.paths.p(HOST_PATHS['upgrade_deletions'])
        before = self.host.read_registry()
        unchanged = upgrades.plan_upgrade(self.host, self.source)
        self.assertIsNone(unchanged['upgrade'])
        self.assertFalse(area.exists())
        (self.source / 'lib/change.py').write_text('# candidate\n')
        plan = upgrades.plan_upgrade(self.host, self.source)
        self.assertEqual(installed['bundle_sha256'], plan['upgrade']['from_digest'])
        self.assertEqual([installed['operation_id']], plan['prune_upgrade_journals'])
        self.assertFalse(area.exists())
        area.mkdir(mode=0o700)
        self.assertEqual(plan, upgrades.plan_upgrade(self.host, self.source))
        self.assertEqual([], list(area.iterdir()))
        self.assertEqual(before, self.host.read_registry())
        result = upgrades.apply_upgrade(self.host, self.source, plan['plan_token'], str(uuid.uuid4()))
        self.assertEqual('succeeded', result['outcome'])
        self.assertEqual([installed['operation_id']], result['removed_upgrade_journals'])

    def test_deletion_area_rejects_unsafe_entries_before_removing_anything(self):
        self.install()
        leftover = self.leftover()
        area = self.host.paths.p(HOST_PATHS['upgrade_deletions'])
        area.mkdir(mode=0o700)
        upgrades.atomic_json(self.host.paths.p(HOST_PATHS['retired_tools']), {'schema': 1, 'digests': []}, 0o600)
        outside = self.root / 'outside'
        outside.mkdir()
        marker = outside / 'keep'
        marker.write_text('keep')
        for kind in ('name', 'file', 'symlink', 'nested_symlink', 'hardlink', 'permissions'):
            path = area / ('unexpected' if kind == 'name' else str(uuid.uuid4()))
            with self.subTest(kind=kind):
                if kind == 'file':
                    path.write_text('unexpected')
                elif kind == 'symlink':
                    path.symlink_to(outside, target_is_directory=True)
                else:
                    path.mkdir()
                    if kind == 'nested_symlink':
                        (path / 'link').symlink_to(marker)
                    if kind == 'hardlink':
                        os.link(marker, path / 'link')
                    if kind == 'permissions':
                        area.chmod(0o755)
                try:
                    for action in (lambda: upgrades.plan_upgrade(self.host, self.source), self.install):
                        with self.assertRaises(DeployError) as caught:
                            action()
                        self.assertEqual('unsafe_path', caught.exception.code)
                    self.assertTrue(leftover.exists())
                    self.assertEqual('keep', marker.read_text())
                finally:
                    area.chmod(0o700)
                    if path.is_file() or path.is_symlink():
                        path.unlink()
                    else:
                        for child in path.iterdir():
                            child.unlink()
                        path.rmdir()

    def test_generation_nine_candidate_cannot_reselect_a_after_a_b_c_pruning(self):
        first = self.install()
        baseline = self.root / 'baseline'
        import shutil
        shutil.copytree(self.source, baseline)
        self.metadata['generation'] = 10
        (self.source / 'protocol.json').write_text(json.dumps(self.metadata))
        second = self.install(allow_upgrade=True)
        (self.source / 'lib/change.py').write_text('# C\n')
        third = self.install(allow_upgrade=True)
        self.assertEqual([third['operation_id']], [path.name for path in self.host.paths.p(HOST_PATHS['upgrades']).iterdir()])
        self.assertIn(first['bundle_sha256'], upgrades._retired_bundles(self.host))
        self.assertIn(second['bundle_sha256'], upgrades._retired_bundles(self.host))
        before = self.host.read_registry()
        # Generation-9 tools lack the ledger reader. The shared generation guard
        # must refuse planning/apply before either history mechanism is consulted.
        with patch.object(upgrades, '_retired_bundles', side_effect=AssertionError('history read')), \
                patch.object(upgrades, '_upgrade_journals', side_effect=AssertionError('history read')):
            for action in (lambda: upgrades.plan_upgrade(self.host, baseline),
                           lambda: upgrades.apply_upgrade(self.host, baseline, 'old-plan', str(uuid.uuid4()))):
                with self.assertRaises(DeployError) as caught:
                    action()
                self.assertEqual('tools_downgrade', caught.exception.code)
        self.assertEqual(before, self.host.read_registry())
        for action in ('activate', 'restart', 'down', 'up'):
            upgrades.check_client(self.host, {'protocol': self.metadata['protocol'], 'client': {'generation': 9}}, action)

    def test_upgrade_reloads_even_when_units_were_already_written(self):
        import provision
        from test_contracts import fixture
        self.install()
        _, profile, entry = fixture()
        entry['client_generation'] = 3
        entry['provisioning']['state'] = 'ready'
        self.host.registry['instances'][profile['instance_id']] = entry
        (self.source / 'lib/new_module.py').write_text('# candidate change\n')
        original_run = self.host.run
        def command(args, **kwargs):
            if args == ['systemctl', 'daemon-reload']:
                return subprocess.CompletedProcess(args, 0, '', '')
            return original_run(args, **kwargs)
        with patch.object(provision, '_install_unit') as reinstall, \
                patch.object(self.host, 'run', side_effect=command) as run:
            result = self.install(allow_upgrade=True)
        self.assertEqual('succeeded', result['outcome'])
        reinstall.assert_called_once()
        self.assertEqual(entry, reinstall.call_args.args[2])
        self.assertEqual(self.host.paths.tools / 'tools' / result['bundle_sha256'], reinstall.call_args.args[3])
        self.assertIn(['systemctl', 'daemon-reload'], [call.args[0] for call in run.call_args_list])
        self.assertEqual(3, self.host.read_registry()['instances'][profile['instance_id']]['client_generation'])

    def test_upgrade_stays_pending_until_new_bundle_rebinds_backup_commands(self):
        self.install()
        self.host.registry["shared"] = {"backup": {"owner": "foundry"},
                                         "postgresql": {"unit": "foundry-postgresql.service"}}
        (self.source / "lib/new_module.py").write_text("# updated bundle\n")
        operation = str(uuid.uuid4())
        calls = []
        original_run = self.host.run
        def command(args, **kwargs):
            if len(args) > 3 and args[3] == "prune":
                return original_run(args, **kwargs)
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
