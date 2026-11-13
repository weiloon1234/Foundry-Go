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
