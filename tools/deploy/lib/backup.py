"""pgBackRest PITR and age-encrypted instance recovery material.

The repository belongs to the Foundry cluster, not one application. Public state
contains completion receipts only. Root-owned intake files hold credentials;
no secret, key, or secret-derived digest is included in a command argument.
"""
from __future__ import annotations

from contextlib import contextmanager
import fcntl
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

from contracts import (DeployError, POSTGRES_PORT, check_path, instance_paths,
                       now, parse_env, read_json, require, valid_uuid)
from host import Host
import packages
import postgres

STANZA = "foundry"
CONFIG = "/etc/pgbackrest/foundry.conf"
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
    _repository(value.get("source"))
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
            and cluster.get("port") == observed.get("port") == POSTGRES_PORT
            and cluster.get("ownership_token") and cluster["ownership_token"] == observed.get("ownership_token")
            and cluster.get("major") in (16, 18)
            and cluster.get("unit") == f"postgresql@{cluster.get('major')}-foundry.service",
            "backup_cluster_ownership", "Only the verified Foundry PostgreSQL cluster may be backed up.")
    data = cluster.get("data_path")
    require(data == f"/var/lib/postgresql/{cluster['major']}/foundry",
            "backup_cluster_ownership", "Foundry data directory must match its dedicated cluster allocation.")
    return cluster


def _pg_env(host):
    settings = _repository_settings(host)
    return {"PGBACKREST_REPO1_S3_KEY": settings["access_key_id"],
            "PGBACKREST_REPO1_S3_KEY_SECRET": settings["secret_access_key"],
            "PGBACKREST_REPO1_CIPHER_PASS": settings["cipher_pass"]}


def _backrest(host, arguments, *, timeout=1800):
    return host.run(["runuser", "-u", "postgres", "--", "pgbackrest",
                     "--config=" + str(host.paths.p(CONFIG)), "--stanza=" + STANZA, *arguments],
                    env=_pg_env(host), timeout=timeout)


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
        value.update(last_complete_id=receipt["backup_id"], last_complete_at=receipt["completed_at"],
                     last_attempt_outcome="complete")
    elif kind == "restore_drill":
        value.update(restore_drill_outcome=receipt["status"], last_restore_drill_at=receipt.get("completed_at"))
    elif kind == "attempt":
        value["last_attempt_outcome"] = receipt["outcome"]
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


def _rclone_environment(repository, name):
    _repository(repository)
    prefix = "RCLONE_CONFIG_" + name.upper() + "_"
    return {prefix + "TYPE": "s3", prefix + "PROVIDER": "Other", prefix + "ENV_AUTH": "false",
            prefix + "ENDPOINT": repository["endpoint"], prefix + "REGION": repository["region"],
            prefix + "ACCESS_KEY_ID": repository["access_key_id"], prefix + "SECRET_ACCESS_KEY": repository["secret_access_key"],
            prefix + "FORCE_PATH_STYLE": str(repository.get("uri_style", "path") == "path").lower()}


def _rclone(host, arguments, source=None):
    environment = _rclone_environment(_repository_settings(host), "backup")
    if source:
        environment.update(_rclone_environment(source, "app"))
    return host.run(["rclone", "--config=/dev/null", "--retries=1", "--low-level-retries=1", *arguments],
                    env=environment, timeout=1800)


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


def copy_objects(host: Host, entry: dict) -> dict:
    """Copy, never sync: deleted source objects remain recoverable in backups."""
    settings, repository = _settings(host, entry), _repository_settings(host)
    source = settings["source"]
    require((source["endpoint"].rstrip("/"), source["bucket"]) !=
            (repository["endpoint"].rstrip("/"), repository["bucket"]),
            "backup_bucket_conflict", "Attachment and backup buckets must be distinct.")
    prefix = f"objects/{entry['profile']['instance_id']}"
    origin, destination = f"app:{source['bucket']}", f"backup:{repository['bucket']}/{prefix}"
    history = f"backup:{repository['bucket']}/object-history/{entry['profile']['instance_id']}/{uuid.uuid4().hex}"
    _rclone(host, ["copy", origin, destination, "--backup-dir", history], source)
    _rclone(host, ["check", origin, destination, "--one-way", "--download"], source)
    receipt = {"objects_prefix": prefix, "completed_at": now(), "status": "complete"}
    _record(host, entry, "objects", receipt)
    return receipt


def _receipt_path(host, entry, backup_id):
    require(valid_uuid(backup_id), "backup_id_invalid", "Select an explicit completed backup UUID.")
    return _private_root(host, entry) / "complete" / (backup_id + ".json")


def _complete(host, entry, backup_id, label, *, restore_point=None):
    keys = archive_keys(host, entry)
    objects = copy_objects(host, entry)
    receipt = {"backup_id": backup_id, "pgbackrest_label": label, "instance_id": entry["profile"]["instance_id"],
               "key_archive": keys, "objects_prefix": objects["objects_prefix"], "completed_at": now(), "status": "complete"}
    if restore_point:
        receipt["restore_point"] = restore_point
    _write_private(host, _receipt_path(host, entry, backup_id), receipt)
    _record(host, entry, "database", receipt)
    return receipt


def run_backup(host: Host, entry: dict, backup_id: str, kind: str = "full") -> dict:
    """F1 manual backup entrypoint; caller holds the instance operation lock."""
    complete = _receipt_path(host, entry, backup_id)
    if complete.exists():
        return read_json(_private(host, complete))
    with _repository_lock(host):
        journal = _private_root(host, entry) / (backup_id + ".json")
        require(not journal.exists(), "backup_outcome_unknown", "This operation was interrupted; inspect its repository outcome before starting a new operation.")
        with _attempt(host, entry):
            _write_private(host, journal, {"backup_id": backup_id, "status": "running", "started_at": now()})
            label = _database_backup(host, kind)
            _write_private(host, journal, {"backup_id": backup_id, "status": "database_complete", "pgbackrest_label": label})
            receipt = _complete(host, entry, backup_id, label)
            _backrest(host, ["expire"], timeout=300)
            return receipt


def recovery_point(host: Host, entry: dict, operation_id: str) -> dict:
    """Finish the base backup BEFORE the named point, then force its WAL off-host."""
    complete = _receipt_path(host, entry, operation_id)
    if complete.exists():
        receipt = read_json(_private(host, complete))
        require(receipt.get("restore_point"), "backup_operation_conflict", "This operation UUID belongs to an ordinary backup.")
        return receipt
    with _repository_lock(host):
        registry = host.read_registry()
        cluster = _cluster(host, registry)
        journal = _private_root(host, entry) / (operation_id + ".json")
        require(not journal.exists(), "backup_outcome_unknown", "Recovery point outcome is uncertain; inspect the repository and use a new operation after reconciliation.")
        with _attempt(host, entry):
            _write_private(host, journal, {"backup_id": operation_id, "status": "running", "started_at": now()})
            label = _database_backup(host, "full")
            point = "foundry_" + uuid.UUID(operation_id).hex
            result = _admin(host, cluster, f"SELECT pg_create_restore_point('{point}');\nSELECT pg_switch_wal();\n")
            require(bool(result.stdout.strip()), "backup_restore_point_failed", "PostgreSQL did not confirm the named restore point.")
            _backrest(host, ["check"], timeout=300)
            _write_private(host, journal, {"backup_id": operation_id, "status": "database_complete",
                                          "pgbackrest_label": label, "restore_point": point})
            receipt = _complete(host, entry, operation_id, label, restore_point=point)
            _backrest(host, ["expire"], timeout=300)
            return receipt


def _admin(host, cluster, sql, *, socket=None, database="postgres", port=None):
    return host.run(["runuser", "-u", "postgres", "--", "psql", "-X", "-qAt", "-v", "ON_ERROR_STOP=1",
                     "-h", socket or cluster["socket_directory"], "-p", str(port or cluster["port"]),
                     "-d", database, "-f", "-"], input=sql, timeout=30)


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
    limits = _recovery_limits(host, cluster)
    selected = [item for item in _info(host) if item["label"] == receipt["pgbackrest_label"]]
    require(len(selected) == 1,
            "backup_expired", "The selected database backup is no longer retained in the repository.")
    size = selected[0].get("info", {}).get("size")
    require(type(size) is int and size > 0, "backup_drill_size", "Repository metadata must provide the restored database size.")
    drill_id = uuid.uuid4().hex
    directory = host.paths.p("/var/lib/postgresql/foundry-drills/" + drill_id)
    check_path(directory, "directory")
    require(not directory.exists(), "backup_drill_conflict", "Drill directory already exists.")
    directory.parent.mkdir(parents=True, exist_ok=True)
    require(shutil.disk_usage(directory.parent).free >= size * 2 + 256 * 1024 * 1024,
            "backup_space", "Insufficient space on the drill filesystem for an isolated restore and WAL replay.")
    # New, owned directory: no existing cluster, tablespace or data is replaced.
    directory.mkdir(parents=True, mode=0o700)
    if host.paths.root == Path("/"):
        shutil.chown(directory, user="postgres", group="postgres")
    data, socket = directory / "data", directory / "socket"
    socket.mkdir(mode=0o700)
    if host.paths.root == Path("/"):
        shutil.chown(socket, user="postgres", group="postgres")
    unit = "foundry-restore-" + drill_id + ".service"
    unit_path = host.paths.p("/etc/systemd/system/" + unit)
    environment = _private_root(host, entry) / ("drill-" + drill_id + ".env")
    env_text = "\n".join(key + "=" + json.dumps(value) for key, value in _pg_env(host).items()) + "\n"
    host.owned_write(environment, env_text, mode=0o600)
    start, started_at = time.monotonic(), now()
    started = False
    result = {"backup_id": backup_id, "drill_id": drill_id, "started_at": started_at,
              "unit": unit, "data_path": str(data), "status": "failed"}
    try:
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
        _backrest(host, [*options, "restore"])
        # Use a fresh configuration, not the restored cluster's includes, HBA,
        # preload libraries, archive command, listener or replication slots.
        configuration = ("listen_addresses = ''\nport = 55433\nssl = off\n"
                         "hot_standby = on\ndefault_transaction_read_only = on\n"
                         "archive_mode = off\narchive_command = ''\nshared_preload_libraries = ''\n"
                         "shared_buffers = '32MB'\n"
                         f"unix_socket_directories = '{socket}'\nunix_socket_permissions = 0700\n")
        configuration += limits
        host.owned_write(directory / "postgresql.conf", configuration, mode=0o600, owner="postgres", group="postgres")
        host.owned_write(directory / "pg_hba.conf", "local all postgres peer\nlocal all all reject\n",
                         mode=0o600, owner="postgres", group="postgres")
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
        host.owned_write(auto, "\n".join(recovery) + "\n", mode=0o600, owner="postgres", group="postgres")
        service = ("# Foundry isolated PITR drill\n[Unit]\nDescription=Foundry isolated PITR drill\n"
                   "[Service]\nType=simple\nUser=postgres\nGroup=postgres\nUMask=0077\n"
                   f"EnvironmentFile={environment}\n"
                   f"ExecStart=/usr/lib/postgresql/{cluster['major']}/bin/postgres -D {data} "
                   f"-c config_file={directory}/postgresql.conf -c hba_file={directory}/pg_hba.conf\n"
                   "KillSignal=SIGINT\nTimeoutStopSec=120\nRestart=no\n")
        require(not unit_path.exists(), "backup_drill_conflict", "Drill service already exists.")
        host.owned_write(unit_path, service)
        host.run(["systemctl", "daemon-reload"])
        started = True
        host.run(["systemctl", "start", unit])
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
                      postgres={"host": str(socket), "port": 55433, "user": "postgres",
                                "database": entry["allocations"]["database"], "sslmode": "disable"})
        return result
    finally:
        try:
            if result["status"] != "complete":
                # Host.run cannot prove that a timed-out runuser child exited.
                # Keep failed restores rather than unlink a surviving writer.
                result["retained"] = True
                if started:
                    host.run(["systemctl", "stop", unit], timeout=150)
            elif not retain:
                if started:
                    host.run(["systemctl", "stop", unit], timeout=150)
                    stopped = host.run(["systemctl", "show", unit, "--property=MainPID", "--value"])
                    require(stopped.stdout.strip() == "0", "backup_drill_cleanup_pending", "Drill process exit is unproven; retain its private data for reconciliation.")
                if unit_path.exists():
                    unit_path.unlink()
                    host.run(["systemctl", "daemon-reload"])
                environment.unlink(missing_ok=True)
                shutil.rmtree(directory)
        except BaseException:
            result.update(status="failed", retained=True, cleanup_status="unknown")
            raise
        finally:
            result.setdefault("elapsed_seconds", round(time.monotonic() - start, 3))
            _record(host, entry, "restore_drill", result)


def _storage_source(storage):
    require(isinstance(storage, dict), "backup_storage_missing", "Supply app_storage credentials and disks through private stdin intake.")
    try:
        credentials = storage["credentials"]
        disks = storage["disks"]
        credentials = json.loads(credentials) if isinstance(credentials, str) else credentials
        disks = json.loads(disks) if isinstance(disks, str) else disks
        require(isinstance(credentials, dict) and isinstance(disks, dict) and disks,
                "backup_storage_invalid", "App storage credentials and disks must be objects.")
        source = None
        for disk in disks.values():
            require(disk.get("driver") in ("r2", "s3", "s3_compatible") and disk.get("config", {}).get("visibility") == 0,
                    "backup_storage_invalid", "Backup requires private cloud disks in one explicit attachment bucket.")
            cloud = disk["cloud"]
            credential = credentials[cloud["credentials"]]
            require(credential.get("mode") == "static", "backup_storage_invalid", "Object copying requires explicit static bucket credentials.")
            candidate = {"endpoint": cloud["endpoint"], "region": cloud["region"], "bucket": cloud["bucket"],
                         "access_key_id": credential["access_key"], "secret_access_key": credential["secret_key"]}
            _repository(candidate)
            require(source is None or source == candidate,
                    "backup_storage_invalid", "All attachment disks must share one bucket and credential binding.")
            source = candidate
        return source
    except (KeyError, TypeError, ValueError, AttributeError):
        raise DeployError("backup_storage_invalid", "App storage configuration does not expose a valid attachment bucket.") from None


def _stage_settings(host, registry, entry, cluster):
    intake = read_json(_private(host, host.paths.instance_config(entry["name"]) / "bootstrap-secrets.json"))
    require(intake.get("instance_id") == entry["profile"]["instance_id"], "backup_ownership", "Private backup intake belongs to another instance.")
    repository = intake.get("backup_s3")
    _repository(repository)
    recipients = _recipients(intake.get("age_recipients"))
    source = _storage_source(intake.get("app_storage"))
    require((source["endpoint"].rstrip("/"), source["bucket"]) !=
            (repository["endpoint"].rstrip("/"), repository["bucket"]),
            "backup_bucket_conflict", "The attachment and backup buckets must be different.")
    record = {"instance_id": entry["profile"]["instance_id"], "source": source, "age_recipients": recipients}
    existing_instance = _instance_settings_path(host, entry)
    if existing_instance.exists():
        require(read_json(_private(host, existing_instance)) == record,
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
    if not existing_instance.exists():
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
            "repo1-cipher-type=aes-256-cbc\nrepo1-retention-full=4\nrepo1-retention-diff=7\n"
            "repo1-retention-archive-type=full\nrepo1-retention-archive=4\n"
            "log-level-console=error\nlog-level-file=off\nstart-fast=y\narchive-timeout=120\n"
            f"lock-path={host.paths.p('/var/lib/pgbackrest/foundry/lock')}\n"
            f"spool-path={host.paths.p('/var/lib/pgbackrest/foundry/spool')}\n"
            f"[{STANZA}]\npg1-path={cluster['data_path']}\npg1-port={cluster['port']}\n"
            f"pg1-socket-path={cluster['socket_directory']}\npg1-user=postgres\n")


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
    changed = False
    for task, calendar, identity in (("full", "Sun *-*-* 02:00:00 UTC", "cluster"),
                                     ("diff", "*-*-* 05:00:00 UTC", "cluster"),
                                     ("objects", "*-*-* 08:00:00 UTC", entry["profile"]["instance_id"])):
        name = "foundry-backup-" + task + "-" + identity
        values = {"@TASK@": task, "@IDENTITY@": identity, "@SCRIPT@": script, "@CALENDAR@": calendar}
        for suffix in ("service", "timer"):
            content = (template / ("foundry-backup." + suffix)).read_text()
            for token, value in values.items():
                content = content.replace(token, value)
            changed |= _owned_public(host, host.paths.p("/etc/systemd/system/" + name + "." + suffix),
                                     content, "# Foundry owned backup")
        timers.append(name + ".timer")
    if changed:
        host.run(["systemctl", "daemon-reload"])
    for timer in timers:
        host.run(["systemctl", "enable", "--now", timer])
        require(host.run(["systemctl", "is-enabled", timer], check=False).returncode == 0
                and host.run(["systemctl", "is-active", timer], check=False).returncode == 0,
                "backup_timer_failed", "Foundry backup timer is not enabled and active.")


def configure(host: Host, registry: dict, entry: dict) -> None:
    """Install the cluster repository, restricted WAL bridge and owned schedules.

    Called under F1's host/instance locks. F2 owns the PostgreSQL configuration
    write and reload, including its archive-transition journal.
    """
    cluster = _cluster(host, registry)
    with _repository_lock(host):
        _stage_settings(host, registry, entry, cluster)
        repository = _repository_settings(host)
        content = _configuration(host, cluster, repository)
        packages.install_missing(host, ["pgbackrest", "age", "rclone"])
        # sudo is part of the host administration prerequisite, not a new
        # unreviewed package installation. Only this exact bridge is allowed.
        require(host.run(["test", "-x", "/usr/bin/sudo"], check=False).returncode == 0,
                "backup_archive_bridge_missing", "The restricted WAL bridge requires the host's existing sudo installation.")
        directory = host.paths.p("/var/lib/pgbackrest/foundry")
        check_path(directory, "directory")
        if directory.exists():
            require(not directory.stat().st_mode & 0o077
                    and (host.paths.root != Path("/") or directory.stat().st_uid == pwd.getpwnam("postgres").pw_uid),
                    "backup_resource_conflict", "The existing pgBackRest work directory has unexpected ownership or permissions.")
        else:
            directory.mkdir(parents=True, mode=0o700)
            if host.paths.root == Path("/"):
                shutil.chown(directory, user="postgres", group="postgres")
        _owned_public(host, host.paths.p(CONFIG), content, "# Foundry owned pgBackRest")
        script = registry["tools"]["active_path"] + "/lib/backup.py"
        require(re.fullmatch(r"/usr/local/lib/foundry/tools/[a-f0-9]{64}/lib/backup\.py", script),
                "backup_tools_identity", "Use the immutable registered Foundry tool bundle for backup jobs.")
        bridge = ("# Foundry owned WAL archive bridge\n"
                  f"postgres ALL=(root) NOPASSWD: /usr/bin/python3 -I {script} archive-push *\n")
        sudoers = host.paths.p("/etc/sudoers.d/foundry-backup-archive")
        # Validate a root-only candidate before making the privilege rule live.
        candidate = host.paths.p("/etc/foundry/backup/archive-sudoers.candidate")
        host.owned_write(candidate, bridge, mode=0o600)
        try:
            host.run(["visudo", "-c", "-f", str(candidate)])
            _owned_public(host, sudoers, bridge, "# Foundry owned WAL archive bridge", mode=0o440)
        finally:
            candidate.unlink(missing_ok=True)
        _backrest(host, ["stanza-create"], timeout=300)
        plan = postgres.cluster_settings(host, registry)
        plan["archive_command"] = f"/usr/bin/sudo -n /usr/bin/python3 -I {script} archive-push %p"
        postgres.prepare(host, registry, shared_service_plan=plan)
        _backrest(host, ["check"], timeout=300)
        _install_timers(host, registry, entry)
        registry["shared"]["backup"] = {"stanza": STANZA, "pgbackrest_config": CONFIG,
                                         "owner": "foundry", "retention_full": 4, "retention_diff": 7}
        entry["backup"].update(destination_ref=CONFIG, policy={"interval_hours": 24, "daily": 7, "weekly": 4, "monthly": 0})
        host.journal(registry, entry)


def archive_push(host: Host, wal: str) -> None:
    """The only privileged archive entrypoint: upload one owned WAL file.

    Never accept pgBackRest options, alternate config paths, arbitrary files,
    shell input or a request envelope from the PostgreSQL account.
    """
    registry = host.read_registry()
    cluster = registry.get("shared", {}).get("postgresql", {})
    require(cluster.get("owner") == "foundry" and cluster.get("cluster") == "foundry"
            and cluster.get("port") == POSTGRES_PORT and cluster.get("major") in (16, 18)
            and cluster.get("data_path") == f"/var/lib/postgresql/{cluster.get('major')}/foundry",
            "backup_cluster_ownership", "WAL bridge requires the registered Foundry cluster.")
    repository = _repository_settings(host)
    require(repository.get("host_id") == registry["host_id"] and repository.get("cluster_token") == cluster.get("ownership_token"),
            "backup_cluster_ownership", "WAL repository identity does not match the registered cluster.")
    require(isinstance(wal, str) and re.fullmatch(
        r"(?:pg_wal/|" + re.escape(cluster["data_path"] + "/pg_wal/") + r")"
        r"(?:[0-9A-F]{24}|[0-9A-F]{8}\.history|[0-9A-F]{24}\.[0-9A-F]{8}\.backup)", wal),
        "backup_wal_path", "WAL bridge accepts only an allocated cluster WAL filename.")
    canonical = wal if wal.startswith("/") else cluster["data_path"] + "/" + wal
    path = check_path(host.paths.p(canonical), "file")
    require(path.exists() and path.stat().st_size <= 1024 * 1024 * 1024,
            "backup_wal_path", "WAL input is absent or exceeds its bound.")
    _backrest(host, ["archive-push", str(path)], timeout=300)


def _scheduled(host, task, identity):
    require(task in ("full", "diff", "objects") and (identity == "cluster" or valid_uuid(identity)),
            "backup_schedule_invalid", "Unknown Foundry backup schedule.")
    with host.lock("host"):
        registry = host.read_registry()
        if task == "objects":
            require(identity in registry["instances"], "backup_schedule_invalid", "Unknown backup instance UUID.")
            entry = registry["instances"][identity]
            with host.lock(identity):
                archive_keys(host, entry)
                copy_objects(host, entry)
                host.journal(registry, entry)
        else:
            require(identity == "cluster", "backup_schedule_invalid", "Database schedules belong to the Foundry cluster.")
            _cluster(host, registry)
            with _repository_lock(host):
                journal = host.paths.p("/etc/foundry/backup/scheduled.json")
                if journal.exists():
                    require(read_json(_private(host, journal)).get("status") == "complete",
                            "backup_outcome_unknown", "The prior scheduled backup is unresolved; inspect it before resuming the schedule.")
                operation = str(uuid.uuid4())
                _write_private(host, journal, {"operation_id": operation, "status": "running", "started_at": now()})
                kind = task
                if kind == "diff" and not _info(host):
                    kind = "full"
                label = _database_backup(host, kind)
                _write_private(host, journal, {"operation_id": operation, "status": "database_complete", "pgbackrest_label": label})
                for entry in registry["instances"].values():
                    if not _instance_settings_path(host, entry).exists():
                        continue
                    with host.lock(entry["profile"]["instance_id"]):
                        _complete(host, entry, str(uuid.uuid4()), label)
                        host.journal(registry, entry)
                _backrest(host, ["expire"], timeout=300)
                _write_private(host, journal, {"operation_id": operation, "status": "complete", "completed_at": now(), "pgbackrest_label": label})


def _main(arguments):
    require(os.geteuid() == 0, "root_required", "Backup control operations require root.")
    host = Host()
    if len(arguments) == 2 and arguments[0] == "archive-push":
        archive_push(host, arguments[1])
    elif len(arguments) == 3 and arguments[0] == "scheduled":
        _scheduled(host, arguments[1], arguments[2])
    else:
        raise DeployError("backup_command_invalid", "Unknown backup control operation.")


if __name__ == "__main__":
    try:
        _main(sys.argv[1:])
    except (DeployError, OSError, ValueError, KeyError):
        # WAL archiver/systemd diagnostics must never print private command
        # output, config, exception reprs or request payloads.
        sys.stderr.write("Foundry backup operation failed; inspect private configuration and operation state.\n")
        sys.exit(1)
