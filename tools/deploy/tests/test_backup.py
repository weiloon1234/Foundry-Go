"""Backup failure boundaries use private fixture files and mocked host commands."""
import json
import copy
from dataclasses import asdict, replace
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


def feature_fixture(*, state="disposable", secret=False, http_dependency=False, public=False,
                    recovery_owner=None, rehearsal_safe=None, background_dependency=False, http_port=None):
    import contracts as c
    import services
    import topology as t
    from test_services import inventory_fixture
    from test_host_capabilities import declaration, profile, selection
    inventory, entry, members = inventory_fixture()
    raw = inventory.topology.projection()
    raw.pop("sha256", None)
    relay = raw["roles"][1]
    relay["state"] = {"kind": state, **({"acknowledged": True} if state == "local-unprotected" else {})}
    if recovery_owner is not None:
        relay["state"]["recovery_owner"] = recovery_owner
    if rehearsal_safe is not None:
        relay["recovery_rehearsal_safe"] = rehearsal_safe
    if secret:
        raw["secrets"] = {"relay-token": {"prompt": "Relay token"}}
        relay["secret_files"] = {"TOKEN_FILE": "relay-token"}
    if http_dependency:
        raw["roles"][0]["requires"] = ["relay"]
        relay["requires"] = []
    if background_dependency:
        relay["requires"] = ["worker"]
        raw["roles"][2]["requires"] = []
    placed = inventory.profile.projection()
    if public:
        relay["listeners"]["ingress"] = declaration()["roles"][1]["listeners"]["ingress"]
        placed["listeners"] = profile()["listeners"]
    topology = t.parse_topology(raw)
    deployment = replace(inventory.deployment, topology_sha256=topology.sha256,
                         profile_sha256=c.canonical_digest(placed),
                         units=tuple(replace(unit, declaration_sha256=c.canonical_digest(role))
                                     for unit, role in zip(inventory.deployment.units, topology.projection()["roles"])))
    resolution = replace(inventory.resolution, deployment_sha256=c.canonical_digest(c.record_document(deployment)))
    if http_port is not None:
        resolution = replace(resolution, endpoints=tuple(
            replace(e, port=http_port) if e.role == "http" and e.listener == "http" else e for e in resolution.endpoints))
    inventory = services.UnitInventory(entry["name"], topology, t.parse_profile(placed, topology), deployment, resolution)
    selected = selection()
    selected["enabled_roles"] = list(inventory.roles)
    selected["admission"].update(deployment_sha256=resolution.deployment_sha256, disposition="committed")
    entry.update(profile=placed, topology=t.validate_topology(topology.projection()), configuration_revision=7,
                 deployment_selection=selected, host_id=placed["host"]["host_id"])
    project = {"schema": 1, "id": entry["project_id"], "slug": entry["allocations"]["namespace"]["application"]}
    entry["allocations"].update(c.identity(project, placed)["allocations"])
    entry["allocations"]["http_port"] = next(e.port for e in resolution.endpoints if e.role == "http" and e.listener == "http")
    declaration = c.ReleaseDeclaration.parse({"schema": 2, "topology": topology.projection(), "profile": placed,
        "topology_sha256": topology.sha256, "inventory": [asdict(m) for m in members] + [
            {"path": "deploy/bin/relay-server", "sha256": "b" * 64, "size": 1, "kind": "application"}],
        "artifacts": [], "application_executables": {"relay-server": "deploy/bin/relay-server"}})
    paths = {"relay": {"TOKEN_FILE": backup.instance_paths(entry["name"])["configuration"] + "/service-secrets/revision-7/token"} if secret else {}}
    snapshot = {"deployment": c.record_document(deployment), "resolution": c.record_document(resolution),
                "selection": selected, "declaration": declaration.projection(), "secret_paths": paths}
    return inventory, entry, snapshot


class BackupTests(unittest.TestCase):
    def test_external_state_receipt_names_declared_recovery_owner(self):
        inventory, entry, snapshot = feature_fixture(state="external", recovery_owner="media-operations")
        parsed = backup.parse_feature_snapshot(snapshot, entry)
        self.assertEqual(parsed.roles["relay"].state.recovery_owner, "media-operations")
        coverage = backup.service_coverage(inventory)
        self.assertEqual(coverage["service_state"], [{"role": "relay", "kind": "external", "recovery_owner": "media-operations"}])
        self.assertTrue(coverage["complete_instance_coverage"])
        self.assertEqual(coverage["findings"], [])
        keys = {"status": "complete", "coverage": coverage, "deployment_sha256": backup.canonical_digest(snapshot["deployment"])}
        with patch.object(backup, "archive_keys", return_value=keys):
            receipt = backup._complete(self.host, entry, str(uuid.uuid4()), {"pgbackrest_label": LABEL, "completed_at": now()})
        self.assertEqual(receipt["coverage"]["service_state"], coverage["service_state"])

    def test_feature_coverage_distinguishes_supported_components_and_local_state(self):
        for kind, complete, code in (("disposable", True, None),
                                     ("external", False, "service_state_external_owner_required"),
                                     ("local-unprotected", False, "service_state_unprotected")):
            inventory, entry, snapshot = feature_fixture(state=kind)
            self.assertEqual(backup.parse_feature_snapshot(snapshot, entry).deployment, inventory.deployment)
            coverage = backup.service_coverage(inventory)
            self.assertEqual(coverage["complete_instance_coverage"], complete)
            self.assertIn("selected_deployment", coverage["supported_components"])
            self.assertEqual(coverage["findings"], [{"code": code, "role": "relay"}] if code else [])
            if kind == "external":
                self.assertIsNone(coverage["service_state"][0]["recovery_owner"])

    def test_feature_archive_contains_exact_descriptor_and_private_service_snapshot(self):
        import feature_lifecycle
        import contracts as c
        inventory, entry, snapshot = feature_fixture(secret=True)
        resolution = replace(inventory.resolution, environments=backup.runtime_config.effective_environments(
            self.host, entry, inventory.resolution.id, inventory.resolution.roles, inventory.resolution.endpoints))
        inventory = backup.services.UnitInventory(entry["name"], inventory.topology, inventory.profile, inventory.deployment, resolution)
        snapshot["resolution"] = c.record_document(resolution)
        snapshot["selection"]["maintenance_holds"] = ["relay"]
        backup._write_private(self.host, backup._instance_settings_path(self.host, entry),
                              {"instance_id": entry["profile"]["instance_id"], "age_recipients": [RECIPIENT]})
        secret = self.host.paths.p(snapshot["secret_paths"]["relay"]["TOKEN_FILE"])
        self.host.owned_write(secret, "private-service-canary\n", mode=0o640, group=entry["name"])
        retained = secret.with_name("retained")
        self.host.owned_write(retained, "retained-service-canary\n", mode=0o640, group=entry["name"])
        index = self.host.paths.instance_config(entry["name"]) / "service-secrets.json"
        backup._write_private(self.host, index, {"private": "index-canary"})
        captured = {}
        def command(args, **kwargs):
            if args[0] == "age":
                with tarfile.open(args[-1]) as archive:
                    captured.update({m.name: archive.extractfile(m).read() for m in archive})
            return self.run_command(args, **kwargs)
        self.host.run = command
        loaded = (None, c.DeploymentSelection.parse(snapshot["selection"]), inventory, snapshot)
        with patch.object(feature_lifecycle, "load", return_value=loaded), patch.object(backup, "_record"), \
                patch.object(backup.services, "enabled_units", return_value=("http",), create=True) as enabled, \
                patch.object(backup.runtime_config, "service_secret_snapshot_paths", return_value=(index, secret, retained), create=True) as secret_files, \
                patch.object(self.host, "load_state", return_value={"current": {"version": inventory.deployment.release, "commit": "a" * 40}}):
            receipt = backup.archive_keys(self.host, entry)
        enabled.assert_called_once_with(self.host, inventory.units)
        secret_files.assert_called_once_with(self.host, entry)
        metadata = json.loads(captured["recovery/instance.json"])
        expected = copy.deepcopy(snapshot)
        expected["selection"]["enabled_roles"] = ["http"]
        canonical = backup.instance_paths(entry["name"])["configuration"] + "/service-secrets/revision-7/"
        expected["service_secret_files"] = [canonical + "retained", canonical + "token"]
        self.assertEqual(metadata["feature_recovery"], expected)
        self.assertEqual(metadata["deployment_selection"], expected["selection"])
        self.assertEqual(entry["deployment_selection"]["enabled_roles"], list(inventory.roles))
        backup.parse_feature_snapshot(metadata["feature_recovery"], metadata)
        self.assertIn("private-service-canary", captured["instances/" + entry["name"] + "/service-secrets/revision-7/token"].decode())
        self.assertNotIn("private-service-canary", json.dumps(receipt))
        self.assertNotIn("private-service-canary", json.dumps(self.commands))
        self.assertEqual(receipt["deployment_sha256"], c.canonical_digest(snapshot["deployment"]))

    def test_backup_after_takeover_keeps_source_declaration_and_effective_profile(self):
        import contracts as c
        import feature_lifecycle
        import services
        from topology import parse_profile
        inventory, entry, snapshot = feature_fixture(public=True)
        source = copy.deepcopy(snapshot["declaration"])
        profile = copy.deepcopy(entry["profile"])
        profile["host"].update(host_id=str(uuid.uuid4()), address="replacement.example.test")
        for binding in profile["listeners"].values():
            binding["dns"]["public_addresses"] = ["203.0.113.11", "2001:db8:42::11"]
        deployment = replace(inventory.deployment, profile_sha256=c.canonical_digest(profile))
        resolution = replace(inventory.resolution, deployment_sha256=c.canonical_digest(c.record_document(deployment)))
        inventory = services.UnitInventory(entry["name"], inventory.topology, parse_profile(profile, inventory.topology), deployment, resolution)
        snapshot.update(deployment=c.record_document(deployment), resolution=c.record_document(resolution))
        snapshot["selection"]["admission"]["deployment_sha256"] = resolution.deployment_sha256
        entry.update(profile=profile, host_id=profile["host"]["host_id"], deployment_selection=snapshot["selection"])
        selected = c.DeploymentSelection.parse(snapshot["selection"])
        with patch.object(feature_lifecycle, "load", return_value=(None, selected, inventory, snapshot)), \
                patch.object(services, "enabled_units", return_value=tuple(inventory.roles)), \
                patch.object(backup.runtime_config, "service_secret_snapshot_paths", return_value=()), \
                patch.object(self.host, "load_state", return_value={"current": {"version": deployment.release}}):
            saved = backup._feature_snapshot(self.host, entry, {})
        self.assertEqual(saved["declaration"], source)
        parsed = backup.parse_feature_snapshot(saved, entry)
        self.assertEqual(parsed.profile.projection(), profile)
        self.assertNotEqual(parsed.profile.projection(), source["profile"])
        changed = copy.deepcopy(entry)
        changed["profile"]["host"]["host_id"] = str(uuid.uuid4())
        with self.assertRaises(DeployError):
            backup.parse_feature_snapshot(saved, changed)

    def test_unknown_live_enablement_refuses_archive_without_using_registry_cache(self):
        import feature_lifecycle
        inventory, entry, snapshot = feature_fixture()
        selected = backup.DeploymentSelection.parse(snapshot["selection"])
        with patch.object(feature_lifecycle, "load", return_value=(None, selected, inventory, snapshot)), \
                patch.object(backup.services, "enabled_units", side_effect=DeployError("service_enablement", "Unknown"), create=True), \
                self.assertRaises(DeployError) as error:
            backup._feature_snapshot(self.host, entry, {})
        self.assertEqual(error.exception.code, "service_enablement")

    def test_feature_snapshot_rejects_selection_drift_and_secret_path_escape(self):
        _, entry, snapshot = feature_fixture(secret=True)
        for mutate in (lambda v: v["selection"].update(selected_resolution_id=str(uuid.uuid4())),
                       lambda v: v["secret_paths"]["relay"].update(TOKEN_FILE="/etc/foreign/token"),
                       lambda v: v["secret_paths"].update(relay={}),
                       lambda v: v["resolution"].update(private_revision=8)):
            bad = copy.deepcopy(snapshot)
            mutate(bad)
            with self.assertRaises(DeployError):
                backup.parse_feature_snapshot(bad, entry)

    def test_feature_bundle_success_retains_incomplete_coverage(self):
        inventory, entry, snapshot = feature_fixture(state="local-unprotected")
        operation = str(uuid.uuid4())
        keys = {"status": "complete", "coverage": backup.service_coverage(inventory),
                "deployment_sha256": backup.canonical_digest(snapshot["deployment"])}
        point = {"pgbackrest_label": LABEL, "completed_at": now()}
        with patch.object(backup, "archive_keys", return_value=keys):
            receipt = backup._complete(self.host, entry, operation, point)
        self.assertEqual(receipt["status"], "complete")
        self.assertFalse(receipt["coverage"]["complete_instance_coverage"])
        self.assertEqual(receipt["coverage"]["findings"], [{"code": "service_state_unprotected", "role": "relay"}])

    def test_reconciliation_refuses_changed_selection_before_database_work(self):
        _, entry, _ = feature_fixture()
        operation = str(uuid.uuid4())
        backup._write_private(self.host, backup._operation_path(self.host, entry, operation),
                              {"configuration_revision": 7, "selection_sha256": "a" * 64, "components": {}})
        with patch.object(backup, "_database_phase") as database, self.assertRaises(DeployError) as error:
            backup._run_bundle(self.host, entry, operation, "full", reconcile=True)
        self.assertEqual(error.exception.code, "backup_operation_conflict")
        database.assert_not_called()

    def test_restore_target_is_canonical_for_postgresql(self):
        for supplied in ("2026-11-02T07:55:00Z", "2026-11-02 07:55:00+00", "2026-11-02 15:55:00+08:00"):
            self.assertEqual("2026-11-02 07:55:00+00:00", backup._restore_target(supplied))
        self.assertEqual("2026-11-02 07:55:00.250000+00:00", backup._restore_target("2026-11-02 07:55:00.25+00"))
        for refused in ("now; rm", "2026-11-02 07:55:00", "2026-13-02 07:55:00+00", None):
            with self.assertRaises(DeployError):
                backup._restore_target(refused)

    def test_rclone_config_works_with_object_scoped_credentials(self):
        config = backup._rclone_config(dict(region="auto", access_key_id="id", secret_access_key="secret",
                                            bucket="backups", endpoint="https://account.r2.cloudflarestorage.com"))
        # Found on real R2: object-scoped credentials cannot create a bucket, and its
        # 501 to the post-upload HEAD ?versionId= fails an upload that succeeded.
        for line in ("no_check_bucket = true", "no_head = true"):
            self.assertIn(line + "\n", config)

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
        elif "SHOW archive_timeout" in kwargs.get("input", ""):
            output = "1min"
        return subprocess.CompletedProcess(args, 0, output, "")

    def test_archive_is_encrypted_before_copy_and_contains_referenced_values(self):
        state = self.host.load_state(self.entry)
        version = "20261103T000000Z-abcdef01"
        state["current"] = {"schema": 1, "layout_version": 2, "project_id": self.entry["project_id"],
                            "instance_id": self.entry["profile"]["instance_id"], "instance": self.entry["name"],
                            "environment": self.entry["environment"], "architecture": "arm64", "native_imaging": False,
                            "version": version, "commit": "a" * 40, "tree_state": "clean", "built_at": now(),
                            "source_kind": "git_archive", "source_sha256": "b" * 64,
                            "files": {name: "c" * 64 for name in ("artisan", "config/deploy.toml", "public/index.html",
                                      "private-source-maps/" + version + "/app.js.map")}}
        self.host.save_state(self.entry, state)
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
        metadata = json.loads(captured["recovery/instance.json"])
        self.assertEqual(metadata["release_commit"], "a" * 40)
        self.assertEqual(metadata["profile"], self.entry["profile"])
        self.assertEqual(metadata["topology"], self.entry["topology"])
        self.assertFalse({"uid", "gid"} & metadata["allocations"].keys())
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
        state = self.host.load_state(self.entry)["backup"]
        self.assertEqual(state["attempt"]["outcome"], "complete")
        self.assertEqual(state["last_attempt_outcome"], "complete")
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
        self.assertIn("--target=2026-11-01 03:00:00+00:00", restored)
        self.assertNotEqual(result["data_path"], cluster["data_path"])
        self.assertNotEqual(result["unit"], cluster["unit"])
        self.assertEqual(result["status"], "complete")
        self.assertGreaterEqual(result["elapsed_seconds"], 0)
        configuration = (Path(result["data_path"]).parent / "postgresql.conf").read_text()
        self.assertIn("listen_addresses = ''", configuration)
        unit = Path(result["unit_path"]).read_text()
        self.assertIn("KillSignal=SIGINT\nKillMode=mixed\n", unit)
        automatic = (Path(result["data_path"]) / "postgresql.auto.conf").read_text()
        self.assertNotIn("foreign", automatic)
        self.assertNotIn("private-secret", json.dumps(result))
        self.assertEqual(self.host.load_state(self.entry)["backup"]["restore_drill"]["status"], "complete")

    def test_feature_drill_starts_only_the_isolated_postgresql_unit(self):
        _, self.entry, _ = feature_fixture()
        self.test_restore_is_new_private_cluster_and_records_timing()
        starts = [args for args, _ in self.commands if args[:2] == ["systemctl", "start"]]
        self.assertEqual(len(starts), 1)
        self.assertRegex(starts[0][2], r"^foundry-restore-[a-f0-9]+\.service$")
        verification = self.host.load_state(self.entry)["backup"]["restore_drill"]["verification"]
        self.assertFalse(verification["custom_services_started"])
        self.assertFalse(verification["network_listeners"])
        self.assertFalse(verification["complete_instance_coverage"])

    def test_drill_reports_an_exited_postgresql_at_once(self):
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
            raise DeployError("command_failed", "no socket")
        with patch.object(backup, "_cluster", return_value=cluster), patch.object(backup, "_info", return_value=[{"label": LABEL, "info": {"size": 1}}]), \
                patch.object(backup, "_recovery_limits", return_value="max_connections = 100\n"), \
                patch.object(backup, "_restore_database", side_effect=restore), patch.object(backup, "_admin", side_effect=admin):
            with self.assertRaises(DeployError) as error:
                backup.restore_drill(self.host, self.entry, operation, retain=True, target_time="2026-11-01 03:00:00+00")
        self.assertEqual("backup_drill_failed", error.exception.code)

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
        self.assertIn((["systemctl", "reset-failed", result["unit"], result["restore_unit"]], {"check": False}), self.commands)

    def test_key_archival_does_not_refresh_an_old_database_point(self):
        operation = str(uuid.uuid4())
        old = backup.datetime.fromtimestamp(time.time() - 4 * 86400, backup.timezone.utc).isoformat()
        point = {"status": "complete", "pgbackrest_label": LABEL, "completed_at": old}
        receipt = backup._complete(self.host, self.entry, operation, point)
        self.assertEqual(receipt["fresh_at"], old)
        self.assertEqual(self.host.load_state(self.entry)["backup"]["last_complete_at"], old)

    def test_receipt_without_freshness_is_rejected_on_replay(self):
        operation = str(uuid.uuid4())
        path = backup._receipt_path(self.host, self.entry, operation)
        receipt = {"backup_id": operation, "kind": "full", "status": "complete", "completed_at": now()}
        backup._write_private(self.host, path, receipt)
        before = self.host.load_state(self.entry)
        with self.assertRaises(DeployError) as caught:
            backup.run_backup(self.host, self.entry, operation)
        self.assertEqual(caught.exception.code, "invalid_backup_receipt")
        self.assertEqual(self.host.load_state(self.entry), before)
        self.assertEqual(read_json(path), receipt)
        self.assertEqual(self.commands, [])

    def test_completion_requires_journal_configuration_revision(self):
        operation = str(uuid.uuid4())
        path = backup._operation_path(self.host, self.entry, operation)
        journal = {"operation_id": operation, "components": {}}
        backup._write_private(self.host, path, journal)
        before = self.host.load_state(self.entry)
        point = {"status": "complete", "pgbackrest_label": LABEL, "completed_at": now()}
        with self.assertRaises(DeployError) as caught:
            backup._complete(self.host, self.entry, operation, point)
        self.assertEqual(caught.exception.code, "backup_operation_conflict")
        self.assertEqual(read_json(path), journal)
        self.assertEqual(self.host.load_state(self.entry), before)
        self.assertFalse(backup._receipt_path(self.host, self.entry, operation).exists())
        self.assertEqual(self.commands, [])

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

    def test_rebinding_requires_live_archive_command(self):
        cluster = self.cluster()
        self.registry["shared"]["backup"] = {"owner": "foundry"}
        with patch.object(backup, "_cluster", return_value=cluster), \
                patch.object(backup.postgres, "cluster_settings", return_value=dict(cluster)), \
                patch.object(backup.postgres, "prepare"), \
                patch.object(backup, "_admin", return_value=subprocess.CompletedProcess([], 0, "old-command", "")), \
                patch.object(backup, "_backrest") as backrest:
            with self.assertRaises(DeployError) as caught:
                backup.rebind(self.host, self.registry)
        self.assertEqual(caught.exception.code, "backup_rebind_failed")
        backrest.assert_not_called()

    def test_rebinding_requires_live_archive_timeout(self):
        cluster = self.cluster()
        self.registry["shared"]["backup"] = {"owner": "foundry"}
        for timeout in ("0", "1min"):
            with self.subTest(timeout=timeout), \
                    patch.object(backup, "_cluster", return_value=cluster), \
                    patch.object(backup.postgres, "cluster_settings", return_value=dict(cluster)), \
                    patch.object(backup.postgres, "prepare"), \
                    patch.object(backup, "_install_timers"), patch.object(backup, "_backrest") as backrest, \
                    patch.object(backup, "_admin", side_effect=[
                        subprocess.CompletedProcess([], 0, backup._archive_command(), ""),
                        subprocess.CompletedProcess([], 0, timeout, "")]) as admin:
                if timeout == "0":
                    with self.assertRaises(DeployError) as caught:
                        backup.rebind(self.host, self.registry)
                    self.assertEqual("backup_rebind_failed", caught.exception.code)
                    backrest.assert_not_called()
                else:
                    backup.rebind(self.host, self.registry)
                    backrest.assert_called_once()
                self.assertEqual("SHOW archive_timeout;\n", admin.call_args.args[2])

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
