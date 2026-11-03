#!/usr/bin/env python3
"""Opt-in acceptance INSIDE a disposable Ubuntu/systemd Docker container only.

This provisions real services, databases and users in that container. It never
resets or drops data. Test certificates replace public ACME issuance; localhost
PostgreSQL/Redis TLS and normal production activation are exercised unchanged.
Pass a locally built release using --archive to also exercise activation.
"""

import argparse
import concurrent.futures
import copy
import hashlib
import io
import itertools
import json
import os
from pathlib import Path
import secrets
import socket
import ssl
import sys
import tarfile
import time
import tomllib
import uuid

sys.dont_write_bytecode = True
sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "lib"))
from topology import instance_units, validate_topology
from contracts import PROFILE_SCHEMA, atomic_json, atomic_write, digest, identity, read_json
from host import Host
from provision import bootstrap_plan
from release import activate
from acceptance_support import action, guard, private_json, postgres_denial, postgres_settings, request, run, wait_for

TOOLS = Path(__file__).resolve().parents[1]
HOST_ID = "10000000-0000-4000-8000-000000000001"
WORK = Path("/var/lib/foundry-acceptance")


def fixtures():
    result = []
    for number, (slug, environment) in enumerate((("abc", "acceptance"), ("abc", "acceptance-stage"), ("xyz", "acceptance")), 1):
        project = {"schema": 1, "id": f"20000000-0000-4000-8000-{1 if slug == 'abc' else 2:012d}", "slug": slug}
        profile = {"schema": PROFILE_SCHEMA, "environment": environment, "instance_id": f"30000000-0000-4000-8000-{number:012d}",
                   "host": {"address": "localhost", "port": 22, "host_id": HOST_ID},
                   "runtime_environment": "testing",
                   "domain": f"{slug}-{environment}.acceptance.invalid", "certbot_email": "test@example.invalid",
                   "architecture": {"aarch64": "arm64", "x86_64": "amd64"}[os.uname().machine],
                   "public_env": {}, "retention": {"server_releases": 2, "local_releases": 2, "pre_migration_dumps": 2}, "edge_origin_lockdown": False}
        result.append((project, profile))
    return result


def archive_contract(source):
    with tarfile.open(source, "r:gz") as archive:
        manifest = json.load(archive.extractfile("manifest.json"))
        config = tomllib.loads(archive.extractfile("config/deploy.toml").read().decode())
    candidates = [validate_topology({"schema": 1, "roles": [{"name": name} for name in roles]})
                  for count in (2, 3) for roles in itertools.permutations(("http", "scheduler", "worker"), count)
                  if "http" in roles and "scheduler" in roles]
    topology = next((item for item in candidates if item["sha256"] == manifest.get("topology_sha256")), None)
    assert topology is not None, "Archive must declare HTTP + scheduler (worker optional) and its topology checksum"
    private_json(WORK / "topology.json", topology)
    disks = copy.deepcopy(config["app"]["services"]["storage"]["disks"])
    assert disks, "Archive must declare its private attachment disks"
    return topology, disks


def bootstrap_secrets(disks):
    from acceptance_backup import rclone, s3_settings
    settings = s3_settings(include_app=True)
    for bucket in (settings["app_bucket"], settings["backup_bucket"]):
        rclone(settings, "mkdir", "acceptance:" + bucket)
    identity_file = WORK / "age-identity.txt"
    run(["age-keygen", "-o", str(identity_file)])
    recipient = run(["age-keygen", "-y", str(identity_file)]).stdout.strip()
    for disk in disks.values():
        assert disk["driver"] == "r2", "Acceptance archive must select the R2 attachment contract"
        # The framework accepts only real R2 endpoints for the r2 driver, so the
        # local stand-in is declared S3-compatible with R2's exact guarantees.
        disk["driver"] = "s3_compatible"
        disk.setdefault("config", {})["visibility"] = 0
        disk.setdefault("cloud", {}).update(endpoint=settings["endpoint"], region="us-east-1",
            bucket=settings["app_bucket"], credentials="acceptance", path_style=True,
            compatible={"requires_nfc_keys": True, "conditional_create": True, "conditional_replace": True,
                        "single_request_conditions": True})
    private = {"backup_s3": {"endpoint": settings["endpoint"], "region": "us-east-1",
        "bucket": settings["backup_bucket"], "access_key_id": settings["access_key"],
        "secret_access_key": settings["secret_key"]}, "age_recipients": [recipient],
        "app_storage": {"credentials": {"acceptance": {"mode": "static", "access_key": settings["access_key"],
            "secret_key": settings["secret_key"]}}, "disks": disks}}
    keys = [key for key in os.environ.get("ACCEPTANCE_SECRET_CONFIG_KEYS", "").split(",") if key]
    if keys:
        # First-deploy application settings travel with the bootstrap intake.
        private["configuration"] = {key: secrets.token_urlsafe(24) for key in keys}
    return private


def fixture_topology():
    return read_json(WORK / "topology.json")


def prepare(host, fixture, private=None):
    project, profile = fixture
    payload = request(project, profile, topology=fixture_topology())
    plan = json.loads(run([sys.executable, str(TOOLS / "server/manage.py"), "bootstrap-plan"],
                          input=json.dumps(payload), timeout=180).stdout)
    assert plan["ok"], "Bootstrap plan refused"
    payload.update(plan_token=plan["plan_token"], secrets=private or {})
    result = json.loads(run([sys.executable, str(TOOLS / "server/manage.py"), "bootstrap-apply"],
                            input=json.dumps(payload), timeout=1200).stdout)
    assert result["ok"] and result["provisioning"] == "ready"
    return result


def certificate_renewal(host):
    from certificates import _timer
    entries = list(host.read_registry()["instances"].values())
    selected = entries[0]
    def served(entry):
        context = ssl.create_default_context()
        with socket.create_connection(("127.0.0.1", 443), timeout=5) as raw:
            with context.wrap_socket(raw, server_hostname=entry["profile"]["domain"]) as secured:
                return hashlib.sha256(secured.getpeercert(binary_form=True)).hexdigest()
    # systemctl reload returns before new workers serve the latest site.
    previous = {entry["name"]: wait_for(lambda entry=entry: served(entry), "Nginx did not serve " + entry["name"], timeout=30)
                for entry in entries}
    master = host.run(["systemctl", "show", "nginx", "-p", "MainPID", "--value"]).stdout
    host.run(["systemctl", "start", _timer(selected) + ".service"], timeout=180)
    result = host.run(["systemctl", "show", _timer(selected) + ".service", "-p", "ExecMainStatus", "--value"]).stdout
    assert result.strip() == "0", "Owned synthetic renewal failed"
    wait_for(lambda: served(selected) != previous[selected["name"]],
             "Nginx did not serve the renewed synthetic certificate", timeout=30)
    assert host.run(["systemctl", "show", "nginx", "-p", "MainPID", "--value"]).stdout == master
    assert all(served(entry) == previous[entry["name"]] for entry in entries[1:])
    print("PASS CA-trusted synthetic renewal through the owned service; changed served certificate and unchanged nginx master")


def snapshot(host):
    registry = host.read_registry()
    secrets = {entry["name"]: digest(host.paths.instance_config(entry["name"]) / "secrets.json")
               for entry in registry["instances"].values()}
    return host.paths.registry.read_bytes(), secrets


def redis(host, entry, *arguments):
    password = read_json(host.paths.instance_config(entry["name"]) / "secrets.json")["redis_password"]
    result = host.run(["redis-cli", "-3", "--json", "--tls", "--cacert", "/etc/ssl/certs/ca-certificates.crt",
                      "-h", "localhost", "-p", str(host.read_registry()["shared"]["redis"]["port"]), "--user", entry["allocations"]["redis_user"], *arguments],
                     env={"REDISCLI_AUTH": password}, check=False)
    return result.stdout.strip()


class RedisReplyError(RuntimeError):
    """Retain only the protocol error code, never credentials or reply bodies."""


class RedisSession:
    """Small bounded RESP2 fixture for commands that must share a connection."""
    def __init__(self, host, entry):
        password = read_json(host.paths.instance_config(entry["name"]) / "secrets.json")["redis_password"]
        context = ssl.create_default_context(cafile="/etc/ssl/certs/ca-certificates.crt")
        raw = socket.create_connection(("localhost", host.read_registry()["shared"]["redis"]["port"]), timeout=5)
        try:
            self.socket = context.wrap_socket(raw, server_hostname="localhost")
        except BaseException:
            raw.close()
            raise
        self.buffer = bytearray()
        try:
            assert self.command("AUTH", entry["allocations"]["redis_user"], password) == "OK"
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


def isolation(host, lease_script=None):
    # Grants are produced by this archive's redis:keys inventory during activation.
    entries = list(host.read_registry()["instances"].values())
    persisted = []
    for entry in entries:
        own = postgres_settings(host, entry)
        cache = [prefix for prefix in entry["redis_grants"]["keys"] if prefix.startswith("foundry:cache:")]
        assert cache, "Archive must declare a cache for positive ACL controls"
        key = cache[0] + "acceptance:" + uuid.uuid4().hex
        with RedisSession(host, entry) as client:
            assert client.command("MULTI") == "OK"
            assert client.command("SET", key, "preserved", "EX", 600) == "QUEUED"
            assert client.command("GET", key) == "QUEUED"
            assert client.command("EXEC") == ["OK", "preserved"]
            assert client.command("MULTI") == "OK"
            assert client.command("SET", key, "discarded", "EX", 600) == "QUEUED"
            assert client.command("DISCARD") == "OK"
            assert client.command("GET", key) == "preserved"
            for foreign in entries:
                if foreign["profile"]["instance_id"] == entry["profile"]["instance_id"]:
                    continue
                postgres_denial(own, foreign["allocations"]["database"])
                for prefix in foreign["redis_grants"]["keys"]:
                    foreign_key = prefix + "acceptance:foreign"
                    assert client.command("MULTI") == "OK"
                    assert client.command("SET", key, "must-not-commit", "EX", 600) == "QUEUED"
                    denied(client, "NOPERM", "GET", foreign_key)
                    denied(client, "EXECABORT", "EXEC")
                    assert client.command("GET", key) == "preserved"
                    denied(client, "NOPERM", "SUBSCRIBE", foreign_key)
                denied(client, "NOPERM", "PUBLISH", "acceptance:foreign", "denied")
            for command in (("KEYS", "*"), ("SCAN", "0"), ("CONFIG", "GET", "*")):
                denied(client, "NOPERM", *command)
            if lease_script:
                prefixes = [prefix for prefix in entry["redis_grants"]["keys"] if prefix.startswith("foundry:lease:")]
                assert prefixes, "Archive must declare scheduler coordination leases"
                lease_key, owner = prefixes[0] + "acceptance:" + uuid.uuid4().hex, uuid.uuid4().hex
                assert client.command("EVAL", lease_script, 1, lease_key, "acquire", owner, 60000, 32) == 1
                script_id, successor = hashlib.sha1(lease_script).hexdigest(), uuid.uuid4().hex
                assert client.command("EVALSHA", script_id, 1, lease_key, "acquire", successor, 60000, 32) == 0
                assert client.command("EVALSHA", script_id, 1, lease_key, "renew", owner, 60000, 32) == 1
                assert client.command("EVALSHA", script_id, 1, lease_key, "transfer", owner, 60000, 32, successor) == 1
                assert client.command("EVALSHA", script_id, 1, lease_key, "release", owner, 10, 32) == 0
                assert client.command("EVALSHA", script_id, 1, lease_key, "release", successor, 10, 32) == 1
                assert client.command("GET", lease_key) is None
        persisted.append((entry, key))
    for project, profile in fixtures():
        action("stop", request(project, profile))
        _, entry = host.require_identity(project, profile)
        assert host.load_state(entry)["status"] == "stopped"
        for unit in instance_units(entry).values():
            assert host.run(["systemctl", "is-active", "--quiet", unit], check=False).returncode != 0
    host.run(["systemctl", "restart", host.read_registry()["shared"]["redis"]["unit"]])
    for entry, key in persisted:
        assert json.loads(redis(host, entry, "GET", key)) == "preserved"
    for project, profile in fixtures():
        action("restart", request(project, profile))
    print("PASS T1 verified TLS database controls; exact Redis grants, transactions, lease Lua, ACL persistence and role stop/restart")


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
    registered = list(host.read_registry()["instances"].values())
    states = {entry["profile"]["instance_id"]: host.load_state(entry) for entry in registered}
    missing_receipts = {entry["profile"]["instance_id"] for entry in registered
                        if not states[entry["profile"]["instance_id"]].get("backup", {}).get("last_complete_id")
                        and not entry.get("backup", {}).get("last_complete_id")}
    for privileged in (False, True):
        argv = ["/usr/local/bin/foundry-host-report"]
        if not privileged:
            argv = ["runuser", "-u", "nobody", "--", *argv]
        result = host.run(argv, check=False)
        value = json.loads(result.stdout)
        assert value["schema"] == 2 and value["host"]["host_id"] == HOST_ID
        entries = [item for project in value["projects"] for item in project["instances"]]
        assert len(entries) == len(registered) == 3
        assert set(value["shared_services"]) == {"postgresql", "redis", "nginx"}
        for service in value["shared_services"].values():
            assert service is not None and service["load"] == "loaded", "Shared-service probes must work without root"
        reported = {item["instance_id"]: item for item in entries}
        assert set(reported) == set(states)
        expected_problems = {("backup_stale", instance_id) for instance_id in missing_receipts}
        serialized = json.dumps(value)
        for entry in registered:
            instance_id = entry["profile"]["instance_id"]
            item, state = reported[instance_id], states[instance_id]
            if entry["provisioning"]["state"] == "ready":
                assert item["service"] is not None and item["service"]["load"] == "loaded"
                assert set(item["services"]) == set(instance_units(entry))
                assert all(role["state"] and role["state"]["load"] == "loaded" for role in item["services"].values())
            status = state["status"]
            if status in {"healthy", "rolled_back"}:
                assert item["recorded_status"] == status
                assert all(role["state"]["active"] == "active" and role["state"]["main_pid"] > 0
                           for role in item["services"].values()), "All declared units must be active"
                assert item["readiness"] is not None and item["readiness"]["ready"] is True
                inaccessible = not privileged and any(role["state"].get("exe") is None
                                                     for role in item["services"].values())
                if inaccessible:
                    # Linux may refuse cross-UID /proc/PID/exe readlink. Public
                    # observations remain useful but cannot claim proven health.
                    expected_problems.update({("running_release_unverified", instance_id),
                                              ("instance_state_inconsistent", instance_id)})
                    assert item["status"] == "unknown" and item["metadata_consistent"] is False
                else:
                    assert item["status"] == status and item["metadata_consistent"] is True
                    executable = str(host.paths.opt(entry["name"]) / "releases" / state["current"]["version"] / "artisan")
                    assert all(role["state"]["exe"] == executable for role in item["services"].values())
            private = read_json(host.paths.instance_config(entry["name"]) / "secrets.json")
            for key, secret in private.items():
                if key.endswith("password"):
                    assert secret not in serialized
        assert {(item["code"], item.get("instance_id")) for item in value["problems"]} == expected_problems
        assert value["complete"] is (not expected_problems)
        assert result.returncode == 0 or (result.returncode == 1 and expected_problems), "Unexpected report failure"
        atomic_json(WORK / ("report-root.json" if privileged else "report.json"), value)
    print("PASS public role/readiness observations and root-proven running release identities")



def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--archive", type=Path, required=True)
    parser.add_argument("--redis-source", type=Path, help="Read lease Lua directly from this mounted pinned Foundry-Go module root")
    args = parser.parse_args()
    guard()
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
    topology, disks = archive_contract(args.archive)
    private = bootstrap_secrets(disks)
    from acceptance_backup import rclone, s3_settings
    settings = s3_settings(include_app=True)
    for number, fixture in enumerate(fixtures(), 1):
        instance_private = copy.deepcopy(private)
        bucket = settings["app_bucket"] if number == 1 else settings["app_bucket"] + "-" + str(number)
        rclone(settings, "mkdir", "acceptance:" + bucket)
        for disk in instance_private["app_storage"]["disks"].values():
            disk["cloud"]["bucket"] = bucket
        prepare(host, fixture, instance_private)
        print("PASS bootstrap " + identity(*fixture)["name"], flush=True)
    from acceptance_capacity import verify_settings
    import capacity
    verify_settings(host, capacity.memory_mib(host))
    print("PASS bootstrap effective PostgreSQL and Redis values match available RAM", flush=True)
    certificate_renewal(host)
    before = snapshot(host)
    for fixture in fixtures():
        assert prepare(host, fixture)["unchanged"]
    assert before == snapshot(host)
    print("PASS repeated bootstrap preserves exact registry, credentials and allocations", flush=True)
    plans = [(project, profile, bootstrap_plan(host, project, profile, topology=topology)) for project, profile in fixtures()]
    def repeat_bootstrap(item):
        project, profile, plan = item
        payload = request(project, profile, plan_token=plan["plan_token"], topology=topology)
        return json.loads(run([sys.executable, str(TOOLS / "server/manage.py"), "bootstrap-apply"],
                                   input=json.dumps(payload), timeout=180).stdout)
    with concurrent.futures.ThreadPoolExecutor(max_workers=3) as pool:
        assert all(result["unchanged"] for result in pool.map(repeat_bootstrap, plans))
    assert before == snapshot(host)
    print("PASS concurrent bootstrap entrypoints preserve registry, credentials and allocations", flush=True)
    report(host)
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
            payload = request(project, profile, archive=str(archive), archive_sha256=digest(archive))
            return json.loads(run([sys.executable, str(TOOLS / "server/manage.py"), "activate"],
                                       input=json.dumps(payload), timeout=120).stdout)
        with concurrent.futures.ThreadPoolExecutor(max_workers=4) as pool:
            assert all(result["unchanged"] for result in pool.map(again, [archives[0], *archives]))
        assert before == snapshot(host)
        report(host)
        print("PASS repeated concurrent deployments; retained registry, credentials and nonroot report", flush=True)
        isolation(host, lease_script)
        report(host)
    assert counter == host.run(["systemctl", "show", unrelated.name, "-p", "ActiveEnterTimestampMonotonic", "-p", "NRestarts"]).stdout
    print("PASS unrelated service remained running without restart; external DNS/ACME unverified", flush=True)


if __name__ == "__main__":
    main()
