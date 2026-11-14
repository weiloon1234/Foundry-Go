"""Deployment wire contracts and identity. Shared by local and installed tools.

Only Python's standard library is used. Application configuration continues to
belong to Go; these contracts describe deployment resources, not application DTOs.
"""
from __future__ import annotations

from dataclasses import dataclass
from enum import Enum
import ipaddress
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
from urllib.parse import urlsplit
import uuid

PREFIX = "foundry"
SCHEMA = 1
SERVICE_ROLES = ("http", "scheduler", "worker")
INSTANCE_STATUSES = frozenset({"never_deployed", "deploying", "healthy", "failed", "failed_applied", "rolled_back", "stopped", "degraded", "unknown"})
RENEWAL_WARNING = ("WARNING: Certificate renewal verification is deferred; "
                   "ACTION=report shows the reason and the next ACTION=deploy retries it.")
LAYOUT_VERSION = 2
REGISTRY_SCHEMA = 2
PROFILE_SCHEMA = 2
PORT_FIRST, PORT_LAST = 8100, 8999
MAX_JSON = 4 * 1024 * 1024
VERSION_PATTERN = re.compile(r"[0-9]{8}T[0-9]{6}Z-[a-f0-9]{8,40}")
SLUG_PATTERN = re.compile(r"[a-z][a-z0-9]*(?:-[a-z0-9]+)*")

POSTGRES_PORT = 55432
REDIS_PORT = 56379
BACKUP_POLICY = {"interval_hours": 24, "full_retention_days": 35, "pitr_days": 28}
BACKUP_JITTER_SECONDS = 600
# A backup is fresh for one interval plus timer jitter and the database backup deadline.
BACKUP_FRESH_SECONDS = BACKUP_POLICY["interval_hours"] * 3600 + BACKUP_JITTER_SECONDS + 1800
# Bootstrap retention protects recent database restores from attachment cleanup.
# Operators may raise it later.
CLEANUP_GRACE_KEY = "APP__FEATURES__ATTACHMENTS__CONFIG__CLEANUP_GRACE"
DEFAULT_CLEANUP_GRACE = "72h"
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
    "upgrade_deletions": f"/var/lib/{PREFIX}/upgrade-deletions",
    "retired_tools": f"/var/lib/{PREFIX}/retired-tools.json",
    "reports": f"/var/lib/{PREFIX}/reports/host-v2.json",
    "locks": f"/run/lock/{PREFIX}",
    "tools": f"/usr/local/lib/{PREFIX}",
    "current": f"/usr/local/lib/{PREFIX}/current",
    "launcher": f"/usr/local/sbin/{PREFIX}-host",
    "report": f"/usr/local/bin/{PREFIX}-host-report",
}
# Root-owned directories every Foundry account must traverse. Each child sets
# its own stricter mode; parents created implicitly would follow the umask.
HOST_ROOTS = (
    (f"/etc/{PREFIX}", 0o755), (f"/etc/{PREFIX}/instances", 0o755),
    (f"/var/lib/{PREFIX}", 0o755), (f"/var/lib/{PREFIX}/private", 0o700),
    (f"/var/lib/{PREFIX}/apps", 0o755), (f"/var/lib/{PREFIX}/instances", 0o755),
    (f"/opt/{PREFIX}", 0o755), (f"/run/{PREFIX}", 0o755), (f"/run/lock/{PREFIX}", 0o755),
)


def unit_name(name: str, role: str) -> str:
    """Canonical layout-v2 unit spelling; topology owns role selection."""
    instance_paths(name)
    require(role in SERVICE_ROLES, "invalid_topology", "Unknown service role.")
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
    require(value["outcome"] in ("running", "succeeded", "failed", "failed_applied", "needs_reconciliation", "reconciled")
            and value["phase"] in ("validation", "redis", "backup", "migration", "seed", "switch", "health", "configuration")
            and (value["error"] is None or isinstance(value["error"], str) and re.fullmatch(r"[a-z][a-z0-9_]{0,127}", value["error"]) is not None), code, detail)
    require(value["finished_at"] is None if value["outcome"] == "running" else valid_timestamp(value["finished_at"]), code, detail)
    require("role" not in value or value["action"] in ("restart", "stop")
            and value["role"] in ("all", *SERVICE_ROLES), code, detail)
    if "roles" in value:
        roles = value["roles"]
        require(value["action"] in ("config", "rollback", "deploy") and isinstance(roles, list)
                and all(isinstance(role, str) and role in SERVICE_ROLES for role in roles)
                and len(roles) == len(set(roles)), code, detail)
    configuration = {"configuration_revision", "configuration_sha256", "previous_configuration_sha256"}
    if value["action"] == "config":
        require(type(value.get("configuration_revision")) is int and value["configuration_revision"] >= 2, code, detail)
        hashes = {"configuration_sha256", "previous_configuration_sha256"}
        redacted = value.get("configuration_redacted", False)
        require(type(redacted) is bool, code, detail)
        if redacted:
            require(not hashes.intersection(value), code, detail)
        else:
            require(hashes.issubset(value), code, detail)
            for key in hashes:
                require(isinstance(value[key], str) and re.fullmatch(r"[a-f0-9]{64}", value[key]) is not None, code, detail)
        require("configuration_applied" not in value or type(value["configuration_applied"]) is bool, code, detail)
    else:
        require(not ((configuration | {"configuration_applied", "configuration_redacted"}) & value.keys()), code, detail)
    require(value["outcome"] != "failed_applied" or value["action"] == "config" and value.get("configuration_applied") is True, code, detail)
    require("result" not in value, code, detail)
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
    if value.get("schema") == FEATURE_PROFILE_SCHEMA:
        from topology import validate_feature_profile
        validate_feature_profile(value, project)
        return value
    optional = {"backup_policy", "uploads"}
    fields({k: v for k, v in value.items() if k not in optional}, "schema environment instance_id host runtime_environment domain certbot_email architecture public_env retention edge_origin_lockdown", "environment")
    require(type(value["edge_origin_lockdown"]) is bool, "invalid_profile", "edge_origin_lockdown must be a boolean; Cloudflare is mandatory.")
    require(type(value["schema"]) is int and value["schema"] == PROFILE_SCHEMA and slug(value["environment"]) and valid_uuid(value["instance_id"]), "invalid_profile", "Invalid environment identity.")
    if "uploads" in value:
        from uploads import validate as validate_uploads
        validate_uploads(value["uploads"])
    if "backup_policy" in value:
        fields(value["backup_policy"], " ".join(BACKUP_POLICY), "backup policy")
        require(all(type(v) is int and 1 <= v <= 365 for k, v in value["backup_policy"].items()), "invalid_profile", "Invalid time-based backup policy.")
        require(value["backup_policy"] == BACKUP_POLICY, "invalid_profile", "Only the supported 35-day full retention and 28-day PITR policy is available.")
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


def normalize_storage_endpoint(endpoint, bucket):
    """Repair two unambiguous slips; validation still refuses anything else.

    A missing scheme can only mean HTTPS here, and R2's dashboard shows the S3 URL
    with the bucket appended. An explicit http:// or any other path stays as given.
    """
    if not isinstance(endpoint, str):
        return endpoint
    endpoint = endpoint.strip()
    if endpoint and "://" not in endpoint:
        endpoint = "https://" + endpoint
    try:
        url = urlsplit(endpoint)
    except ValueError:
        return endpoint
    if isinstance(bucket, str) and url.path.strip("/") == bucket:
        return url._replace(path="").geturl()
    return endpoint[:-1] if url.path == "/" and endpoint.endswith("/") else endpoint


def normalize_storage_disks(disks):
    result = {}
    for name, disk in disks.items():
        cloud = disk.get("cloud") if isinstance(disk, dict) else None
        if isinstance(cloud, dict) and "endpoint" in cloud:
            endpoint = normalize_storage_endpoint(cloud["endpoint"], cloud.get("bucket"))
            disk = {**disk, "cloud": {**cloud, "endpoint": endpoint}}
        result[name] = disk
    return result


def normalize_backup_repository(repository):
    repository = {**repository, "endpoint": normalize_storage_endpoint(repository["endpoint"], repository["bucket"])}
    try:
        _physical_storage_binding(repository)
    except DeployError as error:
        raise DeployError(error.code, "Backup repository: " + error.detail) from None
    return repository


def storage_bindings(storage):
    """Extract public physical ownership; compare aliases before dropping secrets."""
    require(isinstance(storage, dict), "storage_binding_invalid", "Storage must declare credentials and disks.")
    credentials, disks = storage.get("credentials"), storage.get("disks")
    credentials = parse_json(credentials) if isinstance(credentials, str) else credentials
    disks = parse_json(disks) if isinstance(disks, str) else disks
    require(isinstance(credentials, dict) and isinstance(disks, dict),
            "storage_binding_invalid", "Storage credentials and disks must be objects.")
    result, definitions = {}, {}
    for name, disk in disks.items():
        require(isinstance(name, str) and isinstance(disk, dict), "storage_binding_invalid", "Invalid storage disk.")
        require(disk.get("driver") in ("r2", "s3", "s3_compatible"),
                "storage_binding_invalid", "Deployment storage bindings require explicit cloud disks.")
        cloud = disk.get("cloud", {})
        require(isinstance(cloud, dict), "storage_binding_invalid", "Invalid cloud binding.")
        try:
            binding = _physical_storage_binding(cloud)
        except DeployError as error:
            raise DeployError(error.code, f"Disk {name}: {error.detail}") from None
        credential_name = cloud.get("credentials")
        require(isinstance(credential_name, str), "storage_binding_invalid", "Invalid storage credential name.")
        credential = credentials.get(credential_name)
        require(isinstance(credential, dict), "storage_binding_invalid", "Unknown storage credential binding.")
        for previous, other in result.items():
            if storage_overlaps(binding, other):
                require(binding == other and definitions[previous] == (disk, credential),
                        "storage_binding_conflict", "Aliasing disks must have byte-identical bindings.")
        result[name], definitions[name] = binding, (disk, credential)
    return result


def _physical_storage_binding(cloud):
    bucket, namespace = cloud.get("bucket"), cloud.get("namespace", "")
    endpoint = cloud.get("endpoint", "")
    require(isinstance(endpoint, str), "storage_binding_invalid", "Invalid storage endpoint.")
    try:
        url = urlsplit(endpoint)
        valid_endpoint = (url.scheme in ("https", "http") and url.hostname and not url.username and not url.password
                          and url.path in ("", "/") and not url.query and not url.fragment)
    except ValueError:
        valid_endpoint = False
    require(valid_endpoint, "storage_binding_invalid", "Storage requires an explicit credential-free endpoint.")
    require(isinstance(bucket, str) and re.fullmatch(r"[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]", bucket)
            and isinstance(namespace, str) and (not namespace or namespace.endswith("/"))
            and len(namespace) <= 1024 and not namespace.startswith("/")
            and (not namespace or all(part not in ("", ".", "..") for part in namespace[:-1].split("/")))
            and all(32 <= ord(c) < 127 for c in namespace) and "\\" not in namespace,
            "storage_binding_invalid", "Use a valid bucket and an empty or slash-terminated namespace prefix.")
    return {"endpoint": endpoint.rstrip("/").lower(), "bucket": bucket, "namespace": namespace}


def storage_overlaps(left, right):
    # Endpoint spelling/credentials cannot establish separate physical ownership.
    return (left["bucket"] == right["bucket"]
            and (left["namespace"].startswith(right["namespace"])
                 or right["namespace"].startswith(left["namespace"])))


def check_storage_bindings(registry, entry, bindings):
    """Check both selected bindings and reservations held by interrupted config."""
    for instance_id, other in registry["instances"].items():
        if instance_id == entry["profile"]["instance_id"]:
            continue
        require(not bindings or "storage_bindings" in other or other.get("provisioning", {}).get("state") != "ready",
                "storage_binding_missing", "Import existing instances' runtime storage bindings before allocating another namespace.")
        for records in (other.get("storage_bindings", {}), other.get("pending_storage_bindings", {})):
            require(not any(storage_overlaps(left, right) for left in bindings.values() for right in records.values()),
                    "storage_binding_conflict", "Application storage overlaps another instance's binding.")
    return bindings


def reserve_runtime_storage(registry, entry, values, read_value):
    """Call before publishing a config candidate, under the host operation lock.

    Persist the registry reservation before app.env can change. Reconciliation
    selects either the old or new bindings from the proven selected revision.
    """
    return bind_storage(registry, entry, runtime_storage(values, read_value), pending=True)


def select_runtime_storage(registry, entry, values, read_value):
    bindings = bind_storage(registry, entry, runtime_storage(values, read_value))
    entry.pop("pending_storage_bindings", None)
    return bindings


def bind_storage(registry, entry, storage, *, pending=False):
    bindings = check_storage_bindings(registry, entry, storage_bindings(storage))
    entry["pending_storage_bindings" if pending else "storage_bindings"] = bindings
    return bindings


def runtime_storage(values, read_value):
    """Resolve the same collection values used by the selected runtime revision.

    Fine-grained collection overrides must be consolidated into the collection
    before deployment can prove physical ownership.
    """
    names = {"credentials": "APP__SERVICES__CREDENTIALS", "disks": "APP__SERVICES__STORAGE__DISKS"}
    result = {}
    for field, key in names.items():
        require(not any(name.startswith(key + "__") for name in values)
                and not (key in values and key + "_FILE" in values),
                "storage_binding_invalid", "Use one complete storage collection, without nested overrides or conflicting aliases.")
        text = read_value(values[key + "_FILE"]) if key + "_FILE" in values else values.get(key, "{}")
        result[field] = parse_json(text)
    storage_bindings(result)
    return result


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
        if "deployment_selection" in entry:
            AllocatedEndpoint.parse({"role": "http", "listener": "http", "address": "127.0.0.1", "port": port})
        else:
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
        if "deployment_selection" in entry:
            selection = DeploymentSelection.parse(entry["deployment_selection"])
            require(selection.instance_id == instance_id, "invalid_registry", "Deployment selection belongs to another instance.")
        topology = validate_topology(entry.get("topology"), registered=True)
        if topology["schema"] == TOPOLOGY_SCHEMA or entry["profile"]["schema"] == FEATURE_PROFILE_SCHEMA:
            from topology import parse_profile, parse_topology
            parse_profile(entry["profile"], parse_topology(topology, registered=True), project)
        require(type(entry.get("client_generation")) is int and entry["client_generation"] >= 1
                and isinstance(entry.get("framework_version"), str), "invalid_registry", "Invalid client pin.")
        require(isinstance(entry.get("provisioning"), dict) and entry["provisioning"].get("state") in ("preparing", "ready", "failed", "rehearsal")
                and isinstance(entry["provisioning"].get("completed_steps"), list), "invalid_registry", "Invalid provisioning state.")
        require(type(entry.get("configuration_revision")) is int and entry["configuration_revision"] >= 1, "invalid_registry", "Invalid configuration revision.")
        require(valid_timestamp(entry.get("created_at")) and valid_timestamp(entry.get("updated_at")), "invalid_registry", "Invalid instance timestamps.")
    for entry in value["instances"].values():
        for field in ("storage_bindings", "pending_storage_bindings"):
            records = entry.get(field, {})
            require(isinstance(records, dict), "invalid_registry", "Invalid storage bindings.")
            for record in records.values():
                require(isinstance(record, dict) and set(record) == {"endpoint", "bucket", "namespace"}
                        and all(isinstance(item, str) for item in record.values()),
                        "invalid_registry", "Invalid storage binding record.")
                require(record == _physical_storage_binding(record), "invalid_registry", "Noncanonical storage binding record.")
            records = list(records.values())
            require(all(left == right or not storage_overlaps(left, right)
                        for index, left in enumerate(records) for right in records[index + 1:]),
                    "storage_binding_conflict", "Nonidentical storage aliases overlap within an instance.")
    for entry in value["instances"].values():
        for field in ("storage_bindings", "pending_storage_bindings"):
            check_storage_bindings(value, entry, entry.get(field, {}))
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


# Shared readers reuse the frozen feature parsers. Runtime execution has its own
# capability gates, independent of declaration and registry validation.
TOPOLOGY_SCHEMA = 2
FEATURE_PROFILE_SCHEMA = 3
ARCHIVE_SCHEMA = 2
RECORD_SCHEMA = 1
FEATURE_CAPABILITIES = frozenset({"topology-v2", "profile-v3", "release-v2"})
# Historical capability floor, independent of the currently selected tool generation.
FEATURE_INTRODUCTION_GENERATION = 12
FEATURE_LIFECYCLE_GENERATION = 13
MAX_ROLES, MAX_ARTIFACTS, MAX_LISTENERS = 32, 32, 256


def contract_object(value, required, optional=()):
    require(isinstance(value, dict) and set(required) <= value.keys()
            and value.keys() <= set(required) | set(optional),
            "invalid_contract", "Unknown or missing deployment contract fields.")
    return value


def contract_text(value, *, limit=1024):
    require(isinstance(value, str) and 0 < len(value.encode("utf-8", errors="surrogatepass")) <= limit
            and not any(0xD800 <= ord(c) <= 0xDFFF for c in value)
            and not any(ord(c) < 32 or ord(c) == 127 for c in value),
            "invalid_contract", "Invalid deployment contract text.")
    return value


def contract_name(value):
    require(isinstance(value, str) and re.fullmatch(r"[a-z][a-z0-9_-]{0,63}", value),
            "invalid_contract", "Invalid deployment identifier.")
    return value


def contract_int(value, lower=1, upper=2**63 - 1):
    require(type(value) is int and lower <= value <= upper,
            "invalid_contract", "Deployment integer is outside its bounds.")
    return value


def contract_digest(value):
    require(isinstance(value, str) and re.fullmatch(r"[a-f0-9]{64}", value),
            "invalid_contract", "Invalid deployment digest.")
    return value


def contract_uuid(value):
    require(valid_uuid(value), "invalid_contract", "Invalid deployment UUID.")
    return value


def contract_path(value, *, absolute=False):
    contract_text(value)
    require(value.startswith("/") == absolute and "\\" not in value
            and all(part not in ("", ".", "..") for part in value.lstrip("/").split("/")),
            "invalid_contract", "Deployment paths must be canonical and confined.")
    return value


def contract_list(value, parser, *, maximum=256, minimum=0):
    require(isinstance(value, list) and minimum <= len(value) <= maximum,
            "invalid_contract", "Deployment collection is outside its bounds.")
    return tuple(parser(item) for item in value)


def contract_names(value, *, maximum=MAX_ROLES):
    result = contract_list(value, contract_name, maximum=maximum)
    require(len(result) == len(set(result)), "invalid_contract", "Duplicate deployment identifiers.")
    return result


def contract_map(value, parser, *, maximum=256):
    require(isinstance(value, dict) and len(value) <= maximum,
            "invalid_contract", "Invalid deployment mapping.")
    return tuple((contract_name(key), parser(item)) for key, item in sorted(value.items()))


def contract_document(value):
    """Apply the JSON boundary to in-memory callers too; never retain their containers."""
    if isinstance(value, (str, bytes)):
        return parse_json(value)
    try:
        return parse_json(json.dumps(value, allow_nan=False).encode())
    except (TypeError, ValueError, RecursionError, UnicodeError):
        raise DeployError("invalid_contract", "Invalid deployment document.") from None


def canonical_digest(value):
    return hashlib.sha256(json.dumps(value, sort_keys=True, separators=(",", ":"),
                                     allow_nan=False).encode()).hexdigest()


def _record(value, names):
    value = contract_document(value)
    contract_object(value, ("schema", *names))
    require(type(value["schema"]) is int and value["schema"] == RECORD_SCHEMA,
            "unsupported_schema", "Unsupported deployment record schema.")
    return value


@dataclass(frozen=True)
class UnitIdentity:
    instance_id: str
    role: str
    ordinal: int

    @classmethod
    def parse(cls, value):
        contract_object(value, ("instance_id", "role", "ordinal"))
        ordinal = contract_int(value["ordinal"])
        require(ordinal == 1, "unsupported_capability", "Numbered services require a later capability.")
        return cls(contract_uuid(value["instance_id"]), contract_name(value["role"]), ordinal)


@dataclass(frozen=True)
class AllocatedEndpoint:
    role: str
    listener: str
    address: str
    port: int

    @classmethod
    def parse(cls, value):
        contract_object(value, ("role", "listener", "address", "port"))
        require(value["address"] in ("127.0.0.1", "::1"), "invalid_contract", "Private backends require loopback.")
        return cls(contract_name(value["role"]), contract_name(value["listener"]),
                   value["address"], contract_int(value["port"], 1024, 65535))


@dataclass(frozen=True)
class EffectiveEnvironment:
    role: str
    path: str
    sha256: str

    @classmethod
    def parse(cls, value):
        contract_object(value, ("role", "path", "sha256"))
        path = contract_path(value["path"])
        require(path.startswith("runtime/") and path.endswith("/app.env"),
                "invalid_contract", "Effective environments belong to the private runtime tree.")
        return cls(contract_name(value["role"]), path, contract_digest(value["sha256"]))


@dataclass(frozen=True)
class RuntimeResolution:
    """Private record. Environment digests must never enter public reports.

    M3 projects this selection into runtime_config, readiness and edge; none may
    independently change the locked HTTP address or restore a historical keyring.
    """
    id: str
    deployment_sha256: str
    private_revision: int
    roles: tuple[str, ...]
    endpoints: tuple[AllocatedEndpoint, ...]
    environments: tuple[EffectiveEnvironment, ...]
    bindings_sha256: str
    readiness_sha256: str
    nginx_sha256: str

    @classmethod
    def parse(cls, value):
        v = _record(value, ("id", "deployment_sha256", "private_revision", "roles", "endpoints",
                            "environments", "bindings_sha256", "readiness_sha256", "nginx_sha256"))
        roles = contract_names(v["roles"])
        require("http" in roles, "invalid_contract", "HTTP remains required.")
        endpoints = contract_list(v["endpoints"], AllocatedEndpoint.parse, maximum=MAX_LISTENERS)
        envs = contract_list(v["environments"], EffectiveEnvironment.parse, maximum=MAX_ROLES)
        require(all(e.role in roles for e in endpoints) and {e.role for e in envs} == set(roles)
                and len(envs) == len(roles) and len({e.path for e in envs}) == len(envs)
                and len({(e.role, e.listener) for e in endpoints}) == len(endpoints)
                and len({(e.address, e.port) for e in endpoints}) == len(endpoints)
                and any(e.role == "http" and e.listener == "http" for e in endpoints),
                "invalid_contract", "Resolution endpoints/environments must cover unique selected roles.")
        identity = contract_uuid(v["id"])
        require(all(e.path.startswith("runtime/" + identity + "/") for e in envs),
                "invalid_contract", "Environment path belongs to another resolution.")
        return cls(identity, contract_digest(v["deployment_sha256"]), contract_int(v["private_revision"]),
                   roles, endpoints, envs, *(contract_digest(v[k]) for k in
                                            ("bindings_sha256", "readiness_sha256", "nginx_sha256")))


def require_current_private_configuration(target: RuntimeResolution, current_revision: int):
    """Ordinary rollback/restart keeps current private inputs, including all keys."""
    require(target.private_revision == contract_int(current_revision), "private_revision_conflict",
            "Resolve the target against current private configuration; secret restore is a separate operation.")


class AdmissionDisposition(str, Enum):
    PENDING = "pending"
    COMMITTED = "committed"


@dataclass(frozen=True)
class AdmissionRecord:
    instance_id: str
    resolution_id: str
    deployment_sha256: str
    private_revision: int
    operation_id: str
    disposition: AdmissionDisposition

    @classmethod
    def parse(cls, value):
        v = _record(value, ("instance_id", "resolution_id", "deployment_sha256", "private_revision",
                            "operation_id", "disposition"))
        require(v["disposition"] in ("pending", "committed"), "invalid_contract", "Invalid admission disposition.")
        return cls(contract_uuid(v["instance_id"]), contract_uuid(v["resolution_id"]),
                   contract_digest(v["deployment_sha256"]), contract_int(v["private_revision"]),
                   contract_uuid(v["operation_id"]), AdmissionDisposition(v["disposition"]))


class ClosureStage(str, Enum):
    INGRESS_WITHDRAWN = "ingress_withdrawn"
    APPLICATION_CLOSED = "application_admission_closed"


@dataclass(frozen=True)
class ClosureEvidence:
    """An observation record, never authority to perform a start by itself."""
    id: str
    operation_id: str
    boot_id: str
    old_resolution_id: str
    candidate_resolution_id: str
    units: tuple[UnitIdentity, ...]
    endpoints: tuple[AllocatedEndpoint, ...]
    retained_units_sha256: str
    ingress_sha256: str
    backend_exit_sha256: str | None
    old_reference_isolation_sha256: str | None
    observed_at: str
    stage: ClosureStage

    @classmethod
    def parse(cls, value):
        v = _record(value, ("id", "operation_id", "boot_id", "old_resolution_id", "candidate_resolution_id",
                            "units", "endpoints", "retained_units_sha256", "ingress_sha256",
                            "backend_exit_sha256", "old_reference_isolation_sha256", "observed_at", "stage"))
        require(v["stage"] in tuple(s.value for s in ClosureStage) and valid_timestamp(v["observed_at"]),
                "invalid_contract", "Invalid closure observation.")
        units = contract_list(v["units"], UnitIdentity.parse, maximum=MAX_ROLES, minimum=1)
        endpoints = contract_list(v["endpoints"], AllocatedEndpoint.parse)
        require(len(set(units)) == len(units) and len({u.instance_id for u in units}) == 1
                and all(e.role in {u.role for u in units} for e in endpoints),
                "invalid_contract", "Invalid closure scope.")
        proofs = tuple(None if v[k] is None else contract_digest(v[k]) for k in
                       ("backend_exit_sha256", "old_reference_isolation_sha256"))
        require(v["stage"] != ClosureStage.APPLICATION_CLOSED.value or all(proofs),
                "invalid_contract", "Complete closure requires backend exit and old-reference isolation.")
        return cls(*(contract_uuid(v[k]) for k in ("id", "operation_id", "boot_id", "old_resolution_id",
                                                  "candidate_resolution_id")), units, endpoints,
                   contract_digest(v["retained_units_sha256"]), contract_digest(v["ingress_sha256"]),
                   *proofs, v["observed_at"], ClosureStage(v["stage"]))


@dataclass(frozen=True)
class ProcessIdentity:
    pid: int
    start_ticks: int
    invocation_id: str

    @classmethod
    def parse(cls, value):
        contract_object(value, ("pid", "start_ticks", "invocation_id"))
        # systemd INVOCATION_ID is a compact 128-bit identifier, not a UUIDv4.
        invocation = value["invocation_id"]
        require(isinstance(invocation, str) and re.fullmatch(r"[a-f0-9]{32}", invocation),
                "invalid_contract", "Invalid systemd invocation identity.")
        return cls(contract_int(value["pid"], upper=2**31 - 1), contract_int(value["start_ticks"]), invocation)


@dataclass(frozen=True)
class ValidationPermit:
    id: str
    operation_id: str
    boot_id: str
    resolution_id: str
    closure_id: str
    executor: ProcessIdentity
    units: tuple[UnitIdentity, ...]

    @classmethod
    def parse(cls, value):
        v = _record(value, ("id", "operation_id", "boot_id", "resolution_id", "closure_id", "executor", "units"))
        units = contract_list(v["units"], UnitIdentity.parse, minimum=1, maximum=MAX_ROLES)
        require(len(set(units)) == len(units), "invalid_contract", "Duplicate permit allowances.")
        return cls(*(contract_uuid(v[k]) for k in ("id", "operation_id", "boot_id", "resolution_id", "closure_id")),
                   ProcessIdentity.parse(v["executor"]), units)

    def require_closure(self, evidence: ClosureEvidence, resolution: RuntimeResolution):
        require(evidence.stage is ClosureStage.APPLICATION_CLOSED and self.closure_id == evidence.id
                and self.operation_id == evidence.operation_id and self.boot_id == evidence.boot_id
                and self.resolution_id == evidence.candidate_resolution_id
                and set(self.units) <= set(evidence.units)
                and resolution.id == self.resolution_id
                and {u.role for u in self.units} <= set(resolution.roles)
                and {e for e in resolution.endpoints if e.role in {u.role for u in self.units}} <= set(evidence.endpoints),
                "admission_closed_required",
                "Validation requires complete closure for the exact operation, boot, resolution and unit set.")


@dataclass(frozen=True)
class HelperInvocation:
    boot_id: str
    unit: UnitIdentity
    process: ProcessIdentity
    bundle_sha256: str
    unit_spec_sha256: str
    resolution_id: str
    helper_format: int
    cleanup_id: str | None

    @classmethod
    def parse(cls, value):
        v = _record(value, ("boot_id", "unit", "process", "bundle_sha256", "unit_spec_sha256",
                            "resolution_id", "helper_format", "cleanup_id"))
        require(type(v["helper_format"]) is int and v["helper_format"] == 1,
                "unsupported_capability", "Unsupported helper state format; retain its reader.")
        return cls(contract_uuid(v["boot_id"]), UnitIdentity.parse(v["unit"]), ProcessIdentity.parse(v["process"]),
                   contract_digest(v["bundle_sha256"]), contract_digest(v["unit_spec_sha256"]),
                   contract_uuid(v["resolution_id"]), v["helper_format"],
                   None if v["cleanup_id"] is None else contract_uuid(v["cleanup_id"]))


@dataclass(frozen=True)
class BundleReferences:
    """Snapshot under the M3 lifetime lock; unknown observations retain the bundle."""
    bundle_sha256: str
    selected: bool
    unit_generations: tuple[str, ...]
    invocations: tuple[str, ...]
    operations: tuple[str, ...]
    cleanup: tuple[str, ...]
    observations_complete: bool

    @classmethod
    def parse(cls, value):
        v = _record(value, ("bundle_sha256", "selected", "unit_generations", "invocations",
                            "operations", "cleanup", "observations_complete"))
        require(type(v["selected"]) is bool and type(v["observations_complete"]) is bool,
                "invalid_contract", "Invalid bundle lifetime observation.")
        invocation = lambda item: ProcessIdentity.parse({"pid": 1, "start_ticks": 1, "invocation_id": item}).invocation_id
        groups = tuple(contract_list(v[k], parser, maximum=4096) for k, parser in
                       (("unit_generations", contract_digest), ("invocations", invocation),
                        ("operations", contract_uuid), ("cleanup", contract_uuid)))
        require(all(len(g) == len(set(g)) for g in groups), "invalid_contract", "Duplicate bundle references.")
        return cls(contract_digest(v["bundle_sha256"]), v["selected"], *groups, v["observations_complete"])

    @property
    def deletable(self):
        return (self.observations_complete and not self.selected and not self.unit_generations
                and not self.invocations and not self.operations and not self.cleanup)


@dataclass(frozen=True)
class DeploymentSelection:
    """Sticky capability state; legacy rollback never removes this record."""
    instance_id: str
    minimum_generation: int
    required_capabilities: tuple[str, ...]
    selected_resolution_id: str
    previous_resolution_id: str | None
    admission: AdmissionRecord
    enabled_roles: tuple[str, ...]
    maintenance_holds: tuple[str, ...]

    @classmethod
    def parse(cls, value):
        v = _record(value, ("instance_id", "minimum_generation", "required_capabilities", "selected_resolution_id",
                            "previous_resolution_id", "admission", "enabled_roles", "maintenance_holds"))
        capabilities = contract_names(v["required_capabilities"])
        require(set(capabilities) == FEATURE_CAPABILITIES, "unsupported_capability", "Unknown or incomplete deployment capabilities.")
        admission = AdmissionRecord.parse(v["admission"])
        instance, resolution = contract_uuid(v["instance_id"]), contract_uuid(v["selected_resolution_id"])
        require(admission.instance_id == instance and admission.resolution_id == resolution,
                "invalid_contract", "Selection and admission identities differ.")
        return cls(instance, contract_int(v["minimum_generation"], FEATURE_INTRODUCTION_GENERATION), capabilities, resolution,
                   None if v["previous_resolution_id"] is None else contract_uuid(v["previous_resolution_id"]),
                   admission, contract_names(v["enabled_roles"]), contract_names(v["maintenance_holds"]))


def public_address(value):
    try:
        address = ipaddress.ip_address(value) if isinstance(value, str) else None
    except ValueError:
        address = None
    require(address is not None and str(address) == value and not address.is_unspecified
            and not address.is_loopback and not address.is_link_local and not address.is_multicast
            and "%" not in value and not getattr(address, "ipv4_mapped", None), "invalid_contract", "Invalid explicit public address.")
    return value


@dataclass(frozen=True)
class RecoveryListenerBinding:
    name: str
    hostname: str
    ports: tuple[int, ...]
    old_addresses: tuple[str, ...]
    replacement_addresses: tuple[str, ...]
    source_binding_sha256: str

    @classmethod
    def parse(cls, value):
        contract_object(value, ("name", "hostname", "transport", "ports", "dns_mode", "old_addresses",
                                "replacement_addresses", "source_binding_sha256"))
        require(value["transport"] == "tcp" and value["dns_mode"] == "external",
                "unsupported_capability", "Release 1 recovery supports external DNS for TCP listeners only.")
        require(isinstance(value["name"], str) and len(value["name"].split(".")) == 2,
                "invalid_contract", "Recovery requires a qualified listener name.")
        name = ".".join(contract_name(n) for n in value["name"].split("."))
        ports = contract_list(value["ports"], lambda p: contract_int(p, 1, 65535), maximum=1, minimum=1)
        old = contract_list(value["old_addresses"], public_address, maximum=16, minimum=1)
        new = contract_list(value["replacement_addresses"], public_address, maximum=16, minimum=1)
        require(len(set(old)) == len(old) and len(set(new)) == len(new)
                and {ipaddress.ip_address(a).version for a in old} == {ipaddress.ip_address(a).version for a in new},
                "invalid_contract", "Recovery must preserve address families without duplicate addresses.")
        return cls(name, dns_hostname(value["hostname"]), ports, old, new, contract_digest(value["source_binding_sha256"]))


def dns_hostname(value):
    require(isinstance(value, str) and len(value) <= 253 and len(value.split(".")) >= 2
            and all(re.fullmatch(r"[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?", p) for p in value.split(".")),
            "invalid_contract", "Invalid DNS hostname.")
    return value


@dataclass(frozen=True)
class DNSObservation:
    hostname: str
    addresses: tuple[str, ...]
    observed_at: str
    source: str
    state: str = "resolved"

    @classmethod
    def parse(cls, value):
        contract_object(value, ("hostname", "addresses", "observed_at", "source"), ("state",))
        require(valid_timestamp(value["observed_at"]) and value["source"] in ("authoritative", "recursive"),
                "invalid_contract", "Invalid external DNS observation; it is not provider ownership evidence.")
        addresses = contract_list(value["addresses"], public_address, maximum=16)
        require(len(set(addresses)) == len(addresses), "invalid_contract", "Duplicate DNS answers.")
        state = value.get("state", "resolved")
        require(state in ("resolved", "unresolved", "unavailable") and (state == "resolved" or not addresses),
                "invalid_contract", "Unobserved DNS cannot claim resolved addresses.")
        return cls(dns_hostname(value["hostname"]), addresses, value["observed_at"], value["source"], state)


@dataclass(frozen=True)
class RecoveryPublicBinding:
    operation_id: str
    source_instance_id: str
    source_host_id: str
    target_host_id: str
    deployment_sha256: str
    listeners: tuple[RecoveryListenerBinding, ...]
    dns_cutover: str
    observations: tuple[DNSObservation, ...]

    @classmethod
    def parse(cls, value):
        v = _record(value, ("operation_id", "source_instance_id", "source_host_id", "target_host_id",
                            "deployment_sha256", "listeners", "dns_cutover", "observations"))
        require(v["dns_cutover"] in ("external_pending", "external_observed"),
                "unsupported_capability", "Managed DNS cutover requires a later capability.")
        listeners = contract_list(v["listeners"], RecoveryListenerBinding.parse, minimum=1)
        require(len({l.name for l in listeners}) == len(listeners)
                and v["source_host_id"] != v["target_host_id"], "invalid_contract", "Invalid recovery binding identity.")
        observations = contract_list(v["observations"], DNSObservation.parse)
        require(all(o.hostname in {l.hostname for l in listeners} for o in observations),
                "invalid_contract", "DNS observation belongs to an undeclared listener hostname.")
        require(v["dns_cutover"] != "external_observed" or all(
            any(o.state == "resolved" and o.hostname == l.hostname and set(o.addresses) == set(l.replacement_addresses) for o in observations)
            for l in listeners), "invalid_contract", "External DNS has not been observed at the replacement addresses.")
        return cls(*(contract_uuid(v[k]) for k in ("operation_id", "source_instance_id", "source_host_id", "target_host_id")),
                   contract_digest(v["deployment_sha256"]), listeners, v["dns_cutover"], observations)


@dataclass(frozen=True)
class ResolvedExecutableBinding:
    name: str
    path: str
    source_sha256: str

    @classmethod
    def parse(cls, value):
        contract_object(value, ("name", "path", "source_sha256"))
        return cls(contract_name(value["name"]), contract_path(value["path"], absolute=True),
                   contract_digest(value["source_sha256"]))


@dataclass(frozen=True)
class ResolvedEndpointBinding:
    name: str
    endpoint: AllocatedEndpoint
    scheme: str

    @classmethod
    def parse(cls, value):
        contract_object(value, ("name", "endpoint", "scheme"))
        require(value["scheme"] in ("http", "tcp"), "invalid_contract", "Invalid endpoint scheme.")
        return cls(contract_name(value["name"]), AllocatedEndpoint.parse(value["endpoint"]), value["scheme"])


@dataclass(frozen=True)
class ResolvedBundleBinding:
    name: str
    path: str
    artifact_sha256: str

    @classmethod
    def parse(cls, value):
        contract_object(value, ("name", "path", "artifact_sha256"))
        return cls(contract_name(value["name"]), contract_path(value["path"], absolute=True),
                   contract_digest(value["artifact_sha256"]))


@dataclass(frozen=True)
class RoleBindings:
    role: str
    executables: tuple[ResolvedExecutableBinding, ...]
    endpoints: tuple[ResolvedEndpointBinding, ...]
    bundles: tuple[ResolvedBundleBinding, ...]

    @classmethod
    def parse(cls, value):
        v = _record(value, ("role", "executables", "endpoints", "bundles"))
        executables = contract_list(v["executables"], ResolvedExecutableBinding.parse, maximum=128)
        endpoints = contract_list(v["endpoints"], ResolvedEndpointBinding.parse, maximum=128)
        bundles = contract_list(v["bundles"], ResolvedBundleBinding.parse, maximum=64)
        names = [b.name for b in (*executables, *endpoints, *bundles)]
        require(len(set(names)) == len(names), "invalid_contract", "Binding names must be unique across kinds.")
        return cls(contract_name(v["role"]), executables, endpoints, bundles)


@dataclass(frozen=True)
class UnitSpec:
    """Resolved immutable identity; runtime owners verify files before execution.

    Renderer policy is framework-owned, not arbitrary systemd directives. Digest
    references bind the typed role's policies and prepared mounts/configs.
    """
    identity: UnitIdentity
    executable: str
    executable_sha256: str
    arguments: tuple[str, ...]
    declaration_sha256: str
    bindings: RoleBindings
    configuration_sha256: str

    @classmethod
    def parse(cls, value):
        v = _record(value, ("identity", "executable", "executable_sha256", "arguments",
                            "declaration_sha256", "bindings", "configuration_sha256"))
        identity = UnitIdentity.parse(v["identity"])
        bindings = RoleBindings.parse(v["bindings"])
        require(identity.role == bindings.role, "invalid_contract", "Unit bindings belong to another role.")
        def argument(item):
            return "" if item == "" else contract_text(item, limit=4096)
        arguments = contract_list(v["arguments"], argument, maximum=128)
        require(sum(len(a.encode()) for a in arguments) <= 32768, "invalid_contract", "Resolved argv is too large.")
        return cls(identity, contract_path(v["executable"], absolute=True), contract_digest(v["executable_sha256"]),
                   arguments, contract_digest(v["declaration_sha256"]), bindings, contract_digest(v["configuration_sha256"]))


@dataclass(frozen=True)
class PayloadMember:
    path: str
    sha256: str
    size: int
    kind: str

    @classmethod
    def parse(cls, value):
        contract_object(value, ("path", "sha256", "size", "kind"))
        require(value["kind"] in ("application", "config", "artifact", "module", "data", "asset", "source-map"),
                "invalid_contract", "Unknown inventory member kind.")
        return cls(contract_path(value["path"]), contract_digest(value["sha256"]),
                   contract_int(value["size"], 0), value["kind"])


@dataclass(frozen=True)
class ArtifactPayload:
    artifact: str
    platform: str
    path: str
    sha256: str
    members: tuple[PayloadMember, ...]

    @classmethod
    def parse(cls, value):
        contract_object(value, ("artifact", "platform", "path", "sha256", "members"))
        require(value["platform"] in ("linux-amd64", "linux-arm64"), "invalid_contract", "Unsupported artifact architecture.")
        path = contract_path(value["path"])
        require(path.startswith("deploy/artifacts/"), "invalid_contract", "Artifact payload is outside its owned inventory area.")
        members = contract_list(value["members"], PayloadMember.parse, maximum=10000, minimum=1)
        require(len({m.path for m in members}) == len(members) and all(m.kind != "artifact" for m in members),
                "invalid_contract", "Artifact members must be unique; nested extraction is forbidden.")
        return cls(contract_name(value["artifact"]), value["platform"], path, contract_digest(value["sha256"]), members)


@dataclass(frozen=True)
class ReleaseDeclaration:
    """Schema-2 archive extension, alongside the existing manifest provenance.

    M2's builder/host envelope must carry this as `deployment`; legacy schema-1
    manifests omit it. This parser checks declarations, not file bytes or ABI.
    """
    topology_json: str
    profile_json: str
    topology_sha256: str
    inventory: tuple[PayloadMember, ...]
    artifacts: tuple[ArtifactPayload, ...]
    application_executables: tuple[tuple[str, str], ...]

    @classmethod
    def parse(cls, value):
        from topology import parse_topology, parse_profile, ExecutableRole
        v = contract_document(value)
        contract_object(v, ("schema", "topology", "profile", "topology_sha256", "inventory", "artifacts", "application_executables"))
        require(type(v["schema"]) is int and v["schema"] == ARCHIVE_SCHEMA, "unsupported_schema", "Unsupported archive declaration.")
        topology = parse_topology(v["topology"])
        require(topology.schema == TOPOLOGY_SCHEMA and topology.sha256 == contract_digest(v["topology_sha256"]),
                "invalid_contract", "Archive topology digest mismatch.")
        profile = parse_profile(v["profile"], topology)
        inventory = contract_list(v["inventory"], PayloadMember.parse, minimum=1, maximum=10000)
        artifacts = contract_list(v["artifacts"], ArtifactPayload.parse, maximum=MAX_ARTIFACTS)
        application_executables = contract_map(v["application_executables"], contract_path, maximum=64)
        files = {m.path: m for m in inventory}
        require({n for n, _ in application_executables} == {n for n, _ in topology.application_executables}
                and all(p.startswith("deploy/bin/") and p in files and files[p].kind == "application"
                        for _, p in application_executables), "invalid_contract", "Archive must inventory each application executable export.")
        exports = dict(application_executables)
        platform = "linux-" + profile.architecture
        require(all(binary.go_package is not None or exports[name] == dict(binary.platforms)[platform]
                    for name, binary in topology.application_executables),
                "invalid_contract", "Prebuilt executable export differs from its declared target architecture path.")
        require(len(files) == len(inventory) and len({a.artifact for a in artifacts}) == len(artifacts)
                and {a.artifact for a in artifacts} == {n for n, _ in topology.artifacts},
                "invalid_contract", "Archive inventories must be unique and cover all declared artifacts.")
        for role in topology.roles:
            if isinstance(role, ExecutableRole):
                require(all(c.source in files and files[c.source].kind == "config" for _, c in role.configs),
                        "invalid_contract", "Archive lacks a declared configuration file.")
        for payload in artifacts:
            artifact = dict(topology.artifacts)[payload.artifact]
            require(payload.platform == "linux-" + profile.architecture
                    and dict(artifact.platforms)[payload.platform].accepts_digest(payload.sha256)
                    and payload.path in files and files[payload.path].sha256 == payload.sha256
                    and files[payload.path].kind == "artifact", "invalid_contract", "Artifact payload differs from its architecture pin.")
            members = {m.path: m for m in payload.members}
            require({p for p, m in members.items() if m.kind == "application"} == {p for _, p in artifact.executables}
                    and {p for p, m in members.items() if m.kind == "module"} == set(artifact.modules)
                    and all(any(p.startswith(directory + "/") for p in members) for _, directory in artifact.bundles),
                    "invalid_contract", "Artifact inventory lacks declared exports/modules.")
        return cls(topology.canonical_json, profile.canonical_json, topology.sha256, inventory, artifacts, application_executables)

    def projection(self):
        from dataclasses import asdict
        result = {"schema": ARCHIVE_SCHEMA, "topology": json.loads(self.topology_json), "profile": json.loads(self.profile_json),
                  "topology_sha256": self.topology_sha256, "inventory": [asdict(m) for m in self.inventory],
                  "artifacts": [asdict(a) for a in self.artifacts], "application_executables": dict(self.application_executables)}
        result = contract_document(result)
        type(self).parse(result)
        return result


@dataclass(frozen=True)
class HostTransition:
    operation_id: str
    instance_id: str
    old_resolution_id: str
    candidate_resolution_id: str
    current_private_revision: int
    affected_units: tuple[UnitIdentity, ...]
    plan_sha256: str
    closure_id: str | None

    @classmethod
    def parse(cls, value):
        v = _record(value, ("operation_id", "instance_id", "old_resolution_id", "candidate_resolution_id",
                            "current_private_revision", "affected_units", "plan_sha256", "closure_id"))
        instance = contract_uuid(v["instance_id"])
        units = contract_list(v["affected_units"], UnitIdentity.parse, maximum=MAX_ROLES, minimum=1)
        require(len(set(units)) == len(units) and all(u.instance_id == instance for u in units),
                "invalid_contract", "Transition units belong to one instance.")
        return cls(contract_uuid(v["operation_id"]), instance, contract_uuid(v["old_resolution_id"]),
                   contract_uuid(v["candidate_resolution_id"]), contract_int(v["current_private_revision"]), units,
                   contract_digest(v["plan_sha256"]), None if v["closure_id"] is None else contract_uuid(v["closure_id"]))


@dataclass(frozen=True)
class Deployment:
    instance_id: str
    release: str
    archive_schema: int
    topology_sha256: str
    profile_sha256: str
    units: tuple[UnitSpec, ...]

    @classmethod
    def parse(cls, value):
        v = _record(value, ("instance_id", "release", "archive_schema", "topology_sha256", "profile_sha256", "units"))
        require(isinstance(v["release"], str) and VERSION_PATTERN.fullmatch(v["release"])
                and type(v["archive_schema"]) is int and v["archive_schema"] in (1, ARCHIVE_SCHEMA),
                "invalid_contract", "Invalid selected release identity/schema.")
        instance = contract_uuid(v["instance_id"])
        units = contract_list(v["units"], UnitSpec.parse, maximum=MAX_ROLES, minimum=1)
        require(len({u.identity for u in units}) == len(units)
                and all(u.identity.instance_id == instance for u in units)
                and any(u.identity.role == "http" for u in units), "invalid_contract", "Invalid deployment unit inventory.")
        return cls(instance, v["release"], v["archive_schema"], contract_digest(v["topology_sha256"]),
                   contract_digest(v["profile_sha256"]), units)


@dataclass(frozen=True)
class UnitGenerationReference:
    id: str
    unit: UnitIdentity
    bundle_sha256: str
    unit_spec_sha256: str
    resolution_id: str
    state: str
    queued_jobs: tuple[int, ...]
    control_processes: tuple[ProcessIdentity, ...]
    observations_complete: bool

    @classmethod
    def parse(cls, value):
        v = _record(value, ("id", "unit", "bundle_sha256", "unit_spec_sha256", "resolution_id",
                            "state", "queued_jobs", "control_processes", "observations_complete"))
        require(v["state"] in ("staged", "loaded", "retiring") and type(v["observations_complete"]) is bool,
                "invalid_contract", "Unknown loaded-unit generation state.")
        jobs = contract_list(v["queued_jobs"], contract_int)
        processes = contract_list(v["control_processes"], ProcessIdentity.parse)
        require(len(jobs) == len(set(jobs)) and len(processes) == len(set(processes)),
                "invalid_contract", "Duplicate manager observations.")
        return cls(contract_digest(v["id"]), UnitIdentity.parse(v["unit"]), contract_digest(v["bundle_sha256"]),
                   contract_digest(v["unit_spec_sha256"]), contract_uuid(v["resolution_id"]), v["state"],
                   jobs, processes, v["observations_complete"])


@dataclass(frozen=True)
class CleanupObligation:
    id: str
    operation_id: str
    instance_id: str
    resolution_id: str
    bundle_sha256: str
    helper_format: int
    state: str

    @classmethod
    def parse(cls, value):
        v = _record(value, ("id", "operation_id", "instance_id", "resolution_id", "bundle_sha256", "helper_format", "state"))
        require(v["state"] in ("pending", "complete") and type(v["helper_format"]) is int and v["helper_format"] == 1,
                "unsupported_capability", "Unknown cleanup reader/state; retain its bundle.")
        return cls(*(contract_uuid(v[k]) for k in ("id", "operation_id", "instance_id", "resolution_id")),
                   contract_digest(v["bundle_sha256"]), v["helper_format"], v["state"])


def record_document(record):
    """Encode only the frozen record family, then revalidate at the write boundary.

    Declarations use Topology/DeploymentProfile.projection(). This serializer owns
    nested record versions so individual lifecycle writers cannot omit them.
    """
    from dataclasses import fields as dataclass_fields
    versioned = (RuntimeResolution, AdmissionRecord, ClosureEvidence, ValidationPermit, HelperInvocation,
                 BundleReferences, DeploymentSelection, RecoveryPublicBinding, RoleBindings, UnitSpec,
                 HostTransition, Deployment, UnitGenerationReference, CleanupObligation)
    nested = (UnitIdentity, AllocatedEndpoint, EffectiveEnvironment, ProcessIdentity, RecoveryListenerBinding, DNSObservation,
              ResolvedExecutableBinding, ResolvedEndpointBinding, ResolvedBundleBinding)
    def encode(value):
        if type(value) in versioned + nested:
            result = {f.name: encode(getattr(value, f.name)) for f in dataclass_fields(value)}
            if type(value) in versioned:
                result = {"schema": RECORD_SCHEMA, **result}
            if isinstance(value, RecoveryListenerBinding):
                result.update(transport="tcp", dns_mode="external")
            if isinstance(value, DNSObservation) and value.state == "resolved":
                result.pop("state")  # Preserve existing resolved-observation wire records.
            return result
        if isinstance(value, Enum):
            return value.value
        if isinstance(value, tuple):
            return [encode(item) for item in value]
        require(value is None or type(value) in (str, int, bool), "invalid_contract", "Unsupported record member.")
        return value
    require(type(record) in versioned, "invalid_contract", "Unsupported record type.")
    result = encode(record)
    type(record).parse(result)
    return result
