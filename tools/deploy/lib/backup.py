"""pgBackRest PITR and age-encrypted instance recovery material.

The repository belongs to the Foundry cluster, not one application. Public state
contains completion receipts only. Root-owned intake files hold credentials;
no secret, key, or secret-derived digest is included in a command argument.
"""
from __future__ import annotations

from contextlib import contextmanager
import configparser
import fcntl
import hashlib
from datetime import datetime, timezone
import json
import os
from pathlib import Path
import pwd
import re
import secrets
import shutil
import stat
import sys
import tarfile
import tempfile
import time
import uuid
from urllib.parse import urlsplit

# Executable service/archiver entrypoints use Python isolated mode. Import only
# this immutable bundle's sibling modules, never the caller's working directory.
if __name__ == "__main__":
    sys.path.insert(0, str(Path(__file__).resolve().parent))

from contracts import (BACKUP_POLICY, DeployError, POSTGRES_PORT, check_path, instance_paths,
                       now, parse_env, read_json, require, valid_uuid, storage_bindings,
                       check_storage_bindings, bind_storage, runtime_storage)
from host import Host
import packages
import postgres
import runtime_config

STANZA = "foundry"
# Foundry-owned paths only: never the distro /etc/pgbackrest or /var/lib/pgbackrest,
# which a foreign pgBackRest may use and foundry-postgres cannot traverse.
CONFIG_DIR = "/etc/foundry/pgbackrest"
CONFIG = CONFIG_DIR + "/foundry.conf"
WORK = postgres.CLUSTER_HOME + "/pgbackrest"
REPOSITORY = "/etc/foundry/backup/repository.json"
BACKUP_LABEL = r"[0-9]{8}-[0-9]{6}F(?:_[0-9]{8}-[0-9]{6}[DI])?"
MAX_ARCHIVE_BYTES = 64 * 1024 * 1024


def _private(host, path, *, directory=False):
    path = check_path(path, "directory" if directory else "file")
    require(path.exists(), "backup_configuration_missing", "Configure the private backup repository and recovery recipients first.")
    info = path.stat()
    require(stat.S_IMODE(info.st_mode) == (0o700 if directory else 0o600)
            and (info.st_uid == 0 or host.paths.root != Path("/")),
            "backup_private_permissions", "Backup secret files must be root-owned 0600, directories 0700.")
    return path


def _private_directory(host, path):
    check_path(path, "directory")
    host.check_control_directory(path.parent)
    path.mkdir(mode=0o700, parents=True, exist_ok=True)
    return _private(host, path, directory=True)


def _write_private(host, path, value):
    check_path(path.parent, "directory")
    path.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    host.check_control_directory(path.parent)
    host.owned_write(check_path(path, "file"), json.dumps(value, sort_keys=True) + "\n", mode=0o600)


def _instance_settings_path(host, entry):
    return host.paths.instance_config(entry["name"]) / "backup.json"


def _settings(host, entry):
    value = read_json(_private(host, _instance_settings_path(host, entry)))
    require(value.get("instance_id") == entry["profile"]["instance_id"],
            "backup_ownership", "Backup configuration belongs to a different instance.")
    _recipients(value.get("age_recipients"))
    return value


def _line(value):
    require(isinstance(value, str) and 0 < len(value) <= 4096
            and all(32 <= ord(c) < 127 for c in value),
            "backup_configuration_invalid", "Backup configuration contains an invalid value.")
    return value


def _repository(value):
    require(isinstance(value, dict), "backup_configuration_invalid", "A private S3 repository binding is required.")
    for key in ("endpoint", "region", "bucket", "access_key_id", "secret_access_key"):
        _line(value.get(key))
    endpoint = urlsplit(value["endpoint"])
    require(endpoint.scheme in ("https", "http") and endpoint.hostname
            and not endpoint.username and not endpoint.password
            and endpoint.path in ("", "/") and not endpoint.query and not endpoint.fragment,
            "backup_configuration_invalid", "Use an explicit S3 endpoint without credentials or a path.")
    require(endpoint.scheme == "https" or value.get("allow_http") is True,
            "backup_configuration_invalid", "HTTP S3 endpoints require explicit disposable-fixture permission.")
    require(re.fullmatch(r"[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]", value["bucket"]) is not None
            and value.get("uri_style", "path") in ("path", "host"),
            "backup_configuration_invalid", "Invalid bucket or S3 URI style.")
    return endpoint


def _recipients(value):
    require(isinstance(value, list) and 0 < len(value) <= 16
            and all(isinstance(item, str) and re.fullmatch(r"age1[0-9a-z]{58}", item) for item in value)
            and len(set(value)) == len(value),
            "backup_recipients_invalid", "Supply public age recipients only; decryption identities must stay off-host.")
    return value


def _repository_settings(host):
    value = read_json(_private(host, host.paths.p(REPOSITORY)))
    _repository(value)
    require(re.fullmatch(r"[A-Za-z0-9_-]{43,128}", value.get("cipher_pass", "")) is not None,
            "backup_configuration_invalid", "Restore the repository encryption passphrase; never regenerate it for an existing repository.")
    return value


def _cluster(host, registry):
    cluster = registry.get("shared", {}).get("postgresql", {})
    observed = postgres.inspect(host, registry)
    require(cluster.get("owner") == observed.get("owner") == "foundry"
            and cluster.get("cluster") == observed.get("cluster") == "foundry"
            and cluster.get("state") == observed.get("state") == "ready"
            and cluster.get("port") == observed.get("port")
            and type(cluster.get("port")) is int and POSTGRES_PORT <= cluster["port"] <= POSTGRES_PORT + 99
            and cluster.get("ownership_token") and cluster["ownership_token"] == observed.get("ownership_token")
            and cluster.get("major") in (16, 18)
            and cluster.get("unit") == f"postgresql@{cluster.get('major')}-foundry.service",
            "backup_cluster_ownership", "Only the verified Foundry PostgreSQL cluster may be backed up.")
    data = cluster.get("data_path")
    require(data == f"{postgres.CLUSTER_HOME}/{cluster['major']}/foundry",
            "backup_cluster_ownership", "Foundry data directory must match its dedicated cluster allocation.")
    return cluster


def _backrest(host, arguments, *, timeout=1800):
    return host.run(["runuser", "-u", postgres.cluster_owner(), "--", "pgbackrest",
                     "--config=" + str(host.paths.p(CONFIG)), "--stanza=" + STANZA, *arguments],
                    timeout=timeout)


@contextmanager
def _repository_lock(host):
    path = check_path(host.paths.p("/run/lock/foundry/backup.lock"), "file")
    path.parent.mkdir(parents=True, exist_ok=True)
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_NOFOLLOW, 0o600)
    try:
        try:
            fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            raise DeployError("backup_busy", "A Foundry repository operation is active; retry when it finishes.") from None
        yield
    finally:
        os.close(fd)


def _private_root(host, entry):
    return _private_directory(host, host.paths.p(instance_paths(entry["name"])["private"]) / "backups")


def _record(host, entry, kind, receipt):
    state = host.load_state(entry)
    value = state.setdefault("backup", dict(entry.get("backup", {})))
    value[kind] = receipt
    if kind == "database":
        value.update(last_complete_id=receipt["backup_id"], last_complete_at=receipt.get("fresh_at", receipt["completed_at"]),
                     last_attempt_outcome="complete")
    elif kind == "restore_drill":
        value.update(restore_drill_outcome=receipt["status"], last_restore_drill_at=receipt.get("completed_at"))
    elif kind == "attempt":
        value["last_attempt_outcome"] = receipt["outcome"]
    elif kind == "policy":
        value["destination_ref"] = CONFIG
    entry.setdefault("backup", {}).update(value)
    host.save_state(entry, state)


@contextmanager
def _attempt(host, entry):
    _record(host, entry, "attempt", {"outcome": "running", "at": now()})
    try:
        yield
    except BaseException:
        _record(host, entry, "attempt", {"outcome": "unknown", "at": now()})
        raise


def _info(host):
    try:
        result = json.loads(_backrest(host, ["--output=json", "info"], timeout=60).stdout)
        found = [item for item in result if item["name"] == STANZA]
        require(len(found) == 1 and found[0]["status"]["code"] in (0, 2),
                "backup_repository_unhealthy", "The Foundry backup repository is not healthy.")
        backups = found[0].get("backup", [])
        require(isinstance(backups, list), "backup_repository_unhealthy", "Invalid repository backup inventory.")
        return backups
    except (ValueError, TypeError, KeyError):
        raise DeployError("backup_repository_unhealthy", "Cannot read the Foundry backup inventory.") from None


def _database_backup(host, kind):
    require(kind in ("full", "diff"), "backup_type_invalid", "Select a full or differential backup.")
    before = {item["label"] for item in _info(host)}
    _backrest(host, ["--type=" + kind, "--no-expire-auto", "backup"])
    completed = [item for item in _info(host) if item["label"] not in before and item["type"] == kind
                 and not item.get("error") and item.get("timestamp", {}).get("stop")]
    require(len(completed) == 1 and re.fullmatch(BACKUP_LABEL, completed[0]["label"]),
            "backup_outcome_unknown", "Backup completion is uncertain; inspect the repository before retrying.")
    _backrest(host, ["check"], timeout=300)
    return completed[0]["label"]


def _rclone(host, arguments, source=None):
    repositories = {"backup": _repository_settings(host)}
    if source:
        repositories["app"] = source
    root = _private_directory(host, host.paths.p("/etc/foundry/backup"))
    with tempfile.TemporaryDirectory(prefix="rclone-", dir=root) as temporary:
        config = Path(temporary) / "rclone.conf"
        sections = []
        for name, repository in repositories.items():
            _repository(repository)
            sections.append(f"[{name}]\ntype = s3\nprovider = Other\nenv_auth = false\n"
                            f"endpoint = {repository['endpoint']}\nregion = {repository['region']}\n"
                            f"access_key_id = {repository['access_key_id']}\nsecret_access_key = {repository['secret_access_key']}\n"
                            f"force_path_style = {str(repository.get('uri_style', 'path') == 'path').lower()}\n")
        host.owned_write(config, "\n".join(sections), mode=0o600)
        return host.run(["rclone", "--config=" + str(config), "--retries=1", "--low-level-retries=1", *arguments],
                        timeout=1800)


def _archive_files(host, entry):
    directory = host.paths.instance_config(entry["name"])
    files = {}
    # The age recipient holders must also recover the repository's encryption
    # key after loss of this host. This remains inside the encrypted archive.
    files["recovery/pgbackrest-repository.json"] = _private(host, host.paths.p(REPOSITORY))
    for name in ("app.env", "secrets.json"):
        path = check_path(directory / name, "file")
        require(path.exists(), "backup_configuration_missing", "Instance environment and keyring must exist before backup.")
        files["instances/" + entry["name"] + "/" + name] = path
    # The whole keyring/config revision is retained, not only the active key.
    for count, path in enumerate(directory.rglob("*"), start=1):
        require(count <= 2048, "backup_archive_size", "Key/config archive contains too many entries.")
        check_path(path)
        if path.is_file():
            files["instances/" + entry["name"] + "/" + str(path.relative_to(directory))] = path
    for key, value in parse_env(files["instances/" + entry["name"] + "/app.env"].read_text()).items():
        if key.endswith("_FILE") and value:
            require(value.startswith("/") and ".." not in Path(value).parts,
                    "backup_archive_path", "Config file references must be absolute and bounded.")
            # Only this instance's configuration can authorize an archive input.
            require(Path(value).is_relative_to(Path(instance_paths(entry["name"])["configuration"])),
                    "backup_archive_path", "Move referenced config/key files beneath this instance's configuration directory.")
            files["files/" + value.lstrip("/")] = check_path(host.paths.p(value), "file")
    size = 0
    for path in files.values():
        require(path.is_file() and not path.stat().st_mode & 0o007
                and (path.stat().st_uid == 0 or host.paths.root != Path("/")),
                "backup_archive_path", "Key archive inputs must be root-owned private regular files.")
        size += path.stat().st_size
        require(path.stat().st_size <= 16 * 1024 * 1024 and size <= MAX_ARCHIVE_BYTES,
                "backup_archive_size", "Key/config archive exceeds its bounded size.")
    return files, size


def archive_keys(host: Host, entry: dict) -> dict:
    """Encrypt the keyring/config and referenced values before any upload."""
    settings = _settings(host, entry)
    repository = _repository_settings(host)
    files, size = _archive_files(host, entry)
    root = _private_root(host, entry)
    require(shutil.disk_usage(root).free >= 2 * size + 64 * 1024 * 1024,
            "backup_space", "Insufficient private staging space for the encrypted key archive.")
    archive_id = uuid.uuid4().hex
    key = f"keys/{entry['profile']['instance_id']}/{archive_id}.tar.age"
    with tempfile.TemporaryDirectory(prefix="keys-", dir=root) as temporary:
        plain = Path(temporary) / "keys.tar"
        encrypted = Path(temporary) / (archive_id + ".tar.age")
        with tarfile.open(plain, "w", dereference=False) as archive:
            for name, path in sorted(files.items()):
                check_path(path, "file")
                archive.add(path, arcname=name, recursive=False)
        os.chmod(plain, 0o600)
        recipients = [argument for recipient in settings["age_recipients"] for argument in ("-r", recipient)]
        host.run(["age", *recipients, "-o", str(encrypted), str(plain)], timeout=120)
        require(encrypted.is_file() and encrypted.stat().st_size > 0,
                "backup_encryption_failed", "age did not produce a completed encrypted archive.")
        plain.unlink()
        target = f"backup:{repository['bucket']}/{key}"
        _rclone(host, ["copyto", str(encrypted), target, "--immutable"])
        # Download-check compares the uploaded bytes; an exit-zero upload alone
        # is not a completion receipt and a timeout never becomes success.
        _rclone(host, ["check", str(Path(temporary)), f"backup:{repository['bucket']}/keys/{entry['profile']['instance_id']}",
                       "--include", "/" + encrypted.name, "--one-way", "--download"])
    receipt = {"object_key": key, "completed_at": now(), "status": "complete"}
    _record(host, entry, "keys", receipt)
    return receipt


def _runtime_storage(host, entry):
    require("pending_storage_bindings" not in entry, "storage_binding_pending",
            "Reconcile the selected configuration before copying application objects.")
    values = runtime_config.validate_runtime_environment(host, entry)
    storage = runtime_storage(values, lambda path: runtime_config._read_value_file(host, entry, path))
    bindings = storage_bindings(storage)
    check_storage_bindings(host.read_registry(), entry, bindings)
    require(bindings == entry.get("storage_bindings"), "storage_binding_unregistered",
            "Register the selected runtime storage bindings through configuration before backup.")
    return storage, values


def _cleanup_grace(host, entry, values):
    key = "APP__FEATURES__ATTACHMENTS__CONFIG__CLEANUP_GRACE"
    text = (runtime_config._read_value_file(host, entry, values[key + "_FILE"])
            if key + "_FILE" in values else values.get(key, ""))
    parts = re.findall(r"([0-9]+(?:\.[0-9]+)?)(h|ms|us|µs|ns|m|s)", text)
    require(parts and "".join(number + unit for number, unit in parts) == text,
            "backup_cleanup_grace", "Configure an explicit attachment cleanup_grace duration before completing backups.")
    units = {"h": 3600, "m": 60, "s": 1, "ms": .001, "us": .000001, "µs": .000001, "ns": .000000001}
    seconds = sum(float(number) * units[unit] for number, unit in parts)
    require(seconds > BACKUP_POLICY["interval_hours"] * 3600 + 600,
            "backup_cleanup_grace", "cleanup_grace must exceed the object-copy interval plus timer jitter; use 72h or longer.")
    return seconds


def copy_objects(host: Host, entry: dict) -> dict:
    """Copy selected runtime bindings; preserve removed objects indefinitely."""
    storage, values = _runtime_storage(host, entry)
    repository = _repository_settings(host)
    prefix = f"objects/{entry['profile']['instance_id']}"
    copies = []
    for source in _storage_sources(storage):
        require(source["bucket"] != repository["bucket"],
                "backup_bucket_conflict", "Attachment and backup buckets must be distinct.")
        physical = {key: source[key] for key in ("endpoint", "bucket", "namespace")}
        binding_id = hashlib.sha256(json.dumps(physical, sort_keys=True).encode()).hexdigest()
        destination_prefix = prefix + "/" + binding_id
        origin = f"app:{source['bucket']}/{source['namespace']}"
        destination = f"backup:{repository['bucket']}/{destination_prefix}"
        history = f"backup:{repository['bucket']}/object-history/{entry['profile']['instance_id']}/{uuid.uuid4().hex}"
        _rclone(host, ["copy", origin, destination, "--backup-dir", history], source)
        _rclone(host, ["check", origin, destination, "--one-way", "--download"], source)
        copies.append({**physical, "objects_prefix": destination_prefix})
    require(copies, "backup_storage_missing", "No runtime object storage bindings are configured.")
    receipt = {"objects_prefix": prefix, "bindings": copies, "configuration_revision": entry["configuration_revision"],
               "cleanup_grace_seconds": _cleanup_grace(host, entry, values), "completed_at": now(), "status": "complete"}
    _record(host, entry, "objects", receipt)
    return receipt


def _receipt_path(host, entry, backup_id):
    require(valid_uuid(backup_id), "backup_id_invalid", "Select an explicit completed backup UUID.")
    return _private_root(host, entry) / "complete" / (backup_id + ".json")


def _operation_path(host, entry, operation):
    require(valid_uuid(operation), "backup_id_invalid", "Backup operations require a UUID.")
    return _private_root(host, entry) / (operation + ".json")


def _database_phase(host, path, operation, kind, *, reconcile=False, database_label=None):
    journal = read_json(_private(host, path)) if path.exists() else {
        "operation_id": operation, "kind": kind, "started_at": now(), "components": {}}
    require(journal.get("kind") == kind and journal.get("operation_id") == operation,
            "backup_operation_conflict", "This operation belongs to a different backup request.")
    require(not journal.get("abandoned_at"), "backup_abandoned", "This operation was abandoned with its outcome unresolved; select a new operation UUID.")
    database = journal["components"].get("database", {})
    if database.get("status") == "complete":
        return journal
    if database:
        require(reconcile and isinstance(database_label, str) and re.fullmatch(BACKUP_LABEL, database_label),
                "backup_outcome_unknown", "Explicit reconciliation must identify the original database label after inspecting repository evidence.")
        candidates = [item for item in _info(host) if item["label"] == database_label and item["label"] not in database["before"]
                      and item.get("type") == database["kind"] and not item.get("error")
                      and database["started_epoch"] <= item.get("timestamp", {}).get("start", 0)
                      <= item.get("timestamp", {}).get("stop", 0) <= database["deadline_epoch"]]
        require(len(candidates) == 1, "backup_outcome_unknown",
                "Repository evidence does not identify one completed backup; preserve this operation for investigation.")
        label = candidates[0]["label"]
        require(re.fullmatch(BACKUP_LABEL, label), "backup_outcome_unknown", "Invalid repository label.")
        _backrest(host, ["check"], timeout=300)
    else:
        inventory = _info(host)
        recent = [item for item in inventory if item.get("type") in ("full", "diff") and not item.get("error")
                  and 0 <= time.time() - item.get("timestamp", {}).get("stop", 0) < 24 * 3600]
        if kind == "recovery" and recent:
            label = max(recent, key=lambda item: item["timestamp"]["stop"])["label"]
        else:
            actual = "full" if kind == "full" or not any(item.get("type") == "full" and not item.get("error") for item in inventory) else "diff"
            database = {"status": "running", "kind": actual, "before": [item["label"] for item in inventory],
                        "started_epoch": int(time.time()), "deadline_epoch": int(time.time()) + 1800}
            journal["components"]["database"] = database
            _write_private(host, path, journal)
            label = _database_backup(host, actual)
    metadata = next((item for item in _info(host) if item["label"] == label), None)
    require(metadata and metadata.get("timestamp", {}).get("stop"),
            "backup_outcome_unknown", "Completed database metadata is unavailable.")
    database = {"status": "complete", "pgbackrest_label": label,
                "completed_at": datetime.fromtimestamp(metadata["timestamp"]["stop"], timezone.utc).isoformat()}
    journal["components"]["database"] = database
    _write_private(host, path, journal)
    return journal


def _component(host, path, journal, name, action, *, reconcile=False):
    component = journal["components"].get(name, {})
    if component.get("status") == "complete":
        return component
    require(not component or reconcile, "backup_outcome_unknown",
            "A component outcome is unknown; use explicit backup reconciliation.")
    journal["components"][name] = {"status": "running"}
    _write_private(host, path, journal)
    result = action()
    journal["components"][name] = result
    _write_private(host, path, journal)
    return result


def _complete(host, entry, backup_id, database, *, restore_point=None, reconcile=False):
    path = _operation_path(host, entry, backup_id)
    journal = read_json(_private(host, path)) if path.exists() else {
        "operation_id": backup_id, "configuration_revision": entry["configuration_revision"], "components": {}}
    require(not journal.get("abandoned_at"), "backup_abandoned", "This bundle was abandoned; select a new operation UUID.")
    require(journal.get("configuration_revision", entry["configuration_revision"]) == entry["configuration_revision"],
            "backup_operation_conflict", "The runtime revision changed; start a new bundle and retain this operation's evidence.")
    journal.setdefault("configuration_revision", entry["configuration_revision"])
    journal["components"]["database"] = database
    _write_private(host, path, journal)
    keys = _component(host, path, journal, "keys", lambda: archive_keys(host, entry), reconcile=reconcile)
    objects = _component(host, path, journal, "objects", lambda: copy_objects(host, entry), reconcile=reconcile)
    require(objects.get("configuration_revision", entry["configuration_revision"]) == entry["configuration_revision"],
            "backup_operation_conflict", "Object-copy revision differs from the bundle revision.")
    oldest = min(datetime.fromisoformat(value["completed_at"].replace("Z", "+00:00")) for value in (database, objects))
    require(0 <= (datetime.now(timezone.utc) - oldest).total_seconds() < objects["cleanup_grace_seconds"],
            "backup_stale", "Database point and object copy must both be newer than cleanup_grace.")
    receipt = {"backup_id": backup_id, "pgbackrest_label": database["pgbackrest_label"],
               "instance_id": entry["profile"]["instance_id"], "database_point": database,
               "key_archive": keys, "object_copy": objects, "objects_prefix": objects["objects_prefix"],
               "configuration_revision": entry["configuration_revision"], "kind": journal.get("kind", "scheduled"), "fresh_at": oldest.isoformat(),
               "cleanup_grace_seconds": objects["cleanup_grace_seconds"], "completed_at": now(), "status": "complete"}
    if restore_point:
        receipt["restore_point"] = restore_point
    _write_private(host, _receipt_path(host, entry, backup_id), receipt)
    _record(host, entry, "database", receipt)
    return receipt


def _run_bundle(host, entry, operation, kind, *, reconcile=False, database_label=None):
    complete = _receipt_path(host, entry, operation)
    if complete.exists():
        receipt = read_json(_private(host, complete))
        require(receipt.get("kind") == kind and bool(receipt.get("restore_point")) == (kind == "recovery"),
                "backup_operation_conflict", "This UUID belongs to another backup request.")
        _record(host, entry, "database", receipt)
        return receipt
    with _repository_lock(host), _attempt(host, entry):
        path = _operation_path(host, entry, operation)
        if not path.exists():
            _write_private(host, path, {"operation_id": operation, "kind": kind, "started_at": now(),
                "configuration_revision": entry["configuration_revision"], "components": {}})
        else:
            require(read_json(_private(host, path)).get("configuration_revision") == entry["configuration_revision"],
                    "backup_operation_conflict", "The runtime revision changed; preserve this operation and start a new bundle.")
        journal = _database_phase(host, path, operation, kind, reconcile=reconcile, database_label=database_label)
        point = None
        database = journal["components"]["database"]
        if kind == "recovery":
            point = "foundry_" + uuid.UUID(operation).hex
            def create_point():
                result = _admin(host, _cluster(host, host.read_registry()),
                                f"SELECT pg_create_restore_point('{point}');\nSELECT pg_switch_wal();\n")
                require(bool(result.stdout.strip()), "backup_restore_point_failed", "PostgreSQL did not confirm the named restore point.")
                _backrest(host, ["check"], timeout=300)
                return {"status": "complete", "completed_at": now(), "restore_point": point}
            # A repeated named point would make recovery ambiguous. Unknown WAL
            # publication remains unresolved; an operator must choose a new operation.
            named = _component(host, path, journal, "restore_point", create_point)
            database = {**database, "completed_at": named["completed_at"]}
        receipt = _complete(host, entry, operation, database, restore_point=point, reconcile=reconcile)
        _backrest(host, ["expire"], timeout=300)
        return receipt


def run_backup(host: Host, entry: dict, backup_id: str, kind: str = "full") -> dict:
    require(kind in ("full", "diff"), "backup_type_invalid", "Select a full or differential backup.")
    return _run_bundle(host, entry, backup_id, kind)


def reconcile_backup(host: Host, entry: dict, operation: str, database_label=None) -> dict:
    path = _operation_path(host, entry, operation)
    journal = read_json(_private(host, path))
    if database_label == "--abandon":
        require(not _receipt_path(host, entry, operation).exists(), "backup_operation_conflict", "A completed bundle cannot be abandoned.")
        journal["abandoned_at"] = now()
        _write_private(host, path, journal)
        _record(host, entry, "attempt", {"outcome": "unknown", "operation_id": operation, "abandoned_at": journal["abandoned_at"]})
        return {"operation_id": operation, "status": "abandoned", "outcome": "unknown"}
    require(not journal.get("abandoned_at"), "backup_abandoned", "Select a new operation UUID; the abandoned outcome remains unknown.")
    if journal.get("schedule_operation"):
        raise DeployError("backup_schedule_reconcile", "Reconcile this bundle's parent scheduled operation.")
    if journal["kind"] == "objects":
        with _repository_lock(host):
            require(journal["configuration_revision"] == entry["configuration_revision"],
                    "backup_operation_conflict", "The runtime revision changed; use a new operation.")
            _component(host, path, journal, "keys", lambda: archive_keys(host, entry), reconcile=True)
            return _component(host, path, journal, "objects", lambda: copy_objects(host, entry), reconcile=True)
    return _run_bundle(host, entry, operation, journal["kind"], reconcile=True, database_label=database_label)


def recovery_point(host: Host, entry: dict, operation_id: str) -> dict:
    """Use a recent cluster backup, then create and archive a named WAL point."""
    return _run_bundle(host, entry, operation_id, "recovery")


def _admin(host, cluster, sql, *, socket=None, database="postgres", port=None):
    return host.run(["runuser", "-u", postgres.cluster_owner(), "--", "psql", "-X", "-qAt", "-v", "ON_ERROR_STOP=1",
                     "-h", socket or cluster["socket_directory"], "-p", str(port or cluster["port"]),
                     "-U", postgres.cluster_owner(), "-d", database, "-f", "-"], input=sql, timeout=30)


def _recovery_limits(host, cluster):
    names = ("max_connections", "max_worker_processes", "max_wal_senders",
             "max_prepared_transactions", "max_locks_per_transaction")
    # Hot standby refuses replay when these limits are below the primary's.
    query = "SELECT json_build_object(" + ",".join("'" + name + "',current_setting('" + name + "')" for name in names) + ");\n"
    try:
        value = json.loads(_admin(host, cluster, query).stdout)
        require(set(value) == set(names) and all(str(value[name]).isdigit() and int(value[name]) <= 100000 for name in names),
                "backup_drill_limits", "Cannot determine the cluster's recovery resource limits.")
        return "".join(name + " = " + str(value[name]) + "\n" for name in names)
    except (ValueError, TypeError):
        raise DeployError("backup_drill_limits", "Cannot determine the cluster's recovery resource limits.") from None


def restore_drill(host: Host, entry: dict, backup_id: str, *, target_time: str | None = None,
                  target_inclusive: bool = True, retain: bool = False) -> dict:
    """Replay into a new Unix-socket-only cluster; never expose live credentials.

    Optional time targets and retained resources support the lead's PITR fixture.
    Default recovery uses the pre-migration named point, or the backup's first
    consistent point. This proves database recovery, not domain invariants or
    age decryption (the server deliberately has no recovery identities).
    """
    receipt = read_json(_private(host, _receipt_path(host, entry, backup_id)))
    require(receipt.get("instance_id") == entry["profile"]["instance_id"]
            and receipt.get("status") == "complete"
            and re.fullmatch(BACKUP_LABEL, receipt.get("pgbackrest_label", "")),
            "backup_incomplete", "Only a completed backup for this instance can be restored.")
    require(type(target_inclusive) is bool and type(retain) is bool,
            "backup_restore_target", "Invalid restore target options.")
    if target_time is not None:
        require(isinstance(target_time, str) and re.fullmatch(
            r"[0-9]{4}-[0-9]{2}-[0-9]{2}[ T][0-9]{2}:[0-9]{2}:[0-9]{2}(?:\.[0-9]{1,6})?(?:Z|[+-][0-9]{2}(?::?[0-9]{2})?)", target_time),
            "backup_restore_target", "Use an explicit PostgreSQL timestamp with timezone.")
    registry = host.read_registry()
    cluster = _cluster(host, registry)
    intent = {"backup_id": backup_id, "target_time": target_time, "target_inclusive": target_inclusive, "retain": retain}
    drill_id = uuid.uuid5(uuid.UUID(backup_id), json.dumps(intent, sort_keys=True)).hex
    journal_path = _private_root(host, entry) / "drills" / (drill_id + ".json")
    directory = check_path(host.paths.p(f"{postgres.CLUSTER_HOME}/drills/" + drill_id), "directory")
    data, socket = directory / "data", directory / "socket"
    unit = "foundry-restore-" + drill_id + ".service"
    unit_path = check_path(host.paths.p("/etc/systemd/system/" + unit), "file")
    restore_unit = "foundry-restore-" + drill_id + "-prepare.service"
    restore_unit_path = check_path(host.paths.p("/etc/systemd/system/" + restore_unit), "file")
    if journal_path.exists():
        result = read_json(_private(host, journal_path))
        require(result.get("intent") == intent and result.get("data_path") == str(data) and result.get("unit") == unit,
                "backup_drill_conflict", "Drill identity or resource ownership differs.")
        if result["phase"] == "complete":
            _record(host, entry, "restore_drill", result)
            return result
    else:
        require(not directory.exists() and not unit_path.exists() and not restore_unit_path.exists(), "backup_drill_conflict", "Drill resources already exist without an intent.")
        result = {"intent": intent, "backup_id": backup_id, "drill_id": drill_id, "started_at": now(),
                  "unit": unit, "data_path": str(data), "socket_path": str(socket), "unit_path": str(unit_path),
                  "restore_unit": restore_unit, "restore_unit_path": str(restore_unit_path),
                  "phase": "allocating", "status": "running", "retained": True}
        _write_private(host, journal_path, result)
    start = time.monotonic()
    def save_phase(phase):
        result["phase"] = phase
        _write_private(host, journal_path, result)
    def cleanup():
        save_phase("cleanup_pending")
        _stop_drill(host, unit)
        _stop_drill(host, restore_unit)
        for path in (unit_path, restore_unit_path):
            if path.exists():
                path.unlink()
        host.run(["systemctl", "daemon-reload"])
        if directory.exists():
            shutil.rmtree(directory)
        result.update(status="complete", retained=False)
        save_phase("complete")
    if result["phase"] in ("verified", "cleanup_pending"):
        if retain:
            result.update(status="complete", retained=True)
            save_phase("complete")
        else:
            cleanup()
        _record(host, entry, "restore_drill", result)
        return result
    try:
        if result["phase"] == "restoring":
            # Only this journal's isolated restore target can be discarded. The
            # service must be gone before retrying an interrupted restore command.
            _stop_drill(host, unit)
            _stop_drill(host, restore_unit)
            for path in (data, directory / "tablespaces"):
                check_path(path, "directory")
                if path.exists():
                    shutil.rmtree(path)
            save_phase("allocating")
        if result["phase"] == "allocating":
            limits = _recovery_limits(host, cluster)
            selected = [item for item in _info(host) if item["label"] == receipt["pgbackrest_label"]]
            require(len(selected) == 1,
                    "backup_expired", "The selected database backup is no longer retained in the repository.")
            size = selected[0].get("info", {}).get("size")
            require(type(size) is int and size > 0, "backup_drill_size", "Repository metadata must provide the restored database size.")
            directory.parent.mkdir(parents=True, exist_ok=True)
            require(shutil.disk_usage(directory.parent).free >= size * 2 + 256 * 1024 * 1024,
                    "backup_space", "Insufficient space for an isolated restore and WAL replay.")
            directory.mkdir(parents=True, mode=0o700, exist_ok=True)
            socket.mkdir(mode=0o700, exist_ok=True)
            if host.paths.root == Path("/"):
                for path in (directory, socket):
                    shutil.chown(path, user=postgres.cluster_owner(), group=postgres.cluster_owner())
        if result["phase"] == "allocating":
            options = ["--pg1-path=" + str(data), "--set=" + receipt["pgbackrest_label"],
                       "--target-action=pause", "--archive-mode=off", "--tablespace-map-all=" + str(directory / "tablespaces")]
            if target_time is not None:
                options += ["--type=time", "--target=" + target_time]
                if not target_inclusive:
                    options.append("--target-exclusive")
            elif receipt.get("restore_point"):
                require(re.fullmatch(r"foundry_[a-f0-9]{32}", receipt["restore_point"]),
                        "backup_restore_target", "Invalid named restore point.")
                options += ["--type=name", "--target=" + receipt["restore_point"]]
            else:
                options += ["--type=immediate"]
            save_phase("restoring")
            _restore_database(host, restore_unit, restore_unit_path, options)
            # Use a fresh configuration, not the restored cluster's includes, HBA,
            # preload libraries, archive command, listener or replication slots.
            configuration = ("listen_addresses = ''\nport = 55433\nssl = off\n"
                             "hot_standby = on\ndefault_transaction_read_only = on\n"
                             "archive_mode = off\narchive_command = ''\nshared_preload_libraries = ''\n"
                             "shared_buffers = '32MB'\n"
                             f"unix_socket_directories = '{socket}'\nunix_socket_permissions = 0700\n")
            configuration += limits
            host.owned_write(directory / "postgresql.conf", configuration, mode=0o600, owner=postgres.cluster_owner(), group=postgres.cluster_owner())
            host.owned_write(directory / "pg_hba.conf", f"local all {postgres.cluster_owner()} peer\nlocal all all reject\n",
                             mode=0o600, owner=postgres.cluster_owner(), group=postgres.cluster_owner())
            # auto.conf contains pgBackRest's recovery settings. Retain only recovery
            # directives; application ALTER SYSTEM settings must not escape isolation.
            auto = check_path(data / "postgresql.auto.conf", "file")
            recovery = []
            for line in auto.read_text().splitlines():
                setting = line.split("=", 1)[0].strip()
                if setting in {"restore_command", "recovery_target", "recovery_target_name", "recovery_target_time",
                               "recovery_target_inclusive", "recovery_target_action", "recovery_target_timeline"}:
                    recovery.append(line)
            require(any(line.strip().startswith("restore_command") for line in recovery),
                    "backup_drill_invalid", "Restored cluster has no WAL recovery command.")
            host.owned_write(auto, "\n".join(recovery) + "\n", mode=0o600, owner=postgres.cluster_owner(), group=postgres.cluster_owner())
            service = ("# Foundry isolated PITR drill\n[Unit]\nDescription=Foundry isolated PITR drill\n"
                       f"[Service]\nType=simple\nUser={postgres.cluster_owner()}\nGroup={postgres.cluster_owner()}\nUMask=0077\n"
                       f"ExecStart=/usr/lib/postgresql/{cluster['major']}/bin/postgres -D {data} "
                       f"-c config_file={directory}/postgresql.conf -c hba_file={directory}/pg_hba.conf\n"
                       "KillSignal=SIGINT\nTimeoutStopSec=120\nRestart=no\n")
            host.owned_write(unit_path, service)
            save_phase("start_pending")
        host.run(["systemctl", "daemon-reload"])
        # systemd start is idempotent: reattach to a start whose acknowledgement
        # was lost, using the identity persisted before allocation.
        host.run(["systemctl", "start", unit])
        save_phase("started")
        deadline = time.monotonic() + 180
        while True:
            try:
                probe = _admin(host, cluster, "SELECT pg_is_in_recovery() AND pg_is_wal_replay_paused();\n",
                               socket=str(socket), port=55433)
                if probe.stdout.strip() == "t":
                    break
            except DeployError:
                pass
            require(time.monotonic() < deadline, "backup_drill_target_unreached", "PITR did not reach and pause at the selected target within the drill deadline.")
            time.sleep(0.5)
        verify = _admin(host, cluster, "SELECT current_database();\nSELECT count(*) FROM pg_catalog.pg_class;\n",
                        socket=str(socket), port=55433, database=entry["allocations"]["database"])
        rows = verify.stdout.strip().splitlines()
        require(len(rows) == 2 and rows[0] == entry["allocations"]["database"] and rows[1].isdigit() and int(rows[1]) > 0,
                "backup_drill_verification", "The isolated database did not pass catalog verification.")
        result.update(status="complete", completed_at=now(), elapsed_seconds=round(time.monotonic() - start, 3),
                      verification={"recovery_paused_at_target": True, "database_catalog": True,
                                    "key_decryption": "operator_required", "domain_invariants": "application_required"},
                      retained=retain,
                      postgres={"host": str(socket), "port": 55433, "user": postgres.cluster_owner(),
                                "database": entry["allocations"]["database"], "sslmode": "disable"})
        save_phase("verified")
        if retain:
            save_phase("complete")
        else:
            cleanup()
        return result
    except BaseException:
        result.update(status="failed", retained=True)
        _write_private(host, journal_path, result)
        # Preserve the phase and owned paths even if proving exit fails.
        _stop_drill(host, unit)
        _stop_drill(host, restore_unit)
        raise
    finally:
        result.setdefault("elapsed_seconds", round(time.monotonic() - start, 3))
        _write_private(host, journal_path, result)
        _record(host, entry, "restore_drill", result)


def _restore_database(host, unit, path, options):
    arguments = ["/usr/bin/pgbackrest", "--config=" + str(host.paths.p(CONFIG)), "--stanza=" + STANZA, *options, "restore"]
    command = " ".join(json.dumps(value.replace("%", "%%").replace("$", "$$")) for value in arguments)
    service = ("# Foundry isolated PITR restore\n[Unit]\nDescription=Foundry isolated PITR restore\n"
               f"[Service]\nType=oneshot\nRemainAfterExit=yes\nUser={postgres.cluster_owner()}\n"
               f"Group={postgres.cluster_owner()}\nUMask=0077\nExecStart={command}\n"
               "KillMode=control-group\nTimeoutStartSec=1800\nTimeoutStopSec=120\n")
    _owned_public(host, path, service, "# Foundry isolated PITR restore")
    host.run(["systemctl", "daemon-reload"])
    host.run(["systemctl", "start", "--no-block", unit])
    deadline = time.monotonic() + 1800
    while True:
        result = host.run(["systemctl", "show", unit, "--property=ActiveState,SubState,Result,ExecMainStatus"])
        fields = dict(line.split("=", 1) for line in result.stdout.splitlines() if "=" in line)
        if fields.get("ActiveState") == "active" and fields.get("SubState") == "exited":
            require(fields.get("Result") == "success" and fields.get("ExecMainStatus") == "0",
                    "backup_drill_restore_failed", "The isolated restore command failed.")
            _stop_drill(host, unit)
            return
        require(fields.get("ActiveState") in ("activating", "inactive") and time.monotonic() < deadline,
                "backup_drill_restore_failed", "The isolated restore did not complete within its deadline.")
        time.sleep(.5)


def _stop_drill(host, unit):
    host.run(["systemctl", "stop", unit], timeout=150, check=False)
    observed = host.run(["systemctl", "show", unit, "--property=LoadState,ActiveState,MainPID,ControlGroup"], check=False)
    fields = dict(line.split("=", 1) for line in observed.stdout.splitlines() if "=" in line)
    require(fields.get("MainPID") == "0" and fields.get("ActiveState") in ("inactive", "failed"),
            "backup_drill_cleanup_pending", "Drill process exit is unproven; retain private resources.")
    group = fields.get("ControlGroup")
    require(group is not None, "backup_drill_cleanup_pending", "Drill cgroup ownership is unknown.")
    if group:
        require(group.startswith("/") and ".." not in Path(group).parts and Path(group).name == unit,
                "backup_drill_cleanup_pending", "Unexpected drill cgroup identity.")
        root = check_path(host.paths.p("/sys/fs/cgroup" + group), "directory")
        if root.exists():
            for processes in root.rglob("cgroup.procs"):
                require(not check_path(processes, "file").read_text().strip(),
                        "backup_drill_cleanup_pending", "Drill cgroup still contains processes.")


def _storage_sources(storage):
    bindings = storage_bindings(storage)
    credentials, disks = storage["credentials"], storage["disks"]
    credentials = json.loads(credentials) if isinstance(credentials, str) else credentials
    disks = json.loads(disks) if isinstance(disks, str) else disks
    sources = []
    for name, binding in bindings.items():
        disk = disks[name]
        cloud = disk["cloud"]
        credential = credentials[cloud["credentials"]]
        require(disk.get("config", {}).get("visibility") == 0 and credential.get("mode") == "static",
                "backup_storage_invalid", "Backup requires private cloud disks with explicit static credentials.")
        candidate = {**binding, "region": cloud.get("region"), "access_key_id": credential.get("access_key"),
                     "secret_access_key": credential.get("secret_key")}
        _repository(candidate)
        if candidate not in sources:
            sources.append(candidate)
    return sources


def _stage_settings(host, registry, entry, cluster):
    intake = read_json(_private(host, host.paths.instance_config(entry["name"]) / "bootstrap-secrets.json"))
    require(intake.get("instance_id") == entry["profile"]["instance_id"], "backup_ownership", "Private backup intake belongs to another instance.")
    repository = intake.get("backup_s3")
    _repository(repository)
    recipients = _recipients(intake.get("age_recipients"))
    storage, _ = _runtime_storage(host, entry)
    sources = _storage_sources(storage)
    require(all(source["bucket"] != repository["bucket"] for source in sources),
            "backup_bucket_conflict", "The attachment and backup buckets must be different.")
    record = {"instance_id": entry["profile"]["instance_id"], "age_recipients": recipients}
    existing_instance = _instance_settings_path(host, entry)
    if existing_instance.exists():
        require({key: value for key, value in read_json(_private(host, existing_instance)).items() if key != "source"} == record,
                "backup_configuration_conflict", "Backup settings already exist; use an explicit reviewed credential or destination change.")
    path = host.paths.p(REPOSITORY)
    if path.exists():
        existing = _repository_settings(host)
        require(all(existing.get(key) == value for key, value in repository.items())
                and existing.get("host_id") == registry["host_id"]
                and existing.get("cluster_token") == cluster["ownership_token"],
                "backup_repository_conflict", "All instances on the Foundry cluster must reuse its existing backup repository and encryption key.")
    else:
        require(not host.paths.p(CONFIG).exists() and not registry.get("shared", {}).get("backup"),
                "backup_repository_missing", "Restore the existing repository credentials and encryption key; never generate a replacement.")
        _write_private(host, path, {**repository, "cipher_pass": secrets.token_urlsafe(48),
                                    "host_id": registry["host_id"], "cluster_token": cluster["ownership_token"]})
    _write_private(host, existing_instance, record)


def _configuration(host, cluster, repository):
    endpoint = _repository(repository)
    require(endpoint.scheme == "https", "backup_repository_tls_required",
            "The WAL repository requires HTTPS; give the MinIO fixture a trusted TLS endpoint.")
    port = endpoint.port or 443
    return ("# Foundry owned pgBackRest repository\n[global]\n"
            "repo1-type=s3\nrepo1-path=/foundry\n"
            f"repo1-s3-endpoint={endpoint.hostname}\nrepo1-storage-port={port}\n"
            "repo1-storage-verify-tls=y\n"
            f"repo1-s3-bucket={repository['bucket']}\nrepo1-s3-region={repository['region']}\n"
            f"repo1-s3-uri-style={repository.get('uri_style', 'path')}\n"
            "repo1-cipher-type=aes-256-cbc\n"
            f"repo1-s3-key={repository['access_key_id']}\nrepo1-s3-key-secret={repository['secret_access_key']}\n"
            f"repo1-cipher-pass={repository['cipher_pass']}\n"
            "repo1-retention-full-type=time\n"
            f"repo1-retention-full={BACKUP_POLICY['full_retention_days']}\n"
            "repo1-retention-archive-type=full\n"
            "log-level-console=error\nlog-level-file=off\nstart-fast=y\narchive-timeout=120\n"
            f"lock-path={host.paths.p(WORK + '/lock')}\n"
            f"spool-path={host.paths.p(WORK + '/spool')}\n"
            f"[{STANZA}]\npg1-path={cluster['data_path']}\npg1-port={cluster['port']}\n"
            f"pg1-socket-path={cluster['socket_directory']}\npg1-user={postgres.cluster_owner()}\n")


def _owned_public(host, path, content, marker, mode=0o644):
    path = check_path(path, "file")
    if path.exists():
        info = path.stat()
        require(path.read_text().startswith(marker) and not info.st_mode & 0o022
                and (info.st_uid == 0 or host.paths.root != Path("/")),
                "backup_resource_conflict", "An existing backup control file has no matching Foundry ownership marker.")
    return host.owned_write(path, content, mode=mode)


def _install_timers(host, registry, entry):
    script = registry["tools"]["active_path"] + "/lib/backup.py"
    template = Path(__file__).resolve().parents[1] / "server/templates"
    timers = []
    for task, calendar, identity in (("full", "Sun *-*-* 02:00:00 UTC", "cluster"),
                                     ("diff", "*-*-* 05:00:00 UTC", "cluster"),
                                     ("objects", "*-*-* 08:00:00 UTC", entry["profile"]["instance_id"])):
        name = "foundry-backup-" + task + "-" + identity
        values = {"@TASK@": task, "@IDENTITY@": identity, "@SCRIPT@": script, "@CALENDAR@": calendar}
        for suffix in ("service", "timer"):
            content = (template / ("foundry-backup." + suffix)).read_text()
            for token, value in values.items():
                content = content.replace(token, value)
            _owned_public(host, host.paths.p("/etc/systemd/system/" + name + "." + suffix),
                                     content, "# Foundry owned backup")
        timers.append(name + ".timer")
    host.run(["systemctl", "daemon-reload"])
    for timer in timers:
        service = timer.removesuffix(".timer") + ".service"
        command = host.run(["systemctl", "show", service, "--property=ExecStart", "--value"])
        task, identity = service.removeprefix("foundry-backup-").removesuffix(".service").split("-", 1)
        require(f"/usr/bin/python3 -I {script} scheduled {task} {identity}" in command.stdout,
                "backup_rebind_failed", "The loaded backup service does not select the registered bundle.")
        timer_unit = host.run(["systemctl", "show", timer, "--property=Unit", "--value"])
        require(timer_unit.stdout.strip() == service, "backup_rebind_failed", "The backup timer selects an unexpected service.")
        host.run(["systemctl", "enable", "--now", timer])
        require(host.run(["systemctl", "is-enabled", timer], check=False).returncode == 0
                and host.run(["systemctl", "is-active", timer], check=False).returncode == 0,
                "backup_timer_failed", "Foundry backup timer is not enabled and active.")



def _cluster_owned_directory(host, path):
    """Create or verify a private directory owned by the dedicated cluster account."""
    directory = check_path(host.paths.p(path), "directory")
    if directory.exists():
        require(not directory.stat().st_mode & 0o077
                and (host.paths.root != Path("/") or directory.stat().st_uid == pwd.getpwnam(postgres.cluster_owner()).pw_uid),
                "backup_resource_conflict", "A pgBackRest directory has unexpected ownership or permissions.")
        return
    require(directory.parent.is_dir(), "backup_resource_conflict", "A pgBackRest directory parent is missing.")
    directory.mkdir(mode=0o700)
    if host.paths.root == Path("/"):
        shutil.chown(directory, user=postgres.cluster_owner(), group=postgres.cluster_owner())

def _install_configuration(host, cluster):
    path = check_path(host.paths.p(CONFIG), "file")
    if path.exists():
        require(path.read_text().startswith("# Foundry owned pgBackRest") and not path.stat().st_mode & 0o022,
                "backup_resource_conflict", "The repository config is not Foundry-owned.")
        require(host.paths.root != Path("/") or path.stat().st_uid in (0, pwd.getpwnam(postgres.cluster_owner()).pw_uid),
                "backup_resource_conflict", "The repository config has an unexpected owner.")
    host.owned_write(path, _configuration(host, cluster, _repository_settings(host)), mode=0o600,
                     owner=postgres.cluster_owner(), group=postgres.cluster_owner())


def _archive_command(registry):
    script = registry["tools"]["active_path"] + "/lib/backup.py"
    require(re.fullmatch(r"/usr/local/lib/foundry/tools/[a-f0-9]{64}/lib/backup\.py", script),
            "backup_tools_identity", "Use the immutable registered Foundry tool bundle for backup jobs.")
    return f"/usr/bin/python3 -I {script} archive-push %p"


def _remove_wal_bridge(host):
    sudoers = check_path(host.paths.p("/etc/sudoers.d/foundry-backup-archive"), "file")
    if sudoers.exists():
        require(sudoers.read_text().startswith("# Foundry owned WAL archive bridge"),
                "backup_resource_conflict", "The obsolete WAL bridge has unknown ownership.")
        sudoers.unlink()


def rebind(host, registry):
    """Converge every privileged entrypoint before an upgrade can complete."""
    if not registry.get("shared", {}).get("backup"):
        return
    cluster = _cluster(host, registry)
    _remove_wal_bridge(host)
    # Old registries did not record physical ownership. Import every existing
    # runtime binding together; overlapping legacy allocations refuse the upgrade.
    for entry in registry["instances"].values():
        if "storage_bindings" not in entry:
            values = runtime_config.validate_runtime_environment(host, entry)
            storage = runtime_storage(values, lambda path: runtime_config._read_value_file(host, entry, path))
            entry["storage_bindings"] = storage_bindings(storage)
    for entry in registry["instances"].values():
        check_storage_bindings(registry, entry, entry["storage_bindings"])
    host.write_registry(registry)
    _install_configuration(host, cluster)
    plan = postgres.cluster_settings(host, registry)
    plan["archive_command"] = _archive_command(registry)
    postgres.prepare(host, registry, shared_service_plan=plan)
    observed = _admin(host, cluster, "SHOW archive_command;\n").stdout.strip()
    require(observed == plan["archive_command"], "backup_rebind_failed", "The live WAL command does not select the new bundle.")
    for entry in registry["instances"].values():
        if _instance_settings_path(host, entry).exists():
            _install_timers(host, registry, entry)
    _backrest(host, ["check"], timeout=300)
    registry["shared"]["backup"] = {"stanza": STANZA, "pgbackrest_config": CONFIG, "owner": "foundry",
                                     "retention_full_type": "time", "retention_full_days": BACKUP_POLICY["full_retention_days"]}
    for entry in registry["instances"].values():
        if _instance_settings_path(host, entry).exists():
            _record(host, entry, "policy", dict(BACKUP_POLICY))
    host.write_registry(registry)


def configure(host: Host, registry: dict, entry: dict) -> None:
    """Install private repository config and directly owned WAL/schedule commands."""
    cluster = _cluster(host, registry)
    with _repository_lock(host):
        _remove_wal_bridge(host)
        values = runtime_config.validate_runtime_environment(host, entry)
        storage = runtime_storage(values, lambda path: runtime_config._read_value_file(host, entry, path))
        bind_storage(registry, entry, storage)
        host.journal(registry, entry)
        _stage_settings(host, registry, entry, cluster)
        packages.install_missing(host, ["pgbackrest", "age", "rclone"])
        for path in (CONFIG_DIR, WORK):
            _cluster_owned_directory(host, path)
        _install_configuration(host, cluster)
        _backrest(host, ["stanza-create"], timeout=300)
        registry["shared"]["backup"] = {"owner": "foundry"}
        rebind(host, registry)
        entry["backup"].update(destination_ref=CONFIG, policy=dict(BACKUP_POLICY))
        host.journal(registry, entry)


def _wal_pattern(cluster):
    # Accept only PostgreSQL WAL, timeline and backup-history filenames.
    return (r"(pg_wal/|" + cluster["data_path"] + r"/pg_wal/)"
            r"([0-9A-F]{24}|[0-9A-F]{8}[.]history|[0-9A-F]{24}[.][0-9A-F]{8}[.]backup)")


def archive_push(host: Host, wal: str) -> None:
    """Run directly as the dedicated cluster owner, without a sudo bridge."""
    path = check_path(host.paths.p(CONFIG), "file")
    info = path.stat()
    require(stat.S_IMODE(info.st_mode) == 0o600 and (host.paths.root != Path("/")
            or info.st_uid == os.geteuid() == pwd.getpwnam(postgres.cluster_owner()).pw_uid),
            "backup_private_permissions", "WAL archival requires the dedicated cluster owner and private config.")
    config = configparser.ConfigParser(interpolation=None)
    config.read(path)
    data = config.get(STANZA, "pg1-path")
    require(re.fullmatch(re.escape(postgres.CLUSTER_HOME) + r"/(16|18)/foundry", data),
            "backup_cluster_ownership", "WAL archival requires the dedicated Foundry cluster.")
    require(isinstance(wal, str) and re.fullmatch(_wal_pattern({"data_path": data}), wal),
            "backup_wal_path", "WAL archival accepts only an allocated cluster WAL filename.")
    canonical = wal if wal.startswith("/") else data + "/" + wal
    source = check_path(host.paths.p(canonical), "file")
    require(source.exists() and source.stat().st_size <= 1024 * 1024 * 1024,
            "backup_wal_path", "WAL input is absent or exceeds its bound.")
    host.run(["pgbackrest", "--config=" + str(path), "--stanza=" + STANZA, "archive-push", str(source)], timeout=300)


def _schedule_path(host, operation):
    require(valid_uuid(operation), "backup_id_invalid", "Select a scheduled operation UUID.")
    return host.paths.p("/etc/foundry/backup/operations/" + operation + ".json")


def _scheduled_bundle(host, registry, operation, kind, *, reconcile=False, database_label=None):
    path = _schedule_path(host, operation)
    journal = _database_phase(host, path, operation, kind, reconcile=reconcile, database_label=database_label)
    if "instances" not in journal:
        journal["instances"] = {identity: str(uuid.uuid4()) for identity, entry in registry["instances"].items()
                                if _instance_settings_path(host, entry).exists()}
        _write_private(host, path, journal)
    failed = False
    for identity, bundle_id in journal["instances"].items():
        entry = registry["instances"][identity]
        with host.lock(identity):
            try:
                with _attempt(host, entry):
                    if not _receipt_path(host, entry, bundle_id).exists():
                        # Bind the child to its parent for explicit reconciliation.
                        child = _operation_path(host, entry, bundle_id)
                        if not child.exists():
                            _write_private(host, child, {"operation_id": bundle_id, "schedule_operation": operation,
                                "configuration_revision": entry["configuration_revision"], "components": {}})
                        _complete(host, entry, bundle_id, journal["components"]["database"], reconcile=reconcile)
                    else:
                        _record(host, entry, "database", read_json(_private(host, _receipt_path(host, entry, bundle_id))))
            except (DeployError, OSError):
                failed = True
            host.journal(registry, entry)
    # Expiration is cluster-wide and time-based, independent of bundle counts.
    _backrest(host, ["expire"], timeout=300)
    journal.update(status="incomplete" if failed else "complete", updated_at=now())
    _write_private(host, path, journal)
    require(not failed, "backup_bundle_incomplete", "One or more instance bundles need explicit reconciliation; later schedules remain enabled.")


def reconcile_scheduled(host, operation, database_label=None):
    with host.lock("host"), _repository_lock(host):
        registry = host.read_registry()
        journal = read_json(_private(host, _schedule_path(host, operation)))
        if database_label == "--abandon":
            require(journal.get("status") != "complete", "backup_operation_conflict", "A completed schedule cannot be abandoned.")
            journal["abandoned_at"] = now()
            _write_private(host, _schedule_path(host, operation), journal)
            return
        _scheduled_bundle(host, registry, operation, journal["kind"], reconcile=True, database_label=database_label)


def _scheduled(host, task, identity):
    require(task in ("full", "diff", "objects") and (identity == "cluster" or valid_uuid(identity)),
            "backup_schedule_invalid", "Unknown Foundry backup schedule.")
    with host.lock("host"):
        registry = host.read_registry()
        if task == "objects":
            require(identity in registry["instances"], "backup_schedule_invalid", "Unknown backup instance UUID.")
            entry = registry["instances"][identity]
            with host.lock(identity), _repository_lock(host), _attempt(host, entry):
                operation = str(uuid.uuid4())
                path = _operation_path(host, entry, operation)
                journal = {"operation_id": operation, "kind": "objects", "configuration_revision": entry["configuration_revision"], "components": {}}
                _write_private(host, path, journal)
                _component(host, path, journal, "keys", lambda: archive_keys(host, entry))
                _component(host, path, journal, "objects", lambda: copy_objects(host, entry))
                host.journal(registry, entry)
        else:
            require(identity == "cluster", "backup_schedule_invalid", "Database schedules belong to the Foundry cluster.")
            _cluster(host, registry)
            with _repository_lock(host):
                # Every calendar invocation gets its own identity. Unresolved older
                # operations retain their evidence and never disable this schedule.
                _scheduled_bundle(host, registry, str(uuid.uuid4()), task)


def _main(arguments):
    host = Host()
    if len(arguments) == 2 and arguments[0] == "archive-push":
        archive_push(host, arguments[1])
    elif len(arguments) == 3 and arguments[0] == "scheduled":
        require(os.geteuid() == 0, "root_required", "Backup control operations require root.")
        _scheduled(host, arguments[1], arguments[2])
    elif arguments == ["rebind"]:
        require(os.geteuid() == 0, "root_required", "Backup control operations require root.")
        with _repository_lock(host):
            registry = host.read_registry()
            rebind(host, registry)
            host.write_registry(registry)
    elif len(arguments) in (2, 3) and arguments[0] == "reconcile-scheduled":
        require(os.geteuid() == 0, "root_required", "Backup control operations require root.")
        reconcile_scheduled(host, arguments[1], arguments[2] if len(arguments) == 3 else None)
    elif len(arguments) in (3, 4) and arguments[0] == "reconcile":
        require(os.geteuid() == 0, "root_required", "Backup control operations require root.")
        identity, operation = arguments[1:3]
        require(valid_uuid(identity), "backup_id_invalid", "Select an instance UUID.")
        with host.lock("host"), host.lock(identity):
            registry = host.read_registry()
            require(identity in registry["instances"], "backup_ownership", "Unknown backup instance.")
            entry = registry["instances"][identity]
            reconcile_backup(host, entry, operation, arguments[3] if len(arguments) == 4 else None)
            host.journal(registry, entry)
    else:
        raise DeployError("backup_command_invalid", "Unknown backup control operation.")


if __name__ == "__main__":
    try:
        _main(sys.argv[1:])
    except (DeployError, OSError, ValueError, KeyError, configparser.Error):
        # WAL archiver/systemd diagnostics must never print private command
        # output, config, exception reprs or request payloads.
        sys.stderr.write("Foundry backup operation failed; inspect private configuration and operation state.\n")
        sys.exit(1)
