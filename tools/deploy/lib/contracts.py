"""Deployment wire contracts and identity. Shared by local and installed tools.

Only Python's standard library is used. Application configuration continues to
belong to Go; these contracts describe deployment resources, not application DTOs.
"""
from __future__ import annotations

import grp
import pwd
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import tempfile
from datetime import datetime, timezone
import uuid

PREFIX = "foundry"
SCHEMA = 1
LAYOUT_VERSION = 2
REGISTRY_SCHEMA = 2
PROFILE_SCHEMA = 2
PORT_FIRST, PORT_LAST = 8100, 8999
MAX_JSON = 4 * 1024 * 1024
VERSION_PATTERN = re.compile(r"[0-9]{8}T[0-9]{6}Z-[a-f0-9]{8,40}")
SLUG_PATTERN = re.compile(r"[a-z][a-z0-9]*(?:-[a-z0-9]+)*")

POSTGRES_PORT = 55432
REDIS_PORT = 56379
REDIS_ACCOUNT = PREFIX + "-redis"
REDIS_ADMIN = PREFIX + "-admin"
REDIS_UNIT = "redis-server@" + PREFIX + ".service"
REDIS_ACLFILE = f"/etc/{PREFIX}/redis/users.acl"
HOST_PATHS = {
    "registry": f"/etc/{PREFIX}/registry.json",
    "secrets": f"/etc/{PREFIX}/host-secrets.json",
    "pki": f"/etc/{PREFIX}/pki",
    "cloudflare": f"/etc/{PREFIX}/cloudflare",
    "state": f"/var/lib/{PREFIX}",
    "private": f"/var/lib/{PREFIX}/private",
    "upgrades": f"/var/lib/{PREFIX}/upgrades",
    "reports": f"/var/lib/{PREFIX}/reports/host-v2.json",
    "locks": f"/run/lock/{PREFIX}",
    "tools": f"/usr/local/lib/{PREFIX}",
    "current": f"/usr/local/lib/{PREFIX}/current",
    "launcher": f"/usr/local/sbin/{PREFIX}-host",
    "report": f"/usr/local/bin/{PREFIX}-host-report",
}


def unit_name(name: str, role: str) -> str:
    """Canonical layout-v2 unit spelling; topology owns role selection."""
    instance_paths(name)
    require(role in {"http", "scheduler", "worker"}, "invalid_topology", "Unknown service role.")
    return f"{PREFIX}-{role}@{name}.service"


def marker(entry: dict) -> str:
    """UUID ownership marker, independent of the Linux account spelling."""
    return f"{PREFIX}:host={entry['host_id']}:project={entry['project_id']}:instance={entry['profile']['instance_id']}"


class DeployError(Exception):
    def __init__(self, code: str, detail: str):
        self.code, self.detail = code, detail
        super().__init__(detail)


def require(condition, code, detail):
    if not condition:
        raise DeployError(code, detail)


def now():
    return datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")


def valid_timestamp(value):
    """The UTC second precision timestamp written by every deployment owner."""
    if not isinstance(value, str) or not re.fullmatch(r"[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z", value):
        return False
    try:
        datetime.strptime(value, "%Y-%m-%dT%H:%M:%SZ")
        return True
    except ValueError:
        return False


def validate_attempt(value):
    """Validate persisted operation metadata before recovery consumes it."""
    code, detail = "invalid_state", "Invalid deployment operation record; administrator reconciliation required."
    required = {"id", "action", "release", "started_at", "finished_at", "outcome", "phase", "error"}
    require(isinstance(value, dict) and required.issubset(value), code, detail)
    require(isinstance(value["id"], str) and VERSION_PATTERN.fullmatch(value["id"]) is not None
            and value["action"] in ("deploy", "rollback", "restart", "stop", "config")
            and (value["release"] is None or isinstance(value["release"], str) and VERSION_PATTERN.fullmatch(value["release"]) is not None)
            and valid_timestamp(value["started_at"]), code, detail)
    require(value["outcome"] in ("running", "succeeded", "failed", "needs_reconciliation", "reconciled")
            and value["phase"] in ("validation", "redis", "backup", "migration", "seed", "switch", "health", "configuration")
            and (value["error"] is None or isinstance(value["error"], str) and re.fullmatch(r"[a-z][a-z0-9_]{0,127}", value["error"]) is not None), code, detail)
    require(value["finished_at"] is None if value["outcome"] == "running" else valid_timestamp(value["finished_at"]), code, detail)
    configuration = {"configuration_revision", "configuration_sha256", "previous_configuration_sha256"}
    if value["action"] == "config":
        require(configuration.issubset(value) and type(value["configuration_revision"]) is int and value["configuration_revision"] >= 2, code, detail)
        for key in ("configuration_sha256", "previous_configuration_sha256"):
            require(isinstance(value[key], str) and re.fullmatch(r"[a-f0-9]{64}", value[key]) is not None, code, detail)
        require("configuration_applied" not in value or type(value["configuration_applied"]) is bool, code, detail)
    else:
        require(not ((configuration | {"configuration_applied"}) & value.keys()), code, detail)
    return value


def valid_uuid(value):
    try:
        return isinstance(value, str) and str(uuid.UUID(value)) == value and uuid.UUID(value).version == 4
    except (ValueError, AttributeError):
        return False


def slug(value):
    return isinstance(value, str) and len(value) <= 20 and SLUG_PATTERN.fullmatch(value) is not None


def fields(value, names, label):
    require(isinstance(value, dict) and set(value) == set(names.split()), "invalid_contract", f"Invalid {label} fields; use the current deployment profile format.")


def validate_project(value):
    fields(value, "schema id slug", "project")
    require(type(value["schema"]) is int and value["schema"] == SCHEMA and valid_uuid(value["id"]) and slug(value["slug"]), "invalid_project", "Project requires schema 1, a persistent UUID and a lowercase slug (1–20 characters).")
    return value


def validate_profile(value, project=None):
    require(isinstance(value, dict), "invalid_profile", "Invalid environment profile.")
    optional = {"backup_policy"}
    fields({k: v for k, v in value.items() if k not in optional}, "schema environment instance_id host runtime_environment domain certbot_email architecture public_env retention edge_origin_lockdown", "environment")
    require(type(value["edge_origin_lockdown"]) is bool, "invalid_profile", "edge_origin_lockdown must be a boolean; Cloudflare is mandatory.")
    require(type(value["schema"]) is int and value["schema"] == PROFILE_SCHEMA and slug(value["environment"]) and valid_uuid(value["instance_id"]), "invalid_profile", "Invalid environment identity.")
    if "backup_policy" in value:
        fields(value["backup_policy"], "interval_hours daily weekly monthly", "backup policy")
        require(all(type(v) is int and 1 <= v <= 365 for v in value["backup_policy"].values()), "invalid_profile", "Invalid backup policy counts.")
    fields(value["host"], "address port host_id", "host")
    h = value["host"]
    require(isinstance(h["address"], str) and re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9.:-]{0,252}", h["address"]) is not None, "invalid_host", "Host address must be a hostname or IP address, without an SSH login.")
    require(type(h["port"]) is int and 1 <= h["port"] <= 65535 and (h["host_id"] is None or valid_uuid(h["host_id"])), "invalid_host", "Invalid SSH port or host UUID.")
    require(value["runtime_environment"] in ("production", "staging", "testing", "development"), "invalid_profile", "Select a declared application runtime environment.")
    require(value["architecture"] in ("amd64", "arm64"), "invalid_architecture", "Only amd64 and arm64 are supported.")
    domain = value["domain"]
    require(isinstance(domain, str) and len(domain) <= 253 and len(domain.split(".")) >= 2 and all(re.fullmatch(r"[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?", part) for part in domain.split(".")), "invalid_domain", "Use one lowercase DNS hostname, without wildcards.")
    require(isinstance(value["certbot_email"], str) and re.fullmatch(r"[^\s@]+@[^\s@]+\.[^\s@]+", value["certbot_email"]) is not None, "invalid_email", "A certificate contact email is required.")
    public = value["public_env"]
    require(isinstance(public, dict) and len(public) <= 100, "invalid_public_env", "Invalid public build settings.")
    for key, item in public.items():
        require(re.fullmatch(r"VITE_[A-Z0-9_]+", key) is not None and isinstance(item, str) and len(item) <= 1024 and not any(c in item for c in "\x00\r\n$"), "invalid_public_env", "Only literal public VITE_* settings may enter a release build; the portal build validates declared keys.")
    fields(value["retention"], "server_releases local_releases pre_migration_dumps", "retention")
    require(all(type(v) is int and 1 <= v <= 50 for v in value["retention"].values()), "invalid_retention", "Retention counts must be between 1 and 50.")
    if project is not None:
        instance_paths(instance_name(project, value))
    return value


def instance_name(project, profile):
    return project["slug"] + "-" + profile["environment"]


def instance_paths(name):
    """Absolute layout-v2 resource paths; Host.Paths remaps filesystem fixtures."""
    require(isinstance(name, str) and len(name) <= 31 and SLUG_PATTERN.fullmatch(name) is not None,
            "invalid_name", "Combined project/environment name must fit the 31-character Linux account limit.")
    return {"application": f"/opt/{PREFIX}/{name}", "data": f"/var/lib/{PREFIX}/apps/{name}",
            "configuration": f"/etc/{PREFIX}/instances/{name}", "state": f"/var/lib/{PREFIX}/instances/{name}",
            "private": f"/var/lib/{PREFIX}/private/instances/{name}"}


def identity(project, profile):
    validate_project(project)
    validate_profile(profile, project)
    name = instance_name(project, profile)
    database = "f_" + uuid.UUID(profile["instance_id"]).hex
    return {"project_id": project["id"], "environment": profile["environment"], "name": name,
            "allocations": {"account": name, "database": database, "database_role": database,
                "redis_user": PREFIX + "-" + profile["instance_id"],
                "namespace": {"application": project["slug"], "environment": profile["environment"]},
                "paths": instance_paths(name)}}


def validate_registry(value):
    from topology import validate_topology
    fields(value, "schema layout_version host_id revision created_at updated_at tools shared projects instances", "registry")
    require(type(value["schema"]) is int and value["schema"] == REGISTRY_SCHEMA
            and type(value["layout_version"]) is int and value["layout_version"] == LAYOUT_VERSION
            and valid_uuid(value["host_id"]), "incompatible_registry", "Host registry identity/schema/layout is incompatible.")
    require(type(value["revision"]) is int and value["revision"] > 0, "invalid_registry", "Invalid registry revision.")
    require(valid_timestamp(value["created_at"]) and valid_timestamp(value["updated_at"]), "invalid_registry", "Invalid registry timestamps.")
    require(all(isinstance(value[k], dict) for k in ("shared", "projects", "instances", "tools")), "invalid_registry", "Invalid registry records.")
    tools = value["tools"]
    fields(tools, "framework_version generation bundle_sha256 protocol_min protocol_max active_path upgrade_operation", "tools")
    require(isinstance(tools["framework_version"], str) and bool(tools["framework_version"])
            and type(tools["generation"]) is int and tools["generation"] >= 1
            and isinstance(tools["bundle_sha256"], str) and re.fullmatch(r"[a-f0-9]{64}", tools["bundle_sha256"])
            and tools["active_path"] == HOST_PATHS["tools"] + "/tools/" + tools["bundle_sha256"]
            and (tools["upgrade_operation"] is None or valid_uuid(tools["upgrade_operation"])), "invalid_registry", "Invalid tools record.")
    require(tools["protocol_min"] == "2.0" and tools["protocol_max"] == "2.0", "incompatible_registry", "Unsupported protocol range.")
    slugs = set()
    for project_id, project in value["projects"].items():
        fields(project, "slug created_at", "registered project")
        require(valid_uuid(project_id) and slug(project["slug"]) and project["slug"] not in slugs
                and valid_timestamp(project["created_at"]), "registry_collision", "Invalid or conflicting project identity.")
        slugs.add(project["slug"])
    ports, domains, accounts, databases, redis_users, uids, gids = (set() for _ in range(7))
    for instance_id, entry in value["instances"].items():
        require(isinstance(entry, dict) and entry.get("project_id") in value["projects"], "invalid_registry", "Unknown instance project.")
        project = {"schema": SCHEMA, "id": entry["project_id"], "slug": value["projects"][entry["project_id"]]["slug"]}
        expected = identity(project, entry.get("profile"))
        require(instance_id == entry["profile"]["instance_id"] and all(entry.get(k) == v for k,v in expected.items() if k != "allocations"), "invalid_registry", "Instance identity mismatch.")
        allocations = entry.get("allocations", {})
        require(all(allocations.get(k) == v for k,v in expected["allocations"].items()), "invalid_registry", "Instance resource allocation mismatch.")
        fields(allocations, "account uid gid http_port database database_role redis_user namespace paths", "allocations")
        require(entry.get("host_id") == value["host_id"] == entry["profile"]["host"]["host_id"], "invalid_registry", "Instance host binding mismatch.")
        port = allocations["http_port"]
        require(type(port) is int and PORT_FIRST <= port <= PORT_LAST, "invalid_registry", "Invalid HTTP allocation.")
        for resource, seen in ((port,ports), (entry["profile"]["domain"],domains), (entry["name"],accounts),
                               (allocations["database"],databases), (allocations["redis_user"],redis_users)):
            require(resource not in seen, "registry_collision", "Duplicate instance allocation.")
            seen.add(resource)
        for key, seen in (("uid",uids),("gid",gids)):
            allocated = allocations[key]
            require(allocated is None or type(allocated) is int and allocated > 0 and allocated not in seen, "registry_collision", "Invalid or duplicate account allocation.")
            if allocated is not None:
                seen.add(allocated)
        validate_topology(entry.get("topology"), registered=True)
        require(type(entry.get("client_generation")) is int and entry["client_generation"] >= 1
                and isinstance(entry.get("framework_version"), str), "invalid_registry", "Invalid client pin.")
        require(isinstance(entry.get("provisioning"), dict) and entry["provisioning"].get("state") in ("preparing", "ready", "failed")
                and isinstance(entry["provisioning"].get("completed_steps"), list), "invalid_registry", "Invalid provisioning state.")
        require(type(entry.get("configuration_revision")) is int and entry["configuration_revision"] >= 1, "invalid_registry", "Invalid configuration revision.")
        require(valid_timestamp(entry.get("created_at")) and valid_timestamp(entry.get("updated_at")), "invalid_registry", "Invalid instance timestamps.")
    return value


def check_path(path, kind=None):
    """Never follow a symlink when reading or replacing deployment control files."""
    path = Path(path)
    for part in [*reversed(path.parents), path]:
        if part.is_symlink():
            raise DeployError("unsafe_path", "A deployment control path is a symlink; administrator review required.")
    if path.exists() and kind:
        require(path.is_dir() if kind == "directory" else path.is_file(), "unsafe_path", "Deployment path has an unexpected file type.")
    return path


def _unique_object(pairs):
    result = {}
    for key, value in pairs:
        require(key not in result, "invalid_json", "Duplicate JSON object key.")
        result[key] = value
    return result


def parse_json(data):
    require(len(data) <= MAX_JSON, "invalid_json", "Deployment JSON exceeds the size limit.")
    try:
        return json.loads(data, object_pairs_hook=_unique_object,
                          parse_constant=lambda _: require(False, "invalid_json", "Nonfinite JSON numbers are invalid."))
    except (ValueError, UnicodeError, RecursionError):
        raise DeployError("invalid_json", "Deployment JSON is invalid.") from None


def read_json(path):
    check_path(path, "file")
    with open(path, "rb") as stream:
        return parse_json(stream.read(MAX_JSON + 1))


def atomic_write(path, data, mode=0o644, owner=None, group=None):
    """Publish bytes and final metadata together; fsync file then parent directory."""
    uid = os.geteuid() if owner is None else (pwd.getpwnam(owner).pw_uid if isinstance(owner, str) else owner)
    gid = os.getegid() if group is None else (grp.getgrnam(group).gr_gid if isinstance(group, str) else group)
    path = check_path(path, "file")
    if isinstance(data, str):
        data = data.encode()
    path.parent.mkdir(parents=True, exist_ok=True)
    if path.exists() and path.read_bytes() == data and stat.S_IMODE(path.stat().st_mode) == mode and path.stat().st_uid == uid and path.stat().st_gid == gid:
        return False
    fd, temporary = tempfile.mkstemp(prefix="." + path.name + ".", dir=path.parent)
    try:
        with os.fdopen(fd, "wb") as output:
            os.fchown(output.fileno(), uid, gid)
            os.fchmod(output.fileno(), mode)
            output.write(data)
            output.flush()
            os.fsync(output.fileno())
        os.replace(temporary, path)
        directory = os.open(path.parent, os.O_RDONLY)
        try:
            os.fsync(directory)
        finally:
            os.close(directory)
        return True
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)


def atomic_json(path, value, mode=0o644, owner=None, group=None):
    try:
        payload = json.dumps(value, sort_keys=True, indent=2, allow_nan=False) + "\n"
    except (ValueError, RecursionError):
        raise DeployError("invalid_json", "Deployment JSON cannot be encoded safely.") from None
    require(len(payload.encode("utf-8")) <= MAX_JSON, "invalid_json", "Deployment JSON exceeds the size limit.")
    return atomic_write(path, payload, mode, owner=owner, group=group)


def digest(path):
    result = hashlib.sha256()
    with open(path, "rb") as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b""):
            result.update(block)
    return result.hexdigest()


def dump_env(values):
    lines = []
    for key, value in sorted(values.items()):
        require(re.fullmatch(r"[A-Z][A-Z0-9_]*", key) is not None and isinstance(value, str) and not any(c in value for c in "\x00\r\n"), "invalid_configuration", "Invalid configuration key or multiline value.")
        # Go's explicit env loader decodes JSON-compatible double-quoted strings.
        lines.append(key + "=" + json.dumps(value, ensure_ascii=False))
    return "\n".join(lines) + "\n"


def parse_env(data):
    result = {}
    for line in data.splitlines():
        line = line.strip()
        if not line or line.startswith("#"):
            continue
        key, separator, value = line.partition("=")
        require(separator and re.fullmatch(r"[A-Z][A-Z0-9_]*", key) is not None and key not in result, "invalid_configuration", "Runtime configuration has invalid or duplicate keys.")
        try:
            if value.startswith('"'):
                value = json.loads(value)
            elif value.startswith("'"):
                require(value.endswith("'"), "invalid_configuration", "Invalid quoted runtime setting.")
                value = value[1:-1]
            result[key] = value
        except ValueError:
            raise DeployError("invalid_configuration", "Invalid quoted runtime setting.") from None
    dump_env(result)
    return result
