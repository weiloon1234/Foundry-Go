"""Cutover boundary validation and private immutable intent storage.

The release module owns orchestration. M1 owns key/policy derivation; M3 owns
snapshot bytes. This module validates their wire contracts without reproducing them.
"""
import copy
import hashlib
import json
from pathlib import Path
import re
import stat

from contracts import (DeployError, MAX_JSON, REDIS_PORT, check_path, instance_paths,
                       parse_json, read_json, require, valid_timestamp, valid_uuid, VERSION_PATTERN)
import redis_acl

BLOCKERS = frozenset(('opaque_job_provider', 'invalid_configuration', 'inventory_limit',
                     'redis_unavailable', 'layout_1_requires_upgrade', 'unsupported_layout',
                     'queue_policy_mismatch', 'invalid_queue_state', 'layout_inspection_unsupported'))
ATTESTATION = 'Only this instance\'s managed topology roles use its job namespaces; there are no external clients.'
WARNING = 'Full downtime; about one minute is a target. After migration may have run, old-release rollback requires reviewed recovery.'


def sha(value):
    return hashlib.sha256(json.dumps(value, sort_keys=True, separators=(',', ':'), ensure_ascii=True,
                                     allow_nan=False).encode()).hexdigest()


def hash_value(value):
    return isinstance(value, str) and re.fullmatch('[a-f0-9]{64}', value) is not None


def name(value):
    return isinstance(value, str) and re.fullmatch('[a-z0-9][a-z0-9_.-]{0,127}', value) is not None


def fields(value, keys, code='cutover_inventory'):
    require(isinstance(value, dict) and set(value) == set(keys.split()), code, 'Invalid cutover boundary fields.')


def inventory(payload, returncode=0):
    require(isinstance(payload, str) and len(payload.encode()) <= MAX_JSON,
            'cutover_inventory', 'Queue inspection exceeds its output bound.')
    value = parse_json(payload)
    fields(value, 'schema supported_layout coverage connections blockers')
    require(type(value['schema']) is int and value['schema'] == 1
            and type(value['supported_layout']) is int and value['supported_layout'] == 3
            and value['coverage'] == 'declared' and isinstance(value['connections'], list)
            and len(value['connections']) <= 64 and isinstance(value['blockers'], list)
            and len(value['blockers']) <= 4096, 'cutover_inventory', 'Unsupported queue inspection contract.')
    names, total = set(), 0
    for connection in value['connections']:
        fields(connection, 'connection redis_connection storage_id namespace prefix policy_id queues')
        require(name(connection['connection']) and name(connection['redis_connection'])
                and connection['connection'] not in names and hash_value(connection['storage_id'])
                and hash_value(connection['policy_id']), 'cutover_inventory', 'Invalid connection identity.')
        names.add(connection['connection'])
        fields(connection['namespace'], 'application environment')
        require(all(isinstance(v, str) and 0 < len(v) <= 128 for v in connection['namespace'].values())
                and isinstance(connection['prefix'], str) and 0 < len(connection['prefix']) <= 512
                and isinstance(connection['queues'], list) and 0 < len(connection['queues']) <= 4096,
                'cutover_inventory', 'Invalid queue namespace.')
        queues = set()
        for queue in connection['queues']:
            fields(queue, 'queue base keys layout eligible')
            total += 1
            require(total <= 4096 and name(queue['queue']) and queue['queue'] not in queues
                    and isinstance(queue['base'], str) and queue['base'].startswith(connection['prefix'])
                    and len(queue['base']) <= 1024 and isinstance(queue['keys'], list)
                    and 0 < len(queue['keys']) <= 64 and queue['layout'] in ('absent', '1', '2', '3', 'unknown')
                    and type(queue['eligible']) is bool, 'cutover_inventory', 'Invalid queue descriptor.')
            queues.add(queue['queue'])
            require(all(isinstance(key, str) and key.startswith(queue['base'] + ':')
                        and len(key) <= 1152 and not redis_acl.REDIS_GLOB.search(key) for key in queue['keys'])
                    and queue['keys'] == sorted(set(queue['keys'])), 'cutover_inventory', 'Invalid physical queue keys.')
        require([q['queue'] for q in connection['queues']] == sorted(queues),
                'cutover_inventory', 'Queue inventory must be canonical.')
    require([c['connection'] for c in value['connections']] == sorted(names),
            'cutover_inventory', 'Connection inventory must be canonical.')
    seen = set()
    for blocker in value['blockers']:
        fields(blocker, 'code connection queue')
        require(blocker['code'] in BLOCKERS and (blocker['connection'] is None or name(blocker['connection']))
                and (blocker['queue'] is None or name(blocker['queue']) and blocker['connection'] is not None),
                'cutover_inventory', 'Invalid queue blocker.')
        identity = tuple(blocker[k] for k in ('code', 'connection', 'queue'))
        require(identity not in seen, 'cutover_inventory', 'Duplicate queue blocker.')
        seen.add(identity)
    require(returncode == 0 and not value['blockers']
            and all(q['eligible'] and q['layout'] in ('absent', '2', '3') for c in value['connections'] for q in c['queues']),
            'cutover_inventory_blocked', 'Queue inspection is incomplete or reports a blocker; no clients were stopped.')
    return value


def inventory_identity(value):
    identity = copy.deepcopy(value)
    identity.pop('blockers')
    for connection in identity['connections']:
        for queue in connection['queues']:
            queue.pop('layout')
            queue.pop('eligible')
    return sha(identity)


def targets(value):
    result = {}
    for connection in value['connections']:
        for queue in connection['queues']:
            key = (connection['storage_id'], queue['base'])
            item = {'connection': connection['connection'], 'queue': queue['queue'], 'layout': queue['layout'],
                    'storage_id': connection['storage_id'], 'base': queue['base'], 'keys': queue['keys'],
                    'prefix': connection['prefix'], 'policy_id': connection['policy_id']}
            if key in result:
                prior = result[key]
                require(all(prior[k] == item[k] for k in ('keys', 'policy_id', 'layout')),
                        'cutover_inventory', 'Aliased queues disagree about their physical contract.')
            else:
                result[key] = item
    return [result[key] for key in sorted(result)]


def scopes(value):
    found = {}
    for target in targets(value):
        key = (target['storage_id'], target['prefix'])
        found.setdefault(key, set()).update(target['keys'])
    return [{'storage_id': key[0], 'prefix': key[1], 'keys': sorted(found[key])} for key in sorted(found)]


def coverage(host, registry, entry, value):
    spaces = scopes(value)
    if not spaces:
        return []
    observed = redis_acl.inspect(host, registry)
    require(observed.get('owner') == 'foundry' and observed.get('state') == 'ready',
            'cutover_redis_ownership', 'Only the owned local Redis allocation can be cut over.')
    expected = sha(['localhost', REDIS_PORT, 0, entry['allocations']['redis_user'], True])
    for scope in spaces:
        redis_acl._validate_prefix(entry, scope['prefix'])
        require(scope['prefix'].startswith('foundry:jobs:') and scope['storage_id'] == expected
                and scope['prefix'] in entry.get('redis_grants', {}).get('keys', []),
                'cutover_redis_ownership', 'Queue storage or grants differ from the owned allocation.')
        for other in registry['instances'].values():
            if other['profile']['instance_id'] != entry['profile']['instance_id']:
                require(not any(p.startswith(scope['prefix']) or scope['prefix'].startswith(p)
                                for p in other.get('redis_grants', {}).get('keys', [])),
                        'cutover_redis_ownership', 'A queue scope is shared with another instance.')
        cursor, seen, allowed = '0', set(), set(scope['keys'])
        for _ in range(10000):
            page = redis_acl._redis(host, 'SCAN', cursor, 'MATCH', scope['prefix'] + '*', 'COUNT', '256')
            require(isinstance(page, list) and len(page) == 2 and isinstance(page[0], str)
                    and page[0].isdigit() and isinstance(page[1], list) and len(page[1]) <= 65536,
                    'cutover_coverage', 'Redis coverage scan is incomplete.')
            cursor = page[0]
            for key in page[1]:
                require(isinstance(key, str) and key in allowed, 'cutover_unknown_queue',
                        'Stored queue keys cannot be derived from the release; manual queue lists are not supported.')
                seen.add(key)
            require(len(seen) <= 262144, 'cutover_coverage', 'Redis coverage exceeds its bound.')
            if cursor == '0':
                break
        else:
            raise DeployError('cutover_coverage', 'Redis coverage scan exceeded its cursor budget.')
    return spaces


def private_path(host, entry, kind, identity):
    require(kind in ('plan', 'operation', 'restore') and (hash_value(identity) if kind == 'plan' else valid_uuid(identity)),
            'invalid_operation', 'Invalid cutover record identity.')
    root = host.paths.p(instance_paths(entry['name'])['private'])
    host.check_control_directory(root)
    root.mkdir(parents=True, exist_ok=True, mode=0o700)
    path = check_path(root / f'cutover-{kind}-{identity}.json', 'file')
    if path.exists():
        info = path.stat()
        require(stat.S_IMODE(info.st_mode) == 0o600 and (info.st_uid == 0 or host.paths.root != Path('/')),
                'cutover_record_ownership', 'Cutover records must be root-owned private files.')
    return path


def save_private(host, path, value):
    payload = json.dumps(value, sort_keys=True, separators=(',', ':'), allow_nan=False) + '\n'
    require(len(payload.encode()) <= MAX_JSON, 'invalid_json', 'Cutover record exceeds its size bound.')
    host.owned_write(path, payload, mode=0o600)


def validate_progress(value):
    required = {'schema', 'operation_id', 'plan_token', 'action', 'source', 'target', 'phase', 'outcome',
                'started_at', 'rollback_prohibited', 'drain_failed', 'hold', 'queues', 'database', 'seed', 'error'}
    optional = {'snapshot', 'restore', 'safety_snapshot', 'completed_at', 'error_phase'}
    require(isinstance(value, dict) and required <= set(value) <= required | optional
            and type(value['schema']) is int and value['schema'] == 1 and valid_uuid(value['operation_id'])
            and hash_value(value['plan_token']) and value['action'] in ('cutover', 'redis-restore')
            and value['phase'] in ('prepared', 'stopping', 'snapshot', 'queues', 'database', 'seed', 'switch', 'health', 'complete', 'recovering')
            and value['outcome'] in ('running', 'needs_reconciliation', 'succeeded', 'failed')
            and valid_timestamp(value['started_at']) and all(type(value[k]) is bool for k in ('rollback_prohibited', 'drain_failed', 'hold'))
            and isinstance(value['queues'], list) and len(value['queues']) <= 4096
            and value['database'] in ('not_started', 'dispatched_or_unknown', 'confirmed')
            and value['seed'] in ('not_started', 'dispatched_or_unknown', 'confirmed')
            and (value['error'] is None or name(value['error'])), 'cutover_state_invalid', 'Invalid cutover progress.')
    for key in ('source', 'target'):
        require(value[key] is None or isinstance(value[key], str) and VERSION_PATTERN.fullmatch(value[key]),
                'cutover_state_invalid', 'Invalid cutover release.')
    for queue in value['queues']:
        fields(queue, 'connection queue status', 'cutover_state_invalid')
        require(name(queue['connection']) and name(queue['queue']) and queue['status'] in ('pending', 'dispatched_or_unknown', 'confirmed'),
                'cutover_state_invalid', 'Invalid queue progress.')
    return value


def snapshot_evidence(evidence, request, entry):
    fields(evidence, 'schema status snapshot_id operation_id host_id project_id instance_id source_release configuration_revision inventory_sha256 scope_sha256 captured_at completed_at key_count encrypted_bytes sha256 object_key', 'cutover_snapshot_evidence')
    require(type(evidence['schema']) is int and evidence['schema'] == 1 and evidence['status'] == 'complete'
            and all(evidence[k] == request[k] for k in ('snapshot_id', 'operation_id', 'source_release', 'configuration_revision', 'inventory_sha256'))
            and evidence['host_id'] == entry['host_id'] and evidence['project_id'] == entry['project_id']
            and evidence['instance_id'] == entry['profile']['instance_id']
            and evidence['scope_sha256'] == sha(request['scopes']) and hash_value(evidence['sha256'])
            and valid_timestamp(evidence['captured_at']) and valid_timestamp(evidence['completed_at'])
            and type(evidence['key_count']) is int and evidence['key_count'] >= 0
            and type(evidence['encrypted_bytes']) is int and evidence['encrypted_bytes'] > 0
            and evidence['object_key'] == f"redis/{entry['profile']['instance_id']}/{request['snapshot_id']}.json.age",
            'cutover_snapshot_evidence', 'Snapshot completion evidence does not match this operation.')
    return evidence
