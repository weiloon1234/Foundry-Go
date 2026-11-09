"""Activation and interruption acceptance with real files and simulated processes.

No services are started. These fixtures exercise the production state machine,
archive validation, atomic links, locks and recovery on an isolated temporary host.
"""
import copy
import base64
import io
import json
import os
from pathlib import Path
import subprocess
import sys
import tarfile
import tempfile
import time
import unittest
from unittest.mock import MagicMock, patch
import uuid

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "lib"))
from contracts import (DeployError, atomic_json, atomic_write, bind_storage, digest, dump_env, identity,
                       now, parse_env, runtime_storage, storage_bindings)
from host import Host, Paths
import release
from runtime_config import (CREDENTIAL_KEYS, ENCRYPTION_KEYS, runtime_environment,
                            initialize_bootstrap_secrets, candidate_values, rotation_changes,
                            validate_keyring_change)
from topology import instance_units
from test_contracts import fixture, registry, storage_fixture

CHECK_READY = release.ready


class ProcessHost(Host):
    def __init__(self, root):
        super().__init__(Paths(root))
        self.commands = []
        self.fail = None
        self.interrupt = None
        self.pending = False
        self.inspect_output = json.dumps([{"Name": "project.name", "Source": "env-file:PROJECT__NAME", "Secret": False},
                                          {"Name": "developer.enabled", "Source": "defaults", "Secret": False}])
        self.commands_output = json.dumps([{"name": "about"}, {"name": "config.check"}])
        self.redis_keys_output = json.dumps([])
        self.storage_probe_output = json.dumps({"schema": 1, "disks": []})
        self.storage_probe_status = 0
        self.role_states = {}
        self.enabled_roles = set()

    def run(self, args, **kwargs):
        args = list(map(str, args))
        self.commands.append((args, kwargs))
        if self.interrupt and self.interrupt in args:
            raise KeyboardInterrupt()
        if self.fail and self.fail in args:
            raise DeployError("injected_failure", "Synthetic failure")
        output = ""
        if args[:2] == ["systemctl", "is-enabled"]:
            enabled = args[2] in self.enabled_roles
            return subprocess.CompletedProcess(args, 0 if enabled else 1, "enabled\n" if enabled else "disabled\n", "")
        if args[:2] == ["systemctl", "show"]:
            output = "ActiveState=" + self.role_states.get(args[2], "inactive") + "\n"
        if args[:2] == ["systemctl", "enable"]:
            self.enabled_roles.update(args[2:])
        if args[:2] == ["systemctl", "disable"]:
            self.enabled_roles.difference_update(args[2:])
        if args[:2] in (["systemctl", "start"], ["systemctl", "restart"]):
            for unit in args[2:]:
                self.role_states[unit] = "active"
        if args[:2] == ["systemctl", "stop"]:
            for unit in args[2:]:
                self.role_states[unit] = "inactive"
        if "migrate:status" in args:
            output = json.dumps({"targets": [{"schema": "public", "problems": [], "migrations": [
                {"state": "pending" if self.pending else "applied"}]}]})
        if "config:inspect" in args:
            output = self.inspect_output
        if "command:list" in args:
            output = self.commands_output
        if "redis:keys" in args:
            output = self.redis_keys_output
        if "deploy:storage-probe" in args:
            return subprocess.CompletedProcess(args, self.storage_probe_status, self.storage_probe_output, "private provider stderr")
        if args[0] == "pg_dump":
            Path(next(a[7:] for a in args if a.startswith("--file="))).write_bytes(b"fixture-backup")
        return subprocess.CompletedProcess(args, 0, output, "")


class ReleaseTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name).resolve()
        self.host = ProcessHost(self.root)
        self.project, self.profile, self.entry = fixture()
        self.profile["retention"] = {"server_releases": 1, "local_releases": 1, "pre_migration_dumps": 1}
        self.registry = registry(self.entry)
        self.host.write_registry(self.registry)
        self.opt = self.host.paths.opt(self.entry["name"])
        (self.opt / "releases").mkdir(parents=True)
        self.host.paths.data(self.entry["name"]).mkdir(parents=True)
        self.env_file = self.host.paths.instance_config(self.entry["name"]) / "app.env"
        self.secret = "NEVER_INCLUDE_THIS_SECRET_IN_STATE_OR_RELEASE"
        credentials = dict.fromkeys(CREDENTIAL_KEYS, self.secret)
        credentials["instance_id"] = self.entry["profile"]["instance_id"]
        atomic_json(self.env_file.with_name("secrets.json"), credentials, 0o600)
        atomic_write(self.env_file, dump_env(runtime_environment(self.entry, credentials)), 0o640)
        self.health = patch.object(release, "ready", return_value=True).start()
        patch.object(release.backup, "recovery_point", side_effect=self.recovery_point).start()
        patch.object(release.backup, "archive_keys", return_value={"ok": True}).start()
        self.grants = patch.object(release, "reconcile_redis_grants", side_effect=self.reconcile_grants).start()
        self.addCleanup(patch.stopall)

    def reconcile_grants(self, host, registry, entry, patterns, *, channels=None):
        target = {"keys": [value[:-1] for value in patterns], "channels": [value[:-1] for value in channels]}
        if all(entry.get("redis_grants", {}).get(key) == value for key, value in target.items()):
            return False
        entry.setdefault("redis_grants", {}).update(target)
        host.journal(registry, entry)
        return True

    def recovery_point(self, host, entry, operation_id):
        host.commands.append((["recovery_point"], {}))
        if host.fail == "recovery_point":
            raise DeployError("injected_failure", "Synthetic backup failure")
        return {"id": operation_id}

    def bundle(self, sequence=1, mutate=None, extra=None):
        version = f"20261013T00000{sequence}Z-0123456789abcdef"
        source = self.root / f"source-{sequence}"
        source.mkdir()
        files = {"artisan": b"fixture executable", "config/deploy.toml": b"[project]\n[app.worker]\nconnection = \"background\"\n",
                 "public/admin/index.html": b"portal", f"private-source-maps/{version}/admin.js.map": b"private map"}
        for path, data in files.items():
            destination = source / path
            destination.parent.mkdir(parents=True, exist_ok=True)
            destination.write_bytes(data)
        manifest = {"schema": 1, "layout_version": 2, "project_id": self.entry["project_id"],
                    "instance_id": self.entry["profile"]["instance_id"], "instance": self.entry["name"],
                    "environment": self.entry["environment"], "architecture": "arm64", "native_imaging": False,
                    "version": version, "commit": "0123456789abcdef", "tree_state": "clean", "built_at": now(),
                    "source_kind": "git_archive", "source_sha256": "a" * 64,
                    "files": {path: digest(source / path) for path in files}}
        if mutate:
            mutate(manifest)
        atomic_json(source / "manifest.json", manifest)
        archive = self.root / f"release-{sequence}.tar.gz"
        with tarfile.open(archive, "w:gz") as stream:
            for path in sorted(source.rglob("*")):
                if path.is_file():
                    stream.add(path, arcname=str(path.relative_to(source)))
            if extra:
                item = tarfile.TarInfo(extra)
                item.size = 4
                stream.addfile(item, io.BytesIO(b"evil"))
        return archive, manifest

    def deploy(self, archive):
        return release.activate(self.host, self.project, self.profile, archive, digest(archive))

    def select_fixture_release(self):
        archive, manifest = self.bundle()
        release.unpack_archive(archive, release.release_path(self.host, self.entry, manifest["version"]), self.entry, digest(archive))
        release.switch_to(self.host, self.entry, manifest["version"])
        state = self.host.load_state(self.entry)
        state["current"] = manifest
        self.host.save_state(self.entry, state)
        return manifest

    def test_storage_probe_ok_and_app_arguments(self):
        names = ["uploads", "0.assets_v2-archive", "a" * 128]
        self.host.storage_probe_output = json.dumps({"schema": 1, "disks": [
            {"disk": name, "outcome": "ok"} for name in names]})
        candidate = self.env_file.with_name("app.env.next")
        atomic_write(candidate, self.env_file.read_text(), 0o640)
        release.probe_storage(self.host, self.entry, self.opt, candidate)
        args, options = self.host.commands[-1]
        self.assertIn("deploy:storage-probe", args)
        self.assertEqual(str(candidate), args[args.index("--env-file") + 1])
        self.assertFalse(options["check"])
        self.assertEqual(300, options["timeout"])
        release.app(self.host, self.entry, self.opt, "config:check")
        self.assertTrue(self.host.commands[-1][1]["check"])

    def test_storage_probe_failed_names_every_disk_stage_code_and_leftover(self):
        leftover = "foundry-deploy-probe/" + "a" * 32
        self.host.storage_probe_output = json.dumps({"schema": 1, "disks": [
            {"disk": "uploads", "outcome": "failed", "stage": "write", "code": "storage_forbidden"},
            {"disk": "assets", "outcome": "failed", "stage": "delete", "code": "storage_unavailable",
             "leftover_key": leftover}]})
        for status in (0, 1):
            with self.subTest(status=status):
                self.host.storage_probe_status = status
                with self.assertRaises(DeployError) as caught:
                    release.probe_storage(self.host, self.entry, self.opt)
                self.assertEqual("storage_probe_failed", caught.exception.code)
                for text in ("uploads", "write", "storage_forbidden", "assets", "delete", "storage_unavailable",
                             "bucket and credentials", leftover):
                    self.assertIn(text, caught.exception.detail)
                self.assertNotIn("private provider stderr", caught.exception.detail)

    def test_storage_probe_exit_two_requires_registration(self):
        self.host.storage_probe_status = 2
        self.host.storage_probe_output = "private invalid stdout"
        with self.assertRaises(DeployError) as caught:
            release.probe_storage(self.host, self.entry, self.opt)
        self.assertEqual("storage_probe_unavailable", caught.exception.code)
        for text in ("deploy:storage-probe", "deployment guide", "rebuild"):
            self.assertIn(text, caught.exception.detail)
        self.assertNotIn("private", caught.exception.detail)

    def test_storage_probe_refuses_invalid_reports_without_exposing_output(self):
        ok = {"disk": "uploads", "outcome": "ok"}
        failed = {"disk": "uploads", "outcome": "failed", "stage": "write", "code": "storage_forbidden"}
        reports = [None, [], {}, {"schema": True, "disks": []}, {"schema": 1.0, "disks": []},
                   {"schema": 2, "disks": []}, {"schema": 1, "disks": {}},
                   {"schema": 1, "disks": [], "private": "provider material"}]
        disks = [[None], [ok, ok], [ok] * 65, [{**ok, "private": "provider material"}],
                 [{**ok, "stage": "read"}], [{**failed, "code": "private provider material"}],
                 [{**failed, "stage": "private provider material"}], [{"disk": "uploads", "outcome": "failed"}],
                 [{**failed, "leftover_key": "private provider material"}],
                 [{**failed, "leftover_key": "foundry-deploy-probe/" + "a" * 31}],
                 [{**ok, "outcome": ["ok"]}]]
        for name in ("../private", "private\nmaterial", "PRIVATE", "_private", "", "a" * 129, "é"):
            disks.append([{**ok, "disk": name}])
        reports.extend({"schema": 1, "disks": items} for items in disks)
        outputs = ["private invalid JSON", "{\"schema\":1}", "{} {}",
                   '{"schema":1,"schema":1,"disks":[]}', '{"schema":1,"disks":[],"private":NaN}',
                   "[" * 1100 + "]" * 1100, *map(json.dumps, reports)]
        for output in outputs:
            with self.subTest(output=output):
                self.host.storage_probe_output = output
                with self.assertRaises(DeployError) as caught:
                    release.probe_storage(self.host, self.entry, self.opt)
                self.assertEqual("storage_probe_invalid", caught.exception.code)
                self.assertNotIn("private", caught.exception.detail)
        self.host.storage_probe_output = json.dumps({"schema": 1, "disks": [ok]})
        self.host.storage_probe_status = 1
        with self.assertRaises(DeployError) as caught:
            release.probe_storage(self.host, self.entry, self.opt)
        self.assertEqual("storage_probe_invalid", caught.exception.code)

    def test_deploy_probes_after_config_check_before_redis_and_switch_only_with_bindings(self):
        first, _ = self.bundle(1)
        self.deploy(first)
        self.assertFalse(any("deploy:storage-probe" in args for args, _ in self.host.commands))
        release.config_set(self.host, self.project, self.profile, self.storage_changes("old/"))
        self.host.commands.clear()
        second, _ = self.bundle(2)
        switch_to = release.switch_to
        def switch(host, entry, version):
            commands = [args for args, _ in host.commands]
            config = next(i for i, args in enumerate(commands) if "config:check" in args)
            probe = next(i for i, args in enumerate(commands) if "deploy:storage-probe" in args)
            redis = next(i for i, args in enumerate(commands) if "redis:keys" in args)
            self.assertLess(config, probe)
            self.assertLess(probe, redis)
            self.assertEqual(1, sum("deploy:storage-probe" in args for args in commands))
            return switch_to(host, entry, version)
        with patch.object(release, "switch_to", side_effect=switch) as switching:
            self.deploy(second)
        switching.assert_called_once()

    def test_failed_deploy_storage_probe_does_not_switch_release(self):
        first, manifest = self.bundle(1)
        self.deploy(first)
        release.config_set(self.host, self.project, self.profile, self.storage_changes("old/"))
        second, _ = self.bundle(2)
        self.host.storage_probe_output = json.dumps({"schema": 1, "disks": [
            {"disk": "uploads", "outcome": "failed", "stage": "write", "code": "storage_forbidden"}]})
        with patch.object(release, "switch_to") as switch:
            with self.assertRaises(DeployError) as caught:
                self.deploy(second)
        self.assertEqual("storage_probe_failed", caught.exception.code)
        self.assertIn("uploads: stage write, code storage_forbidden", caught.exception.detail)
        switch.assert_not_called()
        self.assertEqual(manifest, self.state()["current"])
        self.assertEqual(manifest["version"], release.current_version(self.host, self.entry))
        self.assertEqual("failed", self.state()["last_attempt"]["outcome"])

    def test_configuration_storage_probe_uses_candidate_and_failure_preserves_publication(self):
        archive, _ = self.bundle()
        self.deploy(archive)
        before = self.env_file.read_bytes(), self.host.paths.registry.read_bytes(), self.state()["configuration_revision"]
        self.host.commands.clear()
        self.host.storage_probe_output = json.dumps({"schema": 1, "disks": [
            {"disk": "uploads", "outcome": "failed", "stage": "write", "code": "storage_forbidden"}]})
        with self.assertRaises(DeployError) as caught:
            release.config_set(self.host, self.project, self.profile, self.storage_changes("new/"))
        self.assertEqual("storage_probe_failed", caught.exception.code)
        self.assertEqual(before, (self.env_file.read_bytes(), self.host.paths.registry.read_bytes(), self.state()["configuration_revision"]))
        commands = [args for args, _ in self.host.commands]
        probe = next(i for i, args in enumerate(commands) if "deploy:storage-probe" in args)
        inspect = next(i for i, args in enumerate(commands) if "config:inspect" in args)
        self.assertLess(inspect, probe)
        args = commands[probe]
        self.assertEqual(str(self.env_file.with_name("app.env.next")), args[args.index("--env-file") + 1])
        self.assertFalse(self.env_file.with_name("app.env.next").exists())
        self.assertFalse(any(args[0] == "systemctl" and args[1] != "is-enabled" for args in commands))

    def test_configuration_only_changed_storage_keys_probe(self):
        archive, _ = self.bundle()
        self.deploy(archive)
        changes = self.storage_changes("old/")
        release.config_set(self.host, self.project, self.profile, changes)
        self.host.inspect_output = json.dumps([
            {"Name": key.lower().replace("__", "."), "Source": "defaults", "Secret": True}
            for key in [*changes, "PROJECT__NAME"]])
        with patch.object(release, "probe_storage", side_effect=DeployError("storage_probe_failed", "storage outage")) as probe:
            self.assertTrue(release.config_set(self.host, self.project, self.profile, changes)["unchanged"])
            release.config_set(self.host, self.project, self.profile, {"PROJECT__NAME": "Changed", **changes})
            probe.assert_not_called()
        credentials = json.loads(changes["APP__SERVICES__CREDENTIALS"])
        credentials["app"]["secret_key"] = "replacement-secret"
        with patch.object(release, "probe_storage", wraps=release.probe_storage) as probe:
            release.config_set(self.host, self.project, self.profile,
                               {"APP__SERVICES__CREDENTIALS": json.dumps(credentials)})
        probe.assert_called_once()

    def test_rollback_with_storage_bindings_never_probes(self):
        first, one = self.bundle(1)
        second, two = self.bundle(2)
        self.deploy(first)
        release.config_set(self.host, self.project, self.profile, self.storage_changes("old/"))
        self.deploy(second)
        with patch.object(release, "probe_storage", side_effect=AssertionError("rollback must not probe")) as probe:
            release.rollback(self.host, self.project, self.profile, one["version"], two["version"], str(uuid.uuid4()))
        probe.assert_not_called()

    def test_recovery_in_both_modes_checks_schema_without_migrations_seeds_or_backups(self):
        for sequence, http_only in enumerate((True, False), start=1):
            with self.subTest(http_only=http_only):
                archive, manifest = self.bundle(sequence)
                self.host.commands.clear()
                with patch.object(release.backup, "recovery_point") as point, \
                        patch.object(release.backup, "archive_keys") as keys:
                    result = release.activate_recovered(self.host, self.registry, self.entry,
                                                        archive, digest(archive), http_only=http_only)
                point.assert_not_called()
                keys.assert_not_called()
                commands = [args for args, _ in self.host.commands]
                self.assertEqual(sum("migrate:status" in args for args in commands), 1)
                self.assertFalse(any("migrate" in args or "db:seed" in args for args in commands))
                self.assertEqual(result["version"], manifest["version"])
                self.assertEqual(self.state()["current"], manifest)

    def test_recovery_refuses_pending_and_unknown_applied_migrations_with_commit_guidance(self):
        archive, _ = self.bundle()
        cases = [("pending", []), ("missing_definition", [{"code": "definition_missing"}]),
                 ("missing_definition", []), ("changed", [{"code": "definition_changed"}]),
                 ("incomplete", [{"code": "reconciliation_required"}])]
        for http_only in (True, False):
            for commit in (None, "a" * 40):
                for state, problems in cases:
                    with self.subTest(http_only=http_only, commit=commit, state=state):
                        self.entry["recovered_from"] = {"release_commit": commit}
                        def app(host, entry, directory, command, *args, **kwargs):
                            self.assertIn(command, ("config:check", "migrate:status"))
                            return subprocess.CompletedProcess([], 0, json.dumps({"targets": [{"schema": "public",
                                "migrations": [{"state": state}], "problems": problems}]}), "")
                        with patch.object(release, "app", side_effect=app), \
                                patch.object(release, "switch_to") as switch, \
                                patch.object(release, "start_roles") as start:
                            with self.assertRaises(DeployError) as error:
                                release.activate_recovered(self.host, self.registry, self.entry, archive,
                                                           digest(archive), http_only=http_only)
                        self.assertEqual(error.exception.code, "recovery_schema_mismatch")
                        self.assertIn(commit or "commit is unknown", error.exception.detail)
                        switch.assert_not_called()
                        start.assert_not_called()
                        self.assertIsNone(self.state()["current"])

    def state(self):
        return self.host.load_state(self.entry)

    def stop_after_persist(self, owner, name, predicate, operation):
        """Exit a child without unwinding finally blocks, then reload its files."""
        original = getattr(owner, name)
        pid = os.fork()
        if pid == 0:
            def boundary(*args, **kwargs):
                result = original(*args, **kwargs)
                if predicate(*args, **kwargs):
                    os._exit(77)
                return result
            try:
                with patch.object(owner, name, side_effect=boundary):
                    operation()
            except BaseException:
                os._exit(78)
            os._exit(79)
        _, status = os.waitpid(pid, 0)
        self.assertEqual(77, os.waitstatus_to_exitcode(status), "persisted boundary was not reached")

    def encryption_configuration(self):
        values = runtime_environment(self.entry, initialize_bootstrap_secrets(self.host, self.entry))
        atomic_write(self.env_file, dump_env(values), 0o640)
        self.host.inspect_output = json.dumps([
            {"Name": key.replace("__", ".").lower(), "Source": "defaults", "Secret": True}
            for key in ENCRYPTION_KEYS] + [{"Name": "project.name", "Source": "defaults", "Secret": False}])
        return values

    def test_key_archive_survives_publication_exit_and_blocks_reconcile_restart_and_correction(self):
        self.encryption_configuration()
        archive, _ = self.bundle()
        self.deploy(archive)
        operation = str(uuid.uuid4())
        self.stop_after_persist(release.os, "replace", lambda source, target: Path(target) == self.env_file,
                                lambda: release.keys_rotate(self.host, self.project, self.profile, operation))
        self.assertTrue(self.state()["key_archive"]["pending"])
        with patch.object(release.backup, "archive_keys", side_effect=DeployError("archive_failed", "fixture")):
            self.host.commands.clear()
            with self.assertRaises(DeployError):
                release.reconcile(self.host, self.project, self.profile)
            self.assertTrue(self.state()["key_archive"]["pending"])
            self.assertEqual("failed_applied", self.state()["last_attempt"]["outcome"])
            _, entry = self.host.require_identity(self.project, self.profile)
            with self.assertRaises(DeployError):
                release.restart(self.host, entry)
            with self.assertRaises(DeployError):
                release.config_set(self.host, self.project, self.profile, {"PROJECT__NAME": "Corrected"})
            self.assertFalse(any(args[:2] in (["systemctl", "restart"], ["systemctl", "start"])
                                 for args, _ in self.host.commands))
        release.config_set(self.host, self.project, self.profile, {"PROJECT__NAME": "Corrected"})
        self.assertFalse(self.state()["key_archive"]["pending"])
        self.assertEqual(2, self.state()["key_archive"]["configuration_revision"])
        self.assertEqual(3, self.state()["configuration_revision"])

    def test_key_archive_completion_exit_repeats_archive_before_restart(self):
        self.encryption_configuration()
        archive, _ = self.bundle()
        self.deploy(archive)
        self.stop_after_persist(release.backup, "archive_keys", lambda *args: True,
                                lambda: release.keys_rotate(self.host, self.project, self.profile, str(uuid.uuid4())))
        self.assertTrue(self.state()["key_archive"]["pending"])
        release.backup.archive_keys.reset_mock()
        release.reconcile(self.host, self.project, self.profile)
        release.backup.archive_keys.assert_called_once()
        self.assertFalse(self.state()["key_archive"]["pending"])

    def test_success_journals_final_acl_for_deploy_config_and_rollback(self):
        first, one = self.bundle(1)
        second, two = self.bundle(2)
        self.deploy(first)
        operations = [lambda: self.deploy(second),
                      lambda: release.config_set(self.host, self.project, self.profile, {"PROJECT__NAME": "Changed"}),
                      lambda: release.rollback(self.host, self.project, self.profile, one["version"], two["version"], str(uuid.uuid4()))]
        for operation in operations:
            with self.subTest(operation=operations.index(operation)):
                self.stop_after_persist(self.host, "save_state", lambda entry, state:
                                        state["last_attempt"]["outcome"] == "succeeded", operation)
                attempt = self.state()["last_attempt"]
                self.assertTrue(attempt["redis_pending"])
                self.assertEqual(attempt["redis_inventory"], attempt["redis_target"])
                self.grants.reset_mock()
                release.reconcile(self.host, self.project, self.profile)
                self.assertEqual(attempt["redis_target"]["patterns"], self.grants.call_args.args[3])
                self.assertFalse(self.state()["last_attempt"]["redis_pending"])

    def test_next_mutation_finishes_acl_before_replacing_attempt(self):
        archive, _ = self.bundle()
        self.stop_after_persist(self.host, "save_state", lambda entry, state:
                                state["last_attempt"]["outcome"] == "succeeded", lambda: self.deploy(archive))
        self.grants.reset_mock()
        release.config_set(self.host, self.project, self.profile, {"PROJECT__NAME": "Changed"})
        self.assertEqual(3, self.grants.call_count)  # old narrowing, new widening, new narrowing
        self.assertFalse(self.state()["last_attempt"]["redis_pending"])

    def test_terminal_configuration_candidates_are_cleaned_by_hash(self):
        archive, _ = self.bundle()
        self.deploy(archive)
        for changes in ({"PROJECT__NAME": self.entry["allocations"]["namespace"]["application"]},
                        {"UNKNOWN__KEY": "invalid"}):
            with self.subTest(changes=list(changes)):
                self.stop_after_persist(self.host, "save_state", lambda entry, state:
                                        state["last_attempt"]["outcome"] in ("succeeded", "failed"),
                                        lambda: release.config_set(self.host, self.project, self.profile, changes))
                candidate = self.env_file.with_name("app.env.next")
                self.assertTrue(candidate.exists())
                original = candidate.read_bytes()
                candidate.write_bytes(b"foreign")
                with self.assertRaises(DeployError):
                    release.reconcile(self.host, self.project, self.profile)
                self.assertEqual(b"foreign", candidate.read_bytes())
                candidate.write_bytes(original)
                self.assertFalse(release.reconcile(self.host, self.project, self.profile)["unchanged"])
                self.assertFalse(candidate.exists())
                self.assertTrue(release.reconcile(self.host, self.project, self.profile)["unchanged"])

    def test_backup_failure_recovers_only_previously_running_roles(self):
        first, _ = self.bundle(1)
        second, _ = self.bundle(2)
        self.deploy(first)
        units = instance_units(self.entry)
        self.host.role_states[units["scheduler"]] = "active"
        self.host.role_states[units["worker"]] = "inactive"
        self.host.pending, self.host.fail = True, "recovery_point"
        self.host.commands.clear()
        with self.assertRaises(DeployError):
            self.deploy(second)
        self.assertEqual("active", self.host.role_states[units["scheduler"]])
        self.assertFalse(self.state()["last_attempt"]["role_recovery_pending"])
        self.assertEqual("inactive", self.host.role_states[units["worker"]])
        self.assertFalse(any(args[:2] == ["systemctl", "restart"] for args, _ in self.host.commands))

    def test_pre_migration_exit_and_failed_role_recovery_remain_reconcilable(self):
        first, _ = self.bundle(1)
        second, _ = self.bundle(2)
        self.deploy(first)
        scheduler = instance_units(self.entry)["scheduler"]
        self.host.role_states[scheduler] = "active"
        self.host.pending = True
        self.stop_after_persist(self.host, "save_state", lambda entry, state:
                                state["last_attempt"]["phase"] == "backup", lambda: self.deploy(second))
        self.host.role_states[scheduler] = "inactive"
        self.host.fail = "start"
        with self.assertRaises(DeployError):
            release.reconcile(self.host, self.project, self.profile)
        self.assertTrue(self.state()["last_attempt"]["role_recovery_pending"])
        self.host.fail = None
        release.reconcile(self.host, self.project, self.profile)
        self.assertEqual("active", self.host.role_states[scheduler])
        self.assertFalse(self.state()["last_attempt"]["role_recovery_pending"])
        self.assertTrue(release.reconcile(self.host, self.project, self.profile)["unchanged"])

    def test_rotation_replays_prepared_candidate_and_success_after_lost_response(self):
        self.encryption_configuration()
        archive, _ = self.bundle()
        self.deploy(archive)
        operation = str(uuid.uuid4())
        rotate = lambda: release.keys_rotate(self.host, self.project, self.profile, operation)
        self.stop_after_persist(release, "_save_operation", lambda host, entry, receipt:
                                receipt.get("outcome") == "prepared", rotate)
        path = release._operation_path(self.host, self.entry, operation)
        prepared = json.loads(path.read_text())
        encryption = {"key_id": prepared["changes"][ENCRYPTION_KEYS[0]], "key": prepared["changes"][ENCRYPTION_KEYS[1]]}
        self.assertEqual(0o600, path.stat().st_mode & 0o777)
        self.stop_after_persist(self.host, "save_state", lambda entry, state:
                                state["last_attempt"]["outcome"] == "succeeded", rotate)
        result = rotate()
        self.assertEqual(prepared["result"], result)
        self.assertEqual(encryption["key"], parse_env(self.env_file.read_text())[ENCRYPTION_KEYS[1]])
        self.assertEqual(result, rotate())
        self.assertEqual(result, release.keys_rotate(self.host, self.project, self.profile, operation, encryption))
        self.assertEqual(1, len(json.loads(parse_env(self.env_file.read_text())[ENCRYPTION_KEYS[2]])))
        self.assertNotIn(encryption["key"], json.dumps(release.public_state(self.state())))
        with self.assertRaises(DeployError):
            release.keys_rotate(self.host, self.project, self.profile, operation, {"key_id": "other", "key": "other"})

    def test_failed_rotation_replays_original_error_after_corrective_revision(self):
        self.encryption_configuration()
        archive, _ = self.bundle()
        self.deploy(archive)
        operation = str(uuid.uuid4())
        with patch.object(release.backup, "archive_keys", side_effect=DeployError("archive_failed", "fixture")):
            with self.assertRaises(DeployError) as initial:
                release.keys_rotate(self.host, self.project, self.profile, operation)
        release.config_set(self.host, self.project, self.profile, {"PROJECT__NAME": "Corrected"})
        with self.assertRaises(DeployError) as replay:
            release.keys_rotate(self.host, self.project, self.profile, operation)
        self.assertEqual(initial.exception.code, replay.exception.code)

    def test_unpublished_rotation_cancels_archive_obligation_without_claiming_rotation(self):
        self.encryption_configuration()
        archive, _ = self.bundle()
        self.deploy(archive)
        before = self.env_file.read_bytes()
        operation = str(uuid.uuid4())
        self.stop_after_persist(self.host, "save_state", lambda entry, state:
                                state["last_attempt"]["phase"] == "configuration",
                                lambda: release.keys_rotate(self.host, self.project, self.profile, operation))
        self.assertTrue(self.state()["key_archive"]["pending"])
        release.backup.archive_keys.reset_mock()
        release.reconcile(self.host, self.project, self.profile)
        release.backup.archive_keys.assert_not_called()
        self.assertEqual(before, self.env_file.read_bytes())
        self.assertNotIn("key_archive", self.state())
        self.assertFalse(self.env_file.with_name("app.env.next").exists())
        with self.assertRaises(DeployError) as replay:
            release.keys_rotate(self.host, self.project, self.profile, operation)
        self.assertEqual("rotation_not_applied", replay.exception.code)

    def test_next_config_cleans_terminal_candidate_and_preserves_rotation_receipt(self):
        self.encryption_configuration()
        archive, _ = self.bundle()
        self.deploy(archive)
        operation = str(uuid.uuid4())
        self.stop_after_persist(self.host, "save_state", lambda entry, state:
                                state["last_attempt"]["outcome"] == "succeeded",
                                lambda: release.keys_rotate(self.host, self.project, self.profile, operation))
        result = json.loads(release._operation_path(self.host, self.entry, operation).read_text())["result"]
        release.config_set(self.host, self.project, self.profile, {"PROJECT__NAME": "Changed"})
        self.assertEqual(result, release.keys_rotate(self.host, self.project, self.profile, operation))
        self.stop_after_persist(self.host, "save_state", lambda entry, state:
                                state["last_attempt"]["outcome"] == "succeeded",
                                lambda: release.config_set(self.host, self.project, self.profile, {"PROJECT__NAME": "Changed"}))
        self.assertTrue(self.env_file.with_name("app.env.next").exists())
        release.config_set(self.host, self.project, self.profile, {"PROJECT__NAME": "Again"})
        self.assertFalse(self.env_file.with_name("app.env.next").exists())

    def test_failed_role_restoration_is_durable_after_terminal_failure(self):
        first, _ = self.bundle(1)
        second, _ = self.bundle(2)
        self.deploy(first)
        scheduler = instance_units(self.entry)["scheduler"]
        self.host.role_states[scheduler] = "active"
        self.host.pending, self.host.fail = True, "start"
        with patch.object(release.backup, "recovery_point", side_effect=DeployError("backup_failed", "fixture")):
            self.stop_after_persist(self.host, "save_state", lambda entry, state:
                                    state["last_attempt"]["outcome"] == "failed", lambda: self.deploy(second))
        self.assertTrue(self.state()["last_attempt"]["role_recovery_pending"])
        self.host.role_states[scheduler], self.host.fail = "inactive", None
        release.reconcile(self.host, self.project, self.profile)
        self.assertEqual("active", self.host.role_states[scheduler])
        self.assertFalse(self.state()["last_attempt"]["role_recovery_pending"])

    def process_observations(self):
        archive, manifest = self.bundle()
        self.deploy(archive)
        executable = self.opt / "releases" / manifest["version"] / "artisan"
        observations = {}
        for number, (role, unit) in enumerate(instance_units(self.entry).items(), 42):
            proc = self.host.paths.p(f"/proc/{number}")
            proc.mkdir(parents=True)
            (proc / "exe").symlink_to(executable)
            (proc / "stat").write_text(f"{number} (artisan) " + " ".join(["S"] + ["0"] * 18 + [str(number * 100)]))
            group = "/system.slice/" + unit
            (proc / "cgroup").write_text("0::" + group + "\n")
            observations[unit] = ("LoadState=loaded\nActiveState=active\nSubState=running\n"
                                  f"NRestarts=0\nMainPID={number}\nControlGroup={group}")
        return observations

    def test_first_activation_repeat_noop_and_instance_only_restart(self):
        archive, manifest = self.bundle()
        self.deploy(archive)
        commands = len(self.host.commands)
        state_bytes = (self.host.paths.instance_state(self.entry["name"]) / "state.json").read_bytes()
        self.assertTrue(self.deploy(archive)["unchanged"])
        self.assertTrue(all(args[:2] == ["systemctl", "is-enabled"] for args, _ in self.host.commands[commands:]))
        self.assertEqual(state_bytes, (self.host.paths.instance_state(self.entry["name"]) / "state.json").read_bytes())
        self.assertEqual(manifest, self.state()["current"])
        self.assertEqual("healthy", self.state()["status"])
        for args, kwargs in self.host.commands:
            if "restart" in args:
                self.assertEqual(["systemctl", "restart", instance_units(self.entry)["http"]], args)
            if args[0] == "runuser" and Path(args[4]).name == "artisan":
                self.assertEqual(self.host.paths.data(self.entry["name"]), kwargs["cwd"])
            self.assertNotIn(args[0], ("go", "gcc", "make", "apt-get", "npm"))
        self.assertNotIn(self.secret, json.dumps(self.state()))

    def test_background_starts_after_http_readiness_and_failed_http_keeps_it_stopped(self):
        self.select_fixture_release()
        units = instance_units(self.entry)
        self.health.return_value = False
        with self.assertRaises(DeployError) as failure:
            release.restart(self.host, self.entry)
        self.assertEqual("readiness_failed", failure.exception.code)
        self.assertFalse(any(args[:2] == ["systemctl", "start"] for args, _ in self.host.commands))
        self.host.commands.clear()
        self.health.return_value = True
        release.restart(self.host, self.entry)
        calls = [args for args, _ in self.host.commands]
        self.assertLess(calls.index(["systemctl", "restart", units["http"]]), calls.index(["systemctl", "start", units["worker"]]))
        release.stop(self.host, self.entry)
        self.assertEqual(self.host.commands[-2][0], ["systemctl", "stop", units["scheduler"], units["worker"]])
        self.assertEqual(self.host.commands[-1][0], ["systemctl", "stop", units["http"]])

    def select_running_roles(self, roles):
        from test_manage import manager
        import upgrades
        request = {**upgrades.protocol_metadata(), "project": self.project, "profile": self.profile}
        manager.execute(self.host, "stop", request)
        for role in roles:
            manager.execute(self.host, "restart", {**request, "role": role})
        self.host.commands.clear()
        self.health.reset_mock()

    def assert_preserved_roles(self, roles):
        units = instance_units(self.entry)
        self.assertEqual({units[role] for role in roles}, self.host.enabled_roles)
        mutations = [args for args, _ in self.host.commands
                     if args[:1] == ["systemctl"] and args[1] in {"start", "restart", "stop", "enable", "disable", "reset-failed"}]
        touched = {arg for args in mutations for arg in args[2:] if arg in units.values()}
        self.assertLessEqual(touched, {units[role] for role in roles})
        self.assertFalse(any(args[1] in {"enable", "disable"} for args in mutations))
        self.assertEqual("degraded" if roles else "stopped", self.state()["status"])
        self.assertEqual(roles, self.state()["last_attempt"]["roles"])
        if not roles:
            self.health.assert_not_called()
        for call in self.health.call_args_list:
            self.assertTrue(call.kwargs.get("include_background") is False or call.kwargs.get("roles") == roles)

    def test_config_rotation_and_rollback_preserve_enablement_and_replay(self):
        self.encryption_configuration()
        one, first = self.bundle(1)
        two, second = self.bundle(2)
        self.deploy(one)
        for roles in ([], ["http"]):
            for action in ("config", "rotation", "rollback"):
                with self.subTest(roles=roles, action=action):
                    self.deploy(two)
                    self.select_running_roles(roles)
                    operation = str(uuid.uuid4())
                    if action == "config":
                        invoke = lambda: release.config_set(self.host, self.project, self.profile, {"PROJECT__NAME": operation})
                    elif action == "rotation":
                        invoke = lambda: release.keys_rotate(self.host, self.project, self.profile, operation)
                    else:
                        invoke = lambda: release.rollback(self.host, self.project, self.profile,
                                                          first["version"], second["version"], operation)
                    with patch.object(release, "prepare_worker", side_effect=AssertionError("disabled worker prepared")):
                        result = invoke()
                    self.assertTrue(result["ok"])
                    self.assert_preserved_roles(roles)
                    if action == "rollback":
                        self.assertEqual(first, self.state()["current"])
                    elif action == "config":
                        self.assertEqual(operation, parse_env(self.env_file.read_text())["PROJECT__NAME"])
                    if action in ("rotation", "rollback"):
                        self.host.commands.clear()
                        self.assertTrue(invoke()["ok"])
                        self.assert_preserved_roles(roles)

    def test_partial_role_failure_paths_do_not_enable_or_start_backgrounds(self):
        self.encryption_configuration()
        one, first = self.bundle(1)
        two, second = self.bundle(2)
        self.deploy(one)
        for roles in ([], ["http"]):
            for action in ("config", "rotation", "rollback"):
                with self.subTest(roles=roles, action=action):
                    self.deploy(two)
                    self.select_running_roles(roles)
                    operation = str(uuid.uuid4())
                    original_replace, original_switch = release.os.replace, release.switch_to
                    def replace(source, target):
                        if Path(target) == self.env_file:
                            raise OSError("fixture publication failure")
                        return original_replace(source, target)
                    def switch(host, entry, version):
                        original_switch(host, entry, version)
                        if version == first["version"]:
                            raise DeployError("fixture_switch_failure", "after switch")
                    with patch.object(release.os, "replace", side_effect=replace), \
                            patch.object(release, "switch_to", side_effect=switch), \
                            patch.object(release, "prepare_worker", side_effect=AssertionError("disabled worker prepared")):
                        with self.assertRaises((DeployError, OSError)):
                            if action == "config":
                                release.config_set(self.host, self.project, self.profile, {"PROJECT__NAME": operation})
                            elif action == "rotation":
                                release.keys_rotate(self.host, self.project, self.profile, operation)
                            else:
                                release.rollback(self.host, self.project, self.profile, first["version"], second["version"], operation)
                    self.assert_preserved_roles(roles)

    def test_interrupted_changes_reconcile_only_the_recorded_enabled_roles(self):
        self.encryption_configuration()
        one, first = self.bundle(1)
        two, second = self.bundle(2)
        self.deploy(one)
        for roles in ([], ["http"]):
            for action in ("config", "rotation", "rollback"):
                with self.subTest(roles=roles, action=action):
                    self.deploy(two)
                    self.select_running_roles(roles)
                    original = release.phase
                    def phase(host, entry, state, name):
                        original(host, entry, state, name)
                        if name == "health":
                            raise KeyboardInterrupt()
                    with patch.object(release, "phase", side_effect=phase), self.assertRaises(KeyboardInterrupt):
                        if action == "config":
                            release.config_set(self.host, self.project, self.profile, {"PROJECT__NAME": str(uuid.uuid4())})
                        elif action == "rotation":
                            release.keys_rotate(self.host, self.project, self.profile, str(uuid.uuid4()))
                        else:
                            release.rollback(self.host, self.project, self.profile, first["version"], second["version"], str(uuid.uuid4()))
                    self.assertEqual(roles, self.state()["last_attempt"]["roles"])
                    self.assertEqual({instance_units(self.entry)[role] for role in roles}, self.host.enabled_roles)
                    with patch.object(release, "prepare_worker", side_effect=AssertionError("disabled worker prepared")):
                        release.reconcile(self.host, self.project, self.profile)
                    self.assert_preserved_roles(roles)

    def test_legacy_config_attempt_without_roles_keeps_all_role_recovery(self):
        archive, _ = self.bundle()
        self.deploy(archive)
        self.select_running_roles(["http"])
        original = release.phase
        def phase(host, entry, state, name):
            original(host, entry, state, name)
            if name == "health":
                raise KeyboardInterrupt()
        with patch.object(release, "phase", side_effect=phase), self.assertRaises(KeyboardInterrupt):
            release.config_set(self.host, self.project, self.profile, {"PROJECT__NAME": "Legacy attempt"})
        state = self.state()
        state["last_attempt"].pop("roles")
        self.host.save_state(self.entry, state)
        release.reconcile(self.host, self.project, self.profile)
        self.assertEqual(set(instance_units(self.entry).values()), self.host.enabled_roles)
        self.assertEqual("healthy", self.state()["status"])

    def test_enabled_roles_come_from_units_even_when_status_is_stale(self):
        archive, _ = self.bundle()
        self.deploy(archive)
        self.select_running_roles(["http"])
        state = self.state()
        state["status"] = "failed"
        self.host.save_state(self.entry, state)
        self.assertEqual(["http"], release.enabled_roles(self.host, self.entry))
        release.config_set(self.host, self.project, self.profile, {"PROJECT__NAME": "Repaired"})
        self.assert_preserved_roles(["http"])
        for output, code in (("unknown", 1), ("masked", 1), ("static", 0), ("", 1), ("enabled", 1)):
            with self.subTest(output=output, code=code):
                with patch.object(self.host, "run", return_value=subprocess.CompletedProcess([], code, output, "")):
                    with self.assertRaises(DeployError) as caught:
                        release.enabled_roles(self.host, self.entry)
                self.assertEqual("service_enablement", caught.exception.code)
        with patch.object(self.host, "run", side_effect=TimeoutError("is-enabled timed out")):
            with self.assertRaises(TimeoutError):
                release.enabled_roles(self.host, self.entry)

    def test_configuration_crash_and_worker_prepare_failure_leave_enablement_intact(self):
        archive, _ = self.bundle()
        self.deploy(archive)
        units = set(instance_units(self.entry).values())
        original = release.phase
        def phase(host, entry, state, name):
            original(host, entry, state, name)
            if name == "configuration":
                raise KeyboardInterrupt()
        with patch.object(release, "phase", side_effect=phase), self.assertRaises(KeyboardInterrupt):
            release.config_set(self.host, self.project, self.profile, {"PROJECT__NAME": "Crash"})
        self.assertEqual(units, self.host.enabled_roles)
        release.reconcile(self.host, self.project, self.profile)
        with patch.object(release, "prepare_worker", side_effect=DeployError("worker_configuration", "fixture")), \
                self.assertRaises(DeployError):
            release.config_set(self.host, self.project, self.profile, {"PROJECT__NAME": "Prepare failure"})
        self.assertEqual(units, self.host.enabled_roles)

    def test_deploy_enablement_failure_stays_pending_instead_of_unsafe_restore(self):
        one, _ = self.bundle(1)
        two, second = self.bundle(2)
        self.deploy(one)
        self.select_running_roles(["http"])
        original = self.host.run
        def run(args, **kwargs):
            result = original(args, **kwargs)
            if args[:2] == ["systemctl", "enable"]:
                raise DeployError("command_failed", "enable completion uncertain")
            return result
        with patch.object(self.host, "run", side_effect=run), self.assertRaises(DeployError) as caught:
            self.deploy(two)
        self.assertEqual("role_enablement_pending", caught.exception.code)
        self.assertEqual(second, self.state()["current"])
        self.assertEqual("needs_reconciliation", self.state()["last_attempt"]["outcome"])
        self.assertFalse(any(args[:2] == ["systemctl", "disable"] for args, _ in self.host.commands))
        with self.assertRaises(DeployError) as caught:
            release.config_set(self.host, self.project, self.profile, {"PROJECT__NAME": "Wait for reconcile"})
        self.assertEqual("reconciliation_required", caught.exception.code)
        release.reconcile(self.host, self.project, self.profile)
        self.assertEqual("healthy", self.state()["status"])

    def test_failed_deploy_preserves_web_only_roles_before_later_config(self):
        one, first = self.bundle(1)
        two, _ = self.bundle(2)
        self.deploy(one)
        for failure in ("config:check", "health"):
            with self.subTest(failure=failure):
                self.select_running_roles(["http"])
                if failure == "config:check":
                    self.host.fail = failure
                else:
                    self.health.side_effect = [False, True, True]
                with self.assertRaises(DeployError):
                    self.deploy(two)
                self.host.fail = None
                self.health.side_effect = None
                self.assertEqual({instance_units(self.entry)["http"]}, self.host.enabled_roles)
                self.assertEqual("degraded", self.state()["status"])
                self.assertEqual(first, self.state()["current"])
                self.host.commands.clear()
                self.health.reset_mock()
                with patch.object(release, "prepare_worker", side_effect=AssertionError("disabled worker prepared")):
                    release.config_set(self.host, self.project, self.profile, {"PROJECT__NAME": failure})
                self.assert_preserved_roles(["http"])

    def test_deploy_reconcile_starts_all_only_after_reaching_target(self):
        one, first = self.bundle(1)
        two, target = self.bundle(2)
        self.deploy(one)
        for failure in ("db:seed", "migrate", "before_switch", "after_switch", "health"):
            with self.subTest(failure=failure):
                self.select_running_roles(["http"])
                original = release.switch_to
                def switch(host, entry, version):
                    if failure == "after_switch":
                        original(host, entry, version)
                    raise KeyboardInterrupt()
                if failure in ("db:seed", "migrate"):
                    self.host.pending = failure == "migrate"
                    self.host.fail = failure
                    with self.assertRaises(DeployError):
                        self.deploy(two)
                elif failure == "health":
                    self.host.interrupt = "restart"
                    with self.assertRaises(KeyboardInterrupt):
                        self.deploy(two)
                else:
                    with patch.object(release, "switch_to", side_effect=switch), self.assertRaises(KeyboardInterrupt):
                        self.deploy(two)
                self.host.fail = self.host.interrupt = None
                self.host.pending = False
                self.host.commands.clear()
                reached = failure in ("after_switch", "health")
                release.reconcile(self.host, self.project, self.profile)
                units = instance_units(self.entry)
                self.assertEqual(set(units.values()) if reached else {units["http"]}, self.host.enabled_roles)
                self.assertEqual("healthy" if reached else "degraded", self.state()["status"])
                self.assertEqual(target if reached else first, self.state()["current"])
                if not reached:
                    mutations = [args for args, _ in self.host.commands
                                 if args[:2] in (["systemctl", "start"], ["systemctl", "restart"], ["systemctl", "enable"])]
                    self.assertFalse(any(unit in args for args in mutations for unit in (units["worker"], units["scheduler"])))
                    self.assertFalse(any(args[:2] == ["systemctl", "enable"] for args in mutations))
                else:
                    self.deploy(one)

    def test_failed_first_deploy_records_failed_without_current(self):
        archive, _ = self.bundle()
        for failure in ("config:check", "db:seed", "migrate"):
            with self.subTest(failure=failure):
                self.host.fail = failure
                self.host.pending = failure == "migrate"
                with self.assertRaises(DeployError):
                    self.deploy(archive)
                self.assertIsNone(self.state()["current"])
                self.assertEqual("failed", self.state()["status"])
                self.host.fail = None
                self.host.pending = False
                release.reconcile(self.host, self.project, self.profile)
                self.assertEqual("failed", self.state()["status"])
        self.host.interrupt = "config:check"
        with self.assertRaises(KeyboardInterrupt):
            self.deploy(archive)
        self.host.interrupt = None
        release.reconcile(self.host, self.project, self.profile)
        self.assertIsNone(self.state()["current"])
        self.assertEqual("failed", self.state()["status"])

    def test_healthy_deploy_restore_does_not_stop_http(self):
        one, first = self.bundle(1)
        two, _ = self.bundle(2)
        self.deploy(one)
        self.host.commands.clear()
        self.health.side_effect = [False, True, True]
        with self.assertRaises(DeployError):
            self.deploy(two)
        self.assertEqual(first, self.state()["current"])
        self.assertEqual("rolled_back", self.state()["status"])
        self.assertEqual(set(instance_units(self.entry).values()), self.host.enabled_roles)
        self.assertFalse(any(args[:2] == ["systemctl", "stop"] and instance_units(self.entry)["http"] in args
                             for args, _ in self.host.commands))

    def test_failed_all_role_restart_reconcile_allows_operator_recovery(self):
        from test_manage import manager
        import upgrades
        archive, _ = self.bundle()
        self.deploy(archive)
        request = {**upgrades.protocol_metadata(), "project": self.project, "profile": self.profile}
        self.host.interrupt = "restart"
        with self.assertRaises(KeyboardInterrupt):
            manager.execute(self.host, "restart", request)
        self.host.interrupt = None
        self.health.return_value = False
        with self.assertRaises(DeployError):
            release.reconcile(self.host, self.project, self.profile)
        self.assertEqual("failed", self.state()["last_attempt"]["outcome"])
        manager.execute(self.host, "stop", request)
        self.assertEqual("stopped", self.state()["status"])
        self.health.return_value = True
        manager.execute(self.host, "restart", request)
        self.assertEqual("healthy", self.state()["status"])

    def test_naming_the_only_declared_role_selects_the_whole_instance(self):
        from test_manage import manager
        from topology import validate_topology
        import upgrades
        archive, _ = self.bundle()
        self.deploy(archive)
        registry = self.host.read_registry()
        registry["instances"][self.entry["profile"]["instance_id"]]["topology"] = validate_topology({"schema": 1, "roles": [{"name": "http"}]})
        self.host.write_registry(registry)
        request = {**upgrades.protocol_metadata(), "project": self.project, "profile": self.profile}
        manager.execute(self.host, "restart", {**request, "role": "http"})
        self.assertEqual("healthy", self.state()["status"])
        self.assertEqual("all", self.state()["last_attempt"]["role"])
        manager.execute(self.host, "stop", {**request, "role": "http"})
        self.assertEqual("stopped", self.state()["status"])

    def test_reconcile_preserves_named_role_and_defaults_legacy_attempts_to_all(self):
        from test_manage import manager
        import upgrades
        archive, _ = self.bundle()
        self.deploy(archive)
        units = instance_units(self.entry)
        request = {**upgrades.protocol_metadata(), "project": self.project, "profile": self.profile}
        for action, role in (("restart", "http"), ("stop", "worker"), ("restart", "all"), ("stop", "all")):
            self.host.interrupt = action
            with self.assertRaises(KeyboardInterrupt):
                manager.execute(self.host, action, {**request, "role": role})
            self.host.interrupt = None
            self.assertEqual(role, self.state()["last_attempt"]["role"])
            if role == "all":
                state = self.state()
                state["last_attempt"].pop("role")
                self.host.save_state(self.entry, state)
            self.host.commands.clear()
            result = release.reconcile(self.host, self.project, self.profile)
            self.assertEqual("degraded" if role != "all" else "healthy" if action == "restart" else "stopped", result["status"])
            touched = {arg for args, _ in self.host.commands for arg in args if arg in units.values()}
            self.assertEqual({units[role]} if role != "all" else set(units.values()), touched)
            self.assertEqual("reconciled", self.state()["last_attempt"]["outcome"])

    def test_failed_named_restart_keeps_stopped_roles_protected(self):
        from test_manage import manager
        import upgrades
        archive, _ = self.bundle()
        self.deploy(archive)
        request = {**upgrades.protocol_metadata(), "project": self.project, "profile": self.profile}
        manager.execute(self.host, "stop", {**request, "role": "worker"})
        self.health.return_value = False
        with self.assertRaises(DeployError):
            manager.execute(self.host, "restart", {**request, "role": "http"})
        self.assertEqual("degraded", self.state()["status"])
        self.health.return_value = True
        release.config_set(self.host, self.project, self.profile, {"DEVELOPER__ENABLED": "true"})
        self.assertNotIn(instance_units(self.entry)["worker"], self.host.enabled_roles)
        self.health.return_value = False
        self.host.interrupt = "restart"
        with self.assertRaises(KeyboardInterrupt):
            manager.execute(self.host, "restart", {**request, "role": "http"})
        self.host.interrupt = None
        with self.assertRaises(DeployError):
            release.reconcile(self.host, self.project, self.profile)
        self.assertEqual("degraded", self.state()["status"])
        self.assertEqual("failed", self.state()["last_attempt"]["outcome"])
        self.health.return_value = True
        self.host.commands.clear()
        manager.execute(self.host, "restart", {**request, "role": "http"})
        self.assertNotIn(instance_units(self.entry)["worker"], self.host.enabled_roles)
        self.assertFalse(any(instance_units(self.entry)["worker"] in args for args, _ in self.host.commands))

    def test_stop_survives_reboot_bootstrap_and_selected_restart(self):
        from test_manage import manager
        import upgrades
        import provision
        archive, _ = self.bundle()
        self.deploy(archive)
        units = instance_units(self.entry)
        request = {**upgrades.protocol_metadata(), "project": self.project, "profile": self.profile}
        manager.execute(self.host, "stop", {**request, "role": "worker"})
        self.assertEqual(set(units.values()) - {units["worker"]}, self.host.enabled_roles)
        with patch.object(provision, "_unit_checks"):
            provision._install_unit(self.host, {"shared": {"postgresql": {"unit": "postgresql@16-foundry.service"}}},
                                    self.entry, Path(__file__).resolve().parents[1])
        self.assertNotIn(units["worker"], self.host.enabled_roles)
        self.host.role_states = {unit: "active" for unit in self.host.enabled_roles}
        self.assertNotIn(units["worker"], self.host.role_states)
        manager.execute(self.host, "restart", {**request, "role": "http"})
        self.assertNotIn(units["worker"], self.host.enabled_roles)
        manager.execute(self.host, "restart", request)
        self.assertEqual(set(units.values()), self.host.enabled_roles)
        manager.execute(self.host, "stop", request)
        self.assertEqual(set(), self.host.enabled_roles)
        self.host.role_states = {unit: "active" for unit in self.host.enabled_roles}
        self.assertEqual({}, self.host.role_states)
        manager.execute(self.host, "restart", {**request, "role": "worker"})
        self.assertEqual({units["worker"]}, self.host.enabled_roles)
        self.deploy(archive)
        self.assertEqual(set(units.values()), self.host.enabled_roles)

    def test_reset_failed_on_a_never_loaded_unit_does_not_stop_the_start(self):
        self.select_fixture_release()
        units = instance_units(self.entry)
        run = self.host.run

        def refuse(args, **kwargs):
            if list(args)[:2] == ["systemctl", "reset-failed"] and kwargs.get("check", True):
                raise DeployError("command_failed", "Unit not loaded")
            return run(args, **kwargs)

        with patch.object(self.host, "run", refuse), patch.object(release, "prepare_worker"):
            release.start_roles(self.host, self.entry, roles=list(units), enable=False)
            self.assertEqual(set(units.values()), set(self.host.role_states))
            self.assertEqual(set(), self.host.enabled_roles)
            release.restart(self.host, self.entry, role="worker")
            self.assertEqual({units["worker"]}, self.host.enabled_roles)

    def test_named_roles_only_touch_selected_unit_and_wait_for_it(self):
        self.select_fixture_release()
        units = instance_units(self.entry)
        for role, unit in units.items():
            self.host.commands.clear()
            with patch.object(release, "prepare_worker") as worker:
                release.restart(self.host, self.entry, role=role)
                self.assertEqual(role == "worker", worker.called)
            self.assertEqual([["systemctl", "enable", unit], ["systemctl", "reset-failed", unit], ["systemctl", "restart", unit]],
                             [args for args, _ in self.host.commands])
            self.assertEqual(role, self.health.call_args.kwargs["role"])
            self.assertLessEqual(self.health.call_args.kwargs["timeout"], release.READINESS_SECONDS)
            release.stop(self.host, self.entry, role=role)
            self.assertEqual(["systemctl", "stop", unit], self.host.commands[-1][0])
        self.health.return_value = False
        with self.assertRaises(DeployError):
            release.restart(self.host, self.entry, role="http")

    def test_named_background_readiness_does_not_require_http(self):
        observations = self.process_observations()
        units = instance_units(self.entry)
        def command(args, **kwargs):
            self.assertEqual(["systemctl", "show", units["worker"]], args[:3])
            return subprocess.CompletedProcess(args, 0, observations[args[2]], "")
        with patch.object(self.host, "run", side_effect=command), \
                patch.object(release.urllib.request, "build_opener") as opener, \
                patch.object(release, "STABILITY_SECONDS", 0):
            self.assertTrue(CHECK_READY(self.host, self.entry, role="worker"))
            opener.return_value.open.assert_not_called()

    def test_named_stop_is_public_and_plain_restart_or_deploy_recovers(self):
        from test_manage import manager
        import upgrades
        archive, _ = self.bundle()
        self.deploy(archive)
        request = {**upgrades.protocol_metadata(), "project": self.project, "profile": self.profile}
        for recovery in ("restart", "deploy"):
            manager.execute(self.host, "stop", {**request, "role": "worker"})
            self.assertEqual("degraded", release.public_state(self.state())["status"])
            manager.execute(self.host, "restart", {**request, "role": "http"})
            self.assertEqual("degraded", self.state()["status"])
            if recovery == "restart":
                manager.execute(self.host, "restart", request)
            else:
                self.deploy(archive)
            self.assertEqual("healthy", self.state()["status"])

    def test_named_operation_does_not_resume_other_roles(self):
        from test_manage import manager
        import upgrades
        archive, _ = self.bundle()
        self.deploy(archive)
        state = self.state()
        state["last_attempt"].update(role_recovery_pending=True, prior_background={"worker": "active"})
        self.host.save_state(self.entry, state)
        request = {**upgrades.protocol_metadata(), "project": self.project, "profile": self.profile, "role": "http"}
        for action in ("restart", "stop"):
            self.host.commands.clear()
            with self.assertRaises(DeployError) as caught:
                manager.execute(self.host, action, request)
            self.assertEqual("role_recovery", caught.exception.code)
            self.assertEqual([], self.host.commands)

    def test_deploy_retries_deferred_certificate_even_on_unchanged_release(self):
        import certificates
        archive, _ = self.bundle()
        self.deploy(archive)
        with patch.object(certificates, "retry_deferred") as retry:
            self.deploy(archive)
            retry.assert_called_once()

    def test_checksum_and_identity_refusal_precedes_activation(self):
        archive, _ = self.bundle(mutate=lambda m: m.update(instance_id=str(uuid.uuid4())))
        with self.assertRaisesRegex(DeployError, "identity"):
            self.deploy(archive)
        self.assertEqual([], self.host.commands)
        self.assertIsNone(self.state()["last_attempt"])
        with self.assertRaises(DeployError) as result:
            release.activate(self.host, self.project, self.profile, archive, "0" * 64)
        self.assertEqual("archive_checksum", result.exception.code)

    def test_archive_traversal_rejected_before_any_payload_write(self):
        archive, _ = self.bundle(extra="../../escape")
        destination = self.root / "extract"
        with self.assertRaises(DeployError) as result:
            release.unpack_archive(archive, destination, self.entry, digest(archive))
        self.assertEqual("unsafe_archive", result.exception.code)
        self.assertFalse(destination.exists())

    def test_private_maps_private_during_extraction(self):
        archive, manifest = self.bundle()
        destination = self.root / "extract"
        release.unpack_archive(archive, destination, self.entry, digest(archive))
        self.assertEqual(0o700, (destination / "private-source-maps").stat().st_mode & 0o777)
        for path in manifest["files"]:
            if path.endswith(".map"):
                self.assertEqual(0o600, (destination / path).stat().st_mode & 0o777)

    def test_public_release_tree_is_traversable_under_a_hardened_umask(self):
        archive, manifest = self.bundle()
        destination = self.root / "extract"
        previous = os.umask(0o077)
        try:
            release.unpack_archive(archive, destination, self.entry, digest(archive))
        finally:
            os.umask(previous)
        for path in [destination, *destination.rglob("*")]:
            if path.is_dir():
                private = path.relative_to(destination).parts[:1] == ("private-source-maps",)
                self.assertEqual(0o700 if private else 0o755, path.stat().st_mode & 0o777, path)

    def test_backup_precedes_migration_failure_and_retry_requires_reconciliation(self):
        archive, _ = self.bundle()
        self.host.pending, self.host.fail = True, "migrate"
        with self.assertRaises(DeployError):
            self.deploy(archive)
        commands = [args for args, _ in self.host.commands]
        backup = next(i for i, args in enumerate(commands) if args[0] == "recovery_point")
        migration = next(i for i, args in enumerate(commands) if "migrate" in args)
        units = instance_units(self.entry)
        self.assertLess(commands.index(["systemctl", "stop", units["scheduler"], units["worker"]]), backup)
        self.assertLess(backup, migration)
        self.assertEqual("needs_reconciliation", self.state()["last_attempt"]["outcome"])
        self.assertEqual("migration", self.state()["last_attempt"]["phase"])
        self.assertIsNone(release.current_version(self.host, self.entry))
        self.assertEqual({"id": self.state()["last_attempt"]["recovery_operation_id"]}, self.state()["last_attempt"]["backup"])
        with self.assertRaises(DeployError) as result:
            self.deploy(archive)
        self.assertEqual("reconciliation_required", result.exception.code)




    def test_backup_failure_does_not_run_migrations(self):
        archive, _ = self.bundle()
        self.host.pending, self.host.fail = True, "recovery_point"
        with self.assertRaises(DeployError):
            self.deploy(archive)
        self.assertFalse(any("migrate" in args for args, _ in self.host.commands))
        self.assertEqual("backup", self.state()["last_attempt"]["phase"])

    def test_failed_health_restores_previous_code_and_preserves_failure(self):
        first, a = self.bundle(1)
        second, b = self.bundle(2)
        self.deploy(first)
        self.health.side_effect = [True, False, True, True]
        with self.assertRaises(DeployError):
            self.deploy(second)
        state = self.state()
        self.assertEqual(a["version"], release.current_version(self.host, self.entry))
        self.assertEqual(a, state["current"])
        self.assertEqual("rolled_back", state["status"])
        self.assertEqual("failed", state["last_attempt"]["outcome"])
        self.assertEqual(b["version"], state["last_attempt"]["release"])

    def test_failed_success_record_restores_captured_prior_release(self):
        first, original = self.bundle(1)
        second, _ = self.bundle(2)
        self.deploy(first)
        finish = release.finish
        def fail_success_record(host, entry, state, outcome, error=None):
            if outcome == "succeeded":
                raise OSError("simulated journal write failure")
            return finish(host, entry, state, outcome, error)
        with patch.object(release, "finish", side_effect=fail_success_record):
            with self.assertRaises(DeployError):
                self.deploy(second)
        self.assertEqual(original["version"], release.current_version(self.host, self.entry))
        self.assertEqual(original, self.state()["current"])
        self.assertEqual("rolled_back", self.state()["status"])

    def test_failure_after_switch_replace_restores_previous_code(self):
        first, original = self.bundle(1)
        second, candidate = self.bundle(2)
        self.deploy(first)
        switch = release.switch_to
        def fail_after_replace(host, entry, version):
            switch(host, entry, version)
            if version == candidate["version"]:
                raise OSError("simulated directory fsync failure")
        with patch.object(release, "switch_to", side_effect=fail_after_replace):
            with self.assertRaises(DeployError):
                self.deploy(second)
        self.assertEqual(original["version"], release.current_version(self.host, self.entry))
        self.assertEqual("rolled_back", self.state()["status"])

    def test_rollback_command_failure_restores_original_code_and_records_failure(self):
        first, target = self.bundle(1)
        second, original = self.bundle(2)
        self.deploy(first)
        self.deploy(second)
        with patch.object(release, "restart", side_effect=[DeployError("command_unavailable", "simulated timeout"), None]):
            with self.assertRaises(DeployError):
                release.rollback(self.host, self.project, self.profile,
                                 target["version"], original["version"], str(uuid.uuid4()))
        self.assertEqual(original["version"], release.current_version(self.host, self.entry))
        self.assertEqual(original, self.state()["current"])
        self.assertEqual("failed", self.state()["last_attempt"]["outcome"])

    def test_first_failed_health_is_not_a_successful_release_noop(self):
        archive, _ = self.bundle()
        self.health.return_value = False
        with self.assertRaises(DeployError):
            self.deploy(archive)
        self.health.return_value = True
        self.assertNotIn("unchanged", self.deploy(archive))
        self.assertEqual("healthy", self.state()["status"])

    def test_interrupted_switch_requires_reconciliation_and_recovers_live_release(self):
        first, _ = self.bundle(1)
        second, candidate = self.bundle(2)
        self.deploy(first)
        self.host.interrupt = "enable"
        with self.assertRaises(KeyboardInterrupt):
            self.deploy(second)
        self.assertEqual("running", self.state()["last_attempt"]["outcome"])
        with self.assertRaises(DeployError) as result:
            self.deploy(second)
        self.assertEqual("reconciliation_required", result.exception.code)
        self.host.interrupt = None
        release.reconcile(self.host, self.project, self.profile)
        self.assertEqual(candidate, self.state()["current"])
        self.assertEqual("reconciled", self.state()["last_attempt"]["outcome"])

    def test_reconciliation_rejects_tampered_candidate_before_execution(self):
        archive, manifest = self.bundle()
        self.host.interrupt = "config:check"
        with self.assertRaises(KeyboardInterrupt):
            self.deploy(archive)
        candidate = release.release_path(self.host, self.entry, manifest["version"])
        (candidate / "artisan").write_bytes(b"changed after the failed attempt")
        self.host.interrupt = None
        self.host.commands.clear()
        with self.assertRaises(DeployError) as result:
            release.reconcile(self.host, self.project, self.profile)
        self.assertEqual("release_conflict", result.exception.code)
        self.assertFalse(any("migrate:status" in args for args, _ in self.host.commands))

    def test_configuration_can_repair_first_candidate_without_starting_a_service(self):
        archive, manifest = self.bundle()
        self.host.fail = "config:check"
        with self.assertRaises(DeployError):
            self.deploy(archive)
        self.host.fail = None
        self.host.commands.clear()
        result = release.config_set(self.host, self.project, self.profile, {"PROJECT__NAME": "Configured"})
        self.assertEqual(2, result["configuration_revision"])
        self.assertIsNone(self.state()["current"])
        self.assertEqual("never_deployed", self.state()["status"])
        self.assertFalse(any(args[0] == "systemctl" and args[1] != "is-enabled" for args, _ in self.host.commands))
        self.deploy(archive)
        self.assertEqual(manifest, self.state()["current"])

    def test_provider_configuration_uses_candidate_schema_and_redacts_values(self):
        archive, _ = self.bundle()
        self.deploy(archive)
        keys = {
            "APP__SERVICES__MAIL__MAILERS": True,
            "CLAIMS__TWILIO__API_KEY_SECRET": True,
            "CLAIMS__TWILIO__API_KEY_SID": False,
            "CLAIMS__TWILIO__ACCOUNT_SID": False,
            "CLAIMS__TWILIO__MESSAGING_SERVICE_SID": False,
            "CLAIMS__TWILIO__FROM": False,
            "CLAIMS__SMS_DRIVER": False,
            "CLAIMS__ENABLED": False,
            "CLAIMS__EMAIL_ENABLED": False,
            "CLAIMS__SMS_ENABLED": False,
            "MEMBERS__NOTIFICATION_MAIL_ENABLED": False,
            "VISIBILITY__ENABLED": False,
            "VISIBILITY__ENGINES": True,
            "VISIBILITY__OPEN_ROUTER_API_KEY": True,
            "BILLING__ENABLED": False,
            "BILLING__MY__SECRET_KEY": True,
            "BILLING__MY__WEBHOOK_SECRET": True,
            "BILLING__SG__SECRET_KEY": True,
            "BILLING__SG__WEBHOOK_SECRET": True,
            "BILLING__MY__PAYMENT_METHODS": False,
            "BILLING__SG__PLANS": False,
            "BILLING__MY__MY_TAX_REGISTERED": False,
            "BILLING__MY__MY_TAX_RATE_ID": False,
        }
        self.host.inspect_output = json.dumps([
            {"Name": key.lower().replace("__", "."), "Source": "defaults", "Secret": secret}
            for key, secret in keys.items()
        ])
        for key in keys:
            with self.subTest(key=key):
                result = release.config_set(self.host, self.project, self.profile, {key: "fixture-provider-value"})
                self.assertNotIn("fixture-provider-value", json.dumps(result) + json.dumps(self.state()))
                self.assertEqual("fixture-provider-value", parse_env(self.env_file.read_text())[key])
        self.assertTrue(any("config:check" in args for args, _ in self.host.commands))

    def test_related_keys_change_in_one_validated_revision(self):
        archive, _ = self.bundle()
        self.deploy(archive)
        self.host.inspect_output = json.dumps([
            {"Name": name, "Source": "defaults", "Secret": True} for name in ("app.encryption.key_id", "app.encryption.key")
        ])
        self.host.commands.clear()
        material = base64.urlsafe_b64encode(bytes(range(32))).decode().rstrip("=")
        result = release.config_set(self.host, self.project, self.profile,
                                    {"APP__ENCRYPTION__KEY_ID": "fixture_2026", "APP__ENCRYPTION__KEY": material})
        self.assertEqual(2, result["configuration_revision"])
        values = parse_env(self.env_file.read_text())
        self.assertEqual(("fixture_2026", material), (values["APP__ENCRYPTION__KEY_ID"], values["APP__ENCRYPTION__KEY"]))
        self.assertEqual(1, sum("config:check" in args for args, _ in self.host.commands))
        self.assertNotIn(material, json.dumps(result) + json.dumps(release.public_state(self.state())))

    def test_change_set_with_an_unknown_or_owned_key_changes_nothing(self):
        archive, _ = self.bundle()
        self.deploy(archive)
        before = (self.env_file.read_bytes(), self.host.paths.registry.read_bytes())
        with self.assertRaises(DeployError) as result:
            release.config_set(self.host, self.project, self.profile, {"PROJECT__NAME": "Changed", "PROJECT__NMAE": "typo"})
        self.assertEqual("unknown_configuration", result.exception.code)
        self.host.commands.clear()
        for changes in ({"PROJECT__NAME": "Changed", sorted(release.PIPELINE_KEYS)[0]: "foreign"}, {}, {f"KEY_{n}": "v" for n in range(release.MAX_CONFIGURATION_CHANGES + 1)}):
            with self.subTest(changes=list(changes)[:2]), self.assertRaises(DeployError):
                release.config_set(self.host, self.project, self.profile, changes)
        self.assertEqual([], self.host.commands)
        self.assertEqual(before, (self.env_file.read_bytes(), self.host.paths.registry.read_bytes()))
        self.assertEqual(1, self.state()["configuration_revision"])

    def test_configuration_validation_failure_preserves_file_and_revision(self):
        archive, _ = self.bundle()
        self.deploy(archive)
        before = self.env_file.read_bytes()
        self.host.fail = "config:check"
        with self.assertRaises(DeployError):
            release.config_set(self.host, self.project, self.profile, {"PROJECT__NAME": "Changed"})
        self.assertEqual(before, self.env_file.read_bytes())
        self.assertEqual(1, self.state()["configuration_revision"])

    def test_unknown_configuration_key_refuses_without_live_changes_or_restart(self):
        archive, _ = self.bundle()
        self.deploy(archive)
        before = (self.env_file.read_bytes(), self.host.paths.registry.read_bytes())
        self.host.commands.clear()
        with self.assertRaises(DeployError) as result:
            release.config_set(self.host, self.project, self.profile, {"PROJECT__NMAE": self.secret})
        self.assertEqual("unknown_configuration", result.exception.code)
        self.assertEqual(before, (self.env_file.read_bytes(), self.host.paths.registry.read_bytes()))
        self.assertEqual(1, self.state()["configuration_revision"])
        self.assertEqual("failed", self.state()["last_attempt"]["outcome"])
        self.assertNotIn(self.secret, str(result.exception) + json.dumps(self.state()))
        self.assertFalse(self.env_file.with_name("app.env.next").exists())
        self.assertFalse(any(args[0] == "systemctl" and args[1] != "is-enabled" for args, _ in self.host.commands))
        inspected = next(args for args, _ in self.host.commands if "config:inspect" in args)
        self.assertEqual(str(self.env_file.with_name("app.env.next")), inspected[inspected.index("--env-file") + 1])

    def test_configuration_metadata_failures_are_private_and_do_not_commit(self):
        archive, _ = self.bundle()
        self.deploy(archive)
        before = (self.env_file.read_bytes(), self.host.paths.registry.read_bytes())
        outputs = [self.secret, "{}", json.dumps([{"Name": "project.name", "Source": "defaults", "Secret": self.secret}]),
                   json.dumps([{"Name": "project.name", "Source": "defaults", "Secret": False, "Value": self.secret}]),
                   json.dumps([{"Name": "project.name", "Source": "defaults", "Secret": False}] * 2)]
        for output in outputs:
            with self.subTest(output=output):
                self.host.inspect_output = output
                self.host.commands.clear()
                with self.assertRaises(DeployError) as result:
                    release.config_set(self.host, self.project, self.profile, {"PROJECT__NAME": "Changed"})
                self.assertIn(result.exception.code, {"invalid_json", "configuration_metadata"})
                self.assertEqual(before, (self.env_file.read_bytes(), self.host.paths.registry.read_bytes()))
                self.assertNotIn(self.secret, str(result.exception) + json.dumps(self.state()))
                self.assertFalse(any(args[0] == "systemctl" and args[1] != "is-enabled" for args, _ in self.host.commands))
                self.assertFalse(self.env_file.with_name("app.env.next").exists())

    def test_configuration_failed_health_is_recorded_and_needs_explicit_correction(self):
        archive, _ = self.bundle()
        self.deploy(archive)
        self.health.return_value = False
        with self.assertRaises(DeployError) as result:
            release.config_set(self.host, self.project, self.profile, {"PROJECT__NAME": "Changed"})
        self.assertEqual("configuration_readiness", result.exception.code)
        self.assertEqual("Changed", parse_env(self.env_file.read_text())["PROJECT__NAME"])
        self.assertEqual(2, self.state()["configuration_revision"])
        self.assertEqual("failed", self.state()["status"])
        self.assertEqual("failed_applied", self.state()["last_attempt"]["outcome"])
        self.assertEqual("failed_applied", release.public_state(self.state())["last_attempt"]["outcome"])
        self.assertNotIn(self.secret, json.dumps(self.state()))
        before = self.host.paths.registry.read_bytes(), self.env_file.read_bytes()
        self.host.commands.clear()
        self.assertTrue(release.reconcile(self.host, self.project, self.profile)["unchanged"])
        self.assertEqual([], self.host.commands)
        self.assertEqual(before, (self.host.paths.registry.read_bytes(), self.env_file.read_bytes()))
        self.health.return_value = True
        corrected = release.config_set(self.host, self.project, self.profile, {"PROJECT__NAME": "Corrected"})
        self.assertEqual(3, corrected["configuration_revision"])
        self.assertEqual("healthy", self.state()["status"])

    def test_configuration_interrupt_reconcile_restarts_committed_configuration(self):
        archive, _ = self.bundle()
        self.deploy(archive)
        self.host.interrupt = "restart"
        with self.assertRaises(KeyboardInterrupt):
            release.config_set(self.host, self.project, self.profile, {"PROJECT__NAME": "Changed"})
        self.host.interrupt = None
        self.host.commands.clear()
        release.reconcile(self.host, self.project, self.profile)
        self.assertTrue(any("restart" in args for args, _ in self.host.commands))
        self.assertEqual(2, self.state()["configuration_revision"])

    def assert_selected_storage(self, entry, namespace):
        storage = runtime_storage(release.validate_runtime_environment(self.host, entry),
                                  lambda value: release._read_value_file(self.host, entry, value))
        self.assertEqual(storage_fixture(namespace), storage)
        self.assertEqual(storage_bindings(storage), entry["storage_bindings"])

    def storage_changes(self, namespace):
        storage = storage_fixture(namespace)
        changes = {"APP__SERVICES__CREDENTIALS": json.dumps(storage["credentials"]),
                   "APP__SERVICES__STORAGE__DISKS": json.dumps(storage["disks"])}
        self.host.inspect_output = json.dumps([
            {"Name": key.lower().replace("__", "."), "Source": "defaults", "Secret": True}
            for key in changes])
        return changes

    def test_configuration_persists_storage_reservation_before_publication_and_selects_runtime_storage(self):
        archive, _ = self.bundle()
        self.deploy(archive)
        release.config_set(self.host, self.project, self.profile, self.storage_changes("old/"))
        changes = self.storage_changes("new/")
        credentials_file = self.env_file.with_name("cloud.secret")
        self.host.owned_write(credentials_file, changes.pop("APP__SERVICES__CREDENTIALS"), 0o640)
        changes["APP__SERVICES__CREDENTIALS_FILE"] = str(credentials_file)
        original = release.os.replace
        observed = []
        def publish(source, destination):
            if destination == self.env_file:
                entry = self.host.read_registry()["instances"][self.profile["instance_id"]]
                self.assertEqual("old/", entry["storage_bindings"]["uploads"]["namespace"])
                self.assertEqual("new/", entry["pending_storage_bindings"]["uploads"]["namespace"])
                observed.append(True)
            return original(source, destination)
        with patch.object(release.os, "replace", side_effect=publish):
            release.config_set(self.host, self.project, self.profile, changes)
        self.assertEqual([True], observed)
        credentials_file.write_text("changed after selection")
        entry = self.host.read_registry()["instances"][self.profile["instance_id"]]
        self.assertNotIn("pending_storage_bindings", entry)
        self.assert_selected_storage(entry, "new/")
        self.assertEqual(3, entry["configuration_revision"])
        self.assertNotIn("canary-secret", self.host.paths.registry.read_text())

    def test_configuration_storage_reconcile_selects_proven_old_or_new_revision(self):
        archive, _ = self.bundle()
        self.deploy(archive)
        release.config_set(self.host, self.project, self.profile, self.storage_changes("old/"))
        for published in (False, True):
            with self.subTest(published=published):
                changes = self.storage_changes("new/")
                original = release.os.replace
                def interrupt(source, destination):
                    if destination == self.env_file:
                        if published:
                            original(source, destination)
                        raise KeyboardInterrupt()
                    return original(source, destination)
                with patch.object(release.os, "replace", side_effect=interrupt), self.assertRaises(KeyboardInterrupt):
                    release.config_set(self.host, self.project, self.profile, changes)
                entry = self.host.read_registry()["instances"][self.profile["instance_id"]]
                self.assertEqual("old/", entry["storage_bindings"]["uploads"]["namespace"])
                self.assertEqual("new/", entry["pending_storage_bindings"]["uploads"]["namespace"])
                release.reconcile(self.host, self.project, self.profile)
                entry = self.host.read_registry()["instances"][self.profile["instance_id"]]
                self.assertNotIn("pending_storage_bindings", entry)
                self.assert_selected_storage(entry, "new/" if published else "old/")
                self.assertEqual(3 if published else 2, entry["configuration_revision"])

    def test_configuration_storage_collision_is_refused_before_publication(self):
        archive, _ = self.bundle()
        self.deploy(archive)
        release.config_set(self.host, self.project, self.profile, self.storage_changes("old/"))
        _, _, other = fixture(project_slug="second", host_id=self.entry["host_id"], port=8101)
        registry = self.host.read_registry()
        registry["projects"][other["project_id"]] = {"slug": "second", "created_at": now()}
        registry["instances"][other["profile"]["instance_id"]] = other
        bind_storage(registry, other, storage_fixture("taken/"))
        self.host.write_registry(registry)
        before = self.env_file.read_bytes(), self.host.paths.registry.read_bytes()
        with self.assertRaises(DeployError) as caught:
            release.config_set(self.host, self.project, self.profile, self.storage_changes("taken/child/"))
        self.assertEqual("storage_binding_conflict", caught.exception.code)
        self.assertEqual(before, (self.env_file.read_bytes(), self.host.paths.registry.read_bytes()))

    def test_configuration_storage_failure_releases_reservation_for_known_selection(self):
        archive, _ = self.bundle()
        self.deploy(archive)
        release.config_set(self.host, self.project, self.profile, self.storage_changes("old/"))
        original = release.os.replace
        def fail(source, destination):
            if destination == self.env_file:
                raise OSError("publication failed")
            return original(source, destination)
        with patch.object(release.os, "replace", side_effect=fail), self.assertRaises(OSError):
            release.config_set(self.host, self.project, self.profile, self.storage_changes("new/"))
        entry = self.host.read_registry()["instances"][self.profile["instance_id"]]
        self.assertNotIn("pending_storage_bindings", entry)
        self.assert_selected_storage(entry, "old/")
        self.health.return_value = False
        with self.assertRaises(DeployError):
            release.config_set(self.host, self.project, self.profile, self.storage_changes("new/"))
        entry = self.host.read_registry()["instances"][self.profile["instance_id"]]
        self.assertNotIn("pending_storage_bindings", entry)
        self.assert_selected_storage(entry, "new/")

    def test_configuration_storage_error_after_publication_selects_applied_revision(self):
        archive, _ = self.bundle()
        self.deploy(archive)
        release.config_set(self.host, self.project, self.profile, self.storage_changes("old/"))
        original = release.os.replace
        def fail_after_publication(source, destination):
            result = original(source, destination)
            if destination == self.env_file:
                raise OSError("publication acknowledgement failed")
            return result
        with patch.object(release.os, "replace", side_effect=fail_after_publication), self.assertRaises(DeployError) as caught:
            release.config_set(self.host, self.project, self.profile, self.storage_changes("new/"))
        self.assertEqual("configuration_readiness", caught.exception.code)
        entry = self.host.read_registry()["instances"][self.profile["instance_id"]]
        self.assertNotIn("pending_storage_bindings", entry)
        self.assert_selected_storage(entry, "new/")
        self.assertEqual(3, entry["configuration_revision"])

    def test_configuration_storage_ambiguous_reconcile_retains_both_bindings(self):
        archive, _ = self.bundle()
        self.deploy(archive)
        release.config_set(self.host, self.project, self.profile, self.storage_changes("old/"))
        original = release.os.replace
        def interrupt(source, destination):
            if destination == self.env_file:
                raise KeyboardInterrupt()
            return original(source, destination)
        with patch.object(release.os, "replace", side_effect=interrupt), self.assertRaises(KeyboardInterrupt):
            release.config_set(self.host, self.project, self.profile, self.storage_changes("new/"))
        values = parse_env(self.env_file.read_text())
        values["PROJECT__NAME"] = "unrecorded change"
        self.host.owned_write(self.env_file, dump_env(values), 0o640)
        before = self.host.paths.registry.read_bytes()
        with self.assertRaises(DeployError) as caught:
            release.reconcile(self.host, self.project, self.profile)
        self.assertEqual("configuration_reconciliation", caught.exception.code)
        self.assertEqual(before, self.host.paths.registry.read_bytes())
        entry = self.host.read_registry()["instances"][self.profile["instance_id"]]
        self.assertEqual("old/", entry["storage_bindings"]["uploads"]["namespace"])
        self.assertEqual("new/", entry["pending_storage_bindings"]["uploads"]["namespace"])

    def test_configuration_interrupt_before_replace_reconciles_unchanged_file(self):
        archive, _ = self.bundle()
        self.deploy(archive)
        before = self.env_file.read_bytes()
        original = release.os.replace
        def interrupted(source, destination):
            if destination == self.env_file:
                raise KeyboardInterrupt()
            return original(source, destination)
        with patch.object(release.os, "replace", side_effect=interrupted):
            with self.assertRaises(KeyboardInterrupt):
                release.config_set(self.host, self.project, self.profile, {"PROJECT__NAME": "Changed"})
        self.assertEqual(before, self.env_file.read_bytes())
        release.reconcile(self.host, self.project, self.profile)
        self.assertFalse(self.state()["last_attempt"]["configuration_applied"])
        self.assertEqual(1, self.state()["configuration_revision"])

    def test_configuration_interrupted_during_validation_requires_reconciliation(self):
        archive, _ = self.bundle()
        self.deploy(archive)
        original = self.env_file.read_bytes()
        self.host.interrupt = "config:check"
        with self.assertRaises(KeyboardInterrupt):
            release.config_set(self.host, self.project, self.profile, {"PROJECT__NAME": "Changed"})
        self.assertEqual("running", self.state()["last_attempt"]["outcome"])
        self.assertEqual(original, self.env_file.read_bytes())
        self.host.interrupt = None
        release.reconcile(self.host, self.project, self.profile)
        self.assertFalse(self.state()["last_attempt"]["configuration_applied"])

    def test_runtime_maps_copy_uses_service_uid_and_rejects_linked_destination(self):
        archive, manifest = self.bundle()
        destination = self.root / "extract"
        release.unpack_archive(archive, destination, self.entry, digest(archive))
        release.protect_maps(self.host, self.entry, destination)
        copies = [args for args, _ in self.host.commands if "copy-runtime-maps" in args]
        self.assertEqual(["runuser", "-u", self.entry["name"], "--", "python3", release.service_script(self.host)], copies[0][:6])
        self.assertTrue(copies[0][5].endswith("/usr/local/lib/foundry/current/lib/release.py"))
        maps = self.root / "runtime-maps"
        release.copy_runtime_maps(destination / "private-source-maps", maps)
        self.assertTrue((maps / manifest["version"] / "admin.js.map").is_file())
        linked = self.root / "linked"
        linked.symlink_to(maps, target_is_directory=True)
        with self.assertRaises(DeployError):
            release.copy_runtime_maps(destination / "private-source-maps", linked)

    def test_runtime_connection_drift_refused_before_activation(self):
        archive, _ = self.bundle()
        values = parse_env(self.env_file.read_text())
        values["APP__SERVICES__NAMESPACE__ENVIRONMENT"] = "another-instance"
        atomic_write(self.env_file, dump_env(values), 0o640)
        with self.assertRaises(DeployError) as result:
            self.deploy(archive)
        self.assertEqual("configuration_identity", result.exception.code)
        self.assertEqual([], self.host.commands)

    def test_owned_config_and_wrong_host_refused_without_processes(self):
        for key in release.PIPELINE_KEYS:
            with self.assertRaises(DeployError):
                release.config_set(self.host, self.project, self.profile, {key: "foreign"})
        profile = copy.deepcopy(self.profile)
        profile["host"]["host_id"] = str(uuid.uuid4())
        with self.assertRaises(DeployError):
            release.activate(self.host, self.project, profile, "absent", "0" * 64)
        self.assertEqual([], self.host.commands)

    def test_readiness_requires_exact_json_and_the_selected_process_listener(self):
        observations = self.process_observations()
        for listener_pid, body, expected in ((42, {"ready": True}, True), (99, {"ready": True}, False), (42, {"ready": 1}, False)):
            def command(args, **kwargs):
                output = observations.get(args[2], "")
                if args[0] == "ss":
                    output = f'LISTEN 0 4096 127.0.0.1:8100 0.0.0.0:* users:(("artisan",pid={listener_pid},fd=5))'
                return subprocess.CompletedProcess(args, 0, output, "")
            response = MagicMock()
            response.__enter__.return_value = response
            response.status = 200
            response.read.return_value = json.dumps(body).encode()
            opener = MagicMock()
            opener.open.return_value = response
            with patch.object(self.host, "run", side_effect=command), patch.object(release.urllib.request, "build_opener", return_value=opener), patch.object(release, "STABILITY_SECONDS", 0):
                # A negative case polls until its deadline; a positive one returns at once.
                self.assertEqual(expected, CHECK_READY(self.host, self.entry, timeout=30 if expected else 0.01))

    def test_readiness_wall_clock_bounds_a_slow_response_body(self):
        observations = self.process_observations()
        def command(args, **kwargs):
            output = observations.get(args[2], "")
            if args[0] == "ss":
                output = 'LISTEN 0 4096 127.0.0.1:8100 0.0.0.0:* users:(("artisan",pid=42,fd=5))'
            return subprocess.CompletedProcess(args, 0, output, "")
        response = MagicMock()
        response.__enter__.return_value = response
        response.status = 200
        def drip(_):
            time.sleep(5)
            return b'{"ready":true}'
        response.read.side_effect = drip
        opener = MagicMock()
        opener.open.return_value = response
        with patch.object(self.host, "run", side_effect=command), patch.object(release.urllib.request, "build_opener", return_value=opener):
            started = time.monotonic()
            self.assertFalse(CHECK_READY(self.host, self.entry, timeout=0.03))
            self.assertLess(time.monotonic() - started, 2)

    def test_pruning_preserves_active_and_rollback_and_rollback_never_migrates(self):
        one, _ = self.bundle(1)
        two, second = self.bundle(2)
        three, third = self.bundle(3)
        self.deploy(one)
        self.deploy(two)
        self.deploy(three)
        self.assertEqual({second["version"], third["version"]}, {p.name for p in (self.opt / "releases").iterdir()})
        command = next(args for args, _ in reversed(self.host.commands) if "prune-runtime-maps" in args)
        self.assertEqual(["runuser", "-u", self.entry["name"], "--", "python3", release.service_script(self.host)], command[:6])
        self.assertEqual({second["version"], third["version"]}, set(command[8:]))
        self.host.commands.clear()
        release.rollback(self.host, self.project, self.profile,
                         second["version"], third["version"], str(uuid.uuid4()))
        self.assertEqual(second["version"], self.state()["current"]["version"])
        self.assertFalse(any("migrate" in args or args[0] == "pg_restore" for args, _ in self.host.commands))

    def test_runtime_map_retention_preserves_protected_versions_and_foreign_paths(self):
        maps = self.host.paths.data(self.entry["name"]) / "private-source-maps"
        versions = [f"20261013T00000{number}Z-0123456789abcdef" for number in range(1, 5)]
        for version in versions:
            (maps / version).mkdir(parents=True)
            (maps / version / "admin.js.map").write_text("private map")
        (maps / "operator-notes").mkdir()
        outside = self.root / "outside"
        outside.mkdir()
        (outside / "keep").write_text("unrelated")
        linked = maps / "20261013T000005Z-0123456789abcdef"
        linked.symlink_to(outside)
        protected = set(versions[1:3])
        release.prune_runtime_maps(maps, protected)
        self.assertEqual(protected | {"operator-notes", linked.name}, {path.name for path in maps.iterdir()})
        self.assertEqual("unrelated", (outside / "keep").read_text())
        with self.assertRaises(DeployError):
            release.prune_runtime_maps(linked, protected)

    def test_tampered_previous_binary_refused_before_execution(self):
        first, original = self.bundle(1)
        second, current = self.bundle(2)
        self.deploy(first)
        self.deploy(second)
        (self.opt / "releases" / original["version"] / "artisan").write_bytes(b"tampered")
        self.host.commands.clear()
        with self.assertRaises(DeployError) as result:
            release.rollback(self.host, self.project, self.profile,
                             original["version"], current["version"], str(uuid.uuid4()))
        self.assertEqual("release_conflict", result.exception.code)
        self.assertEqual([], self.host.commands)

    def test_rollback_explicit_target_is_idempotent_after_history_pruning_and_refuses_stale_expectations(self):
        first, one = self.bundle(1)
        second, two = self.bundle(2)
        third, three = self.bundle(3)
        self.deploy(first)
        self.deploy(second)
        operation = str(uuid.uuid4())
        release.rollback(self.host, self.project, self.profile, one["version"], two["version"], operation)
        self.assertEqual(one["version"], release.current_version(self.host, self.entry))
        self.deploy(third)
        state = self.state()
        state["history"], state["last_attempt"] = [], None
        self.host.save_state(self.entry, state)
        self.host.commands.clear()
        self.assertTrue(release.rollback(self.host, self.project, self.profile,
                                         one["version"], two["version"], operation)["unchanged"])
        self.assertEqual(three["version"], release.current_version(self.host, self.entry))
        self.assertEqual([], self.host.commands)
        with self.assertRaises(DeployError) as stale:
            release.rollback(self.host, self.project, self.profile,
                             one["version"], two["version"], str(uuid.uuid4()))
        self.assertEqual("stale_current", stale.exception.code)
        with self.assertRaises(DeployError) as conflict:
            release.rollback(self.host, self.project, self.profile,
                             one["version"], three["version"], operation)
        self.assertEqual("operation_conflict", conflict.exception.code)

    def test_successful_rollback_repairs_stale_running_receipt_without_repeating_switch(self):
        first, one = self.bundle(1)
        second, two = self.bundle(2)
        self.deploy(first)
        self.deploy(second)
        operation = str(uuid.uuid4())
        release.rollback(self.host, self.project, self.profile, one["version"], two["version"], operation)
        completed = copy.deepcopy(self.state()["last_attempt"])
        stale = {**completed, "outcome": "running", "finished_at": None, "phase": "validation"}
        receipt = release._operation_path(self.host, self.entry, operation)
        atomic_json(receipt, stale, mode=0o600)
        self.host.commands.clear()
        with patch.object(release, "switch_to") as switch:
            result = release.rollback(self.host, self.project, self.profile, one["version"], two["version"], operation)
        self.assertTrue(result["unchanged"])
        switch.assert_not_called()
        self.assertEqual([], self.host.commands)
        self.assertEqual(completed, json.loads(receipt.read_text()))
        self.assertEqual(one["version"], release.current_version(self.host, self.entry))

    def test_interrupted_rollback_before_switch_recovers_original_and_records_target_not_applied(self):
        first, one = self.bundle(1)
        second, two = self.bundle(2)
        self.deploy(first)
        self.deploy(second)
        operation = str(uuid.uuid4())
        with patch.object(release, "switch_to", side_effect=KeyboardInterrupt):
            with self.assertRaises(KeyboardInterrupt):
                release.rollback(self.host, self.project, self.profile, one["version"], two["version"], operation)
        self.assertEqual(two["version"], release.current_version(self.host, self.entry))
        self.host.commands.clear()
        result = release.reconcile(self.host, self.project, self.profile)
        self.assertEqual("failed", result["outcome"])
        self.assertEqual(two["version"], result["version"])
        self.assertEqual("healthy", self.state()["status"])
        self.assertEqual("rollback_not_applied", self.state()["last_attempt"]["error"])
        self.assertIn(["systemctl", "restart", instance_units(self.entry)["http"]],
                      [args for args, _ in self.host.commands])
        with self.assertRaises(DeployError) as repeat:
            release.rollback(self.host, self.project, self.profile, one["version"], two["version"], operation)
        self.assertEqual("rollback_not_applied", repeat.exception.code)

    def test_configuration_replace_failure_restarts_original_configuration_before_closing_failure(self):
        archive, _ = self.bundle()
        self.deploy(archive)
        before = self.env_file.read_bytes()
        self.host.commands.clear()
        self.health.reset_mock()
        original = release.os.replace
        def fail_selection(source, destination):
            if Path(destination) == self.env_file:
                units = instance_units(self.entry)
                self.assertIn(["systemctl", "stop", units["http"]], [args for args, _ in self.host.commands])
                raise OSError("simulated configuration selection failure")
            return original(source, destination)
        with patch.object(release.os, "replace", side_effect=fail_selection):
            with self.assertRaises(OSError):
                release.config_set(self.host, self.project, self.profile, {"PROJECT__NAME": "Changed"})
        self.assertEqual(before, self.env_file.read_bytes())
        self.assertEqual(1, self.state()["configuration_revision"])
        self.assertEqual("failed", self.state()["last_attempt"]["outcome"])
        self.assertEqual("healthy", self.state()["status"])
        self.assertEqual(2, self.health.call_count)
        commands = [args for args, _ in self.host.commands]
        http = instance_units(self.entry)["http"]
        self.assertLess(commands.index(["systemctl", "stop", http]), commands.index(["systemctl", "restart", http]))

    def test_repeated_completed_reconciliation_has_no_side_effects(self):
        archive, _ = self.bundle()
        self.deploy(archive)
        before = self.host.paths.registry.read_bytes(), self.state()
        self.host.commands.clear()
        self.grants.reset_mock()
        for _ in range(2):
            self.assertTrue(release.reconcile(self.host, self.project, self.profile)["unchanged"])
        self.assertEqual([], self.host.commands)
        self.grants.assert_not_called()
        self.assertEqual(before, (self.host.paths.registry.read_bytes(), self.state()))

    def test_reconciliation_restart_failure_cannot_close_as_reconciled(self):
        archive, _ = self.bundle()
        self.host.interrupt = "enable"
        with self.assertRaises(KeyboardInterrupt):
            self.deploy(archive)
        self.host.interrupt = None
        self.health.return_value = False
        with self.assertRaises(DeployError) as failure:
            release.reconcile(self.host, self.project, self.profile)
        self.assertEqual("reconciliation_health", failure.exception.code)
        self.assertEqual("needs_reconciliation", self.state()["last_attempt"]["outcome"])
        self.assertEqual("failed", self.state()["status"])

    def test_required_background_failure_uses_one_remaining_deadline(self):
        self.select_fixture_release()
        self.health.side_effect = [True, False]
        with self.assertRaises(DeployError) as failure:
            release.restart(self.host, self.entry)
        self.assertEqual("readiness_failed", failure.exception.code)
        initial, final = self.health.call_args_list
        self.assertFalse(initial.kwargs["include_background"])
        self.assertLessEqual(final.kwargs["timeout"], initial.kwargs["timeout"])
        self.assertLessEqual(initial.kwargs["timeout"], release.READINESS_SECONDS)
        units = instance_units(self.entry)
        self.assertIn(["systemctl", "start", units["scheduler"]], [args for args, _ in self.host.commands])

    def test_undeclared_worker_is_never_started(self):
        self.select_fixture_release()
        from topology import validate_topology
        entry = copy.deepcopy(self.entry)
        entry["topology"] = validate_topology({"schema": 1, "roles": [{"name": "http"}, {"name": "scheduler"}]})
        release.restart(self.host, entry)
        self.assertFalse(any("foundry-worker@" in value for args, _ in self.host.commands for value in args))

    def test_live_services_report_process_executable_and_detect_old_running_release(self):
        observations = self.process_observations()
        def command(args, **kwargs):
            return subprocess.CompletedProcess(args, 0, observations[args[2]], "")
        with patch.object(self.host, "run", side_effect=command):
            current = release.services(self.host, self.entry)
            self.assertEqual(set(instance_units(self.entry)), set(current))
            self.assertTrue(all(role["executable_matches"] for role in current.values()))
            self.assertEqual("42", current["http"]["MainPID"])
            self.assertEqual("0", current["http"]["NRestarts"])
            old = self.root / "old-artisan"
            old.write_bytes(b"old process")
            executable = self.host.paths.p("/proc/42/exe")
            executable.unlink()
            executable.symlink_to(old)
            changed = release.services(self.host, self.entry)
            self.assertEqual(str(old), changed["http"]["exe"])
            self.assertFalse(changed["http"]["executable_matches"])
            with patch.object(release, "STABILITY_SECONDS", 0):
                self.assertFalse(CHECK_READY(self.host, self.entry, timeout=0.01))

    def test_redis_transition_widens_both_inventories_then_narrows_after_readiness(self):
        archive, _ = self.bundle()
        namespace = self.entry["allocations"]["namespace"]
        suffix = namespace["application"] + ":" + namespace["environment"] + ":"
        old_key, new_key = "foundry:cache:v1:" + suffix, "foundry:queue:v1:" + suffix
        old_channel, new_channel = "foundry:events:v1:" + suffix, "foundry:broadcast:v1:" + suffix
        self.entry["redis_grants"].update(keys=[old_key], channels=[old_channel])
        self.host.write_registry(self.registry)
        self.host.redis_keys_output = json.dumps([{"connection": "main", "keys": [{"prefix": new_key}],
                                                  "channels": [{"prefix": new_channel}]}])
        def inspect_grants(*args, **kwargs):
            _, selected = self.host.require_identity(self.project, self.profile)
            self.assertEqual({old_key, new_key}, set(selected["redis_grants"]["keys"]))
            self.assertEqual({old_channel, new_channel}, set(selected["redis_grants"]["channels"]))
            return True
        self.health.side_effect = inspect_grants
        self.deploy(archive)
        self.assertEqual(2, self.grants.call_count)
        widened, narrowed = self.grants.call_args_list
        self.assertEqual({old_key + "*", new_key + "*"}, set(widened.args[3]))
        self.assertEqual({old_channel + "*", new_channel + "*"}, set(widened.kwargs["channels"]))
        self.assertEqual([new_key + "*"], narrowed.args[3])
        self.assertEqual([new_channel + "*"], narrowed.kwargs["channels"])

    def test_failed_narrowing_is_resumed_before_a_different_transition(self):
        archive, _ = self.bundle()
        calls = 0
        def fail_narrow(*args, **kwargs):
            nonlocal calls
            calls += 1
            if calls == 2:
                raise DeployError("redis_acl", "Synthetic narrowing interruption")
            return self.reconcile_grants(*args, **kwargs)
        self.grants.side_effect = fail_narrow
        result = self.deploy(archive)
        self.assertIn("redis_grants_not_narrowed", result["warnings"])
        self.assertTrue(self.state()["last_attempt"]["redis_pending"])
        before = self.state()
        self.assertFalse(release.reconcile(self.host, self.project, self.profile)["unchanged"])
        self.assertFalse(self.state()["last_attempt"]["redis_pending"])
        self.assertEqual(before["current"], self.state()["current"])
        self.assertEqual(before["last_attempt"]["id"], self.state()["last_attempt"]["id"])
        self.assertEqual([], self.grants.call_args.args[3])
        namespace = self.entry["allocations"]["namespace"]
        prefix = "foundry:cache:v1:" + namespace["application"] + ":" + namespace["environment"] + ":"
        self.host.redis_keys_output = json.dumps([{"connection": "main", "keys": [{"prefix": prefix}], "channels": []}])
        self.grants.reset_mock()
        self.grants.side_effect = self.reconcile_grants
        release.config_set(self.host, self.project, self.profile, {"PROJECT__NAME": "Corrected"})
        widened, narrowed = self.grants.call_args_list
        self.assertEqual([prefix + "*"], widened.args[3])
        self.assertEqual([prefix + "*"], narrowed.args[3])
        self.assertFalse(self.state()["last_attempt"]["redis_pending"])

    def test_configuration_and_rollback_refresh_candidate_key_and_channel_grants(self):
        first, one = self.bundle(1)
        second, two = self.bundle(2)
        self.deploy(first)
        self.deploy(second)
        namespace = self.entry["allocations"]["namespace"]
        suffix = namespace["application"] + ":" + namespace["environment"] + ":"
        key, channel = "foundry:cache:v1:" + suffix, "foundry:events:v1:" + suffix
        self.host.redis_keys_output = json.dumps([{"connection": "main", "keys": [{"prefix": key}],
                                                  "channels": [{"prefix": channel}]}])
        self.grants.reset_mock()
        release.config_set(self.host, self.project, self.profile, {"PROJECT__NAME": "Changed"})
        self.assertEqual(2, self.grants.call_count)
        self.assertEqual([channel + "*"], self.grants.call_args.kwargs["channels"])
        self.host.redis_keys_output = "[]"
        self.grants.reset_mock()
        release.rollback(self.host, self.project, self.profile, one["version"], two["version"], str(uuid.uuid4()))
        widened, narrowed = self.grants.call_args_list
        self.assertEqual([key + "*"], widened.args[3])
        self.assertEqual([channel + "*"], widened.kwargs["channels"])
        self.assertEqual([], narrowed.args[3])
        self.assertEqual([], narrowed.kwargs["channels"])

    def test_bootstrap_validates_storage_before_persisting_so_a_retry_can_correct_it(self):
        disks = {"banner": {"driver": "r2", "cloud": {"bucket": "app-bucket", "credentials": "app",
                                                      "endpoint": "https://account.invalid/other"}}}
        intake = {"app_storage": {"credentials": {"app": {"secret_key": "app-secret"}}, "disks": disks}}
        private = self.env_file.with_name("bootstrap-secrets.json")
        with self.assertRaises(DeployError) as error:
            initialize_bootstrap_secrets(self.host, self.entry, intake)
        self.assertEqual("storage_binding_invalid", error.exception.code)
        self.assertIn("Disk banner:", error.exception.detail)
        self.assertNotIn("account.invalid", error.exception.detail)
        self.assertFalse(private.exists())
        # Both slips the first real bootstrap met are repaired, then stored canonically.
        for endpoint in ("account.invalid", "https://account.invalid/app-bucket"):
            disks["banner"]["cloud"]["endpoint"] = endpoint
            initialize_bootstrap_secrets(self.host, self.entry, intake)
            stored = json.loads(private.read_text())["app_storage"]["disks"]["banner"]["cloud"]["endpoint"]
            self.assertEqual("https://account.invalid", stored)
        bad_backup = dict(endpoint="https://backup.invalid/other", region="auto", bucket="backup",
                          access_key_id="backup-id", secret_access_key="backup-secret")
        with self.assertRaises(DeployError) as error:
            initialize_bootstrap_secrets(self.host, self.entry, {"backup_s3": bad_backup})
        self.assertIn("Backup repository:", error.exception.detail)

    def test_config_storage_and_values_survive_bootstrap_with_no_secrets(self):
        from test_rotations import bootstrap_without_secrets
        from runtime_config import POSTGRES_PORT
        intake = {"app_storage": storage_fixture("original/"), "configuration": {"PROJECT__NAME": "Original"}}
        credentials = initialize_bootstrap_secrets(self.host, self.entry, intake)
        self.host.owned_write(self.env_file, dump_env(runtime_environment(self.entry, credentials)), 0o640)
        self.registry["shared"]["postgresql"] = {"port": POSTGRES_PORT}
        self.host.write_registry(self.registry)
        archive, _ = self.bundle()
        self.deploy(archive)
        changed = self.storage_changes("original/")
        storage_credentials = json.loads(changed["APP__SERVICES__CREDENTIALS"])
        for credential in storage_credentials.values():
            credential["secret_key"] = "replacement-storage-canary"
        changed["APP__SERVICES__CREDENTIALS"] = json.dumps(storage_credentials)
        changed["PROJECT__NAME"] = "Replaced"
        self.host.inspect_output = json.dumps([
            {"Name": key.lower().replace("__", "."), "Source": "defaults", "Secret": True} for key in changed])
        release.config_set(self.host, self.project, self.profile, changed)
        registry = self.host.read_registry()
        registry["instances"][self.profile["instance_id"]]["provisioning"]["completed_steps"].append("configuration")
        self.host.write_registry(registry)
        before = self.env_file.read_bytes()
        result = bootstrap_without_secrets(self.host, self.project, self.profile)
        self.assertTrue(result["ok"])
        self.assertEqual(before, self.env_file.read_bytes())
        values = parse_env(self.env_file.read_text())
        self.assertEqual("Replaced", values["PROJECT__NAME"])
        self.assertIn("replacement-storage-canary", values["APP__SERVICES__CREDENTIALS"])
        # Intake is intentionally initial-only. It cannot recreate a lost app.env.
        self.env_file.unlink()
        with self.assertRaises(DeployError) as caught:
            bootstrap_without_secrets(self.host, self.project, self.profile)
        self.assertEqual("configuration_missing", caught.exception.code)

    def test_bootstrap_initializes_keyring_once_and_preserves_private_intake(self):
        intake = {"cloudflare_dns_token": "fixture-token", "age_recipients": ["age1fixture"],
                  "backup_s3": dict(endpoint="https://backup.invalid", region="auto", bucket="backup",
                                    access_key_id="backup-id", secret_access_key="backup-secret"),
                  "app_storage": {"credentials": '{"uploads":{"secret_key":"app-secret"}}',
                                  "disks": json.dumps({"uploads": {"driver": "r2", "cloud": {
                                      "bucket": "app-bucket", "endpoint": "https://app.invalid", "credentials": "uploads"}}})}}
        credentials = initialize_bootstrap_secrets(self.host, self.entry, intake)
        values = runtime_environment(self.entry, credentials)
        key_file = Path(values[ENCRYPTION_KEYS[1] + "_FILE"])
        self.assertEqual(32, len(base64.urlsafe_b64decode(key_file.read_text().strip() + "=")))
        self.assertEqual({}, json.loads(values[ENCRYPTION_KEYS[2]]))
        private = self.env_file.with_name("bootstrap-secrets.json")
        self.assertEqual(0o600, private.stat().st_mode & 0o777)
        self.assertEqual(0o640, key_file.stat().st_mode & 0o777)
        before = private.read_bytes(), key_file.read_bytes()
        repeated = initialize_bootstrap_secrets(self.host, self.entry, intake)
        self.assertEqual(credentials, repeated)
        self.assertEqual(before, (private.read_bytes(), key_file.read_bytes()))
        self.assertIn("APP__SERVICES__CREDENTIALS", values)
        self.assertIn("APP__SERVICES__STORAGE__DISKS", values)
        with self.assertRaises(DeployError):
            initialize_bootstrap_secrets(self.host, self.entry, {"encryption": {"key_id": "partial"}})
        self.entry["provisioning"]["completed_steps"].append("certificate")
        with self.assertRaisesRegex(DeployError, "ACTION=dns-token-rotate"):
            initialize_bootstrap_secrets(self.host, self.entry, {"cloudflare_dns_token": "replacement"})
        private.unlink()
        with self.assertRaises(DeployError) as missing:
            initialize_bootstrap_secrets(self.host, self.entry)
        self.assertEqual("credentials_missing", missing.exception.code)
        self.assertEqual(before[1], key_file.read_bytes())

    def test_bootstrap_initial_configuration_reaches_first_environment_only_for_app_keys(self):
        for owned in ({ENCRYPTION_KEYS[1]: "x"}, {"APP__SERVICES__STORAGE__DISKS_FILE": "/etc/x"},
                      {sorted(release.PIPELINE_KEYS)[0]: "x"}, {"ADMINS__SEED__SUPERADMIN_PASSWORD": 1}, {}):
            with self.subTest(owned=sorted(owned)), self.assertRaises(DeployError):
                initialize_bootstrap_secrets(self.host, self.entry, {"configuration": owned})
        self.assertFalse(self.env_file.with_name("bootstrap-secrets.json").exists())
        intake = {"configuration": {"ADMINS__SEED__SUPERADMIN_PASSWORD": "seed-canary"}}
        credentials = initialize_bootstrap_secrets(self.host, self.entry, intake)
        self.assertEqual("seed-canary", runtime_environment(self.entry, credentials)["ADMINS__SEED__SUPERADMIN_PASSWORD"])
        self.assertEqual(credentials, initialize_bootstrap_secrets(self.host, self.entry))
        self.entry["provisioning"]["completed_steps"].append("configuration")
        with self.assertRaisesRegex(DeployError, "ACTION=config"):
            initialize_bootstrap_secrets(self.host, self.entry, {"configuration": {"ADMINS__SEED__SUPERADMIN_PASSWORD": "other"}})

    def test_rotation_retains_all_prior_material_and_rejects_reuse_and_overflow(self):
        values = runtime_environment(self.entry, initialize_bootstrap_secrets(self.host, self.entry))
        for _ in range(2):
            changes = rotation_changes(self.host, self.entry, values)
            updated = candidate_values(self.host, self.entry, values, changes)
            validate_keyring_change(self.host, self.entry, values, updated)
            values = updated
        self.assertEqual(2, len(json.loads(values[ENCRYPTION_KEYS[2]])))
        old_id = next(iter(json.loads(values[ENCRYPTION_KEYS[2]])))
        material = values[ENCRYPTION_KEYS[1]]
        for pair in ({"key_id": old_id, "key": base64.urlsafe_b64encode(b"x" * 32).decode().rstrip("=")},
                     {"key_id": "new_id", "key": material}, {"key_id": "new_id", "key": material + "="}):
            with self.subTest(pair_id=pair["key_id"]), self.assertRaises(DeployError):
                rotation_changes(self.host, self.entry, values, pair)
        with self.assertRaises(DeployError):
            candidate_values(self.host, self.entry, values, {ENCRYPTION_KEYS[2]: "{}"})
        retained = {"old_" + str(i): {"key": base64.urlsafe_b64encode(bytes([i]) * 32).decode().rstrip("=")}
                    for i in range(31)}
        full = {ENCRYPTION_KEYS[0]: "active", ENCRYPTION_KEYS[1]: base64.urlsafe_b64encode(b"z" * 32).decode().rstrip("="),
                ENCRYPTION_KEYS[2]: json.dumps(retained)}
        with self.assertRaises(DeployError):
            rotation_changes(self.host, self.entry, full)

    def test_file_aliases_are_private_immutable_snapshots_and_direct_values_supersede_them(self):
        source = self.env_file.with_name("import-value.secret")
        atomic_write(source, "original\n\n", 0o640)
        values = candidate_values(self.host, self.entry, {"CUSTOM__VALUE": "old"}, {"CUSTOM__VALUE_FILE": str(source)})
        self.assertNotIn("CUSTOM__VALUE", values)
        snapshot = Path(values["CUSTOM__VALUE_FILE"])
        self.assertNotEqual(source, snapshot)
        self.assertEqual(0o640, snapshot.stat().st_mode & 0o777)
        source.write_text("mutated\n")
        self.assertEqual("original\n\n", snapshot.read_text())
        direct = candidate_values(self.host, self.entry, values, {"CUSTOM__VALUE": "direct"})
        self.assertEqual({"CUSTOM__VALUE": "direct"}, direct)
        self.assertEqual({}, candidate_values(self.host, self.entry, direct, {"CUSTOM__VALUE": None}))
        with self.assertRaises(DeployError):
            candidate_values(self.host, self.entry, {}, {"CUSTOM__VALUE": "direct", "CUSTOM__VALUE_FILE": str(source)})
        foreign = self.root / "foreign.secret"
        atomic_write(foreign, "private", 0o640)
        symlink = self.env_file.with_name("linked.secret")
        symlink.symlink_to(source)
        for path in (foreign, symlink, self.env_file.with_name("secrets.json")):
            with self.subTest(path=path.name), self.assertRaises(DeployError):
                candidate_values(self.host, self.entry, {}, {"CUSTOM__VALUE_FILE": str(path)})

    def test_configuration_accepts_sixteen_values_and_blocks_owned_aliases_and_leaves(self):
        archive, _ = self.bundle()
        self.deploy(archive)
        self.host.inspect_output = json.dumps([{"Name": f"custom.key_{n}", "Source": "defaults", "Secret": False} for n in range(16)])
        result = release.config_set(self.host, self.project, self.profile, {f"CUSTOM__KEY_{n}": str(n) for n in range(16)})
        self.assertEqual(2, result["configuration_revision"])
        self.host.commands.clear()
        for key in ("APP__SERVICES__REDIS__CONNECTIONS_FILE", "APP__SERVICES__REDIS__CONNECTIONS__MAIN__PASSWORD",
                    "DEVELOPER__RELEASE_FILE"):
            with self.subTest(key=key), self.assertRaises(DeployError):
                release.config_set(self.host, self.project, self.profile, {key: "foreign"})
        self.assertEqual([], self.host.commands)

    def test_public_configuration_state_omits_private_hashes(self):
        archive, _ = self.bundle()
        self.deploy(archive)
        release.config_set(self.host, self.project, self.profile, {"PROJECT__NAME": "Changed"})
        state = self.state()
        public = release.public_state(state)
        self.assertIn("configuration_sha256", state["last_attempt"])
        self.assertNotIn("configuration_sha256", json.dumps(public))
        self.assertNotIn("previous_configuration_sha256", json.dumps(public))
        persisted = (self.host.paths.instance_state(self.entry["name"]) / "state.json").read_text()
        self.assertNotIn("configuration_sha256", persisted)
        self.assertIn('"configuration_redacted": true', persisted)


    def test_worker_selects_toml_then_private_environment_connection(self):
        archive, _ = self.bundle()
        self.deploy(archive)
        selected = self.env_file.with_name("worker.env")
        self.assertEqual("background", parse_env(selected.read_text())["FOUNDRY_WORKER_CONNECTION"])
        values = parse_env(self.env_file.read_text())
        values["APP__WORKER__CONNECTION"] = "custom-jobs"
        atomic_write(self.env_file, dump_env(values), 0o640)
        release.prepare_worker(self.host, self.entry)
        self.assertEqual("custom-jobs", parse_env(selected.read_text())["FOUNDRY_WORKER_CONNECTION"])
        values["APP__WORKER__CONNECTION"] = "jobs --arbitrary-command"
        atomic_write(self.env_file, dump_env(values), 0o640)
        before = selected.read_bytes()
        with self.assertRaises(DeployError):
            release.prepare_worker(self.host, self.entry)
        self.assertEqual(before, selected.read_bytes())

    def test_lifecycle_preserves_backup_freshness_written_by_backup_owner(self):
        archive, _ = self.bundle()
        self.host.pending = True
        def record(host, entry, operation):
            state = host.load_state(entry)
            state["backup"] = {"last_complete_id": operation, "last_complete_at": now(), "last_attempt_outcome": "complete"}
            host.save_state(entry, state)
            return {"backup_id": operation}
        with patch.object(release.backup, "recovery_point", side_effect=record):
            self.deploy(archive)
        self.assertEqual("complete", self.state()["backup"]["last_attempt_outcome"])
        self.assertEqual(self.state()["last_attempt"]["recovery_operation_id"], self.state()["backup"]["last_complete_id"])



class RuntimePortTests(unittest.TestCase):
    def test_runtime_uses_the_allocated_postgres_port(self):
        from test_contracts import fixture
        _, _, entry = fixture()
        values = runtime_environment(entry, {"database_password": "private", "redis_password": "private"}, postgres_port=55433)
        databases = json.loads(values["APP__SERVICES__DATABASE__CONNECTIONS"])
        self.assertEqual(55433, databases["primary"]["primary"]["port"])


if __name__ == "__main__":
    unittest.main()
