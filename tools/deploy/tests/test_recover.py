"""Recovery refusals and interruption boundaries without packages or services."""
import copy
from contextlib import ExitStack
import io
import json
from pathlib import Path
import subprocess
import sys
import tarfile
import tempfile
import unittest
from unittest.mock import patch
import uuid

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "lib"))
import backup
import certificates
import postgres
import capacity
import recover
import upgrades
from contracts import DeployError, dump_env, read_json
from host import Host, Paths
from runtime_config import runtime_environment
from test_contracts import fixture, registry

TOOLS = Path(__file__).resolve().parents[1]
LABEL = "20261101-020000F"


class RecoveryTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name).resolve()
        self.host = Host(Paths(self.root / "host"))
        self.project, self.old_profile, self.entry = fixture()
        self.profile = copy.deepcopy(self.old_profile)
        self.profile["host"].update(address="replacement.example.invalid", host_id=str(uuid.uuid4()))
        self.operation = str(uuid.uuid4())
        self.commands = []
        self.host.run = self.command
        self.host.owned_write(self.host.paths.p("/etc/os-release"), 'ID=ubuntu\nVERSION_ID="24.04"\n')
        self.repository = {"endpoint": "https://backup.example.invalid", "region": "auto", "bucket": "backup-fixture",
                           "access_key_id": "secret-access-canary", "secret_access_key": "secret-key-canary", "cipher_pass": "c" * 43,
                           "host_id": self.old_profile["host"]["host_id"], "cluster_token": "old-cluster"}
        credentials = {"instance_id": self.profile["instance_id"], "database_password": "d" * 43, "redis_password": "r" * 43}
        encryption = {"key_id": "fixture", "key": "a" * 43}
        # Canonical base64url encoding for 32 bytes.
        import base64
        encryption["key"] = base64.urlsafe_b64encode(b"x" * 32).decode().rstrip("=")
        prefix = "instances/" + self.entry["name"] + "/"
        self.files = {"recovery/pgbackrest-repository.json": json.dumps(self.repository).encode(),
                      prefix + "secrets.json": json.dumps(credentials).encode(),
                      prefix + "app.env": dump_env(runtime_environment(self.entry, credentials)).encode(),
                      prefix + "bootstrap-secrets.json": json.dumps({"instance_id": self.profile["instance_id"], "encryption": encryption}).encode(),
                      prefix + "backup.json": json.dumps({"instance_id": self.profile["instance_id"], "age_recipients": ["age1" + "a" * 58]}).encode()}
        self.archive = self.root / "recovery.tar"
        self.write_archive()
        self.arguments = dict(recovery_archive=str(self.archive), topology=self.entry["topology"], mode="rehearsal",
                              operation_id=self.operation, archive_sha256="a" * 64)

    def command(self, args, **kwargs):
        self.commands.append((list(map(str, args)), kwargs))
        output = "arm64\n" if args[:2] == ["dpkg", "--print-architecture"] else ""
        return subprocess.CompletedProcess(args, 0, output, "")

    def write_archive(self):
        with tarfile.open(self.archive, "w") as archive:
            for name, value in self.files.items():
                item = tarfile.TarInfo(name)
                item.size, item.mode = len(value), 0o600
                archive.addfile(item, io.BytesIO(value))

    def plan(self, **changes):
        return recover.plan(self.host, self.project, self.profile, TOOLS, **(self.arguments | changes))

    def apply(self, plan, **changes):
        return recover.apply(self.host, self.project, self.profile, TOOLS, plan_token=plan["plan_token"],
                             archive="unused-release", **(self.arguments | changes))

    def inventory(self, major=16, backups=True):
        return {"backup": [{"label": LABEL, "database": {"id": 1, "repo-key": 1}, "timestamp": {"stop": 1}, "info": {"size": 1}}] if backups else [],
                "db": [{"id": 1, "repo-key": 1, "version": str(major), "system-id": 123}]}

    def test_plan_is_read_only_and_does_not_disclose_secrets(self):
        before = {p.relative_to(self.root): p.read_bytes() for p in self.root.rglob("*") if p.is_file()}
        plan = self.plan()
        self.assertEqual(before, {p.relative_to(self.root): p.read_bytes() for p in self.root.rglob("*") if p.is_file()})
        public = json.dumps(recover.public_plan(plan))
        for secret in ("secret-access-canary", "secret-key-canary", "c" * 43):
            self.assertNotIn(secret, public)
        self.assertIn("pgBackRest", plan["notice"])
        self.assertIn("recreate", plan["notice"])
        self.assertTrue(all(args[0] in {"dpkg", "getent", "ss"} for args, _ in self.commands))

    def test_refuses_registry_and_every_foundry_resource_family(self):
        self.host.write_registry(registry(self.entry))
        with self.assertRaisesRegex(DeployError, "empty"):
            self.plan()
        # Each case gets a new host; never delete framework/server data in a test.
        for number, path in enumerate(("/etc/foundry/orphan", "/var/lib/foundry/orphan", "/opt/foundry/orphan",
                                      "/run/foundry/orphan", "/var/log/foundry/orphan", "/usr/local/lib/foundry/orphan",
                                      "/etc/systemd/system/foundry-http@foreign.service",
                                      "/etc/postgresql/18/foundry", "/usr/local/share/ca-certificates/foundry-old.crt",
                                      "/etc/nginx/sites-available/foundry-old.conf")):
            with self.subTest(path=path):
                self.host = Host(Paths(self.root / str(number)))
                self.host.run = self.command
                self.host.owned_write(self.host.paths.p("/etc/os-release"), 'ID=ubuntu\nVERSION_ID="24.04"\n')
                self.host.owned_write(self.host.paths.p(path), "owned elsewhere")
                with self.assertRaises(DeployError) as error:
                    self.plan()
                self.assertEqual(error.exception.code, "recovery_not_empty")

    def test_refuses_foundry_accounts(self):
        def command(args, **kwargs):
            if args == ["getent", "passwd"]:
                return subprocess.CompletedProcess(args, 0, "foundry-postgres:x:101:101::/:/bin/false\n", "")
            return self.command(args, **kwargs)
        self.host.run = command
        with self.assertRaises(DeployError) as error:
            self.plan()
        self.assertEqual(error.exception.code, "recovery_not_empty")

    def test_missing_foreign_and_unsafe_members_refuse_before_packages(self):
        original = copy.deepcopy(self.files)
        prefix = "instances/" + self.entry["name"] + "/"
        variants = []
        for name in original:
            value = dict(original)
            del value[name]
            variants.append(value)
        variants += [original | {prefix + "secrets.json": json.dumps({"instance_id": str(uuid.uuid4())}).encode()},
                     original | {"../outside": b"secret"}, original | {"instances/other/app.env": b"x"}]
        for files in variants:
            with self.subTest(members=list(files)):
                self.files = files
                self.write_archive()
                with patch.object(recover.packages, "install_missing") as install:
                    with self.assertRaises(DeployError):
                        self.plan()
                    install.assert_not_called()

    def test_unsupported_os_and_missing_mode_refuse_read_only(self):
        with self.assertRaises(DeployError):
            self.plan(mode=None)
        self.host.owned_write(self.host.paths.p("/etc/os-release"), 'ID=ubuntu\nVERSION_ID="22.04"\n')
        with self.assertRaises(DeployError) as error:
            self.plan()
        self.assertEqual(error.exception.code, "unsupported_host")

    def test_metadata_archive_and_legacy_archive_select_same_allocations(self):
        legacy = self.plan()["_recovered"]
        metadata = copy.deepcopy(self.entry)
        metadata["allocations"].pop("uid")
        metadata["allocations"].pop("gid")
        metadata["release_commit"] = "a" * 40
        self.files["recovery/instance.json"] = json.dumps(metadata).encode()
        self.write_archive()
        current = self.plan()["_recovered"]
        self.assertIsNone(legacy["metadata"])
        self.assertEqual(current["metadata"]["release_commit"], "a" * 40)
        for key in ("topology", "http_port", "postgres_port"):
            self.assertEqual(legacy[key], current[key])
        metadata["profile"]["instance_id"] = str(uuid.uuid4())
        self.files["recovery/instance.json"] = json.dumps(metadata).encode()
        self.write_archive()
        with self.assertRaises(DeployError):
            self.plan()

    def test_resources_resume_after_roots_and_preserve_private_certificate_claim(self):
        prefix = "instances/" + self.entry["name"] + "/"
        claim = {"instance_id": self.profile["instance_id"], "domain": self.profile["domain"],
                 "lineage": self.profile["domain"], "zone_ref": "a" * 32}
        self.files[prefix + "certificate.json"] = json.dumps(claim).encode()
        self.write_archive()
        plan = self.plan()
        journal = {"intent": plan["_intent"], "phase": "resources", "completed": ["preflight"],
                   "source": {"label": LABEL}}
        recover._save(self.host, journal)
        roots = recover.provision._ensure_roots
        def interrupted(host):
            roots(host)
            raise KeyboardInterrupt()
        with patch.object(recover.provision, "_ensure_roots", side_effect=interrupted):
            with self.assertRaises(KeyboardInterrupt):
                recover._resources(self.host, journal, plan["_recovered"], self.files, TOOLS)
        self.assertEqual(self.plan()["phase"], "resources")
        with self.host.lock("host"), patch.object(recover.upgrades, "prune_tools", return_value=[]), \
                patch.object(recover.packages, "install_missing"), \
                patch.object(recover.provision, "_prepare_instance"), patch.object(recover, "select_runtime_storage"):
            recover._resources(self.host, journal, plan["_recovered"], self.files, TOOLS)
        path = self.host.paths.instance_config(self.entry["name"]) / "certificate.json"
        self.assertEqual(read_json(certificates._private(self.host, path)), claim)

    def test_resume_refuses_unready_services_before_backup_timers_or_success(self):
        for completed in (list(recover.PHASES[:5]), list(recover.PHASES[:-1])):
            with self.subTest(completed=completed):
                entry = copy.deepcopy(self.entry)
                entry["host_id"] = self.profile["host"]["host_id"]
                entry["profile"] = self.profile
                self.host.write_registry(registry(entry))
                journal = {"intent": {"mode": "takeover", "profile": self.profile}, "phase": "backup",
                           "completed": completed, "release": {"version": "recovered"}, "backup_id": str(uuid.uuid4())}
                recover._save(self.host, journal)
                plan = {"plan_token": "token", "_recovered": {}}
                with patch.object(recover, "plan", return_value=plan), \
                        patch.object(recover, "_ready", side_effect=DeployError("readiness_failed", "Services stopped")), \
                        patch.object(backup, "run_backup") as take, patch.object(backup, "_install_timers") as timers:
                    with self.assertRaises(DeployError) as error:
                        self.apply(plan, mode="takeover", old_server_gone=True)
                self.assertEqual(error.exception.code, "readiness_failed")
                take.assert_not_called()
                timers.assert_not_called()
                self.assertNotEqual(read_json(self.host.paths.p(recover.JOURNAL))["phase"], "complete")

    def test_preflight_installs_only_pgbackrest_and_deletes_credentials_on_refusal(self):
        for version, backups, code in ((18, True, "recovery_major_mismatch"), (16, False, "recovery_no_backup")):
            with self.subTest(version=version, backups=backups):
                journal = {"intent": {"major": 16, "target_time": None}, "phase": "preflight"}
                recovered = {"repository": self.repository, "postgres_port": 55432}
                def info(host, **options):
                    path = options["config"]
                    self.assertEqual(path.stat().st_mode & 0o777, 0o600)
                    self.assertIn("secret-key-canary", path.read_text())
                    self.assertEqual(options["user"], "root")
                    return self.inventory(version, backups)
                with patch.object(recover.packages, "install_missing") as install, patch.object(backup, "_info", side_effect=info):
                    with self.assertRaises(DeployError) as error:
                        recover._verify_repository(self.host, journal, recovered)
                install.assert_called_once_with(self.host, ["pgbackrest"])
                self.assertEqual(error.exception.code, code)
                self.assertIn("PostgreSQL 16", error.exception.detail)
                self.assertIn("Recreate", error.exception.detail)
                if backups:
                    self.assertIn("PostgreSQL 18", error.exception.detail)
                    self.assertIn("26.04", error.exception.detail)
                self.assertEqual(read_json(self.host.paths.p(recover.JOURNAL))["phase"], "refused")
                self.assertFalse((self.host.paths.p(recover.JOURNAL).parent / "preflight.conf").exists())
                self.assertIsNone(self.host.read_registry(missing=True))

    def test_preflight_root_access_can_only_issue_info(self):
        config = self.root / "private.conf"
        for command in ("backup", "expire", "check", "archive-push", "stanza-create", "restore"):
            with self.assertRaises(DeployError):
                backup._backrest(self.host, [command], config=config, user="root")
        backup._backrest(self.host, ["--output=json", "info"], config=config, user="root")
        args = self.commands[-1][0]
        self.assertEqual(args[:5], ["runuser", "-u", "root", "--", "pgbackrest"])
        self.assertEqual(args[-1], "info")

    def test_plan_token_and_takeover_confirmation_precede_install(self):
        plan = self.plan()
        with patch.object(recover.packages, "install_missing") as install:
            with self.assertRaises(DeployError):
                self.apply(plan | {"plan_token": "wrong"})
            with self.assertRaises(DeployError) as error:
                self.apply(plan, mode="takeover")
            self.assertEqual(error.exception.code, "old_server_confirmation")
            install.assert_not_called()

    def test_rehearsal_guards_direct_writes_and_all_mutating_actions(self):
        entry = copy.deepcopy(self.entry)
        entry.update(recovered_from={"mode": "rehearsal"})
        entry["provisioning"]["state"] = "rehearsal"
        self.host.write_registry(registry(entry))
        for command in ("backup", "expire", "check", "archive-push", "stanza-create", "stanza-upgrade", "stanza-delete"):
            with self.assertRaises(DeployError):
                backup._backrest(self.host, [command])
        with self.assertRaises(DeployError):
            backup.archive_keys(self.host, entry)
        with self.assertRaises(DeployError):
            backup._install_timers(self.host, self.host.read_registry())
        for action in ("activate", "restart", "stop", "config-set", "keys-rotate", "bootstrap-apply", "tools-apply", "backup-run", "backup-configure", "restore-drill"):
            with self.assertRaises(DeployError) as error:
                upgrades.check_client(self.host, {}, action)
            self.assertEqual(error.exception.code, "rehearsal_read_only")
        for command in ("info", "archive-get"):
            backup._backrest(self.host, [command])
        self.assertTrue(all(args[-1] in {"info", "archive-get"} for args, _ in self.commands))

    def test_resumes_after_every_orchestration_phase_without_repeating_completed_phases(self):
        for failed_phase in recover.PHASES[:-1]:
            with self.subTest(phase=failed_phase), tempfile.TemporaryDirectory() as temporary, ExitStack() as stack:
                host = Host(Paths(Path(temporary)))
                host.run = self.command
                entry = copy.deepcopy(self.entry)
                entry["profile"] = copy.deepcopy(self.profile)
                entry["host_id"] = self.profile["host"]["host_id"]
                entry["recovered_from"] = {"mode": "takeover", "operation_id": self.operation}
                entry["provisioning"]["state"] = "preparing"
                host.write_registry(registry(entry))
                intent = {"mode": "takeover", "major": 16}
                approved = {"plan_token": "token", "_intent": intent, "_recovered": {"repository": self.repository, "postgres_port": 55432}}
                stack.enter_context(patch.object(recover, "plan", return_value=approved))
                stack.enter_context(patch.object(recover, "_ready"))
                events = []
                interrupted = [False]
                def step(name):
                    def run(*args, **kwargs):
                        events.append(name)
                        if name == "preflight":
                            args[1]["source"] = {"label": LABEL}
                        return {"version": "recovered"} if name == "release" else {"status": "complete"}
                    return run
                bindings = [(recover, "_verify_repository", "preflight"), (recover, "_resources", "resources"),
                            (postgres, "recover_cluster", "database"), (recover.redis_acl, "prepare", "application"),
                            (recover.release, "activate_recovered", "release"), (backup, "run_backup", "backup"),
                            (backup, "_install_timers", "timers")]
                for owner, name, phase in bindings:
                    stack.enter_context(patch.object(owner, name, side_effect=step(phase)))
                for owner, name in ((backup, "_backrest"), (recover, "validate_runtime_environment"),
                                    (recover, "read_credentials"), (recover.redis_acl, "provision"),
                                    (recover.provision, "_install_unit"), (recover.provision, "_install_site")):
                    stack.enter_context(patch.object(owner, name))
                save = recover._save
                def crash_after_phase(host, journal):
                    save(host, journal)
                    if journal["completed"] and journal["completed"][-1] == failed_phase and not interrupted[0]:
                        interrupted[0] = True
                        raise KeyboardInterrupt()
                stack.enter_context(patch.object(recover, "_save", side_effect=crash_after_phase))
                args = self.arguments | {"mode": "takeover", "old_server_gone": True, "plan_token": "token", "archive": "unused"}
                with self.assertRaises(KeyboardInterrupt):
                    recover.apply(host, self.project, self.profile, TOOLS, **args)
                result = recover.apply(host, self.project, self.profile, TOOLS, **args)
                again = recover.apply(host, self.project, self.profile, TOOLS, **args)
                self.assertEqual(result, again)
                self.assertEqual(events, list(recover.PHASES[:-1]))
                self.assertEqual(result["remaining_manual_steps"], ["Point DNS at the new server."])

    def test_failed_restore_is_not_automatically_restarted(self):
        def command(args, **kwargs):
            output = "ActiveState=failed\nExecMainStartTimestampMonotonic=123\n" if args[:2] == ["systemctl", "show"] else ""
            self.commands.append((args, kwargs))
            return subprocess.CompletedProcess(args, 0, output, "")
        self.host.run = command
        with self.assertRaises(DeployError) as error:
            backup._restore_database(self.host, "foundry-recover-fixture.service", self.root / "restore.service",
                                     ["--pg1-path=" + str(self.root / "data"), "--archive-mode=off"], retain=True)
        self.assertEqual(error.exception.code, "recovery_restore_unknown")
        self.assertFalse(any(args[:2] == ["systemctl", "start"] for args, _ in self.commands))

    def test_control_data_drives_recovery_limits(self):
        output = "Database system identifier: 123\n" + "\n".join(name + " setting: " + value for name, value in
            (("max_connections", "200"), ("max_worker_processes", "16"), ("max_wal_senders", "10"),
             ("max_prepared_xacts", "0"), ("max_locks_per_xact", "128")))
        self.host.run = lambda *a, **k: subprocess.CompletedProcess([], 0, output, "")
        limits, identifier = postgres._restored_limits(self.host, 16, self.root / "data")
        self.assertIn("max_connections = 200\n", limits)
        self.assertIn("max_locks_per_transaction = 128\n", limits)
        self.assertEqual(identifier, "123")

    def test_postgres_recovery_resumes_each_phase_without_second_restore(self):
        for failed_phase in ("reserve", "restore", "restored", "replay", "promoted", "managed", "complete"):
            with self.subTest(phase=failed_phase), tempfile.TemporaryDirectory() as temporary, ExitStack() as stack:
                host = Host(Paths(Path(temporary)))
                host.owned_write(host.paths.p("/proc/meminfo"), "MemTotal: 4194304 kB\n")
                entry = copy.deepcopy(self.entry)
                entry["profile"] = copy.deepcopy(self.profile)
                entry["host_id"] = self.profile["host"]["host_id"]
                entry["recovered_from"] = {"host_id": self.old_profile["host"]["host_id"], "mode": "rehearsal"}
                registered = registry(entry)
                host.write_registry(registered)
                commands = []
                def command(args, **kwargs):
                    commands.append(args)
                    if args[:2] == ["systemctl", "is-failed"]:
                        return subprocess.CompletedProcess(args, 1, "", "")
                    output = "MainPID=0\nActiveState=inactive\nControlGroup=\n" if args[:2] == ["systemctl", "show"] else ""
                    return subprocess.CompletedProcess(args, 0, output, "")
                host.run = command
                operation = {"intent": {"major": 16, "operation_id": self.operation, "mode": "rehearsal", "target_time": None},
                             "source": {"label": LABEL, "system_id": "123", "size": 1}, "cluster_token": "new-token"}
                cert, key = Path(temporary) / "cert", Path(temporary) / "key"
                cert.write_text("fixture cert")
                key.write_text("fixture key")
                stack.enter_context(patch.object(postgres, "prepare_tls", return_value={"cert": cert, "key": key}))
                stack.enter_context(patch.object(postgres, "_prepare_account"))
                stack.enter_context(patch.object(postgres, "_restored_limits", return_value=("max_connections = 100\n", "123")))
                for name in ("_verify_settings", "_publish_hba", "_rebind_recovered_database", "inspect"):
                    stack.enter_context(patch.object(postgres, name))
                stack.enter_context(patch("runtime_config.read_credentials", return_value={}))
                stack.enter_context(patch.object(backup, "_admin", return_value=subprocess.CompletedProcess([], 0, "t\n", "")))
                def restore(host, unit, path, options, **kwargs):
                    self.assertTrue(kwargs["retain"])
                    self.assertIn("--archive-mode=off", options)
                    data = Path(next(value.split("=", 1)[1] for value in options if value.startswith("--pg1-path=")))
                    data.mkdir()
                    (data / "PG_VERSION").write_text("16\n")
                    (data / "postgresql.auto.conf").write_text("restore_command = 'archive-get'\n")
                restored = stack.enter_context(patch.object(backup, "_restore_database", side_effect=restore))
                save = postgres._save_cluster
                interrupted = [False]
                def crash(host, journal):
                    save(host, journal)
                    if journal.get("recovery", {}).get("phase") == failed_phase and not interrupted[0]:
                        interrupted[0] = True
                        raise KeyboardInterrupt()
                stack.enter_context(patch.object(postgres, "_save_cluster", side_effect=crash))
                with self.assertRaises(KeyboardInterrupt):
                    postgres.recover_cluster(host, registered, entry, operation, self.repository, 55432)
                with patch.object(capacity, "memory_mib", side_effect=AssertionError("recovery resume recomputed RAM")):
                    postgres.recover_cluster(host, registered, entry, operation, self.repository, 55432)
                    postgres.recover_cluster(host, registered, entry, operation, self.repository, 55432)
                self.assertEqual(registered["shared"]["postgresql"]["capacity"], capacity.postgres(4096))
                self.assertEqual(restored.call_count, 1)
                self.assertFalse(any("pg_createcluster" in args or "initdb" in args for args in commands))
                self.assertFalse(any("archive-push" in " ".join(args) for args in commands))
                self.assertEqual(registered["shared"]["postgresql"]["archive_mode"], "off")

    def test_unknown_backup_is_not_retried_on_recovery_resume(self):
        self.entry["recovered_from"] = {"mode": "takeover"}
        self.host.write_registry(registry(self.entry))
        operation = str(uuid.uuid4())
        with patch.object(backup, "_info", return_value=[]), patch.object(backup, "_database_backup",
                side_effect=DeployError("command_failed", "lost acknowledgement")) as take:
            for _ in range(2):
                with self.assertRaises(DeployError):
                    backup.run_backup(self.host, self.entry, operation, "full")
        self.assertEqual(take.call_count, 1)

    def test_rehearsal_orchestration_never_checks_backs_up_uploads_or_enables_timers(self):
        plan = self.plan()
        entry = copy.deepcopy(self.entry)
        entry["profile"] = copy.deepcopy(self.profile)
        entry["host_id"] = self.profile["host"]["host_id"]
        entry["provisioning"]["state"] = "rehearsal"
        entry["recovered_from"] = {"mode": "rehearsal", "operation_id": self.operation}
        def resources(host, *args):
            host.write_registry(registry(entry))
        def preflight(host, journal, recovered):
            journal["source"] = {"label": LABEL}
        with ExitStack() as stack:
            stack.enter_context(patch.object(recover, "plan", return_value=plan))
            stack.enter_context(patch.object(recover, "_ready"))
            stack.enter_context(patch.object(recover, "_verify_repository", side_effect=preflight))
            stack.enter_context(patch.object(recover, "_resources", side_effect=resources))
            for owner, name in ((postgres, "recover_cluster"), (recover, "validate_runtime_environment"),
                                (recover, "read_credentials"), (recover.redis_acl, "prepare"), (recover.redis_acl, "provision")):
                stack.enter_context(patch.object(owner, name))
            units = stack.enter_context(patch.object(recover.provision, "_install_unit"))
            activate = stack.enter_context(patch.object(recover.release, "activate_recovered", return_value={"version": "restored"}))
            forbidden = [stack.enter_context(patch.object(owner, name, side_effect=AssertionError("rehearsal write")))
                         for owner, name in ((backup, "_backrest"), (backup, "run_backup"), (backup, "archive_keys"),
                                             (backup, "_install_timers"), (recover.provision, "_install_site"))]
            result = self.apply(plan)
        for command in forbidden:
            command.assert_not_called()
        self.assertTrue(units.call_args.kwargs["http_only"])
        self.assertTrue(activate.call_args.kwargs["http_only"])
        self.assertEqual(result["mode"], "rehearsal")
        self.assertEqual(self.host.read_registry()["instances"][self.profile["instance_id"]]["provisioning"]["state"], "rehearsal")

    def test_local_profile_changes_only_after_takeover_success_and_resume_reuses_identity(self):
        import contextlib
        import os
        from test_local import cli
        for mode in ("rehearsal", "takeover"):
            with self.subTest(mode=mode), tempfile.TemporaryDirectory() as temporary, ExitStack() as stack:
                directory = Path(temporary).resolve()
                folder = directory / "decrypted"
                for name, content in self.files.items():
                    path = folder / name
                    path.parent.mkdir(parents=True, exist_ok=True)
                    path.write_bytes(content)
                selected = directory / "release.tar.gz"
                selected.write_bytes(b"release fixture")
                original = copy.deepcopy(self.old_profile)
                profile = copy.deepcopy(original)
                requests = []
                class Remote:
                    failed = False
                    def close(self):
                        pass
                    @contextlib.contextmanager
                    def staged_tools(self):
                        yield "/tmp/foundry.fixture"
                    def upload(self, source, remote):
                        pass
                    def request(self, action, request, remote):
                        requests.append((action, copy.deepcopy(request)))
                        if action == "recover-plan":
                            return {"ok": True, "plan_token": "token"}
                        if not self.failed:
                            self.failed = True
                            raise cli.LocalError("connection lost; outcome unknown")
                        return {"ok": True, "mode": mode, "host_id": request["profile"]["host"]["host_id"],
                                "operation_id": request["operation_id"]}
                remote = Remote()
                stack.enter_context(patch.object(cli, "ROOT", directory))
                ssh = stack.enter_context(patch.object(cli, "SSH", return_value=remote))
                stack.enter_context(patch.object(cli, "load_topology", return_value=self.entry["topology"]))
                select = stack.enter_context(patch.object(cli, "select_archive", return_value=selected))
                save = stack.enter_context(patch.object(cli, "save_profile"))
                stack.enter_context(patch.object(cli, "display"))
                stack.enter_context(patch.object(cli, "say"))
                stack.enter_context(patch.dict(os.environ, {"RECOVER_HOST": "replacement.example.invalid", "RECOVER_MODE": mode,
                    "FOUNDRY_RECOVERY_DIR": str(folder), "CONFIRM": "yes", "CONFIRM_OLD_SERVER_GONE": "yes"}, clear=True))
                with self.assertRaises(cli.LocalError):
                    cli.recover(self.project, profile, {"hosts": {}})
                self.assertEqual(profile, original)
                save.assert_not_called()
                cli.recover(self.project, profile, {"hosts": {}})
                applies = [request for action, request in requests if action == "recover-apply"]
                self.assertEqual(applies[0]["operation_id"], applies[1]["operation_id"])
                self.assertEqual(applies[0]["profile"]["host"], applies[1]["profile"]["host"])
                self.assertTrue(ssh.call_args.kwargs["first_contact"])
                self.assertFalse((directory / ".deploy/recovery" / (profile["instance_id"] + ".json")).exists())
                select.assert_called_once()
                if mode == "takeover":
                    self.assertEqual(profile["host"]["address"], "replacement.example.invalid")
                    self.assertNotEqual(profile["host"]["host_id"], original["host"]["host_id"])
                    save.assert_called_once_with(profile)
                else:
                    self.assertEqual(profile, original)
                    save.assert_not_called()

    def test_recovery_folder_skips_finder_files_and_refuses_age_identities(self):
        folder = self.root / "decrypted"
        (folder / "recovery").mkdir(parents=True)
        (folder / "recovery" / "pgbackrest-repository.json").write_text("{}")
        (folder / ".DS_Store").write_bytes(b"finder")
        (folder / "recovery" / ".DS_Store").write_bytes(b"finder")
        destination = self.root / "recovery.tar"
        recover.folder_archive(folder, destination)
        self.assertEqual(set(recover.read_archive(destination)), {"recovery/pgbackrest-repository.json"})
        (folder / "identity.txt").write_text("AGE-SECRET-KEY-1FIXTURE")
        with self.assertRaisesRegex(DeployError, "age private identity"):
            recover.folder_archive(folder, destination)
