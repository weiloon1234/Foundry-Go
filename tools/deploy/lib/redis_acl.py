"""Exact, journaled grants for the dedicated Foundry Redis instance.

The grant engine is inherited from ooo's corrected disk-before-live reconciler.
Public registry entries contain literal prefixes; credential-derived ACL digests
and transition intent belong exclusively to each instance's private journal.
"""
from __future__ import annotations

from collections.abc import Sequence
import hashlib
import grp
import os
import stat
import json
from pathlib import Path
import re
import secrets
import socket
import ssl
import time
import uuid
import capacity
from typing import TYPE_CHECKING

from contracts import (DeployError, REDIS_PORT, REDIS_ACCOUNT, REDIS_ADMIN,
                       REDIS_ACLFILE, REDIS_UNIT, HOST_PATHS, MAX_JSON, check_path, instance_paths, read_json, require)
from runtime_config import read_credentials
if TYPE_CHECKING:
    from host import Host

ROOTS = "/etc/ssl/certs/ca-certificates.crt"
REDIS_CONFIG = "/etc/foundry/redis/redis.conf"
REDIS_DATA = "/var/lib/foundry/redis"
REDIS_CERT = "/etc/foundry/redis/server.crt"
REDIS_KEY = "/etc/foundry/redis/server.key"
REDIS_UNIT_PATH = "/etc/systemd/system/" + REDIS_UNIT
REDIS_TEMPLATE = "redis-server@.service"
REDIS_COMMANDS = frozenset("""
ping hello auth quit select client|setname multi exec discard
get set mget mset del unlink exists type strlen getdel getex
expire pexpire expireat pexpireat ttl pttl expiretime pexpiretime persist
incr incrby incrbyfloat decr decrby
hget hset hdel hexists hkeys hlen hmget hgetall hstrlen hincrby
sadd srem sismember smembers scard
zadd zrem zscore zcard zcount zrange zrangebylex zrangebyscore
zremrangebyscore zrank zrevrank zrevrange zrevrangebyscore zincrby
llen lindex lpush rpush lpop rpop lrange ltrim
publish subscribe unsubscribe psubscribe punsubscribe
eval evalsha time
""".split())

REDIS_PREFIX = re.compile(r"foundry:(?P<feature>[a-z][a-z0-9-]*):v1:(?P<application>[^:]+):(?P<environment>[^:]+):")
REDIS_GLOB = re.compile(r"[*?\[\]\\\s]")
MAX_REDIS_PATTERNS = 64


def _service_claim(host: Host, registry: dict) -> tuple[dict | None, dict]:
    shared = registry.get("shared", {}).get("redis")
    path = check_path(host.paths.p(HOST_PATHS["private"]) / "redis-prepare.json", "file")
    if not path.exists():
        return shared, {}
    info = path.stat()
    require(not info.st_mode & 0o077 and (info.st_uid == 0 or host.paths.root != Path("/")),
            "redis_ownership", "Redis preparation journal must be root-owned and private.")
    claim = read_json(path)
    require(isinstance(claim, dict) and claim.get("host_id") == registry["host_id"],
            "redis_ownership", "Redis preparation journal belongs to another host.")
    return shared, claim


def _save_claim(host: Host, claim: dict) -> None:
    directory = host.check_control_directory(host.paths.p(HOST_PATHS["private"]))
    directory.mkdir(parents=True, exist_ok=True, mode=0o700)
    require(not directory.stat().st_mode & 0o077, "redis_ownership", "Redis preparation journal directory must be private.")
    host.owned_write(directory / "redis-prepare.json", json.dumps(claim, sort_keys=True, indent=2) + "\n", mode=0o600)


def _unit(host: Host) -> dict:
    result = host.run(["systemctl", "show", REDIS_UNIT, "--property=LoadState,ActiveState,FragmentPath,MainPID,UnitFileState,DropInPaths"], check=False)
    value = dict(line.split("=", 1) for line in result.stdout.splitlines() if "=" in line)
    require(result.returncode == 0 or value.get("LoadState") == "not-found",
            "redis_inspection", "Cannot inspect the dedicated Redis unit.")
    return value


def _unused_template(unit: dict) -> bool:
    return (unit.get("FragmentPath") in {"/lib/systemd/system/redis-server@.service", "/usr/lib/systemd/system/redis-server@.service"}
            and unit.get("ActiveState") == "inactive" and unit.get("UnitFileState") in {"disabled", "static", "indirect"}
            and not unit.get("DropInPaths"))


def _package_template_pending(host: Host, registry: dict) -> bool:
    if registry.get("tools", {}).get("generation", 0) < 12:
        return False
    from packages import default_activation_pending
    return default_activation_pending(host, REDIS_TEMPLATE)


def _reserved_template(host: Host, registry: dict, unit: dict) -> bool:
    if (unit.get("LoadState") != "masked" or unit.get("ActiveState") != "inactive"
            or unit.get("MainPID") != "0" or unit.get("DropInPaths")):
        return False
    # A concrete/runtime override is never covered by the distro template claim.
    for directory, names in (("/etc/systemd/system", (REDIS_UNIT, REDIS_UNIT + ".d", REDIS_TEMPLATE + ".d")),
                             ("/run/systemd/system", (REDIS_UNIT, REDIS_TEMPLATE, REDIS_UNIT + ".d", REDIS_TEMPLATE + ".d"))):
        for name in names:
            path = host.paths.p(directory) / name
            if path.exists() or path.is_symlink():
                return False
    return _package_template_pending(host, registry)


def _port_busy(host: Host) -> bool:
    result = host.run(["ss", "-H", "-ltn", "sport", "=", str(REDIS_PORT)])
    return bool(result.stdout.strip())


def _owned_service(shared: dict | None, claim: dict) -> None:
    require(bool(shared) and shared.get("owner") == "foundry" and shared.get("unit") == REDIS_UNIT
            and shared.get("port") == REDIS_PORT and shared.get("aclfile") == REDIS_ACLFILE
            and shared.get("ownership_token") == claim.get("ownership_token") and bool(claim)
            and capacity.redis_valid(shared.get("capacity"))
            and shared["capacity"] == claim.get("allocation", {}).get("capacity"),
            "redis_ownership", "The dedicated Redis allocation has no matching ownership journal.")


def _claimed_files(host: Host, claim: dict, *, complete: bool = False, capacity_target=None) -> None:
    for absolute, digest in claim.get("files", {}).items():
        require(absolute in {REDIS_CONFIG, REDIS_UNIT_PATH, REDIS_CERT, REDIS_KEY},
                "redis_ownership", "Unexpected file in Redis preparation journal.")
        path = check_path(host.paths.p(absolute), "file")
        require(not complete or path.exists(), "redis_ownership", "An owned Redis service file is missing.")
        if path.exists():
            allowed = {digest}
            if absolute == REDIS_CONFIG and capacity_target is not None:
                allowed.add(hashlib.sha256(_redis_config(claim["ownership_token"], capacity_target).encode()).hexdigest())
            require(hashlib.sha256(path.read_bytes()).hexdigest() in allowed,
                    "redis_ownership", "An owned Redis service file changed outside its recorded plan.")


def inspect(host: Host, registry: dict, *, capacity_target=None) -> dict:
    """Inspect dedicated metadata only; no requests ever reach foreign Redis."""
    shared, claim = _service_claim(host, registry)
    unit = _unit(host)
    unused = _unused_template(unit) or _reserved_template(host, registry, unit)
    paths = ("/etc/foundry/redis", REDIS_DATA, REDIS_UNIT_PATH)
    present = any(check_path(host.paths.p(path)).exists() for path in paths)
    if not shared and not claim:
        account = host.run(["getent", "passwd", REDIS_ACCOUNT], check=False)
        require(account.returncode in (0, 2), "redis_inspection", "Cannot inspect the Redis service account.")
        return {"state": "conflict" if present or (unit.get("LoadState") != "not-found" and not unused) or account.returncode == 0 or _port_busy(host) else "absent",
                "owner": None, "unit": REDIS_UNIT, "port": REDIS_PORT, "aclfile": REDIS_ACLFILE}
    shared = shared or claim.get("allocation")
    _owned_service(shared, claim)
    _claimed_files(host, claim, complete=shared.get("state") == "ready", capacity_target=capacity_target)
    active = unit.get("ActiveState") == "active"
    require(not unit.get("DropInPaths") and (unit.get("LoadState") == "not-found"
            or unit.get("FragmentPath") == REDIS_UNIT_PATH or (claim["phase"] != "ready" and unused)),
            "redis_ownership", "The dedicated Redis unit resolves to an unowned fragment.")
    return {"state": "ready" if shared.get("state") == "ready" and active else "resume",
            "owner": "foundry", "unit": REDIS_UNIT, "port": REDIS_PORT, "aclfile": REDIS_ACLFILE,
            "ownership_token": shared["ownership_token"], "active": active}


def _read_acl(host: Host) -> str:
    path = check_path(host.paths.p(REDIS_ACLFILE), "file")
    require(path.exists(), "redis_ownership", "The owned Redis ACL file is missing.")
    info = path.stat()
    expected_uid = 0 if host.paths.root == Path("/") else os.geteuid()
    expected_gid = grp.getgrnam(REDIS_ACCOUNT).gr_gid if host.paths.root == Path("/") else os.getegid()
    require(stat.S_IMODE(info.st_mode) == 0o640 and info.st_uid == expected_uid and info.st_gid == expected_gid,
            "redis_ownership", "Redis ACL file must be root:foundry-redis 0640.")
    try:
        return path.read_bytes().decode("utf-8")
    except UnicodeError:
        raise DeployError("redis_ownership", "Redis ACL file is not valid UTF-8.") from None


def _admin_line(credentials: dict) -> str:
    return "user " + REDIS_ADMIN + " reset on #" + hashlib.sha256(credentials["redis_password"].encode()).hexdigest() + " ~* &* +@all"


def _verify_admin(host: Host) -> None:
    credentials = _host_secrets(host)
    lines = _read_acl(host).splitlines()
    for user, expected in (("default", "user default reset off"), (REDIS_ADMIN, _admin_line(credentials))):
        index = _acl_index(lines, {"allocations": {"redis_user": user}})
        require(index is not None and lines[index] == expected, "redis_ownership", "Redis administrator disk policy changed.")
    value = _redis(host, "ACL", "GETUSER", REDIS_ADMIN)
    require(isinstance(value, dict) and "on" in value.get("flags", [])
            and not set(value.get("flags", [])) & {"off", "nopass"}
            and value.get("passwords") == [hashlib.sha256(credentials["redis_password"].encode()).hexdigest()]
            and value.get("commands") == "+@all" and value.get("keys") == "~*"
            and value.get("channels") == "&*" and not value.get("selectors"),
            "redis_ownership", "Foundry Redis administrator permissions or credential changed.")
    default = _redis(host, "ACL", "GETUSER", "default")
    require(isinstance(default, dict) and "off" in default.get("flags", [])
            and "on" not in default.get("flags", []) and not default.get("passwords")
            and default.get("commands") == "-@all" and not default.get("keys")
            and not default.get("channels") and not default.get("selectors"),
            "redis_ownership", "Foundry Redis default user is not disabled and reset.")


def _verify_connection(host: Host) -> None:
    """Authenticate through a hostname-verified TLS connection using system roots."""
    credentials = _host_secrets(host)
    context = ssl.create_default_context(cafile=str(host.paths.p(ROOTS)))
    # A just-started unit may not listen yet; wait only for the listener.
    # Verification and authentication failures are never retried.
    deadline = time.monotonic() + 30
    while True:
        try:
            socket.create_connection(("localhost", REDIS_PORT), timeout=2).close()
            break
        except (ConnectionRefusedError, ConnectionResetError, socket.timeout):
            if time.monotonic() >= deadline:
                raise DeployError("redis_tls", "Foundry Redis did not start listening.") from None
            time.sleep(0.5)
    try:
        with socket.create_connection(("localhost", REDIS_PORT), timeout=10) as connection:
            with context.wrap_socket(connection, server_hostname="localhost") as encrypted:
                with encrypted.makefile("rb") as reader:
                    for parts, expected in ((("AUTH", REDIS_ADMIN, credentials["redis_password"]), b"+OK\r\n"),
                                            (("PING",), b"+PONG\r\n")):
                        encoded = [part.encode() for part in parts]
                        payload = b"*" + str(len(encoded)).encode() + b"\r\n"
                        for part in encoded:
                            payload += b"$" + str(len(part)).encode() + b"\r\n" + part + b"\r\n"
                        encrypted.sendall(payload)
                        require(reader.readline(1024) == expected, "redis_tls", "Foundry Redis TLS authentication probe failed.")
    except (OSError, ssl.SSLError):
        raise DeployError("redis_tls", "Foundry Redis must verify its hostname and system trust before use.") from None


def _redis_config(token: str, limits: dict) -> str:
    return f"""# Foundry-owned Redis: {token}
bind 127.0.0.1 ::1
protected-mode yes
port 0
tls-port {REDIS_PORT}
tls-cert-file {REDIS_CERT}
tls-key-file {REDIS_KEY}
tls-ca-cert-file {ROOTS}
tls-auth-clients no
aclfile {REDIS_ACLFILE}
dir {REDIS_DATA}
daemonize no
supervised systemd
logfile ""
appendonly yes
appendfsync everysec
save 900 1
save 300 10
save 60 10000
stop-writes-on-bgsave-error yes
maxmemory {limits["maxmemory_bytes"] // (1024 * 1024)}mb
maxmemory-policy noeviction
maxclients {limits["maxclients"]}
"""


def _verify_runtime(host: Host, limits: dict) -> None:
    _verify_connection(host)
    _verify_admin(host)
    expected = {"port": "0", "tls-port": str(REDIS_PORT), "bind": "127.0.0.1 ::1", "appendonly": "yes",
                "appendfsync": "everysec", "save": "900 1 300 10 60 10000", "maxmemory-policy": "noeviction",
                "maxmemory": str(limits["maxmemory_bytes"]), "maxclients": str(limits["maxclients"]), "dir": REDIS_DATA,
                "aclfile": REDIS_ACLFILE, "tls-auth-clients": "no", "stop-writes-on-bgsave-error": "yes"}
    value = _redis(host, "CONFIG", "GET", *expected)
    require(isinstance(value, dict) and all(value.get(key) == wanted for key, wanted in expected.items()),
            "redis_configuration", "Foundry Redis effective settings differ from the owned plan.")
    info = _redis(host, "INFO", "persistence", raw=True)
    require(isinstance(info, str), "redis_persistence", "Cannot inspect Foundry Redis persistence.")
    fields = dict(line.split(":", 1) for line in info.splitlines() if ":" in line and not line.startswith("#"))
    require(fields.get("aof_enabled") == "1" and fields.get("aof_last_write_status") == "ok"
            and fields.get("rdb_last_bgsave_status") == "ok", "redis_persistence", "Foundry Redis persistence is unhealthy.")


def prepare(host: Host, registry: dict) -> None:
    """Create/resume the reserved Redis service without changing any foreign unit."""
    from packages import install_missing, authorize_default_activation
    from postgres import prepare_tls, record_trust_probe

    observation = inspect(host, registry)
    require(observation["state"] != "conflict", "redis_ownership", "Redis allocation is occupied or unclaimed.")
    shared, claim = _service_claim(host, registry)
    if not shared and claim:
        shared = registry["shared"]["redis"] = claim["allocation"]
        host.journal(registry)
    if not shared:
        sized = capacity.redis(capacity.allocation_memory(host, "host_capacity"))
        token = str(uuid.uuid4())
        shared = registry["shared"]["redis"] = {
            "owner": "foundry", "state": "installing", "ownership_token": token,
            "unit": REDIS_UNIT, "port": REDIS_PORT, "aclfile": REDIS_ACLFILE,
            "admin_user": REDIS_ADMIN, "config_path": REDIS_CONFIG, "data_path": REDIS_DATA,
            "capacity": sized,
        }
        claim = {"host_id": registry["host_id"], "ownership_token": token, "phase": "reserved", "files": {}, "allocation": dict(shared)}
        _save_claim(host, claim)
        host.journal(registry)
    _owned_service(shared, claim)
    # A crash between private reservation and the public journal is recoverable
    # only by the matching reservation; unknown paths are never adopted.
    needed = [package for binary, package in (("redis-server", "redis-server"), ("redis-cli", "redis-tools"))
              if not host.paths.p("/usr/bin/" + binary).is_file()]
    if needed:
        install_missing(host, needed)
    if _package_template_pending(host, registry):
        authorize_default_activation(host, REDIS_TEMPLATE,
                                     {**shared, "installed_by_foundry": True, "unit": REDIS_TEMPLATE})
        inspect(host, registry)
    version = host.run(["/usr/bin/redis-server", "--version"]).stdout
    found = re.search(r"\bv=(\d+)\.(\d+)\.(\d+)\b", version)
    require(found and int(found[1]) >= 7, "redis_version", "Foundry requires Redis 7 or newer with TLS and ACL DRYRUN support.")
    help_text = host.run(["redis-cli", "--help"]).stdout
    require("--tls" in help_text and "--sni" in help_text, "redis_capability", "Installed Redis CLI lacks required TLS capabilities.")
    tls = prepare_tls(host, registry)
    marker = "foundry-host=" + registry["host_id"]
    account = host.run(["getent", "passwd", REDIS_ACCOUNT], check=False)
    require(account.returncode in (0, 2), "redis_account", "Cannot inspect Foundry Redis account.")
    if account.returncode == 0:
        fields = account.stdout.strip().split(":")
        require(len(fields) == 7 and fields[4] == marker and fields[5] == REDIS_DATA and fields[6] == "/usr/sbin/nologin",
                "redis_account", "Foundry Redis account is occupied by a different owner.")
    else:
        require(shared["state"] != "ready", "redis_account", "The acknowledged Redis service account disappeared.")
        group = host.run(["getent", "group", REDIS_ACCOUNT], check=False)
        require(group.returncode == 2, "redis_account", "Foundry Redis group exists without its owned account.")
        host.run(["useradd", "--system", "--user-group", "--home-dir", REDIS_DATA, "--no-create-home",
                  "--shell", "/usr/sbin/nologin", "--comment", marker, REDIS_ACCOUNT])
    if observation["state"] == "ready":
        _verify_runtime(host, shared["capacity"])
        record_trust_probe(host, registry, "redis")
        return
    secret_path = check_path(host.paths.etc / "host-secrets.json", "file")
    secrets_value = {}
    if secret_path.exists():
        info = secret_path.stat()
        require(stat.S_IMODE(info.st_mode) == 0o600 and (info.st_uid == 0 or host.paths.root != Path("/")),
                "redis_credentials_invalid", "Host credentials must be root-owned and private.")
        secrets_value = read_json(secret_path)
        require(isinstance(secrets_value, dict), "redis_credentials_invalid", "Host credentials have an invalid shape.")
    if "redis_user" not in secrets_value and "redis_password" not in secrets_value:
        require(not claim.get("credentials_installed"), "redis_credentials_missing", "Restore the persisted Redis administrator credential.")
        secrets_value.update(redis_user=REDIS_ADMIN, redis_password=secrets.token_urlsafe(48))
        host.owned_write(secret_path, json.dumps(secrets_value, sort_keys=True) + "\n", mode=0o600)
    credentials = _host_secrets(host)
    claim["credentials_installed"] = True
    _save_claim(host, claim)
    for absolute, owner, group in (("/etc/foundry/redis", "root", REDIS_ACCOUNT), (REDIS_DATA, REDIS_ACCOUNT, REDIS_ACCOUNT)):
        path = check_path(host.paths.p(absolute), "directory")
        host.run(["install", "-d", "-m", "0750", "-o", owner, "-g", group, str(path)])
    template = Path(__file__).resolve().parents[1] / "server/templates/redis-server@foundry.service"
    unit_text = template.read_text().replace("@OWNERSHIP_TOKEN@", shared["ownership_token"])
    files = {REDIS_CONFIG: _redis_config(shared["ownership_token"], shared["capacity"]), REDIS_UNIT_PATH: unit_text,
             REDIS_CERT: Path(tls["cert"]).read_text(), REDIS_KEY: Path(tls["key"]).read_text()}
    _claimed_files(host, claim)
    for absolute, content in files.items():
        digest = hashlib.sha256(content.encode()).hexdigest()
        previous = claim["files"].get(absolute)
        require(previous is None or previous == digest, "redis_service_plan", "Redis certificate or settings changes require an explicit shared-service plan.")
        path = check_path(host.paths.p(absolute), "file")
        require(not path.exists() or previous == digest, "redis_ownership", "Unclaimed Redis service file already exists.")
        claim["files"][absolute] = digest
    claim["phase"] = "publishing"
    _save_claim(host, claim)
    for absolute, content in files.items():
        host.owned_write(host.paths.p(absolute), content, mode=0o640 if absolute in (REDIS_CONFIG, REDIS_KEY) else 0o644,
                         group=REDIS_ACCOUNT if absolute in (REDIS_CONFIG, REDIS_KEY) else "root")
    aclpath = check_path(host.paths.p(REDIS_ACLFILE), "file")
    initial_acl = "user default reset off\n" + _admin_line(credentials) + "\n"
    if not aclpath.exists():
        require(not claim.get("acl_installed"), "redis_ownership", "The persistent Redis ACL file disappeared.")
        host.owned_write(aclpath, initial_acl, mode=0o640, group=REDIS_ACCOUNT)
    else:
        lines = aclpath.read_text().splitlines()
        require(lines.count("user default reset off") == 1 and lines.count(_admin_line(credentials)) == 1
                and (claim.get("acl_installed") or aclpath.read_bytes() == initial_acl.encode()),
                "redis_ownership", "Foundry Redis administrator ACL ownership mismatch.")
    claim["acl_installed"] = True
    claim["phase"] = "starting"
    _save_claim(host, claim)
    if _unit(host).get("ActiveState") != "active":
        require(not _port_busy(host), "redis_port_conflict", "The reserved Redis TLS port is occupied.")
        host.run(["systemctl", "daemon-reload"])
        host.run(["systemctl", "enable", "--now", REDIS_UNIT])
    _verify_runtime(host, shared["capacity"])
    record_trust_probe(host, registry, "redis")
    claim["phase"] = "ready"
    _save_claim(host, claim)
    shared["state"] = "ready"
    shared["version"] = ".".join(found.groups())
    host.journal(registry)


def provision(host: Host, registry: dict, entry: dict, credentials: dict) -> None:
    """Publish an empty-grant owned user, with private intent before disk/live."""
    shared = registry["shared"]["redis"]
    require(shared.get("owner") == "foundry" and shared.get("state") == "ready"
            and shared.get("port") == REDIS_PORT and shared.get("aclfile") == REDIS_ACLFILE,
            "redis_ownership", "Redis instance provisioning requires the ready owned service.")
    password = credentials["redis_password"]
    path = check_path(host.paths.p(REDIS_ACLFILE), "file")
    content = _read_acl(host)
    state = _load_state(host, entry)
    pending = state.get("pending")
    committed = state.get("committed")
    live = _redis(host, "ACL", "GETUSER", entry["allocations"]["redis_user"])
    owned = _acl_owned(content, entry, password, state)
    if committed:
        require(owned, "redis_ownership", "An acknowledged Redis ACL declaration disappeared.")
        if pending:
            reconcile_redis_grants(host, registry, entry, pending["patterns"], channels=pending["channels"])
            return
        require(_validate_redis_live(live, entry, password, host, [committed]), "redis_ownership", "An acknowledged Redis user disappeared.")
        _redis_scope_probes(host, entry, committed["patterns"], committed["channels"])
    else:
        require("redis" not in entry["provisioning"].get("completed_steps", []), "redis_ownership", "An acknowledged Redis ownership journal disappeared.")
        require(not live or (pending is not None and owned), "redis_ownership", "Existing Redis user has no persistent claim.")
        target = _policy(entry, password, [], [])
        require(pending is None or pending == target, "redis_ownership", "Unexpected initial Redis grant transition.")
        if live:
            _validate_redis_live(live, entry, password, host, [target])
        state["pending"] = target
        state["phase"] = "provision_intent"
        _save_state(host, entry, state)
        if not owned:
            content += ("" if not content or content.endswith("\n") else "\n") + _acl_line(entry, password, [], []) + "\n"
            host.owned_write(path, content, mode=0o640, group=REDIS_ACCOUNT)
        state["phase"] = "provision_disk"
        _save_state(host, entry, state)
        require(_redis(host, "ACL", "SETUSER", entry["allocations"]["redis_user"], *redis_rules(entry, password, [], channels=[])) == "OK",
                "redis_acl", "Redis refused the initial instance ACL policy.")
        require(_validate_redis_live(_redis(host, "ACL", "GETUSER", entry["allocations"]["redis_user"]), entry, password, host, [target]),
                "redis_acl", "Redis did not apply the initial instance ACL policy.")
        _redis_scope_probes(host, entry, [], [])
        state["committed"] = target
        state.pop("pending", None)
        state["phase"] = "committed"
        _save_state(host, entry, state)
    if "redis" not in entry["provisioning"].setdefault("completed_steps", []):
        entry["provisioning"]["completed_steps"].append("redis")
        host.journal(registry, entry)


def _validate_prefix(entry: dict, prefix: str) -> None:
    namespace = entry["allocations"]["namespace"]
    match = REDIS_PREFIX.fullmatch(prefix) if isinstance(prefix, str) else None
    require(match is not None and not REDIS_GLOB.search(prefix)
            and match["environment"] == namespace["environment"]
            and re.fullmatch(r"[a-z][a-z0-9-]*(?:\.[a-z][a-z0-9-]*)*", match["application"])
            and (match["application"] == namespace["application"]
                 or match["application"].startswith(namespace["application"] + ".")),
            "redis_inventory", "Redis inventory contains a malformed or foreign namespace prefix.")


def _inventory_patterns(entry: dict, inventory: list, kind: str) -> list[str]:
    require(isinstance(inventory, list) and len(inventory) <= MAX_REDIS_PATTERNS
            and len(json.dumps(inventory, ensure_ascii=True).encode()) <= MAX_JSON,
            "redis_inventory", "Redis inventory is malformed or exceeds its bound.")
    prefixes = {"keys": set(), "channels": set()}
    connections = set()
    for connection in inventory:
        require(isinstance(connection, dict) and isinstance(connection.get("connection"), str)
                and connection["connection"] and connection["connection"] not in connections
                and isinstance(connection.get("keys"), list) and isinstance(connection.get("channels"), list),
                "redis_inventory", "Redis inventory contains malformed or duplicate connections.")
        connections.add(connection["connection"])
        for field in prefixes:
            require(len(connection[field]) <= MAX_REDIS_PATTERNS, "redis_inventory", "Redis inventory exceeds its bound.")
            for item in connection[field]:
                prefix = item.get("prefix") if isinstance(item, dict) else None
                _validate_prefix(entry, prefix)
                prefixes[field].add(prefix)
            require(len(prefixes[field]) <= MAX_REDIS_PATTERNS, "redis_inventory", "Redis inventory exceeds its bound.")
    return sorted(prefix + "*" for prefix in prefixes[kind])


def redis_grant_patterns(entry: dict, inventory: list) -> list[str]:
    """Validate artisan redis:keys inventory and return exact key ACL patterns."""
    return _inventory_patterns(entry, inventory, "keys")


def redis_channel_patterns(entry: dict, inventory: list) -> list[str]:
    """Validate artisan redis:keys inventory and return exact channel ACL patterns."""
    return _inventory_patterns(entry, inventory, "channels")


def granted_redis_patterns(entry: dict) -> list[str]:
    """Render committed literal key prefixes; new instances have no grants."""
    return [prefix + "*" for prefix in entry.get("redis_grants", {}).get("keys", [])]


def granted_redis_channels(entry: dict) -> list[str]:
    """Render committed literal channel prefixes; no wildcard fallback exists."""
    return [prefix + "*" for prefix in entry.get("redis_grants", {}).get("channels", [])]


def _validated_patterns(entry: dict, patterns: Sequence[str]) -> list[str]:
    require(not isinstance(patterns, (str, bytes)) and len(patterns) <= 2 * MAX_REDIS_PATTERNS,
            "redis_inventory", "Redis patterns exceed their bound or have an invalid shape.")
    for pattern in patterns:
        require(isinstance(pattern, str) and pattern.endswith("*"), "redis_inventory", "Invalid Redis ACL pattern.")
        _validate_prefix(entry, pattern[:-1])
    return sorted(set(patterns))


def redis_rules(entry: dict, password: str, patterns: Sequence[str] | None = None,
                *, channels: Sequence[str] | None = None) -> list[str]:
    """Render exact key/channel grants and the versioned explicit command policy."""
    patterns = _validated_patterns(entry, granted_redis_patterns(entry) if patterns is None else patterns)
    channels = _validated_patterns(entry, granted_redis_channels(entry) if channels is None else channels)
    return ["reset", "on", "#" + hashlib.sha256(password.encode()).hexdigest(),
            *["~" + pattern for pattern in patterns], "resetchannels", *["&" + pattern for pattern in channels],
            "-@all", *["+" + command for command in sorted(REDIS_COMMANDS)]]


def _acl_line(entry: dict, password: str, patterns=None, channels=None) -> str:
    return "user " + entry["allocations"]["redis_user"] + " " + " ".join(redis_rules(entry, password, patterns, channels=channels))


def _acl_index(lines: list[str], entry: dict) -> int | None:
    user = entry["allocations"]["redis_user"]
    matches = [i for i, line in enumerate(lines) if re.match(r"^\s*user\s+" + re.escape(user) + r"\s", line)]
    require(len(matches) <= 1, "redis_ownership", "Duplicate Redis ACL user declarations.")
    return matches[0] if matches else None


def _state_path(host: Host, entry: dict) -> Path:
    directory = host.paths.p(instance_paths(entry["name"])["private"])
    host.check_control_directory(directory)
    return check_path(directory / "redis-acl.json", "file")


def _load_state(host: Host, entry: dict) -> dict:
    path = _state_path(host, entry)
    if not path.exists():
        return {"instance_id": entry["profile"]["instance_id"]}
    info = path.stat()
    require(not info.st_mode & 0o077 and (info.st_uid == 0 or host.paths.root != Path("/")),
            "redis_ownership", "Redis ACL journal must be root-owned and private.")
    state = read_json(path)
    require(isinstance(state, dict) and state.get("instance_id") == entry["profile"]["instance_id"],
            "redis_ownership", "Redis ACL journal identity mismatch.")
    return state


def _save_state(host: Host, entry: dict, state: dict) -> None:
    path = _state_path(host, entry)
    path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    require(not path.parent.stat().st_mode & 0o077, "redis_ownership", "Redis journal directory must be private.")
    host.owned_write(path, json.dumps(state, sort_keys=True, indent=2) + "\n", mode=0o600)


def _acl_owned(text: str, entry: dict, password: str, state: dict) -> bool:
    lines = text.splitlines()
    index = _acl_index(lines, entry)
    if index is None:
        return False
    for candidate in (state.get("committed"), state.get("pending")):
        if candidate is None:
            continue
        expected = _acl_line(entry, password, candidate["patterns"], candidate["channels"])
        if lines[index] == expected and candidate.get("sha256") == hashlib.sha256(expected.encode()).hexdigest():
            return True
    raise DeployError("redis_ownership", "Redis persistent user ownership or policy mismatch.")


def _host_secrets(host: Host) -> dict:
    path = check_path(host.paths.etc / "host-secrets.json", "file")
    require(path.exists(), "redis_credentials_missing", "Restore the host administrator credential file.")
    info = path.stat()
    require(not info.st_mode & 0o077 and (info.st_uid == 0 or host.paths.root != Path("/")),
            "redis_credentials_invalid", "Host credentials must be root-owned and private.")
    value = read_json(path)
    require(isinstance(value, dict) and value.get("redis_user") == REDIS_ADMIN
            and isinstance(value.get("redis_password"), str)
            and re.fullmatch(r"[A-Za-z0-9_-]{32,128}", value["redis_password"]),
            "redis_credentials_invalid", "Invalid host administrator credential file.")
    return value


def _redis(host: Host, *args: str, allow_denied: bool = False, raw: bool = False, input: str | None = None):
    # raw: INFO replies are verbatim strings that redis-cli --json prints as
    # plain text on Redis 7.0, so they are read unformatted instead.
    credentials = _host_secrets(host)
    command = ["redis-cli", "-3", "--no-auth-warning", *([] if raw else ["--json"]), "--tls", "--cacert", ROOTS,
               "--sni", "localhost", "-h", "localhost", "-p", str(REDIS_PORT), "--user", credentials["redis_user"]]
    # Binary snapshot writes are hex-encoded on private stdin, never process arguments.
    options = {"input": input} if input is not None else {}
    result = host.run(command + (["-x"] if input is not None else []) + list(args),
                      env={"REDISCLI_AUTH": credentials["redis_password"]}, timeout=15, **options)
    if raw:
        text = result.stdout.decode() if isinstance(result.stdout, bytes) else (result.stdout or "")
        require(not text.startswith("(error)") and not text.startswith("ERR"), "redis_inspection",
                "Redis administration refused; check administrator access.")
        return text
    try:
        value = json.loads(result.stdout)
    except (ValueError, TypeError):
        raise DeployError("redis_inspection", "Redis command did not return valid JSON.") from None
    if isinstance(value, dict) and "error" in value and not allow_denied:
        raise DeployError("redis_inspection", "Redis administration refused; check administrator access.")
    return value


def _redis_patterns_match(wire, rules, prefix):
    return isinstance(wire, str) and set(wire.split()) == {rule for rule in rules if rule.startswith(prefix)}


def _redis_commands_match(host, wire):
    if not isinstance(wire, str):
        return False
    tokens = wire.split()
    explicit = {"-@all"} | {"+" + command for command in REDIS_COMMANDS}
    if tokens and tokens[0] == "-@all" and set(tokens) == explicit:
        return True
    if host is None or not tokens or tokens[0] != "-@all":
        return False
    # Redis 7's ACLDescribeSelectorCommandRules compresses a command bitmap to
    # categories plus corrections. Compare permissions, not that display text.
    inventory = _redis(host, "COMMAND", "LIST")
    if not isinstance(inventory, list) or not all(isinstance(item, str) for item in inventory):
        raise DeployError("redis_command_inventory", "Redis command inventory cannot be inspected.")
    inventory = set(inventory)
    selected = set()
    for token in tokens[1:]:
        if token[:1] not in {"+", "-"}:
            return False
        name = token[1:]
        if name == "@all":
            return False  # future/module grants must remain disabled
        if name.startswith("@"):
            commands = _redis(host, "ACL", "CAT", name[1:])
            if not isinstance(commands, list) or not all(isinstance(item, str) for item in commands):
                return False
        elif name in inventory:
            commands = [name]
        else:
            return False
        expanded = set(commands)
        for command in commands:
            expanded.update(item for item in inventory if item.startswith(command + "|"))
        if token[0] == "+":
            selected.update(expanded)
        else:
            selected.difference_update(expanded)
    return selected == REDIS_COMMANDS



def _validate_redis_live(value, entry: dict, password: str, host: Host | None = None,
                         candidates: Sequence[dict] | None = None) -> bool:
    """Match the live policy against explicit, durably recorded candidate grants."""
    if value is None:
        return False
    require(isinstance(value, dict), "redis_ownership", "Redis ACL inspection unavailable.")
    flags = set(value.get("flags", []))
    if candidates is None:
        candidates = [{"patterns": granted_redis_patterns(entry), "channels": granted_redis_channels(entry)}]
    for candidate in candidates:
        rules = redis_rules(entry, password, candidate["patterns"], channels=candidate["channels"])
        if not ("on" not in flags or flags & {"off", "nopass", "allkeys", "allchannels"}
                or value.get("passwords") != [next(rule[1:] for rule in rules if rule.startswith("#"))]
                or not _redis_patterns_match(value.get("keys"), rules, "~")
                or not _redis_patterns_match(value.get("channels"), rules, "&") or value.get("selectors")
                or not _redis_commands_match(host, value.get("commands", ""))):
            return True
    raise DeployError("redis_ownership", "Redis live user credential or policy mismatch.")


def _redis_scope_probes(host: Host, entry: dict, patterns: Sequence[str], channels: Sequence[str]) -> None:
    """Prove the selected union/target and reject explicit ACL permission denials."""
    namespace = entry["allocations"]["namespace"]
    application, environment = namespace["application"], namespace["environment"]
    user = entry["allocations"]["redis_user"]
    denied = [("FLUSHDB",), ("FLUSHALL",), ("KEYS", "*"), ("SCAN", "0"), ("CONFIG", "GET", "*"),
              ("ACL", "WHOAMI"), ("SCRIPT", "FLUSH"), ("PUBLISH", "foreign", "probe"),
              ("GET", f"foundry:cache:v1:{application}-other:{environment}:probe")]
    for pattern in set(patterns) | set(channels):
        matched = REDIS_PREFIX.fullmatch(pattern[:-1])
        feature, owner = matched["feature"], matched["application"]
        for key in (f"foundry:{feature}:v1:{application}-other:{environment}:probe",
                    f"foundry:{feature}:v1:{owner}:other-{environment}:probe",
                    f"foundry:{feature}:v1:{owner}:other-{environment}:{environment}:probe",
                    f"foundry:{feature}:v1:{application}-other.{owner}:{environment}:probe"):
            denied.extend((("GET", key), ("PUBLISH", key, "probe")))
    for command in denied:
        result = _redis(host, "ACL", "DRYRUN", user, *command, allow_denied=True)
        denial = result.get("error") if isinstance(result, dict) else result
        # Redis 7 says "This user has no permissions"; Redis 8 names the probed user.
        require(isinstance(denial, str) and (denial == "NOPERM" or denial.startswith("NOPERM ")
                or denial.startswith(("This user has no permissions ", f"User {user} has no permissions "))),
                "redis_acl", "Redis scope probe must prove a permission denial, not a connection/command error.")
    for pattern in patterns:
        require(_redis(host, "ACL", "DRYRUN", user, "GET", pattern[:-1] + "probe", allow_denied=True) == "OK",
                "redis_acl", "Redis instance ACL is missing a required key prefix.")
        require(_redis(host, "ACL", "DRYRUN", user, "SET", pattern[:-1] + "probe", "probe", allow_denied=True) == "OK",
                "redis_acl", "Redis instance ACL is missing required cache writes.")
    for pattern in channels:
        for command in (("PUBLISH", pattern[:-1] + "probe", "probe"), ("SUBSCRIBE", pattern[:-1] + "probe")):
            require(_redis(host, "ACL", "DRYRUN", user, *command, allow_denied=True) == "OK",
                    "redis_acl", "Redis instance ACL is missing a required channel prefix.")


def _policy(entry: dict, password: str, patterns: list[str], channels: list[str]) -> dict:
    line = _acl_line(entry, password, patterns, channels)
    return {"patterns": patterns, "channels": channels, "sha256": hashlib.sha256(line.encode()).hexdigest()}


def _inventory(policy: dict) -> dict:
    return {"keys": [item[:-1] for item in policy["patterns"]], "channels": [item[:-1] for item in policy["channels"]]}


def _publish_policy(host: Host, entry: dict, path: Path, content: str, password: str, target: dict) -> None:
    lines = content.splitlines(keepends=True)
    index = _acl_index(lines, entry)
    require(index is not None, "redis_ownership", "The owned Redis user declaration disappeared.")
    ending = "\r\n" if lines[index].endswith("\r\n") else "\n" if lines[index].endswith("\n") else ""
    lines[index] = _acl_line(entry, password, target["patterns"], target["channels"]) + ending
    host.owned_write(path, "".join(lines), mode=0o640, owner="root", group=REDIS_ACCOUNT)


def reconcile_redis_grants(host: Host, registry: dict, entry: dict, patterns: Sequence[str],
                           *, channels: Sequence[str] | None = None) -> bool:
    """Converge one user under host/instance locks, preserving all unrelated bytes.

    Lifecycle supplies the running/candidate union before activation and selected
    inventory after every role is ready. Each call has its own durable intent;
    failures during narrowing retain an explicit cleanup obligation for the lifecycle owner.
    """
    patterns = _validated_patterns(entry, patterns)
    channels = _validated_patterns(entry, granted_redis_channels(entry) if channels is None else channels)
    require("redis" in entry["provisioning"].get("completed_steps", []),
            "bootstrap_incomplete", "Redis grants change only after instance provisioning.")
    shared = registry["shared"]["redis"]
    require(shared.get("owner") == "foundry" and shared.get("port") == REDIS_PORT
            and shared.get("aclfile") == REDIS_ACLFILE,
            "redis_ownership", "Only the dedicated Foundry Redis ACL may be changed.")
    password = read_credentials(host, entry)["redis_password"]
    path = check_path(host.paths.p(REDIS_ACLFILE), "file")
    content = _read_acl(host)
    state = _load_state(host, entry)
    require(_acl_owned(content, entry, password, state), "redis_ownership", "Redis user has no owned persistent declaration.")
    pending = state.get("pending")
    if pending:
        require(pending["patterns"] == patterns and pending["channels"] == channels,
                "redis_acl_pending", "Resume the pending Redis grant transition before changing its target.")
    candidates = [candidate for candidate in (state.get("committed"), pending) if candidate]
    require(any(candidate["patterns"] == sorted(granted_redis_patterns(entry))
                and candidate["channels"] == sorted(granted_redis_channels(entry)) for candidate in candidates),
            "redis_ownership", "Public Redis grants differ from every durably recorded policy.")
    require(_validate_redis_live(_redis(host, "ACL", "GETUSER", entry["allocations"]["redis_user"]),
                                entry, password, host, candidates),
            "redis_ownership", "The acknowledged Redis user disappeared.")
    if not pending and patterns == sorted(granted_redis_patterns(entry)) and channels == sorted(granted_redis_channels(entry)):
        _redis_scope_probes(host, entry, patterns, channels)
        return False
    target = _policy(entry, password, patterns, channels)
    if not pending:
        old = state["committed"]
        narrowing = bool(set(old["patterns"]) - set(patterns) or set(old["channels"]) - set(channels))
        operation = str(uuid.uuid4())
        state["transition"] = {
            "operation_id": operation, "credential_identity": hashlib.sha256(password.encode()).hexdigest(),
            "old_inventory": _inventory(old), "candidate_inventory": _inventory(target),
            "widened_union": {"keys": sorted(set(_inventory(old)["keys"]) | set(_inventory(target)["keys"])),
                              "channels": sorted(set(_inventory(old)["channels"]) | set(_inventory(target)["channels"]))},
            "final_target": _inventory(target), "old_line_sha256": old["sha256"], "new_line_sha256": target["sha256"],
            "phase": "narrow_intent" if narrowing else "widen_intent", "narrowing": narrowing,
        }
        state["pending"] = target
        _save_state(host, entry, state)
    transition = state["transition"]
    entry["redis_grants"]["pending_operation"] = transition["operation_id"]
    if transition["narrowing"]:
        entry.setdefault("cleanup_pending", {})["redis_acl"] = {"operation_id": transition["operation_id"]}
    host.journal(registry, entry)
    _publish_policy(host, entry, path, content, password, target)
    transition["phase"] = "disk_published"
    _save_state(host, entry, state)
    require(_redis(host, "ACL", "SETUSER", entry["allocations"]["redis_user"],
                   *redis_rules(entry, password, patterns, channels=channels)) == "OK",
            "redis_acl", "Redis refused the instance ACL policy.")
    transition["phase"] = "live_applied"
    _save_state(host, entry, state)
    require(_validate_redis_live(_redis(host, "ACL", "GETUSER", entry["allocations"]["redis_user"]),
                                 entry, password, host, [target]),
            "redis_acl", "Redis did not apply the instance ACL policy.")
    _redis_scope_probes(host, entry, patterns, channels)
    transition["phase"] = "verified"
    _save_state(host, entry, state)
    entry["redis_grants"].update(_inventory(target), pending_operation=None)
    entry.get("cleanup_pending", {}).pop("redis_acl", None)
    host.journal(registry, entry)
    state["committed"] = target
    state.pop("pending", None)
    transition["phase"] = "committed"
    _save_state(host, entry, state)
    return True
