"""Credential replacement and bootstrap replay against private fixture files."""
from contextlib import ExitStack, contextmanager
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "lib"))

import backup
import certificates
import provision
import runtime_config
import upgrades
from contracts import DeployError, dump_env, read_json
from host import Host, Paths
from test_contracts import fixture, registry
from test_manage import manager

TOOLS = Path(__file__).resolve().parents[1]


def bootstrap_without_secrets(host, project, profile, *, backup_stage=None, certificate_stage=None):
    """Run apply and its real intake/environment owners; substitute host services."""
    current = host.read_registry()
    entry = current["instances"][profile["instance_id"]]
    plan = {"ok": True, "findings": [], "plan_token": "reviewed", "entry": entry,
            "observations": {"packages": {"missing": []}}}
    with ExitStack() as stack:
        stack.enter_context(patch.object(provision, "bootstrap_plan", return_value=plan))
        for module, names in ((provision, ("_ensure_roots", "_prepare_instance", "_install_unit")),
                              (upgrades, ("install_tools",)),
                              (provision.packages, ("install_missing",)),
                              (provision.postgres, ("prepare", "provision")),
                              (provision.redis_acl, ("prepare", "provision"))):
            for name in names:
                stack.enter_context(patch.object(module, name))
        stack.enter_context(patch.object(backup, "configure", side_effect=backup_stage))
        stack.enter_context(patch.object(provision, "_install_site",
            side_effect=lambda host, registry, entry, *args, **kwargs: certificate_stage(host, registry, entry)
                        if certificate_stage else None))
        return provision.bootstrap_apply(host, project, profile, TOOLS, "reviewed", topology=entry["topology"])


class RotationTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.host = Host(Paths(Path(temporary.name).resolve()))
        self.project, self.profile, self.entry = fixture()
        self.other_project, self.other_profile, self.other = fixture(
            project_slug="other", host_id=self.profile["host"]["host_id"], port=8101)
        self.registry = registry(self.entry, self.other)
        self.cluster = {"ownership_token": "cluster-token", "port": 55432,
                        "data_path": "/var/lib/foundry/postgresql/18/foundry", "socket_directory": "/run/foundry/postgresql"}
        self.registry["shared"]["postgresql"] = {"port": 55432}
        self.repository = dict(endpoint="https://backup.example.invalid", region="auto", bucket="backups",
                               access_key_id="old-access-canary", secret_access_key="old-secret-canary")
        self.recipients = ["age1" + "a" * 58]
        self.token = "old-token-canary-" * 3
        for entry in (self.entry, self.other):
            entry["certificate"]["zone_ref"] = "a" * 32
            path = self.host.paths.instance_config(entry["name"])
            self.host.owned_write(path / "secrets.json", json.dumps({
                "instance_id": entry["profile"]["instance_id"], "database_password": "d" * 43,
                "redis_password": "r" * 43}), mode=0o600)
            credentials = runtime_config.initialize_bootstrap_secrets(self.host, entry, {
                "backup_s3": self.repository, "age_recipients": self.recipients,
                "cloudflare_dns_token": self.token})
            credentials["encryption"].pop("key_file")
            self.host.owned_write(path / "app.env", dump_env(runtime_config.runtime_environment(
                entry, credentials, postgres_port=self.cluster["port"])), mode=0o640)
            backup._stage_settings(self.host, self.registry, entry, self.cluster)
            certificates._stage_credentials(self.host, self.registry, entry)
        self.host.write_registry(self.registry)
        backup._install_configuration(self.host, self.cluster)
        self.host.paths.p(backup.CONFIG_DIR).chmod(0o700)
        self.commands = []
        self.archives = []
        self.host.run = self.command
        patcher = patch.object(backup, "_cluster", return_value=self.cluster)
        patcher.start()
        self.addCleanup(patcher.stop)

    def command(self, args, **kwargs):
        self.commands.append(list(args))
        if args[0] == "age":
            import tarfile
            with tarfile.open(args[-1]) as archive:
                self.archives.append({member.name: archive.extractfile(member).read() for member in archive.getmembers()})
            Path(args[args.index("-o") + 1]).write_bytes(b"encrypted")
        return subprocess.CompletedProcess(args, 0, "", "")

    def request(self, action, name, value):
        return manager.execute(self.host, action, {**upgrades.protocol_metadata(),
            "project": self.project, "profile": self.profile, "confirm": True, "secrets": {name: value}})

    def replay(self, project, profile):
        result = bootstrap_without_secrets(self.host, project, profile,
            backup_stage=lambda host, registry, entry: backup._stage_settings(host, registry, entry, self.cluster),
            certificate_stage=certificates._stage_credentials)
        self.assertTrue(result["ok"])

    def intake(self, entry):
        return read_json(self.host.paths.instance_config(entry["name"]) / "bootstrap-secrets.json")

    def test_bad_backup_key_can_be_corrected_before_backup_completes(self):
        original = backup._repository_settings(self.host)
        corrected = {**self.repository, "secret_access_key": "corrected-secret-canary"}
        evidence = []
        def backrest(host, arguments, **kwargs):
            config = kwargs.get("config") or host.paths.p(backup.CONFIG)
            text = config.read_text()
            if arguments == ["--output=json", "info"]:
                self.assertIn("corrected-secret-canary", text)
                # Inventory must not select candidate credentials for live WAL.
                self.assertIn(self.repository["secret_access_key"], host.paths.p(backup.CONFIG).read_text())
                evidence.append(True)
                return subprocess.CompletedProcess([], 0, json.dumps([
                    {"name": backup.STANZA, "status": {"code": 1}, "backup": []}]), "")
            if "corrected-secret-canary" not in text:
                raise DeployError("command_failed", "provider-private-canary")
            return subprocess.CompletedProcess([], 0, "", "")
        with patch.object(backup.packages, "install_missing"), patch.object(backup, "_cluster_owned_directory"), \
                patch.object(backup, "_remove_wal_bridge"), patch.object(backup, "rebind"), \
                patch.object(backup, "_backrest", side_effect=backrest):
            with self.assertRaises(DeployError):
                backup.configure(self.host, self.registry, self.entry)
            self.assertNotIn("backup", self.entry["provisioning"]["completed_steps"])
            before = self.intake(self.entry)
            runtime_config.initialize_bootstrap_secrets(self.host, self.entry, {"backup_s3": corrected})
            self.assertEqual({**before, "backup_s3": corrected}, self.intake(self.entry))
            backup.configure(self.host, self.registry, self.entry)
            self.assertEqual([True], evidence)
            # Identical retries neither need an empty repository nor change secrets.
            provision._step(self.host, self.registry, self.entry, "backup")
            runtime_config.initialize_bootstrap_secrets(self.host, self.entry, {"backup_s3": corrected})
            backup.configure(self.host, self.registry, self.entry)
            self.assertEqual([True], evidence)
        selected = backup._repository_settings(self.host)
        self.assertEqual(corrected["secret_access_key"], selected["secret_access_key"])
        self.assertIn("repo1-s3-key-secret=corrected-secret-canary", self.host.paths.p(backup.CONFIG).read_text())
        for key in ("cipher_pass", "host_id", "cluster_token"):
            self.assertEqual(original[key], selected[key])

    def test_backup_correction_refuses_completed_shared_step_or_backup_evidence(self):
        paths = [self.host.paths.p(backup.REPOSITORY), self.host.paths.p(backup.CONFIG),
                 backup._instance_settings_path(self.host, self.entry)]
        corrected = {**self.repository, "secret_access_key": "corrected-secret-canary"}
        runtime_config.initialize_bootstrap_secrets(self.host, self.entry, {"backup_s3": corrected})
        before = [path.read_bytes() for path in paths]
        self.other["provisioning"]["completed_steps"].append("backup")
        with patch.object(backup, "_info") as info, patch.object(self.host, "owned_write") as write, \
                self.assertRaises(DeployError) as caught:
            backup._stage_settings(self.host, self.registry, self.entry, self.cluster)
        self.assertEqual("backup_repository_conflict", caught.exception.code)
        write.assert_not_called()
        info.assert_not_called()
        self.other["provisioning"]["completed_steps"].remove("backup")
        for result in ([{"label": "20261101-020000F", "timestamp": {"stop": 1}}], None):
            with self.subTest(result=result), patch.object(backup, "_info", return_value=result,
                    side_effect=DeployError("unavailable", "private-canary") if result is None else None), \
                    self.assertRaises(DeployError) as caught:
                backup._stage_settings(self.host, self.registry, self.entry, self.cluster)
            self.assertEqual("backup_repository_conflict", caught.exception.code)
            self.assertNotIn("canary", str(caught.exception))
            self.assertEqual(before, [path.read_bytes() for path in paths])
            self.assertEqual([], list(self.host.paths.p(backup.CONFIG_DIR).glob("bootstrap-*.conf")))
        # Retained local evidence prevents treating expired backups as never used.
        journal = self.host.paths.p("/etc/foundry/backup/operations") / "retained.json"
        backup._write_private(self.host, journal, {"components": {"database": {"status": "complete"}}})
        with patch.object(backup, "_info", return_value=[]) as info, patch.object(self.host, "owned_write") as write, \
                self.assertRaises(DeployError):
            backup._stage_settings(self.host, self.registry, self.entry, self.cluster)
        info.assert_not_called()
        write.assert_not_called()
        self.assertEqual(before, [path.read_bytes() for path in paths])

    def test_backup_correction_refuses_ambiguous_remote_inventory(self):
        corrected = {**self.repository, "secret_access_key": "corrected-secret-canary"}
        runtime_config.initialize_bootstrap_secrets(self.host, self.entry, {"backup_s3": corrected})
        path = self.host.paths.p(backup.REPOSITORY)
        original = path.read_bytes()
        for inventory in ([], [{"name": backup.STANZA, "status": {"code": 3}, "backup": []}],
                          [{"name": backup.STANZA, "status": {"code": 99}, "backup": []}],
                          [{"name": backup.STANZA, "status": {"code": 2}}]):
            result = subprocess.CompletedProcess([], 0, json.dumps(inventory), "")
            with self.subTest(inventory=inventory), patch.object(backup, "_backrest", return_value=result), \
                    self.assertRaises(DeployError) as caught:
                backup._stage_settings(self.host, self.registry, self.entry, self.cluster)
            self.assertEqual("backup_repository_conflict", caught.exception.code)
            self.assertEqual(original, path.read_bytes())

    def test_unfinished_backup_recipients_require_the_same_unused_repository_proof(self):
        corrected = ["age1" + "b" * 58]
        runtime_config.initialize_bootstrap_secrets(self.host, self.entry, {"age_recipients": corrected})
        path = backup._instance_settings_path(self.host, self.entry)
        before = path.read_bytes()
        with patch.object(backup, "_info", return_value=[{"label": "completed"}]), self.assertRaises(DeployError) as caught:
            backup._stage_settings(self.host, self.registry, self.entry, self.cluster)
        self.assertEqual("backup_configuration_conflict", caught.exception.code)
        self.assertEqual(before, path.read_bytes())
        with patch.object(backup, "_info", return_value=[]):
            backup._stage_settings(self.host, self.registry, self.entry, self.cluster)
        self.assertEqual(corrected, backup._settings(self.host, self.entry)["age_recipients"])

    def test_bad_dns_token_retry_preserves_zone_and_refuses_completed_sibling(self):
        token = "corrected-token-canary-" * 3
        path = certificates._credentials(self.host, self.entry)
        def dry_run(host, entry):
            if token not in path.read_text():
                raise DeployError("command_failed", "private-provider-canary")
        with patch.object(certificates, "inspect", return_value={"state": "ready"}), \
                patch.object(certificates.packages, "install_missing"), \
                patch.object(certificates, "_identity_ready"), patch.object(certificates, "_renewal_ready"), \
                patch.object(certificates, "_renewal_dry_run", side_effect=dry_run), \
                patch.object(certificates, "_staging_unreachable", return_value=False), \
                patch.object(certificates, "_resolve_zone", return_value="a" * 32):
            with self.assertRaises(DeployError):
                certificates.provision(self.host, self.registry, self.entry)
            self.assertNotIn("certificate", self.entry["provisioning"]["completed_steps"])
            before = self.intake(self.entry)
            runtime_config.initialize_bootstrap_secrets(self.host, self.entry, {"cloudflare_dns_token": token})
            self.assertEqual({**before, "cloudflare_dns_token": token}, self.intake(self.entry))
            self.other["provisioning"]["completed_steps"].append("certificate")
            previous = path.read_bytes()
            with patch.object(self.host, "owned_write") as write, self.assertRaises(DeployError) as caught:
                certificates._stage_credentials(self.host, self.registry, self.entry)
            self.assertEqual("certificate_credentials_conflict", caught.exception.code)
            write.assert_not_called()
            self.assertEqual(previous, path.read_bytes())
            self.other["provisioning"]["completed_steps"].remove("certificate")
            with patch.object(certificates, "_resolve_zone", return_value="b" * 32), self.assertRaises(DeployError):
                certificates._stage_credentials(self.host, self.registry, self.entry)
            self.assertEqual(previous, path.read_bytes())
            certificates.provision(self.host, self.registry, self.entry)
            runtime_config.initialize_bootstrap_secrets(self.host, self.entry, {"cloudflare_dns_token": token})
            certificates.provision(self.host, self.registry, self.entry)
        self.assertIn(token, path.read_text())
        self.assertIn("certificate", self.entry["provisioning"]["completed_steps"])
        self.assertEqual("a" * 32, self.entry["certificate"]["zone_ref"])
        self.assertNotIn(token, json.dumps(self.host.read_registry()))

    def test_intake_completion_routes_and_encryption_are_immutable(self):
        from test_contracts import storage_fixture
        replacements = {"app_storage": storage_fixture("corrected/"),
                        "configuration": {"PROJECT__NAME": "corrected-private-canary"},
                        "backup_s3": {**self.repository, "secret_access_key": "corrected-private-canary"},
                        "age_recipients": ["age1" + "b" * 58],
                        "cloudflare_dns_token": "corrected-token-canary-" * 3,
                        "encryption": {"key_id": "replacement", "key": "b" * 42 + "A"}}
        path = self.host.paths.instance_config(self.entry["name"]) / "bootstrap-secrets.json"
        for key, replacement in replacements.items():
            step, route = provision.BOOTSTRAP_SECRET_STEPS[key]
            with self.subTest(key=key):
                if step is not None:
                    before = self.intake(self.entry)
                    runtime_config.initialize_bootstrap_secrets(self.host, self.entry, {key: replacement})
                    self.assertEqual({**before, key: replacement}, self.intake(self.entry))
                    self.entry["provisioning"]["completed_steps"] = [step]
                    same = path.read_bytes()
                    runtime_config.initialize_bootstrap_secrets(self.host, self.entry, {key: replacement})
                    self.assertEqual(same, path.read_bytes())
                    # A previously omitted input is still a change after completion.
                    self.host.owned_write(path, json.dumps(before), mode=0o600)
                before_bytes = path.read_bytes()
                with self.assertRaises(DeployError) as caught:
                    runtime_config.initialize_bootstrap_secrets(self.host, self.entry, {key: replacement})
                self.assertEqual("provision_conflict", caught.exception.code)
                self.assertIn(route, str(caught.exception))
                self.assertNotIn("canary", str(caught.exception))
                self.assertEqual(before_bytes, path.read_bytes())
                self.entry["provisioning"]["completed_steps"] = []
        for completed in ([], ["configuration", "backup", "certificate"]):
            self.entry["provisioning"]["completed_steps"] = completed
            with self.subTest(completed=completed), self.assertRaises(DeployError) as caught:
                runtime_config.initialize_bootstrap_secrets(self.host, self.entry, {"encryption": replacements["encryption"]})
            self.assertEqual("provision_conflict", caught.exception.code)

    def test_keys_rotation_refreshes_all_archives_then_bootstrap_without_secrets(self):
        changed = {**self.repository, "access_key_id": "new-access-canary", "secret_access_key": "new-secret-canary"}
        before = backup._repository_settings(self.host)
        first = backup.archive_keys(self.host, self.entry)
        result = self.request("backup-keys-rotate", "backup_s3", changed)
        self.assertEqual({self.profile["instance_id"], self.other_profile["instance_id"]}, set(result["key_archives"]))
        self.assertNotEqual(first["object_key"], result["key_archives"][self.profile["instance_id"]]["object_key"])
        self.assertTrue(any(command[-1] == "check" and "pgbackrest" in command for command in self.commands))
        self.replay(self.project, self.profile)
        self.replay(self.other_project, self.other_profile)
        for entry in (self.entry, self.other):
            self.assertEqual(changed, self.intake(entry)["backup_s3"])
        for archive in self.archives[1:]:
            recovered = json.loads(archive["recovery/pgbackrest-repository.json"])
            self.assertEqual(changed["secret_access_key"], recovered["secret_access_key"])
        after = backup._repository_settings(self.host)
        self.assertEqual(changed["secret_access_key"], after["secret_access_key"])
        for key in ("cipher_pass", "host_id", "cluster_token"):
            self.assertEqual(before[key], after[key])
        for secret in (changed["access_key_id"], changed["secret_access_key"]):
            self.assertNotIn(secret, json.dumps(result))
            self.assertNotIn(secret, json.dumps(self.commands))
            self.assertNotIn(secret, json.dumps(self.host.read_registry()))
            self.assertNotIn(secret, json.dumps(self.host.load_state(self.entry)))

    def test_keys_refuse_destination_changes_before_verification(self):
        before = self.host.paths.p(backup.CONFIG).read_bytes()
        for key, value in (("endpoint", "https://different.invalid"), ("region", "other"), ("bucket", "other")):
            with self.subTest(key=key), self.assertRaises(DeployError) as caught:
                self.request("backup-keys-rotate", "backup_s3", {**self.repository, key: value})
            self.assertEqual("backup_destination_conflict", caught.exception.code)
            self.assertEqual(before, self.host.paths.p(backup.CONFIG).read_bytes())
        self.assertEqual([], self.commands)

    def test_keys_check_failure_restores_exact_settings_and_configuration(self):
        paths = [self.host.paths.p(backup.REPOSITORY), self.host.paths.p(backup.CONFIG),
                 *[self.host.paths.instance_config(entry["name"]) / "bootstrap-secrets.json"
                   for entry in (self.entry, self.other)]]
        # Preserve valid operator formatting too, not merely equivalent settings.
        paths[1].write_text(paths[1].read_text() + "\n# retained formatting\n")
        before = [path.read_bytes() for path in paths]
        with patch.object(backup, "_backrest", side_effect=DeployError("failed", "provider-secret-canary")), \
                patch.object(backup, "archive_keys") as archive, self.assertRaises(DeployError) as caught:
            self.request("backup-keys-rotate", "backup_s3", {**self.repository, "secret_access_key": "replacement-canary"})
        archive.assert_not_called()
        self.assertEqual(before, [path.read_bytes() for path in paths])
        self.assertNotIn("canary", str(caught.exception))

    def test_bootstrap_record_write_failure_restores_all_records_and_repository(self):
        paths = [self.host.paths.p(backup.REPOSITORY), self.host.paths.p(backup.CONFIG),
                 *[self.host.paths.instance_config(entry["name"]) / "bootstrap-secrets.json"
                   for entry in (self.entry, self.other)]]
        before = [path.read_bytes() for path in paths]
        original = self.host.owned_write
        failed = False
        def write(path, *args, **kwargs):
            nonlocal failed
            if path == paths[-1] and not failed:
                failed = True
                raise OSError("private-canary")
            return original(path, *args, **kwargs)
        with patch.object(self.host, "owned_write", side_effect=write), self.assertRaises(DeployError):
            self.request("backup-keys-rotate", "backup_s3", {**self.repository, "secret_access_key": "new-canary"})
        self.assertTrue(failed)
        self.assertEqual(before, [path.read_bytes() for path in paths])

    def test_recipients_replace_only_selected_instance_and_bootstrap_reuses_them(self):
        first = backup.archive_keys(self.host, self.entry)
        changed = ["age1" + "b" * 58]
        result = self.request("backup-recipients-rotate", "age_recipients", changed)
        self.assertNotEqual(first["object_key"], result["key_archive"]["object_key"])
        self.replay(self.project, self.profile)
        self.assertEqual(changed, backup._settings(self.host, self.entry)["age_recipients"])
        self.assertEqual(changed, self.intake(self.entry)["age_recipients"])
        self.assertEqual(self.recipients, self.intake(self.other)["age_recipients"])
        self.assertEqual(changed[0], [command for command in self.commands if command[0] == "age"][-1][2])

    def dns_context(self):
        stack = ExitStack()
        stack.enter_context(patch.object(certificates, "_resolve_zone", return_value="a" * 32))
        stack.enter_context(patch.object(certificates, "_identity_ready"))
        stack.enter_context(patch.object(certificates, "_renewal_ready"))
        return stack

    def test_dns_rotation_updates_shared_zone_then_bootstrap_without_secrets(self):
        token = "replacement-token-canary-" * 3
        with self.dns_context():
            result = self.request("dns-token-rotate", "cloudflare_dns_token", token)
        self.assertEqual({self.entry["name"], self.other["name"]}, set(result["instances"]))
        for command in self.commands:
            self.assertIn("--no-random-sleep-on-renew", command)
        self.assertEqual(2, len(self.commands))
        self.replay(self.project, self.profile)
        self.replay(self.other_project, self.other_profile)
        self.assertIn(token, certificates._credentials(self.host, self.entry).read_text())
        for entry in (self.entry, self.other):
            self.assertEqual(token, self.intake(entry)["cloudflare_dns_token"])
        self.assertNotIn(token, json.dumps(result))
        self.assertNotIn(token, json.dumps(self.commands))
        self.assertNotIn(token, json.dumps(self.host.read_registry()))

    def test_dns_wrong_zone_and_failed_dry_run_preserve_previous_token_and_intakes(self):
        path = certificates._credentials(self.host, self.entry)
        before = path.read_bytes(), self.intake(self.entry), self.intake(self.other)
        with self.dns_context(), patch.object(certificates, "_resolve_zone", return_value="b" * 32), \
                self.assertRaises(DeployError):
            self.request("dns-token-rotate", "cloudflare_dns_token", "new-token-canary-" * 3)
        self.assertEqual([], self.commands)
        with self.dns_context(), patch.object(certificates, "_staging_unreachable", return_value=True) as probe, \
                patch.object(certificates, "_renewal_dry_run", side_effect=DeployError("failed", "provider-secret-canary")), self.assertRaises(DeployError) as caught:
            self.request("dns-token-rotate", "cloudflare_dns_token", "new-token-canary-" * 3)
        probe.assert_not_called()
        self.assertEqual(before, (path.read_bytes(), self.intake(self.entry), self.intake(self.other)))
        self.assertNotIn("canary", str(caught.exception))

    def test_every_rotation_confirms_and_holds_host_then_affected_instance_locks(self):
        cases = (("backup-keys-rotate", "backup_s3", backup, "rotate_keys", True),
                 ("backup-recipients-rotate", "age_recipients", backup, "rotate_recipients", False),
                 ("dns-token-rotate", "cloudflare_dns_token", certificates, "rotate_token", True))
        for action, name, module, function, shared in cases:
            held = []
            expected = ["host", *sorted([self.profile["instance_id"], self.other_profile["instance_id"]]
                                       if shared else [self.profile["instance_id"]])]
            @contextmanager
            def lock(identity):
                held.append(identity)
                try:
                    yield
                finally:
                    self.assertEqual(identity, held.pop())
            def rotate(*args):
                self.assertEqual(expected, held)
                return {"ok": True}
            with self.subTest(action=action), patch.object(self.host, "lock", side_effect=lock), \
                    patch.object(module, function, side_effect=rotate) as handler:
                self.request(action, name, "private")
                self.assertEqual([], held)
                with self.assertRaises(DeployError):
                    manager.execute(self.host, action, {**upgrades.protocol_metadata(), "project": self.project,
                        "profile": self.profile, "secrets": {name: "private"}})
                self.assertEqual(1, handler.call_count)
