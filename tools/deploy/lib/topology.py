"""Legacy supervision and immutable host-capability declaration parsing.

parse_topology/parse_profile own feature schemas; shared readers dispatch to
those same parsers without changing the legacy schema projections.
"""
from __future__ import annotations

from dataclasses import dataclass
from enum import Enum
import ipaddress
import re
from urllib.parse import urlsplit
from typing import Union
import hashlib
import json
from pathlib import Path

from contracts import SERVICE_ROLES, fields, read_json, require, unit_name


def validate_topology(value: dict, *, registered: bool = False) -> dict:
    """Validate roles; a registry projection also carries its canonical SHA-256."""
    if isinstance(value, dict) and value.get("schema") == TOPOLOGY_SCHEMA:
        parsed = parse_topology(value, registered=registered)
        return {**parsed.projection(), "sha256": parsed.sha256}
    fields(value, "schema roles sha256" if registered else "schema roles", "topology")
    require(type(value["schema"]) is int and value["schema"] == 1
            and isinstance(value["roles"], list), "invalid_topology", "Topology requires schema 1 and roles.")
    names = []
    for role in value["roles"]:
        fields(role, "name", "topology role")
        require(role["name"] in SERVICE_ROLES and role["name"] not in names,
                "invalid_topology", "Declare each supported role at most once.")
        names.append(role["name"])
    require("http" in names, "invalid_topology", "The HTTP role is required.")
    projection = {"schema": 1, "roles": [{"name": name} for name in names]}
    checksum = hashlib.sha256(json.dumps(projection, sort_keys=True, separators=(",", ":")).encode()).hexdigest()
    if registered:
        require(value["sha256"] == checksum, "invalid_topology", "Topology checksum mismatch.")
    return {**projection, "sha256": checksum}


def load_topology(app_root: Path) -> dict:
    """Load the explicit application's deploy/topology.json; never infer roles."""
    return validate_topology(read_json(Path(app_root) / "deploy/topology.json"))


def instance_units(entry: dict) -> dict[str, str]:
    """Map every declared role to its canonical concrete systemd unit."""
    topology = validate_topology(entry["topology"], registered=True)
    if topology["schema"] == TOPOLOGY_SCHEMA:
        parsed = parse_topology(topology, registered=True)
        profile = parse_profile(entry["profile"], parsed)
        placed = dict(profile.placement)
        return {role.common.name: role_unit_name(entry["name"], role) for role in parsed.roles if placed[role.common.name]}
    return {role["name"]: unit_name(entry["name"], role["name"]) for role in topology["roles"]}


def role_unit_name(name, role):
    return unit_name(name, role.common.name) if isinstance(role, BuiltinRole) else f"foundry-app-{name}-{role.common.name}@1.service"


def selected_units(entry: dict, role="all", *, roles=None) -> dict[str, str]:
    units = instance_units(entry)
    if roles is not None:
        require(isinstance(roles, list) and all(isinstance(item, str) and item in units for item in roles)
                and len(roles) == len(set(roles)), "invalid_role", "Select declared topology roles.")
        return {name: unit for name, unit in units.items() if name in roles}
    require(isinstance(role, str) and (role == "all" or role in units),
            "invalid_role", "Select a declared topology role.")
    return units if role == "all" else {role: units[role]}


# The frozen parsers are also the validation owners for shared lifecycle readers.
from contracts import (ARCHIVE_SCHEMA, FEATURE_PROFILE_SCHEMA, MAX_ARTIFACTS, MAX_LISTENERS, MAX_ROLES,
                       TOPOLOGY_SCHEMA, PROFILE_SCHEMA, canonical_digest, contract_digest, contract_document,
                       contract_int, contract_list, contract_map, contract_name, contract_names,
                       contract_object, contract_path, contract_text, dns_hostname, public_address,
                       validate_profile)


@dataclass(frozen=True)
class PackageExecutable:
    package: str
    path: str


@dataclass(frozen=True)
class ArtifactExecutable:
    artifact: str
    entry: str


@dataclass(frozen=True)
class ApplicationExecutable:
    application: str


Executable = Union[PackageExecutable, ArtifactExecutable, ApplicationExecutable]


def package_name(value):
    require(isinstance(value, str) and re.fullmatch(r"[a-z0-9][a-z0-9+.-]{0,127}", value),
            "invalid_topology", "Use an archive package name without versions, options or repositories.")
    return value


def parse_executable(value) -> Executable:
    require(isinstance(value, dict), "invalid_topology", "Invalid executable reference.")
    if "package" in value:
        contract_object(value, ("package", "path"))
        return PackageExecutable(package_name(value["package"]), contract_path(value["path"], absolute=True))
    if "artifact" in value:
        contract_object(value, ("artifact", "entry"))
        return ArtifactExecutable(contract_name(value["artifact"]), contract_name(value["entry"]))
    contract_object(value, ("application",))
    return ApplicationExecutable(contract_name(value["application"]))


class ArgumentKind(str, Enum):
    LITERAL = "literal"
    CONFIG = "config"
    LISTENER_PORT = "listener_port"
    LISTENER_ADDRESS = "listener_address"
    BUNDLE = "bundle"
    COPY_NUMBER = "copy_number"
    STATE_PATH = "state_path"
    RUNTIME_PATH = "runtime_path"
    SECRET_FILE = "secret_file"
    BINDING = "binding"
    CONFIG_ROOT = "config_root"
    DATA_ROOT = "data_root"


def _secret_file_name(value):
    """Secret argv references name the same environment key as secret_files."""
    require(isinstance(value, str) and re.fullmatch(r"[A-Z][A-Z0-9_]{0,119}_FILE", value),
            "invalid_topology", "Secret environment references require _FILE names.")
    require(not value.startswith(("FOUNDRY_", "DEPLOY_", "APP__", "LD_", "PYTHON")),
            "invalid_topology", "Secret file variables cannot override framework or loader settings.")
    return value


@dataclass(frozen=True)
class Argument:
    kind: ArgumentKind
    value: str

    @classmethod
    def parse(cls, value):
        require(isinstance(value, dict) and len(value) == 1, "invalid_topology", "Arguments require exactly one tag.")
        tag, item = next(iter(value.items()))
        require(tag in tuple(k.value for k in ArgumentKind), "invalid_topology", "Unknown argument reference.")
        if tag == "literal":
            # Empty argv elements are meaningful; controls are not.
            require(isinstance(item, str) and len(item.encode("utf-8", errors="surrogatepass")) <= 4096
                    and not any(0xD800 <= ord(c) <= 0xDFFF for c in item)
                    and all(ord(c) >= 32 and ord(c) != 127 for c in item), "invalid_topology", "Invalid literal argument.")
        elif tag in ("copy_number", "config_root", "data_root"):
            require(item == "self", "invalid_topology", "Copy ordinal reference must select self.")
        elif tag in ("state_path", "runtime_path"):
            contract_path(item)
        elif tag == "secret_file":
            _secret_file_name(item)
        else:
            contract_name(item)
        return cls(ArgumentKind(tag), item)


@dataclass(frozen=True)
class EndpointBinding:
    role: str
    listener: str
    scheme: str


@dataclass(frozen=True)
class PublicListenerReference:
    role: str
    listener: str


@dataclass(frozen=True)
class ExecutableBinding:
    executable: Executable


@dataclass(frozen=True)
class BundleDirectoryBinding:
    mount: str


Binding = Union[EndpointBinding, ExecutableBinding, BundleDirectoryBinding, PublicListenerReference]


def parse_binding(value) -> Binding:
    require(isinstance(value, dict) and len(value) == 1, "invalid_topology", "Bindings require exactly one tag.")
    if "bundle" in value:
        return BundleDirectoryBinding(contract_name(value["bundle"]))
    if "executable" in value:
        return ExecutableBinding(parse_executable(value["executable"]))
    if "public_listener" in value:
        listener = contract_object(value["public_listener"], ("role", "listener"))
        return PublicListenerReference(contract_name(listener["role"]), contract_name(listener["listener"]))
    contract_object(value, ("endpoint",))
    endpoint = contract_object(value["endpoint"], ("role", "listener", "scheme"))
    require(endpoint["scheme"] in ("tcp", "http"), "unsupported_capability", "Unsupported private endpoint scheme.")
    return EndpointBinding(contract_name(endpoint["role"]), contract_name(endpoint["listener"]), endpoint["scheme"])


@dataclass(frozen=True)
class ConfigFile:
    source: str
    mode: str
    bindings: tuple[tuple[str, Argument], ...]

    @classmethod
    def parse(cls, value):
        contract_object(value, ("source", "mode"), ("bindings",))
        source = contract_path(value["source"])
        require(source.startswith("deploy/files/"), "invalid_topology", "Config sources belong under deploy/files/.")
        require(value["mode"] in ("immutable", "seed-once", "recreate", "rendered-json"),
                "invalid_topology", "Unknown configuration mode.")
        bindings = value.get("bindings", {})
        require(isinstance(bindings, dict) and len(bindings) <= 128
                and (not bindings or value["mode"] in ("rendered-json", "recreate")),
                "invalid_topology", "Only rendered-json/recreate config accepts field bindings.")
        result = []
        for pointer, raw in sorted(bindings.items()):
            contract_text(pointer)
            require(pointer.startswith("/") and not re.search(r"~(?![01])", pointer),
                    "invalid_topology", "Invalid JSON pointer.")
            argument = Argument.parse(raw)
            require(argument.kind in (ArgumentKind.LISTENER_ADDRESS, ArgumentKind.LISTENER_PORT,
                                      ArgumentKind.BUNDLE, ArgumentKind.BINDING, ArgumentKind.COPY_NUMBER,
                                      ArgumentKind.SECRET_FILE),
                    "invalid_topology", "Config fields require typed nonsecret bindings.")
            result.append((pointer, argument))
        pointers = [p for p, _ in result]
        require(not any(b.startswith(a + "/") for a in pointers for b in pointers if a != b),
                "invalid_topology", "Configuration pointers overlap.")
        return cls(source, value["mode"], tuple(result))


@dataclass(frozen=True)
class Listener:
    transport: str
    bind: str
    tls: str | None
    authentication: bool

    @classmethod
    def parse(cls, value):
        require(not isinstance(value, dict) or "proxy_protocol" not in value,
                "unsupported_capability", "PROXY protocol requires a later capability.")
        contract_object(value, ("transport", "bind"), ("tls", "authentication"))
        require(value["transport"] == "tcp", "unsupported_capability", "UDP listeners require a later capability.")
        require(value["bind"] in ("loopback", "public"), "invalid_topology", "Invalid listener intent.")
        if value["bind"] == "public":
            require(value.get("tls") in ("edge", "none") and value.get("authentication") is True,
                    "unsupported_capability", "Public TCP requires tls edge or none and explicit application authentication acknowledgement.")
        else:
            require("tls" not in value and "authentication" not in value,
                    "invalid_topology", "Private listeners do not declare public edge policy.")
        return cls("tcp", value["bind"], value.get("tls"), value.get("authentication", False))


@dataclass(frozen=True)
class Probe:
    kind: str
    listener: str
    timeout_seconds: int
    path: str | None
    status: int | None
    body: str | None

    @classmethod
    def parse(cls, value):
        contract_object(value, ("kind", "listener"), ("timeout_seconds", "path", "status", "body"))
        require(value["kind"] in ("tcp", "http"), "invalid_topology", "Only local TCP/HTTP readiness is supported.")
        if value["kind"] == "http":
            path = contract_text(value.get("path"))
            require(path.startswith("/") and not path.startswith("//") and not any(c in path for c in "?#\\"),
                    "invalid_topology", "Readiness uses a local HTTP path without query or fragment.")
            status = contract_int(value.get("status"), 200, 299)
            body = contract_text(value["body"], limit=1024) if "body" in value else None
        else:
            require(not set(value).intersection(("path", "status", "body")), "invalid_topology", "TCP probes have no HTTP fields.")
            path, status, body = None, None, None
        return cls(value["kind"], contract_name(value["listener"]),
                   contract_int(value.get("timeout_seconds", 30), 1, 300), path, status, body)


@dataclass(frozen=True)
class RestartPolicy:
    policy: str
    delay_seconds: int
    burst: int
    window_seconds: int
    stop_seconds: int

    @classmethod
    def parse(cls, value):
        contract_object(value, ("policy", "delay_seconds", "burst"), ("window_seconds", "stop_seconds"))
        require(value["policy"] in ("on-failure", "always", "no"), "invalid_topology", "Invalid restart policy.")
        return cls(value["policy"], contract_int(value["delay_seconds"], 1, 300), contract_int(value["burst"], 1, 20),
                   contract_int(value.get("window_seconds", 60), 1, 3600), contract_int(value.get("stop_seconds", 30), 1, 300))


@dataclass(frozen=True)
class ResourceLimits:
    memory_bytes: int
    cpu_percent: int
    tasks: int
    file_descriptors: int

    @classmethod
    def parse(cls, value):
        contract_object(value, ("memory_bytes", "cpu_percent", "tasks"), ("file_descriptors",))
        return cls(contract_int(value["memory_bytes"], 1024**2, 2**50),
                   contract_int(value["cpu_percent"], 1, 6400), contract_int(value["tasks"], 1, 65536),
                   contract_int(value.get("file_descriptors", 1024), 16, 1048576))


@dataclass(frozen=True)
class StatePolicy:
    kind: str
    acknowledged: bool
    recovery_owner: str | None = None

    @classmethod
    def parse(cls, value):
        contract_object(value, ("kind",), ("acknowledged", "recovery_owner"))
        require(value["kind"] in ("disposable", "external", "local-unprotected"), "invalid_topology", "Invalid service state policy.")
        require((value["kind"] == "local-unprotected" and value.get("acknowledged") is True)
                or (value["kind"] != "local-unprotected" and "acknowledged" not in value),
                "invalid_topology", "Unprotected local state requires an explicit acknowledgement.")
        owner = None
        if "recovery_owner" in value:
            require(value["kind"] == "external", "invalid_topology", "Only external state names a recovery owner.")
            owner = contract_text(value["recovery_owner"], limit=256)
            require(owner == owner.strip() and bool(owner.strip()),
                    "invalid_topology", "External recovery owners must be nonblank public labels without surrounding whitespace.")
        return cls(value["kind"], value.get("acknowledged", False), owner)


@dataclass(frozen=True)
class ReleaseArtifactSource:
    path: str
    build: tuple[str, ...]

    @classmethod
    def parse(cls, value):
        contract_object(value, ("release",))
        source = value["release"]
        contract_object(source, ("path",), ("build",))
        path = contract_path(source["path"])
        require(path.startswith("deploy/artifacts/"), "invalid_topology",
                "Release artifact sources belong under deploy/artifacts/.")
        build = contract_list(source["build"], contract_text, minimum=1, maximum=64) if "build" in source else ()
        return cls(path, build)


@dataclass(frozen=True)
class ReleaseArtifactPlatform:
    source: ReleaseArtifactSource

    def accepts_digest(self, checksum):
        # The release inventory, rather than a remote download pin, owns this digest.
        return contract_digest(checksum) == checksum


@dataclass(frozen=True)
class ArtifactPlatform:
    url: str
    sha256: str

    def accepts_digest(self, checksum):
        return self.sha256 == checksum

    @classmethod
    def parse(cls, value):
        if isinstance(value, dict) and "source" in value:
            contract_object(value, ("source",))
            return ReleaseArtifactPlatform(ReleaseArtifactSource.parse(value["source"]))
        contract_object(value, ("url", "sha256"))
        url = contract_text(value["url"], limit=2048)
        try:
            parsed = urlsplit(url)
            valid = (parsed.scheme == "https" and parsed.hostname and not parsed.username
                     and not parsed.password and not parsed.query and not parsed.fragment
                     and parsed.port in (None, 443) and not any(c in url for c in "\\ %"))
            if valid:
                try:
                    address = ipaddress.ip_address(parsed.hostname)
                except ValueError:
                    dns_hostname(parsed.hostname)
                else:
                    valid = address.is_global and not address.is_multicast and not getattr(address, "ipv4_mapped", None)
        except ValueError:
            valid = False
        require(valid, "invalid_topology", "Artifacts require a public HTTPS origin without credentials, query or fragment.")
        return cls(url, contract_digest(value["sha256"]))


@dataclass(frozen=True)
class ArtifactProvenance:
    source_revision: str
    build_inputs: str
    license: str
    service_abi: str

    @classmethod
    def parse(cls, value):
        contract_object(value, ("source_revision", "build_inputs", "license", "service_abi"))
        return cls(*(contract_text(value[k]) for k in ("source_revision", "build_inputs", "license", "service_abi")))


@dataclass(frozen=True)
class Artifact:
    version: str
    format: str
    platforms: tuple[tuple[str, ArtifactPlatform | ReleaseArtifactPlatform], ...]
    executables: tuple[tuple[str, str], ...]
    bundles: tuple[tuple[str, str], ...]
    modules: tuple[str, ...]
    provenance: ArtifactProvenance | None

    @classmethod
    def parse(cls, value):
        contract_object(value, ("version", "format", "platforms"), ("executables", "bundles", "modules", "provenance"))
        version = contract_path(value["version"])
        require("/" not in version and len(version) <= 128, "invalid_topology", "Invalid artifact version label.")
        require(value["format"] in ("file", "tar.gz"), "unsupported_capability", "Unsupported artifact format.")
        platforms = value["platforms"]
        require(isinstance(platforms, dict) and 1 <= len(platforms) <= 2
                and platforms.keys() <= {"linux-amd64", "linux-arm64"}, "invalid_topology", "Unsupported artifact platforms.")
        executables = contract_map(value.get("executables", {}), contract_path, maximum=64)
        bundles = contract_map(value.get("bundles", {}), contract_path, maximum=64)
        modules = contract_list(value.get("modules", []), contract_path, maximum=256)
        require(executables or bundles, "invalid_topology", "Artifacts must export an executable or bundle.")
        require(len(set(modules)) == len(modules) and all(any(m.startswith(p + "/") for _, p in bundles) for m in modules),
                "invalid_topology", "Modules must be unique declared bundle members.")
        require(value["format"] != "file" or (len(executables) == 1 and not bundles and not modules),
                "invalid_topology", "File artifacts export one executable only.")
        provenance = ArtifactProvenance.parse(value["provenance"]) if "provenance" in value else None
        require(not bundles or provenance is not None, "invalid_topology", "Bundle exports require source/build/license/ABI provenance.")
        return cls(version, value["format"], tuple((k, ArtifactPlatform.parse(v)) for k, v in sorted(platforms.items())),
                   executables, bundles, modules, provenance)


@dataclass(frozen=True)
class ApplicationBinary:
    go_package: str | None
    platforms: tuple[tuple[str, str], ...]

    @classmethod
    def parse(cls, value):
        require(isinstance(value, dict) and len(value) == 1, "invalid_topology", "Application executable requires one build source.")
        if "go_package" in value:
            package = contract_text(value["go_package"])
            require(package.startswith("./cmd/"), "invalid_topology", "Declare a local cmd package.")
            contract_path(package[2:])
            return cls(package, ())
        contract_object(value, ("platforms",))
        platforms = value["platforms"]
        require(isinstance(platforms, dict) and 1 <= len(platforms) <= 2
                and platforms.keys() <= {"linux-amd64", "linux-arm64"}, "invalid_topology", "Invalid application binary platforms.")
        parsed = tuple((k, contract_path(v)) for k, v in sorted(platforms.items()))
        require(all(p.startswith("deploy/bin/") for _, p in parsed), "invalid_topology", "Application binaries belong under deploy/bin/.")
        return cls(None, parsed)


@dataclass(frozen=True)
class ArtifactMount:
    artifact: str
    bundle: str
    area: str
    path: str

    @classmethod
    def parse(cls, value):
        contract_object(value, ("artifact", "bundle", "area", "path"))
        require(value["area"] in ("config", "data"), "invalid_topology", "Bundles must use a service-local config/data area.")
        return cls(contract_name(value["artifact"]), contract_name(value["bundle"]), value["area"], contract_path(value["path"]))


@dataclass(frozen=True)
class RoleCommon:
    name: str
    packages: tuple[str, ...]
    requires: tuple[str, ...]
    bindings: tuple[tuple[str, Binding], ...]
    maintenance: str
    artifact_mounts: tuple[tuple[str, ArtifactMount], ...]


@dataclass(frozen=True)
class BuiltinRole:
    common: RoleCommon


@dataclass(frozen=True)
class ExecutableRole:
    common: RoleCommon
    executable: Executable
    arguments: tuple[Argument, ...]
    configs: tuple[tuple[str, ConfigFile], ...]
    secret_files: tuple[tuple[str, str], ...]
    listeners: tuple[tuple[str, Listener], ...]
    readiness: Probe
    restart: RestartPolicy
    limits: ResourceLimits
    state: StatePolicy
    environment: tuple[tuple[str, Argument], ...]
    recovery_rehearsal_safe: bool = False


Role = Union[BuiltinRole, ExecutableRole]


def _role(value):
    require(isinstance(value, dict), "invalid_topology", "Role must be an object.")
    require(not set(value).intersection(("wrapper", "instances", "proxy_protocol")),
            "unsupported_capability", "Wrappers and numbered services require a later capability.")
    common = ("packages", "requires", "bindings", "maintenance", "artifact_mounts")
    custom = ("executable", "arguments", "configs", "secret_files", "listeners", "readiness", "restart", "limits", "state", "environment",
              "recovery_rehearsal_safe")
    contract_object(value, ("name", "kind"), common + custom)
    name = contract_name(value["name"])
    require(value["kind"] in ("builtin", "executable"), "invalid_topology", "Unknown role kind.")
    builtin = value["kind"] == "builtin"
    require((name in SERVICE_ROLES) == builtin, "invalid_topology", "Built-in names are reserved.")
    packages = contract_list(value.get("packages", []), package_name, maximum=128)
    require(len(packages) == len(set(packages)), "invalid_topology", "Duplicate package requirements.")
    requires = contract_names(value.get("requires", ["http"] if name in ("scheduler", "worker") else []))
    maintenance = value.get("maintenance", "serve" if name == "http" else "stop")
    require(maintenance in ("continue", "serve", "stop"), "invalid_topology", "Invalid maintenance policy.")
    shared = RoleCommon(name, packages, requires, contract_map(value.get("bindings", {}), parse_binding, maximum=128), maintenance,
                        contract_map(value.get("artifact_mounts", {}), ArtifactMount.parse, maximum=64))
    if builtin:
        require(not set(value).intersection(custom), "invalid_topology", "Built-in runtime commands cannot be overridden.")
        return BuiltinRole(shared)
    contract_object(value, ("name", "kind", "executable", "readiness", "restart", "limits", "state"), common + custom)
    rehearsal_safe = value.get("recovery_rehearsal_safe", False)
    require(type(rehearsal_safe) is bool, "invalid_topology", "Recovery rehearsal safety requires an explicit boolean.")
    configs = contract_map(value.get("configs", {}), ConfigFile.parse, maximum=64)
    listeners = contract_map(value.get("listeners", {}), Listener.parse, maximum=MAX_LISTENERS)
    args = contract_list(value.get("arguments", []), Argument.parse, maximum=128)
    require(sum(len(a.value.encode()) for a in args) <= 32768, "invalid_topology", "Argument vector is too large.")
    secrets = value.get("secret_files", {})
    require(isinstance(secrets, dict) and len(secrets) <= 64,
            "invalid_topology", "Secret environment references require _FILE names.")
    environment = value.get("environment", {})
    require(isinstance(environment, dict) and len(environment) <= 64
            and all(isinstance(k, str) and re.fullmatch(r"[A-Z][A-Z0-9_]{0,127}", k)
                    and not k.endswith("_FILE") and not k.startswith(("APP__", "LD_", "PYTHON"))
                    and k not in ("PATH", "HOME", "USER", "LOGNAME", "SHELL") for k in environment),
            "invalid_topology", "Environment must be bounded explicit nonsecret settings; use secret_files for secrets.")
    env = tuple((k, Argument.parse(v)) for k, v in sorted(environment.items()))
    require(not any(key.removesuffix("_FILE") in environment for key in secrets),
            "invalid_topology", "A service secret file cannot also have a value environment variable.")
    require(all(a.kind is not ArgumentKind.SECRET_FILE for _, a in env),
            "invalid_topology", "Secret paths require the secret_files owner.")
    return ExecutableRole(shared, parse_executable(value["executable"]), args, configs,
                          tuple((_secret_file_name(k), contract_name(v)) for k, v in sorted(secrets.items())), listeners,
                          Probe.parse(value["readiness"]), RestartPolicy.parse(value["restart"]),
                          ResourceLimits.parse(value["limits"]), StatePolicy.parse(value["state"]),
                          env, rehearsal_safe)


@dataclass(frozen=True)
class Topology:
    schema: int
    roles: tuple[Role, ...]
    artifacts: tuple[tuple[str, Artifact], ...]
    application_executables: tuple[tuple[str, ApplicationBinary], ...]
    secrets: tuple[tuple[str, str], ...]
    sha256: str
    canonical_json: str

    def projection(self):
        """Owned wire snapshot; the immutable parsed declaration remains authoritative."""
        return json.loads(self.canonical_json)


SERVICE_SECRET_PREFIX = "FOUNDRY_SERVICE_SECRET__"


def service_secret_keys(names):
    """Map declaration names to the reserved private configuration namespace."""
    result, aliases = {}, set()
    for name in sorted(names):
        contract_name(name)
        key = SERVICE_SECRET_PREFIX + name.upper().replace("-", "_")
        require(key not in aliases and key + "_FILE" not in aliases,
                "invalid_topology", "Service secret names collide in the configuration namespace.")
        result[key] = name
        aliases.update((key, key + "_FILE"))
    return result


def parse_topology(document, *, registered=False) -> Topology:
    value = contract_document(document)
    require(isinstance(value, dict) and type(value.get("schema")) is int,
            "invalid_topology", "Topology requires an integer schema.")
    if value["schema"] == 1:
        legacy = validate_topology(value, registered=registered)
        projection = {k: v for k, v in legacy.items() if k != "sha256"}
        roles = tuple(_role({**r, "kind": "builtin"}) for r in projection["roles"])
        return Topology(1, roles, (), (), (), legacy["sha256"], json.dumps(projection, sort_keys=True, separators=(",", ":")))
    require(value["schema"] == TOPOLOGY_SCHEMA, "unsupported_schema", "Unsupported topology schema.")
    contract_object(value, ("schema", "roles", *(["sha256"] if registered else [])),
                    ("artifacts", "application_executables", "secrets"))
    roles = contract_list(value["roles"], _role, maximum=MAX_ROLES, minimum=1)
    names = {r.common.name for r in roles}
    require(len(names) == len(roles) and "http" in names, "invalid_topology", "Roles must be unique and include HTTP.")
    artifacts = contract_map(value.get("artifacts", {}), Artifact.parse, maximum=MAX_ARTIFACTS)
    binaries = contract_map(value.get("application_executables", {}), ApplicationBinary.parse, maximum=64)
    def secret(v):
        contract_object(v, (), ("prompt",))
        return contract_text(v.get("prompt", "Service secret"), limit=256)
    secrets = contract_map(value.get("secrets", {}), secret, maximum=128)
    service_secret_keys(dict(secrets))
    graph = {r.common.name: r.common.requires for r in roles}
    visited, visiting = set(), set()
    def visit(name):
        require(name in names and name not in visiting, "invalid_topology", "Unknown role dependency or dependency cycle.")
        if name in visited:
            return
        visiting.add(name)
        for dependency in graph[name]:
            visit(dependency)
        visiting.remove(name)
        visited.add(name)
    for name in graph:
        visit(name)
    require(sum(len(r.listeners) for r in roles if isinstance(r, ExecutableRole)) <= MAX_LISTENERS,
            "invalid_topology", "Too many listeners.")
    role_map, artifact_map, binary_map = {r.common.name: r for r in roles}, dict(artifacts), dict(binaries)
    def executable(reference, role):
        if isinstance(reference, PackageExecutable):
            require(reference.package in role.common.packages, "invalid_topology", "Executable package must be declared by its consuming role.")
        elif isinstance(reference, ArtifactExecutable):
            require(reference.artifact in artifact_map and reference.entry in dict(artifact_map[reference.artifact].executables),
                    "invalid_topology", "Unknown artifact executable export.")
        else:
            require(reference.application in binary_map, "invalid_topology", "Unknown application executable.")
    for role in roles:
        mounts = dict(role.common.artifact_mounts)
        for mount in mounts.values():
            require(mount.artifact in artifact_map and mount.bundle in dict(artifact_map[mount.artifact].bundles),
                    "invalid_topology", "Unknown artifact bundle export.")
        for _, binding in role.common.bindings:
            if isinstance(binding, ExecutableBinding):
                executable(binding.executable, role)
            elif isinstance(binding, BundleDirectoryBinding):
                require(binding.mount in mounts, "invalid_topology", "Bundle binding requires a service-local mounted export.")
            else:
                target = role_map.get(binding.role)
                listeners = dict(target.listeners) if isinstance(target, ExecutableRole) else {"http": None} if binding.role == "http" else {}
                require(binding.listener in listeners, "invalid_topology", "Unknown endpoint listener.")
                if isinstance(binding, PublicListenerReference):
                    require(isinstance(target, ExecutableRole) and listeners[binding.listener].bind == "public",
                            "invalid_topology", "Public listener bindings require a declared public listener.")
        destinations = [(m.area, m.path) for m in mounts.values()]
        if isinstance(role, ExecutableRole):
            destinations += [("config", name) for name, _ in role.configs]
        require(not any(a == b and (p == q or p.startswith(q + "/") or q.startswith(p + "/"))
                        for i, (a, p) in enumerate(destinations) for b, q in destinations[i+1:]),
                "invalid_topology", "Service-local config and bundle destinations overlap.")
        if not isinstance(role, ExecutableRole):
            continue
        executable(role.executable, role)
        listeners, configs = dict(role.listeners), dict(role.configs)
        require(role.readiness.listener in listeners, "invalid_topology", "Readiness requires a declared listener.")
        require(all(secret in dict(secrets) for _, secret in role.secret_files), "invalid_topology", "Unknown secret reference.")
        references = list(role.arguments) + [a for _, a in role.environment] + [a for c in configs.values() for _, a in c.bindings]
        for argument in references:
            targets = {ArgumentKind.CONFIG: configs, ArgumentKind.LISTENER_PORT: listeners,
                       ArgumentKind.LISTENER_ADDRESS: listeners, ArgumentKind.BUNDLE: mounts,
                       ArgumentKind.SECRET_FILE: dict(role.secret_files), ArgumentKind.BINDING: dict(role.common.bindings)}
            require(argument.kind not in targets or argument.value in targets[argument.kind],
                    "invalid_topology", "Unknown role argument/config reference.")
            require(argument.kind is not ArgumentKind.BINDING or
                    not isinstance(dict(role.common.bindings)[argument.value], PublicListenerReference),
                    "invalid_topology", "Public listener records are available through the runtime descriptor, not scalar binding arguments.")
    projection = {k: v for k, v in value.items() if k != "sha256"}
    checksum = canonical_digest(projection)
    require(not registered or value["sha256"] == checksum, "invalid_topology", "Topology checksum mismatch.")
    return Topology(TOPOLOGY_SCHEMA, roles, artifacts, binaries, secrets, checksum,
                    json.dumps(projection, sort_keys=True, separators=(",", ":")))


@dataclass(frozen=True)
class PublicListenerBinding:
    ports: tuple[int, ...]
    source_cidrs: tuple[str, ...]
    hostname: str
    public_addresses: tuple[str, ...]

    @classmethod
    def parse(cls, value):
        contract_object(value, ("ports", "source_cidrs", "dns"))
        ports = contract_list(value["ports"], lambda p: contract_int(p, 1, 65535), minimum=1, maximum=1)
        def cidr(item):
            try:
                network = ipaddress.ip_network(item, strict=True) if isinstance(item, str) else None
            except ValueError:
                network = None
            require(network is not None and str(network) == item, "invalid_profile", "Source CIDRs must be canonical.")
            return item
        cidrs = contract_list(value["source_cidrs"], cidr, minimum=1, maximum=64)
        dns = contract_object(value["dns"], ("mode", "hostname", "public_addresses", "proxied"))
        require(dns["mode"] == "external" and dns["proxied"] is False,
                "unsupported_capability", "Public TCP requires external DNS-only records in release 1.")
        addresses = contract_list(dns["public_addresses"], public_address, minimum=1, maximum=16)
        require(len(set(cidrs)) == len(cidrs) and len(set(addresses)) == len(addresses), "invalid_profile", "Duplicate network bindings.")
        require({ipaddress.ip_network(c).version for c in cidrs} <= {ipaddress.ip_address(a).version for a in addresses},
                "invalid_profile", "Source CIDRs require a bound address family.")
        return cls(ports, cidrs, dns_hostname(dns["hostname"]), addresses)


@dataclass(frozen=True)
class DeploymentProfile:
    schema: int
    instance_id: str
    host_id: str | None
    architecture: str
    placement: tuple[tuple[str, int], ...]
    listeners: tuple[tuple[str, PublicListenerBinding], ...]
    canonical_json: str

    def projection(self):
        return json.loads(self.canonical_json)


def validate_feature_profile(value, project=None):
    """Validate topology-independent profile fields through their frozen owners."""
    require(type(value.get("schema")) is int and value["schema"] == FEATURE_PROFILE_SCHEMA,
            "unsupported_schema", "Feature profiles require schema 3.")
    require("placement" in value and "listeners" in value, "invalid_profile", "Profile 3 requires placement and listener bindings.")
    legacy = {k: v for k, v in value.items() if k not in ("placement", "listeners")}
    legacy["schema"] = PROFILE_SCHEMA
    validate_profile(legacy, project)
    def count(v):
        contract_int(v, 0, 65535)
        require(v <= 1, "unsupported_capability", "Numbered services require a later capability.")
        return v
    placement = contract_map(value["placement"], count, maximum=MAX_ROLES)
    raw = value["listeners"]
    require(isinstance(raw, dict) and len(raw) <= MAX_LISTENERS, "invalid_profile", "Invalid public listener bindings.")
    listeners = tuple((n, PublicListenerBinding.parse(v)) for n, v in sorted(raw.items()))
    ports = [p for _, listener in listeners for p in listener.ports]
    require(len(ports) == len(set(ports)) and not {22, 80, 443}.intersection(ports),
            "invalid_profile", "Public listener ports overlap or conflict with reserved host services.")
    return placement, listeners


def parse_profile(document, topology: Topology, project=None) -> DeploymentProfile:
    value = contract_document(document)
    require(isinstance(value, dict), "invalid_profile", "Invalid deployment profile.")
    if topology.schema == 1:
        require(value.get("schema") == PROFILE_SCHEMA, "unsupported_schema", "Topology 1 requires profile 2.")
        validate_profile(value, project)
        placement, listeners = tuple((r.common.name, 1) for r in topology.roles), ()
    else:
        placement, listeners = validate_feature_profile(value, project)
        counts = dict(placement)
        require(set(counts) == {r.common.name for r in topology.roles} and counts.get("http") == 1,
                "invalid_profile", "Placement must explicitly cover every role and place HTTP.")
        for role in topology.roles:
            require(not counts[role.common.name] or all(counts[d] for d in role.common.requires),
                    "invalid_profile", "Placed role prerequisites must be placed.")
            require(not counts[role.common.name] or all(not isinstance(b, (EndpointBinding, PublicListenerReference)) or counts[b.role]
                                                       for _, b in role.common.bindings),
                    "invalid_profile", "A placed consumer cannot bind an unplaced listener.")
        raw = value["listeners"]
        expected = {r.common.name + "." + n for r in topology.roles if isinstance(r, ExecutableRole)
                    and counts[r.common.name] for n, l in r.listeners if l.bind == "public"}
        require(set(raw) == expected, "invalid_profile", "Bind every placed public listener exactly once.")
        platform = "linux-" + value["architecture"]
        require(all(platform in dict(a.platforms) for _, a in topology.artifacts)
                and all(a.go_package or platform in dict(a.platforms) for _, a in topology.application_executables),
                "invalid_profile", "Missing target architecture artifact or executable.")
    return DeploymentProfile(value["schema"], value["instance_id"], value["host"]["host_id"], value["architecture"],
                             placement, listeners, json.dumps(value, sort_keys=True, separators=(",", ":")))
