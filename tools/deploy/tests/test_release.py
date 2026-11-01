"""Activation and interruption acceptance with real files and simulated processes.

No services are started. These fixtures exercise the production state machine,
archive validation, atomic links, locks and recovery on an isolated temporary host.
"""
import copy
import io
import json
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
from contracts import DeployError, atomic_json, atomic_write, digest, dump_env, identity, now, parse_env
from host import Host, Paths
import release
from runtime_config import CREDENTIAL_KEYS, runtime_environment
from topology import instance_units

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
        # Releases built before redis:keys keep the legacy namespace grant.
        self.commands_output = json.dumps([{"name": "about"}, {"name": "config.check"}])
        self.redis_keys_output = json.dumps([])

    def run(self, args, **kwargs):
        args = list(map(str, args))
        self.commands.append((args, kwargs))
        if self.interrupt and self.interrupt in args:
            raise KeyboardInterrupt()
        if self.fail and self.fail in args:
            raise DeployError("injected_failure", "Synthetic failure")
        output = ""
        if "migrate:status" in args:
            output = json.dumps({"targets": [{"schema": "public", "problems": [], "migrations": [
                {"state": "pending" if self.pending else "applied"}]}]})
        if "config:inspect" in args:
            output = self.inspect_output
        if "command:list" in args:
            output = self.commands_output
        if "redis:keys" in args:
            output = self.redis_keys_output
        if args[0] == "pg_dump":
            Path(next(a[7:] for a in args if a.startswith("--file="))).write_bytes(b"fixture-backup")
        return subprocess.CompletedProcess(args, 0, output, "")


class ReleaseTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name).resolve()
        self.host = ProcessHost(self.root)
        from test_contracts import fixture, registry
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
        self.addCleanup(patch.stopall)

    def recovery_point(self, host, entry, operation_id):
        host.commands.append((["recovery_point"], {}))
        if host.fail == "recovery_point":
            raise DeployError("injected_failure", "Synthetic backup failure")
        return {"id": operation_id}

    def bundle(self, sequence=1, mutate=None, extra=None):
        version = f"20261013T00000{sequence}Z-0123456789abcdef"
        source = self.root / f"source-{sequence}"
        source.mkdir()
        files = {"artisan": b"fixture executable", "config/deploy.toml": b"[project]\n",
                 "public/admin/index.html": b"portal", f"private-source-maps/{version}/admin.js.map": b"private map"}
        for path, data in files.items():
            destination = source / path
            destination.parent.mkdir(parents=True, exist_ok=True)
            destination.write_bytes(data)
        manifest = {"schema": 1, "layout_version": 2, "project_id": self.entry["project_id"],
                    "instance_id": self.entry["profile"]["instance_id"], "instance": self.entry["name"],
                    "environment": self.entry["environment"], "architecture": "arm64", "native_imaging": False,
                    "version": version, "commit": "0123456789abcdef", "tree_state": "clean", "built_at": now(),
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

    def state(self):
        return self.host.load_state(self.entry)

    def test_first_activation_repeat_noop_and_instance_only_restart(self):
        archive, manifest = self.bundle()
        self.deploy(archive)
        commands = len(self.host.commands)
        state_bytes = (self.host.paths.instance_state(self.entry["name"]) / "state.json").read_bytes()
        self.assertTrue(self.deploy(archive)["unchanged"])
        self.assertEqual(commands, len(self.host.commands))
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
        units = instance_units(self.entry)
        self.health.return_value = False
        release.restart(self.host, self.entry)
        self.assertFalse(any(args[:2] == ["systemctl", "start"] for args, _ in self.host.commands))
        self.host.commands.clear()
        self.health.return_value = True
        release.restart(self.host, self.entry)
        calls = [args for args, _ in self.host.commands]
        self.assertLess(calls.index(["systemctl", "restart", units["http"]]), calls.index(["systemctl", "start", units["worker"]]))
        release.stop(self.host, self.entry)
        self.assertEqual(self.host.commands[-2][0], ["systemctl", "stop", units["scheduler"], units["worker"]])
        self.assertEqual(self.host.commands[-1][0], ["systemctl", "stop", units["http"]])

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

    def test_backup_precedes_migration_failure_and_retry_requires_reconciliation(self):
        archive, _ = self.bundle()
        self.host.pending, self.host.fail = True, "migrate"
        with self.assertRaises(DeployError):
            self.deploy(archive)
        commands = [args for args, _ in self.host.commands]
        backup = next(i for i, args in enumerate(commands) if args[0] == "recovery_point")
        migration = next(i for i, args in enumerate(commands) if "migrate" in args)
        self.assertLess(backup, migration)
        self.assertEqual("needs_reconciliation", self.state()["last_attempt"]["outcome"])
        self.assertEqual("migration", self.state()["last_attempt"]["phase"])
        self.assertIsNone(release.current_version(self.host, self.entry))
        self.assertEqual({"id": self.state()["last_attempt"]["id"]}, self.state()["last_attempt"]["backup"])
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
        first, _ = self.bundle(1)
        second, original = self.bundle(2)
        self.deploy(first)
        self.deploy(second)
        with patch.object(release, "restart", side_effect=[DeployError("command_unavailable", "simulated timeout"), None]):
            with self.assertRaises(DeployError):
                release.rollback(self.host, self.project, self.profile)
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
        self.host.fail = "config:check"
        with self.assertRaises(DeployError):
            self.deploy(archive)
        candidate = release.release_path(self.host, self.entry, manifest["version"])
        (candidate / "artisan").write_bytes(b"changed after the failed attempt")
        self.host.fail = None
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
        self.assertFalse(any(args[0] == "systemctl" for args, _ in self.host.commands))
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
        result = release.config_set(self.host, self.project, self.profile,
                                    {"APP__ENCRYPTION__KEY_ID": "fixture_2026", "APP__ENCRYPTION__KEY": "fixture-key-material"})
        self.assertEqual(2, result["configuration_revision"])
        values = parse_env(self.env_file.read_text())
        self.assertEqual(("fixture_2026", "fixture-key-material"), (values["APP__ENCRYPTION__KEY_ID"], values["APP__ENCRYPTION__KEY"]))
        self.assertEqual(1, sum("config:check" in args for args, _ in self.host.commands))
        self.assertNotIn("fixture-key-material", json.dumps(result) + json.dumps(self.state()))

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
        self.assertFalse(any(args[0] == "systemctl" for args, _ in self.host.commands))
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
                self.assertFalse(any(args[0] == "systemctl" for args, _ in self.host.commands))
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
        self.assertNotIn(self.secret, json.dumps(self.state()))

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
        self.assertEqual(["runuser", "-u", self.entry["name"], "--", "python3"], copies[0][:5])
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
        for listener_pid, body, expected in ((42, {"ready": True}, True), (99, {"ready": True}, False), (42, {"ready": 1}, False)):
            def command(args, **kwargs):
                output = "0" if "--value" in args else "ActiveState=active\nNRestarts=0\nMainPID=42"
                if args[0] == "ss":
                    output = f'LISTEN 0 4096 127.0.0.1:8100 0.0.0.0:* users:(("artisan",pid={listener_pid},fd=5))'
                return subprocess.CompletedProcess(args, 0, output, "")
            response = MagicMock()
            response.__enter__.return_value = response
            response.status = 200
            response.read.return_value = json.dumps(body).encode()
            opener = MagicMock()
            opener.open.return_value = response
            with patch.object(self.host, "run", side_effect=command), patch.object(release.urllib.request, "build_opener", return_value=opener):
                self.assertEqual(expected, CHECK_READY(self.host, self.entry, timeout=0.01))

    def test_readiness_wall_clock_bounds_a_slow_response_body(self):
        def command(args, **kwargs):
            output = "0" if "--value" in args else "ActiveState=active\nNRestarts=0\nMainPID=42"
            if args[0] == "ss":
                output = 'LISTEN 0 4096 127.0.0.1:8100 0.0.0.0:* users:(("artisan",pid=42,fd=5))'
            return subprocess.CompletedProcess(args, 0, output, "")
        response = MagicMock()
        response.__enter__.return_value = response
        response.status = 200
        def drip(_):
            time.sleep(0.15)
            return b'{"ready":true}'
        response.read.side_effect = drip
        opener = MagicMock()
        opener.open.return_value = response
        with patch.object(self.host, "run", side_effect=command), patch.object(release.urllib.request, "build_opener", return_value=opener):
            started = time.monotonic()
            self.assertFalse(CHECK_READY(self.host, self.entry, timeout=0.03))
            self.assertLess(time.monotonic() - started, 0.12)

    def test_pruning_preserves_active_and_rollback_and_rollback_never_migrates(self):
        one, _ = self.bundle(1)
        two, second = self.bundle(2)
        three, third = self.bundle(3)
        self.deploy(one)
        self.deploy(two)
        self.deploy(three)
        self.assertEqual({second["version"], third["version"]}, {p.name for p in (self.opt / "releases").iterdir()})
        command = next(args for args, _ in reversed(self.host.commands) if "prune-runtime-maps" in args)
        self.assertEqual(["runuser", "-u", self.entry["name"], "--", "python3"], command[:5])
        self.assertEqual({second["version"], third["version"]}, set(command[8:]))
        self.host.commands.clear()
        release.rollback(self.host, self.project, self.profile)
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
        second, _ = self.bundle(2)
        self.deploy(first)
        self.deploy(second)
        (self.opt / "releases" / original["version"] / "artisan").write_bytes(b"tampered")
        self.host.commands.clear()
        with self.assertRaises(DeployError) as result:
            release.rollback(self.host, self.project, self.profile)
        self.assertEqual("release_conflict", result.exception.code)
        self.assertEqual([], self.host.commands)



if __name__ == "__main__":
    unittest.main()
