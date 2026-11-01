"""Activation and interruption acceptance with real files and simulated processes.

No services are started. These fixtures exercise the production state machine,
archive validation, atomic links, locks and recovery on an isolated temporary host.
"""
import copy
import base64
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
from runtime_config import (CREDENTIAL_KEYS, ENCRYPTION_KEYS, runtime_environment,
                            initialize_bootstrap_secrets, candidate_values, rotation_changes,
                            validate_keyring_change)
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

    def state(self):
        return self.host.load_state(self.entry)

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
        archive, manifest = self.bundle()
        release.unpack_archive(archive, release.release_path(self.host, self.entry, manifest["version"]), self.entry, digest(archive))
        release.switch_to(self.host, self.entry, manifest["version"])
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
                self.assertEqual(expected, CHECK_READY(self.host, self.entry, timeout=0.01))

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
        archive, manifest = self.bundle()
        release.unpack_archive(archive, release.release_path(self.host, self.entry, manifest["version"]), self.entry, digest(archive))
        release.switch_to(self.host, self.entry, manifest["version"])
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

    def test_bootstrap_initializes_keyring_once_and_preserves_private_intake(self):
        intake = {"cloudflare_dns_token": "fixture-token", "age_recipients": ["age1fixture"],
                  "backup_s3": dict(endpoint="https://backup.invalid", region="auto", bucket="backup",
                                    access_key_id="backup-id", secret_access_key="backup-secret"),
                  "app_storage": {"credentials": '{"uploads":{"secret_key":"app-secret"}}',
                                  "disks": '{"uploads":{"driver":"r2"}}'}}
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
        with self.assertRaises(DeployError):
            initialize_bootstrap_secrets(self.host, self.entry, {"cloudflare_dns_token": "replacement"})
        private.unlink()
        with self.assertRaises(DeployError) as missing:
            initialize_bootstrap_secrets(self.host, self.entry)
        self.assertEqual("credentials_missing", missing.exception.code)
        self.assertEqual(before[1], key_file.read_bytes())

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
