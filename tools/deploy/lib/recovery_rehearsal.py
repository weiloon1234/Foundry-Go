"""Feature recovery rehearsal: private endpoints and proven custom-role IP filters."""
from dataclasses import asdict
import ipaddress
import uuid

import admission
import network_confinement
from contracts import (ClosureEvidence, ClosureStage, canonical_digest, now, require)
from topology import EndpointBinding, ExecutableRole


def active(entry):
    return entry.get('recovered_from', {}).get('mode') == 'rehearsal' and entry.get('topology', {}).get('schema') == 2


def roles(topology, placed):
    declarations = {role.common.name: role for role in topology.roles}
    selected = set()
    def visit(name):
        if name in selected:
            return
        role = declarations[name]
        require(name in placed, 'recovery_rehearsal_dependency', f'Rehearsal dependency {name} is not placed on this host.')
        require(name == 'http' or isinstance(role, ExecutableRole) and role.recovery_rehearsal_safe,
                'recovery_rehearsal_dependency', f'HTTP rehearsal requires unsafe dependency {name}; declare an eligible custom role recovery_rehearsal_safe or remove the dependency.')
        selected.add(name)
        for dependency in role.common.requires:
            visit(dependency)
        for _, binding in role.common.bindings:
            if isinstance(binding, EndpointBinding):
                visit(binding.role)
    visit('http')
    return selected


def projection(entry, endpoints):
    return {'rehearsal': entry['profile']['instance_id'], 'private_endpoints': [asdict(e) for e in endpoints], 'public': []}


def observe(host, entry, inventory):
    import edge
    import listeners
    from contracts import PREFIX
    instance = inventory.profile.instance_id
    site = host.paths.p(f'/etc/nginx/sites-enabled/{PREFIX}-{entry["name"]}.conf')
    require(not site.exists() and not site.is_symlink(), 'rehearsal_exposure', 'Rehearsal must have no enabled public HTTP site.')
    route = host.paths.p(edge.RUNTIME + '/' + instance + '/http-open.conf')
    require(not route.exists(), 'rehearsal_exposure', 'Rehearsal HTTP admission must remain closed.')
    edge.observe_projection(host, instance, set(inventory.roles) - {'http'})
    references = edge.upstream_references(host)
    claims = listeners.private_claims(instance, inventory.resolution, inventory.topology)
    require(not any(listeners.conflicts(listeners.reservation_socket(c), r) for c in claims for r in references),
            'rehearsal_exposure', 'An nginx upstream can reach a rehearsal listener.')
    sockets = listeners.observe_sockets(host)
    for name, unit in inventory.units.items():
        from release import service
        if service(host, unit)['ActiveState'] != 'active':
            continue
        owners = listeners.process_members(host, unit)
        role = inventory.roles[name.role]
        if role.common.confinement == "loopback":
            network_confinement.isolation(host, inventory, name)
        require(all(ipaddress.ip_address(s.socket.address).is_loopback or
                    (role.common.confinement == "loopback" and set(s.pids) <= owners and
                     any(c.identity == name and c.wildcard and listeners.matches(c, s.socket) for c in claims))
                    for s in sockets if owners & set(s.pids)),
                'rehearsal_exposure', f'Rehearsal role {name.role} copy {name.ordinal} has a non-loopback listener.')
    return canonical_digest({'projection': projection(entry, inventory.resolution.endpoints), 'references': [asdict(r) for r in references]})


def withdraw(host, entry, transition, resolution, units):
    import listeners
    transition, inventory, old, scoped, retained = listeners._transition_scope(host, entry, transition, resolution, units)
    require(not retained, 'rehearsal_scope', 'Recovery rehearsal requires whole-graph closure.')
    proof = observe(host, entry, inventory)
    endpoints = tuple(dict.fromkeys((*old.resolution.endpoints, *resolution.endpoints)))
    return ClosureEvidence(str(uuid.uuid4()), transition.operation_id, admission.boot_id(host), old.resolution.id,
                           resolution.id, transition.affected_units, endpoints, canonical_digest({}), proof,
                           None, None, now(), ClosureStage.INGRESS_WITHDRAWN)


def close(host, entry, transition, resolution, units):
    import listeners
    evidence = withdraw(host, entry, transition, resolution, units)
    inventory = listeners._feature_inventory(host, entry, resolution.id)
    exits = listeners._feature_exit(host, inventory, units)
    require(not any(listeners.conflicts(listeners.reservation_socket(c), s.socket) for c in
                    listeners.private_claims(inventory.profile.instance_id, resolution, inventory.topology)
                    for s in listeners.observe_sockets(host)), 'network_backend_alive', 'A rehearsal backend is still listening.')
    return listeners.complete_closure(evidence, resolution, backend_exit_sha256=canonical_digest(exits),
                                     reserved=resolution.endpoints, old_references=())
