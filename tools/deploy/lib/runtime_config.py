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
import uploads

from contracts import (CLEANUP_GRACE_KEY, DEFAULT_CLEANUP_GRACE, DeployError, POSTGRES_PORT, REDIS_PORT, check_path,
                       dump_env, instance_paths, normalize_backup_repository, normalize_storage_disks, parse_env,
                       read_json, require, storage_bindings)

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
PIPELINE_KEYS = frozenset(_FIELDS) - {"PROJECT__NAME"} | {"DEVELOPER__RELEASE"} | (uploads.RUNTIME_KEYS - {"APP__FEATURES__ATTACHMENTS__CONFIG__TIMEOUT"})


# Settings that validate only together (a key ring's ID and material) change in
# one validated revision; each change set stays small and explicit.
MAX_CONFIGURATION_CHANGES = 16


def configuration_parts(changes, entry=None, *, topology=None):
    from topology import SERVICE_SECRET_PREFIX, parse_topology, service_secret_keys
    topology = topology or (parse_topology(entry["topology"], registered="sha256" in entry["topology"])
                            if entry and "topology" in entry else None)
    names = service_secret_keys(dict(topology.secrets)) if topology else {}
    application, service = {}, {}
    for key, value in changes.items():
        if isinstance(key, str) and key.startswith(SERVICE_SECRET_PREFIX):
            require(key in names or key.endswith("_FILE") and key[:-5] in names,
                    "unknown_configuration", "Unknown declared service secret.")
            service[key] = value
        else:
            application[key] = value
    logical = [key if key in names else key[:-5] for key in service]
    require(len(logical) == len(set(logical)), "invalid_configuration",
            "A service secret and its _FILE alias cannot both be supplied.")
    return application, service


SCHEDULER_ENABLED = "APP__SCHEDULER__ENABLED"


def scheduler_environment(roles):
    return {SCHEDULER_ENABLED: "true" if "scheduler" in roles else "false"}


def pipeline_keys(entry=None):
    scheduler = {SCHEDULER_ENABLED} if entry and entry["profile"].get("schema") == 3 else frozenset()
    return PIPELINE_KEYS | scheduler | (uploads.RUNTIME_KEYS if entry and "uploads" in entry["profile"] else frozenset())


def validate_configuration_changes(changes, entry=None):
    require(isinstance(changes, dict) and 0 < len(changes) <= MAX_CONFIGURATION_CHANGES,
            "invalid_configuration", "Name between one and sixteen configuration keys to change together.")
    application, _ = configuration_parts(changes, entry)
    for key in application:
        require(isinstance(key, str) and re.fullmatch(r"[A-Z][A-Z0-9_]*", key),
                "invalid_configuration", "Configuration key name is invalid.")
        logical = key.removesuffix("_FILE")
        require(not any(logical == owned or logical.startswith(owned + "__") for owned in pipeline_keys(entry))
                and not key.startswith(("DEPLOY_", "FOUNDRY_", "VITE_")),
                "owned_configuration", "Identity, infrastructure and seed credentials cannot be edited by generic configuration commands.")
    dump_env({key: value for key, value in changes.items() if value is not None})


def service_secret_snapshot(host, entry):
    """Current private paths; never include this record in a public response."""
    from admission import read_owned
    path = host.paths.instance_config(entry["name"]) / "service-secrets.json"
    if not check_path(path, "file").exists():
        return {}
    require(stat.S_IMODE(path.stat().st_mode) == 0o600,
            "configuration_ownership", "The service secret index must remain private.")
    value = read_owned(host, path, lambda value: value)
    require(isinstance(value, dict) and set(value) == {"schema", "instance_id", "paths"}
            and value["schema"] == 1 and value["instance_id"] == entry["profile"]["instance_id"]
            and isinstance(value["paths"], dict) and len(value["paths"]) <= 128,
            "service_secret", "Invalid private service secret snapshot.")
    from topology import service_secret_keys
    service_secret_keys(value["paths"])
    for path in value["paths"].values():
        _read_value_file(host, entry, path)
    return dict(value["paths"])


def stage_service_secrets(host, entry, topology, changes, *, required=True):
    """Snapshot inputs without selecting them; callers hold host then instance locks."""
    from topology import service_secret_keys
    _, changes = configuration_parts(changes, topology=topology)
    names = service_secret_keys(dict(topology.secrets))
    paths = service_secret_snapshot(host, entry)
    for key, value in changes.items():
        indirect = key not in names
        name = names[key[:-5] if indirect else key]
        if value is None:
            paths.pop(name, None)
            continue
        content = _read_value_file(host, entry, value) if indirect else value
        require(isinstance(content, str) and 0 < len(content.encode("utf-8")) <= MAX_VALUE_BYTES
                and "\x00" not in content, "invalid_configuration", "Service secret value is invalid.")
        if name in paths and _read_value_file(host, entry, paths[name]) == content:
            continue
        path = host.paths.instance_config(entry["name"]) / ("service-" + uuid.uuid4().hex + ".secret")
        host.owned_write(path, content + "\n", 0o640, group=entry["name"])
        paths[name] = str(path)
    if required:
        service_secret_paths(host, entry, topology, paths=paths)
    return paths


def select_service_secrets(host, entry, paths):
    """Publish the private selection as part of a configuration revision."""
    for path in paths.values():
        _read_value_file(host, entry, path)
    host.owned_write(host.paths.instance_config(entry["name"]) / "service-secrets.json",
                     _json({"schema": 1, "instance_id": entry["profile"]["instance_id"], "paths": paths}) + "\n", 0o600)


def service_secret_paths(host, entry, topology, *, paths=None, profile=None):
    """Resolve only each placed service's declared file interfaces, never values."""
    from topology import ExecutableRole
    paths = service_secret_snapshot(host, entry) if paths is None else paths
    placement = dict(profile.placement) if profile else entry["profile"].get("placement", {})
    result = {}
    for role in topology.roles:
        name = role.common.name
        if not isinstance(role, ExecutableRole) or placement.get(name, 1) == 0:
            continue
        result[name] = {}
        for variable, secret in role.secret_files:
            require(secret in paths, "service_secret_missing", "Supply required service secrets before reviewing the release plan.")
            _read_value_file(host, entry, paths[secret])
            result[name][variable] = paths[secret]
    return result


def service_secret_snapshot_paths(host, entry):
    """Backup/recovery interface: exact current service-secret files, including retained names."""
    paths = service_secret_snapshot(host, entry)
    index = host.paths.instance_config(entry["name"]) / "service-secrets.json"
    return tuple([index, *(Path(path) for path in sorted(set(paths.values())))]) if index.exists() else ()


def service_secret_requirements(topology, profile, paths):
    from topology import ExecutableRole, service_secret_keys
    placement = dict(profile.placement)
    required = {secret for role in topology.roles if isinstance(role, ExecutableRole)
                and placement[role.common.name] for _, secret in role.secret_files}
    keys = service_secret_keys(dict(topology.secrets))
    prompts = dict(topology.secrets)
    return [{"key": key, "prompt": prompts[name], "present": name in paths, "redacted": True}
            for key, name in keys.items() if name in required]


def stage_service_secret_intent(host, entry, topology, profile, changes):
    """Private, immutable review input. The public plan receives only its opaque ID."""
    from admission import protected_directory
    from contracts import digest
    paths = stage_service_secrets(host, entry, topology, changes, required=False)
    service_secret_paths(host, entry, topology, paths=paths, profile=profile)
    root = host.paths.instance_config(entry["name"]) / "service-secret-intents"
    protected_directory(host, root, 0o700)
    require(len(list(root.iterdir())) < 128, "service_secret_intents", "Retained private intents require reconciliation.")
    identifier = str(uuid.uuid4())
    record = {"schema": 1, "id": identifier, "instance_id": entry["profile"]["instance_id"],
              "private_revision": entry["configuration_revision"], "topology_sha256": topology.sha256,
              "paths": paths, "sha256": {name: digest(Path(path)) for name, path in paths.items()}}
    host.owned_write(root / (identifier + ".json"), _json(record) + "\n", 0o600)
    return identifier


def read_service_secret_intent(host, entry, identifier, topology, profile):
    from admission import read_owned
    from contracts import contract_uuid, digest
    path = host.paths.instance_config(entry["name"]) / "service-secret-intents" / (contract_uuid(identifier) + ".json")
    record = read_owned(host, path, lambda value: value)
    require(stat.S_IMODE(path.stat().st_mode) == 0o600,
            "configuration_ownership", "Private configuration intents require mode 0600.")
    require(record.get("schema") == 1 and record.get("id") == identifier
            and record.get("instance_id") == entry["profile"]["instance_id"]
            and record.get("private_revision") == entry["configuration_revision"]
            and record.get("topology_sha256") == topology.sha256,
            "stale_plan", "Private configuration intent no longer matches the reviewed transition.")
    paths = record["paths"]
    require(isinstance(paths, dict) and len(paths) <= 128, "service_secret", "Invalid private snapshot paths.")
    for value in paths.values():
        _read_value_file(host, entry, value)
    service_secret_paths(host, entry, topology, paths=paths, profile=profile)
    require({name: digest(check_path(Path(value), "file")) for name, value in paths.items()} == record["sha256"],
            "stale_plan", "Private configuration snapshot changed after review.")
    return dict(paths)


def stage_configuration_revision(host, entry, changes, topology, profile):
    """Use the existing configuration aliases/keyring rules before lifecycle effects."""
    from admission import protected_directory
    from contracts import digest
    validate_configuration_changes(changes, {**entry, "topology": topology.projection()})
    application, service = configuration_parts(changes, topology=topology)
    current = validate_runtime_environment(host, entry)
    values = candidate_values(host, entry, current, application)
    paths = stage_service_secrets(host, entry, topology, service)
    identifier = str(uuid.uuid4())
    directory = host.paths.instance_config(entry["name"])
    candidate = directory / ("revision-" + identifier + ".env")
    host.owned_write(candidate, dump_env(values), 0o640, group=entry["name"])
    root = directory / "configuration-intents"
    protected_directory(host, root, 0o700)
    require(len(list(root.iterdir())) < 128, "configuration_reconciliation", "Reconcile retained configuration intents.")
    record = {"schema": 1, "id": identifier, "instance_id": entry["profile"]["instance_id"],
              "revision": entry["configuration_revision"] + 1, "path": str(candidate), "sha256": digest(candidate),
              "previous_sha256": digest(directory / "app.env"), "paths": paths,
              "secret_sha256": {name: digest(Path(path)) for name, path in paths.items()},
              "application_keys": list(application), "service_keys": list(service)}
    host.owned_write(root / (identifier + ".json"), _json(record) + "\n", 0o600)
    return identifier


def read_configuration_revision(host, entry, identifier):
    from admission import read_owned
    from contracts import contract_uuid, digest
    directory = host.paths.instance_config(entry["name"])
    record = read_owned(host, directory / "configuration-intents" / (contract_uuid(identifier) + ".json"), lambda value: value)
    require(record.get("schema") == 1 and record.get("id") == identifier
            and record.get("instance_id") == entry["profile"]["instance_id"]
            and record.get("revision") == entry["configuration_revision"] + 1
            and digest(directory / "app.env") == record.get("previous_sha256"),
            "stale_plan", "Configuration changed after private review.")
    content = _read_value_file(host, entry, record["path"])
    require(digest(Path(record["path"])) == record["sha256"], "stale_plan", "Private configuration intent changed.")
    for name, path in record["paths"].items():
        _read_value_file(host, entry, path)
        require(digest(Path(path)) == record["secret_sha256"][name], "stale_plan", "Private service snapshot changed.")
    return record, parse_env(content)


def publish_configuration_revision(host, registry, entry, intent):
    """Caller journals configuration publication first; an interrupted write stays pending."""
    record, values = read_configuration_revision(host, entry, intent)
    select_service_secrets(host, entry, record["paths"])
    host.owned_write(host.paths.instance_config(entry["name"]) / "app.env", dump_env(values), 0o640, group=entry["name"])
    entry["configuration_revision"] = record["revision"]
    host.journal(registry, entry)


def runtime_environment(entry, credentials, *, postgres_port=POSTGRES_PORT):
    require(type(postgres_port) is int and POSTGRES_PORT <= postgres_port <= POSTGRES_PORT + 99,
            "postgres_identity", "Invalid allocated Foundry PostgreSQL port.")
    credentials = {**credentials, "postgres_port": postgres_port}
    values = {key: resolve(entry, credentials) for key, resolve in _FIELDS.items()}
    values.update(uploads.runtime_values(entry))
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
    application, _ = configuration_parts(credentials.get("configuration") or {}, entry)
    values.update(application)
    if entry["profile"].get("schema") == 3:
        values.pop(SCHEDULER_ENABLED + "_FILE", None)
        values.update(scheduler_environment(name for name, count in entry["profile"]["placement"].items() if count))
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


def update_bootstrap_secrets(host, entries, changes):
    """Replace intake records under the host and affected instance locks."""
    records = []
    for entry in entries:
        prior = _read_bootstrap_secrets(host, entry)
        require(prior is not None, "credentials_missing", "Restore the bootstrap secret record before rotation.")
        path = host.paths.instance_config(entry["name"]) / "bootstrap-secrets.json"
        records.append((path, path.read_text(), {**prior, **changes}))
    written = []
    try:
        for path, original, updated in records:
            written.append((path, original))
            host.owned_write(path, _json(updated) + "\n", 0o600)
    except (DeployError, OSError):
        for path, original in reversed(written):
            host.owned_write(path, original, 0o600)
        raise


def initialize_bootstrap_secrets(host, entry, private_secrets=None):
    """Correct unfinished bootstrap steps without replacing acknowledged secrets."""
    from provision import BOOTSTRAP_SECRET_STEPS
    credentials = read_credentials(host, entry)
    supplied = {} if private_secrets is None else private_secrets
    allowed = set(BOOTSTRAP_SECRET_STEPS)
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
        # Keep malformed values out of the durable intake, including on retries.
        supplied["backup_s3"] = normalize_backup_repository(repository)
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
        storage["disks"] = normalize_storage_disks(storage["disks"])
        storage_bindings(storage)
        supplied["app_storage"] = storage
    if "configuration" in supplied:
        configuration = supplied["configuration"]
        validate_configuration_changes(configuration, entry)
        require(all(isinstance(value, str) for value in configuration.values())
                and not any(key.removesuffix("_FILE") in BOOTSTRAP_OWNED_KEYS for key in configuration),
                "owned_configuration", "Encryption and storage are supplied through their own bootstrap inputs.")
    prior = _read_bootstrap_secrets(host, entry)
    if prior is not None:
        changes = {key: value for key, value in supplied.items() if prior.get(key) != value}
        for key in changes:
            step, route = BOOTSTRAP_SECRET_STEPS[key]
            require(step is not None and step not in entry["provisioning"]["completed_steps"],
                    "provision_conflict", "Bootstrap cannot replace " + key + "; use " + route + ".")
        if changes:
            update_bootstrap_secrets(host, [entry], changes)
            prior = {**prior, **changes}
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
    uploads.inspect(host, entry, runtime=True)
    credentials = read_credentials(host, entry)
    path = check_path(host.paths.instance_config(entry["name"]) / "app.env", "file")
    resolution = None
    if "deployment_selection" in entry:
        from admission import AdmissionStore
        _, resolution = AdmissionStore(host, entry["profile"]["instance_id"])._selection()
        path = effective_environment_path(host, entry, resolution)
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
    if resolution is not None:
        http = next(e for e in resolution.endpoints if e.role == "http" and e.listener == "http")
        require(http.port == entry["allocations"]["http_port"], "runtime_resolution_conflict",
                "HTTP registry projection differs from the selected runtime resolution.")
        expected["APP__HTTP__SERVER__ADDRESS"] = _http_address(http)
        expected.update(scheduler_environment(resolution.roles))
    owned_keys = pipeline_keys(entry)
    if any(values.get(key) != value for key, value in expected.items() if key in owned_keys):
        raise DeployError("configuration_identity", "Pipeline-owned runtime configuration differs from registered identity/credentials.")
    for key in values:
        logical = key.removesuffix("_FILE")
        if (logical in owned_keys and (logical not in expected or key != logical)
                or any(logical.startswith(owner + "__") for owner in owned_keys)):
            raise DeployError("configuration_identity", "Pipeline-owned runtime configuration contains an overriding alias or collection field.")
    return values


# Feature-service preparation is separate from the legacy app.env/upload owner.
MAX_SERVICE_VIEWS = 32
MAX_SERVICE_CONFIG_BYTES = 16 * MAX_VALUE_BYTES


def _service_directory(host, path):
    from admission import protected_directory
    return protected_directory(host, path)


def _service_source(host, root, member):
    """Read only an inventoried, protected regular file, with bounded allocation."""
    import hashlib
    from admission import protected_path
    path = protected_path(host, root / member.path, "file")
    require(member.size <= MAX_VALUE_BYTES and path.exists(),
            "service_config_size", "Service configuration source is missing or exceeds its bound.")
    with path.open("rb") as stream:
        info = os.fstat(stream.fileno())
        require(stat.S_ISREG(info.st_mode) and info.st_nlink == 1 and info.st_size == member.size,
                "service_config_source", "Configuration source differs from its regular-file inventory.")
        data = stream.read(MAX_VALUE_BYTES + 1)
    require(len(data) == member.size and hashlib.sha256(data).hexdigest() == member.sha256,
            "service_config_source", "Configuration source fingerprint differs from its inventory.")
    return data


def _render_service_json(data, bindings, resolve):
    from contracts import parse_json
    value = parse_json(data)
    for pointer, argument in bindings:
        tokens = [part.replace("~1", "/").replace("~0", "~") for part in pointer[1:].split("/")]
        target = value
        for index, token in enumerate(tokens):
            if isinstance(target, dict):
                require(token in target, "service_config_binding", "JSON binding target does not exist.")
                key = token
            else:
                require(isinstance(target, list) and re.fullmatch(r"0|[1-9][0-9]*", token) and
                        int(token) < len(target), "service_config_binding", "JSON binding target does not exist.")
                key = int(token)
            if index == len(tokens) - 1:
                resolved = resolve(argument)
                from topology import ArgumentKind
                target[key] = int(resolved) if argument.kind in (ArgumentKind.LISTENER_PORT, ArgumentKind.COPY_NUMBER) else resolved
            else:
                target = target[key]
    result = (json.dumps(value, sort_keys=True, separators=(",", ":"), allow_nan=False) + "\n").encode()
    require(len(result) <= MAX_VALUE_BYTES, "service_config_size", "Rendered service configuration exceeds its bound.")
    return result


def prepare_bundle_views(host, views):
    """Before loading units, create only protected mountpoint skeletons.

    Lane B verifies/retains the complete immutable artifact inventory. This step
    verifies the exported members again at the mount seam.
    No mount command or global plugin installation is performed here.
    """
    from admission import protected_path
    from contracts import PayloadMember, digest
    from dataclasses import asdict
    for view in views:
        source = protected_path(host, host.paths.p(view.source), "directory")
        require(source.exists(), "service_bundle", "Immutable bundle source is unavailable.")
        expected = {m.path: PayloadMember.parse(asdict(m)) for m in view.members}
        require(expected and len(expected) == len(view.members) and len(expected) <= 10000,
                "service_bundle", "Invalid bundle member inventory.")
        files = set()
        for count, path in enumerate(source.rglob("*"), 1):
            require(count <= 20000, "service_bundle", "Bundle view exceeds its member bound.")
            protected_path(host, path, "directory" if path.is_dir() else "file")
            if path.is_dir():
                continue
            name = path.relative_to(source).as_posix()
            member = expected.get(name)
            require(member is not None and path.stat().st_nlink == 1 and path.stat().st_size == member.size
                    and digest(path) == member.sha256,
                    "service_bundle", "Bundle member differs from its immutable inventory.")
            files.add(name)
        require(files == set(expected), "service_bundle", "Bundle export has missing members.")
        _service_directory(host, host.paths.p(view.destination))


def prepare_unit_paths(host, role, spec, resolution, *, views=()):
    """Inert staging before unit load (and boot): all namespace paths already exist."""
    from services import ServicePaths, validate_views
    require(role.common.name == spec.identity.role and role.common.name in resolution.roles,
            "service_preparation", "Unit paths must belong to a selected role.")
    paths = ServicePaths.for_unit(spec.identity, resolution.id)
    validate_views(role, paths, views)
    for path in (paths.base, paths.base + "/invocations", paths.base + "/view/config",
                 paths.base + "/view/data", paths.state):
        _service_directory(host, host.paths.p(path))
    prepare_bundle_views(host, views)
    return paths


def prepare_service(host, store, invocation, role, spec, release_root, members, *,
                    views=(), secret_paths=None, uid, gid, permit_id=None):
    """Publish one complete invocation view after admission, never reuse failed output.

    Call with the invocation-pinned UnitSpec and the selected release inventory.
    Seed leaves are service-owned; every ancestor and mountpoint is protected.
    Failed/retained views count toward a hard bound until proven-exit cleanup.
    """
    from contracts import (PayloadMember, UnitSpec, atomic_json, atomic_write,
                           canonical_digest, record_document)
    from admission import protected_directory, read_owned
    from services import ServicePaths, configuration_digest, resolve_argument, validate_views
    from topology import ExecutableRole
    spec = UnitSpec.parse(record_document(spec))
    require(isinstance(role, ExecutableRole) and role.common.name == spec.identity.role
            and invocation.unit == spec.identity
            and invocation.unit_spec_sha256 == canonical_digest(record_document(spec)),
            "service_preparation", "Preparation must use the admitted frozen unit specification.")
    require(type(uid) is int and type(gid) is int and uid >= 0 and gid >= 0 and
            (host.paths.root != Path("/") or uid > 0 and gid > 0),
            "service_account", "Service files require the unprivileged instance account.")
    paths = ServicePaths.for_unit(spec.identity, invocation.resolution_id)
    validate_views(role, paths, views)
    require(spec.configuration_sha256 == configuration_digest(role, members, views, secret_paths),
            "service_preparation", "Configuration sources differ from the admitted unit specification.")
    inventory = {member.path: PayloadMember.parse({"path": member.path, "sha256": member.sha256,
                 "size": member.size, "kind": member.kind}) for member in members}
    require(len(inventory) == len(members), "service_preparation", "Duplicate configuration inventory members.")
    with store.preparation(invocation, permit_id=permit_id) as resolution:
        from dataclasses import replace
        prepare_unit_paths(host, role, spec, replace(resolution, id=invocation.resolution_id), views=views)
        base = _service_directory(host, host.paths.p(paths.base))
        invocations = _service_directory(host, base / "invocations")
        view = invocations / invocation.process.invocation_id
        complete = view / "prepared.json"
        current = base / "current"
        if complete.exists():
            require(read_owned(host, complete, lambda v: v) == record_document(invocation) and
                    current.is_symlink() and os.readlink(current) == "invocations/" + view.name,
                    "service_preparation", "Prepared invocation is no longer selected.")
            return view
        require(not view.exists() and len(list(invocations.iterdir())) < MAX_SERVICE_VIEWS,
                "service_preparation_retained", "Failed/retained views need proven-exit cleanup before another start.")
        protected_directory(host, view)
        configs = _service_directory(host, view / "config")
        baselines = _service_directory(host, view / "baselines")
        runtime = _service_directory(host, view / "runtime")
        os.chown(runtime, uid, gid)
        os.chmod(runtime, 0o700)
        state = _service_directory(host, host.paths.p(paths.state))
        seeds = _service_directory(host, state / "seeds")
        data = check_path(state / "data", "directory")
        if not data.exists():
            protected_directory(host, data, 0o700)
            os.chown(data, uid, gid)
        # Never walk/chown existing app-writable state or follow a seed link.
        require(data.stat().st_uid == uid, "service_account", "Persistent data has an unexpected owner.")
        resolve = lambda argument: resolve_argument(argument, role, spec, resolution, paths, views, secret_paths or {})
        require(tuple(resolve(a) for a in role.arguments) == spec.arguments,
                "service_arguments", "Prepared bindings differ from frozen argv.")
        total = 0
        for name, config in role.configs:
            member = inventory.get(config.source)
            require(member is not None and member.kind == "config", "service_config_source", "Configuration is not inventoried.")
            content = _service_source(host, release_root, member)
            if config.mode == "rendered-json" or config.bindings:
                content = _render_service_json(content, config.bindings, resolve)
            total += len(content)
            require(total <= MAX_SERVICE_CONFIG_BYTES, "service_config_size", "Service configuration view exceeds its bound.")
            atomic_write(baselines / name, content, mode=0o444)
            if config.mode == "seed-once":
                directory = check_path(seeds / name, "directory")
                if directory.exists():
                    require(directory.stat().st_uid == uid and not directory.stat().st_mode & 0o077,
                            "service_config_seed", "Existing seed directory has foreign ownership.")
                    fd = os.open(directory, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
                    try:
                        info = os.stat("value", dir_fd=fd, follow_symlinks=False)
                    finally:
                        os.close(fd)
                    require(stat.S_ISREG(info.st_mode) and info.st_uid == uid and info.st_nlink == 1
                            and not info.st_mode & 0o077,
                            "service_config_seed", "Existing seed is not an owned private regular file.")
                else:
                    protected_directory(host, directory, 0o700)
                    atomic_write(directory / "value", content, mode=0o600, owner=uid, group=gid)
                    os.chown(directory, uid, gid)
            else:
                directory = _service_directory(host, configs / name)
                if config.mode == "recreate":
                    atomic_write(directory / "value", content, mode=0o600, owner=uid, group=gid)
                    os.chmod(directory, 0o700)
                    os.chown(directory, uid, gid)
                else:
                    atomic_write(directory / "value", content, mode=0o444)
        # Only a framework-created relative link may be replaced, under this lock.
        if current.exists() or current.is_symlink():
            info = current.lstat()
            require(stat.S_ISLNK(info.st_mode) and info.st_uid == os.geteuid() and
                    re.fullmatch(r"invocations/[a-f0-9]{32}", os.readlink(current)),
                    "service_preparation", "Prepared-view selection has foreign ownership or ancestry.")
        atomic_json(complete, record_document(invocation), mode=0o600)
        pending = base / (".current-" + view.name)
        require(not pending.exists() and not pending.is_symlink(), "service_preparation", "Unresolved view publication remains.")
        pending.symlink_to("invocations/" + view.name)
        os.replace(pending, current)
        directory = os.open(base, os.O_RDONLY)
        try:
            os.fsync(directory)
        finally:
            os.close(directory)
        return view


def _http_address(endpoint):
    address = "[" + endpoint.address + "]" if ":" in endpoint.address else endpoint.address
    return f"{address}:{endpoint.port}"


def effective_environments(host, entry, resolution_id, roles, endpoints, *, values=None):
    """Snapshot private settings with the selected HTTP allocation and role placement."""
    from admission import protected_directory
    from contracts import EffectiveEnvironment, atomic_write, contract_uuid, digest, parse_env
    contract_uuid(resolution_id)
    source = host.paths.instance_config(entry["name"]) / "app.env"
    values = dict(values) if values is not None else parse_env(check_path(source, "file").read_text())
    http = next((e for e in endpoints if e.role == "http" and e.listener == "http"), None)
    require(http is not None, "runtime_resolution", "The HTTP runtime allocation is missing.")
    values["APP__HTTP__SERVER__ADDRESS"] = _http_address(http)
    values.pop(SCHEDULER_ENABLED + "_FILE", None)
    values.update(scheduler_environment(roles))
    payload = dump_env(values)
    result = []
    for role in roles:
        relative = f"runtime/{resolution_id}/{role}/app.env"
        target = host.paths.instance_config(entry["name"]) / relative
        protected_directory(host, target.parent)
        if target.exists():
            require(target.read_text() == payload, "runtime_resolution_conflict", "An immutable effective environment already differs.")
        host.owned_write(target, payload, mode=0o640, group=entry["name"])
        result.append(EffectiveEnvironment(role, relative, digest(target)))
    return tuple(result)


def effective_environment_path(host, entry, resolution, role="http", *, candidate=False):
    from contracts import digest, require_current_private_configuration
    selected = next((e for e in resolution.environments if e.role == role), None)
    if candidate:
        require(resolution.private_revision in (entry["configuration_revision"], entry["configuration_revision"] + 1),
                "runtime_resolution_conflict", "Candidate configuration revision is stale.")
    elif resolution.private_revision != entry["configuration_revision"]:
        from admission import AdmissionStore
        _, current = AdmissionStore(host, entry["profile"]["instance_id"])._selection()
        require_current_private_configuration(current, entry["configuration_revision"])
        require(selected is not None and any(e.role == role and e.sha256 == selected.sha256 for e in current.environments), "runtime_resolution_conflict",
                "A retained role no longer has identical private configuration.")
    else:
        require_current_private_configuration(resolution, entry["configuration_revision"])
    require(selected is not None, "runtime_resolution", "Role has no effective environment.")
    path = check_path(host.paths.instance_config(entry["name"]) / selected.path, "file")
    require(path.exists() and digest(path) == selected.sha256,
            "runtime_resolution_conflict", "Selected effective environment has drifted.")
    return path


def write_binding_descriptor(host, entry, deployment, resolution, spec, *, candidate=False):
    """Final app-facing descriptor: schema 2 embeds the frozen RoleBindings record."""
    from dataclasses import asdict
    from contracts import atomic_json, canonical_digest, record_document
    from admission import protected_directory
    require(spec in deployment.units and resolution.deployment_sha256 == canonical_digest(record_document(deployment)),
            "runtime_resolution", "Bindings belong to another selected deployment.")
    env = effective_environment_path(host, entry, resolution, spec.identity.role, candidate=candidate)
    absolute = lambda path: "/" + path.relative_to(host.paths.root).as_posix()
    path = host.paths.opt(entry["name"]) / "runtime" / resolution.id / (spec.identity.role + ".json")
    protected_directory(host, path.parent)
    descriptor = {"schema": 2, "instance": entry["name"], "user": entry["name"],
                  "env_file": absolute(env), "work_dir": entry["allocations"]["paths"]["data"],
                  "release": entry["allocations"]["paths"]["application"] + "/releases/" + deployment.release,
                  "deployment_sha256": resolution.deployment_sha256, "unit": asdict(spec.identity),
                  "bindings": record_document(spec.bindings)}
    if deployment.archive_schema == 1:
        descriptor = {key: descriptor[key] for key in ("instance", "user", "env_file", "work_dir")}
        descriptor["schema"] = 1
    if path.exists():
        require(read_json(path) == descriptor, "runtime_resolution_conflict", "An immutable role descriptor already differs.")
    atomic_json(path, descriptor, mode=0o644)
    return absolute(path)
