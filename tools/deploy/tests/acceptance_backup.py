#!/usr/bin/env python3
"""Real pgBackRest PITR, age recovery and rclone copy against an isolated S3 stand-in.

Required environment: ACCEPTANCE_S3_ENDPOINT, ACCEPTANCE_S3_ACCESS_KEY,
ACCEPTANCE_S3_SECRET_KEY, ACCEPTANCE_APP_BUCKET, ACCEPTANCE_BACKUP_BUCKET.
Credentials and decrypted archives remain private inside the disposable host.
"""
import json
import os
from pathlib import Path
import tarfile
from urllib.parse import urlparse
import uuid

from acceptance_support import (WORK, action, fixture, guard, postgres_settings,
                                psql, request, run, service, wait_for)


def s3_settings():
    names = ("ENDPOINT", "ACCESS_KEY", "SECRET_KEY", "APP_BUCKET", "BACKUP_BUCKET")
    values = {key.lower(): os.environ["ACCEPTANCE_" + ("S3_" if key in names[:3] else "") + key]
              for key in names}
    endpoint = urlparse(values["endpoint"])
    assert endpoint.scheme == "https" and endpoint.hostname
    assert not endpoint.username and not endpoint.password
    assert values["app_bucket"] != values["backup_bucket"]
    return values


def rclone(settings, *args, timeout=600):
    return run(["rclone", *args], env={
        "RCLONE_CONFIG_ACCEPTANCE_TYPE": "s3", "RCLONE_CONFIG_ACCEPTANCE_PROVIDER": "Other",
        "RCLONE_CONFIG_ACCEPTANCE_ENDPOINT": settings["endpoint"],
        "RCLONE_CONFIG_ACCEPTANCE_ACCESS_KEY_ID": settings["access_key"],
        "RCLONE_CONFIG_ACCEPTANCE_SECRET_ACCESS_KEY": settings["secret_key"],
        "RCLONE_CONFIG_ACCEPTANCE_REGION": "us-east-1", "RCLONE_CONFIG_ACCEPTANCE_ENV_AUTH": "false",
    }, timeout=timeout)


def admin(shared, sql):
    # F2 registers the dedicated Unix socket; administrative access uses peer auth.
    socket = run(["pg_conftool", str(shared["major"]), "foundry", "show", "unix_socket_directories"]).stdout
    socket = socket.split("=", 1)[-1].strip().strip("'").split(",")[0].strip()
    assert socket.startswith("/") and socket != "/var/run/postgresql", "Expected Foundry-specific socket"
    return run(["runuser", "-u", "foundry-postgres", "--", "psql", "-X", "-qAt", "-v", "ON_ERROR_STOP=1",
                "-h", socket, "-p", str(shared["port"]), "-d", "postgres"], input=sql).stdout.strip()


def main():
    guard()
    host, project, profile, entry = fixture()
    from contracts import parse_env
    settings = s3_settings()
    for bucket in (settings["app_bucket"], settings["backup_bucket"]):
        # Buckets belong exclusively to this runner's S3 stand-in volume/network.
        rclone(settings, "mkdir", "acceptance:" + bucket)
    work = WORK / ("backup-" + uuid.uuid4().hex)
    work.mkdir(mode=0o700)
    from acceptance_host import WORK as HOST_WORK
    identity = HOST_WORK / "age-identity.txt"
    assert identity.is_file(), "Bootstrap recovery identity is missing"
    object_key = "attachments/acceptance-" + uuid.uuid4().hex
    object_file = work / "attachment.txt"
    object_file.write_text("attachment-original-" + uuid.uuid4().hex)
    rclone(settings, "copyto", str(object_file), f"acceptance:{settings['app_bucket']}/{object_key}")
    assert rclone(settings, "cat", f"acceptance:{settings['app_bucket']}/{object_key}").stdout == object_file.read_text()

    # Bootstrap consumes request["secrets"] and configures the shared repository.
    # Manual backup uses that same persisted binding; it does not reconfigure it.
    from backup import STANZA, CONFIG, _backrest
    stanza, config = STANZA, CONFIG
    shared = host.read_registry()["shared"]["postgresql"]
    assert shared["cluster"] == "foundry" and shared["port"] != 5432
    pg = postgres_settings(host, entry)
    assert psql(pg, "SELECT 1;").stdout.strip() == "1"
    assert psql(pg, "SHOW archive_mode;").stdout.strip() == "on"
    archive_command = psql(pg, "SHOW archive_command;").stdout
    assert "sudo" not in archive_command and "/usr/bin/python3 -I " in archive_command and archive_command.strip().endswith("/backup.py archive-push %p")
    assert Path(admin(shared, "SHOW data_directory;")).resolve() == Path(shared["data_path"]).resolve()
    _backrest(host, ["check"], timeout=300)
    table = "acceptance_pitr_" + uuid.uuid4().hex
    psql(pg, f"CREATE TABLE {table} (id integer PRIMARY KEY, marker text NOT NULL);"
             f" INSERT INTO {table} VALUES (1, 'before-backup');")
    config_dir = host.paths.instance_config(entry["name"])
    originals = {f"instances/{entry['name']}/{name}": (config_dir / name).read_bytes()
                 for name in ("app.env", "secrets.json")}
    # Referenced _FILE values use files/<absolute path without />
    # in the encrypted tar. Compare their actual contents, not just references.
    for name, value in parse_env((config_dir / "app.env").read_text()).items():
        if name.endswith("_FILE") and value:
            source = Path(value)
            assert source.is_absolute() and source.is_file() and source.stat().st_size <= 16 * 1024 * 1024
            originals["files/" + str(source).lstrip("/")] = source.read_bytes()
    # backup-run(kind=full) returns backup_id, pgbackrest_label,
    # key_archive.object_key, and objects_prefix. Completion means uploaded bytes.
    backup = action("backup-run", request(project, profile, kind="full"), timeout=1800)
    info = json.loads(_backrest(host, ["--output=json", "info"]).stdout)
    found = [item for item in info if item["name"] == stanza]
    assert len(found) == 1 and found[0]["status"]["code"] == 0
    assert any(item["label"] == backup["pgbackrest_label"] and item["type"] == "full"
               for item in found[0]["backup"]), "No completed full backup in repository"
    psql(pg, f"INSERT INTO {table} VALUES (2, 'after-backup-before-target');")
    target = psql(pg, "SELECT clock_timestamp();").stdout.strip()
    # Server time creates an unambiguous interval without trusting client clocks.
    psql(pg, "SELECT pg_sleep(1);")
    psql(pg, f"INSERT INTO {table} VALUES (3, 'after-target');")
    count = int(admin(shared, "SELECT archived_count FROM pg_stat_archiver;"))
    admin(shared, "SELECT pg_switch_wal();")
    wait_for(lambda: int(admin(shared, "SELECT archived_count FROM pg_stat_archiver;")) > count,
             "WAL containing post-backup writes was not archived", timeout=180)
    _backrest(host, ["check"], timeout=300)
    # restore-drill uses pgBackRest time recovery in a NEW temporary
    # cluster, keeps it for inspection, and returns connection settings (same
    # peer-authenticated socket, data_path and unit. It never targets live PG.
    live_pid = run(["systemctl", "show", shared["unit"], "-p", "MainPID", "--value"]).stdout
    drill = action("restore-drill", request(project, profile, backup_id=backup["backup_id"],
                   target_time=target, target_inclusive=True, retain=True), timeout=1800)
    restored = drill["postgres"]
    assert restored["port"] not in (5432, shared["port"])
    assert Path(drill["data_path"]).resolve() != Path(shared["data_path"]).resolve()
    assert drill["unit"] != shared["unit"]
    assert restored["sslmode"] == "disable" and restored["host"].startswith("/")
    assert restored["user"] == "foundry-postgres"
    def restored_sql(sql):
        return run(["runuser", "-u", "foundry-postgres", "--", "psql", "-X", "-qAt", "--no-password",
                    "-v", "ON_ERROR_STOP=1", "-h", restored["host"], "-p", str(restored["port"]),
                    "-d", restored["database"]], input=sql).stdout.strip()
    assert restored_sql("SELECT 1;") == "1"
    assert restored_sql("SHOW port;") == str(restored["port"])
    assert restored_sql("SHOW listen_addresses;") == "", "Drill must expose no TCP listener"
    restored_pid = service(drill["unit"])["pid"]
    assert (Path(drill["data_path"]) / "postmaster.pid").read_text().splitlines()[0] == str(restored_pid)
    rows = restored_sql(f"SELECT id, marker FROM {table} ORDER BY id;").splitlines()
    assert rows == ["1|before-backup", "2|after-backup-before-target"], "PITR did not stop at requested time"
    assert psql(pg, f"SELECT count(*) FROM {table};").stdout.strip() == "3", "Live rows were changed by restore"
    assert run(["systemctl", "show", shared["unit"], "-p", "MainPID", "--value"]).stdout == live_pid

    encrypted, decrypted = work / "keys.age", work / "keys.tar"
    rclone(settings, "copyto", f"acceptance:{settings['backup_bucket']}/{backup['key_archive']['object_key']}", str(encrypted))
    assert encrypted.read_bytes().startswith(b"age-encryption.org/v1\n"), "Key archive is not age ciphertext"
    run(["age", "--decrypt", "-i", str(identity), "-o", str(decrypted), str(encrypted)])
    # Key tar members use instances/<name>/{app.env,secrets.json};
    # referenced key/config files are also included by the production backup owner.
    # Never extract untrusted archive paths as root.
    with tarfile.open(decrypted) as archive:
        assert len(archive.getnames()) == len(set(archive.getnames())), "Duplicate key archive members"
        for name, expected in originals.items():
            member = archive.getmember(name)
            assert member.isfile() and member.size == len(expected)
            assert archive.extractfile(member).read() == expected, "Recovered key/config bytes differ"
    binding = next(item for item in backup["object_copy"]["bindings"] if item["bucket"] == settings["app_bucket"])
    copied = f"acceptance:{settings['backup_bucket']}/{binding['objects_prefix'].rstrip('/')}/{object_key}"
    assert rclone(settings, "cat", copied).stdout == object_file.read_text(), "Attachment backup bytes differ"
    assert rclone(settings, "cat", f"acceptance:{settings['app_bucket']}/{object_key}").stdout == object_file.read_text()
    # No schema/database/bucket reset. Resources expire with this owned container.
    decrypted.unlink()
    print("PASS pgBackRest full/WAL/PITR boundary; live cluster preserved; age key/config and attachment round trips")


if __name__ == "__main__":
    main()
