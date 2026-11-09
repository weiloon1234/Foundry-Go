"""Private, operation-bound Redis recovery artifacts for the cutover coordinator.

Callers hold host -> instance locks and own executor supervision. Only the shared
quiescence boundary authorizes reads/writes in the fenced window. This module
never changes role state, startup permission, or the compatibility floor.
"""
from __future__ import annotations

from contextlib import contextmanager
import hashlib
import json
import os
from pathlib import Path
import re
import tempfile
import time

import backup
import redis_acl
from contracts import (DeployError, MAX_JSON, REDIS_PORT, _unique_object, check_path,
                       instance_paths, now, require, valid_timestamp, valid_uuid)
from runtime_config import read_credentials

MAX_KEYS = 4096 * 64
MAX_KEY_BYTES = 4096
MAX_ARCHIVE_BYTES = backup.MAX_ARCHIVE_BYTES
MAX_DUMP_BYTES = 1024 * 1024
MAX_SCAN_CALLS = 10000
SCAN_SECONDS = 120
WARNING = "restore_may_replay_external_side_effects"
CAPTURE_FIELDS = {"schema", "operation_id", "snapshot_id", "source_release",
                  "configuration_revision", "inventory_sha256", "scopes"}
PLAN_FIELDS = {"schema", "operation_id", "snapshot_id", "inventory_sha256", "scopes", "identity_file"}
APPLY_FIELDS = {"schema", "operation_id", "plan_token", "confirm", "safety_snapshot"}
EVIDENCE_FIELDS = {"schema", "status", "snapshot_id", "operation_id", "host_id", "project_id", "instance_id",
                   "source_release", "configuration_revision", "inventory_sha256", "scope_sha256",
                   "captured_at", "completed_at", "key_count", "encrypted_bytes", "sha256", "object_key"}

# Encoding inside Redis avoids redis-cli's lossy handling of arbitrary DUMP bytes.
DUMP_FUNCTION = """
local function snapshot(key, limit)
    local dump = redis.call('DUMP', key)
    if not dump then return false end
    if #dump > limit then return {'limit'} end
    local expiry = redis.call('PEXPIRETIME', key)
    local hex = string.gsub(dump, '.', function(c) return string.format('%02x', string.byte(c)) end)
    return {hex, expiry}
end
"""
DUMP_SCRIPT = DUMP_FUNCTION + "return snapshot(KEYS[1], tonumber(ARGV[1]))"
SCAN_SCRIPT = """
local page = redis.call('SCAN', ARGV[1], 'MATCH', ARGV[2], 'COUNT', 256)
local size = 0
for _, key in ipairs(page[2]) do
    size = size + #key
    if #key > tonumber(ARGV[3]) or size > tonumber(ARGV[4]) then return {'limit'} end
end
return page
"""
RESTORE_SCRIPT = DUMP_FUNCTION + """
local expiry = tonumber(ARGV[1])
if expiry > 0 then
    local clock = redis.call('TIME')
    if expiry <= tonumber(clock[1]) * 1000 + math.floor(tonumber(clock[2]) / 1000) then
        redis.call('DEL', KEYS[1])
        return false
    end
end
local dump = string.gsub(ARGV[3], '..', function(c) return string.char(tonumber(c, 16)) end)
redis.call('RESTORE', KEYS[1], expiry, dump, 'REPLACE', 'ABSTTL')
return snapshot(KEYS[1], tonumber(ARGV[2]))
"""


def _json(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=True, allow_nan=False)


def _digest(value):
    return hashlib.sha256(_json(value).encode()).hexdigest()


def _crc64_table():
    table = []
    for byte in range(256):
        value = byte
        for _ in range(8):
            value = (value >> 1) ^ (0x95AC9329AC4BC9B5 if value & 1 else 0)
        table.append(value)
    return tuple(table)


CRC64_TABLE = _crc64_table()


def _crc64(data):
    # Redis's reflected CRC64/Jones, initial value zero (src/crc64.c).
    value = 0
    for byte in data:
        value = CRC64_TABLE[(value ^ byte) & 255] ^ (value >> 8)
    return value


def _sha(value):
    return isinstance(value, str) and re.fullmatch(r"[a-f0-9]{64}", value) is not None


def _integer(value, minimum=0):
    return type(value) is int and value >= minimum


def _key_text(value):
    try:
        return isinstance(value, str) and 0 < len(value.encode("utf-8")) <= MAX_KEY_BYTES and "\x00" not in value
    except UnicodeError:
        return False


def _check(condition, code):
    require(condition, code, "Redis recovery validation failed; inspect the private operation and reviewed intent.")


@contextmanager
def _failure(code, *, backup_errors=False):
    try:
        yield
    except DeployError as error:
        if (error.code.startswith(("redis_snapshot_", "redis_restore_"))
                or backup_errors and error.code.startswith("backup_")
                or error.code in {"operation_conflict", "operation_locked", "rehearsal_read_only"}):
            raise
        raise DeployError(code, "Redis recovery operation failed; no completion is claimed.") from None
    except (OSError, ValueError, TypeError, KeyError, AttributeError, OverflowError, RecursionError):
        raise DeployError(code, "Redis recovery operation failed; no completion is claimed.") from None


def _read(host, path, code):
    with _failure(code):
        with open(backup._private(host, path), "rb") as stream:
            data = stream.read(MAX_ARCHIVE_BYTES + 1)
        _check(len(data) <= MAX_ARCHIVE_BYTES, code)
        return json.loads(data, object_pairs_hook=_unique_object,
                          parse_constant=lambda _: _check(False, code))


def _write(host, path, value):
    _check(len(_json(value).encode()) <= MAX_ARCHIVE_BYTES, "redis_snapshot_limit")
    backup._write_private(host, path, value)


def _root(host, entry):
    return backup._private_directory(host, backup._private_root(host, entry) / "redis")


def _snapshot_paths(host, entry, snapshot_id):
    root = backup._private_directory(host, _root(host, entry) / "snapshots" / snapshot_id)
    return root, root / "journal.json", root / "receipt.json"


def _quiescence(host, entry, operation_id):
    import release
    _check(callable(getattr(release, "require_cutover_quiescence", None)), "operation_locked")
    release.require_cutover_quiescence(host, entry, operation_id)


def _request(request, fields, code):
    _check(isinstance(request, dict) and set(request) == fields, code)
    _check(type(request["schema"]) is int and request["schema"] == 1
           and valid_uuid(request["operation_id"]), code)


def _scopes(scopes, code="redis_snapshot_scope"):
    _check(isinstance(scopes, list) and 0 < len(scopes) <= 64, code)
    total, identities = 0, []
    for scope in scopes:
        _check(isinstance(scope, dict) and set(scope) == {"storage_id", "prefix", "keys"}, code)
        _check(_sha(scope["storage_id"]) and _key_text(scope["prefix"]), code)
        keys = scope["keys"]
        _check(isinstance(keys, list), code)
        _check(len(keys) <= MAX_KEYS, "redis_snapshot_limit")
        _check(all(_key_text(key) and key.startswith(scope["prefix"]) for key in keys), code)
        _check(keys == sorted(set(keys)), code)
        identities.append((scope["storage_id"], scope["prefix"]))
        total += len(keys)
    _check(len(identities) == len(set(identities)), code)
    _check(total <= MAX_KEYS and len(_json(scopes).encode()) <= MAX_JSON, "redis_snapshot_limit")


def _identities(registry, entry):
    return {"host_id": registry["host_id"], "project_id": entry["project_id"],
            "instance_id": entry["profile"]["instance_id"]}


def _destination(host):
    repository = backup._repository_settings(host)
    return {key: repository.get(key, "path" if key == "uri_style" else None)
            for key in ("endpoint", "region", "bucket", "uri_style")}


def _ownership(host, registry, entry, scopes):
    with _failure("redis_snapshot_ownership"):
        identity = _identities(registry, entry)
        _check(all(valid_uuid(value) for value in identity.values())
               and entry["host_id"] == registry["host_id"]
               and registry["instances"].get(identity["instance_id"]) == entry, "redis_snapshot_ownership")
        observed = redis_acl.inspect(host, registry)
        _check(observed.get("owner") == "foundry" and observed.get("state") == "ready",
               "redis_snapshot_ownership")
        expected = hashlib.sha256(json.dumps(["localhost", REDIS_PORT, 0, entry["allocations"]["redis_user"], True],
                                            separators=(",", ":"), ensure_ascii=False).encode()).hexdigest()
        grants = redis_acl.granted_redis_patterns(entry)
        state = redis_acl._load_state(host, entry)
        _check(not state.get("pending") and not entry.get("redis_grants", {}).get("pending_operation"),
               "operation_locked")
        committed = state.get("committed", {})
        _check(committed.get("patterns") == sorted(grants)
               and committed.get("channels") == sorted(redis_acl.granted_redis_channels(entry)),
               "redis_snapshot_ownership")
        for scope in scopes:
            redis_acl._validate_prefix(entry, scope["prefix"])
            _check(scope["prefix"].startswith("foundry:jobs:") and scope["prefix"] + "*" in grants,
                   "redis_snapshot_scope")
            _check(scope["storage_id"] == expected, "redis_snapshot_ownership")
            for other in registry["instances"].values():
                if other["profile"]["instance_id"] == identity["instance_id"]:
                    continue
                other_state = redis_acl._load_state(host, other)
                patterns = set(redis_acl.granted_redis_patterns(other))
                for policy in (other_state.get("committed"), other_state.get("pending")):
                    if policy:
                        patterns.update(policy["patterns"])
                _check(other["allocations"]["redis_user"] != entry["allocations"]["redis_user"],
                       "redis_snapshot_ownership")
                for pattern in patterns:
                    redis_acl._validate_prefix(other, pattern[:-1])
                    _check(not (scope["prefix"].startswith(pattern[:-1])
                                or pattern[:-1].startswith(scope["prefix"])), "redis_snapshot_ownership")
        password = read_credentials(host, entry)["redis_password"]
        _check(redis_acl._acl_owned(redis_acl._read_acl(host), entry, password, state), "redis_snapshot_ownership")
        _check(redis_acl._validate_redis_live(
            redis_acl._redis(host, "ACL", "GETUSER", entry["allocations"]["redis_user"]),
            entry, password, host, [committed]), "redis_snapshot_ownership")


def _scan(host, scopes):
    result, calls, deadline = set(), 0, time.monotonic() + SCAN_SECONDS
    with _failure("redis_snapshot_capture"):
        for scope in scopes:
            allowed, cursor = set(scope["keys"]), "0"
            while True:
                calls += 1
                _check(calls <= MAX_SCAN_CALLS and time.monotonic() < deadline, "redis_snapshot_limit")
                page = redis_acl._redis(host, "EVAL", SCAN_SCRIPT, "0", cursor, scope["prefix"] + "*",
                                        str(MAX_KEY_BYTES), str(MAX_JSON))
                _check(page != ["limit"], "redis_snapshot_limit")
                _check(isinstance(page, list) and len(page) == 2 and isinstance(page[0], str)
                       and re.fullmatch(r"0|[1-9][0-9]{0,19}", page[0]) and isinstance(page[1], list),
                       "redis_snapshot_capture")
                for key in page[1]:
                    _check(isinstance(key, str) and key in allowed, "redis_snapshot_scope")
                    result.add(key)
                _check(len(result) <= MAX_KEYS, "redis_snapshot_limit")
                cursor = page[0]
                if cursor == "0":
                    break
    return sorted(result)


def _dump(host, key):
    with _failure("redis_snapshot_capture"):
        value = redis_acl._redis(host, "EVAL", DUMP_SCRIPT, "1", key, str(MAX_DUMP_BYTES))
        return _dump_record(key, value)


def _dump_record(key, value):
    if value is None:
        return None
    _check(value != ["limit"], "redis_snapshot_limit")
    _check(isinstance(value, list) and len(value) == 2 and isinstance(value[0], str)
           and 0 < len(value[0]) <= MAX_DUMP_BYTES * 2 and len(value[0]) % 2 == 0
           and re.fullmatch(r"[0-9a-f]+", value[0]) and type(value[1]) is int
               and (value[1] == -1 or 0 < value[1] <= 2**53 - 1), "redis_snapshot_capture")
    return {"key": key, "dump": value[0], "expires_at_ms": max(0, value[1])}


def _clock(host):
    with _failure("redis_snapshot_capture"):
        value = redis_acl._redis(host, "TIME")
        _check(isinstance(value, list) and len(value) == 2
               and all(isinstance(item, str) and item.isdigit() for item in value), "redis_snapshot_capture")
        return int(value[0]) * 1000 + int(value[1]) // 1000


def _cipher(host, path):
    path = backup._private(host, path)
    size = path.stat().st_size
    _check(0 < size <= MAX_ARCHIVE_BYTES + MAX_DUMP_BYTES, "redis_snapshot_limit")
    digest = hashlib.sha256()
    with open(path, "rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(chunk)
    return {"encrypted_bytes": size, "sha256": digest.hexdigest()}


def _evidence(value, registry, entry):
    code = "redis_snapshot_verification"
    _check(isinstance(value, dict) and set(value) == EVIDENCE_FIELDS, code)
    _check(type(value["schema"]) is int and value["schema"] == 1 and value["status"] == "complete", code)
    _check(all(valid_uuid(value[field]) for field in ("snapshot_id", "operation_id", "host_id", "project_id", "instance_id")), code)
    _check(all(value[key] == item for key, item in _identities(registry, entry).items()), code)
    _check(all(_sha(value[field]) for field in ("inventory_sha256", "scope_sha256", "sha256")), code)
    _check(_integer(value["configuration_revision"], 1) and _integer(value["key_count"])
           and value["key_count"] <= MAX_KEYS and _integer(value["encrypted_bytes"], 1)
           and value["encrypted_bytes"] <= MAX_ARCHIVE_BYTES + MAX_DUMP_BYTES, code)
    _check(isinstance(value["source_release"], str) and 0 < len(value["source_release"]) <= 256, code)
    _check(all(valid_timestamp(value[field]) and value[field].endswith("Z")
               for field in ("captured_at", "completed_at")), code)
    _check(value["captured_at"] <= value["completed_at"], code)
    _check(value["object_key"] == f"redis/{value['instance_id']}/{value['snapshot_id']}.json.age", code)
    return value


def capture(host, registry, entry, request: dict) -> dict:
    _request(request, CAPTURE_FIELDS, "redis_snapshot_scope")
    _check(valid_uuid(request["snapshot_id"]) and _sha(request["inventory_sha256"])
           and _integer(request["configuration_revision"], 1)
           and isinstance(request["source_release"], str) and 0 < len(request["source_release"]) <= 256,
           "redis_snapshot_scope")
    _scopes(request["scopes"])
    _quiescence(host, entry, request["operation_id"])
    with _failure("redis_snapshot_reconciliation", backup_errors=True):
        backup.require_writable(host)
        _ownership(host, registry, entry, request["scopes"])
        root, journal_path, receipt_path = _snapshot_paths(host, entry, request["snapshot_id"])
        intent = {**request, **_identities(registry, entry)}
        if journal_path.exists():
            journal = _read(host, journal_path, "redis_snapshot_reconciliation")
            _check(journal["intent"] == intent, "operation_conflict")
            if receipt_path.exists():
                receipt = _evidence(_read(host, receipt_path, "redis_snapshot_reconciliation"), registry, entry)
                _check(receipt == journal.get("evidence"), "redis_snapshot_reconciliation")
                return receipt
        else:
            _check(not any(root.iterdir()), "redis_snapshot_reconciliation")
            _check(request["configuration_revision"] == entry["configuration_revision"], "redis_snapshot_scope")
            settings = backup._settings(host, entry)
            journal = {"intent": intent, "phase": "capture_intent", "captured_at": now(),
                       "captured_ns": time.time_ns(),
                       "destination": _destination(host), "recipients": settings["age_recipients"]}
            _write(host, journal_path, journal)
            keys, records, size = _scan(host, request["scopes"]), [], len(_json(intent))
            for key in keys:
                item = _dump(host, key)
                if item is not None:
                    size += len(_json(item)) + 1
                    _check(size <= MAX_ARCHIVE_BYTES - MAX_JSON, "redis_snapshot_limit")
                    records.append(item)
            archive = {"schema": 1, "intent": intent, "captured_at": journal["captured_at"], "records": records}
            _write(host, root / "archive.json", archive)
            journal.update(phase="captured", key_count=len(records), archive_sha256=_digest(archive))
            _write(host, journal_path, journal)
        _check(journal["phase"] in {"captured", "encrypted", "upload_intent", "verified"},
               "redis_snapshot_reconciliation")
        _check(journal["destination"] == _destination(host), "redis_snapshot_reconciliation")
        plain, encrypted = root / "archive.json", root / (request["snapshot_id"] + ".json.age")
        if journal["phase"] == "captured":
            _check(_digest(_read(host, plain, "redis_snapshot_reconciliation")) == journal["archive_sha256"],
                   "redis_snapshot_reconciliation")
            with _failure("redis_snapshot_encryption"):
                # An interrupted age process cannot publish completion. The saved archive is never recaptured.
                if encrypted.exists():
                    check_path(encrypted, "file").unlink()
                backup.encrypt_archive(host, plain, encrypted, journal["recipients"])
                journal.update(phase="encrypted", cipher=_cipher(host, encrypted))
                _write(host, journal_path, journal)
        if plain.exists():
            check_path(plain, "file").unlink()
        _check(_cipher(host, encrypted) == journal["cipher"], "redis_snapshot_reconciliation")
        key = f"redis/{entry['profile']['instance_id']}/{request['snapshot_id']}.json.age"
        uploaded = False
        if journal["phase"] == "encrypted":
            journal["phase"] = "upload_intent"
            _write(host, journal_path, journal)
            with _failure("redis_snapshot_upload"):
                backup.upload_archive(host, encrypted, key)
            uploaded = True
        # First reconcile a lost response by readback. If absent, immutable copy may
        # retry the retained bytes, but may never replace an existing different object.
        with _failure("redis_snapshot_verification"):
            try:
                backup.verify_archive(host, encrypted, key)
            except DeployError:
                if uploaded:
                    raise
                with _failure("redis_snapshot_upload"):
                    backup.upload_archive(host, encrypted, key)
                backup.verify_archive(host, encrypted, key)
        if "evidence" not in journal:
            journal["evidence"] = {key: value for key, value in intent.items() if key != "scopes"}
            journal["evidence"].update(status="complete", scope_sha256=_digest(request["scopes"]),
                                      captured_at=journal["captured_at"], completed_at=now(),
                                      key_count=journal["key_count"], object_key=key, **journal["cipher"])
        evidence = _evidence(journal["evidence"], registry, entry)
        journal["phase"] = "verified"
        _write(host, journal_path, journal)
        _write(host, receipt_path, evidence)
        return evidence


def _archive(host, path, evidence, scopes):
    code = "redis_restore_archive"
    value = _read(host, path, code)
    _check(isinstance(value, dict) and set(value) == {"schema", "intent", "captured_at", "records"}
           and type(value["schema"]) is int and value["schema"] == 1, code)
    expected = {key: evidence[key] for key in CAPTURE_FIELDS - {"scopes"}}
    expected.update({key: evidence[key] for key in ("host_id", "project_id", "instance_id")}, scopes=scopes)
    _check(value["intent"] == expected and value["captured_at"] == evidence["captured_at"]
           and type(value["intent"]["schema"]) is int
           and type(value["intent"]["configuration_revision"]) is int, code)
    records, keys = value["records"], []
    allowed = {key for scope in scopes for key in scope["keys"]}
    _check(isinstance(records, list) and len(records) == evidence["key_count"] <= MAX_KEYS, code)
    for item in records:
        _check(isinstance(item, dict) and set(item) == {"key", "dump", "expires_at_ms"}, code)
        _check(isinstance(item["key"], str) and item["key"] in allowed
               and isinstance(item["dump"], str) and 0 < len(item["dump"]) <= MAX_DUMP_BYTES * 2
               and len(item["dump"]) % 2 == 0 and re.fullmatch(r"[0-9a-f]+", item["dump"])
               and _integer(item["expires_at_ms"]) and item["expires_at_ms"] <= 2**53 - 1, code)
        payload = bytes.fromhex(item["dump"])
        # https://github.com/redis/redis/blob/7.2/src/cluster.c (verifyDumpPayload).
        _check(len(payload) > 10 and _crc64(payload[:-8]) == int.from_bytes(payload[-8:], "little"), code)
        keys.append(item["key"])
    _check(keys == sorted(set(keys)), code)
    _validate_rdb(host, path.parent, records)
    return value


def _validate_rdb(host, root, records):
    """Use Redis's offline parser to reject malformed values before any live write."""
    if not records:
        return
    with _failure("redis_restore_archive"), tempfile.TemporaryDirectory(prefix="rdb-check-", dir=root) as temporary:
        payloads = [bytes.fromhex(item["dump"]) for item in records]
        versions = {int.from_bytes(payload[-10:-8], "little") for payload in payloads}
        _check(len(versions) == 1 and 1 <= next(iter(versions)) <= 9999, "redis_restore_archive")
        version = next(iter(versions))
        data = bytearray(f"REDIS{version:04d}".encode())
        for item, payload in zip(records, payloads):
            key = item["key"].encode()
            # RDB object type, 32-bit literal key length, key, and opaque object bytes.
            data.extend(payload[:1] + b"\x80" + len(key).to_bytes(4, "big") + key + payload[1:-10])
        data.extend(b"\xff")
        data.extend(_crc64(data).to_bytes(8, "little"))
        path = Path(temporary) / "validate.rdb"
        host.owned_write(path, bytes(data), mode=0o600)
        host.run(["prlimit", "--as=536870912", "--cpu=30", "--", "redis-check-rdb", str(path)], timeout=60)


def _restore_paths(host, entry, operation):
    root = backup._private_directory(host, _root(host, entry) / "restores" / operation)
    return root, root / "plan.json", root / "progress.json", root / "receipt.json"


def _alive(item, clock):
    return item["expires_at_ms"] == 0 or item["expires_at_ms"] > clock


def plan_restore(host, registry, entry, request: dict) -> dict:
    _request(request, PLAN_FIELDS, "redis_restore_archive")
    _check(valid_uuid(request["snapshot_id"]) and _sha(request["inventory_sha256"]), "redis_restore_archive")
    _scopes(request["scopes"])
    with _failure("redis_restore_archive", backup_errors=True):
        backup.require_writable(host)
        _ownership(host, registry, entry, request["scopes"])
        identity = request["identity_file"]
        private = host.paths.p(instance_paths(entry["name"])["private"])
        with _failure("redis_restore_identity"):
            _check(isinstance(identity, str) and Path(identity).is_absolute()
                   and ".." not in Path(identity).parts and Path(identity).is_relative_to(private)
                   and request["operation_id"] in Path(identity).relative_to(private).parts,
                   "redis_restore_identity")
            backup._private(host, Path(identity))
        root, plan_path, progress_path, _ = _restore_paths(host, entry, request["operation_id"])
        intent = {key: value for key, value in request.items() if key != "identity_file"}
        intent.update(_identities(registry, entry))
        intent_path = root / "intent.json"
        if intent_path.exists():
            _check(_read(host, intent_path, "redis_restore_reconciliation") == intent, "operation_conflict")
        else:
            _write(host, intent_path, intent)
        if plan_path.exists():
            plan = _read(host, plan_path, "redis_restore_reconciliation")
            _check(plan["intent"] == intent, "operation_conflict")
            return plan["public"]
        _check(not progress_path.exists(), "redis_restore_reconciliation")
        _, _, receipt_path = _snapshot_paths(host, entry, request["snapshot_id"])
        evidence = _evidence(_read(host, receipt_path, "redis_restore_archive"), registry, entry)
        _check(evidence["inventory_sha256"] == request["inventory_sha256"]
               and evidence["scope_sha256"] == _digest(request["scopes"]), "redis_restore_archive")
        repository = backup._repository_settings(host)
        with tempfile.TemporaryDirectory(prefix="decrypt-", dir=root) as temporary:
            encrypted, plain = Path(temporary) / "snapshot.age", Path(temporary) / "archive.json"
            with _failure("redis_snapshot_verification"):
                backup._rclone(host, ["copyto", f"backup:{repository['bucket']}/{evidence['object_key']}", str(encrypted),
                                      "--max-transfer", str(evidence["encrypted_bytes"]), "--cutoff-mode", "cautious"])
                os.chmod(encrypted, 0o600)
                _check(_cipher(host, encrypted) == {key: evidence[key] for key in ("sha256", "encrypted_bytes")},
                       "redis_snapshot_verification")
            with _failure("redis_restore_identity"):
                # Limit age's file output even when the supplied encrypted object is hostile.
                host.owned_write(plain, b"", mode=0o600)
                host.run(["prlimit", "--fsize=" + str(MAX_ARCHIVE_BYTES), "--", "age", "-d", "-i", identity,
                          "-o", str(plain), str(encrypted)], timeout=120)
            archive = _archive(host, plain, evidence, request["scopes"])
            _write(host, root / "archive.json", archive)
        clock = _clock(host)
        live = {item["key"] for item in archive["records"] if _alive(item, clock)}
        current = _scan(host, request["scopes"])
        current_expiry = {}
        for key in current:
            item = _dump(host, key)
            if item is not None:
                current_expiry[key] = item["expires_at_ms"]
        current = sorted(current_expiry)
        remove = sorted(set(current) - live)
        binding = {"intent": intent, "snapshot": evidence, "archive_sha256": _digest(archive),
                   "current_keys": current, "current_expiry": current_expiry,
                   "remove": remove, "planned_at": now(), "planned_ns": time.time_ns(), "clock_ms": clock,
                   "configuration_revision": entry["configuration_revision"]}
        public = {"schema": 1, "operation_id": request["operation_id"], "snapshot": evidence,
                  "scope_sha256": evidence["scope_sha256"], "restore_keys": len(live), "remove_keys": len(remove),
                  "plan_token": _digest(binding), "warning": WARNING}
        _write(host, plan_path, {**binding, "public": public})
        return public


def apply_restore(host, registry, entry, request: dict) -> dict:
    return _restore(host, registry, entry, request, resume=False)


def resume_restore(host, registry, entry, request: dict) -> dict:
    return _restore(host, registry, entry, request, resume=True)


def _restore(host, registry, entry, request, *, resume):
    _request(request, APPLY_FIELDS, "redis_restore_confirmation")
    _check(request["confirm"] is True and _sha(request["plan_token"]), "redis_restore_confirmation")
    _quiescence(host, entry, request["operation_id"])
    with _failure("redis_restore_reconciliation"):
        backup.require_writable(host)
        root, plan_path, progress_path, receipt_path = _restore_paths(host, entry, request["operation_id"])
        plan = _read(host, plan_path, "redis_restore_stale_plan")
        binding = {key: value for key, value in plan.items() if key != "public"}
        _check(_digest(binding) == request["plan_token"] == plan["public"]["plan_token"], "redis_restore_stale_plan")
        intent, evidence = plan["intent"], _evidence(plan["snapshot"], registry, entry)
        _check(intent["operation_id"] == request["operation_id"]
               and plan["configuration_revision"] == entry["configuration_revision"]
               and all(intent[key] == value for key, value in _identities(registry, entry).items()),
               "redis_restore_stale_plan")
        scopes = intent["scopes"]
        _scopes(scopes)
        _ownership(host, registry, entry, scopes)
        safety = _evidence(request["safety_snapshot"], registry, entry)
        _check(safety["operation_id"] == request["operation_id"] and safety["snapshot_id"] != evidence["snapshot_id"]
               and safety["scope_sha256"] == evidence["scope_sha256"]
               and safety["inventory_sha256"] == evidence["inventory_sha256"]
               and safety["configuration_revision"] == entry["configuration_revision"]
               and safety["captured_at"] >= plan["planned_at"], "redis_restore_stale_plan")
        _, safety_journal_path, safety_path = _snapshot_paths(host, entry, safety["snapshot_id"])
        _check(_read(host, safety_path, "redis_restore_stale_plan") == safety, "redis_restore_stale_plan")
        safety_journal = _read(host, safety_journal_path, "redis_restore_stale_plan")
        _check(safety_journal.get("evidence") == safety and safety_journal.get("phase") == "verified"
               and _integer(safety_journal.get("captured_ns"), 1)
               and safety_journal["captured_ns"] >= plan["planned_ns"], "redis_restore_stale_plan")
        if progress_path.exists():
            progress = _read(host, progress_path, "redis_restore_reconciliation")
            _check(progress["request"] == request, "operation_conflict")
            if receipt_path.exists():
                receipt = _read(host, receipt_path, "redis_restore_reconciliation")
                _check(receipt == progress.get("evidence"), "redis_restore_reconciliation")
                return receipt
            _check(resume, "redis_restore_reconciliation")
        else:
            _check(not resume and not receipt_path.exists(), "redis_restore_reconciliation")
            progress = {"request": request, "writes": {}, "phase": "prepared"}
        archive = _archive(host, root / "archive.json", evidence, scopes)
        _check(_digest(archive) == plan["archive_sha256"], "redis_restore_archive")
        current = set(_scan(host, scopes))
        records = archive["records"]
        clock = _clock(host)
        if not progress["writes"]:
            # Natural expiry can remove keys after planning; additions always require review.
            required = {key for key, expiry in plan["current_expiry"].items() if expiry == 0 or expiry > clock}
            _check(current.issubset(set(plan["current_keys"]))
                   and required.issubset(current),
                   "redis_restore_stale_plan")
        else:
            _check(current.issubset(set(plan["current_keys"]) | {item["key"] for item in records}),
                   "redis_restore_stale_plan")
        _write(host, progress_path, progress)
        actions = [("restore", item["key"], item) for item in records if _alive(item, plan["clock_ms"])]
        actions += [("remove", key, None) for key in plan["remove"]]
        for action, key, item in actions:
            identity = action + ":" + key
            if isinstance(progress["writes"].get(identity), dict):
                continue
            progress["writes"][identity] = "dispatched_or_unknown"
            _write(host, progress_path, progress)
            if action == "restore":
                result = redis_acl._redis(host, "EVAL", RESTORE_SCRIPT, "1", key, str(item["expires_at_ms"]),
                                          str(MAX_DUMP_BYTES), input=item["dump"])
                postimage = _dump_record(key, result)
                _check(postimage is None and not _alive(item, _clock(host))
                       or postimage is not None and postimage["expires_at_ms"] == item["expires_at_ms"],
                       "redis_restore_reconciliation")
            else:
                result = redis_acl._redis(host, "DEL", key)
                _check(type(result) is int and result in (0, 1), "redis_restore_reconciliation")
                postimage = None
            progress["writes"][identity] = {"postimage_sha256": _digest(postimage), "present": postimage is not None}
            _write(host, progress_path, progress)
        # RESTORE validates/loads the exact payload. Redis may re-encode a hash or set;
        # compare its atomic post-RESTORE image, not the original object's byte order.
        for item in records:
            observed = _dump(host, item["key"])
            if _alive(item, _clock(host)):
                written = progress["writes"].get("restore:" + item["key"], {})
                _check(observed is not None and _digest(observed) == written.get("postimage_sha256"),
                       "redis_restore_reconciliation")
            else:
                _check(observed is None, "redis_restore_reconciliation")
        current = set(_scan(host, scopes))
        clock = _clock(host)
        _check(current.issubset({item["key"] for item in records})
               and not (current & set(plan["remove"]))
               and {item["key"] for item in records if _alive(item, clock)}.issubset(current),
               "redis_restore_reconciliation")
        result = progress.get("evidence") or {"schema": 1, "status": "complete", "operation_id": request["operation_id"],
                  "snapshot_id": evidence["snapshot_id"], "safety_snapshot_id": safety["snapshot_id"],
                  "scope_sha256": evidence["scope_sha256"], "completed_at": now(),
                  "restored_keys": sum(action == "restore" and progress["writes"][action + ":" + key]["present"]
                                       for action, key, _ in actions),
                  "removed_keys": len(plan["remove"])}
        progress.update(phase="complete", evidence=result)
        _write(host, progress_path, progress)
        _write(host, receipt_path, result)
        return result
