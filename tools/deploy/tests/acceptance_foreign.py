#!/usr/bin/env python3
"""Install/capture foreign services BEFORE bootstrap; verify AFTER every lane.

Raw config snapshots and observer logs stay root-only inside the disposable
container. The fixture owns all seed writes. Foundry has no reason to connect
to either foreign data service during the observation window.
"""
import argparse
import base64
import hashlib
import http.client
import json
from pathlib import Path

from acceptance_support import WORK, guard, private_json, run, service, wait_for

BASELINE = WORK / "foreign.json"
DOMAIN = "foreign-rust.acceptance.invalid"
SITE = Path("/etc/nginx/sites-available/foreign-rust.conf")
ENABLED = Path("/etc/nginx/sites-enabled/foreign-rust.conf")
PAGE = Path("/var/www/foreign-rust/index.html")
MONITOR = WORK / "foreign-redis.monitor"
OBSERVER = "foundry-acceptance-foreign-observer.service"
SENTINEL = "foreign-rust-sentinel-v1"


def redis(*args):
    return run(["redis-cli", "--raw", "-h", "127.0.0.1", "-p", "6379", *args]).stdout.strip()


def redis_connections():
    fields = dict(line.split(":", 1) for line in redis("INFO", "stats").splitlines()
                  if ":" in line and not line.startswith("#"))
    return int(fields["total_connections_received"])


def pg(major, sql, database="postgres"):
    return run(["runuser", "-u", "postgres", "--", "psql", "-X", "-qAt",
                "-v", "ON_ERROR_STOP=1", "-h", "/var/run/postgresql", "-p", "5432",
                "-d", database], input=sql).stdout.strip()


def foreign_http():
    client = http.client.HTTPConnection("127.0.0.1", 80, timeout=5)
    try:
        client.request("GET", "/", headers={"Host": DOMAIN})
        response = client.getresponse()
        payload = response.read(4097)
        assert response.status == 200 and payload == (SENTINEL + "\n").encode()
        return {"status": response.status, "body": payload.decode()}
    finally:
        client.close()


def config_snapshot(known_paths=None):
    paths = set(Path("/etc/redis").rglob("*"))
    for root in Path("/etc/postgresql").glob("*/main"):
        paths.add(root)
        paths.update(root.rglob("*"))
    paths.update((Path("/etc/redis"), SITE, ENABLED, PAGE, Path("/etc/nginx/nginx.conf")))
    if known_paths is not None:
        # New Foundry-owned Redis files coexist beside the frozen foreign files.
        paths = {Path(name) for name in known_paths}
    result = {}
    for path in sorted(paths):
        stat = path.lstat()
        item = {"mode": stat.st_mode, "uid": stat.st_uid, "gid": stat.st_gid}
        if path.is_symlink():
            item["target"] = str(path.readlink())
            if path.is_file():
                data = path.read_bytes()
                item.update(bytes=base64.b64encode(data).decode(), sha256=hashlib.sha256(data).hexdigest())
        elif path.is_file():
            data = path.read_bytes()
            item.update(bytes=base64.b64encode(data).decode(), sha256=hashlib.sha256(data).hexdigest())
        elif not path.is_dir():
            raise AssertionError("Unexpected foreign configuration file type")
        result[str(path)] = item
    return result


def install():
    assert not Path("/etc/foundry/registry.json").exists(), "Baseline must precede Foundry bootstrap"
    assert not BASELINE.exists(), "Never overwrite a prior baseline"
    run(["apt-get", "install", "-y", "--no-install-recommends", "redis-server", "postgresql", "nginx"],
        env={"DEBIAN_FRONTEND": "noninteractive"}, timeout=1200)
    clusters = run(["pg_lsclusters", "--no-header"]).stdout.splitlines()
    mains = [line.split()[0] for line in clusters if line.split()[1] == "main"]
    assert len(mains) == 1, "Expected exactly one distro main cluster"
    major = mains[0]
    config = Path(f"/etc/postgresql/{major}/main/postgresql.conf")
    # Audit is configured before capture; Foundry may not change these bytes.
    with config.open("a") as stream:
        stream.write("\n# Disposable foreign fixture audit\nlog_connections = on\n"
                     "log_disconnections = off\nlog_statement = 'all'\n"
                     "logging_collector = off\nlog_destination = 'stderr'\n")
    run(["systemctl", "restart", f"postgresql@{major}-main.service"])
    run(["systemctl", "start", "redis-server.service"])
    assert redis("PING") == "PONG"
    default = redis("ACL", "GETUSER", "default")
    assert "nopass" in default.splitlines() and "on" in default.splitlines()
    assert redis("SET", "foreign:rust:sentinel", SENTINEL) == "OK"
    pg(major, "CREATE ROLE foreign_rust LOGIN;\nCREATE DATABASE foreign_rust OWNER foreign_rust;")
    pg(major, "SET ROLE foreign_rust; CREATE TABLE sentinel (id integer PRIMARY KEY, value text NOT NULL);"
              f" INSERT INTO sentinel VALUES (1, '{SENTINEL}');", "foreign_rust")
    PAGE.parent.mkdir(parents=True)
    PAGE.write_text(SENTINEL + "\n")
    PAGE.chmod(0o644)
    PAGE.parent.chmod(0o755)
    assert not SITE.exists() and not ENABLED.exists()
    SITE.write_text(f"server {{ listen 80; server_name {DOMAIN}; root {PAGE.parent}; }}\n")
    SITE.chmod(0o644)
    ENABLED.symlink_to(SITE)
    run(["nginx", "-t"])
    run(["systemctl", "enable", "--now", "nginx.service"])
    run(["systemctl", "reload", "nginx.service"])
    wait_for(lambda: foreign_http(), "Foreign HTTP did not become ready")
    return major


def baseline(mode):
    guard()
    assert not BASELINE.exists() and not Path("/etc/foundry/registry.json").exists()
    if mode == "fresh":
        assert not Path("/etc/redis").exists()
        assert not list(Path("/etc/postgresql").glob("*/main"))
        for unit in ("redis-server.service", "postgresql.service", "nginx.service"):
            assert run(["systemctl", "is-active", "--quiet", unit], check=False).returncode != 0
        private_json(BASELINE, {"mode": mode})
        print("PASS fresh baseline: no foreign services/configuration")
        return
    major = install()
    value = {"mode": mode, "major": major, "config": config_snapshot(),
             "services": {unit: service(unit) for unit in (
                 "redis-server.service", f"postgresql@{major}-main.service", "nginx.service")},
             "redis": redis("GET", "foreign:rust:sentinel"), "redis_size": redis("DBSIZE"),
             "postgres": pg(major, "TABLE sentinel;", "foreign_rust"), "http": foreign_http()}
    assert value["redis"] == SENTINEL and value["postgres"] == "1|" + SENTINEL
    log = Path(f"/var/log/postgresql/postgresql-{major}-main.log")
    # Last fixture connection has exited; only Foundry runs until verify().
    value["pg_log"] = {"path": str(log), "inode": log.stat().st_ino, "offset": log.stat().st_size}
    value["redis_connections"] = redis_connections()
    MONITOR.touch(mode=0o600, exist_ok=False)
    run(["systemd-run", "--unit", OBSERVER, "--property=Type=exec", "/bin/sh", "-c",
         f"exec redis-cli --raw -h 127.0.0.1 -p 6379 MONITOR > {MONITOR}"])
    wait_for(lambda: MONITOR.read_text().startswith("OK\n"), "Redis observer failed to start")
    value["observer"] = service(OBSERVER)
    private_json(BASELINE, value)
    print("PASS foreign baseline: config bytes/hashes, PID/start ticks, data, HTTP and audit window")


def verify():
    guard()
    before = json.loads(BASELINE.read_text())
    if before["mode"] == "fresh":
        # Package-created unused defaults are allowed; they must remain stopped.
        assert run(["systemctl", "is-active", "--quiet", "redis-server.service"], check=False).returncode != 0
        for main in Path("/etc/postgresql").glob("*/main"):
            unit = f"postgresql@{main.parent.name}-main.service"
            assert run(["systemctl", "is-active", "--quiet", unit], check=False).returncode != 0
        print("PASS fresh host: package-created defaults remain inactive")
        return
    assert service(OBSERVER) == before["observer"], "Foreign Redis observation was interrupted"
    # End audit BEFORE making fixture-owned verification connections.
    run(["systemctl", "stop", OBSERVER])
    assert MONITOR.read_text().splitlines() == ["OK"], "Unexpected command on foreign Redis (private audit retained)"
    # The observer and this INFO probe are the only two permitted connections.
    # This also catches AUTH failures and commands excluded from MONITOR output.
    assert redis_connections() == before["redis_connections"] + 2, "Unexpected connection to foreign Redis"
    log = Path(before["pg_log"]["path"])
    assert log.stat().st_ino == before["pg_log"]["inode"]
    assert log.stat().st_size >= before["pg_log"]["offset"], "Foreign PostgreSQL audit truncated"
    with log.open("rb") as stream:
        stream.seek(before["pg_log"]["offset"])
        audit = stream.read().lower()
    assert not any(token in audit for token in (
        b"connection received", b"connection authenticated", b"connection authorized",
        b"statement:", b"execute ", b"password authentication failed", b"pg_hba.conf"
    )), "Unexpected connection/query on foreign PostgreSQL (private audit retained)"
    assert config_snapshot(before["config"]) == before["config"], "Foreign configuration bytes or metadata changed"
    assert {unit: service(unit) for unit in before["services"]} == before["services"], "Foreign service restarted"
    assert redis("GET", "foreign:rust:sentinel") == before["redis"]
    assert redis("DBSIZE") == before["redis_size"]
    assert pg(before["major"], "TABLE sentinel;", "foreign_rust") == before["postgres"]
    assert foreign_http() == before["http"]
    print("PASS foreign config/data/PIDs/HTTP preserved; no observed Redis commands or PostgreSQL connections")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("baseline", "verify"))
    parser.add_argument("--mode", choices=("foreign", "fresh"), default="foreign")
    args = parser.parse_args()
    baseline(args.mode) if args.action == "baseline" else verify()
