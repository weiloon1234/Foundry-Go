"""Cutover recovery uses private fixture files and Redis/age/repository fakes only."""
import copy
import hashlib
import json
from pathlib import Path
import stat
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch
import uuid

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "lib"))
import backup
import redis_acl
import redis_snapshot as snapshot
import release
from contracts import DeployError, REDIS_ACLFILE, REDIS_PORT, instance_paths
from host import Host, Paths
from test_backup import RECIPIENT
from test_contracts import fixture, registry


class RedisSnapshotTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.host = Host(Paths(Path(temporary.name)))
        _, _, self.entry = fixture()
        self.registry = registry(self.entry)
        self.host.write_registry(self.registry)
        self.prefix = "foundry:jobs:v1:abc:production:"
        self.keys = [self.prefix + name for name in ("a", "b", "c")]
        self.entry["redis_grants"]["keys"] = [self.prefix]
        self.password = "private-redis-password"
        policy = redis_acl._policy(self.entry, self.password, [self.prefix + "*"], [])
        redis_acl._save_state(self.host, self.entry, {
            "instance_id": self.entry["profile"]["instance_id"], "committed": policy})
        self.host.owned_write(self.host.paths.p(REDIS_ACLFILE),
                              redis_acl._acl_line(self.entry, self.password) + "\n", mode=0o640)
        backup._write_private(self.host, backup._instance_settings_path(self.host, self.entry), {
            "instance_id": self.entry["profile"]["instance_id"], "age_recipients": [RECIPIENT]})
        backup._write_private(self.host, self.host.paths.p(backup.REPOSITORY), {
            "endpoint": "https://backup.invalid", "region": "auto", "bucket": "private-backups",
            "access_key_id": "private-access", "secret_access_key": "private-secret", "cipher_pass": "x" * 43})
        storage = hashlib.sha256(json.dumps(["localhost", REDIS_PORT, 0,
                                            self.entry["allocations"]["redis_user"], True],
                                           separators=(",", ":")).encode()).hexdigest()
        self.scopes = [{"storage_id": storage, "prefix": self.prefix, "keys": self.keys}]
        self.request = {"schema": 1, "operation_id": str(uuid.uuid4()), "snapshot_id": str(uuid.uuid4()),
                        "source_release": "20261108T000000Z-abcdef01", "configuration_revision": 1,
                        "inventory_sha256": "a" * 64, "scopes": self.scopes}
        self.clock = 2000000000000
        self.data = {self.keys[0]: (b"\x00\xff\x80\x00\r\nprivate-payload\x00" * 2, 0),
                     self.keys[1]: (b"expiring-payload\x00" * 2, self.clock + 10000)}
        self.remote, self.commands, self.redis_calls = {}, [], []
        self.failure, self.lost_upload, self.fail_write = None, False, None
        self.pages = None
        self.host.run = self.command
        self.admin = redis_acl._redis
        self.patch(redis_acl, "_redis", side_effect=self.redis)
        self.inspection = self.patch(redis_acl, "inspect", return_value={"owner": "foundry", "state": "ready"})
        self.patch(snapshot, "read_credentials", return_value={"redis_password": self.password})
        self.quiescence = self.patch(release, "require_cutover_quiescence", create=True)

    def patch(self, target, name, **options):
        patcher = patch.object(target, name, **options)
        mocked = patcher.start()
        self.addCleanup(patcher.stop)
        return mocked

    def command(self, args, **options):
        args = list(map(str, args))
        self.commands.append((args, options))
        if args[0] == "age":
            if self.failure == "age":
                raise DeployError("command_failed", "private failure canary")
            plain = Path(args[-1])
            self.assertEqual(stat.S_IMODE(plain.stat().st_mode), 0o600)
            self.assertEqual(stat.S_IMODE(plain.parent.stat().st_mode), 0o700)
            self.assertEqual(args[1:3], ["-r", RECIPIENT])
            Path(args[args.index("-o") + 1]).write_bytes(b"fake-age\x00" + plain.read_bytes())
        elif args[0] == "prlimit":
            if "redis-check-rdb" in args:
                if self.failure == "rdb":
                    raise DeployError("command_failed", "private invalid RDB canary")
                data = Path(args[-1]).read_bytes()
                self.assertTrue(data.startswith(b"REDIS0010"))
                self.assertEqual(int.from_bytes(data[-8:], "little"), snapshot._crc64(data[:-8]))
                return subprocess.CompletedProcess(args, 0, "", "")
            if self.failure == "decrypt":
                raise DeployError("command_failed", "private identity canary")
            self.assertEqual(args[1], "--fsize=" + str(snapshot.MAX_ARCHIVE_BYTES))
            self.assertIn("-i", args)
            Path(args[args.index("-o") + 1]).write_bytes(Path(args[-1]).read_bytes().removeprefix(b"fake-age\x00"))
        elif args[0] == "rclone":
            if "copyto" in args:
                index = args.index("copyto")
                source, target = args[index + 1:index + 3]
                if source.startswith("backup:"):
                    if self.failure == "download":
                        raise DeployError("command_failed", "private download canary")
                    Path(target).write_bytes(self.remote[source])
                else:
                    self.assertIn("--immutable", args)
                    self.assertFalse((Path(source).parent / "archive.json").exists())
                    if self.failure == "upload":
                        raise DeployError("command_failed", "private upload canary")
                    payload = Path(source).read_bytes()
                    if target in self.remote:
                        self.assertEqual(payload, self.remote[target])
                    self.remote[target] = payload
                    if self.lost_upload:
                        self.lost_upload = False
                        raise DeployError("command_unavailable", "lost upload response")
            elif "check" in args:
                if self.failure == "check":
                    raise DeployError("command_failed", "private verification canary")
                index = args.index("check")
                directory, target = args[index + 1:index + 3]
                name = args[args.index("--include") + 1].lstrip("/")
                if self.remote.get(target + "/" + name) != (Path(directory) / name).read_bytes():
                    raise DeployError("command_failed", "cipher mismatch")
                self.assertIn("--download", args)
            else:
                self.fail("Unexpected repository operation")
        else:
            self.fail("Unexpected external command: " + args[0])
        return subprocess.CompletedProcess(args, 0, "", "")

    def redis(self, host, *args, **options):
        self.redis_calls.append((args, options))
        self.data = {key: value for key, value in self.data.items() if not value[1] or value[1] > self.clock}
        if args[:2] == ("ACL", "GETUSER"):
            return {"flags": ["on"], "passwords": [hashlib.sha256(self.password.encode()).hexdigest()],
                    "keys": "~" + self.prefix + "*", "channels": "", "selectors": [],
                    "commands": "-@all " + " ".join("+" + item for item in sorted(redis_acl.REDIS_COMMANDS))}
        if args == ("TIME",):
            return [str(self.clock // 1000), str(self.clock % 1000 * 1000)]
        request = json.loads(options["input"]) if args[0] == "EVAL" else None
        if args[0] == "EVAL" and args[1] == snapshot.SCAN_SCRIPT:
            if self.pages is not None:
                return self.pages.pop(0)
            return ["0", sorted(key for key in self.data if key.startswith(request["pattern"][:-1]))]
        if args[0] == "EVAL" and args[1] == snapshot.DUMP_SCRIPT:
            value = self.data.get(request["key"])
            if value is None:
                return None
            dump = b"\x00\x80" + len(value[0]).to_bytes(4, "big") + value[0] + b"\x0a\x00"
            dump += snapshot._crc64(dump).to_bytes(8, "little")
            if len(dump) > request["limit"]:
                return ["limit"]
            return [dump.hex(), value[1] or -1]
        if args[0] == "EVAL" and args[1] == snapshot.RESTORE_SCRIPT:
            key, expiry = request["key"], request["expiry"]
            if expiry and expiry <= self.clock:
                self.data.pop(key, None)
                result = None
            else:
                dump = bytes.fromhex(request["dump"])
                self.data[key] = (dump[6:-10], expiry)
                result = self.redis(host, "EVAL", snapshot.DUMP_SCRIPT, "0",
                                    input=json.dumps({"key": key, "limit": request["limit"]}))
            if self.fail_write == key:
                self.fail_write = None
                raise DeployError("command_failed", "write applied, response lost")
            return result
        if args[:2] == ("EVAL", snapshot.DELETE_SCRIPT):
            key = request["key"]
            result = int(self.data.pop(key, None) is not None)
            if self.fail_write == key:
                self.fail_write = None
                raise DeployError("command_failed", "delete applied, response lost")
            return result
        self.fail("Unexpected Redis command")

    def capture(self, **changes):
        return snapshot.capture(self.host, self.registry, self.entry, {**self.request, **changes})

    def assert_code(self, code, callback):
        with self.assertRaises(DeployError) as raised:
            callback()
        self.assertEqual(raised.exception.code, code)
        self.assertNotIn("canary", str(raised.exception))

    def restore_plan(self, evidence, operation=None):
        operation = operation or str(uuid.uuid4())
        identity = self.host.paths.p(instance_paths(self.entry["name"])["private"]) / "operations" / operation / "restore.identity"
        identity.parent.mkdir(parents=True, mode=0o700)
        self.host.owned_write(identity, "private-identity-canary", mode=0o600)
        request = {"schema": 1, "operation_id": operation, "snapshot_id": evidence["snapshot_id"],
                   "inventory_sha256": self.request["inventory_sha256"], "scopes": self.scopes,
                   "identity_file": str(identity)}
        return request, snapshot.plan_restore(self.host, self.registry, self.entry, request)

    def restore_request(self, plan):
        safety = self.capture(operation_id=plan["operation_id"], snapshot_id=str(uuid.uuid4()))
        return {"schema": 1, "operation_id": plan["operation_id"], "plan_token": plan["plan_token"],
                "confirm": True, "safety_snapshot": safety}

    def apply(self, request):
        return snapshot.apply_restore(self.host, self.registry, self.entry, request)

    def resume(self, request):
        return snapshot.resume_restore(self.host, self.registry, self.entry, request)

    def test_capture_exact_evidence_checksum_private_files_and_replay(self):
        result = self.capture()
        self.assertEqual(set(result), snapshot.EVIDENCE_FIELDS)
        self.assertEqual(result["key_count"], 2)
        self.assertEqual(result["scope_sha256"], snapshot._digest(self.scopes))
        self.assertEqual(result["sha256"], hashlib.sha256(next(iter(self.remote.values()))).hexdigest())
        self.assertEqual(result["encrypted_bytes"], len(next(iter(self.remote.values()))))
        self.assertEqual(result["object_key"], f"redis/{self.entry['profile']['instance_id']}/{self.request['snapshot_id']}.json.age")
        root, journal, receipt = snapshot._snapshot_paths(self.host, self.entry, result["snapshot_id"])
        self.assertFalse((root / "archive.json").exists())
        for path in (journal, receipt, root / (result["snapshot_id"] + ".json.age")):
            self.assertEqual(stat.S_IMODE(path.stat().st_mode), 0o600)
        self.assertEqual(stat.S_IMODE(root.stat().st_mode), 0o700)
        commands = len(self.commands)
        self.data.clear()
        self.assertEqual(self.capture(), result)
        self.assertEqual(len(self.commands), commands)
        self.assertNotIn("private-payload", json.dumps(result))
        self.quiescence.assert_called_with(self.host, self.entry, self.request["operation_id"])
        self.assert_code("operation_conflict", lambda: self.capture(source_release="different"))

    def test_capture_calls_quiescence_and_propagates_its_error(self):
        self.quiescence.side_effect = DeployError("cutover_not_drained", "Roles remain live.")
        self.assert_code("cutover_not_drained", self.capture)
        self.assertEqual(self.commands, [])
        self.inspection.assert_not_called()

    def test_unowned_storage_and_instance_refused_before_dump(self):
        with patch.object(snapshot.redis_acl, "inspect", return_value={"owner": None, "state": "absent"}):
            self.assert_code("redis_snapshot_ownership", self.capture)
        foreign = copy.deepcopy(self.scopes)
        foreign[0]["storage_id"] = "b" * 64
        self.assert_code("redis_snapshot_ownership", lambda: self.capture(scopes=foreign))
        self.entry["redis_grants"]["keys"] = []
        self.assert_code("redis_snapshot_ownership", self.capture)
        self.assertFalse(any(args[0] == "EVAL" for args, _ in self.redis_calls))

    def test_shared_scope_and_pending_grants_are_refused(self):
        _, _, other = fixture(project_slug="abc", host_id=self.entry["host_id"])
        other["redis_grants"]["keys"] = [self.prefix]
        self.registry["instances"][other["profile"]["instance_id"]] = other
        self.assert_code("redis_snapshot_ownership", self.capture)
        del self.registry["instances"][other["profile"]["instance_id"]]
        state = redis_acl._load_state(self.host, self.entry)
        state["pending"] = state["committed"]
        redis_acl._save_state(self.host, self.entry, state)
        self.assert_code("operation_locked", self.capture)

    def test_unknown_keys_incomplete_scan_duplicate_pages_and_size_bound(self):
        self.pages = [["2", []], ["3", [self.keys[0]]], ["0", [self.keys[0], self.keys[1]]]]
        self.assertEqual(self.capture()["key_count"], 2)
        self.pages = [["1", []]] * 4
        with patch.object(snapshot, "MAX_SCAN_CALLS", 3):
            self.assert_code("redis_snapshot_limit", lambda: self.capture(snapshot_id=str(uuid.uuid4())))
        self.pages = None
        self.data[self.prefix + "unknown"] = (b"secret", 0)
        self.assert_code("redis_snapshot_scope", lambda: self.capture(snapshot_id=str(uuid.uuid4())))
        del self.data[self.prefix + "unknown"]
        with patch.object(snapshot, "MAX_DUMP_BYTES", 1):
            self.assert_code("redis_snapshot_limit", lambda: self.capture(snapshot_id=str(uuid.uuid4())))

    def test_strict_requests_reject_bool_unknown_duplicate_and_foreign_scopes(self):
        for changes in ({"schema": True}, {"configuration_revision": True}, {"schema": 2},
                        {"snapshot_id": "not-uuid"}, {"inventory_sha256": "A" * 64},
                        {"scopes": []}, {"scopes": self.scopes * 2}, {"drained": True}):
            with self.subTest(changes=changes):
                self.assert_code("redis_snapshot_scope", lambda: self.capture(**changes))
        foreign = copy.deepcopy(self.scopes)
        foreign[0]["prefix"] = "foundry:jobs:v1:other:production:"
        foreign[0]["keys"] = []
        self.assert_code("redis_snapshot_ownership", lambda: self.capture(scopes=foreign))

    def test_encryption_upload_and_verification_failures_have_safe_codes(self):
        for stage, code in (("age", "redis_snapshot_encryption"), ("upload", "redis_snapshot_upload"),
                            ("check", "redis_snapshot_verification")):
            self.failure = stage
            snapshot_id = str(uuid.uuid4())
            with self.subTest(stage=stage):
                self.assert_code(code, lambda: self.capture(snapshot_id=snapshot_id))
                _, _, receipt = snapshot._snapshot_paths(self.host, self.entry, snapshot_id)
                self.assertFalse(receipt.exists())
        self.failure = None

    def test_lost_upload_reconciles_original_ciphertext_without_recapture(self):
        self.lost_upload = True
        self.assert_code("redis_snapshot_upload", self.capture)
        payload = next(iter(self.remote.values()))
        self.data.clear()
        result = self.capture()
        self.assertEqual(result["key_count"], 2)
        self.assertEqual(result["sha256"], hashlib.sha256(payload).hexdigest())
        self.assertEqual(sum("copyto" in args for args, _ in self.commands), 1)
        self.assertEqual(sum(args[0] == "age" for args, _ in self.commands), 1)

    def test_failed_upload_retries_only_the_retained_immutable_artifact(self):
        self.failure = "upload"
        self.assert_code("redis_snapshot_upload", self.capture)
        self.failure = None
        self.data.clear()
        self.assertEqual(self.capture()["key_count"], 2)
        self.assertEqual(sum(args[0] == "age" for args, _ in self.commands), 1)
        self.assertEqual(sum("copyto" in args for args, _ in self.commands), 2)

    def test_orphaned_files_repository_drift_and_changed_ciphertext_refuse_recapture(self):
        orphan = str(uuid.uuid4())
        root, _, _ = snapshot._snapshot_paths(self.host, self.entry, orphan)
        self.host.owned_write(root / "archive.json", "{}", mode=0o600)
        self.assert_code("redis_snapshot_reconciliation", lambda: self.capture(snapshot_id=orphan))
        self.lost_upload = True
        self.assert_code("redis_snapshot_upload", self.capture)
        repository_path = self.host.paths.p(backup.REPOSITORY)
        repository = backup._repository_settings(self.host)
        backup._write_private(self.host, repository_path, {**repository, "bucket": "another-bucket"})
        self.assert_code("redis_snapshot_reconciliation", self.capture)
        backup._write_private(self.host, repository_path, repository)
        root, _, _ = snapshot._snapshot_paths(self.host, self.entry, self.request["snapshot_id"])
        (root / (self.request["snapshot_id"] + ".json.age")).write_bytes(b"modified")
        self.assert_code("redis_snapshot_reconciliation", self.capture)
        self.assertEqual(sum(args[0] == "age" for args, _ in self.commands), 1)

    def test_scan_command_and_malformed_reply_fail_safely(self):
        self.pages = [[True, []]]
        self.assert_code("redis_snapshot_capture", self.capture)
        original = self.redis
        def failed(host, *args, **options):
            if args[:2] == ("EVAL", snapshot.SCAN_SCRIPT):
                raise DeployError("command_failed", "private error canary")
            return original(host, *args, **options)
        with patch.object(redis_acl, "_redis", side_effect=failed):
            self.assert_code("redis_snapshot_capture", lambda: self.capture(snapshot_id=str(uuid.uuid4())))

    def test_interrupted_capture_is_never_silently_recaptured(self):
        with patch.object(snapshot, "_dump", side_effect=DeployError("command_failed", "lost read")):
            self.assert_code("redis_snapshot_reconciliation", self.capture)
        self.data.clear()
        self.assert_code("redis_snapshot_reconciliation", self.capture)
        self.assertEqual(self.commands, [])

    def test_restore_exact_plan_confirmation_binary_expiry_and_extra_key_removal(self):
        original_state = self.host.load_state(self.entry)
        original = copy.deepcopy(self.data)
        evidence = self.capture()
        self.data[self.keys[0]] = (b"changed-current-value", 0)
        self.data[self.keys[2]] = (b"extra-current-value", 0)
        self.data["foreign:key"] = (b"keep", 0)
        plan_request, plan = self.restore_plan(evidence)
        self.assertEqual(set(plan), {"schema", "operation_id", "snapshot", "scope_sha256", "restore_keys",
                                     "remove_keys", "plan_token", "warning"})
        self.assertEqual((plan["restore_keys"], plan["remove_keys"]), (2, 1))
        self.assertEqual(plan["warning"], snapshot.WARNING)
        self.assertEqual(snapshot.plan_restore(self.host, self.registry, self.entry, plan_request), plan)
        request = self.restore_request(plan)
        for confirm in (False, 1, "true", None):
            self.assert_code("redis_restore_confirmation", lambda: self.apply({**request, "confirm": confirm}))
        result = self.apply(request)
        self.assertEqual(set(result), {"schema", "status", "operation_id", "snapshot_id", "safety_snapshot_id",
                                       "scope_sha256", "completed_at", "restored_keys", "removed_keys"})
        self.assertEqual(result["restored_keys"], 2)
        self.assertEqual(result["removed_keys"], 1)
        self.assertEqual(self.data, {**original, "foreign:key": (b"keep", 0)})
        self.assertEqual(self.apply(request), result)
        self.assertEqual(self.resume(request), result)
        self.assertFalse(any(args[0] == "systemctl" for args, _ in self.commands))
        self.assertEqual([json.loads(options["input"])["key"] for args, options in self.redis_calls
                          if args[:2] == ("EVAL", snapshot.DELETE_SCRIPT)], [self.keys[2]])
        public = json.dumps([plan, result])
        self.assertNotIn("identity_file", public)
        self.assertNotIn("private-payload", public)
        self.assertEqual(self.host.load_state(self.entry), original_state)

    def test_expired_keys_are_not_revived_and_ttl_is_never_extended(self):
        evidence = self.capture()
        self.clock += 20000
        _, plan = self.restore_plan(evidence)
        self.assertEqual(plan["restore_keys"], 1)
        request = self.restore_request(plan)
        result = self.apply(request)
        self.assertEqual(result["restored_keys"], 1)
        self.assertNotIn(self.keys[1], self.data)

    def test_expiration_between_plan_and_apply_is_allowed(self):
        evidence = self.capture()
        _, plan = self.restore_plan(evidence)
        self.clock += 20000
        self.apply(self.restore_request(plan))
        self.assertNotIn(self.keys[1], self.data)

    def test_restore_requires_matching_fresh_safety_snapshot_and_quiescence(self):
        evidence = self.capture()
        _, plan = self.restore_plan(evidence)
        request = self.restore_request(plan)
        self.assert_code("redis_restore_stale_plan", lambda: self.apply({**request, "safety_snapshot": evidence}))
        self.assert_code("redis_restore_stale_plan", lambda: self.apply({**request, "plan_token": "b" * 64}))
        self.quiescence.side_effect = DeployError("cutover_not_drained", "live roles")
        self.assert_code("cutover_not_drained", lambda: self.apply(request))
        self.assertFalse(any(args[:2] in (("EVAL", snapshot.DELETE_SCRIPT), ("EVAL", snapshot.RESTORE_SCRIPT))
                             for args, _ in self.redis_calls))

    def test_safety_snapshot_before_plan_in_same_second_cannot_authorize_restore(self):
        with patch.object(snapshot, "now", return_value="2026-11-09T00:00:00Z"):
            evidence = self.capture()
            operation = str(uuid.uuid4())
            early = self.capture(operation_id=operation, snapshot_id=str(uuid.uuid4()))
            _, plan = self.restore_plan(evidence, operation)
            self.assert_code("redis_restore_stale_plan", lambda: self.apply({
                "schema": 1, "operation_id": operation, "plan_token": plan["plan_token"],
                "confirm": True, "safety_snapshot": early}))

    def test_invalid_utf8_scope_fails_with_contract_error(self):
        scopes = copy.deepcopy(self.scopes)
        scopes[0]["keys"].append(self.prefix + "\ud800")
        self.assert_code("redis_snapshot_scope", lambda: self.capture(scopes=scopes))

    def test_new_or_missing_permanent_keys_stale_the_plan(self):
        evidence = self.capture()
        _, plan = self.restore_plan(evidence)
        self.data[self.keys[2]] = (b"new-key", 0)
        request = self.restore_request(plan)
        self.assert_code("redis_restore_stale_plan", lambda: self.apply(request))
        del self.data[self.keys[2]]
        del self.data[self.keys[0]]
        self.assert_code("redis_restore_stale_plan", lambda: self.apply(request))

    def test_partial_restore_and_delete_require_same_intent_resume(self):
        for failed_key in (self.keys[0], self.keys[2]):
            with self.subTest(key=failed_key):
                evidence = self.capture(snapshot_id=str(uuid.uuid4()))
                self.data[self.keys[2]] = (b"extra-current-value", 0)
                _, plan = self.restore_plan(evidence)
                request = self.restore_request(plan)
                self.fail_write = failed_key
                self.assert_code("redis_restore_reconciliation", lambda: self.apply(request))
                self.assert_code("redis_restore_reconciliation", lambda: self.apply(request))
                replacement = self.capture(operation_id=request["operation_id"], snapshot_id=str(uuid.uuid4()))
                self.assert_code("operation_conflict", lambda: self.resume({**request, "safety_snapshot": replacement}))
                self.assertEqual(self.resume(request)["status"], "complete")
                self.assertNotIn(self.keys[2], self.data)

    def test_resume_reconciles_dispatch_before_m3_or_failed_initial_progress_publication(self):
        for failed_publication in (False, True):
            with self.subTest(failed_publication=failed_publication):
                original = copy.deepcopy(self.data)
                evidence = self.capture(snapshot_id=str(uuid.uuid4()))
                self.data[self.keys[0]] = (b"changed", 0)
                _, plan = self.restore_plan(evidence)
                request = self.restore_request(plan)
                root, _, progress_path, _ = snapshot._restore_paths(self.host, self.entry, plan["operation_id"])
                if failed_publication:
                    original_write = snapshot._write
                    def fail_progress(host, path, value):
                        if path == progress_path:
                            raise OSError("private publication canary")
                        return original_write(host, path, value)
                    with patch.object(snapshot, "_write", side_effect=fail_progress):
                        self.assert_code("redis_restore_reconciliation", lambda: self.apply(request))
                self.assertFalse(progress_path.exists())
                self.assertFalse((root / "write-intent.json").exists())
                self.assertEqual(self.resume(request)["status"], "complete")
                self.assertEqual(self.data, original)
                self.quiescence.assert_called_with(self.host, self.entry, plan["operation_id"])

    def test_missing_progress_after_possible_restore_or_delete_is_not_unstarted(self):
        for failed_key in (self.keys[0], self.keys[2]):
            with self.subTest(key=failed_key):
                self.data.pop(self.keys[2], None)
                evidence = self.capture(snapshot_id=str(uuid.uuid4()))
                self.data[self.keys[2]] = (b"extra", 0)
                _, plan = self.restore_plan(evidence)
                request = self.restore_request(plan)
                self.fail_write = failed_key
                self.assert_code("redis_restore_reconciliation", lambda: self.apply(request))
                root, _, progress_path, _ = snapshot._restore_paths(self.host, self.entry, plan["operation_id"])
                self.assertEqual(snapshot._read(self.host, root / "write-intent.json", "redis_restore_reconciliation"), request)
                progress_path.unlink()
                before = copy.deepcopy(self.data)
                calls = len(self.redis_calls)
                self.assert_code("redis_restore_reconciliation", lambda: self.resume(request))
                self.assertEqual(self.data, before)
                self.assertFalse(any(args[:2] in (("EVAL", snapshot.RESTORE_SCRIPT), ("EVAL", snapshot.DELETE_SCRIPT))
                                     for args, _ in self.redis_calls[calls:]))

    def test_unstarted_resume_preserves_intent_safety_drift_ownership_and_exit_checks(self):
        evidence = self.capture()
        _, plan = self.restore_plan(evidence)
        request = self.restore_request(plan)
        root, _, progress_path, receipt_path = snapshot._restore_paths(self.host, self.entry, plan["operation_id"])
        self.assert_code("redis_restore_stale_plan", lambda: self.resume({**request, "plan_token": "b" * 64}))
        self.assert_code("redis_restore_stale_plan", lambda: self.resume({**request, "safety_snapshot": evidence}))
        self.assert_code("redis_restore_stale_plan", lambda: self.resume({**request, "operation_id": str(uuid.uuid4())}))
        with patch.object(redis_acl, "inspect", return_value={"owner": None}):
            self.assert_code("redis_snapshot_ownership", lambda: self.resume(request))
        self.quiescence.side_effect = DeployError("cutover_not_drained", "Executor has not exited.")
        self.assert_code("cutover_not_drained", lambda: self.resume(request))
        self.quiescence.side_effect = None
        self.data[self.keys[2]] = (b"new", 0)
        self.assert_code("redis_restore_stale_plan", lambda: self.resume(request))
        self.data.pop(self.keys[2])
        self.assertFalse(progress_path.exists())
        snapshot._write(self.host, receipt_path, {})
        self.assert_code("redis_restore_reconciliation", lambda: self.resume(request))
        receipt_path.unlink()
        self.host.owned_write(progress_path, "{", mode=0o600)
        self.assert_code("redis_restore_reconciliation", lambda: self.resume(request))
        self.assertFalse((root / "write-intent.json").exists())

    def test_write_intent_is_durable_before_first_redis_write(self):
        evidence = self.capture()
        _, plan = self.restore_plan(evidence)
        request = self.restore_request(plan)
        root, _, progress_path, _ = snapshot._restore_paths(self.host, self.entry, plan["operation_id"])
        actual = self.redis
        def check_dispatch(host, *args, **options):
            if args[:2] == ("EVAL", snapshot.RESTORE_SCRIPT):
                self.assertEqual(snapshot._read(host, root / "write-intent.json", "redis_restore_reconciliation"), request)
                progress = snapshot._read(host, progress_path, "redis_restore_reconciliation")
                key = json.loads(options["input"])["key"]
                self.assertEqual(progress["writes"]["restore:" + key], "dispatched_or_unknown")
            return actual(host, *args, **options)
        with patch.object(redis_acl, "_redis", side_effect=check_dispatch):
            self.assertEqual(self.resume(request)["status"], "complete")

    def test_restore_verifies_bytes_before_receipt(self):
        evidence = self.capture()
        _, plan = self.restore_plan(evidence)
        request = self.restore_request(plan)
        actual = self.redis
        def corrupt(host, *args, **options):
            value = actual(host, *args, **options)
            if args[:2] == ("EVAL", snapshot.RESTORE_SCRIPT):
                self.data[json.loads(options["input"])["key"]] = (b"wrong-restored-value", 0)
            return value
        with patch.object(redis_acl, "_redis", side_effect=corrupt):
            self.assert_code("redis_restore_reconciliation", lambda: self.apply(request))
        _, _, _, receipt = snapshot._restore_paths(self.host, self.entry, request["operation_id"])
        self.assertFalse(receipt.exists())

    def test_corrupt_cipher_archive_wrong_instance_and_private_identity(self):
        evidence = self.capture()
        key = "backup:private-backups/" + evidence["object_key"]
        original = self.remote[key]
        self.remote[key] = original + b"corrupt"
        self.assert_code("redis_snapshot_verification", lambda: self.restore_plan(evidence))
        self.remote[key] = original
        self.failure = "decrypt"
        self.assert_code("redis_restore_identity", lambda: self.restore_plan(evidence))
        self.failure = None
        plan_request, plan = self.restore_plan(evidence)
        private = Path(plan_request["identity_file"])
        private.chmod(0o644)
        self.assert_code("redis_restore_identity", lambda: snapshot.plan_restore(self.host, self.registry, self.entry, plan_request))
        private.chmod(0o600)
        root, _, _, _ = snapshot._restore_paths(self.host, self.entry, plan["operation_id"])
        archive = snapshot._read(self.host, root / "archive.json", "redis_restore_archive")
        archive["records"][-1]["key"] = "foreign:key"
        snapshot._write(self.host, root / "archive.json", archive)
        request = self.restore_request(plan)
        self.assert_code("redis_restore_archive", lambda: self.apply(request))
        wrong = copy.deepcopy(evidence)
        wrong["instance_id"] = str(uuid.uuid4())
        self.assert_code("redis_snapshot_verification", lambda: snapshot._evidence(wrong, self.registry, self.entry))

    def test_capture_and_restore_keep_keys_prefixes_and_payloads_out_of_process_argv(self):
        observed, original_command = [], self.command
        def run(args, **options):
            if args[0] != "redis-cli":
                return original_command(args, **options)
            observed.append((args, options))
            for private in (self.prefix, "private-payload", "private-payload".encode().hex(), "private-auth"):
                self.assertNotIn(private, " ".join(args))
            self.assertEqual(options["env"], {"REDISCLI_AUTH": "private-auth"})
            if "EVAL" in args:
                self.assertIn("-x", args)
                command = args[args.index("EVAL"):]
                self.assertEqual(len(command), 3)
            elif "ACL" in args:
                command = args[args.index("ACL"):]
            else:
                command = args[args.index("TIME"):]
            result = self.redis(self.host, *command, **options)
            return subprocess.CompletedProcess(args, 0, json.dumps(result), "")
        with patch.object(redis_acl, "_host_secrets", return_value={"redis_user": "admin", "redis_password": "private-auth"}), \
                patch.object(redis_acl, "_redis", side_effect=self.admin), \
                patch.object(self.host, "run", side_effect=run):
            original = copy.deepcopy(self.data)
            evidence = self.capture()
            self.data[self.keys[2]] = (b"extra", 0)
            _, plan = self.restore_plan(evidence)
            self.assertEqual(self.apply(self.restore_request(plan))["status"], "complete")
            self.assertEqual(self.data, original)
        scripts = {args[args.index("EVAL") + 1] for args, _ in observed if "EVAL" in args}
        self.assertEqual(scripts, {snapshot.SCAN_SCRIPT, snapshot.DUMP_SCRIPT, snapshot.RESTORE_SCRIPT, snapshot.DELETE_SCRIPT})
        self.assertTrue(any("private-payload".encode().hex() in options.get("input", "") for _, options in observed))

    def test_crc64_matches_redis_vector_and_every_dump_is_checked_before_writes(self):
        # Independent check vector from Redis src/crc64.c.
        self.assertEqual(snapshot._crc64(b"123456789"), 0xe9c6d914c4b8d9ca)
        evidence = self.capture()
        _, plan = self.restore_plan(evidence)
        root, _, _, _ = snapshot._restore_paths(self.host, self.entry, plan["operation_id"])
        path = root / "archive.json"
        original = snapshot._read(self.host, path, "redis_restore_archive")
        for change in ("dump", "schema", "revision"):
            archive = copy.deepcopy(original)
            if change == "dump":
                archive["records"][-1]["dump"] = "ff" + archive["records"][-1]["dump"][2:]
            elif change == "schema":
                archive["intent"]["schema"] = True
            else:
                archive["intent"]["configuration_revision"] = True
            snapshot._write(self.host, path, archive)
            self.assert_code("redis_restore_archive", lambda: snapshot._archive(self.host, path, evidence, self.scopes))
        self.assertFalse(any(args[:2] == ("EVAL", snapshot.RESTORE_SCRIPT) for args, _ in self.redis_calls))

    def test_offline_parser_failure_blocks_plan_and_apply(self):
        evidence = self.capture()
        self.failure = "rdb"
        self.assert_code("redis_restore_archive", lambda: self.restore_plan(evidence))
        self.failure = None
        _, plan = self.restore_plan(evidence)
        request = self.restore_request(plan)
        self.failure = "rdb"
        self.assert_code("redis_restore_archive", lambda: self.apply(request))
        self.assertFalse(any(args[:2] == ("EVAL", snapshot.RESTORE_SCRIPT) for args, _ in self.redis_calls))

    def test_restore_identity_path_and_changed_plan_intent_fail_closed(self):
        evidence = self.capture()
        request, _ = self.restore_plan(evidence)
        for path in ("relative", str(Path(request["identity_file"]).parents[1] / "identity"), "/private/tmp/identity"):
            self.assert_code("redis_restore_identity", lambda: snapshot.plan_restore(
                self.host, self.registry, self.entry, {**request, "identity_file": path}))
        self.assert_code("operation_conflict", lambda: snapshot.plan_restore(
            self.host, self.registry, self.entry, {**request, "snapshot_id": str(uuid.uuid4())}))

    def test_identity_requires_exact_operation_directory_filename_and_private_modes(self):
        evidence = self.capture()
        request, plan = self.restore_plan(evidence)
        identity = Path(request["identity_file"])
        private = self.host.paths.p(instance_paths(self.entry["name"])["private"])
        self.assertEqual(identity, private / "operations" / request["operation_id"] / "restore.identity")
        self.assertEqual(plan["operation_id"], request["operation_id"])
        alternatives = [identity.with_name("identity"), identity.parent / "nested" / "restore.identity",
                        private / "intake" / request["operation_id"] / "restore.identity",
                        private / "operations" / str(uuid.uuid4()) / "restore.identity",
                        private / "operations" / ("cutover-restore-" + request["operation_id"] + ".identity")]
        for alternative in alternatives:
            with self.subTest(path=alternative):
                alternative.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
                self.host.owned_write(alternative, "private-identity-canary", mode=0o600)
                self.assert_code("redis_restore_identity", lambda: snapshot.plan_restore(
                    self.host, self.registry, self.entry, {**request, "identity_file": str(alternative)}))
        for path, invalid_mode, original_mode in ((identity, 0o644, 0o600), (identity.parent, 0o755, 0o700)):
            path.chmod(invalid_mode)
            self.assert_code("redis_restore_identity", lambda: snapshot.plan_restore(self.host, self.registry, self.entry, request))
            path.chmod(original_mode)
        identity.unlink()
        identity.symlink_to(alternatives[0])
        self.assert_code("redis_restore_identity", lambda: snapshot.plan_restore(self.host, self.registry, self.entry, request))


if __name__ == "__main__":
    unittest.main()
