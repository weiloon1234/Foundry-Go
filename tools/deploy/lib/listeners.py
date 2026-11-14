"""Socket ownership, reservations and network evidence for the admission owner.

The caller holds host then instance locks and retains *all* historical resolutions
while processes, pending operations or nginx workers can still reference them.
This module does not select deployments, grant permits or write admission state.
"""
from dataclasses import asdict, dataclass, replace
import ipaddress
import re
import socket
import uuid

from contracts import (AllocatedEndpoint, ClosureEvidence, ClosureStage, DNSObservation,
                       canonical_digest, contract_uuid, now, record_document, require)


@dataclass(frozen=True)
class SocketIdentity:
    transport: str
    family: int
    address: str
    port: int

    def __post_init__(self):
        address = ipaddress.ip_address(self.address)
        require(self.transport in ("tcp", "udp") and self.family == address.version
                and type(self.port) is int and 0 < self.port < 65536,
                "listener_invalid", "Invalid socket identity.")


@dataclass(frozen=True)
class ListenerClaim:
    instance_id: str
    role: str
    listener: str
    ordinal: int
    socket: SocketIdentity
    owner: str


@dataclass(frozen=True)
class ObservedSocket:
    socket: SocketIdentity
    pids: tuple[int, ...]


def conflicts(left, right):
    """IPv6 wildcards may also bind IPv4, irrespective of host sysctl defaults."""
    if left.transport != right.transport or left.port != right.port:
        return False
    a, b = ipaddress.ip_address(left.address), ipaddress.ip_address(right.address)
    if a == ipaddress.ip_address("::") or b == ipaddress.ip_address("::"):
        return True
    if getattr(a, "ipv4_mapped", None):
        a = a.ipv4_mapped
    if getattr(b, "ipv4_mapped", None):
        b = b.ipv4_mapped
    return a.version == b.version and (a == b or a.is_unspecified or b.is_unspecified)


def public_policies(profile, topology):
    """Resolve modes from the matching topology, never from an address binding."""
    from topology import ExecutableRole, parse_profile
    parse_profile(profile.projection(), topology)
    declarations = {role.common.name + "." + name: listener
                    for role in topology.roles if isinstance(role, ExecutableRole)
                    for name, listener in role.listeners}
    return {name: declarations[name] for name, _ in profile.listeners}


def public_claims(profile):
    return tuple(ListenerClaim(profile.instance_id, *name.split("."), 1,
                               SocketIdentity("tcp", ipaddress.ip_address(address).version, address, port), "nginx")
                 for name, binding in profile.listeners for address in binding.public_addresses for port in binding.ports)


def private_claims(instance_id, resolution):
    return tuple(ListenerClaim(instance_id, endpoint.role, endpoint.listener, 1,
                               SocketIdentity("tcp", ipaddress.ip_address(endpoint.address).version,
                                              endpoint.address, endpoint.port), "application")
                 for endpoint in resolution.endpoints)


def require_available(requested, reserved, observed):
    """New allocations cannot reuse even an identical historical claim."""
    requested, reserved, observed = tuple(requested), tuple(reserved), tuple(observed)
    for index, claim in enumerate(requested):
        require(not any(conflicts(claim.socket, other.socket) for other in (*reserved, *requested[:index], *observed)),
                "listener_conflict", "A requested socket overlaps a reservation or a live socket.")


def allocate_private(instance_id, roles, reserved, observed, *, first=18000, last=60999):
    """Resolve (role, listener) pairs once; lane A owns the resulting resolution."""
    contract_uuid(instance_id)
    result, claims = [], list(reserved)
    for role, name in roles:
        for port in range(first, last + 1):
            endpoint = AllocatedEndpoint.parse({"role": role, "listener": name, "address": "127.0.0.1", "port": port})
            claim = ListenerClaim(instance_id, role, name, 1, SocketIdentity("tcp", 4, endpoint.address, port), "application")
            if any(conflicts(claim.socket, other.socket) for other in (*claims, *observed)):
                continue
            result.append(endpoint)
            claims.append(claim)
            break
        else:
            require(False, "listener_capacity", "No fresh private backend allocation is available.")
    return tuple(result)


def parse_sockets(text, transport, family):
    require(len(text) <= 4 * 1024 * 1024, "listener_observation", "Socket inventory exceeds its bound.")
    result = []
    for line in text.splitlines():
        columns = line.split()
        require(len(columns) >= 5, "listener_observation", "Socket inventory is incomplete.")
        address, separator, port = columns[3].rpartition(":")
        address = address.strip("[]").split("%", 1)[0]
        if address == "*":
            address = "0.0.0.0" if family == 4 else "::"
        require(separator and port.isdigit(), "listener_observation", "Socket inventory is not numeric.")
        result.append(ObservedSocket(SocketIdentity(transport, family, address, int(port)),
                                     tuple(sorted({int(p) for p in re.findall(r"pid=(\d+)", line)}))))
    return tuple(result)


def observe_sockets(host):
    result = []
    for family in (4, 6):
        for protocol, flag in (("tcp", "-t"), ("udp", "-u")):
            response = host.run(["ss", "-H", "-l", "-n", "-p", flag, "-" + str(family)], timeout=5)
            result.extend(parse_sockets(response.stdout, protocol, family))
    return tuple(result)


def process_members(host, unit):
    """Read the entire cgroup-v2 subtree, not only systemd MainPID."""
    result = host.run(["systemctl", "show", unit, "--property=ControlGroup", "--value"], timeout=5)
    group = result.stdout.strip()
    require(group.startswith("/") and ".." not in group.split("/") and group != "/",
            "listener_owner_unverified", "A concrete service cgroup is required.")
    root = host.paths.p("/sys/fs/cgroup" + group)
    require(root.is_dir() and not root.is_symlink(), "listener_owner_unverified", "Service cgroup is unavailable.")
    members = set()
    files = list(root.rglob("cgroup.procs"))
    require(len(files) <= 4096, "listener_owner_unverified", "Service cgroup exceeds its observation bound.")
    for path in files:
        require(not path.is_symlink(), "listener_owner_unverified", "Service cgroup is not inspectable.")
        members.update(int(pid) for pid in path.read_text().split())
    return frozenset(members)


def ownership_findings(claims, observed, owners, *, edge_verified=False):
    """owners maps (instance, role) and 'nginx' to freshly observed cgroup PIDs."""
    findings = []
    for observed_socket in observed:
        identity = observed_socket.socket
        matching = [c for c in claims if c.socket == identity]
        if not matching:
            if not ipaddress.ip_address(identity.address).is_loopback:
                findings.append({"code": "unexpected_public_listener", **asdict(identity)})
            continue
        for claim in matching:
            expected = owners.get("nginx" if claim.owner == "nginx" else (claim.instance_id, claim.role), frozenset())
            if (not observed_socket.pids or not set(observed_socket.pids) <= expected
                    or claim.owner == "nginx" and not edge_verified):
                findings.append({"code": "listener_owner_unverified", "instance_id": claim.instance_id,
                                 "role": claim.role, "listener": claim.listener, **asdict(identity)})
    for claim in claims:
        if not any(s.socket == claim.socket for s in observed):
            findings.append({"code": "declared_listener_absent", "instance_id": claim.instance_id,
                             "role": claim.role, "listener": claim.listener, **asdict(claim.socket)})
    return findings


def ingress_evidence(operation_id, boot_id, old, candidate, units, retained_units_sha256, firewall, edge, *, claims):
    """Only new ingress is withdrawn here; this never authorizes a candidate start."""
    require(firewall.closed and edge.closed and firewall.boot_id == edge.boot_id == boot_id,
            "ingress_unverified", "Fresh closed firewall and edge observations are required.")
    roles = {u.role for u in units}
    require(roles and roles == set(edge.roles) and len({u.instance_id for u in units}) == 1
            and {u.instance_id for u in units} == {edge.instance_id, firewall.instance_id},
            "ingress_unverified", "Network observations must cover the exact affected instance and roles.")
    require(set(firewall.sockets) == {c.socket for c in claims}
            and all(c.role in roles and c.instance_id == edge.instance_id for c in claims),
            "ingress_unverified", "The firewall proof must cover the complete affected old/candidate socket union.")
    endpoints = tuple(dict.fromkeys(e for r in (old, candidate) for e in r.endpoints if e.role in roles))
    evidence = ClosureEvidence(str(uuid.uuid4()), operation_id, boot_id, old.id, candidate.id, tuple(units),
                               endpoints, retained_units_sha256,
                               canonical_digest({"firewall": firewall.sha256, "edge": edge.sha256}),
                               None, None, now(), ClosureStage.INGRESS_WITHDRAWN)
    record_document(evidence)
    return evidence


def complete_closure(evidence, candidate, *, backend_exit_sha256, reserved, old_references):
    """Lane A supplies fresh cgroup/job/control-process exit proof for this scope.

    Reference inventory includes HTTP, all old workers and unaffected upstreams.
    Reservations stay live until their owner proves those references retired.
    This conservative path never reuses a possibly referenced address (A/B/A).
    """
    require(evidence.stage is ClosureStage.INGRESS_WITHDRAWN and evidence.candidate_resolution_id == candidate.id,
            "admission_closed_required", "Complete the current ingress withdrawal before closure.")
    roles = {u.role for u in evidence.units}
    endpoints = tuple(e for e in candidate.endpoints if e.role in roles)
    require(set(endpoints) <= set(reserved) and set(endpoints) <= set(evidence.endpoints),
            "backend_reservation_required", "Candidate backends must retain their exact reservations.")
    references = {(e.address, e.port) for e in old_references}
    require(not any(conflicts(SocketIdentity("tcp", ipaddress.ip_address(e.address).version, e.address, e.port),
                              SocketIdentity("tcp", ipaddress.ip_address(address).version, address, port))
                    for e in endpoints for address, port in references),
            "old_backend_reference", "An old nginx reference can still reach a candidate backend.")
    result = replace(evidence, backend_exit_sha256=backend_exit_sha256,
                     old_reference_isolation_sha256=canonical_digest({"reserved": [asdict(e) for e in endpoints],
                         "references": sorted(references)}), observed_at=now(), stage=ClosureStage.APPLICATION_CLOSED)
    record_document(result)
    return result


def external_dns(binding, observation=None, *, provider_dns_only=None):
    """No resolver answer proves provider proxy mode or global propagation."""
    if observation is not None:
        require(isinstance(observation, DNSObservation) and observation.hostname == binding.hostname,
                "dns_observation_invalid", "DNS observation belongs to another hostname.")
    return {"hostname": binding.hostname, "mode": "external", "provider_write": False,
            "provider_dns_only": provider_dns_only,
            "observed_at": observation.observed_at if observation else None,
            "source": observation.source if observation else None,
            "addresses_match": set(observation.addresses) == set(binding.public_addresses) if observation else None,
            "cutover": "pending_external_dns",
            "instructions": [{"name": binding.hostname, "type": "A" if ipaddress.ip_address(a).version == 4 else "AAAA",
                              "address": a, "proxied": False} for a in binding.public_addresses]}


def observe_dns(binding):
    """Bounded recursive lookup; no provider credentials or provider ownership claim."""
    from host import wall_clock_budget
    with wall_clock_budget(5):
        answers = socket.getaddrinfo(binding.hostname, None, socket.AF_UNSPEC, socket.SOCK_STREAM)
    addresses = tuple(sorted({str(ipaddress.ip_address(answer[4][0])) for answer in answers}))
    return DNSObservation.parse({"hostname": binding.hostname, "addresses": list(addresses),
                                 "observed_at": now(), "source": "recursive"})


def _feature_owners():
    import importlib
    from contracts import DeployError
    try:
        admission = importlib.import_module("admission")
        services = importlib.import_module("services")
    except ModuleNotFoundError as error:
        if error.name not in ("admission", "services"):
            raise
        raise DeployError("capability_unavailable", "Install lane A's admission and service owners before using the network seam.") from None
    require(hasattr(services, "UnitInventory"), "capability_unavailable", "The typed service inventory is required.")
    return admission, services


def _feature_inventory(host, entry, resolution_id):
    from contracts import Deployment, RuntimeResolution
    from topology import parse_profile, parse_topology
    admission, services = _feature_owners()
    path = host.paths.opt(entry["name"]) / "runtime" / contract_uuid(resolution_id) / "launch.json"
    launch = admission.read_owned(host, path, lambda value: value)
    topology = parse_topology(launch["topology"])
    profile = parse_profile(launch["profile"], topology)
    resolution = RuntimeResolution.parse(launch["resolution"])
    require(resolution.id == resolution_id and profile.instance_id == entry["profile"]["instance_id"]
            and profile.projection()["domain"] == entry["profile"]["domain"],
            "network_selection", "Launch identity differs from the registered instance.")
    return services.UnitInventory(entry["name"], topology, profile, Deployment.parse(launch["deployment"]), resolution)


def _feature_history(host):
    """All immutable launch records remain reservations, including failed candidates."""
    admission, _ = _feature_owners()
    registry = host.read_registry()
    result = []
    for identity, entry in registry["instances"].items():
        require(identity == entry["profile"]["instance_id"], "network_inventory", "Invalid host instance inventory.")
        root = admission.protected_path(host, host.paths.opt(entry["name"]) / "runtime", "directory")
        if not root.exists():
            continue
        paths = list(root.iterdir())
        require(len(paths) <= 256, "network_inventory", "Retained resolution inventory exceeds its bound.")
        for path in paths:
            contract_uuid(path.name)
            result.append((entry, _feature_inventory(host, entry, path.name)))
    return registry, tuple(result)


def _feature_projection(entry, topology, profile, endpoints):
    """Reuse the HTTP/upload renderer with a builtin HTTP-only template view."""
    from pathlib import Path
    from types import SimpleNamespace
    from provision import _template
    from topology import validate_topology
    import edge
    endpoint = next(e for e in endpoints if (e.role, e.listener) == ("http", "http"))
    require(endpoint.address == "127.0.0.1", "network_projection", "The owned HTTP renderer requires IPv4 loopback.")
    view = {**entry, "profile": profile.projection(), "allocations": {**entry["allocations"], "http_port": endpoint.port},
            "topology": validate_topology({"schema": 1, "roles": [{"name": "http"}]})}
    http = _template(Path(__file__).resolve().parents[1], "nginx-https.conf", view)
    return {"http": edge.gate_http(profile.instance_id, http),
            **edge.stream_projection(profile, SimpleNamespace(endpoints=endpoints), entry["certificate"]["lineage"], topology=topology)}


def _feature_http(host, entry, inventory, history, *, closed, write=True):
    """Only exact owned renderings may be replaced. Closed HTTP has no upstream."""
    from contracts import PREFIX
    import edge
    admission, _ = _feature_owners()
    projection = _feature_projection(entry, inventory.topology, inventory.profile, inventory.resolution.endpoints)
    def without_upstreams(text):
        return re.sub(r"proxy_pass http://127\.0\.0\.1:[0-9]+;", "return 503;", text)
    site = host.paths.p(f"/etc/nginx/sites-available/{PREFIX}-{entry['name']}.conf")
    enabled = host.paths.p(f"/etc/nginx/sites-enabled/{site.name}")
    admission.protected_path(host, enabled.parent, "directory")
    require(enabled.is_symlink() and enabled.resolve() == site, "network_projection", "The owned HTTP site must be enabled.")
    accepted = {projection["http"], without_upstreams(projection["http"])}
    for owner, previous in history:
        if owner["profile"]["instance_id"] == inventory.profile.instance_id:
            text = _feature_projection(owner, previous.topology, previous.profile, previous.resolution.endpoints)["http"]
            accepted.update((text, without_upstreams(text)))
    # Initial adoption can start from the existing ungated HTTP/upload site.
    from provision import _template
    from pathlib import Path
    from topology import validate_topology
    view = {**entry, "topology": validate_topology({"schema": 1, "roles": [{"name": "http"}]})}
    accepted.add(_template(Path(__file__).resolve().parents[1], "nginx-https.conf", view))
    current = admission.protected_path(host, site, "file").read_text()
    require(current in accepted,
            "network_projection", "HTTP configuration is foreign or differs from retained owned projections.")
    target = without_upstreams(projection["http"]) if closed else projection["http"]
    if not write:
        require(current == target, "network_not_withdrawn", "The owned HTTP site is not withdrawn.")
    elif current != target:
        host.owned_write(site, target)
    return projection


def feature_allocate(host, entry, topology, profile, operation_id, *, retained=None):
    """Lane A checkpoint-2 seam: inspect/reserve in memory; perform no writes/reloads."""
    import edge
    from topology import ExecutableRole
    contract_uuid(operation_id)
    require(profile.instance_id == entry["profile"]["instance_id"], "network_scope", "Allocation belongs to another instance.")
    public_policies(profile, topology)
    registry, history = _feature_history(host)
    from report import claim_inventory
    observed = observe_sockets(host)
    requested = public_claims(profile)
    other = tuple(c for c in claim_inventory(registry) if c.instance_id != profile.instance_id)
    other += tuple(c for _, inv in history if inv.profile.instance_id != profile.instance_id for c in public_claims(inv.profile))
    owned = {c.socket for _, inv in history if inv.profile.instance_id == profile.instance_id for c in public_claims(inv.profile)}
    nginx = process_members(host, "nginx.service") if owned else frozenset()
    unknown = tuple(s for s in observed if not (s.socket in owned and s.pids and set(s.pids) <= nginx))
    require_available(requested, other, unknown)
    reserved = tuple(c for _, inv in history for c in private_claims(inv.profile.instance_id, inv.resolution))
    reserved += tuple(ListenerClaim(identity, "http", "http", 1,
                      SocketIdentity("tcp", 4, "127.0.0.1", owner["allocations"]["http_port"]), "application")
                      for identity, owner in registry["instances"].items())
    # Reading the effective inventory is deliberately different from retain_global_references (which reloads/writes).
    references = edge.upstream_references(host)
    from contracts import check_path
    path = check_path(host.paths.p("/var/lib/foundry/network/nginx-references.json"), "file")
    if path.exists():
        admission, _ = _feature_owners()
        value = admission.read_owned(host, path, lambda value: value)
        from firewall import boot_id
        if value["boot_id"] == boot_id(host):
            references += tuple(SocketIdentity(**v) for v in value["sockets"])
    for _, inv in history:
        references += tuple(SocketIdentity("tcp", ipaddress.ip_address(e.address).version, e.address, e.port)
                            for e in edge.retained_references(host, inv.profile.instance_id))
    roles = [("http", "http")]
    placed = dict(profile.placement)
    roles.extend((role.common.name, name) for role in topology.roles if isinstance(role, ExecutableRole)
                 and placed[role.common.name] for name, _ in role.listeners)
    retained = tuple(retained or ())
    require(all((e.role, e.listener) in roles for e in retained)
            and len({(e.role, e.listener) for e in retained}) == len(retained),
            "network_scope", "Retained endpoints must belong to the candidate declaration.")
    roles = [pair for pair in roles if pair not in {(e.role, e.listener) for e in retained}]
    endpoints = retained + allocate_private(profile.instance_id, roles, reserved,
                                 (*observed, *(ObservedSocket(s, ()) for s in references)))
    return endpoints, canonical_digest(_feature_projection(entry, topology, profile, endpoints))


def _feature_exit(host, inventory, units):
    """Observe systemd jobs, control processes, the full cgroup and admitted helpers."""
    admission, _ = _feature_owners()
    from contracts import HelperInvocation
    def manager(unit):
        output = host.run(["systemctl", "show", unit, "--property=Id,LoadState,ActiveState,MainPID,ControlPID,ControlGroup,Job",
                           "--no-pager"], timeout=5).stdout
        values = dict(line.split("=", 1) for line in output.splitlines() if "=" in line)
        require(values.get("Id") == unit and values.get("LoadState") == "loaded"
                and values.get("ActiveState") in ("inactive", "failed")
                and values.get("MainPID") == values.get("ControlPID") == "0" and values.get("Job") in ("", "0"),
                "network_backend_alive", "Workload, control-process or queued-job exit is unproven.")
        return values
    observations = {}
    for role, unit in units.items():
        before = manager(unit)
        group = before.get("ControlGroup")
        require(group is not None, "network_backend_alive", "Missing cgroup observation.")
        if group:
            require(group.startswith("/") and group != "/" and ".." not in group.split("/"), "network_backend_alive", "Invalid service cgroup.")
            root = admission.protected_path(host, host.paths.p("/sys/fs/cgroup" + group), "directory")
            paths = list(root.rglob("cgroup.procs"))
            require(root.is_dir() and paths and len(paths) <= 4096, "network_backend_alive", "Cgroup observation is incomplete.")
            for path in paths:
                require(not admission.protected_path(host, path, "file").read_text().strip(),
                        "network_backend_alive", "Workload descendants remain alive.")
        require(manager(unit) == before, "network_backend_alive", "Unit state changed during exit observation.")
        observations[role] = before
    store = admission.AdmissionStore(host, inventory.profile.instance_id)
    root = admission.protected_path(host, store.runtime / "admitted", "directory")
    receipts = list(root.glob("*/*.json")) if root.exists() else []
    require(len(receipts) <= 4096, "network_backend_alive", "Admitted invocation inventory exceeds its bound.")
    from firewall import boot_id
    for path in receipts:
        receipt = admission.read_owned(host, path, HelperInvocation.parse)
        require(receipt.unit.instance_id == inventory.profile.instance_id, "network_scope", "Foreign admitted invocation.")
        if receipt.unit.role in units and receipt.boot_id == boot_id(host):
            require(not admission.executor_alive(host, receipt.process), "network_backend_alive", "An admitted helper remains alive.")
    return observations


def _transition_scope(host, entry, transition, resolution, units):
    from contracts import HostTransition, RuntimeResolution
    transition = HostTransition.parse(record_document(transition))
    resolution = RuntimeResolution.parse(record_document(resolution))
    inventory = _feature_inventory(host, entry, resolution.id)
    old = _feature_inventory(host, entry, transition.old_resolution_id)
    affected = {u.role for u in transition.affected_units}
    all_units = {**old.units, **inventory.units}
    scoped = {name: unit for name, unit in all_units.items() if name in affected}
    require(transition.instance_id == inventory.profile.instance_id
            and transition.candidate_resolution_id == resolution.id
            and (transition.current_private_revision == old.resolution.private_revision or
                 "deployment_selection" not in entry and old.resolution.id == resolution.id
                 and transition.current_private_revision == entry["configuration_revision"])
            and entry["configuration_revision"] in (transition.current_private_revision, resolution.private_revision)
            and transition.plan_sha256 == resolution.deployment_sha256 and inventory.resolution == resolution
            and isinstance(units, dict) and units in (scoped, all_units)
            and affected == set(scoped)
            and all(u == (inventory.specs.get(u.role) or old.specs[u.role]).identity for u in transition.affected_units),
            "network_scope", "Closure requires the exact affected transition units.")
    retained = set(all_units) - affected
    require(retained <= old.roles.keys() & inventory.roles.keys(),
            "network_scope", "Added and removed roles must be affected by the transition.")
    for name in retained:
        require(old.specs[name] == inventory.specs[name]
                and tuple(e for e in old.resolution.endpoints if e.role == name) == tuple(e for e in resolution.endpoints if e.role == name)
                and tuple(e.sha256 for e in old.resolution.environments if e.role == name) == tuple(e.sha256 for e in resolution.environments if e.role == name)
                and tuple(c for c in public_claims(old.profile) if c.role == name) == tuple(c for c in public_claims(inventory.profile) if c.role == name)
                and {k: v for k, v in old.profile.listeners if k.split(".")[0] == name} ==
                    {k: v for k, v in inventory.profile.listeners if k.split(".")[0] == name},
                "network_retained_changed", "Retained roles must have identical effective inputs and exposure.")
    admission, _ = _feature_owners()
    store = admission.AdmissionStore(host, transition.instance_id)
    selection, selected = store._selection()
    require(selected == resolution and selection.admission.operation_id == transition.operation_id,
            "network_selection", "Closure is not for the selected operation.")
    return transition, inventory, old, scoped, retained


def _retained_state(host, inventory, names):
    from release import service
    import edge
    admission, _ = _feature_owners()
    result = {}
    root = host.paths.p(edge.RUNTIME + "/" + inventory.profile.instance_id)
    for name in sorted(names):
        observed = service(host, inventory.units[name])
        require(observed["ActiveState"] in ("active", "inactive", "failed"),
                "network_retained_changed", "Retained units must have stable process identities.")
        files = {path.name: admission.protected_path(host, path, "file").read_text()
                 for path in root.glob("*.conf") if (name == "http" and path.name == "http-open.conf")
                 or path.name.startswith("stream-" + name + ".")}
        result[name] = {"spec": record_document(inventory.specs[name]), "process": observed, "exposure": files}
    return result


def feature_withdraw(host, entry, transition, resolution, units):
    """Record scoped ingress withdrawal without claiming backend exit."""
    import edge
    import firewall
    import json
    from contracts import DeployError
    transition, inventory, old, scoped, retained = _transition_scope(host, entry, transition, resolution, units)
    resolution = inventory.resolution
    admission, _ = _feature_owners()
    _, history = _feature_history(host)
    claims = tuple(dict.fromkeys(c for inv in (old, inventory) for c in public_claims(inv.profile) if c.role in scoped))
    # Runtime exposure, not enablement alone, determines what must be preserved.
    before = _retained_state(host, old, retained)
    exposed = {name for name, observation in before.items() if observation["exposure"]}
    retained_claims = tuple(c for c in public_claims(old.profile) if c.role in exposed)
    request = record_document(replace(transition, closure_id=None))
    context = canonical_digest({"transition": request, "units": scoped,
                                "old": record_document(old.resolution), "candidate": record_document(resolution)})
    path = admission.protected_path(host, host.paths.p(f"/var/lib/foundry/network/{transition.instance_id}/withdrawal.json"), "file")
    previous = admission.read_owned(host, path, lambda value: value) if path.exists() else None
    boot = firewall.boot_id(host)
    same = previous and previous.get("context") == context and previous.get("boot_id") == boot
    if same:
        require(isinstance(previous.get("workers"), list) and 0 < len(previous["workers"]) <= 4096
                and all(isinstance(w, list) and len(w) == 2 and all(type(n) is int and n > 0 for n in w)
                        for w in previous["workers"]), "network_withdrawal", "Invalid retained worker identities.")
    workers = {tuple(w) for w in previous["workers"]} if same else set()
    references = set(tuple(r) for r in previous.get("references", [])) if same else set()
    if same and "retained" in previous:
        require(previous["retained"] == before, "network_retained_changed", "Retained units changed during the transition.")
    withdrawn = False
    if same and previous.get("state") == "complete":
        try:
            if "http" in scoped:
                _feature_http(host, entry, inventory, history, closed=True, write=False)
            firewall.observe(host, transition.instance_id, claims, retained_claims=retained_claims, bindings=old.profile.listeners)
            edge.observe_projection(host, transition.instance_id, scoped)
            withdrawn = True
        except DeployError as error:
            if error.code not in ("network_not_withdrawn", "firewall_drift", "nginx_projection_drift"):
                raise
    if not withdrawn:
        def record(state):
            require(0 < len(workers) <= 4096, "network_withdrawal", "Nginx worker observation exceeds its bound.")
            host.owned_write(path, json.dumps({"context": context, "boot_id": boot, "workers": sorted(workers),
                                              "state": state, "retained": before, "references": sorted(references)}) + "\n", mode=0o600)
        references.update((r.address, r.port) for r in edge.upstream_references(host))
        workers.update(edge._worker_identities(host))
        record("in_progress")
        firewall.withdraw(host, transition.instance_id, claims, retained_claims=retained_claims, bindings=old.profile.listeners)
        edge.retain_global_references(host)
        workers.update(edge._worker_identities(host))
        record("in_progress")
        if "http" in scoped:
            _feature_http(host, entry, inventory, history, closed=True)
        edge.withdraw(host, transition.instance_id, scoped)
        record("complete")
    require(_retained_state(host, inventory, retained) == before,
            "network_retained_changed", "Withdrawal changed a retained unit or route.")
    fw = firewall.observe(host, transition.instance_id, claims, retained_claims=retained_claims, bindings=old.profile.listeners)
    ng = edge.observe_projection(host, transition.instance_id, scoped)
    return ingress_evidence(transition.operation_id, boot, old.resolution, resolution,
                            transition.affected_units, canonical_digest(before), fw, ng, claims=claims)


def feature_close(host, entry, transition, resolution, units):
    """Prove scoped backend exit and reference isolation after withdrawing ingress."""
    import edge
    import time
    evidence = feature_withdraw(host, entry, transition, resolution, units)
    transition, inventory, old, scoped, retained = _transition_scope(host, entry, transition, resolution, units)
    resolution = inventory.resolution
    exits = _feature_exit(host, inventory, scoped)
    admission, _ = _feature_owners()
    path = host.paths.p(f"/var/lib/foundry/network/{transition.instance_id}/withdrawal.json")
    record = admission.read_owned(host, path, lambda value: value)
    references = edge.upstream_references(host)
    previous_workers = {tuple(w) for w in record["workers"]}
    candidate = tuple(e for e in resolution.endpoints if e.role in scoped)
    historical = tuple(e for e in old.resolution.endpoints if e.role in scoped)
    reuse = not retained or bool({(e.address, e.port) for e in candidate} &
                                ({(e.address, e.port) for e in historical} | {tuple(r) for r in record["references"]}))
    if reuse:
        deadline = time.monotonic() + 10
        while previous_workers & set(edge._worker_identities(host)) and time.monotonic() < deadline:
            time.sleep(.05)
        require(not previous_workers & set(edge._worker_identities(host)),
                "network_old_workers", "Old nginx sessions can still reach a reused backend.")
    else:
        # Old workers may keep serving retained roles. Their affected old addresses
        # stay reserved and cannot reach any freshly allocated candidate backend.
        references = (*references, *historical, *edge.retained_references(host, transition.instance_id))
    require(not any(conflicts(c.socket, s.socket) for c in private_claims(transition.instance_id, resolution) if c.role in scoped
                    for s in observe_sockets(host)), "network_backend_alive", "An affected candidate endpoint is already listening.")
    require(canonical_digest(_retained_state(host, inventory, retained)) == evidence.retained_units_sha256,
            "network_retained_changed", "Retained workloads changed before complete closure.")
    proof = canonical_digest({"transition": record_document(replace(transition, closure_id=None)), "boot_id": evidence.boot_id,
                              "units": exits, "retired_workers": sorted(previous_workers) if reuse else []})
    return complete_closure(evidence, resolution, backend_exit_sha256=proof, reserved=candidate, old_references=references)


def feature_open(host, entry, resolution, evidence):
    """Verify the enabled, unheld graph and open only the proven affected scope."""
    import edge
    import firewall
    import certificates
    import time
    from contracts import AdmissionDisposition, HelperInvocation, RuntimeResolution
    from release import READINESS_SECONDS
    resolution = RuntimeResolution.parse(record_document(resolution))
    evidence = ClosureEvidence.parse(record_document(evidence))
    admission, services = _feature_owners()
    inventory = _feature_inventory(host, entry, resolution.id)
    store = admission.AdmissionStore(host, inventory.profile.instance_id)
    affected = {u.role for u in evidence.units}
    retained_roles = set(inventory.roles) - affected
    selection, _ = store._selection()
    enabled = set(services.enabled_units(host, inventory.units)) - set(selection.maintenance_holds)
    configured_enabled = set(enabled)
    attempt = store._operations().get(selection.admission.operation_id, {})
    if selection.admission.disposition is AdmissionDisposition.PENDING and attempt.get("action") == "config" and "running_roles" in attempt:
        require(set(attempt["running_roles"]) <= enabled, "network_selection", "Enablement changed during configuration publication.")
        enabled = set(attempt["running_roles"])
    inventory.start_order(enabled)
    opening = enabled & affected
    retained = _retained_state(host, inventory, retained_roles)
    require(inventory.resolution == resolution and evidence.stage is ClosureStage.APPLICATION_CLOSED
            and evidence.boot_id == firewall.boot_id(host) and evidence.candidate_resolution_id == resolution.id
            and all(u.instance_id == inventory.profile.instance_id for u in evidence.units)
            and {e for e in resolution.endpoints if e.role in affected} <= set(evidence.endpoints)
            and canonical_digest(retained) == evidence.retained_units_sha256,
            "network_selection", "Opening requires current closure and unchanged retained units.")
    registry, history = _feature_history(host)
    projection = _feature_projection(entry, inventory.topology, inventory.profile, resolution.endpoints)
    require(canonical_digest(projection) == resolution.nginx_sha256, "network_projection", "Selected nginx projection digest differs.")
    exposed = {name for name, state in retained.items() if state["exposure"]}
    retained_claims = tuple(c for c in public_claims(inventory.profile) if c.role in exposed)
    claims = tuple(c for c in public_claims(inventory.profile) if c.role in opening)
    def authorize():
        deadline = time.monotonic() + READINESS_SECONDS
        selection, selected = store._selection()
        require(selected == resolution and selection.admission.operation_id == evidence.operation_id
                and configured_enabled == set(services.enabled_units(host, inventory.units)) - set(selection.maintenance_holds)
                and admission.read_owned(host, store.runtime / "boot.json", ClosureEvidence.parse) == evidence
                and canonical_digest(_retained_state(host, inventory, retained_roles)) == evidence.retained_units_sha256,
                "network_selection", "Opening authority or retained workloads changed.")
        owners = {}
        for role in sorted(enabled):
            unit = inventory.units[role]
            output = host.run(["systemctl", "show", unit, "--property=MainPID,InvocationID", "--no-pager"], timeout=5).stdout
            values = dict(line.split("=", 1) for line in output.splitlines() if "=" in line)
            invocation_id = values.get("InvocationID", "")
            require(re.fullmatch(r"[a-f0-9]{32}", invocation_id) is not None, "network_invocation", "Missing service invocation.")
            invocation = admission.read_owned(host, store.runtime / "admitted" / role / (invocation_id + ".json"), HelperInvocation.parse)
            require(invocation.unit == inventory.specs[role].identity
                    and invocation.unit_spec_sha256 == canonical_digest(record_document(inventory.specs[role]))
                    and str(invocation.process.pid) == values.get("MainPID") and admission.executor_alive(host, invocation.process),
                    "network_invocation", "The selected admitted process is not the running workload.")
            permit = None
            if selection.admission.disposition is AdmissionDisposition.PENDING and role in affected:
                permit = contract_uuid(admission.protected_path(host, store.runtime / "active-permit", "file").read_text())
            with store.preparation(invocation, permit_id=permit) as authorized:
                require(authorized == resolution, "network_readiness", "Selected workload admission is unverified.")
            owners[inventory.profile.instance_id, role] = process_members(host, unit)
        require(not enabled or services.wait_ready_set(host, inventory, enabled, deadline),
                "network_readiness", "The complete enabled set did not remain ready.")
        private = tuple(c for c in private_claims(inventory.profile.instance_id, resolution) if c.role in enabled)
        observed = tuple(s for s in observe_sockets(host) if any(conflicts(s.socket, claim.socket) for claim in private))
        require(not ownership_findings(private, observed, owners), "network_readiness", "Private listener ownership is unverified.")
        return True
    try:
        authorize()
        history = [inv for _, inv in history if inv.profile.instance_id == inventory.profile.instance_id]
        certificates.ensure_hostnames(host, registry, entry, tuple(inv.profile for inv in history),
                                     topologies=tuple(inv.topology for inv in history))
        if "http" in opening:
            _feature_http(host, entry, inventory, tuple((entry, inv) for inv in history), closed=False)
        closed = firewall.observe(host, inventory.profile.instance_id, claims,
                                  retained_claims=retained_claims, bindings=inventory.profile.listeners)
        edge.open_runtime(host, inventory.profile, resolution, entry["certificate"]["lineage"], opening,
                          topology=inventory.topology, authorize=authorize)
        if any(policy.tls == "edge" for name, policy in public_policies(inventory.profile, inventory.topology).items()
               if name.split(".")[0] in opening):
            certificates.verify_network_postcondition(host, entry)
        firewall.open_runtime(host, inventory.profile.instance_id, claims, inventory.profile.listeners,
                              closed_sha256=closed.sha256, authorize=authorize, retained_claims=retained_claims)
        authorize()
        expected = {name: value for name, value in projection.items() if name != "http"
                    and any(name.startswith("stream-" + role + ".") for role in opening)}
        if "http" in opening:
            expected["http-open.conf"] = '\"\" 1;\n'
        edge.observe_projection(host, inventory.profile.instance_id, opening, expected=expected)
        observed = tuple(s for s in observe_sockets(host) if any(conflicts(s.socket, claim.socket) for claim in claims))
        require(not ownership_findings(claims, observed, {"nginx": process_members(host, "nginx.service")}, edge_verified=True),
                "public_opening_unverified", "Public sockets do not belong to the selected nginx projection.")
        firewall.observe(host, inventory.profile.instance_id, claims, bindings=inventory.profile.listeners,
                         opened=True, retained_claims=retained_claims)
        return True
    except Exception:
        try:
            firewall.withdraw(host, inventory.profile.instance_id, claims,
                              retained_claims=retained_claims, bindings=inventory.profile.listeners)
        finally:
            edge.withdraw(host, inventory.profile.instance_id, opening)
        raise
