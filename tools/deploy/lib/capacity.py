"""RAM sizing and explicit, journaled changes to the owned database services."""
from __future__ import annotations

import copy
import hashlib
import json
import re

from contracts import DeployError, check_path, now, require

MINIMUM_MEMORY_MIB = 1536
MIB = 1024 * 1024


def memory_mib(host):
    try:
        text = check_path(host.paths.p('/proc/meminfo'), 'file').read_text()
        match = re.search(r'^MemTotal:\s+(\d+) kB$', text, re.M)
        require(match is not None and int(match[1]) > 0, 'host_capacity',
                'Cannot read MemTotal; host memory must be inspectable before allocation.')
        available = int(match[1]) * 1024
    except (OSError, UnicodeError):
        raise DeployError('host_capacity', 'Cannot read MemTotal; host memory must be inspectable before allocation.') from None
    limit = check_path(host.paths.p('/sys/fs/cgroup/memory.max'), 'file')
    if limit.exists():
        try:
            value = limit.read_text().strip()
        except (OSError, UnicodeError):
            raise DeployError('host_capacity', 'Cannot read the cgroup memory limit.') from None
        require(value == 'max' or value.isdecimal(), 'host_capacity', 'Cannot interpret the cgroup memory limit.')
        if value != 'max':
            available = min(available, int(value))
    return available // MIB


def allocation_memory(host, code):
    memory = memory_mib(host)
    require(memory >= MINIMUM_MEMORY_MIB, code,
            f'At least {MINIMUM_MEMORY_MIB} MiB available memory is required by Foundry services.')
    return memory


def _memory(value):
    for unit, divisor in (('TB', 1024 * 1024), ('GB', 1024), ('MB', 1)):
        if value % divisor == 0:
            return str(value // divisor) + unit


def postgres(memory):
    shared = min(memory // 256 * 64, 8192)
    connections = max(40, min(memory // 40, 200))
    work = max(4, min(memory // (8 * connections), 16))
    maintenance = max(64, min(memory // 256 * 16, 1024))
    return {'shared_buffers': _memory(shared), 'effective_cache_size': _memory(memory // 128 * 64),
            'max_connections': connections, 'work_mem': _memory(work),
            'maintenance_work_mem': _memory(maintenance),
            'budget_mib': shared + maintenance + connections * work}


def redis(memory):
    maximum = max(128, min(memory // 512 * 64, 4096)) * MIB
    return {'maxmemory_bytes': maximum, 'budget_bytes': 2 * maximum, 'maxclients': 256}


def _mib(value):
    match = re.fullmatch(r'([1-9][0-9]*)(MB|GB|TB)', value) if isinstance(value, str) else None
    if not match:
        return None
    number = int(match[1]) * {'MB': 1, 'GB': 1024, 'TB': 1024 * 1024}[match[2]]
    return number if _memory(number) == value else None


def postgres_valid(value):
    if not isinstance(value, dict):
        return False
    keys = {'budget_mib', 'max_connections', 'shared_buffers', 'work_mem', 'maintenance_work_mem'}
    if set(value) not in (keys, keys | {'effective_cache_size'}):
        return False
    shared, work, maintenance = (_mib(value[key]) for key in ('shared_buffers', 'work_mem', 'maintenance_work_mem'))
    connections, budget = value['max_connections'], value['budget_mib']
    if not (shared and 128 <= shared <= 8192 and shared % 64 == 0
            and work and 4 <= work <= 16 and maintenance and 64 <= maintenance <= 1024 and maintenance % 16 == 0
            and type(connections) is int and 40 <= connections <= 200 and type(budget) is int
            and shared + maintenance + connections * work <= budget <= 8192 + 1024 + 200 * 16):
        return False
    cache = _mib(value['effective_cache_size']) if 'effective_cache_size' in value else None
    return 'effective_cache_size' not in value or bool(cache and shared <= cache <= 16777215 and cache % 64 == 0)


def redis_valid(value):
    return (isinstance(value, dict) and set(value) == {'maxmemory_bytes', 'budget_bytes', 'maxclients'}
            and type(value['maxmemory_bytes']) is int and 128 * MIB <= value['maxmemory_bytes'] <= 4096 * MIB
            and value['maxmemory_bytes'] % (64 * MIB) == 0 and type(value['budget_bytes']) is int
            and value['budget_bytes'] == 2 * value['maxmemory_bytes']
            and type(value['maxclients']) is int and value['maxclients'] == 256)


def _hash(value):
    return hashlib.sha256(json.dumps(value, sort_keys=True, separators=(',', ':')).encode()).hexdigest()


def _load(host):
    import postgres as pg
    return pg._read_private(host, pg._private(host, 'capacity-transition.json'))


def _save(host, journal):
    import postgres as pg
    pg._write_private(host, pg._private(host, 'capacity-transition.json'), journal)


def require_idle(host):
    journal = _load(host)
    require(not journal or journal['phase'] == 'complete', 'capacity_pending',
            'Resume ACTION=capacity before other mutations; its approved sizes remain pinned.')


def _eligible(host, registry):
    import recover
    import release
    recovery = recover._load(host)
    require(not recovery or recovery.get('phase') == 'complete', 'recovery_pending',
            'Finish the recorded recovery before changing capacity.')
    require(not registry['tools']['upgrade_operation'], 'tools_upgrade_pending', 'Finish the tool upgrade before changing capacity.')
    states = {}
    for key, entry in registry['instances'].items():
        require(entry.get('recovered_from', {}).get('mode') != 'rehearsal', 'rehearsal_read_only',
                'Rehearsals cannot change capacity; recreate the server for takeover.')
        require(entry['provisioning']['state'] == 'ready', 'capacity_not_ready', 'Finish provisioning before changing capacity.')
        state = host.load_state(entry)
        release.require_idle(state)
        require(state['status'] != 'deploying', 'reconciliation_required', 'Finish the deployment before changing capacity.')
        states[key] = state
    return states


def _inspect(host, registry, target=None):
    import postgres as pg
    import redis_acl
    pg.inspect(host, registry, capacity_target=target['postgresql'] if target else None)
    observation = redis_acl.inspect(host, registry, capacity_target=target['redis'] if target else None)
    require(registry['shared']['postgresql']['state'] == registry['shared']['redis']['state'] == 'ready'
            and (target is not None or observation['state'] == 'ready'),
            'capacity_not_ready', 'Capacity requires allocated, ready PostgreSQL and Redis services.')
    if target is None:
        redis_acl._verify_runtime(host, registry['shared']['redis']['capacity'])


def plan(host):
    registry = host.read_registry()
    states = _eligible(host, registry)
    journal = _load(host)
    if journal and journal['phase'] != 'complete':
        require(journal['registry']['host_id'] == registry['host_id'], 'capacity_conflict', 'Capacity journal host identity differs.')
        return copy.deepcopy(journal['plan'])
    _inspect(host, registry)
    memory = allocation_memory(host, 'postgres_capacity')
    current = {name: registry['shared'][name]['capacity'] for name in ('postgresql', 'redis')}
    sized = {'postgresql': postgres(memory), 'redis': redis(memory)}
    restarts = [registry['shared'][name]['unit'] for name in current if current[name] != sized[name]]
    value = {'host_id': registry['host_id'], 'memory_mib': memory, 'current': current, 'sized': sized,
             'restart_units': restarts, 'noop': not restarts,
             'ready_instances': sorted(key for key, state in states.items() if state['status'] in ('healthy', 'rolled_back')),
             'message': 'Capacity already matches; apply is a no-op.' if not restarts else
                        'PostgreSQL and/or Redis will restart as listed; the application sees a brief interruption.',
             'binding': _hash({'registry': registry, 'states': states})}
    value['plan_token'] = _hash(value)
    return value


RESTART_RETRY = ('Rerun ACTION=capacity. If restarting keeps failing, recreate the server with ACTION=recover.')


def _unit_state(host, unit):
    result = host.run(['systemctl', 'show', unit, '--property=ActiveState,SubState,InvocationID,Job'])
    value = dict(line.split('=', 1) for line in result.stdout.splitlines() if '=' in line)
    require(value.get('Job') in ('', '0'), 'capacity_restart_pending',
            'A systemd job is still pending. Wait and rerun ACTION=capacity.')
    return value


def _running_invocation(value):
    return (value.get('ActiveState') == 'active' and value.get('SubState') == 'running'
            and re.fullmatch(r'[0-9a-f]{32}', value.get('InvocationID', '')))


def _restart(host, unit, previous_invocation):
    value = _unit_state(host, unit)
    if not (_running_invocation(value) and value['InvocationID'] != previous_invocation):
        before_restart = value.get('InvocationID', '')
        try:
            host.run(['systemctl', 'restart', unit], timeout=120)
        except (DeployError, OSError):
            raise DeployError('capacity_restart_failed', RESTART_RETRY) from None
        value = _unit_state(host, unit)
        require(value.get('InvocationID') != before_restart, 'capacity_restart_failed', RESTART_RETRY)
    require(_running_invocation(value) and value['InvocationID'] != previous_invocation,
            'capacity_restart_failed', RESTART_RETRY)


def _backups_idle(host):
    import release
    for kind in ('full', 'diff'):
        unit = 'foundry-backup-' + kind + '-cluster.service'
        state = release.service(host, unit)['ActiveState']
        require(state in ('inactive', 'failed'), 'capacity_backup_running',
                'A scheduled backup is running or not settled. Wait for it to finish and rerun ACTION=capacity.')


def _desired(journal):
    import redis_acl
    registry = copy.deepcopy(journal['registry'])
    registry['revision'] += 1
    registry['updated_at'] = journal['updated_at']
    pg_claim, redis_claim = copy.deepcopy(journal['postgres']), copy.deepcopy(journal['redis'])
    for name, sized in journal['plan']['sized'].items():
        registry['shared'][name]['capacity'] = copy.deepcopy(sized)
    pg_claim['allocation']['capacity'] = copy.deepcopy(journal['plan']['sized']['postgresql'])
    redis_claim['allocation']['capacity'] = copy.deepcopy(journal['plan']['sized']['redis'])
    redis_claim['files'][redis_acl.REDIS_CONFIG] = hashlib.sha256(redis_acl._redis_config(
        redis_claim['ownership_token'], journal['plan']['sized']['redis']).encode()).hexdigest()
    return registry, pg_claim, redis_claim


def _bound(host, journal):
    import postgres as pg
    import redis_acl
    registry = host.read_registry()
    desired, pg_new, redis_new = _desired(journal)
    committing = journal['phase'] == 'committing'
    require(registry == journal['registry'] or (committing and registry == desired),
            'capacity_conflict', 'Host allocation changed outside the capacity transition.')
    pg_claim = pg._cluster_journal(host, registry)
    _, redis_claim = redis_acl._service_claim(host, registry)
    require((pg_claim == journal['postgres'] or (committing and pg_claim == pg_new))
            and (redis_claim == journal['redis'] or (committing and redis_claim == redis_new)),
            'capacity_conflict', 'Service ownership journals changed outside the capacity transition.')
    require(_hash(_eligible(host, registry)) == journal['states'], 'capacity_conflict',
            'Instance deployment state changed during the capacity transition.')


def _verify(host, registry, name):
    import postgres as pg
    import redis_acl
    if name == 'postgresql':
        pg._verify_settings(host, registry)
    else:
        redis_acl._verify_runtime(host, registry['shared']['redis']['capacity'])


def apply(host, plan_token):
    import postgres as pg
    import redis_acl
    import release
    require(isinstance(plan_token, str) and re.fullmatch(r'[0-9a-f]{64}', plan_token),
            'plan_stale', 'Review capacity-plan and supply its plan token.')
    previous = _load(host)
    if not previous or (previous['phase'] == 'complete' and previous['plan']['plan_token'] != plan_token):
        # Scheduled backups hold the host lock too; refuse before waiting on it.
        _backups_idle(host)
    with host.lock('host', capacity_transition=True):
        journal = _load(host)
        registry = host.read_registry()
        states = _eligible(host, registry)
        if journal and journal['phase'] == 'complete' and journal['plan']['plan_token'] == plan_token:
            require(journal['registry']['host_id'] == registry['host_id'], 'capacity_conflict', 'Capacity host identity differs.')
            return journal['result']
        if not journal or journal['phase'] == 'complete':
            approved = plan(host)
            require(approved['plan_token'] == plan_token, 'plan_stale', 'Capacity plan changed; review a new plan.')
            _backups_idle(host)
            if approved['noop']:
                return {'ok': True, 'noop': True, 'capacity': approved['sized']}
            _, redis_claim = redis_acl._service_claim(host, registry)
            journal = {'plan': approved, 'registry': registry, 'states': _hash(states),
                       'postgres': pg._cluster_journal(host, registry), 'redis': redis_claim,
                       'updated_at': now(), 'phase': 'services', 'services': {name: {'phase': 'pending'} for name in approved['current']}}
            require(journal['postgres'].get('archive_transition', {}).get('phase') != 'publishing'
                    and journal['postgres'].get('hba', {}).get('phase') != 'publishing',
                    'capacity_not_ready', 'Finish pending PostgreSQL configuration publication before changing capacity.')
            _save(host, journal)
        require(journal['plan']['plan_token'] == plan_token, 'plan_stale', 'Resume with the recorded capacity plan token.')
        _bound(host, journal)
        desired, pg_new, redis_new = _desired(journal)
        if journal['phase'] == 'services':
            _inspect(host, registry, journal['plan']['sized'])
            for name, step in journal['services'].items():
                if journal['plan']['current'][name] == journal['plan']['sized'][name]:
                    continue
                allocation = desired['shared'][name]
                if step['phase'] == 'pending':
                    step['phase'] = 'publishing'
                    _save(host, journal)
                if step['phase'] == 'publishing':
                    if name == 'postgresql':
                        host.owned_write(host.paths.p(allocation['config_path']) / 'conf.d/foundry.conf',
                                         pg._configuration(allocation), owner=pg.cluster_owner(), group=pg.cluster_owner())
                    else:
                        host.owned_write(host.paths.p(redis_acl.REDIS_CONFIG),
                                         redis_acl._redis_config(allocation['ownership_token'], allocation['capacity']),
                                         mode=0o640, group=redis_acl.REDIS_ACCOUNT)
                    step.update(phase='restarting', previous_invocation=_unit_state(host, allocation['unit']).get('InvocationID', ''))
                    _save(host, journal)
                if step['phase'] == 'restarting':
                    _restart(host, allocation['unit'], step['previous_invocation'])
                    _verify(host, desired, name)
                    step['phase'] = 'verified'
                    _save(host, journal)
            journal['phase'] = 'readiness'
            _save(host, journal)
        if journal['phase'] == 'readiness':
            _inspect(host, registry, journal['plan']['sized'])
            for name in journal['services']:
                require(_running_invocation(_unit_state(host, desired['shared'][name]['unit'])),
                        'capacity_restart_failed', RESTART_RETRY)
                _verify(host, desired, name)
            for key in journal['plan']['ready_instances']:
                require(release.ready(host, registry['instances'][key]), 'capacity_not_ready',
                        'An instance is not ready after resizing. Rerun ACTION=capacity; if it keeps failing, recreate the server with ACTION=recover.')
            journal['phase'] = 'committing'
            _save(host, journal)
        if journal['phase'] == 'committing':
            # Each write is recoverable against the old/new snapshots above. No
            # ordinary mutation may enter while the transition is incomplete.
            pg._save_cluster(host, pg_new)
            redis_acl._save_claim(host, redis_new)
            host.write_registry(desired)
            _inspect(host, desired)
            for key in journal['plan']['ready_instances']:
                require(release.ready(host, desired['instances'][key]), 'capacity_not_ready', 'Instance readiness must verify before capacity is acknowledged.')
            journal['result'] = {'ok': True, 'noop': False, 'capacity': journal['plan']['sized'],
                                 'restart_units': journal['plan']['restart_units']}
            journal['phase'] = 'complete'
            _save(host, journal)
        return journal['result']
