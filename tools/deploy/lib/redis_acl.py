"""Exact, journaled grants for the dedicated Foundry Redis instance (lane F2).

The grant engine is inherited from ooo's corrected disk-before-live reconciler.
Public registry entries contain literal prefixes; credential-derived ACL digests
and transition intent belong exclusively to each instance's private journal.
"""
from __future__ import annotations

from collections.abc import Sequence
import hashlib
import json
from pathlib import Path
import re
from typing import TYPE_CHECKING

from contracts import (DeployError, REDIS_PORT, REDIS_ACCOUNT, REDIS_ADMIN,
                       REDIS_ACLFILE, check_path, instance_paths, read_json, require)
from runtime_config import read_credentials
if TYPE_CHECKING:
    from host import Host

ROOTS = "/etc/ssl/certs/ca-certificates.crt"
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


def inspect(host: Host, registry: dict) -> dict:
    """Inspect only the dedicated Foundry Redis unit, TLS and persistent ACL state.

    Return public planning findings. Foreign instances (including passwordless
    port 6379) are never authenticated to, adopted, modified, or restarted.
    """
    raise NotImplementedError("F2: dedicated Redis inspection")


def prepare(host: Host, registry: dict) -> None:
    """Prepare the owned Redis instance on TLS 56379 using guarded package installs.

    Journal the administrator credential before service creation; enable AOF,
    explicit RDB, noeviction, default off, and the dedicated account/data paths.
    """
    raise NotImplementedError("F2: dedicated Redis preparation")


def provision(host: Host, registry: dict, entry: dict, credentials: dict) -> None:
    """Create this instance's UUID-named ACL user with initially empty grants.

    Persist its line digest via _save_state before disk then live publication;
    reuse owned credentials, verify exact permissions and record completed_steps.
    Resume pending transitions through reconcile_redis_grants under both locks.
    """
    raise NotImplementedError("F2: dedicated Redis user provisioning")


def _validate_prefix(entry: dict, prefix: str) -> None:
    namespace = entry["allocations"]["namespace"]
    match = REDIS_PREFIX.fullmatch(prefix) if isinstance(prefix, str) else None
    require(match is not None and not REDIS_GLOB.search(prefix)
            and match["environment"] == namespace["environment"]
            and (match["application"] == namespace["application"]
                 or match["application"].startswith(namespace["application"] + ".")),
            "redis_inventory", "Redis inventory contains a malformed or foreign namespace prefix.")


def _inventory_patterns(entry: dict, inventory: list, kind: str) -> list[str]:
    require(isinstance(inventory, list) and len(inventory) <= MAX_REDIS_PATTERNS,
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
    require(not isinstance(patterns, (str, bytes)) and len(patterns) <= MAX_REDIS_PATTERNS,
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


def _redis(host: Host, *args: str, allow_denied: bool = False):
    credentials = _host_secrets(host)
    command = ["redis-cli", "-3", "--no-auth-warning", "--json", "--tls", "--cacert", ROOTS,
               "-h", "localhost", "-p", str(REDIS_PORT), "--user", credentials["redis_user"]]
    result = host.run(command + list(args), env={"REDISCLI_AUTH": credentials["redis_password"]}, timeout=15)
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
        if not ("on" not in flags or flags & {"nopass", "allkeys", "allchannels"}
                or value.get("passwords") != [next(rule[1:] for rule in rules if rule.startswith("#"))]
                or not _redis_patterns_match(value.get("keys"), rules, "~")
                or not _redis_patterns_match(value.get("channels"), rules, "&") or value.get("selectors")
                or not _redis_commands_match(host, value.get("commands", ""))):
            return True
    raise DeployError("redis_ownership", "Redis live user credential or policy mismatch.")


def _redis_scope_probes(host: Host, entry: dict, patterns: Sequence[str], channels: Sequence[str]) -> None:
    """Prove every granted prefix and deny foreign/crafted names and global commands."""
    namespace = entry["allocations"]["namespace"]
    application, environment = namespace["application"], namespace["environment"]
    user = entry["allocations"]["redis_user"]
    denied = [("FLUSHDB",), ("KEYS", "*"), ("SCAN", "0"), ("CONFIG", "GET", "*"),
              ("GET", f"foundry:jobs:v1:{application}-other:{environment}:key"),
              ("GET", f"foundry:jobs:v1:{application}.other:staging:{environment}:key"),
              ("PUBLISH", "foreign", "probe")]
    for command in denied:
        require(_redis(host, "ACL", "DRYRUN", user, *command, allow_denied=True) != "OK",
                "redis_acl", "Redis instance ACL permits an out-of-scope operation.")
    for pattern in patterns:
        require(_redis(host, "ACL", "DRYRUN", user, "GET", pattern[:-1] + "probe", allow_denied=True) == "OK",
                "redis_acl", "Redis instance ACL is missing a required key prefix.")
    for pattern in channels:
        require(_redis(host, "ACL", "DRYRUN", user, "PUBLISH", pattern[:-1] + "probe", "probe", allow_denied=True) == "OK",
                "redis_acl", "Redis instance ACL is missing a required channel prefix.")


def reconcile_redis_grants(host: Host, registry: dict, entry: dict, patterns: Sequence[str],
                           *, channels: Sequence[str] | None = None) -> bool:
    """Converge this instance to exact grants under the host and instance locks.

    Return whether anything changed. Durable private intent precedes disk, then
    ACL SETUSER, verification, and public commitment. A pending target must be
    resumed before starting a different transition. All unrelated ACL bytes stay
    unchanged; neither ACL LOAD nor ACL SAVE is used. None preserves channels.
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
    content = path.read_bytes().decode("utf-8")
    state = _load_state(host, entry)
    require(_acl_owned(content, entry, password, state), "redis_ownership", "Redis user has no owned persistent declaration.")
    pending = state.get("pending")
    if pending:
        require(pending["patterns"] == patterns and pending["channels"] == channels,
                "redis_acl_pending", "Resume the pending Redis grant transition before changing its target.")
    candidates = [candidate for candidate in (state.get("committed"), pending) if candidate]
    live_matches = _validate_redis_live(_redis(host, "ACL", "GETUSER", entry["allocations"]["redis_user"]),
                                        entry, password, host, candidates)
    if live_matches and not pending and patterns == sorted(granted_redis_patterns(entry)) and channels == sorted(granted_redis_channels(entry)):
        return False
    line = _acl_line(entry, password, patterns, channels)
    target = {"patterns": patterns, "channels": channels, "sha256": hashlib.sha256(line.encode()).hexdigest()}
    state["pending"] = target
    _save_state(host, entry, state)
    lines = content.splitlines(keepends=True)
    index = _acl_index(lines, entry)
    ending = "\r\n" if lines[index].endswith("\r\n") else "\n" if lines[index].endswith("\n") else ""
    lines[index] = line + ending
    host.owned_write(path, "".join(lines), mode=0o640, owner="root", group=REDIS_ACCOUNT)
    require(_redis(host, "ACL", "SETUSER", entry["allocations"]["redis_user"],
                   *redis_rules(entry, password, patterns, channels=channels)) == "OK",
            "redis_acl", "Redis refused the instance ACL policy.")
    require(_validate_redis_live(_redis(host, "ACL", "GETUSER", entry["allocations"]["redis_user"]),
                                 entry, password, host, [target]),
            "redis_acl", "Redis did not apply the instance ACL policy.")
    _redis_scope_probes(host, entry, patterns, channels)
    entry["redis_grants"].update(keys=[pattern[:-1] for pattern in patterns], channels=[pattern[:-1] for pattern in channels])
    host.journal(registry, entry)
    state["committed"] = target
    state.pop("pending", None)
    _save_state(host, entry, state)
    return True
