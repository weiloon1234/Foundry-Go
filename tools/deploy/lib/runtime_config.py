"""Server-owned runtime configuration: one derivation and validation owner.

Shared by bootstrap, release activation and explicit configuration operations.
Never include returned credential values in logs, reports, or exceptions.
"""
from pathlib import Path
import base64
import binascii
import json
import os
import re
import secrets
import stat
import uuid

from contracts import (CLEANUP_GRACE_KEY, DEFAULT_CLEANUP_GRACE, DeployError, POSTGRES_PORT, REDIS_PORT, check_path,
                       dump_env, instance_paths, parse_env, read_json, require)

CREDENTIAL_KEYS = ("database_password", "redis_password")
ENCRYPTION_KEYS = tuple("APP__ENCRYPTION__" + suffix for suffix in ("KEY_ID", "KEY", "PREVIOUS"))
BOOTSTRAP_OWNED_KEYS = frozenset({*ENCRYPTION_KEYS, "APP__SERVICES__CREDENTIALS", "APP__SERVICES__STORAGE__DISKS"})
MAX_VALUE_BYTES = 1 << 20  # config.MaxTableBytes
MAX_ENCRYPTION_KEYS = 32  # encryption.MaxKeys


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
    bootstrap = _read_bootstrap_secrets(host, entry)
    if bootstrap is not None:
        value.update({key: bootstrap[key] for key in ("encryption", "app_storage") if key in bootstrap})
        value["encryption"] = dict(value["encryption"])
        value["encryption"]["key_file"] = str(host.paths.instance_config(entry["name"]) / "bootstrap-encryption.key")
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
        "host": "localhost", "port": c["postgres_port"], "user": e["allocations"]["database"], "password": c["database_password"],
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


# Settings that validate only together (a key ring's ID and material) change in
# one validated revision; each change set stays small and explicit.
MAX_CONFIGURATION_CHANGES = 16


def validate_configuration_changes(changes):
    require(isinstance(changes, dict) and 0 < len(changes) <= MAX_CONFIGURATION_CHANGES,
            "invalid_configuration", "Name between one and sixteen configuration keys to change together.")
    for key in changes:
        require(isinstance(key, str) and re.fullmatch(r"[A-Z][A-Z0-9_]*", key),
                "invalid_configuration", "Configuration key name is invalid.")
        logical = key.removesuffix("_FILE")
        require(not any(logical == owned or logical.startswith(owned + "__") for owned in PIPELINE_KEYS)
                and not key.startswith(("DEPLOY_", "FOUNDRY_", "VITE_")),
                "owned_configuration", "Identity, infrastructure and seed credentials cannot be edited by generic configuration commands.")
    dump_env({key: value for key, value in changes.items() if value is not None})


def runtime_environment(entry, credentials, *, postgres_port=POSTGRES_PORT):
    require(type(postgres_port) is int and POSTGRES_PORT <= postgres_port <= POSTGRES_PORT + 99,
            "postgres_identity", "Invalid allocated Foundry PostgreSQL port.")
    credentials = {**credentials, "postgres_port": postgres_port}
    values = {key: resolve(entry, credentials) for key, resolve in _FIELDS.items()}
    encryption = credentials.get("encryption")
    if encryption:
        values[ENCRYPTION_KEYS[0]] = encryption["key_id"]
        if encryption.get("key_file"):
            values[ENCRYPTION_KEYS[1] + "_FILE"] = encryption["key_file"]
        else:
            values[ENCRYPTION_KEYS[1]] = encryption["key"]
        values[ENCRYPTION_KEYS[2]] = "{}"
    storage = credentials.get("app_storage")
    if storage:
        values["APP__SERVICES__CREDENTIALS"] = _json(storage["credentials"])
        values["APP__SERVICES__STORAGE__DISKS"] = _json(storage["disks"])
        values[CLEANUP_GRACE_KEY] = DEFAULT_CLEANUP_GRACE
    # Initial application settings (such as first-deploy seed credentials) apply last.
    values.update(credentials.get("configuration") or {})
    return values


def _validate_key(key_id, material):
    require(isinstance(key_id, str) and len(key_id) <= 128 and re.fullmatch(r"[a-z0-9][a-z0-9_.-]*", key_id),
            "invalid_encryption", "Encryption key ID is invalid.")
    require(isinstance(material, str) and re.fullmatch(r"[A-Za-z0-9_-]{43}", material),
            "invalid_encryption", "Encryption key must be canonical unpadded base64url for 32 bytes.")
    try:
        raw = base64.b64decode(material + "=", altchars=b"-_", validate=True)
    except (ValueError, binascii.Error):
        raise DeployError("invalid_encryption", "Encryption key encoding is invalid.") from None
    require(len(raw) == 32 and base64.urlsafe_b64encode(raw).decode().rstrip("=") == material,
            "invalid_encryption", "Encryption key encoding is not canonical.")


def _encryption_pair(encryption=None):
    if encryption is None:
        encryption = {"key_id": "app_" + uuid.uuid4().hex, "key": secrets.token_urlsafe(32)}
    require(isinstance(encryption, dict) and set(encryption) == {"key_id", "key"},
            "invalid_encryption", "Supply the encryption key ID and key together.")
    _validate_key(encryption["key_id"], encryption["key"])
    return dict(encryption)


def _read_bootstrap_secrets(host, entry):
    path = check_path(host.paths.instance_config(entry["name"]) / "bootstrap-secrets.json", "file")
    if not path.exists():
        return None
    info = path.stat()
    require(stat.S_IMODE(info.st_mode) == 0o600 and (info.st_uid == 0 or host.paths.root != Path("/")),
            "configuration_ownership", "Bootstrap secrets must be root-owned with mode 0600.")
    value = read_json(path)
    require(isinstance(value, dict) and value.get("instance_id") == entry["profile"]["instance_id"],
            "provision_conflict", "Bootstrap secret identity differs from the selected instance.")
    require(isinstance(value.get("encryption"), dict), "credentials_missing", "Restore the initialized encryption key; it must never be regenerated.")
    _encryption_pair(value["encryption"])
    return value


def initialize_bootstrap_secrets(host, entry, private_secrets=None):
    """Persist private stdin intake once; retries never rotate or replace secrets."""
    credentials = read_credentials(host, entry)
    supplied = {} if private_secrets is None else private_secrets
    allowed = {"cloudflare_dns_token", "backup_s3", "age_recipients", "app_storage", "encryption", "configuration"}
    require(isinstance(supplied, dict) and not set(supplied) - allowed,
            "invalid_secrets", "Bootstrap secret input contains unsupported fields.")
    supplied = dict(supplied)
    if "encryption" in supplied:
        require(isinstance(supplied["encryption"], dict), "invalid_encryption", "Supply the encryption key ID and key together.")
        supplied["encryption"] = _encryption_pair(supplied["encryption"])
    if "cloudflare_dns_token" in supplied:
        token = supplied["cloudflare_dns_token"]
        require(isinstance(token, str) and token and len(token) <= 4096 and not any(c.isspace() for c in token),
                "invalid_secrets", "Cloudflare DNS token is invalid.")
    if "backup_s3" in supplied:
        repository = supplied["backup_s3"]
        required = {"endpoint", "region", "bucket", "access_key_id", "secret_access_key"}
        require(isinstance(repository, dict) and set(repository) == required
                and all(isinstance(value, str) and value and not any(c in value for c in "\x00\r\n") for value in repository.values()),
                "invalid_secrets", "Backup credentials are incomplete or invalid.")
    if "age_recipients" in supplied:
        recipients = supplied["age_recipients"]
        require(isinstance(recipients, list) and 1 <= len(recipients) <= 32
                and all(isinstance(value, str) and value and not any(c.isspace() for c in value) for value in recipients),
                "invalid_secrets", "Supply public age recipients as a bounded nonempty list.")
    if "app_storage" in supplied:
        storage = supplied["app_storage"]
        require(isinstance(storage, dict) and set(storage) == {"credentials", "disks"},
                "invalid_secrets", "App storage requires credentials and disks JSON together.")
        storage = dict(storage)
        for key in storage:
            if isinstance(storage[key], str):
                try:
                    storage[key] = json.loads(storage[key])
                except ValueError:
                    raise DeployError("invalid_secrets", "App storage JSON is invalid.") from None
            require(isinstance(storage[key], dict) and storage[key], "invalid_secrets", "App storage JSON must be a nonempty object.")
        supplied["app_storage"] = storage
    if "configuration" in supplied:
        configuration = supplied["configuration"]
        validate_configuration_changes(configuration)
        require(all(isinstance(value, str) for value in configuration.values())
                and not any(key.removesuffix("_FILE") in BOOTSTRAP_OWNED_KEYS for key in configuration),
                "owned_configuration", "Encryption and storage are supplied through their own bootstrap inputs.")
    prior = _read_bootstrap_secrets(host, entry)
    if prior is not None:
        require(all(prior.get(key) == value for key, value in supplied.items()),
                "provision_conflict", "Bootstrap secrets already exist; use an explicit configuration or credential rotation operation.")
    else:
        key_file = host.paths.instance_config(entry["name"]) / "bootstrap-encryption.key"
        require(not key_file.exists() and not key_file.is_symlink()
                and "bootstrap_secrets" not in entry.get("provisioning", {}).get("completed_steps", []),
                "credentials_missing", "Restore the bootstrap secret record before retrying.")
        env = host.paths.instance_config(entry["name"]) / "app.env"
        if env.exists():
            existing = parse_env(check_path(env, "file").read_text())
            require(not any(key.startswith("APP__ENCRYPTION__") for key in existing),
                    "credentials_missing", "Restore the bootstrap encryption record before retrying.")
        prior = {"instance_id": entry["profile"]["instance_id"], **supplied}
        if "encryption" not in prior:
            prior["encryption"] = _encryption_pair()
        host.owned_write(host.paths.instance_config(entry["name"]) / "bootstrap-secrets.json", _json(prior) + "\n", 0o600)
    key_file = check_path(host.paths.instance_config(entry["name"]) / "bootstrap-encryption.key", "file")
    material = prior["encryption"]["key"]
    if key_file.exists():
        require(_read_value_file(host, entry, str(key_file)) == material,
                "provision_conflict", "Bootstrap encryption value file differs from its recorded key.")
    else:
        host.owned_write(key_file, material + "\n", 0o640, group=entry["name"])
    credentials.update({key: prior[key] for key in ("encryption", "app_storage", "configuration") if key in prior})
    credentials["encryption"] = {**credentials["encryption"], "key_file": str(key_file)}
    return credentials


def _read_value_file(host, entry, value):
    """Read only app-readable, root-owned files inside this instance's config."""
    require(isinstance(value, str) and value and "\x00" not in value,
            "invalid_configuration", "Configuration file reference is invalid.")
    directory = host.check_control_directory(host.paths.instance_config(entry["name"]))
    path = Path(value)
    require(path.is_absolute() and path.is_relative_to(directory) and ".." not in path.parts,
            "configuration_ownership", "Configuration value files must belong to the selected instance.")
    check_path(path, "file")
    for parent in path.parents:
        if parent == directory:
            break
        host.check_control_directory(parent)
    try:
        fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
        with os.fdopen(fd, "rb") as source:
            info = os.fstat(source.fileno())
            group = _instance_group(host, entry)
            require(stat.S_ISREG(info.st_mode) and stat.S_IMODE(info.st_mode) == 0o640
                    and (info.st_uid == 0 or host.paths.root != Path("/"))
                    and (group is None or info.st_gid == group),
                    "configuration_ownership", "Configuration value files must be root-owned and readable only by the instance group.")
            if host.paths.root == Path("/"):
                readable = host.run(["runuser", "-u", entry["name"], "--", "test", "-r", str(path)], check=False)
                require(readable.returncode == 0, "configuration_ownership", "Configuration value file is not readable by the instance account.")
            data = source.read(MAX_VALUE_BYTES + 1)
        require(len(data) <= MAX_VALUE_BYTES, "invalid_configuration", "Configuration value file exceeds its byte limit.")
        text = data.decode("utf-8")
    except (OSError, UnicodeError):
        raise DeployError("invalid_configuration", "Configuration value file cannot be read safely.") from None
    return text[:-1].removesuffix("\r") if text.endswith("\n") else text


def candidate_values(host, entry, existing, changes):
    """Resolve aliases together and snapshot external inputs before selection."""
    values = dict(existing)
    logical = set()
    for key in changes:
        name = key.removesuffix("_FILE")
        require(name not in logical, "invalid_configuration", "A configuration key and its _FILE alias cannot both be changed.")
        logical.add(name)
    for key, value in changes.items():
        name = key.removesuffix("_FILE")
        values.pop(name, None)
        values.pop(name + "_FILE", None)
        if value is not None:
            values[key] = value
    validate_keyring_change(host, entry, existing, values)
    for key in changes:
        if key.endswith("_FILE") and key in values:
            content = _read_value_file(host, entry, values[key])
            destination = host.paths.instance_config(entry["name"]) / ("value-" + uuid.uuid4().hex + ".secret")
            host.owned_write(destination, content + "\n", 0o640, group=entry["name"])
            values[key] = str(destination)
    return values


def _keyring(host, entry, values):
    def value(name):
        require(not (name in values and name + "_FILE" in values), "invalid_encryption", "Encryption configuration contains conflicting aliases.")
        return _read_value_file(host, entry, values[name + "_FILE"]) if name + "_FILE" in values else values.get(name, "")
    key_id, material, previous = (value(name) for name in ENCRYPTION_KEYS)
    try:
        retained = json.loads(previous or "{}")
    except ValueError:
        raise DeployError("invalid_encryption", "Retained encryption keys must be a valid JSON object.") from None
    require(isinstance(retained, dict), "invalid_encryption", "Retained encryption keys must be a JSON object.")
    prefix = ENCRYPTION_KEYS[2] + "__"
    for name in values:
        if name.startswith(prefix):
            logical = name.removesuffix("_FILE")
            parts = logical[len(prefix):].split("__")
            require(len(parts) == 2 and parts[1] == "KEY", "invalid_encryption", "Retained encryption override is invalid.")
            retained[parts[0].lower()] = {"key": value(logical)}
    if not key_id and not material and not retained:
        return {}
    _validate_key(key_id, material)
    require(key_id not in retained and len(retained) < MAX_ENCRYPTION_KEYS,
            "invalid_encryption", "Encryption keyring exceeds its limit or duplicates the active key.")
    ring = {key_id: material}
    for previous_id, item in retained.items():
        require(isinstance(item, dict) and set(item) == {"key"}, "invalid_encryption", "Retained encryption key is invalid.")
        _validate_key(previous_id, item["key"])
        require(item["key"] not in ring.values(), "invalid_encryption", "Encryption key material cannot be reused under another ID.")
        ring[previous_id] = item["key"]
    return ring


def validate_keyring_change(host, entry, before, after):
    old = _keyring(host, entry, before)
    new = _keyring(host, entry, after)
    require(all(new.get(key_id) == material for key_id, material in old.items()),
            "encryption_retention", "Configuration cannot remove retained encryption keys or replace their material.")


def rotation_changes(host, entry, values, encryption=None):
    ring = _keyring(host, entry, values)
    new = _encryption_pair(encryption)
    require(new["key_id"] not in ring and new["key"] not in ring.values() and len(ring) < MAX_ENCRYPTION_KEYS,
            "invalid_encryption", "Rotation requires a new key ID and material within the 32-key limit.")
    return {ENCRYPTION_KEYS[0]: new["key_id"], ENCRYPTION_KEYS[1]: new["key"],
            ENCRYPTION_KEYS[2]: _json({key_id: {"key": material} for key_id, material in ring.items()})}


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
    registry = host.read_registry()
    postgres_port = registry["shared"].get("postgresql", {}).get("port", POSTGRES_PORT)
    require(host.paths.root != Path("/") or "postgresql" in registry["shared"],
            "postgres_identity", "The Foundry PostgreSQL allocation is missing.")
    expected = runtime_environment(entry, credentials, postgres_port=postgres_port)
    if any(values.get(key) != value for key, value in expected.items() if key in PIPELINE_KEYS):
        raise DeployError("configuration_identity", "Pipeline-owned runtime configuration differs from registered identity/credentials.")
    for key in values:
        logical = key.removesuffix("_FILE")
        if (logical in PIPELINE_KEYS and (logical not in expected or key != logical)
                or any(logical.startswith(owner + "__") for owner in PIPELINE_KEYS)):
            raise DeployError("configuration_identity", "Pipeline-owned runtime configuration contains an overriding alias or collection field.")
    return values
