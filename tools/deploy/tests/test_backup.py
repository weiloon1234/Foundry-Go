"""Backup failure boundaries use private fixture files and mocked host commands."""
import json
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
from contracts import DeployError
from host import Host, Paths
from test_contracts import fixture, registry

LABEL = "20261101-020000F"
RECIPIENT = "age1" + "a" * 58


class BackupTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.host = Host(Paths(Path(self.temporary.name)))
        _, _, self.entry = fixture()
        self.registry = registry(self.entry)
        self.host.write_registry(self.registry)
        self.commands = []
        self.host.run = self.run_command
        self.repository = {"endpoint": "https://r2.example.invalid", "region": "auto", "bucket": "private-backups",
                           "access_key_id": "private-access", "secret_access_key": "private-secret", "cipher_pass": "x" * 43}
        self.source = {**self.repository, "bucket": "private-attachments"}
        backup._write_private(self.host, self.host.paths.p(backup.REPOSITORY), self.repository)
        path = backup._instance_settings_path(self.host, self.entry)
        backup._write_private(self.host, path, {"instance_id": self.entry["profile"]["instance_id"],
                                               "source": self.source, "age_recipients": [RECIPIENT]})
        self.host.owned_write(path.parent / "app.env", "PROJECT__ENV=production\n", mode=0o640)
        self.host.owned_write(path.parent / "secrets.json", '{"key":"private-key-canary"}', mode=0o600)

    def run_command(self, args, **kwargs):
        args = list(map(str, args))
        self.commands.append((args, kwargs))
        if args[0] == "age":
            Path(args[args.index("-o") + 1]).write_bytes(b"age-encryption.org/v1\nprivate-ciphertext")
        return subprocess.CompletedProcess(args, 0, "", "")

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

    def test_object_copy_keeps_source_and_overwrite_history(self):
        receipt = backup.copy_objects(self.host, self.entry)
        command = self.commands[0][0]
        self.assertIn("copy", command)
        self.assertIn("--backup-dir", command)
        self.assertFalse(any(word in command for word in ("sync", "delete", "purge", "move")))
        self.assertIn("check", self.commands[1][0])
        self.assertIn("--one-way", self.commands[1][0])
        self.assertNotIn("private-secret", json.dumps(receipt))
        self.assertEqual(receipt["objects_prefix"], "objects/" + self.entry["profile"]["instance_id"])

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
                patch.object(backup, "_admin", side_effect=admin), \
                patch.object(backup, "_backrest", side_effect=lambda h, args, **k: events.append(args[0])), \
                patch.object(backup, "archive_keys", return_value={"object_key": "keys/fixture"}), \
                patch.object(backup, "copy_objects", return_value={"objects_prefix": "objects/fixture"}):
            receipt = backup.recovery_point(self.host, self.entry, operation)
            again = backup.recovery_point(self.host, self.entry, operation)
        self.assertEqual(events, ["backup", "restore-point", "check", "expire"])
        self.assertEqual(receipt, again)
        self.assertEqual(receipt["backup_id"], operation)
        self.assertEqual(receipt["pgbackrest_label"], LABEL)
        self.assertEqual(receipt["restore_point"], "foundry_" + uuid.UUID(operation).hex)

    def test_backup_uncertainty_is_not_retried_or_counted_complete(self):
        operation = str(uuid.uuid4())
        with patch.object(backup, "_database_backup", side_effect=DeployError("command_failed", "timeout")) as database:
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
                with self.assertRaises(DeployError):
                    backup.archive_keys(self.host, self.entry)
        self.assertEqual(self.commands, [])

    def test_private_keys_http_and_same_bucket_are_rejected(self):
        with self.assertRaises(DeployError):
            backup._recipients(["AGE-SECRET-KEY-private-canary"])
        with self.assertRaises(DeployError):
            backup._repository({**self.repository, "endpoint": "http://minio.invalid"})
        backup._repository({**self.repository, "endpoint": "http://minio.invalid:9000", "allow_http": True})
        backup._write_private(self.host, backup._instance_settings_path(self.host, self.entry),
                              {"instance_id": self.entry["profile"]["instance_id"], "source": self.repository, "age_recipients": [RECIPIENT]})
        with self.assertRaises(DeployError):
            backup.copy_objects(self.host, self.entry)
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
                   "data_path": "/var/lib/postgresql/16/foundry", "socket_directory": "/run/postgresql-foundry"}
        self.registry["shared"]["postgresql"] = cluster
        self.host.write_registry(self.registry)
        return cluster

    def test_configure_uses_guarded_packages_f2_plan_and_owned_timers(self):
        cluster = self.cluster()
        backup._instance_settings_path(self.host, self.entry).unlink()
        backup._write_private(self.host, self.host.paths.p(backup.REPOSITORY),
                              {**self.repository, "host_id": self.registry["host_id"], "cluster_token": cluster["ownership_token"]})
        intake = {"instance_id": self.entry["profile"]["instance_id"],
                  "backup_s3": {key: self.repository[key] for key in ("endpoint", "region", "bucket", "access_key_id", "secret_access_key")},
                  "age_recipients": [RECIPIENT], "app_storage": {
                      "credentials": {"uploads": {"mode": "static", "access_key": "private-access", "secret_key": "private-secret"}},
                      "disks": {"uploads": {"driver": "r2", "config": {"visibility": 0}, "cloud": {
                          "endpoint": "https://r2.example.invalid", "region": "auto", "bucket": "private-attachments", "credentials": "uploads"}}}}}
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
        sudoers = self.host.paths.p("/etc/sudoers.d/foundry-backup-archive").read_text()
        rule = sudoers.split("/usr/bin/python3 ", 1)[1].strip()
        self.assertTrue(rule.startswith("^") and rule.endswith("$"))
        script = self.registry["tools"]["active_path"] + "/lib/backup.py"
        self.assertRegex("-I " + script + " archive-push pg_wal/" + "A" * 24, rule)
        for arguments in ("-I " + script + " scheduled full cluster",
                          "-I " + script + " archive-push pg_wal/" + "A" * 24 + " extra",
                          "-I " + script + " archive-push /etc/shadow",
                          "-c print(1)"):
            self.assertIsNone(re.fullmatch(rule, arguments))
        config = self.host.paths.p(backup.CONFIG).read_text()
        for value in ("repo1-type=s3", "repo1-s3-uri-style=path", "repo1-cipher-type=aes-256-cbc",
                      "repo1-retention-full=4", "repo1-retention-diff=7", "pg1-port=55432"):
            self.assertIn(value, config)
        for value in ("private-access", "private-secret", "x" * 43):
            self.assertNotIn(value, config)
            self.assertNotIn(value, json.dumps([args for args, _ in self.commands]))
            self.assertNotIn(value, json.dumps(self.registry))
        self.assertEqual(self.host.paths.p(backup.REPOSITORY).stat().st_mode & 0o777, 0o600)
        units = self.host.paths.p("/etc/systemd/system")
        timers = list(units.glob("foundry-backup-*.timer"))
        self.assertEqual(len(timers), 3)
        self.assertTrue(all("Persistent=true" in timer.read_text() for timer in timers))
        self.assertFalse(self.host.paths.p("/etc/postgresql/16/foundry/postgresql.conf").exists())

    def test_wal_bridge_refuses_arbitrary_paths_options_and_symlinks(self):
        cluster = self.cluster()
        backup._write_private(self.host, self.host.paths.p(backup.REPOSITORY),
                              {**self.repository, "host_id": self.registry["host_id"], "cluster_token": cluster["ownership_token"]})
        for value in ("/etc/shadow", "pg_wal/../../etc/shadow", "--config=/etc/foreign", "pg_wal/" + "A" * 24 + " more"):
            with self.subTest(value=value), self.assertRaises(DeployError):
                backup.archive_push(self.host, value)
        path = self.host.paths.p(cluster["data_path"] + "/pg_wal/" + "A" * 24)
        path.parent.mkdir(parents=True)
        path.symlink_to(self.host.paths.p(backup.REPOSITORY))
        with self.assertRaises(DeployError):
            backup.archive_push(self.host, "pg_wal/" + path.name)
        self.assertEqual(self.commands, [])
        path.unlink()
        path.write_bytes(b"wal")
        with patch.object(backup, "_backrest") as archive:
            backup.archive_push(self.host, "pg_wal/" + path.name)
        self.assertEqual(archive.call_args.args[1], ["archive-push", str(path)])

    def test_restore_is_new_private_cluster_and_records_timing(self):
        cluster = self.cluster()
        operation = str(uuid.uuid4())
        backup._write_private(self.host, backup._receipt_path(self.host, self.entry, operation),
                              {"instance_id": self.entry["profile"]["instance_id"], "status": "complete", "pgbackrest_label": LABEL})
        restored = []
        def restore(host, args, **kwargs):
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
                patch.object(backup, "_backrest", side_effect=restore), patch.object(backup, "_admin", side_effect=admin):
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
                patch.object(backup, "_backrest", side_effect=DeployError("command_unavailable", "interrupted")):
            with self.assertRaises(DeployError):
                backup.restore_drill(self.host, self.entry, operation)
        result = self.host.load_state(self.entry)["backup"]["restore_drill"]
        self.assertEqual(result["status"], "failed")
        self.assertTrue(result["retained"])
        self.assertTrue(Path(result["data_path"]).parent.is_dir())
        self.assertNotIn("verification", result)

    def test_cluster_and_archive_use_allocated_nondefault_postgres_port(self):
        cluster = self.cluster()
        cluster["port"] = backup.POSTGRES_PORT + 7
        self.host.write_registry(self.registry)
        with patch.object(backup.postgres, "inspect", return_value=dict(cluster)):
            self.assertEqual(backup._cluster(self.host, self.registry)["port"], cluster["port"])
        backup._write_private(self.host, self.host.paths.p(backup.REPOSITORY),
                              {**self.repository, "host_id": self.registry["host_id"], "cluster_token": cluster["ownership_token"]})
        path = self.host.paths.p(cluster["data_path"] + "/pg_wal/" + "B" * 24)
        path.parent.mkdir(parents=True)
        path.write_bytes(b"wal")
        with patch.object(backup, "_backrest") as archive:
            backup.archive_push(self.host, "pg_wal/" + path.name)
        archive.assert_called_once_with(self.host, ["archive-push", str(path)], timeout=300)
        self.assertIn("pg1-port=" + str(cluster["port"]), backup._configuration(self.host, cluster, self.repository))
