#!/usr/bin/env python3
"""Lead-only three-host recovery acceptance; run_acceptance.sh owns the hosts.

The source keeps the age identity. Only its decrypted recovery archive and release
are transferred to the empty targets. No production provider is contacted.
"""
import copy
import importlib.util
import json
from pathlib import Path
import shutil
import sys
import tarfile
from unittest.mock import patch
import uuid

sys.dont_write_bytecode = True
TOOLS = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(TOOLS / "lib"))
from acceptance_support import WORK, action, fixture, guard, postgres_settings, psql, request, run
from acceptance_backup import admin, rclone, s3_settings
from contracts import atomic_json, digest, read_json
from host import Host
import backup
import recover
import release
from topology import instance_units

EXPORT = WORK / "recover-export"


def listing():
    settings = s3_settings()
    rows = json.loads(rclone(settings, "lsjson", "acceptance:" + settings["backup_bucket"],
                             "--recursive", "--files-only", "--hash").stdout)
    return sorted(({key: row.get(key) for key in ("Path", "Size", "ModTime", "Hashes")} for row in rows), key=lambda row: row["Path"])


def table_snapshot(pg):
    tables = json.loads(psql(pg, "SELECT coalesce(json_agg(json_build_array(n.nspname,c.relname) ORDER BY n.nspname,c.relname),'[]') "
        "FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace "
        "WHERE c.relkind IN ('r','p') AND n.nspname NOT IN ('pg_catalog','information_schema') AND n.nspname NOT LIKE 'pg_toast%';").stdout)
    result = {}
    for schema, table in tables:
        quoted = '.'.join('"' + value.replace('"', '""') + '"' for value in (schema, table))
        result[schema + "." + table] = psql(pg, "SELECT count(*),md5(coalesce(string_agg(md5(row_to_json(t)::text),'' "
            f"ORDER BY md5(row_to_json(t)::text)),'')) FROM {quoted} t;").stdout.strip()
    assert result, "Source must contain application tables"
    return result


def source():
    host, project, profile, entry = fixture()
    # Keep PostgreSQL and the source host alive for rehearsal, but quiesce writers
    # and scheduled backups so an unrelated source upload cannot hide a target write.
    for item in host.read_registry()["instances"].values():
        release.stop(host, item)
    run(["systemctl", "disable", "--now", "foundry-backup-full-cluster.timer", "foundry-backup-diff-cluster.timer"])
    for unit in ("foundry-backup-full-cluster.service", "foundry-backup-diff-cluster.service"):
        observed = run(["systemctl", "show", unit, "--property=ActiveState", "--value"]).stdout.strip()
        assert observed == "inactive", "Wait for the source's admitted backup before recovery acceptance"
    pg = postgres_settings(host, entry)
    table = "acceptance_recover_" + uuid.uuid4().hex
    psql(pg, f"CREATE TABLE {table} (id integer PRIMARY KEY, value text NOT NULL); "
             f"INSERT INTO {table} SELECT n, 'recovery-row-' || n FROM generate_series(1,25) n;")
    snapshot = table_snapshot(pg)
    receipt = action("backup-run", request(project, profile, kind="full"), timeout=3600)
    backup._backrest(host, ["check"], timeout=300)
    assert table_snapshot(pg) == snapshot, "Source changed while capturing its recovery baseline"
    EXPORT.mkdir(mode=0o700)
    settings = s3_settings()
    encrypted, decrypted = EXPORT / "keys.age", EXPORT / "recovery.tar"
    rclone(settings, "copyto", f"acceptance:{settings['backup_bucket']}/{receipt['key_archive']['object_key']}", str(encrypted))
    recipients = backup._settings(host, entry)["age_recipients"]
    from acceptance_host import WORK as HOST_WORK
    candidates = [HOST_WORK / "age-identity.txt", *WORK.glob("backup-*/replacement-age-identity.txt")]
    identities = [path for path in candidates if path.is_file()
                  and run(["age-keygen", "-y", str(path)]).stdout.strip() in recipients]
    assert identities, "Matching operator recovery identity is unavailable"
    run(["age", "--decrypt", "-i", str(identities[-1]), "-o", str(decrypted), str(encrypted)])
    encrypted.unlink()
    files = recover.read_archive(decrypted)
    recover.archive_configuration(project, profile, files, entry["topology"])
    current = host.load_state(entry)["current"]
    directory = release.verified_release(host, entry, current)
    with tarfile.open(EXPORT / "release.tar.gz", "w:gz") as archive:
        for name in ["manifest.json", *sorted(current["files"])]:
            archive.add(directory / name, arcname=name, recursive=False)
    atomic_json(EXPORT / "source.json", {"project": project, "profile": profile, "topology": entry["topology"],
                "snapshot": snapshot, "table": table, "repository": listing()}, mode=0o600)
    assert not any("identity" in path.name for path in EXPORT.iterdir()), "An age identity must never leave the source fixture"
    print("PASS source A: quiesced application, 25 rows, full backup, decrypted archive, per-table counts and hashes")


def repository_unchanged():
    expected = read_json(EXPORT / "source.json")["repository"]
    assert listing() == expected, "Rehearsal changed the repository object listing, sizes, hashes or modification times"
    host, _, _, _ = fixture()
    assert admin(host.read_registry()["shared"]["postgresql"], "SELECT NOT pg_is_in_recovery();") == "t"
    print("PASS source A is still alive; repository object listing unchanged through rehearsal")


def target(mode):
    assert mode in {"rehearsal", "takeover"}
    host = Host()
    assert host.read_registry(missing=True) is None, "Target B/C must be empty"
    source = read_json(EXPORT / "source.json")
    project, profile = source["project"], copy.deepcopy(source["profile"])
    profile["host"].update(address=mode + ".acceptance.invalid", host_id=str(uuid.uuid4()))
    payload = request(project, profile, recovery_archive=str(EXPORT / "recovery.tar"), topology=source["topology"], mode=mode,
                      archive_sha256=digest(EXPORT / "release.tar.gz"))
    spec = importlib.util.spec_from_file_location("acceptance_recovery_manage", TOOLS / "server/manage.py")
    manage = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(manage)
    plan = manage.execute(host, "recover-plan", payload)
    assert plan["ok"] and host.read_registry(missing=True) is None
    print("PASS target read-only recovery plan", flush=True)
    assert not Path("/usr/bin/pgbackrest").exists(), "Recovery target must exercise pgBackRest installation"
    # Synthetic CA setup is deliberately deferred until the application phase,
    # after production preflight has proved an empty host and the repository.
    install_site = recover.provision._install_site
    def synthetic_site(host, registry, entry, tools_dir, **options):
        directory = Path("/etc/foundry/acceptance-ca")
        directory.mkdir(mode=0o700, exist_ok=True)
        for name in ("ca.crt", "ca.key"):
            shutil.copyfile(WORK / name, directory / name)
            (directory / name).chmod(0o600)
        return install_site(host, registry, entry, tools_dir, **options)
    activate = release.activate_recovered
    def compare_then_activate(host, registry, entry, *args, **options):
        assert table_snapshot(postgres_settings(host, entry)) == source["snapshot"], "Restored per-table counts/hashes differ"
        return activate(host, registry, entry, *args, **options)
    with patch.object(recover.provision, "_install_site", side_effect=synthetic_site), \
            patch.object(release, "activate_recovered", side_effect=compare_then_activate):
        result = manage.execute(host, "recover-apply", {**payload, "archive": str(EXPORT / "release.tar.gz"),
                                "plan_token": plan["plan_token"], "old_server_gone": mode == "takeover"})
    assert result["ok"] and result["host_id"] == profile["host"]["host_id"]
    _, entry = host.require_identity(project, profile)
    pg = postgres_settings(host, entry)
    assert psql(pg, f"SELECT count(*) FROM {source['table']};").stdout.strip() == "25"
    assert entry["recovered_from"]["host_id"] == source["profile"]["host"]["host_id"]
    assert entry["allocations"]["database"] == "f_" + uuid.UUID(profile["instance_id"]).hex
    cluster = host.read_registry()["shared"]["postgresql"]
    assert cluster["ownership_token"] != json.loads(recover.read_archive(EXPORT / "recovery.tar")["recovery/pgbackrest-repository.json"])["cluster_token"]
    if mode == "rehearsal":
        assert table_snapshot(pg) == source["snapshot"]
        assert psql(pg, "SHOW archive_mode;").stdout.strip() == "off"
        assert entry["provisioning"]["state"] == "rehearsal"
        for role, unit in instance_units(entry).items():
            if role != "http":
                assert not Path("/etc/systemd/system", unit).exists(), "Rehearsal installed a background role"
        assert not list(Path("/etc/systemd/system").glob("foundry-backup-*.timer"))
        assert not Path("/etc/foundry/letsencrypt").exists()
        for mutation in ("restart", "backup-run", "tools-apply", "bootstrap-apply", "activate"):
            try:
                manage.execute(host, mutation, request(project, profile))
            except Exception as error:
                assert getattr(error, "code", None) == "rehearsal_read_only"
            else:
                raise AssertionError("Rehearsal accepted a mutation")
        print("PASS B rehearsal: exact table counts/hashes, localhost HTTP, no certificate/background roles/timers, mutations refused")
        return
    assert entry["provisioning"]["state"] == "ready"
    assert psql(pg, "SHOW archive_mode;").stdout.strip() == "on"
    assert result["backup"]["status"] == "complete"
    assert result["remaining_manual_steps"] == ["Point DNS at the new server."]
    from acceptance_host import WORK as HOST_WORK
    HOST_WORK.mkdir(mode=0o700, exist_ok=True)
    from acceptance_roles import second_archive
    candidate, version = second_archive(EXPORT / "release.tar.gz", project, profile)
    deployed = action("activate", request(project, profile, archive=str(candidate), archive_sha256=digest(candidate)), timeout=1800)
    assert deployed["version"] == version
    fresh = action("backup-run", request(project, profile, kind="full"), timeout=3600)
    assert fresh["status"] == "complete" and fresh["backup_id"] != result["backup"]["backup_id"]
    drill = action("restore-drill", request(project, profile, backup_id=fresh["backup_id"]), timeout=3600)
    assert drill["status"] == "complete" and not drill["retained"]
    failed = run(["systemctl", "list-units", "--state=failed", "--all", "--no-legend", "--plain", "foundry-restore-*"]).stdout
    assert not failed.strip(), "Completed drill left a failed unit"
    for unit in (drill["unit"], drill["restore_unit"]):
        assert "abnormal database system shutdown" not in run(["journalctl", "-u", unit, "--no-pager", "-o", "cat"]).stdout
    for timer in ("foundry-backup-full-cluster.timer", "foundry-backup-diff-cluster.timer"):
        run(["systemctl", "is-enabled", timer])
        run(["systemctl", "is-active", timer])
    print("PASS C takeover: recovered rows, managed ownership, later deploy, new full/key backup, clean restore drill, active timers")


if __name__ == "__main__":
    guard()
    command = sys.argv[1:]
    if command == ["source"]:
        source()
    elif command == ["unchanged"]:
        repository_unchanged()
    elif len(command) == 1 and command[0] in {"rehearsal", "takeover"}:
        target(command[0])
    else:
        raise SystemExit("Expected source, unchanged, rehearsal or takeover")
