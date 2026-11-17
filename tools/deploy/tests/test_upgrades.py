"""Tool upgrade fault coverage uses an isolated filesystem, never host services."""
import copy
import json
import os
import shlex
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
        self.loaded_boot = None

    def read_registry(self, missing=False):
        return copy.deepcopy(self.registry)

    def write_registry(self, registry):
        self.registry = copy.deepcopy(registry)

    def run(self, args, **kwargs):
        import feature_network
        if args == ["systemctl", "daemon-reload"]:
            self.loaded_boot = self.paths.p(feature_network.BOOT_PATH).read_text()
            return subprocess.CompletedProcess(args, 0, "", "")
        if args[:3] == ["systemctl", "show", feature_network.BOOT_UNIT]:
            command = next(line.split("=", 1)[1] for line in self.loaded_boot.splitlines() if line.startswith("ExecStart="))
            fields = {"LoadState": "loaded", "FragmentPath": feature_network.BOOT_PATH, "DropInPaths": "",
                      "NeedDaemonReload": "no", "ExecStart": "{ argv[]=" + " ".join(shlex.split(command)) + " ; }"}
            return subprocess.CompletedProcess(args, 0, "\n".join(key + "=" + value for key, value in fields.items()), "")
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
        self.metadata = {"protocol": {"major": 2, "minor": 0}, "generation": 19,
                         "minimum_host_generation": 19, "minimum_client_generation": 19,
                         "framework_version": "v0.42.1"}
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

    def same_generation_runtime(self):
        import services
        runtime_bytes = (Path(__file__).resolve().parents[1] / "server/runtime_v1.py").read_bytes()
        self.metadata["generation"] = 19
        (self.source / "protocol.json").write_text(json.dumps(self.metadata))
        (self.source / "server/runtime_v1.py").write_bytes(runtime_bytes)
        self.host.registry["tools"] = upgrades._active_record(self.source, upgrades.tool_bundle(self.source)[2], None)
        self.install()
        runtime = self.host.paths.p(services.RUNTIME_ENTRY)
        self.host.owned_write(runtime, runtime_bytes, mode=0o755)
        candidate = self.root / "candidate"
        import shutil
        shutil.copytree(self.source, candidate)
        (candidate / "lib/candidate.py").write_text("# distinct same-generation bundle\n")
        return runtime, candidate

    def test_same_generation_upgrade_preserves_runtime_and_invocation_pins_and_advances_boot(self):
        import feature_network
        import operations
        runtime, candidate = self.same_generation_runtime()
        old = self.host.registry["tools"].copy()
        pinned = upgrades._installed(self.host, old)
        boot = feature_network.boot_files(pinned, old["bundle_sha256"])
        for name, content in boot.items():
            self.host.owned_write(self.host.paths.p(name), content)
        pin = self.host.paths.p("/run/foundry/runtime-pins/instance/relay/invocation.json")
        self.host.owned_write(pin, b"retained invocation")
        operation = str(uuid.uuid4())
        # Pin inventories remain conservative during this migration.
        with patch.object(upgrades, "_obsolete_bundles", return_value=[]):
            plan = upgrades.plan_upgrade(self.host, candidate)
            self.assertEqual(len(plan["upgrade"]["migration_steps"]), 1)
            result = upgrades.apply_upgrade(self.host, candidate, plan["plan_token"], operation)
            self.assertEqual(result["outcome"], "succeeded")
            self.assertEqual(upgrades.apply_upgrade(self.host, candidate, plan["plan_token"], operation)["outcome"], "unchanged")
        operations.check_runtime(self.host, candidate)
        self.assertEqual(runtime.read_bytes(), (candidate / "server/runtime_v1.py").read_bytes())
        self.assertEqual(pin.read_bytes(), b"retained invocation")
        self.assertEqual(feature_network.checked_boot_files(self.host, candidate, self.host.registry["tools"]["bundle_sha256"]),
                         feature_network.boot_files(candidate, self.host.registry["tools"]["bundle_sha256"]))
        self.assertNotEqual(self.host.paths.p(feature_network.BOOT_PATH).read_text(), boot[feature_network.BOOT_PATH])

    def test_same_generation_updates_advance_boot_before_selection(self):
        import feature_network
        import services
        self.metadata["generation"] = 19
        (self.source / "protocol.json").write_text(json.dumps(self.metadata))
        self.host.registry["tools"] = upgrades._active_record(self.source, upgrades.tool_bundle(self.source)[2], None)
        self.install()
        old = self.host.registry["tools"]["bundle_sha256"]
        for name, content in feature_network.boot_files(self.source, old).items():
            self.host.owned_write(self.host.paths.p(name), content)
        self.host.owned_write(self.host.paths.p(services.RUNTIME_ENTRY), (self.source / "server/runtime_v1.py").read_bytes())
        select = upgrades._select
        for revision, generation in enumerate((19, 19, 19)):
            with self.subTest(generation=generation, revision=revision):
                (self.source / "protocol.json").write_text(json.dumps({**self.metadata, "generation": generation,
                    "framework_version": f"integration-{revision}"}))
                target = upgrades.tool_bundle(self.source)[2]
                plan = upgrades.plan_upgrade(self.host, self.source)
                self.assertIn({"path": feature_network.BOOT_PATH, "from_bundle": old, "to_bundle": target},
                              plan["upgrade"]["migration_steps"])
                def select_after_proof(host, bundle):
                    self.assertIn(bundle, host.loaded_boot)
                    self.assertIn(bundle, host.paths.p(feature_network.BOOT_PATH).read_text())
                    select(host, bundle)
                with patch.object(upgrades, "_select", side_effect=select_after_proof):
                    self.assertEqual(upgrades.apply_upgrade(self.host, self.source, plan["plan_token"], str(uuid.uuid4()))["outcome"], "succeeded")
                old = target

    def test_boot_advance_resumes_before_and_after_atomic_publication_and_reload(self):
        import feature_network
        runtime, candidate = self.same_generation_runtime()
        old = self.host.registry["tools"].copy()
        pinned = upgrades._installed(self.host, old)
        for name, content in feature_network.boot_files(pinned, old["bundle_sha256"]).items():
            self.host.owned_write(self.host.paths.p(name), content)
        plan = upgrades.plan_upgrade(self.host, candidate)
        operation = str(uuid.uuid4())
        write = self.host.owned_write
        def interrupted(path, content, **kwargs):
            if str(path).endswith(feature_network.BOOT_PATH):
                raise OSError("interrupted before publication")
            return write(path, content, **kwargs)
        with patch.object(self.host, "owned_write", side_effect=interrupted), self.assertRaises(OSError):
            upgrades.apply_upgrade(self.host, candidate, plan["plan_token"], operation)
        self.assertIn(old["bundle_sha256"], self.host.paths.p(feature_network.BOOT_PATH).read_text())
        run = self.host.run
        def interrupted_reload(args, **kwargs):
            if args == ["systemctl", "daemon-reload"]:
                raise OSError("interrupted after publication")
            return run(args, **kwargs)
        with patch.object(self.host, "run", side_effect=interrupted_reload), self.assertRaises(OSError):
            upgrades.apply_upgrade(self.host, candidate, plan["plan_token"], operation)
        target = upgrades.tool_bundle(candidate)[2]
        self.assertIn(target, self.host.paths.p(feature_network.BOOT_PATH).read_text())
        self.assertEqual(self.host.registry["tools"]["bundle_sha256"], old["bundle_sha256"])
        def wrong_loaded(args, **kwargs):
            result = run(args, **kwargs)
            if args[:3] == ["systemctl", "show", feature_network.BOOT_UNIT]:
                return subprocess.CompletedProcess(args, 0, result.stdout.replace(target, old["bundle_sha256"]), "")
            return result
        with patch.object(self.host, "run", side_effect=wrong_loaded), self.assertRaises(DeployError) as caught:
            upgrades.apply_upgrade(self.host, candidate, plan["plan_token"], operation)
        self.assertEqual(caught.exception.code, "boot_coordinator_unverified")
        self.assertEqual(self.host.registry["tools"]["bundle_sha256"], old["bundle_sha256"])
        self.assertEqual(upgrades.apply_upgrade(self.host, candidate, plan["plan_token"], operation)["outcome"], "succeeded")
        self.assertIn(target, self.host.loaded_boot)
        self.assertEqual(self.host.registry["tools"]["bundle_sha256"], target)
        journal = json.loads((self.host.paths.p(HOST_PATHS["upgrades"]) / operation / "intent.json").read_text())
        self.assertEqual(journal["migration_steps"], plan["upgrade"]["migration_steps"])


    def test_upgrade_refuses_unknown_installed_runtime_bytes(self):
        runtime, candidate = self.same_generation_runtime()
        runtime.write_bytes(runtime.read_bytes() + b"# unknown edit\n")
        before = self.host.read_registry()
        with self.assertRaises(DeployError) as caught:
            upgrades.plan_upgrade(self.host, candidate)
        self.assertEqual(caught.exception.code, "runtime_mismatch")
        self.assertEqual(self.host.read_registry(), before)

    def test_upgrade_refuses_changed_candidate_runtime_without_replacing_installed_bytes(self):
        runtime, candidate = self.same_generation_runtime()
        before = runtime.read_bytes()
        (candidate / "server/runtime_v1.py").write_bytes(before + b"# different runtime\n")
        with self.assertRaises(DeployError) as caught:
            upgrades.plan_upgrade(self.host, candidate)
        self.assertEqual(caught.exception.code, "runtime_mismatch")
        self.assertEqual(runtime.read_bytes(), before)

    def test_bundle_digest_keeps_inventory_encoding(self):
        import hashlib
        files = sorted(p for p in self.source.rglob("*") if p.is_file())
        inventory = [{"path": p.relative_to(self.source).as_posix(), "size": p.stat().st_size,
                      "mode": 0o644, "sha256": hashlib.sha256(p.read_bytes()).hexdigest()} for p in files]
        encoded = json.dumps({"schema": 1, "files": inventory}, sort_keys=True, separators=(",", ":")).encode()
        self.assertEqual(upgrades.tool_bundle(self.source)[2], hashlib.sha256(encoded).hexdigest())

    def test_check_client_accepts_registered_and_unregistered_feature_topologies(self):
        from test_host_capabilities import declaration
        from topology import validate_topology
        self.host.registry["tools"]["generation"] = 19
        for templates, public_listeners in ((False, False), (True, False), (False, True), (True, True)):
            raw = declaration()
            if templates:
                raw["roles"][1]["configs"] = {"settings": {
                    "source": "deploy/files/config", "mode": "rendered-template", "format": "ini"}}
            if public_listeners:
                raw["roles"][0]["bindings"] = {
                    "public": {"public_listener": {"role": "relay", "listener": "ingress"}}}
            for topology in (raw, validate_topology(raw)):
                request = {"protocol": self.metadata["protocol"], "client": {"generation": 19},
                           "topology": topology}
                for action in ("bootstrap-plan", "release-plan", "activate"):
                    with self.subTest(templates=templates, public_listeners=public_listeners,
                                      registered="sha256" in topology, action=action):
                        upgrades.check_client(self.host, request, action)

    def test_check_client_registered_topologies_preserve_hash_and_generation_checks(self):
        from test_host_capabilities import declaration
        from topology import validate_topology
        raw = declaration()
        raw["roles"][1]["configs"] = {"settings": {
            "source": "deploy/files/config", "mode": "rendered-template", "format": "ini"}}
        topology = validate_topology(raw)
        for action in ("bootstrap-plan", "release-plan", "activate"):
            for host_generation, client_generation, digest, code in (
                    (19, 19, "0" * 64, "invalid_topology"),
                    (19, 18, topology["sha256"], "client_too_old")):
                self.host.registry["tools"]["generation"] = host_generation
                request = {"protocol": self.metadata["protocol"], "client": {"generation": client_generation},
                           "topology": {**topology, "sha256": digest}}
                with self.subTest(action=action, code=code):
                    with self.assertRaises(DeployError) as caught:
                        upgrades.check_client(self.host, request, action)
                    self.assertEqual(caught.exception.code, code)

    def test_new_selections_use_protocol_floor_and_preserve_higher_stored_values(self):
        for stored in (0, *range(12, 21)):
            entry = {"deployment_selection": {"minimum_generation": stored}} if stored else {}
            for archived in (0, 13, 18, 19, 20):
                with self.subTest(stored=stored, archived=archived):
                    self.assertEqual(max(19, stored, archived), upgrades.check_selection_generation(
                        {"tools": {"generation": 20}}, entry, archived_minimum=archived))
                    if max(stored, archived) > 19:
                        with self.assertRaises(DeployError) as caught:
                            upgrades.check_selection_generation({"tools": {"generation": 19}}, entry,
                                                                archived_minimum=archived)
                        self.assertEqual("host_upgrade_required", caught.exception.code)

    def test_boot_writer_checks_stored_floor_against_selected_bundle(self):
        import feature_network
        from test_host_capabilities import declaration, selection
        from topology import validate_topology
        self.same_generation_runtime()
        current = self.host.registry["tools"]
        for name, content in feature_network.boot_files(self.source, current["bundle_sha256"]).items():
            self.host.owned_write(self.host.paths.p(name), content)
        for minimum in (13, 18, 19, 20):
            self.host.registry["instances"] = {"feature": {"topology": validate_topology(declaration()),
                "deployment_selection": {**selection(), "minimum_generation": minimum}}}
            with self.subTest(minimum=minimum):
                if minimum <= 19:
                    feature_network.check_registry_writer(self.host, self.host.registry)
                else:
                    with self.assertRaises(DeployError) as caught:
                        feature_network.check_registry_writer(self.host, self.host.registry)
                    self.assertEqual("boot_coordinator_upgrade_required", caught.exception.code)

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

    def test_unresolved_operation_staging_is_outside_upgrade_release_inventory(self):
        import operations
        from test_contracts import fixture
        self.install()
        _, profile, entry = fixture()
        entry["client_generation"] = 9
        self.host.registry["instances"][profile["instance_id"]] = entry
        releases = self.host.paths.opt(entry["name"]) / "releases"
        releases.mkdir(parents=True)
        staging = operations.staging(self.host, entry, str(uuid.uuid4()))
        (staging / "artifacts").mkdir(parents=True)
        (staging / "artifacts/incomplete").write_bytes(b"unfinished")
        with self.host.lock("host"):
            self.assertIsNone(upgrades.tools_upgrade(self.host, self.host.read_registry(), self.source))
        self.assertTrue((staging / "artifacts/incomplete").exists())
        self.assertEqual(list(releases.iterdir()), [])


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
        request = {"protocol": self.metadata["protocol"], "client": {"generation": 19}}
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
        for generation, code in [(20, "host_upgrade_required"), (18, "client_too_old")]:
            request = {"protocol": {"major": 2, "minor": 0}, "client": {"generation": generation}}
            with self.assertRaises(DeployError) as caught:
                upgrades.check_client(self.host, request, "activate")
            self.assertEqual(code, caught.exception.code)
            upgrades.check_client(self.host, request, "hello")
        self.assertEqual({"minimum": 19, "maximum": 19}, upgrades.hello(self.host)["client_generation"])

    def test_reads_reject_below_floor_clients_and_incompatible_protocol_major(self):
        self.install()
        for action in ("status", "tools-plan"):
            for generation, major in ((18, 2), (20, 3)):
                request = {"protocol": {"major": major, "minor": 0}, "client": {"generation": generation}}
                with self.subTest(action=action, generation=generation, major=major), self.assertRaises(DeployError) as caught:
                    upgrades.check_client(self.host, request, action)
                self.assertEqual("client_too_old", caught.exception.code)

    def test_newer_client_can_read_and_plan_with_a_higher_protocol_minor(self):
        self.install()
        for action in upgrades.READ_ACTIONS - {"hello"}:
            for minor in (0, 1):
                request = {"protocol": {"major": 2, "minor": minor}, "client": {"generation": 20}}
                with self.subTest(action=action, minor=minor):
                    upgrades.check_client(self.host, request, action)

    def test_mutations_still_reject_higher_protocol_minor(self):
        self.install()
        request = {"protocol": {"major": 2, "minor": 1}, "client": {"generation": 19}}
        with self.assertRaises(DeployError) as caught:
            upgrades.check_client(self.host, request, "activate")
        self.assertEqual("client_too_old", caught.exception.code)

    def test_installed_bundle_cannot_lower_the_running_tools_client_floor(self):
        self.metadata.update(minimum_host_generation=18, minimum_client_generation=18)
        (self.source / "protocol.json").write_text(json.dumps(self.metadata))
        self.host.registry["tools"] = upgrades._active_record(self.source, upgrades.tool_bundle(self.source)[2], None)
        self.install()
        request = {"protocol": self.metadata["protocol"], "client": {"generation": 18}}
        for action in (upgrades.READ_ACTIONS - {"hello"}) | {"activate", "tools-apply", "upgrade-resume"}:
            with self.subTest(action=action), self.assertRaises(DeployError) as caught:
                upgrades.check_client(self.host, request, action)
            self.assertEqual("client_too_old", caught.exception.code)

    def test_supported_reads_remain_available_during_pending_mutations(self):
        self.install()
        self.host.registry["tools"]["upgrade_operation"] = str(uuid.uuid4())
        request = {"protocol": self.metadata["protocol"], "client": {"generation": 19}}
        with patch("capacity.require_idle", side_effect=AssertionError("mutation guard")):
            for action in upgrades.READ_ACTIONS:
                with self.subTest(action=action):
                    upgrades.check_client(self.host, request, action)

    def test_generation_nineteen_upgrades_to_current(self):
        self.assert_upgrade_to_current()

    def assert_upgrade_to_current(self):
        actual = Path(__file__).resolve().parents[1]
        metadata = upgrades.protocol(actual)
        self.assertEqual(metadata["generation"], 19)
        self.assertEqual(19, metadata["minimum_host_generation"])
        self.assertEqual(19, metadata["minimum_client_generation"])
        _, files, _ = upgrades.tool_bundle(actual)
        self.assertIn(actual / "lib/maintenance.py", files)
        self.assertIn(actual / "lib/uploads.py", files)
        self.assertIn(actual / "server/templates/nginx-upload.conf", files)
        self.install()
        self.metadata['generation'] = metadata['generation']
        (self.source / 'protocol.json').write_text(json.dumps(self.metadata))
        (self.source / "lib/maintenance.py").write_text("# current module\n")
        plan = upgrades.plan_upgrade(self.host, self.source)
        result = upgrades.apply_upgrade(self.host, self.source, plan["plan_token"], str(uuid.uuid4()))
        self.assertEqual("succeeded", result["outcome"])
        self.assertEqual(metadata["generation"], self.host.read_registry()["tools"]["generation"])
        self.assertEqual({"minimum": 19, "maximum": metadata["generation"]}, upgrades.hello(self.host)["client_generation"])
        for old_generation in (18,):
            self.metadata["generation"] = old_generation
            (self.source / "protocol.json").write_text(json.dumps({**self.metadata, "minimum_host_generation": 18,
                "minimum_client_generation": 18}))
            with self.assertRaises(DeployError) as caught:
                upgrades.plan_upgrade(self.host, self.source)
            self.assertEqual(caught.exception.code, "tools_upgrade_floor")


    def test_supported_bundle_requires_upload_files(self):
        (self.source / "protocol.json").write_text(json.dumps({**self.metadata, "generation": 19}))
        for name in ("lib/uploads.py", "server/templates/nginx-upload.conf"):
            with self.subTest(name=name):
                path = self.source / name
                data = path.read_bytes()
                path.unlink()
                try:
                    with self.assertRaises(DeployError) as caught:
                        upgrades.tool_bundle(self.source)
                    self.assertEqual("tools_incomplete", caught.exception.code)
                finally:
                    path.write_bytes(data)

    def test_below_floor_refuses_planning_and_installation_before_writes(self):
        self.install()
        self.host.registry['tools']['generation'] = 18
        before = self.host.read_registry()
        for action in (lambda: upgrades.plan_upgrade(self.host, self.source),
                       lambda: self.install(allow_upgrade=True)):
            with self.assertRaises(DeployError) as caught:
                action()
            self.assertEqual('tools_upgrade_floor', caught.exception.code)
            self.assertIn('must already run generation 19 tools', caught.exception.detail)
            self.assertEqual(before, self.host.read_registry())

    def test_candidate_python_inventory_contains_only_runtime_and_shipped_rehearsal_modules(self):
        source = Path(__file__).resolve().parents[1]
        _, files, _ = upgrades.tool_bundle(source)
        expected = {name for name in upgrades.REQUIRED_FILES | upgrades.REHEARSAL_FILES if name.endswith('.py')}
        expected.update({'lib/unix_socket_probe.py', 'lib/copy_enablement.py', 'lib/operation_selection.py',
                         'lib/rehearsal_storage.py', 'lib/release_review.py', 'lib/disposable.py'})
        self.assertEqual(expected, {path.relative_to(source).as_posix() for path in files if path.suffix == '.py'})

    def test_supported_bundles_require_the_complete_inventory(self):
        for generation in (19,):
            with self.subTest(generation=generation):
                (self.source / "protocol.json").write_text(json.dumps({**self.metadata, "generation": generation}))
                _, files, _ = upgrades.tool_bundle(self.source)
                self.assertEqual({p.relative_to(self.source).as_posix() for p in files}, upgrades.REQUIRED_FILES)
                for name in upgrades.REQUIRED_FILES - {"protocol.json"}:
                    path = self.source / name
                    data = path.read_bytes()
                    path.unlink()
                    try:
                        with self.assertRaises(DeployError) as error:
                            upgrades.tool_bundle(self.source)
                        self.assertEqual(error.exception.code, "tools_incomplete")
                    finally:
                        path.write_bytes(data)

    def test_below_floor_bundle_is_rejected(self):
        for generation in range(1, 19):
            (self.source / "protocol.json").write_text(json.dumps({**self.metadata, "generation": generation,
                "minimum_host_generation": 1, "minimum_client_generation": 1}))
            with self.assertRaises(DeployError) as caught:
                upgrades.tool_bundle(self.source)
            self.assertEqual(caught.exception.code, "tools_upgrade_floor")

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

    def test_generation_eighteen_candidate_is_rejected_before_history_reads(self):
        self.install()
        baseline = self.root / 'baseline'
        import shutil
        shutil.copytree(self.source, baseline)
        (baseline / 'protocol.json').write_text(json.dumps({**self.metadata, 'generation': 18,
            'minimum_host_generation': 18, 'minimum_client_generation': 18}))
        before = self.host.read_registry()
        with patch.object(upgrades, '_retired_bundles', side_effect=AssertionError('history read')), \
                patch.object(upgrades, '_upgrade_journals', side_effect=AssertionError('history read')):
            for action in (lambda: upgrades.plan_upgrade(self.host, baseline),
                           lambda: upgrades.apply_upgrade(self.host, baseline, 'old-plan', str(uuid.uuid4()))):
                with self.assertRaises(DeployError) as caught:
                    action()
                self.assertEqual('tools_upgrade_floor', caught.exception.code)
        self.assertEqual(before, self.host.read_registry())

    def test_upgrade_preserves_old_registration_and_reloads_units_for_current_clients(self):
        import provision
        from test_contracts import fixture
        self.install()
        _, profile, entry = fixture()
        entry['client_generation'] = 3
        entry['provisioning']['state'] = 'ready'
        self.host.registry['instances'][profile['instance_id']] = entry
        (self.source / 'lib/new_module.py').write_text('# candidate change\n')
        self.metadata["generation"] = 19
        (self.source / "protocol.json").write_text(json.dumps(self.metadata))
        plan = upgrades.plan_upgrade(self.host, self.source)
        original_run = self.host.run
        def command(args, **kwargs):
            if args == ['systemctl', 'daemon-reload']:
                return subprocess.CompletedProcess(args, 0, '', '')
            return original_run(args, **kwargs)
        with patch.object(provision, '_install_unit') as reinstall, \
                patch.object(self.host, 'run', side_effect=command) as run:
            result = upgrades.apply_upgrade(self.host, self.source, plan["plan_token"], str(uuid.uuid4()))
        self.assertEqual('succeeded', result['outcome'])
        reinstall.assert_called_once()
        self.assertEqual(entry, reinstall.call_args.args[2])
        self.assertEqual(self.host.paths.tools / 'tools' / result['bundle_sha256'], reinstall.call_args.args[3])
        self.assertIn(['systemctl', 'daemon-reload'], [call.args[0] for call in run.call_args_list])
        self.assertEqual(3, self.host.read_registry()['instances'][profile['instance_id']]['client_generation'])
        for generation in (19,):
            upgrades.check_client(self.host, {"protocol": self.metadata["protocol"], "client": {"generation": generation}}, "activate")
        with self.assertRaises(DeployError) as caught:
            upgrades.check_client(self.host, {"protocol": self.metadata["protocol"], "client": {"generation": 18}}, "activate")
        self.assertEqual(caught.exception.code, "client_too_old")


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
