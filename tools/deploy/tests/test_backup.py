"""Backup failure boundaries use private fixture files and mocked host commands."""
import json
import time
from pathlib import Path
import re
import subprocess
import sys
import tarfile
import tempfile
import unittest
from unittest.mock import patch
import uuid

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "lib"))
import backup
from contracts import DeployError, now, read_json
from host import Host, Paths
from test_contracts import fixture, registry

LABEL = "20261101-020000F"
RECIPIENT = "age1" + "a" * 58


class BackupTests(unittest.TestCase):
    def test_rclone_config_works_with_object_scoped_credentials(self):
        common = dict(region="auto", access_key_id="id", secret_access_key="secret", bucket="backups")
        r2 = backup._rclone_config({**common, "endpoint": "https://account.r2.cloudflarestorage.com"})
        for line in ("provider = Cloudflare", "acl = private", "no_check_bucket = true"):
            self.assertIn(line + "\n", r2)
        other = backup._rclone_config({**common, "endpoint": "https://s3:9000"})
        for line in ("provider = Other", "no_check_bucket = true"):
            self.assertIn(line + "\n", other)
        self.assertNotIn("acl =", other)

    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.host = Host(Paths(Path(self.temporary.name)))
        _, _, self.entry = fixture()
        self.registry = registry(self.entry)
        self.host.write_registry(self.registry)
        owner = patch.object(backup.postgres, "cluster_owner", return_value="foundry-postgres", create=True)
        owner.start()
        self.addCleanup(owner.stop)
        self.commands = []
        self.host.run = self.run_command
        self.repository = {"endpoint": "https://r2.example.invalid", "region": "auto", "bucket": "private-backups",
                           "access_key_id": "private-access", "secret_access_key": "private-secret", "cipher_pass": "x" * 43}
        backup._write_private(self.host, self.host.paths.p(backup.REPOSITORY), self.repository)
        path = backup._instance_settings_path(self.host, self.entry)
        backup._write_private(self.host, path, {"instance_id": self.entry["profile"]["instance_id"],
                                               "age_recipients": [RECIPIENT]})
        self.host.owned_write(path.parent / "app.env", "PROJECT__ENV=production\n", mode=0o640)
        self.host.owned_write(path.parent / "secrets.json", '{"key":"private-key-canary"}', mode=0o600)

    def run_command(self, args, **kwargs):
        args = list(map(str, args))
        self.commands.append((args, kwargs))
        if args[0] == "age":
            Path(args[args.index("-o") + 1]).write_bytes(b"age-encryption.org/v1\nprivate-ciphertext")
        output = ""
        if "--property=ExecStart" in args:
            service = args[2].removeprefix("foundry-backup-").removesuffix(".service")
            task, identity = service.split("-", 1)
            output = f"/usr/bin/python3 -I {self.registry['tools']['active_path']}/lib/backup.py scheduled {task} {identity}"
        elif "--property=Unit" in args:
            output = args[2].removesuffix(".timer") + ".service"
        elif "--property=LoadState,ActiveState,MainPID,ControlGroup" in args:
            output = "LoadState=loaded\nActiveState=inactive\nMainPID=0\nControlGroup=\n"
        elif "SHOW archive_command" in kwargs.get("input", ""):
            output = backup._archive_command()
        return subprocess.CompletedProcess(args, 0, output, "")

    def test_archive_is_encrypted_before_copy_and_contains_referenced_values(self):
        directory = self.host.paths.instance_config(self.entry["name"])
        secret = directory / "keyring.json"
        self.host.owned_write(secret, '{"old":"retained","active":"current"}', mode=0o600)
        canonical = backup.instance_paths(self.entry["name"])["configuration"]
        self.host.owned_write(directory / "app.env", f"APP_KEYS_FILE={canonical}/keyring.json\n", mode=0o640)
        captured = {}
        def command(args, **kwargs):
            if args[0] == "age":
                with tarfile.open(args[-1]) as archive:
                    for member in archive.getmembers():
                        self.assertTrue(member.isfile())
                        captured[member.name] = archive.extractfile(member).read()
            return self.run_command(args, **kwargs)
        self.host.run = command
        result = backup.archive_keys(self.host, self.entry)
        self.assertEqual(captured["files/" + canonical.lstrip("/") + "/keyring.json"], secret.read_bytes())
        self.assertIn("private-key-canary", captured[f"instances/{self.entry['name']}/secrets.json"].decode())
        self.assertTrue(result["object_key"].startswith("keys/" + self.entry["profile"]["instance_id"] + "/"))
        commands = [args for args, _ in self.commands]
        self.assertEqual(commands[0][0], "age")
        self.assertIn("copyto", commands[1])
        self.assertIn("check", commands[2])
        self.assertIn("--download", commands[2])
        uploaded_name = commands[1][-2].rsplit("/", 1)[-1]
        self.assertIn("/" + uploaded_name, commands[2])
        self.assertNotIn("private-key-canary", json.dumps(commands))
        self.assertNotIn("private-secret", json.dumps(commands))
        self.assertEqual(self.host.load_state(self.entry)["backup"]["keys"], result)

    def test_failed_upload_or_readback_never_updates_freshness(self):
        for failure in ("copyto", "check"):
            with self.subTest(failure=failure):
                def command(args, **kwargs):
                    if failure in args:
                        raise DeployError("command_failed", "upload failed")
                    return self.run_command(args, **kwargs)
                self.host.run = command
                with self.assertRaises(DeployError):
                    backup.archive_keys(self.host, self.entry)
                self.assertNotIn("backup", self.host.load_state(self.entry))

    def test_bundle_completes_with_only_database_and_key_archive(self):
        operation = str(uuid.uuid4())
        inventory = [{"label": LABEL, "type": "full", "timestamp": {"stop": int(time.time())}}]
        with patch.object(backup, "_info", return_value=inventory), \
                patch.object(backup, "_database_backup", return_value=LABEL) as database, \
                patch.object(backup, "_backrest"):
            receipt = backup.run_backup(self.host, self.entry, operation)
            again = backup.run_backup(self.host, self.entry, operation)
        database.assert_called_once_with(self.host, "full")
        self.assertEqual(receipt, again)
        self.assertEqual(receipt["status"], "complete")
        self.assertEqual(receipt["configuration_revision"], self.entry["configuration_revision"])
        self.assertEqual(receipt["fresh_at"], receipt["database_point"]["completed_at"])
        journal = read_json(backup._operation_path(self.host, self.entry, operation))
        self.assertEqual(set(journal["components"]), {"database", "keys"})
        commands = [args for args, _ in self.commands]
        self.assertEqual([args[0] for args in commands], ["age", "rclone", "rclone"])
        self.assertIn("copyto", commands[1])
        self.assertIn(receipt["key_archive"]["object_key"], commands[1][-2])
        self.assertIn("check", commands[2])
        self.assertTrue(all("/keys/" in arg for args in commands for arg in args if arg.startswith("backup:")))
        self.assertFalse(any(arg.startswith("app:") or arg == "copy" for args in commands for arg in args))

    def test_recovery_point_backs_up_before_named_point_and_checks_wal(self):
        operation = str(uuid.uuid4())
        events = []
        def database(*args):
            events.append("backup")
            return LABEL
        def admin(*args, **kwargs):
            events.append("restore-point")
            self.assertIn("pg_create_restore_point", args[2])
            self.assertIn("pg_switch_wal", args[2])
            return subprocess.CompletedProcess([], 0, "0/123\n0/456\n", "")
        with patch.object(backup, "_cluster", return_value={}), patch.object(backup, "_database_backup", side_effect=database), \
                patch.object(backup, "_info", return_value=[{"label": LABEL, "type": "full", "timestamp": {"stop": int(time.time()) - 172800}}]), \
                patch.object(backup, "_admin", side_effect=admin), \
                patch.object(backup, "_backrest", side_effect=lambda h, args, **k: events.append(args[0])), \
                patch.object(backup, "archive_keys", return_value={"status": "complete", "completed_at": now(), "object_key": "keys/fixture"}):
            receipt = backup.recovery_point(self.host, self.entry, operation)
            again = backup.recovery_point(self.host, self.entry, operation)
        self.assertEqual(events, ["backup", "restore-point", "check", "expire"])
        self.assertEqual(receipt, again)
        self.assertEqual(receipt["backup_id"], operation)
        self.assertEqual(receipt["pgbackrest_label"], LABEL)
        self.assertEqual(receipt["restore_point"], "foundry_" + uuid.UUID(operation).hex)

    def test_backup_uncertainty_is_not_retried_or_counted_complete(self):
        operation = str(uuid.uuid4())
        with patch.object(backup, "_info", return_value=[]), \
                patch.object(backup, "_database_backup", side_effect=DeployError("command_failed", "timeout")) as database:
            with self.assertRaises(DeployError):
                backup.run_backup(self.host, self.entry, operation)
            with self.assertRaises(DeployError) as error:
                backup.run_backup(self.host, self.entry, operation)
        self.assertEqual(database.call_count, 1)
        self.assertEqual(error.exception.code, "backup_outcome_unknown")
        self.assertFalse(backup._receipt_path(self.host, self.entry, operation).exists())
        state = self.host.load_state(self.entry)["backup"]
        self.assertEqual(state["last_attempt_outcome"], "unknown")
        self.assertNotIn("database", state)

    def test_unrelated_or_unsafe_archive_inputs_are_refused(self):
        directory = self.host.paths.instance_config(self.entry["name"])
        for value in ("/etc/shadow", "/etc/foundry/instances/other/key", "relative/key"):
            with self.subTest(value=value):
                self.host.owned_write(directory / "app.env", f"APP_KEYS_FILE={value}\n", mode=0o640)
                with self.assertRaises(DeployError) as caught:
                    backup.archive_keys(self.host, self.entry)
                self.assertEqual("backup_archive_path", caught.exception.code)
        self.assertEqual(self.commands, [])

    def test_private_keys_and_unapproved_http_are_rejected(self):
        with self.assertRaises(DeployError):
            backup._recipients(["AGE-SECRET-KEY-private-canary"])
        with self.assertRaises(DeployError):
            backup._repository({**self.repository, "endpoint": "http://minio.invalid"})
        backup._repository({**self.repository, "endpoint": "http://minio.invalid:9000", "allow_http": True})
        self.assertEqual(self.commands, [])

    def test_restore_rejects_incomplete_foreign_and_invalid_targets_before_start(self):
        operation = str(uuid.uuid4())
        receipt = {"instance_id": self.entry["profile"]["instance_id"], "status": "complete", "pgbackrest_label": LABEL}
        path = backup._receipt_path(self.host, self.entry, operation)
        for change in ({"status": "running"}, {"instance_id": str(uuid.uuid4())}, {"pgbackrest_label": "../../live"}):
            with self.subTest(change=change):
                backup._write_private(self.host, path, {**receipt, **change})
                with self.assertRaises(DeployError):
                    backup.restore_drill(self.host, self.entry, operation)
        backup._write_private(self.host, path, receipt)
        with self.assertRaises(DeployError):
            backup.restore_drill(self.host, self.entry, operation, target_time="now; rm")
        self.assertEqual(self.commands, [])

    def cluster(self):
        cluster = {"owner": "foundry", "cluster": "foundry", "state": "ready", "major": 16, "port": 55432,
                   "unit": "postgresql@16-foundry.service", "ownership_token": "owned-cluster",
                   "data_path": "/var/lib/foundry/postgresql/16/foundry", "socket_directory": "/run/foundry/postgresql"}
        self.registry["shared"]["postgresql"] = cluster
        self.host.write_registry(self.registry)
        self.host.paths.p(backup.postgres.CLUSTER_HOME).mkdir(parents=True, exist_ok=True)
        return cluster

    def test_configure_uses_guarded_packages_f2_plan_and_owned_timers(self):
        cluster = self.cluster()
        backup._instance_settings_path(self.host, self.entry).unlink()
        backup._write_private(self.host, self.host.paths.p(backup.REPOSITORY),
                              {**self.repository, "host_id": self.registry["host_id"], "cluster_token": cluster["ownership_token"]})
        intake = {"instance_id": self.entry["profile"]["instance_id"],
                  "backup_s3": {key: self.repository[key] for key in ("endpoint", "region", "bucket", "access_key_id", "secret_access_key")},
                  "age_recipients": [RECIPIENT]}
        backup._write_private(self.host, self.host.paths.instance_config(self.entry["name"]) / "bootstrap-secrets.json", intake)
        with patch.object(backup, "_cluster", return_value=cluster), \
                patch.object(backup.packages, "install_missing") as install, \
                patch.object(backup.postgres, "cluster_settings", return_value=dict(cluster)) as settings, \
                patch.object(backup.postgres, "prepare") as prepare, \
                patch.object(backup, "_backrest"):
            backup.configure(self.host, self.registry, self.entry)
        install.assert_called_once_with(self.host, ["pgbackrest", "age", "rclone"])
        settings.assert_called_once_with(self.host, self.registry)
        plan = prepare.call_args.kwargs["shared_service_plan"]
        self.assertIn("archive-push %p", plan["archive_command"])
        self.assertEqual(plan["ownership_token"], cluster["ownership_token"])
        self.assertFalse(self.host.paths.p("/etc/sudoers.d/foundry-backup-archive").exists())
        self.assertNotIn("sudo", plan["archive_command"])
        self.assertEqual(self.host.paths.p(backup.CONFIG).stat().st_mode & 0o777, 0o600)
        for path in (backup.CONFIG_DIR, backup.WORK):
            self.assertEqual(self.host.paths.p(path).stat().st_mode & 0o777, 0o700)
        for path in ("/etc/pgbackrest", "/var/lib/pgbackrest"):
            self.assertFalse(self.host.paths.p(path).exists())
        config = self.host.paths.p(backup.CONFIG).read_text()
        for value in ("repo1-type=s3", "repo1-s3-uri-style=path", "repo1-cipher-type=aes-256-cbc",
                      "repo1-retention-full=35", "repo1-retention-full-type=time", "pg1-port=55432"):
            self.assertIn(value, config)
        for value in ("private-access", "private-secret", "x" * 43):
            self.assertIn(value, config)
            self.assertNotIn(value, json.dumps([kwargs.get("env", {}) for _, kwargs in self.commands]))
            self.assertNotIn(value, json.dumps([args for args, _ in self.commands]))
            self.assertNotIn(value, json.dumps(self.registry))
        self.assertEqual(self.host.paths.p(backup.REPOSITORY).stat().st_mode & 0o777, 0o600)
        units = self.host.paths.p("/etc/systemd/system")
        timers = list(units.glob("foundry-backup-*.timer"))
        self.assertEqual({timer.name for timer in timers},
                         {"foundry-backup-full-cluster.timer", "foundry-backup-diff-cluster.timer"})
        self.assertTrue(all(f"RandomizedDelaySec={backup.BACKUP_JITTER_SECONDS}" in timer.read_text() for timer in timers))
        self.assertEqual(read_json(backup._instance_settings_path(self.host, self.entry)),
                         {"instance_id": self.entry["profile"]["instance_id"], "age_recipients": [RECIPIENT]})
        self.assertTrue(all("Persistent=true" in timer.read_text() for timer in timers))
        self.assertFalse(self.host.paths.p("/etc/postgresql/16/foundry/postgresql.conf").exists())

    def test_wal_command_calls_pgbackrest_directly_with_foundry_config(self):
        command = backup._archive_command()
        self.assertTrue(command.startswith("/usr/bin/pgbackrest "))
        self.assertIn("--config=" + backup.CONFIG, command)
        self.assertIn("--stanza=" + backup.STANZA, command)
        self.assertTrue(command.endswith(" archive-push %p"))
        self.assertNotIn("sudo", command)
        self.assertNotIn("python", command)

    def test_restore_is_new_private_cluster_and_records_timing(self):
        cluster = self.cluster()
        operation = str(uuid.uuid4())
        backup._write_private(self.host, backup._receipt_path(self.host, self.entry, operation),
                              {"instance_id": self.entry["profile"]["instance_id"], "status": "complete", "pgbackrest_label": LABEL})
        restored = []
        def restore(host, unit, path, args):
            restored.extend(args)
            directory = Path(next(arg.split("=", 1)[1] for arg in args if arg.startswith("--pg1-path=")))
            directory.mkdir()
            (directory / "postgresql.auto.conf").write_text("restore_command = 'pgbackrest archive-get %f %p'\n"
                                                           "recovery_target_action = 'pause'\n"
                                                           "primary_conninfo = 'foreign'\nshared_preload_libraries = 'foreign'\n")
        def admin(host, allocation, sql, **kwargs):
            self.assertNotEqual(kwargs["socket"], cluster["socket_directory"])
            self.assertEqual(kwargs["port"], 55433)
            output = self.entry["allocations"]["database"] + "\n100\n" if "pg_catalog" in sql else "t\n"
            return subprocess.CompletedProcess([], 0, output, "")
        with patch.object(backup, "_cluster", return_value=cluster), patch.object(backup, "_info", return_value=[{"label": LABEL, "info": {"size": 1}}]), \
                patch.object(backup, "_recovery_limits", return_value="max_connections = 100\n"), \
                patch.object(backup, "_restore_database", side_effect=restore), patch.object(backup, "_admin", side_effect=admin):
            result = backup.restore_drill(self.host, self.entry, operation, retain=True, target_time="2026-11-01 03:00:00+00")
        self.assertIn("--type=time", restored)
        self.assertIn("--target=2026-11-01 03:00:00+00", restored)
        self.assertNotEqual(result["data_path"], cluster["data_path"])
        self.assertNotEqual(result["unit"], cluster["unit"])
        self.assertEqual(result["status"], "complete")
        self.assertGreaterEqual(result["elapsed_seconds"], 0)
        configuration = (Path(result["data_path"]).parent / "postgresql.conf").read_text()
        self.assertIn("listen_addresses = ''", configuration)
        automatic = (Path(result["data_path"]) / "postgresql.auto.conf").read_text()
        self.assertNotIn("foreign", automatic)
        self.assertNotIn("private-secret", json.dumps(result))
        self.assertEqual(self.host.load_state(self.entry)["backup"]["restore_drill"]["status"], "complete")

    def test_failed_restore_retains_owned_data_without_claiming_verification(self):
        cluster = self.cluster()
        operation = str(uuid.uuid4())
        backup._write_private(self.host, backup._receipt_path(self.host, self.entry, operation),
                              {"instance_id": self.entry["profile"]["instance_id"], "status": "complete", "pgbackrest_label": LABEL})
        with patch.object(backup, "_cluster", return_value=cluster), patch.object(backup, "_info", return_value=[{"label": LABEL, "info": {"size": 1}}]), \
                patch.object(backup, "_recovery_limits", return_value="max_connections = 100\n"), \
                patch.object(backup, "_restore_database", side_effect=DeployError("command_unavailable", "interrupted")):
            with self.assertRaises(DeployError):
                backup.restore_drill(self.host, self.entry, operation)
        result = self.host.load_state(self.entry)["backup"]["restore_drill"]
        self.assertEqual(result["status"], "failed")
        self.assertTrue(result["retained"])
        self.assertTrue(Path(result["data_path"]).parent.is_dir())
        drills = self.host.paths.p(backup.postgres.CLUSTER_HOME + "/drills")
        self.assertEqual(0o700, drills.stat().st_mode & 0o777)
        self.assertNotIn("verification", result)

    def test_cluster_and_backup_config_use_allocated_nondefault_postgres_port(self):
        cluster = self.cluster()
        cluster["port"] = backup.POSTGRES_PORT + 7
        self.host.write_registry(self.registry)
        with patch.object(backup.postgres, "inspect", return_value=dict(cluster)):
            self.assertEqual(backup._cluster(self.host, self.registry)["port"], cluster["port"])
        backup._write_private(self.host, self.host.paths.p(backup.REPOSITORY),
                              {**self.repository, "host_id": self.registry["host_id"], "cluster_token": cluster["ownership_token"]})
        backup._install_configuration(self.host, cluster)
        self.assertIn("pg1-port=" + str(cluster["port"]), backup._configuration(self.host, cluster, self.repository))

    def test_repository_credentials_use_private_files_not_process_metadata(self):
        self.cluster()
        captured = []
        def command(args, **kwargs):
            if args[0] == "rclone":
                path = Path(next(arg.split("=", 1)[1] for arg in args if arg.startswith("--config=")))
                self.assertEqual(path.stat().st_mode & 0o777, 0o600)
                self.assertIn("private-secret", path.read_text())
                self.assertNotIn("[app]", path.read_text())
                captured.append(path)
            return self.run_command(args, **kwargs)
        self.host.run = command
        backup._backrest(self.host, ["check"])
        backup._rclone(self.host, ["copyto", "/private/key.tar.age", "backup:bucket/keys/key.tar.age"])
        self.assertEqual(self.commands[0][0][2], "foundry-postgres")
        self.assertTrue(all(not path.exists() for path in captured))
        self.assertNotIn("private-secret", json.dumps(self.commands))
        self.assertNotIn("PGBACKREST_REPO", json.dumps(self.commands))

    def test_component_resume_requires_reconciliation_and_reuses_database(self):
        operation = str(uuid.uuid4())
        inventory = [{"label": LABEL, "type": "full", "timestamp": {"stop": int(time.time())}}]
        keys = {"status": "complete", "completed_at": now(), "object_key": "keys/one"}
        with patch.object(backup, "_info", return_value=inventory), \
                patch.object(backup, "_database_backup", return_value=LABEL) as database, \
                patch.object(backup, "_backrest"), \
                patch.object(backup, "archive_keys", side_effect=[DeployError("upload_failed", "unknown"), keys]) as archive:
            with self.assertRaises(DeployError):
                backup.run_backup(self.host, self.entry, operation)
            with self.assertRaises(DeployError) as caught:
                backup.run_backup(self.host, self.entry, operation)
            self.assertEqual(caught.exception.code, "backup_outcome_unknown")
            result = backup.reconcile_backup(self.host, self.entry, operation)
            again = backup.run_backup(self.host, self.entry, operation)
        database.assert_called_once_with(self.host, "full")
        self.assertEqual(archive.call_count, 2)
        self.assertEqual(result, again)
        self.assertEqual(result["status"], "complete")
        journal = read_json(backup._operation_path(self.host, self.entry, operation))
        self.assertTrue(all(item["status"] == "complete" for item in journal["components"].values()))

    def test_completed_keys_survive_interrupted_receipt_publication(self):
        operation = str(uuid.uuid4())
        database = {"status": "complete", "completed_at": now(), "pgbackrest_label": LABEL}
        keys = {"status": "complete", "completed_at": now(), "object_key": "keys/one"}
        write = backup._write_private
        def interrupt(host, path, value):
            if path == backup._receipt_path(host, self.entry, operation):
                raise KeyboardInterrupt()
            return write(host, path, value)
        with patch.object(backup, "archive_keys", return_value=keys) as archive:
            with patch.object(backup, "_write_private", side_effect=interrupt):
                with self.assertRaises(KeyboardInterrupt):
                    backup._complete(self.host, self.entry, operation, database)
            result = backup._complete(self.host, self.entry, operation, database)
        archive.assert_called_once_with(self.host, self.entry)
        self.assertEqual(result["key_archive"], keys)

    def test_schedule_accepts_only_cluster_database_tasks(self):
        for task, identity in (("objects", self.entry["profile"]["instance_id"]), ("keys", "cluster"),
                               ("full", self.entry["profile"]["instance_id"]), ("diff", "unknown")):
            with self.subTest(task=task, identity=identity), self.assertRaises(DeployError) as caught:
                backup._scheduled(self.host, task, identity)
            self.assertEqual(caught.exception.code, "backup_schedule_invalid")
        self.assertEqual(self.commands, [])

    def test_unknown_database_requires_operator_label_and_repository_evidence(self):
        operation = str(uuid.uuid4())
        path = backup._operation_path(self.host, self.entry, operation)
        with patch.object(backup, "_info", return_value=[]), \
                patch.object(backup, "_database_backup", side_effect=DeployError("timeout", "unknown")):
            with self.assertRaises(DeployError):
                backup._database_phase(self.host, path, operation, "full")
        timestamp = int(time.time())
        inventory = [{"label": LABEL, "type": "full", "timestamp": {"start": timestamp, "stop": timestamp}}]
        with patch.object(backup, "_info", return_value=inventory), patch.object(backup, "_backrest"):
            with self.assertRaises(DeployError):
                backup._database_phase(self.host, path, operation, "full", reconcile=True)
            result = backup._database_phase(self.host, path, operation, "full", reconcile=True, database_label=LABEL)
        self.assertEqual(result["components"]["database"]["pgbackrest_label"], LABEL)

    def test_later_schedule_runs_after_an_incomplete_bundle(self):
        database = {"status": "complete", "completed_at": now(), "pgbackrest_label": LABEL}
        operations = []
        def phase(host, path, operation, kind, **kwargs):
            operations.append(operation)
            return {"operation_id": operation, "kind": kind, "components": {"database": database}}
        with patch.object(backup, "_cluster"), patch.object(backup, "_database_phase", side_effect=phase), \
                patch.object(backup, "_complete", side_effect=[DeployError("upload", "failed"), {}]) as complete, \
                patch.object(backup, "_backrest"):
            with self.assertRaises(DeployError):
                backup._scheduled(self.host, "full", "cluster")
            backup._scheduled(self.host, "diff", "cluster")
        self.assertEqual(complete.call_count, 2)
        self.assertEqual(len(set(operations)), 2)
        self.assertEqual(len(list(self.host.paths.p("/etc/foundry/backup/operations").glob("*.json"))), 2)

    def test_recovery_uses_recent_base_and_takes_diff_only_when_stale(self):
        for hours, expected in ((2, None), (25, "diff")):
            with self.subTest(hours=hours):
                operation = str(uuid.uuid4())
                inventory = [{"label": LABEL, "type": "full", "timestamp": {"stop": int(time.time()) - hours * 3600}}]
                with patch.object(backup, "_info", return_value=inventory), \
                        patch.object(backup, "_database_backup", return_value=LABEL) as take:
                    backup._database_phase(self.host, backup._operation_path(self.host, self.entry, operation), operation, "recovery")
                if expected:
                    take.assert_called_once_with(self.host, expected)
                else:
                    take.assert_not_called()

    def test_drill_cleanup_refuses_live_descendants(self):
        unit = "foundry-restore-fixture.service"
        group = "/system.slice/" + unit
        path = self.host.paths.p("/sys/fs/cgroup" + group + "/cgroup.procs")
        path.parent.mkdir(parents=True)
        path.write_text("123\n")
        def command(args, **kwargs):
            return subprocess.CompletedProcess(args, 0, f"MainPID=0\nActiveState=inactive\nControlGroup={group}\n", "")
        self.host.run = command
        with self.assertRaises(DeployError) as caught:
            backup._stop_drill(self.host, unit)
        self.assertEqual(caught.exception.code, "backup_drill_cleanup_pending")

    def test_interrupted_drill_reattaches_before_starting_another_restore(self):
        cluster = self.cluster()
        operation = str(uuid.uuid4())
        backup._write_private(self.host, backup._receipt_path(self.host, self.entry, operation),
            {"instance_id": self.entry["profile"]["instance_id"], "status": "complete", "pgbackrest_label": LABEL})
        starts = []
        def restore(host, unit, path, options):
            data = Path(next(arg.split("=", 1)[1] for arg in options if arg.startswith("--pg1-path=")))
            data.mkdir()
            (data / "postgresql.auto.conf").write_text("restore_command = 'pgbackrest archive-get %f %p'\n")
        def command(args, **kwargs):
            if args[:2] == ["systemctl", "start"]:
                starts.append(args[-1])
                if len(starts) == 1:
                    raise KeyboardInterrupt()
            return self.run_command(args, **kwargs)
        def admin(host, allocation, sql, **kwargs):
            text = self.entry["allocations"]["database"] + "\n100\n" if "pg_catalog" in sql else "t\n"
            return subprocess.CompletedProcess([], 0, text, "")
        self.host.run = command
        with patch.object(backup, "_cluster", return_value=cluster), \
                patch.object(backup, "_info", return_value=[{"label": LABEL, "info": {"size": 1}}]), \
                patch.object(backup, "_recovery_limits", return_value=""), patch.object(backup, "_admin", side_effect=admin), \
                patch.object(backup, "_restore_database", side_effect=restore) as recover:
            with self.assertRaises(KeyboardInterrupt):
                backup.restore_drill(self.host, self.entry, operation)
            journal = list((backup._private_root(self.host, self.entry) / "drills").glob("*.json"))
            self.assertEqual(len(journal), 1)
            before = read_json(journal[0])
            self.assertEqual(before["phase"], "start_pending")
            result = backup.restore_drill(self.host, self.entry, operation)
            replay = backup.restore_drill(self.host, self.entry, operation)
        self.assertEqual(recover.call_count, 1)
        self.assertEqual(starts[0], starts[1])
        self.assertEqual(result["drill_id"], before["drill_id"])
        self.assertEqual(replay, result)
        self.assertFalse(Path(result["data_path"]).parent.exists())

    def test_key_archival_does_not_refresh_an_old_database_point(self):
        operation = str(uuid.uuid4())
        old = backup.datetime.fromtimestamp(time.time() - 4 * 86400, backup.timezone.utc).isoformat()
        point = {"status": "complete", "pgbackrest_label": LABEL, "completed_at": old}
        receipt = backup._complete(self.host, self.entry, operation, point)
        self.assertEqual(receipt["fresh_at"], old)
        self.assertEqual(self.host.load_state(self.entry)["backup"]["last_complete_at"], old)

    def test_timer_rebinding_checks_loaded_commands_even_when_files_match(self):
        backup._install_timers(self.host, self.registry)
        def command(args, **kwargs):
            if "--property=ExecStart" in args:
                return subprocess.CompletedProcess(args, 0, "/usr/bin/python3 -I /old/lib/backup.py scheduled full cluster", "")
            return self.run_command(args, **kwargs)
        self.host.run = command
        with self.assertRaises(DeployError) as caught:
            backup._install_timers(self.host, self.registry)
        self.assertEqual(caught.exception.code, "backup_rebind_failed")

    def test_rebinding_removes_bridge_and_requires_live_archive_command(self):
        cluster = self.cluster()
        self.registry["shared"]["backup"] = {"owner": "foundry"}
        bridge = self.host.paths.p("/etc/sudoers.d/foundry-backup-archive")
        self.host.owned_write(bridge, "# Foundry owned WAL archive bridge\nold rule\n", mode=0o440)
        with patch.object(backup, "_cluster", return_value=cluster), \
                patch.object(backup.postgres, "cluster_settings", return_value=dict(cluster)), \
                patch.object(backup.postgres, "prepare"), \
                patch.object(backup, "_admin", return_value=subprocess.CompletedProcess([], 0, "old-command", "")), \
                patch.object(backup, "_backrest") as backrest:
            with self.assertRaises(DeployError) as caught:
                backup.rebind(self.host, self.registry)
        self.assertFalse(bridge.exists())
        self.assertEqual(caught.exception.code, "backup_rebind_failed")
        backrest.assert_not_called()

    def test_repository_config_is_written_only_for_dedicated_owner(self):
        cluster = self.cluster()
        with patch.object(self.host, "owned_write", wraps=self.host.owned_write) as write:
            backup._install_configuration(self.host, cluster)
        self.assertEqual(write.call_args.kwargs, {"mode": 0o600, "owner": "foundry-postgres", "group": "foundry-postgres"})
        configuration = self.host.paths.p(backup.CONFIG).read_text()
        self.assertIn("repo1-retention-full-type=time", configuration)
        self.assertIn("repo1-retention-full=35", configuration)
        self.assertNotIn("repo1-retention-diff=", configuration)
        self.assertNotIn("repo1-retention-archive=", configuration)

    def test_explicit_abandonment_preserves_unknown_outcome_without_retry(self):
        operation = str(uuid.uuid4())
        path = backup._operation_path(self.host, self.entry, operation)
        journal = {"operation_id": operation, "kind": "recovery", "configuration_revision": self.entry["configuration_revision"],
                   "components": {"database": {"status": "complete", "pgbackrest_label": LABEL}, "restore_point": {"status": "running"}}}
        backup._write_private(self.host, path, journal)
        with patch.object(backup, "_backrest") as command:
            result = backup.reconcile_backup(self.host, self.entry, operation, "--abandon")
            with self.assertRaises(DeployError) as caught:
                backup.recovery_point(self.host, self.entry, operation)
        self.assertEqual(result["outcome"], "unknown")
        self.assertEqual(caught.exception.code, "backup_abandoned")
        self.assertEqual(read_json(path)["components"], journal["components"])
        command.assert_not_called()
