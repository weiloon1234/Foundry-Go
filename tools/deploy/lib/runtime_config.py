"""Server-owned runtime configuration: one derivation and validation owner.

Shared by bootstrap, release activation and explicit configuration operations.
Never include returned credential values in logs, reports, or exceptions.
"""
from pathlib import Path
import json
import re
import stat

from contracts import DeployError, POSTGRES_PORT, REDIS_PORT, check_path, instance_paths, parse_env, read_json

CREDENTIAL_KEYS = ("database_password", "redis_password")


def read_credentials(host, entry, missing=False):
    for directory in (host.paths.etc, host.paths.etc / "instances", host.paths.instance_config(entry["name"])):
        host.check_control_directory(directory)
    path = host.paths.instance_config(entry["name"]) / "secrets.json"
    check_path(path)
    if not path.exists() and missing:
        return None
    if not path.exists() or (path.stat().st_uid != 0 and host.paths.root == Path("/")) or path.stat().st_mode & 0o077:
        raise DeployError("provision_conflict", "instance credential file missing or unsafe; restore it, never regenerate")
    value = read_json(path)
    if not isinstance(value, dict) or value.get("instance_id") != entry["profile"]["instance_id"]:
        raise DeployError("provision_conflict", "instance credential identity mismatch")
    for key in CREDENTIAL_KEYS:
        if not re.fullmatch(r"[A-Za-z0-9_-]{32,128}", value.get(key, "")):
            raise DeployError("provision_conflict", "instance credential file invalid")
    return value


def _json(value):
    return json.dumps(value, separators=(",", ":"))


# Each key is declared once. Generic editing locks these generated values while
# allowing the display name; release identity comes from the binary's linker.
_FIELDS = {
    "PROJECT__ENV": lambda e, c: e["profile"]["runtime_environment"],
    "PROJECT__NAME": lambda e, c: e["allocations"]["namespace"]["application"],
    "APP__HTTP__SERVER__ADDRESS": lambda e, c: f"127.0.0.1:{e['allocations']['http_port']}",
    "APP__SERVICES__NAMESPACE__APPLICATION": lambda e, c: e["allocations"]["namespace"]["application"],
    "APP__SERVICES__NAMESPACE__ENVIRONMENT": lambda e, c: e["allocations"]["namespace"]["environment"],
    "APP__SERVICES__DATABASE__CONNECTIONS": lambda e, c: _json({"primary": {"primary": {
        "host": "localhost", "port": POSTGRES_PORT, "user": e["allocations"]["database"], "password": c["database_password"],
        "database": e["allocations"]["database"], "tls": "verify-full"}}}),
    "APP__SERVICES__REDIS__CONNECTIONS": lambda e, c: _json({name: {
        "host": "localhost", "port": REDIS_PORT, "user": e["allocations"]["redis_user"], "password": c["redis_password"], "tls": "verify-full"}
        for name in ("main", "background")}),
    "TRANSPORT__UPLOAD_DIRECTORY": lambda e, c: instance_paths(e["name"])["data"] + "/uploads",
    "WEB__PUBLIC_DIR": lambda e, c: instance_paths(e["name"])["application"] + "/current/public",
    "DEVELOPER__LOG_DIRECTORY": lambda e, c: instance_paths(e["name"])["data"] + "/logs",
    "DEVELOPER__SOURCE_MAP_DIRECTORY": lambda e, c: instance_paths(e["name"])["data"] + "/private-source-maps",
}
PIPELINE_KEYS = frozenset(_FIELDS) - {"PROJECT__NAME"} | {"DEVELOPER__RELEASE"}


def runtime_environment(entry, credentials):
    return {key: resolve(entry, credentials) for key, resolve in _FIELDS.items()}


def _instance_group(host, entry, account=None):
    if host.paths.root != Path("/"):
        return None  # Filesystem fixtures do not create real operating-system users.
    if account is None:
        result = host.run(["getent", "passwd", entry["name"]], check=False)
        account = result.stdout.strip().split(":") if result.returncode == 0 else []
    fields = account
    if (len(fields) != 7 or fields[0] != entry["name"]
            or not all(value.isdigit() and int(value) > 0 for value in fields[2:4])):
        raise DeployError("configuration_ownership", "Instance account group cannot be verified.")
    group = host.run(["getent", "group", entry["name"]], check=False)
    members = group.stdout.strip().split(":")
    if (group.returncode or len(members) != 4 or members[0] != entry["name"] or members[2] != fields[3]
            or set(filter(None, members[3].split(","))) - {entry["name"]}):
        raise DeployError("configuration_ownership", "Instance group membership differs from the isolated service account.")
    # getent group omits users whose primary GID is this group. They can still
    # read app.env, so inspect the passwd inventory as well as explicit members.
    inventory = host.run(["getent", "passwd"], check=False)
    accounts = [line.split(":") for line in inventory.stdout.splitlines()]
    if (inventory.returncode or fields not in accounts
            or any(len(item) != 7 or not all(value.isdigit() for value in item[2:4]) for item in accounts)
            or any(item[0] != entry["name"] and (item[2] == fields[2] or item[3] == fields[3]) for item in accounts)):
        raise DeployError("configuration_ownership", "Instance UID or primary group is shared with another account or cannot be inspected.")
    supplementary = host.run(["id", "-G", entry["name"]], check=False)
    if supplementary.returncode or set(supplementary.stdout.split()) != {fields[3]}:
        raise DeployError("configuration_ownership", "Instance service account has unexpected supplementary groups.")
    return int(fields[3])


def validate_runtime_environment(host, entry):
    credentials = read_credentials(host, entry)
    path = check_path(host.paths.instance_config(entry["name"]) / "app.env", "file")
    if not path.exists():
        raise DeployError("configuration_missing", "Server-owned runtime configuration is missing; restore it explicitly.")
    info = path.stat()
    if stat.S_IMODE(info.st_mode) != 0o640 or info.st_uid != 0 and host.paths.root == Path("/"):
        raise DeployError("configuration_ownership", "Instance environment must be root-owned with mode 0640.")
    group = _instance_group(host, entry)
    if group is not None and info.st_gid != group:
        raise DeployError("configuration_ownership", "Instance environment group differs from the selected service account.")
    values = parse_env(path.read_text(encoding="utf-8"))
    expected = runtime_environment(entry, credentials)
    if any(values.get(key) != value for key, value in expected.items() if key in PIPELINE_KEYS):
        raise DeployError("configuration_identity", "Pipeline-owned runtime configuration differs from registered identity/credentials.")
    if "DEVELOPER__RELEASE" in values:
        raise DeployError("configuration_identity", "Release identity comes from the compiled binary; remove the environment override.")
    return values
