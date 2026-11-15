"""Loaded systemd loopback policy and live cgroup BPF attachment proof."""
import ctypes
import os
import platform

from contracts import DeployError, require

FILTER = 'IPAddressDeny=any\nIPAddressAllow=localhost\n'


def attached_programs(host, group):
    """BPF_PROG_QUERY proves both filters are attached to this actual cgroup.

    No feature/build flag alone establishes kernel permission or attachment.
    Linux uapi: bpf.h BPF_PROG_QUERY, BPF_CGROUP_INET_{INGRESS,EGRESS}.
    """
    require(group.startswith('/') and group != '/' and '..' not in group.split('/'),
            'network_confinement', 'Loopback confinement requires an observable cgroup v2 unit.')
    require(host.paths.p('/sys/fs/cgroup/cgroup.controllers').is_file(),
            'network_confinement', 'Loopback confinement requires cgroup v2 BPF IP filtering.')
    syscall = {'x86_64': 321, 'aarch64': 280}.get(platform.machine())
    require(syscall is not None, 'network_confinement', 'BPF observation is unsupported on this architecture.')
    class Query(ctypes.Structure):
        _fields_ = [('target_fd', ctypes.c_uint32), ('attach_type', ctypes.c_uint32),
                    ('query_flags', ctypes.c_uint32), ('attach_flags', ctypes.c_uint32),
                    ('prog_ids', ctypes.c_uint64), ('prog_cnt', ctypes.c_uint32)]
    libc = ctypes.CDLL(None, use_errno=True)
    result = []
    try:
        fd = os.open(host.paths.p('/sys/fs/cgroup' + group), os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    except OSError:
        raise DeployError('network_confinement', 'Cannot inspect the unit cgroup for BPF IP filtering; start refused.') from None
    try:
        for direction in (0, 1):
            ids = (ctypes.c_uint32 * 64)()
            query = Query(fd, direction, 0, 0, ctypes.addressof(ids), len(ids))
            status = libc.syscall(ctypes.c_long(syscall), ctypes.c_int(16), ctypes.byref(query), ctypes.c_uint(ctypes.sizeof(query)))
            require(status == 0 and 0 < query.prog_cnt <= len(ids), 'network_confinement',
                    'Cannot prove attached cgroup BPF ingress and egress filtering; start refused.')
            result.append(list(ids[:query.prog_cnt]))
    finally:
        os.close(fd)
    return result


def isolation(host, inventory, identity):
    properties = 'IPAddressDeny,IPAddressAllow,ControlGroup,DropInPaths'
    output = host.run(['systemctl', 'show', inventory.units[identity], '--property=' + properties, '--no-pager'], timeout=5).stdout
    values = dict(line.split('=', 1) for line in output.splitlines() if '=' in line)
    require(set(values.get('IPAddressDeny', '').split()) == {'0.0.0.0/0', '::/0'} and
            set(values.get('IPAddressAllow', '').split()) == {'127.0.0.0/8', '::1/128'} and
            values.get('DropInPaths') == '', 'network_confinement',
            f'Role {identity.role} lacks the exact loaded loopback-only IP filtering policy; start refused.')
    require(values.get('ControlGroup', '').endswith('/' + inventory.units[identity]),
            'network_confinement', f'Role {identity.role} has no matching live unit cgroup; start refused.')
    return {'properties': values, 'programs': attached_programs(host, values.get('ControlGroup', ''))}

