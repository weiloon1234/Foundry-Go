"""Topology-2 inventory, literal unit rendering and tool-driven dependencies.

UnitSpec and RuntimeResolution are the handoff from artifact/package and listener
owners. No package installation, listener allocation or lifecycle activation lives
here. Production dispatch stays closed until the complete lifecycle is installed.
"""
from dataclasses import dataclass, replace
import http.client
from pathlib import Path
import re
import socket
import time

from contracts import (Deployment, RuntimeResolution, UnitIdentity, canonical_digest,
                       contract_digest, contract_path, contract_uuid, instance_paths,
                       marker, record_document, require)
from topology import (ArgumentKind, BuiltinRole, ExecutableRole, parse_profile,
                      parse_topology)

RUNTIME_ENTRY = "/usr/local/lib/foundry/runtime-v1"
TEMPLATES = Path(__file__).resolve().parents[1] / "server/templates"


def systemd_word(value, *, command=False):
    """One literal systemd word. Dollar expansion applies only to command argv."""
    require(isinstance(value, str) and all(ord(c) >= 32 and ord(c) != 127 for c in value),
            "service_argument", "Unit values must be literal text without controls.")
    value = value.replace("\\", "\\\\").replace('"', '\\"').replace("%", "%%")
    if command:
        value = value.replace("$", "$$")
    return '"' + value + '"'


def command_line(arguments):
    return " ".join(systemd_word(value, command=True) for value in arguments)


@dataclass(frozen=True)
class RoleState:
    enabled: bool
    running: bool
    held: bool
    pending: bool
    ready: bool


class UnitInventory:
    def __init__(self, name, topology, profile, deployment, resolution):
        instance_paths(name)
        self.name = name
        self.topology = parse_topology(topology.projection())
        self.profile = parse_profile(profile.projection(), self.topology)
        self.deployment = Deployment.parse(record_document(deployment))
        self.resolution = RuntimeResolution.parse(record_document(resolution))
        placed = {name for name, count in self.profile.placement if count}
        require(deployment.instance_id == profile.instance_id and deployment.topology_sha256 == topology.sha256
                and deployment.profile_sha256 == canonical_digest(profile.projection())
                and resolution.deployment_sha256 == canonical_digest(record_document(deployment))
                and {u.identity.role for u in deployment.units} == placed == set(resolution.roles),
                "service_inventory", "Unit inventory differs from the selected declaration or resolution.")
        self.roles = {role.common.name: role for role in self.topology.roles if role.common.name in placed}
        self.specs = {spec.identity.role: spec for spec in self.deployment.units}
        declarations = {r["name"]: r for r in self.topology.projection()["roles"]}
        require(all(spec.declaration_sha256 == canonical_digest(declarations[name])
                    for name, spec in self.specs.items()),
                "service_inventory", "Resolved unit policy differs from its declared role.")
        from topology import role_unit_name
        self.units = {role: role_unit_name(name, value) for role, value in self.roles.items()}
        self.start_order(placed)

    def start_order(self, selected):
        selected = set(selected)
        require(selected <= self.roles.keys(), "service_selection", "Unknown selected role.")
        require(all(set(self.roles[name].common.requires) <= selected for name in selected),
                "service_dependency", "Enabled roles must include every prerequisite; use plain restart.")
        ordered = []
        def visit(name):
            if name in ordered:
                return
            for dependency in self.roles[name].common.requires:
                visit(dependency)
            ordered.append(name)
        for name in self.roles:
            if name in selected:
                visit(name)
        return tuple(ordered)

    def stop_order(self, selected):
        selected = set(selected)
        require(selected <= self.roles.keys(), "service_selection", "Unknown selected role.")
        return tuple(name for name in reversed(self.start_order(self.roles)) if name in selected)

    def start_in_order(self, selected, deadline, start, await_ready):
        """Lifecycle adapters share one deadline; a failed provider never starts a consumer.

        These adapters belong to the framework executor, never to a declaration.
        They do not run on normal boot or automatic systemd restart.
        """
        for name in self.start_order(selected):
            require(time.monotonic() < deadline, "service_deadline", "Startup exhausted its shared deadline.")
            start(name, deadline)
            require(await_ready(name, deadline) and time.monotonic() < deadline,
                    "readiness_failed", "Prerequisite identity, readiness or stability was not established.")

    def prerequisites(self, selected):
        result = set()
        def visit(name):
            for dependency in self.roles[name].common.requires:
                if dependency not in result:
                    result.add(dependency)
                    visit(dependency)
        for name in selected:
            visit(name)
        return result

    def named_operation(self, action, selected, states):
        """Consume bounded readiness observations; never expand or enable a selection."""
        selected = set(selected)
        require(action in ("restart", "stop") and selected and selected <= self.roles.keys()
                and set(states) == set(self.roles), "service_selection", "Incomplete named-operation observation.")
        enabled = {name for name, state in states.items() if state.enabled}
        self.start_order(enabled)
        dependents = {name for name, state in states.items() if (state.enabled or state.running)
                      and name not in selected and self.prerequisites({name}) & selected}
        require(not dependents, "service_dependents", "Include dependent roles: " +
                ", ".join(sorted(selected | dependents)) + "; use plain restart/stop.")
        if action == "restart":
            prerequisites = self.prerequisites(selected) - selected
            rejected = {name for name in prerequisites if not states[name].enabled or
                        states[name].held or states[name].pending or not states[name].ready}
            require(not rejected, "service_prerequisite", "Prerequisites unavailable: " +
                    ", ".join(sorted(rejected)) + "; use plain restart.")
            require(not any(states[name].held or states[name].pending for name in selected),
                    "service_held", "Selected roles are held or pending reconciliation.")
        return self.stop_order(selected) if action == "stop" else tuple(
            name for name in self.start_order(self.roles) if name in selected)


@dataclass(frozen=True)
class ServicePaths:
    base: str
    state: str

    @classmethod
    def for_unit(cls, identity, resolution_id):
        from dataclasses import asdict
        UnitIdentity.parse(asdict(identity))
        contract_uuid(resolution_id)
        return cls(f"/run/foundry/services/{identity.instance_id}/{resolution_id}/{identity.role}",
                   f"/var/lib/foundry/service-state/{identity.instance_id}/{identity.role}")

    def config(self, name, mode):
        from contracts import contract_name
        contract_name(name)
        return (self.state + "/seeds/" + name if mode == "seed-once"
                else self.base + "/current/config/" + name) + "/value"

    def bundle(self, mount):
        return self.base + "/view/" + mount.area + "/" + mount.path


@dataclass(frozen=True)
class BundleView:
    """Lane B supplies immutable inventoried exports; no discovery by directory scan."""
    name: str
    source: str
    destination: str
    artifact_sha256: str
    members: tuple


def bundle_views(role, paths, topology, architecture, artifact_roots, payloads):
    result = []
    for name, mount in role.common.artifact_mounts:
        artifact = dict(topology.artifacts)[mount.artifact]
        require(mount.artifact in artifact_roots, "service_bundle", "Verified artifact installation is missing.")
        root, checksum = artifact_roots[mount.artifact]
        contract_path(root, absolute=True)
        contract_digest(checksum)
        require(checksum == dict(artifact.platforms)["linux-" + architecture].sha256,
                "service_bundle", "Bundle source is not a declared artifact pin.")
        payload = next((p for p in payloads if p.artifact == mount.artifact), None)
        require(payload is not None and payload.sha256 == checksum and payload.platform == "linux-" + architecture,
                "service_bundle", "Bundle inventory differs from the selected artifact pin.")
        prefix = dict(artifact.bundles)[mount.bundle] + "/"
        source = root + "/" + prefix[:-1]
        members = tuple(replace(m, path=m.path[len(prefix):]) for m in payload.members if m.path.startswith(prefix))
        require(members, "service_bundle", "Bundle export is empty.")
        result.append(BundleView(name, source, paths.bundle(mount), checksum, members))
    return tuple(result)


def configuration_digest(role, members, views=(), secret_paths=None):
    """Nonsecret configuration identity shared by resolution and preparation."""
    from dataclasses import asdict
    sources = {m.path: m for m in members}
    require(all(c.source in sources for _, c in role.configs),
            "service_config_source", "Configuration inventory is incomplete.")
    return canonical_digest({"configs": [(name, asdict(config), asdict(sources[config.source]))
                                          for name, config in role.configs],
                             "bundles": [asdict(view) for view in views],
                             "secret_paths": secret_paths or {}})


def validate_views(role, paths, views):
    expected = dict(role.common.artifact_mounts)
    require({v.name for v in views} == set(expected) and len(views) == len(expected),
            "service_bundle", "Bundle views must exactly cover the declared placements.")
    for view in views:
        contract_path(view.source, absolute=True)
        contract_digest(view.artifact_sha256)
        require(":" not in view.source and view.destination == paths.bundle(expected[view.name]),
                "service_bundle", "Bundle view is outside its protected service-local placement.")


def resolve_argument(argument, role, spec, resolution, paths, views, secret_paths):
    kind, value = argument.kind, argument.value
    if kind is ArgumentKind.LITERAL:
        return value
    if kind is ArgumentKind.CONFIG:
        return paths.config(value, dict(role.configs)[value].mode)
    if kind in (ArgumentKind.LISTENER_PORT, ArgumentKind.LISTENER_ADDRESS):
        endpoint = next((e for e in resolution.endpoints if e.role == role.common.name and e.listener == value), None)
        require(endpoint is not None, "service_listener", "Declared listener has no selected allocation.")
        return str(endpoint.port) if kind is ArgumentKind.LISTENER_PORT else endpoint.address
    if kind is ArgumentKind.BUNDLE:
        return next(view.destination for view in views if view.name == value)
    if kind is ArgumentKind.SECRET_FILE:
        require(value in secret_paths, "service_secret", "Declared secret-file reference is unavailable.")
        return contract_path(secret_paths[value], absolute=True)
    if kind is ArgumentKind.BINDING:
        for binding in (*spec.bindings.executables, *spec.bindings.bundles):
            if binding.name == value:
                return binding.path
        endpoint = next((b for b in spec.bindings.endpoints if b.name == value), None)
        require(endpoint is not None, "service_binding", "Declared binding is unresolved.")
        address = endpoint.endpoint.address
        return f"{endpoint.scheme}://{'[' + address + ']' if ':' in address else address}:{endpoint.endpoint.port}"
    if kind is ArgumentKind.COPY_NUMBER:
        return str(spec.identity.ordinal)
    if kind is ArgumentKind.STATE_PATH:
        return paths.state + "/data/" + value
    if kind is ArgumentKind.RUNTIME_PATH:
        return paths.base + "/current/runtime/" + value
    if kind is ArgumentKind.CONFIG_ROOT:
        return paths.base + "/view/config"
    require(kind is ArgumentKind.DATA_ROOT, "service_argument", "Unsupported argument kind.")
    return paths.base + "/view/data"


def render_unit(inventory, name, entry, bundle_sha256, *, views=(), secret_paths=None):
    """Render inert units. Installation and observed loaded-unit verification are lifecycle work."""
    require(entry["profile"]["instance_id"] == inventory.profile.instance_id and entry["name"] == inventory.name,
            "service_inventory", "Unit account belongs to another instance.")
    contract_digest(bundle_sha256)
    role, spec = inventory.roles[name], inventory.specs[name]
    paths = ServicePaths.for_unit(spec.identity, inventory.resolution.id)
    validate_views(role, paths, views)
    helper = ["/usr/bin/python3", "-I", RUNTIME_ENTRY]
    identity = [spec.identity.instance_id, name, "1", inventory.resolution.id,
                bundle_sha256, canonical_digest(record_document(spec))]
    hooks = {action: command_line([*helper, action, *identity]) for action in ("admit", "prepare", "dispatch", "cleanup")}
    after = " ".join(inventory.units[n] for n in role.common.requires)
    boot = f"foundry-admission-{spec.identity.instance_id}.service"
    mounts = "\n".join("BindReadOnlyPaths=" + systemd_word(view.source + ":" + view.destination) for view in views)
    if isinstance(role, BuiltinRole):
        # Keep the existing built-in policies, including B26 upload write paths.
        from provision import _template
        legacy = {**entry, "topology": {"schema": 1, "roles": [{"name": "http"}]}}
        from topology import validate_topology
        legacy["topology"] = validate_topology(legacy["topology"])
        content = _template(TEMPLATES.parent.parent, f"foundry-{name}@.service", legacy)
        content = re.sub(r"^ExecStart=.*$", "ExecStart=" + hooks["dispatch"], content, flags=re.M)
        # Worker connection is frozen in argv; a legacy worker.env is not an input.
        content = re.sub(r"^EnvironmentFile=.*\n", "", content, flags=re.M)
        content = content.replace("[Service]\n", "[Service]\nExecCondition=+" + hooks["admit"] +
                                  "\nExecStartPre=+" + hooks["prepare"] + "\nExecStopPost=+" + hooks["cleanup"] + "\n")
        content = content.replace("[Unit]\n", f"[Unit]\nAfter={boot} {after}\n")
        if mounts:
            content = content.replace("[Service]\n", "[Service]\n" + mounts + "\n")
        return content
    secret_paths = secret_paths or {}
    arguments = tuple(resolve_argument(a, role, spec, inventory.resolution, paths, views, secret_paths) for a in role.arguments)
    require(arguments == spec.arguments, "service_arguments", "Frozen argv differs from the selected typed arguments.")
    environment = [(key, resolve_argument(value, role, spec, inventory.resolution, paths, views, secret_paths))
                   for key, value in role.environment]
    environment += [(key, contract_path(secret_paths[key], absolute=True)) for key, _ in role.secret_files]
    replacements = {"MARKER": marker(entry), "ROLE": name, "ACCOUNT": inventory.name,
                    "AFTER": f"{boot} {after}", "BASE": contract_path(paths.base, absolute=True),
                    "WRITABLE": " ".join(systemd_word(p) for p in (paths.state, paths.base + "/invocations")),
                    "ADMIT": hooks["admit"], "PREPARE": hooks["prepare"], "DISPATCH": hooks["dispatch"],
                    "CLEANUP": hooks["cleanup"], "RESTART": role.restart.policy,
                    "DELAY": str(role.restart.delay_seconds), "BURST": str(role.restart.burst),
                    "WINDOW": str(role.restart.window_seconds), "STOP": str(role.restart.stop_seconds),
                    "MEMORY": str(role.limits.memory_bytes), "CPU": str(role.limits.cpu_percent),
                    "TASKS": str(role.limits.tasks), "FILES": str(role.limits.file_descriptors),
                    "ENVIRONMENT": "\n".join("Environment=" + systemd_word(key + "=" + value) for key, value in environment),
                    "MOUNTS": mounts}
    content = (TEMPLATES / "custom.service").read_text()
    return re.sub(r"@([A-Z]+)@", lambda match: replacements[match[1]], content)


def readiness_probe(role):
    from topology import Probe
    if isinstance(role, ExecutableRole):
        return role.readiness
    return Probe("http", "http", 90, "/health/ready", 200, None) if role.common.name == "http" else None


def probe(role, resolution, deadline):
    from host import OperationTimeout, wall_clock_budget
    contract = readiness_probe(role)
    if contract is None:
        return True
    remaining = min(contract.timeout_seconds, deadline - time.monotonic())
    if remaining <= 0:
        return False
    try:
        with wall_clock_budget(remaining):
            return _probe(role, resolution, deadline)
    except OperationTimeout:
        return False


def _probe(role, resolution, deadline):
    """One bounded local probe. Identity/stability checks remain required around it."""
    contract = readiness_probe(role)
    endpoint = next((e for e in resolution.endpoints if e.role == role.common.name and
                     e.listener == contract.listener), None)
    require(endpoint is not None, "service_listener", "Readiness allocation is missing.")
    remaining = min(contract.timeout_seconds, deadline - time.monotonic())
    if remaining <= 0:
        return False
    try:
        if contract.kind == "tcp":
            with socket.create_connection((endpoint.address, endpoint.port), timeout=remaining):
                return True
        connection = http.client.HTTPConnection(endpoint.address, endpoint.port, timeout=remaining)
        try:
            connection.request("GET", contract.path, headers={"Connection": "close"})
            response = connection.getresponse()
            # A literal bounded response, no redirects, proxies or remote URLs.
            if response.status != contract.status:
                return False
            body = response.read(65537)
            if len(body) > 65536:
                return False
            if not isinstance(role, ExecutableRole):
                from release import readiness_body
                import json
                try:
                    return readiness_body(json.loads(body))
                except (ValueError, UnicodeError):
                    return False
            return contract.body is None or contract.body.encode() in body
        finally:
            connection.close()
    except (OSError, http.client.HTTPException):
        return False


def wait_ready(host, inventory, name, deadline):
    return wait_ready_set(host, inventory, (name,), deadline)


def wait_ready_set(host, inventory, names, deadline):
    """Observe every selected identity/probe in one shared stability window."""
    from contracts import digest
    from admission import protected_path
    from host import OperationTimeout, wall_clock_budget
    from release import STABILITY_SECONDS, _matches_executable, service
    names = tuple(names)
    require(names and set(names) <= inventory.roles.keys(), "service_selection", "Select known readiness roles.")
    checks = {}
    for name in names:
        role, spec = inventory.roles[name], inventory.specs[name]
        contract = readiness_probe(role)
        executable = protected_path(host, host.paths.p(spec.executable), "file")
        require(executable.exists() and executable.stat().st_mode & 0o111 and digest(executable) == spec.executable_sha256,
                "service_executable", "Selected executable fingerprint has drifted.")
        endpoint = next((e for e in inventory.resolution.endpoints if e.role == name and contract and e.listener == contract.listener), None)
        local = (f"[{endpoint.address}]:{endpoint.port}" if ":" in endpoint.address else f"{endpoint.address}:{endpoint.port}") if endpoint else None
        checks[name] = (role, executable, endpoint, local)
        if contract:
            deadline = min(deadline, time.monotonic() + contract.timeout_seconds)
    tokens, stable_since = {}, None
    if deadline <= time.monotonic():
        return False
    try:
        with wall_clock_budget(deadline - time.monotonic()):
            while time.monotonic() < deadline:
                healthy, observed_tokens = True, {}
                for name, (role, executable, endpoint, local) in checks.items():
                    if time.monotonic() >= deadline:
                        return False
                    observed = service(host, inventory.units[name], timeout=min(5, deadline - time.monotonic()))
                    if observed["ActiveState"] == "failed" or observed["NRestarts"] != "0":
                        return False
                    current = (observed["MainPID"], (observed.get("process") or {}).get("start"))
                    if name in tokens and current != tokens[name]:
                        return False
                    observed_tokens[name] = current
                    ready = _matches_executable(observed, executable)
                    if ready and endpoint:
                        listeners = host.run(["ss", "-Hltpn", f"sport = :{endpoint.port}"], check=False,
                                             timeout=min(5, deadline - time.monotonic()))
                        ready = listeners.returncode == 0 and any(
                            len(line.split()) >= 5 and line.split()[3] == local and
                            f"pid={observed['MainPID']}," in line for line in listeners.stdout.splitlines())
                    healthy = ready and probe(role, inventory.resolution, deadline) and healthy
                # Recheck the entire set after the last probe, including prerequisites
                # observed before a slow dependent. A changed identity resets nothing.
                if healthy:
                    for name, (_, executable, _, _) in checks.items():
                        if time.monotonic() >= deadline:
                            return False
                        after = service(host, inventory.units[name], timeout=min(5, deadline - time.monotonic()))
                        if (not _matches_executable(after, executable) or after["NRestarts"] != "0" or
                                (after["MainPID"], (after.get("process") or {}).get("start")) != observed_tokens[name]):
                            return False
                    if stable_since is None:
                        tokens, stable_since = observed_tokens, time.monotonic()
                    if time.monotonic() - stable_since >= STABILITY_SECONDS and time.monotonic() < deadline:
                        return True
                else:
                    stable_since = None
                time.sleep(max(0, min(.1, deadline - time.monotonic())))
    except OperationTimeout:
        return False
    return False
