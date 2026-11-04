"""Deployment identity, persistence and refusal boundaries without services."""

import copy
import configparser
import json
from pathlib import Path
import stat
import sys
import tempfile
import threading
import time
import tomllib
import unittest
from unittest.mock import patch
import uuid

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "lib"))

from contracts import (DeployError, HOST_ROOTS, MAX_JSON, atomic_json, atomic_write, check_path, normalize_storage_endpoint,
                       identity, parse_json, read_json, validate_profile, storage_bindings, bind_storage,
                       runtime_storage, reserve_runtime_storage, select_runtime_storage,
                       validate_project, validate_registry, valid_timestamp, instance_paths)
from topology import instance_units
from host import Host, Paths


def fixture(project_slug="abc", environment="production", project_id=None, host_id=None, port=8100):
    from provision import new_entry
    from topology import validate_topology
    project = {"schema": 1, "id": project_id or str(uuid.uuid4()), "slug": project_slug}
    profile = {"schema": 2, "environment": environment, "instance_id": str(uuid.uuid4()),
               "host": {"address": "server.example.invalid", "port": 22, "host_id": host_id or str(uuid.uuid4())},
               "runtime_environment": "production", "domain": project_slug + "-" + environment + ".example.invalid",
               "certbot_email": "ops@example.invalid", "architecture": "arm64", "public_env": {},
               "retention": {"server_releases": 5, "local_releases": 3, "pre_migration_dumps": 5}, "edge_origin_lockdown": False}
    topology = validate_topology({"schema": 1, "roles": [{"name": name} for name in ("http", "worker", "scheduler")]})
    entry = new_entry(project, profile, topology, port, {"generation": 1, "framework_version": "development"})
    entry["profile"] = profile
    entry["provisioning"]["state"] = "ready"
    return project, profile, entry


def registry(*entries):
    checksum = "a" * 64
    return {"schema": 2, "layout_version": 2, "host_id": entries[0]["host_id"], "revision": 1,
            "created_at": "2026-10-13T00:00:00Z", "updated_at": "2026-10-13T00:00:00Z",
            "tools": {"framework_version": "development", "generation": 1, "bundle_sha256": checksum,
                      "protocol_min": "2.0", "protocol_max": "2.0", "active_path": "/usr/local/lib/foundry/tools/" + checksum, "upgrade_operation": None},
            "shared": {}, "projects": {entry["project_id"]: {"slug": entry["allocations"]["namespace"]["application"], "created_at": entry["created_at"]} for entry in entries},
            "instances": {entry["profile"]["instance_id"]: entry for entry in entries}}


class DeploymentContractTests(unittest.TestCase):


    def test_instance_accounts_can_traverse_every_shared_parent(self):
        roots = dict(HOST_ROOTS)
        paths = instance_paths("abc-production")
        for key in ("application", "data", "configuration", "state"):
            self.assertEqual(roots.get(str(Path(paths[key]).parent)), 0o755, key)

    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(dir=Path(tempfile.gettempdir()).resolve())
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.host = Host(Paths(self.root))
        self.project, self.profile, self.entry = fixture()

    def test_multiple_projects_and_environments_derive_unique_owned_resources(self):
        _, _, staging = fixture(environment="staging", project_id=self.project["id"], host_id=self.entry["host_id"], port=8101)
        _, _, other = fixture(project_slug="other", host_id=self.entry["host_id"], port=8102)
        value = registry(self.entry, staging, other)
        self.assertIs(validate_registry(value), value)
        for key in ("database", "redis_user", "http_port"):
            self.assertEqual(len({item["allocations"][key] for item in value["instances"].values()}), 3)
        self.assertEqual(instance_units(self.entry)["http"], "foundry-http@abc-production.service")
        self.assertEqual(self.entry["allocations"]["database"], "f_" + uuid.UUID(self.profile["instance_id"]).hex)
        self.assertEqual(staging["allocations"]["namespace"], {"application": "abc", "environment": "staging"})

    def test_profile_environment_label_does_not_silently_change_runtime_or_name(self):
        project, profile, entry = fixture(environment="uat")
        self.assertEqual(entry["name"], "abc-uat")
        self.assertEqual(profile["runtime_environment"], "production")
        self.assertEqual(identity(project, copy.deepcopy(profile)), identity(project, profile))

    def test_uninitialized_invalid_and_oversized_identities_refuse(self):
        for field, value in (("id", None), ("id", "not-a-uuid"), ("slug", "Abc"), ("slug", "a" * 21), ("schema", 2), ("schema", True)):
            project = {**self.project, field: value}
            with self.subTest(field=field, value=value), self.assertRaises(DeployError):
                validate_project(project)
        project = {**self.project, "slug": "a" * 20}
        profile = {**self.profile, "environment": "b" * 20}
        with self.assertRaises(DeployError):
            validate_profile(profile, project)

    def test_profile_refuses_private_build_keys_invalid_runtime_and_public_interpolation(self):
        variants = [{"public_env": {"PASSWORD": "canary"}}, {"public_env": {"VITE_NAME": "$SECRET"}},
                    {"runtime_environment": "uat"}, {"architecture": "mips"},
                    {"retention": {"server": 0, "local": 3, "backups": 5}},
                    {"domain": "https://example.invalid/path"}]
        for change in variants:
            with self.subTest(change=change), self.assertRaises(DeployError):
                validate_profile({**self.profile, **change}, self.project)

    def test_backup_policy_is_the_supported_policy_without_silent_customization(self):
        from contracts import BACKUP_POLICY
        validate_profile({**self.profile, "backup_policy": dict(BACKUP_POLICY)}, self.project)
        for monthly in (12, False):
            with self.subTest(monthly=monthly), self.assertRaises(DeployError):
                validate_profile({**self.profile, "backup_policy": {**BACKUP_POLICY, "monthly": monthly}}, self.project)

    def test_registry_refuses_identity_binding_and_resource_collisions(self):
        _, _, sibling = fixture(environment="staging", project_id=self.project["id"], host_id=self.entry["host_id"], port=8101)
        changes = [lambda v: v["instances"][sibling["profile"]["instance_id"]]["allocations"].update(http_port=8100),
                   lambda v: v["instances"][sibling["profile"]["instance_id"]]["profile"].update(domain=self.profile["domain"]),
                   lambda v: v["instances"][sibling["profile"]["instance_id"]].update(host_id=str(uuid.uuid4())),
                   lambda v: v["instances"][sibling["profile"]["instance_id"]]["allocations"].update(database=self.entry["allocations"]["database"]),
                   lambda v: v.update(layout_version=99),
                   lambda v: v.update(schema=True)]
        for change in changes:
            value = registry(copy.deepcopy(self.entry), copy.deepcopy(sibling))
            change(value)
            with self.subTest(value=value), self.assertRaises(DeployError):
                validate_registry(value)
        value = registry(self.entry, copy.deepcopy(sibling))
        duplicate = value["instances"][sibling["profile"]["instance_id"]]
        duplicate["profile"]["instance_id"] = self.entry["profile"]["instance_id"]
        with self.assertRaises(DeployError):
            validate_registry(value)

    def test_project_uuid_cannot_be_reused_with_another_slug(self):
        _, _, other = fixture(project_slug="other", project_id=self.project["id"], host_id=self.entry["host_id"], port=8101)
        with self.assertRaises(DeployError):
            validate_registry(registry(self.entry, other))

    def test_json_rejects_duplicates_nonfinite_invalid_and_oversized_input(self):
        for value in (b'{"schema":1,"schema":2}', b'{"nested":{"a":1,"a":2}}',
                      b'{"value":NaN}', b'{"value":Infinity}', b'{"value":-Infinity}',
                      b'{', b'\xff', b' ' * (MAX_JSON + 1)):
            with self.subTest(value=value[:64]), self.assertRaises(DeployError):
                parse_json(value)

    def test_json_recursion_failure_is_sanitized(self):
        with patch("contracts.json.loads", side_effect=RecursionError("PRIVATE_CANARY")):
            with self.assertRaises(DeployError) as result:
                parse_json(b'{"ready":true}')
        self.assertEqual(result.exception.code, "invalid_json")
        self.assertNotIn("PRIVATE_CANARY", str(result.exception))

    def test_registry_timestamps_are_valid_utc_values(self):
        self.assertTrue(valid_timestamp("2024-02-29T12:34:56Z"))
        for value in (None, True, "PRIVATE_CANARY", "2026-02-29T12:00:00Z", "2026-10-13T25:00:00Z", "2026-10-13T00:00:00+08:00"):
            self.assertFalse(valid_timestamp(value))
            for scope in ("host", "instance"):
                for key in ("created_at", "updated_at"):
                    record = registry(copy.deepcopy(self.entry))
                    target = record if scope == "host" else record["instances"][self.entry["profile"]["instance_id"]]
                    target[key] = value
                    with self.subTest(scope=scope, key=key, value=value), self.assertRaises(DeployError):
                        validate_registry(record)

    def test_malformed_state_refuses_before_commands_or_file_changes(self):
        version = "20261013T000000Z-0123456789abcdef"
        manifest = {"schema": 1, "layout_version": 2, "project_id": self.entry["project_id"],
                    "instance_id": self.entry["profile"]["instance_id"], "instance": self.entry["name"],
                    "environment": self.entry["environment"], "architecture": "arm64", "native_imaging": False,
                    "version": version, "commit": "0123456789abcdef", "tree_state": "clean", "built_at": "2026-10-13T00:00:00Z",
                    "source_kind": "git_archive", "source_sha256": "b" * 64,
                    "files": dict.fromkeys(("artisan", "config/deploy.toml", "public/index.html", "private-source-maps/" + version + "/index.map"), "a" * 64)}
        attempt = {"id": version, "action": "deploy", "release": version, "started_at": "2026-10-13T00:00:00Z",
                   "finished_at": "2026-10-13T00:00:01Z", "outcome": "succeeded", "phase": "health", "error": None}
        valid = {**self.host.load_state(self.entry), "current": manifest, "status": "healthy", "last_attempt": attempt, "history": [attempt]}
        self.host.save_state(self.entry, valid)
        self.assertEqual(self.host.load_state(self.entry), valid)
        changes = [{"current": []}, {"previous": "PRIVATE_CANARY"}, {"current": {**manifest, "instance_id": str(uuid.uuid4())}},
                   {"current": {**manifest, "files": []}}, {"last_attempt": []}, {"history": [None]}, {"history": [attempt] * 51},
                   {"configuration_revision": True}, {"configuration_revision": 0}, {"updated_at": "PRIVATE_CANARY"}]
        for key, value in (("id", "../canary"), ("release", "../canary"), ("action", []), ("outcome", {}),
                           ("phase", "unknown"), ("error", {"detail": "PRIVATE_CANARY"}), ("started_at", "invalid"), ("finished_at", None)):
            changes.append({"last_attempt": {**attempt, key: value}})
        changes.append({"history": [{**attempt, "outcome": "running", "finished_at": None}]})
        path = self.host.paths.instance_state(self.entry["name"]) / "state.json"
        for change in changes:
            malformed = {**copy.deepcopy(valid), **change}
            atomic_json(path, malformed)
            before = path.read_bytes()
            with self.subTest(change=change):
                with patch.object(self.host, "run") as run, self.assertRaises(DeployError) as result:
                    self.host.load_state(self.entry)
                self.assertEqual(result.exception.code, "invalid_state")
                self.assertNotIn("PRIVATE_CANARY", str(result.exception))
                self.assertEqual(path.read_bytes(), before)
                run.assert_not_called()

    def test_configuration_attempt_metadata_is_typed_and_required_for_recovery(self):
        version = "20261013T000000Z-0123456789abcdef"
        attempt = {"id": version, "action": "config", "release": version, "started_at": "2026-10-13T00:00:00Z",
                   "finished_at": None, "outcome": "running", "phase": "configuration", "error": None,
                   "configuration_revision": 2, "configuration_sha256": "a" * 64, "previous_configuration_sha256": "b" * 64}
        state = {**self.host.load_state(self.entry), "status": "deploying", "last_attempt": attempt}
        self.host.save_state(self.entry, state)
        self.assertEqual(self.host.load_state(self.entry), state)
        path = self.host.paths.instance_state(self.entry["name"]) / "state.json"
        before = path.read_bytes()
        for key, value in (("configuration_revision", True), ("configuration_revision", 1), ("configuration_sha256", []),
                           ("previous_configuration_sha256", "PRIVATE_CANARY"), ("configuration_applied", "true"),
                           ("roles", "http"), ("roles", ["http", "http"]), ("roles", ["unknown"]), ("roles", [None])):
            changed = {**state, "last_attempt": {**attempt, key: value}}
            with self.subTest(key=key, value=value), self.assertRaises(DeployError):
                self.host.save_state(self.entry, changed)
            self.assertEqual(path.read_bytes(), before)
        for key in ("configuration_revision", "configuration_sha256", "previous_configuration_sha256"):
            changed = copy.deepcopy(state)
            del changed["last_attempt"][key]
            with self.assertRaises(DeployError):
                self.host.save_state(self.entry, changed)
        self.assertEqual(path.read_bytes(), before)

    def test_atomic_write_is_idempotent_and_modes_are_explicit(self):
        path = self.root / "control" / "registry.json"
        value = registry(self.entry)
        self.assertTrue(atomic_json(path, value))
        before = path.stat()
        self.assertEqual(stat.S_IMODE(before.st_mode), 0o644)
        self.assertFalse(atomic_json(path, value))
        after = path.stat()
        self.assertEqual((before.st_ino, before.st_mtime_ns), (after.st_ino, after.st_mtime_ns))
        self.assertEqual(read_json(path), value)
        secret = self.root / "private" / "app.env"
        atomic_write(secret, "PASSWORD=fixture-only", 0o600)
        self.assertEqual(stat.S_IMODE(secret.stat().st_mode), 0o600)
        self.assertEqual(list(path.parent.glob(".registry.json.*")), [])

    def test_control_file_and_ancestor_symlinks_are_refused_without_touching_target(self):
        destination = self.root / "foreign"
        destination.mkdir()
        canary = destination / "canary.json"
        canary.write_text('{"foreign":true}')
        link = self.root / "linked.json"
        link.symlink_to(canary)
        ancestor = self.root / "linked-directory"
        ancestor.symlink_to(destination, target_is_directory=True)
        for path in (link, ancestor / "canary.json"):
            for operation in (lambda p: read_json(p), lambda p: atomic_json(p, {"replace": True}), lambda p: check_path(p)):
                with self.subTest(path=path), self.assertRaises(DeployError):
                    operation(path)
        self.assertEqual(canary.read_text(), '{"foreign":true}')

    def test_host_registry_modes_and_expected_host_binding_are_enforced(self):
        self.host.write_registry(registry(self.entry))
        before = self.host.paths.registry.read_bytes()
        self.assertEqual(self.host.require_identity(self.project, self.profile)[1]["profile"]["instance_id"], self.entry["profile"]["instance_id"])
        wrong = copy.deepcopy(self.profile)
        wrong["host"]["host_id"] = str(uuid.uuid4())
        with self.assertRaises(DeployError) as error:
            self.host.require_identity(self.project, wrong)
        self.assertEqual(error.exception.code, "host_mismatch")
        self.assertEqual(self.host.paths.registry.read_bytes(), before)
        self.host.paths.registry.chmod(0o666)
        with self.assertRaises(DeployError) as error:
            self.host.read_registry()
        self.assertEqual(error.exception.code, "registry_mode")

    def test_host_rejects_copied_profile_instance_and_profile_drift(self):
        self.host.write_registry(registry(self.entry))
        for changes, expected in (({"instance_id": str(uuid.uuid4())}, "identity_mismatch"),
                                  ({"public_env": {"VITE_LOCALE": "ms"}}, "profile_drift")):
            with self.subTest(changes=changes), self.assertRaises(DeployError) as error:
                self.host.require_identity(self.project, {**self.profile, **changes})
            self.assertEqual(error.exception.code, expected)

    def test_missing_state_is_never_deployed_but_dangling_symlink_is_not_missing(self):
        self.assertEqual(self.host.load_state(self.entry)["status"], "never_deployed")
        path = self.host.paths.instance_state(self.entry["name"]) / "state.json"
        path.parent.mkdir(parents=True)
        path.symlink_to(self.root / "missing")
        with self.assertRaises(DeployError):
            self.host.load_state(self.entry)

    def test_registered_paths_cannot_drift_from_the_identity_layout(self):
        value = registry(self.entry)
        value["instances"][self.entry["profile"]["instance_id"]]["allocations"]["paths"]["data"] = "/var/lib/foreign"
        with self.assertRaises(DeployError):
            self.host.write_registry(value)
        self.assertFalse(self.host.paths.registry.exists())

    def test_json_writer_never_replaces_a_readable_file_with_unreadable_data(self):
        path = self.root / "bounded.json"
        atomic_json(path, {"preserved": True})
        original = path.read_bytes()
        with patch("contracts.MAX_JSON", 64):
            for value in ({"payload": "x" * 100}, {"value": float("nan")}):
                with self.subTest(value_type=type(value).__name__), self.assertRaises(DeployError):
                    atomic_json(path, value)
                self.assertEqual(original, path.read_bytes())
                self.assertEqual({"preserved": True}, read_json(path))

    def test_instance_lock_serializes_same_instance_only(self):
        production, staging = str(uuid.uuid4()), str(uuid.uuid4())
        with self.host.lock(production):
            with self.host.lock(staging, timeout=0.01):
                pass
            with self.assertRaises(DeployError) as error:
                with self.host.lock(production, timeout=0.01):
                    pass
            self.assertEqual(error.exception.code, "operation_locked")
        with self.host.lock(production, timeout=0.01):
            pass

    def test_storage_endpoint_normalization_repairs_only_unambiguous_slips(self):
        account = "https://account.r2.cloudflarestorage.com"
        for supplied in ("account.r2.cloudflarestorage.com", account + "/", account + "/bucket",
                         account + "/bucket/", " " + account + " "):
            self.assertEqual(account, normalize_storage_endpoint(supplied, "bucket"))
        for kept in ("http://minio.internal:9000", account + "/other", account + "/bucket/extra"):
            self.assertEqual(kept, normalize_storage_endpoint(kept, "bucket"))

    def test_lock_queues_behind_a_holder_that_finishes(self):
        production, held = str(uuid.uuid4()), threading.Event()

        def holder():
            with self.host.lock(production):
                held.set()
                time.sleep(0.3)

        thread = threading.Thread(target=holder)
        thread.start()
        self.assertTrue(held.wait(5))
        with self.host.lock(production, timeout=5):
            pass
        thread.join()


def storage_fixture(namespace="", bucket="shared-bucket"):
    return {"credentials": {"app": {"mode": "static", "access_key": "canary-access", "secret_key": "canary-secret"}},
            "disks": {"uploads": {"driver": "r2", "config": {"visibility": 0}, "cloud": {
                "endpoint": "https://account.example.invalid", "bucket": bucket, "region": "auto",
                "namespace": namespace, "credentials": "app"}}}}


class StorageBindingTests(unittest.TestCase):
    def entries(self):
        _, _, first = fixture()
        _, _, second = fixture(project_slug="second", host_id=first["host_id"], port=8101)
        first["storage_bindings"], second["storage_bindings"] = {}, {}
        return registry(first, second), first, second

    def test_overlapping_and_duplicate_namespaces_are_exclusive_across_instances(self):
        for namespace in ("", "tenant/", "tenant/child/"):
            with self.subTest(namespace=namespace):
                value, first, second = self.entries()
                bind_storage(value, first, storage_fixture("tenant/"))
                with self.assertRaises(DeployError) as caught:
                    bind_storage(value, second, storage_fixture(namespace))
                self.assertEqual(caught.exception.code, "storage_binding_conflict")
        value, first, second = self.entries()
        bind_storage(value, first, storage_fixture("tenant/"))
        bind_storage(value, second, storage_fixture("tenant-other/"))
        validate_registry(value)
        self.assertNotIn("canary-secret", json.dumps(value))
        self.assertNotIn("canary-access", json.dumps(value))

    def test_endpoint_alias_does_not_bypass_bucket_ownership(self):
        value, first, second = self.entries()
        bind_storage(value, first, storage_fixture())
        changed = storage_fixture()
        changed["disks"]["uploads"]["cloud"]["endpoint"] = "https://other-endpoint.example.invalid"
        with self.assertRaises(DeployError):
            bind_storage(value, second, changed)

    def test_same_instance_aliases_require_identical_definitions(self):
        storage = storage_fixture("tenant/")
        storage["disks"]["alias"] = copy.deepcopy(storage["disks"]["uploads"])
        self.assertEqual(len(storage_bindings(storage)), 2)
        for change in ({"namespace": "tenant/child/"}, {"region": "other"}):
            changed = copy.deepcopy(storage)
            changed["disks"]["alias"]["cloud"].update(change)
            with self.assertRaises(DeployError):
                storage_bindings(changed)

    def test_registry_revalidates_overlaps_on_read(self):
        value, first, second = self.entries()
        first["storage_bindings"] = storage_bindings(storage_fixture("tenant/"))
        second["storage_bindings"] = storage_bindings(storage_fixture("tenant/child/"))
        with self.assertRaises(DeployError):
            validate_registry(value)

    def test_candidate_reservation_survives_until_selected_revision_is_known(self):
        value, first, second = self.entries()
        bind_storage(value, first, storage_fixture("old/"))
        candidate = storage_fixture("new/")
        values = {"APP__SERVICES__CREDENTIALS": json.dumps(candidate["credentials"]),
                  "APP__SERVICES__STORAGE__DISKS_FILE": "/private/disks"}
        reader = lambda path: json.dumps(candidate["disks"])
        reserve_runtime_storage(value, first, values, reader)
        for namespace in ("old/", "new/"):
            with self.assertRaises(DeployError):
                bind_storage(value, second, storage_fixture(namespace))
        select_runtime_storage(value, first, values, reader)
        self.assertNotIn("pending_storage_bindings", first)
        bind_storage(value, second, storage_fixture("old/"))

    def test_nested_override_cannot_bypass_physical_binding_validation(self):
        for values in ({"APP__SERVICES__STORAGE__DISKS__UPLOADS__CLOUD__BUCKET": "other"},
                       {"APP__SERVICES__STORAGE__DISKS": "{}", "APP__SERVICES__STORAGE__DISKS_FILE": "/private/disks"}):
            with self.assertRaises(DeployError):
                runtime_storage(values, lambda path: "{}")


if __name__ == "__main__":
    unittest.main()
