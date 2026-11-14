"""Feature recovery rehearsal: private endpoints and proven custom-role IP filters."""
import ctypes
from dataclasses import asdict
import ipaddress
import os
import platform
import uuid

import admission
from contracts import (ClosureEvidence, ClosureStage, canonical_digest, now, record_document, require)
from topology import EndpointBinding, ExecutableRole

FILTER = 'IPAddressDeny=any\nIPAddressAllow=localhost\n'


def active(entry):
    return entry.get('recovered_from', {}).get('mode') == 'rehearsal' and entry.get('topology', {}).get('schema') == 2


def roles(topology, placed):
    declarations = {role.common.name: role for role in topology.roles}
    selected = set()
    def visit(name):
        if name in selected:
            return
        role = declarations[name]
        require(name in placed, 'recovery_dependency', f'Rehearsal dependency {name} is not placed on this host.')
        require(name == 'http' or isinstance(role, ExecutableRole) and role.recovery_rehearsal_safe,
                'recovery_dependency', f'HTTP rehearsal requires unsafe dependency {name}; declare an eligible custom role recovery_rehearsal_safe or remove the dependency.')
        selected.add(name)
        for dependency in role.common.requires:
            visit(dependency)
        for _, binding in role.common.bindings:
            if isinstance(binding, EndpointBinding):
                visit(binding.role)
    visit('http')
    return selected


def attached_programs(host, group):
    """BPF_PROG_QUERY proves both filters are attached to this actual cgroup.

    No feature/build flag alone establishes kernel permission or attachment.
    Linux uapi: bpf.h BPF_PROG_QUERY, BPF_CGROUP_INET_{INGRESS,EGRESS}.
    """
    require(group.startswith('/') and group != '/' and '..' not in group.split('/'),
            'rehearsal_isolation', 'Rehearsal requires an observable cgroup v2 unit.')
    require(host.paths.p('/sys/fs/cgroup/cgroup.controllers').is_file(),
            'rehearsal_isolation', 'Rehearsal requires cgroup v2 BPF IP filtering.')
    syscall = {'x86_64': 321, 'aarch64': 280}.get(platform.machine())
    require(syscall is not None, 'rehearsal_isolation', 'BPF observation is unsupported on this architecture.')
    class Query(ctypes.Structure):
        _fields_ = [('target_fd', ctypes.c_uint32), ('attach_type', ctypes.c_uint32),
                    ('query_flags', ctypes.c_uint32), ('attach_flags', ctypes.c_uint32),
                    ('prog_ids', ctypes.c_uint64), ('prog_cnt', ctypes.c_uint32)]
    libc = ctypes.CDLL(None, use_errno=True)
    result = []
    fd = os.open(host.paths.p('/sys/fs/cgroup' + group), os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        for direction in (0, 1):
            ids = (ctypes.c_uint32 * 64)()
            query = Query(fd, direction, 0, 0, ctypes.addressof(ids), len(ids))
            status = libc.syscall(ctypes.c_long(syscall), ctypes.c_int(16), ctypes.byref(query), ctypes.c_uint(ctypes.sizeof(query)))
            require(status == 0 and 0 < query.prog_cnt <= len(ids), 'rehearsal_isolation',
                    'Cannot prove attached cgroup BPF ingress and egress filtering; rehearsal refused.')
            result.append(list(ids[:query.prog_cnt]))
    finally:
        os.close(fd)
    return result


def isolation(host, inventory, name):
    properties = 'IPAddressDeny,IPAddressAllow,ControlGroup,DropInPaths'
    output = host.run(['systemctl', 'show', inventory.units[name], '--property=' + properties, '--no-pager'], timeout=5).stdout
    values = dict(line.split('=', 1) for line in output.splitlines() if '=' in line)
    require(set(values.get('IPAddressDeny', '').split()) == {'0.0.0.0/0', '::/0'} and
            set(values.get('IPAddressAllow', '').split()) == {'127.0.0.0/8', '::1/128'} and
            values.get('DropInPaths') == '', 'rehearsal_isolation',
            f'Role {name} lacks the exact loaded loopback-only IP filtering policy; rehearsal refused.')
    return {'properties': values, 'programs': attached_programs(host, values.get('ControlGroup', ''))}


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
    require(not {(e.address, e.port) for e in inventory.resolution.endpoints} & {(r.address, r.port) for r in references},
            'rehearsal_exposure', 'An nginx upstream can reach a rehearsal listener.')
    sockets = listeners.observe_sockets(host)
    for name, unit in inventory.units.items():
        from release import service
        if service(host, unit)['ActiveState'] != 'active':
            continue
        owners = listeners.process_members(host, unit)
        require(all(ipaddress.ip_address(s.socket.address).is_loopback for s in sockets if owners & set(s.pids)),
                'rehearsal_exposure', f'Rehearsal role {name} has a non-loopback listener.')
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
    require(not any(listeners.conflicts(c.socket, s.socket) for c in listeners.private_claims(inventory.profile.instance_id, resolution)
                    for s in listeners.observe_sockets(host)), 'network_backend_alive', 'A rehearsal backend is still listening.')
    return listeners.complete_closure(evidence, resolution, backend_exit_sha256=canonical_digest(exits),
                                     reserved=resolution.endpoints, old_references=())
