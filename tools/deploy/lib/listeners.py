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
