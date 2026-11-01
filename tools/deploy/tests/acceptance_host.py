#!/usr/bin/env python3
"""Opt-in acceptance INSIDE a disposable Ubuntu/systemd Docker container only.

This provisions real services, databases and users in that container. It never
resets or drops data. Test certificates replace public ACME issuance; localhost
PostgreSQL/Redis TLS and normal production activation are exercised unchanged.
Pass a locally built release using --archive to also exercise activation.
"""

# Moved from the upstream deploy harness; F4 must port this before acceptance.
if __name__ == "__main__":
    raise SystemExit("F4 acceptance port pending; this moved harness is not runnable yet.")

import argparse
import concurrent.futures
import copy
import hashlib
import io
import json
import os
from pathlib import Path
import socket
import ssl
import subprocess
import sys
import tarfile
import time
import uuid

sys.dont_write_bytecode = True
sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "lib"))
from topology import instance_units
from contracts import atomic_json, atomic_write, digest, identity, read_json
from host import Host
from provision import ROOTS, bootstrap_apply, bootstrap_plan
from release import activate
import release
from acceptance_http import https_response

TOOLS = Path(__file__).resolve().parents[1]
HOST_ID = "10000000-0000-4000-8000-000000000001"
WORK = Path("/var/lib/foundry-acceptance")


def fixtures():
    result = []
    for number, (slug, environment) in enumerate((("abc", "production"), ("abc", "staging"), ("xyz", "production")), 1):
        project = {"schema": 1, "id": f"20000000-0000-4000-8000-{1 if slug == 'abc' else 2:012d}", "slug": slug}
        profile = {"schema": 1, "environment": environment, "instance_id": f"30000000-0000-4000-8000-{number:012d}",
                   "host": {"address": "localhost", "port": 22, "host_id": HOST_ID},
                   "runtime_environment": "production" if environment == "production" else "staging",
                   "domain": f"{slug}-{environment}.acceptance.invalid", "certbot_email": "test@example.invalid",
                   "architecture": {"aarch64": "arm64", "x86_64": "amd64"}[os.uname().machine],
                   "public_env": {}, "retention": {"server": 2, "local": 2, "backups": 2}, "native_imaging": False}
        result.append((project, profile))
    return result


def prepare(host, fixture):
    project, profile = fixture
    domain = profile["domain"]
    certificate = Path("/etc/letsencrypt/live") / domain
    certificate.mkdir(parents=True, exist_ok=True)
    if not (certificate / "fullchain.pem").exists():
        host.run(["openssl", "req", "-x509", "-newkey", "rsa:2048", "-nodes", "-days", "2",
                  "-subj", "/CN=" + domain, "-addext", "subjectAltName=DNS:" + domain,
                  "-keyout", str(certificate / "privkey.pem"), "-out", str(certificate / "fullchain.pem")])
        (certificate / "privkey.pem").chmod(0o600)
    plan = bootstrap_plan(host, project, profile)
    if not plan["ok"]:
        raise RuntimeError("Bootstrap refused: " + json.dumps(plan["findings"]))
    result = bootstrap_apply(host, project, profile, TOOLS, plan_token=plan["plan_token"])
    assert result["provisioning"] == "ready"
    return result


def snapshot(host):
    registry = host.read_registry()
    secrets = {name: digest(host.paths.instance_config(name) / "secrets.json") for name in registry["instances"]}
    return host.paths.registry.read_bytes(), secrets


def redis(host, entry, *arguments):
    password = read_json(host.paths.instance_config(entry["name"]) / "secrets.json")["redis_password"]
    result = host.run(["redis-cli", "-3", "--json", "--tls", "--cacert", "/etc/ssl/certs/ca-certificates.crt",
                      "-h", "localhost", "-p", "6380", "--user", entry["redis_user"], *arguments],
                     env={"REDISCLI_AUTH": password}, check=False)
    return result.stdout.strip()


class RedisReplyError(RuntimeError):
    """Retain only the protocol error code, never credentials or reply bodies."""


class RedisSession:
    """Small bounded RESP2 fixture for commands that must share a connection."""
    def __init__(self, host, entry):
        password = read_json(host.paths.instance_config(entry["name"]) / "secrets.json")["redis_password"]
        context = ssl.create_default_context(cafile="/etc/ssl/certs/ca-certificates.crt")
        raw = socket.create_connection(("localhost", 6380), timeout=5)
        try:
            self.socket = context.wrap_socket(raw, server_hostname="localhost")
        except BaseException:
            raw.close()
            raise
        self.buffer = bytearray()
        try:
            assert self.command("AUTH", entry["redis_user"], password) == "OK"
        except BaseException:
            self.socket.close()
            raise

    def __enter__(self):
        return self

    def __exit__(self, *_):
        self.socket.close()

    def command(self, *arguments):
        parts = [item if isinstance(item, bytes) else str(item).encode() for item in arguments]
        payload = f"*{len(parts)}\r\n".encode() + b"".join(
            f"${len(item)}\r\n".encode() + item + b"\r\n" for item in parts)
        self.socket.settimeout(5)
        self.socket.sendall(payload)
        return self.receive()

    def receive(self):
        # One deadline covers every fragment of an entire reply, including a
        # blocked SUBSCRIBE wait; a slow peer cannot extend it by trickling bytes.
        deadline = time.monotonic() + 5

        def fill():
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                raise TimeoutError("Redis fixture reply deadline exceeded")
            self.socket.settimeout(remaining)
            chunk = self.socket.recv(4096)
            if not chunk:
                raise RuntimeError("Redis fixture connection ended before its reply")
            self.buffer.extend(chunk)
            if len(self.buffer) > 65536:
                raise RuntimeError("Redis fixture reply exceeds its bound")

        def take(length):
            while len(self.buffer) < length:
                fill()
            result = bytes(self.buffer[:length])
            del self.buffer[:length]
            return result

        def reply(depth=0):
            if depth > 4:
                raise RuntimeError("Redis fixture reply nesting exceeds its bound")
            while b"\r\n" not in self.buffer:
                fill()
            line = take(self.buffer.index(b"\r\n") + 2)[:-2]
            kind, value = line[:1], line[1:]
            if kind == b"-":
                raise RedisReplyError(value.split(b" ", 1)[0].decode("ascii", errors="replace"))
            if kind == b"+":
                return value.decode()
            if kind == b":":
                return int(value)
            if kind in (b"$", b"*"):
                length = int(value)
                if length == -1:
                    return None
                if not 0 <= length <= (65534 if kind == b"$" else 64):
                    raise RuntimeError("Redis fixture reply length exceeds its bound")
                if kind == b"*":
                    return [reply(depth + 1) for _ in range(length)]
                payload = take(length + 2)
                assert payload[-2:] == b"\r\n"
                return payload[:-2].decode()
            raise RuntimeError("Redis fixture received an unsupported reply")

        return reply()


def denied(client, code, *arguments):
    try:
        client.command(*arguments)
    except RedisReplyError as error:
        assert str(error) == code
        return
    raise AssertionError("Redis accepted a command expected to be denied")


def redis_workflows(host, entry, foreign_keys, lease_script):
    namespace = entry["namespace"]
    prefix = f"foundry:cache:v1:{namespace['application']}:{namespace['environment']}:acceptance:"
    key, channel = prefix + "transaction", prefix + "subscription"
    with RedisSession(host, entry) as client:
        assert client.command("MULTI") == "OK"
        assert client.command("SET", key, "transaction-value", "EX", 60) == "QUEUED"
        assert client.command("GET", key) == "QUEUED"
        assert client.command("EXEC") == ["OK", "transaction-value"]
        assert client.command("MULTI") == "OK"
        assert client.command("SET", key, "discarded-value", "EX", 60) == "QUEUED"
        assert client.command("DISCARD") == "OK"
        for foreign in foreign_keys:
            assert client.command("MULTI") == "OK"
            assert client.command("SET", key, "must-not-commit", "EX", 60) == "QUEUED"
            denied(client, "NOPERM", "GET", foreign)
            denied(client, "EXECABORT", "EXEC")
            assert client.command("GET", key) == "transaction-value"
            denied(client, "NOPERM", "SUBSCRIBE", foreign)
        child_channel = f"foundry:cache:v1:{namespace['application']}.acceptance:{namespace['environment']}:subscription"
        for own_channel in (channel, child_channel):
            denied(client, "NOPERM", "SUBSCRIBE", own_channel)
            denied(client, "NOPERM", "PSUBSCRIBE", own_channel + "*")
            denied(client, "NOPERM", "PUBLISH", own_channel, "acceptance-denied")
        if lease_script is not None:
            # Exact source supplied from the pinned module, assembled identically
            # to redis/lease.go. Only this fixture's unique finite lease is touched.
            lease_key = f"foundry:lease:v1:{namespace['application']}:{namespace['environment']}:acceptance:{uuid.uuid4().hex}"
            owner, successor = uuid.uuid4().hex, uuid.uuid4().hex
            script_id = hashlib.sha1(lease_script).hexdigest()
            assert client.command("EVAL", lease_script, 1, lease_key, "acquire", owner, 60000, 32) == 1
            assert client.command("EVALSHA", script_id, 1, lease_key, "acquire", successor, 60000, 32) == 0
            assert client.command("EVALSHA", script_id, 1, lease_key, "renew", owner, 60000, 32) == 1
            assert client.command("EVALSHA", script_id, 1, lease_key, "transfer", owner, 60000, 32, successor) == 1
            assert client.command("EVALSHA", script_id, 1, lease_key, "release", owner, 10, 32) == 0
            for foreign in foreign_keys:
                denied(client, "NOPERM", "EVALSHA", script_id, 1, foreign, "acquire", owner, 60000, 32)
            assert client.command("EVALSHA", script_id, 1, lease_key, "release", successor, 10, 32) == 1
            assert client.command("GET", lease_key) is None


def isolation(host, lease_script=None):
    entries = list(host.read_registry()["instances"].values())
    for entry in entries:
        namespace = entry["namespace"]
        password = read_json(host.paths.instance_config(entry["name"]) / "secrets.json")["database_password"]
        postgres_env = {"PGPASSWORD": password, "PGSSLMODE": "verify-full", "PGSSLROOTCERT": ROOTS}
        own = host.run(["psql", "-X", "-qAt", "--no-password", "-h", "localhost", "-U", entry["database"],
                        "-d", entry["database"], "-c", "SELECT 1"], env=postgres_env)
        assert own.stdout.strip() == "1", "Own-database positive control failed"
        key = f"foundry:cache:v1:{namespace['application']}:{namespace['environment']}:acceptance:key"
        assert json.loads(redis(host, entry, "SET", key, "preserved")) == "OK"
        assert json.loads(redis(host, entry, "GET", key)) == "preserved"
        assert "NOPERM" in redis(host, entry, "PUBLISH", key, "fixture")
        child_key = f"foundry:cache:v1:{namespace['application']}.acceptance:{namespace['environment']}:acceptance:key"
        assert json.loads(redis(host, entry, "SET", child_key, "preserved", "EX", "600")) == "OK"
        assert json.loads(redis(host, entry, "GET", child_key)) == "preserved"
        assert "NOPERM" in redis(host, entry, "PUBLISH", child_key, "fixture")
        foreign_keys = []
        for foreign in entries:
            if foreign["instance_id"] == entry["instance_id"]:
                continue
            other = foreign["namespace"]
            foreign_key = f"foundry:cache:v1:{other['application']}:{other['environment']}:acceptance:key"
            foreign_keys.append(foreign_key)
            foreign_keys.append(f"foundry:cache:v1:{other['application']}.acceptance:{other['environment']}:acceptance:key")
            assert "NOPERM" in redis(host, entry, "GET", foreign_key)
            assert "NOPERM" in redis(host, entry, "PUBLISH", foreign_key, "fixture")
            result = host.run(["psql", "-X", "-qAt", "--no-password", "-h", "localhost", "-U", entry["database"],
                               "-d", foreign["database"], "-c", "SELECT 1"], check=False,
                              env=postgres_env)
            assert result.returncode != 0
        foreign_keys.extend(f"foundry:cache:v1:{namespace['application']}{suffix}:{namespace['environment']}:acceptance:key"
                            for suffix in ("-other", "x"))
        foreign_keys.append(f"foundry:cache:v1:{namespace['application']}.acceptance:{namespace['environment']}2:acceptance:key")
        for foreign_key in foreign_keys:
            assert "NOPERM" in redis(host, entry, "GET", foreign_key)
            assert "NOPERM" in redis(host, entry, "PUBLISH", foreign_key, "fixture")
        redis_workflows(host, entry, foreign_keys, lease_script)
        # Destructive-command permission is checked by bootstrap ACL DRYRUN;
        # never actually execute a flush even as an expected-denied test.
        for command in (("KEYS", "*"), ("SCAN", "0"), ("CONFIG", "GET", "*")):
            assert "NOPERM" in redis(host, entry, *command)
    host.run(["systemctl", "restart", "redis-server.service"])
    for entry in entries:
        namespace = entry["namespace"]
        key = f"foundry:cache:v1:{namespace['application']}:{namespace['environment']}:acceptance:key"
        assert json.loads(redis(host, entry, "GET", key)) == "preserved"
    print("PASS real PostgreSQL cross-instance denial; Redis transactions, base/child key isolation, all channels denied and ACL persistence")
    print("PASS exact pinned Foundry lease Lua with EVAL/EVALSHA and foreign-key denial" if lease_script is not None
          else "UNVERIFIED pinned Foundry lease Lua: pass --redis-source with the mounted pinned module")


def archive_for(source, project, profile):
    # Reuse the already locally compiled payload; only fixture identities change.
    name = identity(project, profile)["name"]
    destination = WORK / (name + ".tar.gz")
    with tarfile.open(source, "r:gz") as incoming, tarfile.open(destination, "w:gz") as outgoing:
        for item in incoming:
            if item.name != "manifest.json":
                outgoing.addfile(item, incoming.extractfile(item) if item.isfile() else None)
                continue
            manifest = json.load(incoming.extractfile(item))
            manifest.update(project_id=project["id"], instance_id=profile["instance_id"], instance=name,
                            environment=profile["environment"], architecture=profile["architecture"])
            payload = json.dumps(manifest).encode()
            item.size = len(payload)
            outgoing.addfile(item, io.BytesIO(payload))
    return destination


def report(host):
    value = json.loads(host.run(["runuser", "-u", "nobody", "--", "/usr/local/bin/foundry-host-report"]).stdout)
    assert value["schema"] == 1 and value["host"]["host_id"] == HOST_ID
    entries = [entry for project in value["projects"] for entry in project["instances"]]
    assert len(entries) == 3
    assert set(value["shared_services"]) == {"postgresql", "redis", "nginx"}
    for service in value["shared_services"].values():
        assert service is not None and service["load"] == "loaded", "Shared-service probes must work without root"
    registered = host.read_registry()["instances"].values()
    reported = {entry["instance_id"]: entry for entry in entries}
    assert set(reported) == {entry["instance_id"] for entry in registered}
    serialized = json.dumps(value)
    for entry in registered:
        item = reported[entry["instance_id"]]
        if entry["provisioning"]["state"] == "ready":
            assert item["service"] is not None and item["service"]["load"] == "loaded", "Prepared instance service probes must work without root"
            assert set(item["services"]) == {"http", "worker", "scheduler"}
            assert all(s["state"] and s["state"]["load"] == "loaded" for s in item["services"].values())
        status = host.load_state(entry)["status"]
        if status in {"healthy", "rolled_back"}:
            assert item["status"] == status and item["metadata_consistent"], "Healthy recorded state must have matching live observations"
            assert item["service"]["active"] == "active"
            assert all(s["state"]["active"] == "active" for s in item["services"].values()), "All three supervised units must be active"
            assert item["readiness"] is not None and item["readiness"]["ready"] is True
        private = read_json(host.paths.instance_config(entry["name"]) / "secrets.json")
        for key, secret in private.items():
            if key.endswith("password"):
                assert secret not in serialized
    atomic_json(WORK / "report.json", value)
    return entries


def operate(host, fixture, action):
    project, profile = fixture
    result = host.run([sys.executable, str(TOOLS / "server/manage.py"), action],
                      input=json.dumps({"project": project, "profile": profile}), timeout=180)
    return json.loads(result.stdout)


def topology_lifecycle(host, fixture, rollback_archive):
    project, profile = fixture
    _, entry = host.require_identity(project, profile)
    others = {name: release.services(host, other) for name, other in host.read_registry()["instances"].items()
              if name != entry["name"]}
    original = host.load_state(entry)["current"]["version"]
    before = release.services(host, entry)
    started = time.monotonic()
    assert operate(host, fixture, "stop")["ok"]
    assert time.monotonic() - started < 45, "Idle topology exceeded its stop budget"
    assert host.load_state(entry)["status"] == "stopped"
    assert all(item["ActiveState"] == "inactive" for item in release.services(host, entry).values())
    for unit in instance_units(entry).values():
        result = host.run(["systemctl", "show", unit, "--property=Result,ExecMainStatus"]).stdout
        values = dict(line.split("=", 1) for line in result.splitlines() if "=" in line)
        assert values == {"Result": "success", "ExecMainStatus": "130"}, "Process did not stop gracefully"
    assert operate(host, fixture, "restart")["ok"]
    assert release.ready(host, entry)
    assert all(before[role]["MainPID"] != item["MainPID"] for role, item in release.services(host, entry).items())
    print("PASS real three-unit stop (exit 130 within 45s), restart and readiness", flush=True)
    archive = archive_for(rollback_archive, project, profile)
    candidate = activate(host, project, profile, archive, digest(archive))
    assert candidate["version"] != original
    candidate_pids = release.services(host, entry)
    assert operate(host, fixture, "rollback")["version"] == original
    assert host.load_state(entry)["status"] == "rolled_back" and release.current_version(host, entry) == original
    assert release.ready(host, entry)
    assert all(candidate_pids[role]["MainPID"] != item["MainPID"] for role, item in release.services(host, entry).items())
    for name, previous in others.items():
        assert previous == release.services(host, host.read_registry()["instances"][name]), "Another instance was restarted"
    report(host)
    print("PASS real release switch and rollback restore all three units; other instances unchanged", flush=True)


def background_runtime(host, fixture):
    _, entry = host.require_identity(*fixture)
    for path in ("/agent/", "/admin/", "/account/", "/en-my/"):
        status, headers, _ = https_response(entry, path)
        assert status == 200 and "text/html" in headers.get("Content-Type", "")
    deadline = time.monotonic() + 35
    while time.monotonic() < deadline:
        output = host.run(["journalctl", "--unit", instance_units(entry)["scheduler"], "--lines=200",
                           "--no-pager", "--output=cat"], timeout=5).stdout
        if any(json.loads(line).get("msg") == "delivery health check completed"
               for line in output.splitlines() if line.startswith("{")):
            print("PASS full-profile portal/SSR HTML and scheduled delivery-health execution", flush=True)
            return
        time.sleep(1)
    raise AssertionError("The supervised scheduler did not execute delivery health")




def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--archive", type=Path)
    parser.add_argument("--rollback-archive", type=Path, help="A second locally built release for live three-unit rollback")
    parser.add_argument("--redis-source", type=Path, help="Read lease Lua directly from this mounted pinned Foundry-Go module root")
    args = parser.parse_args()
    if os.geteuid() != 0 or os.environ.get("FOUNDRY_DISPOSABLE_HOST") != "1" or not Path("/.dockerenv").is_file():
        raise RuntimeError("Run only in the explicitly designated disposable Docker acceptance host")
    lease_script = None
    if args.redis_source:
        paths = [args.redis_source / "redis" / name for name in ("lease_owner.lua", "lease.lua")]
        if any(path.is_symlink() or not path.is_file() or path.stat().st_size > 65536 for path in paths):
            raise RuntimeError("Pinned Redis source must contain bounded regular lease_owner.lua and lease.lua files")
        lease_script = b"".join(path.read_bytes() for path in paths)
    host = Host()
    registry = host.read_registry(missing=True)
    if registry and (registry["host_id"] != HOST_ID or not (WORK / "owned").exists()):
        raise RuntimeError("Refusing an existing host outside this fixture")
    WORK.mkdir(mode=0o700, exist_ok=True)
    atomic_write(WORK / "owned", "disposable acceptance fixture\n", 0o600)
    unrelated = Path("/etc/systemd/system/foundry-acceptance-unrelated.service")
    atomic_write(unrelated, "[Service]\nExecStart=/usr/bin/sleep infinity\n")
    host.run(["systemctl", "daemon-reload"])
    host.run(["systemctl", "start", unrelated.name])
    counter = host.run(["systemctl", "show", unrelated.name, "-p", "ActiveEnterTimestampMonotonic", "-p", "NRestarts"]).stdout
    for fixture in fixtures():
        prepare(host, fixture)
        print("PASS bootstrap " + identity(*fixture)["name"], flush=True)
    before = snapshot(host)
    for fixture in fixtures():
        assert prepare(host, fixture)["unchanged"]
    assert before == snapshot(host)
    print("PASS repeated bootstrap preserves exact registry, credentials and allocations", flush=True)
    plans = [(project, profile, bootstrap_plan(host, project, profile)) for project, profile in fixtures()]
    def repeat_bootstrap(item):
        project, profile, plan = item
        request = {"project": project, "profile": profile, "confirm": True, "plan_token": plan["plan_token"]}
        return json.loads(host.run([sys.executable, str(TOOLS / "server/manage.py"), "bootstrap-apply"],
                                   input=json.dumps(request), timeout=180).stdout)
    with concurrent.futures.ThreadPoolExecutor(max_workers=3) as pool:
        assert all(result["unchanged"] for result in pool.map(repeat_bootstrap, plans))
    assert before == snapshot(host)
    print("PASS concurrent bootstrap entrypoints preserve registry, credentials and allocations", flush=True)
    report(host)
    isolation(host, lease_script)
    if args.archive:
        archives = []
        for project, profile in fixtures():
            archive = archive_for(args.archive, project, profile)
            activate(host, project, profile, archive, digest(archive))
            archives.append((project, profile, archive))
            print("PASS real binary activation " + identity(project, profile)["name"], flush=True)
        before = snapshot(host)
        def again(item):
            project, profile, archive = item
            # SSH runs each operation in its own process. Exercise that exact
            # entrypoint, lock boundary and main-thread readiness deadline.
            request = {"project": project, "profile": profile, "archive": str(archive), "archive_sha256": digest(archive)}
            return json.loads(host.run([sys.executable, str(TOOLS / "server/manage.py"), "activate"],
                                       input=json.dumps(request), timeout=120).stdout)
        with concurrent.futures.ThreadPoolExecutor(max_workers=4) as pool:
            assert all(result["unchanged"] for result in pool.map(again, [archives[0], *archives]))
        assert before == snapshot(host)
        report(host)
        print("PASS repeated concurrent deployments; retained registry, credentials and nonroot report", flush=True)
        if args.rollback_archive:
            topology_lifecycle(host, fixtures()[0], args.rollback_archive)
        background_runtime(host, fixtures()[0])
    assert counter == host.run(["systemctl", "show", unrelated.name, "-p", "ActiveEnterTimestampMonotonic", "-p", "NRestarts"]).stdout
    print("PASS unrelated service remained running without restart; external DNS/ACME unverified", flush=True)


if __name__ == "__main__":
    main()
