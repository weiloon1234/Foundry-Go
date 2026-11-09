"""Persistent queue compatibility and process-exit evidence, independent of history."""
from pathlib import Path
import re

from contracts import DeployError, check_path, digest, read_json, require, valid_uuid, VERSION_PATTERN
from topology import instance_units, selected_units

SCHEMA = 1
GENERATION = 5
GUARD = '/usr/local/lib/foundry/current/server/start_check.py'


def validate(value):
    code = 'queue_compatibility_invalid'
    require(isinstance(value, dict) and set(value) == {'schema', 'floor', 'permission', 'admitted'}
            and type(value['schema']) is int and value['schema'] == SCHEMA
            and (value['floor'] is None or type(value['floor']) is int and value['floor'] == 3)
            and value['permission'] in ('closed', 'admitted')
            and isinstance(value['admitted'], dict) and len(value['admitted']) <= 256,
            code, 'Queue compatibility state requires reconciliation.')
    for version, checksum in value['admitted'].items():
        require(isinstance(version, str) and VERSION_PATTERN.fullmatch(version)
                and isinstance(checksum, str) and re.fullmatch('[a-f0-9]{64}', checksum),
                code, 'Invalid admitted release identity.')
    return value


def require_mutation_idle(state):
    cutover = state.get('cutover')
    require(not cutover or cutover.get('outcome') in ('succeeded', 'failed'),
            'cutover_pending', 'Resume the recorded cutover before another lifecycle operation.')


def validate_state(state):
    progress = state.get('cutover')
    gate = state.get('queue_compatibility')
    require(not progress or gate is not None, 'queue_compatibility_missing',
            'Cutover progress without its persistent startup gate requires review.')
    if gate is not None:
        validate(gate)
    if progress:
        dispatched = any(q['status'] != 'pending' for q in progress['queues'])
        irreversible = progress['rollback_prohibited'] or dispatched or progress['action'] == 'redis-restore'
        require(not irreversible or gate['floor'] == 3 and progress['rollback_prohibited'],
                'queue_compatibility_invalid', 'Queue dispatch requires the persistent layout-3 floor.')


def require_release(host, entry, manifest, *, selecting=False):
    state_path = host.paths.instance_state(entry['name']) / 'state.json'
    state = host.load_state(entry)
    validate_state(state)
    progress = state.get('cutover')
    require(not progress or progress['outcome'] in ('succeeded', 'failed') or not progress['drain_failed'],
            'cutover_drain_review', 'Startup is blocked until the recorded drain uncertainty is reviewed.')
    gate = state.get('queue_compatibility')
    if gate is None:
        # A unit cannot establish a safe legacy deployment from a missing state.
        require(selecting or state_path.exists(), 'queue_compatibility_missing',
                'Deployment state is missing; refuse application startup.')
        return
    validate(gate)
    require(gate['permission'] == 'admitted', 'queue_start_fenced',
            'Application startup is fenced by the recorded cutover.')
    attempt = (progress or {}).get('candidate_start')
    if attempt is not None and attempt['outcome'] != 'healthy':
        require(candidate_executor_matches(host, entry, progress), 'cutover_drain_review',
                'Candidate admission belongs to another boot or executor; resolve and explicitly resume.')
    if gate['floor'] is None:
        return  # No queue compatibility transition: ordinary retained-release rollback remains valid.
    require(gate['admitted'].get(manifest['version']) == manifest['files']['artisan'],
            'queue_release_incompatible', 'This release is not admitted against the queue layout; reviewed forward recovery is required.')


def candidate_executor_matches(host, entry, progress):
    import release
    attempt = progress.get('candidate_start') or {}
    try:
        return (attempt.get('outcome') == 'dispatched_or_unknown'
                and attempt.get('boot_id') == (progress.get('drain') or {}).get('boot_id') == release._boot_id(host)
                and isinstance(attempt.get('executor_invocation_id'), str)
                and re.fullmatch('[a-f0-9]{32}', attempt['executor_invocation_id']) is not None
                and release.cutover_executor_admits_start(host, entry, progress['operation_id'], attempt['executor_invocation_id']))
    except (DeployError, OSError):
        return False


def reconcile_candidate_admission(host, entry, state):
    """Derive the fence on every read, without racing the coordinator's state writer."""
    import cutover_contracts as contract
    progress = state.get('cutover')
    if progress and contract.candidate_review_required(progress) and not candidate_executor_matches(host, entry, progress):
        progress['drain_failed'] = True
        state['queue_compatibility']['permission'] = 'closed'


def require_current(host, entry):
    import release
    version = release.current_version(host, entry)
    require(version is not None, 'no_release', 'No release is selected.')
    manifest = read_json(release.release_path(host, entry, version) / 'manifest.json')
    release.validate_manifest(manifest, entry)
    require_release(host, entry, manifest)
    require(digest(release.release_path(host, entry, version) / 'artisan') == manifest['files']['artisan'],
            'release_conflict', 'Selected executable differs from its manifest.')


def startup_check(host, name, role):
    registry = host.read_registry()
    entries = [entry for entry in registry['instances'].values() if entry['name'] == name]
    require(len(entries) == 1, 'identity_mismatch', 'Unknown application instance.')
    entry = entries[0]
    selected_units(entry, role)
    require(role in instance_units(entry), 'invalid_role', 'Startup requires one declared role.')
    require_current(host, entry)


def guard_line(entry, role):
    return f'ExecStartPre=+/usr/bin/python3 -I {GUARD} {entry["name"]} {role}'


def require_guards(host, entry):
    from provision import _unit_checks
    _unit_checks(host, entry)
    for role, unit in instance_units(entry).items():
        path = check_path(host.paths.p('/etc/systemd/system/' + unit), 'file')
        require(path.exists() and guard_line(entry, role) in path.read_text().splitlines(),
                'cutover_guard_missing', 'Upgrade host tools and install the startup guards before cutover.')
        result = host.run(['systemctl', 'show', unit,
                           '--property=FragmentPath,DropInPaths,NeedDaemonReload', '--no-pager'], timeout=5)
        observed = dict(line.split('=', 1) for line in result.stdout.splitlines() if '=' in line)
        require(observed.get('FragmentPath') == '/etc/systemd/system/' + unit
                and observed.get('DropInPaths') == '' and observed.get('NeedDaemonReload') == 'no',
                'cutover_guard_missing', 'Loaded units must match the guarded owned units.')


def _empty_cgroup(host, group):
    require(isinstance(group, str) and group.startswith('/') and group != '/'
            and '..' not in Path(group).parts, 'cutover_stop_unproven', 'Cannot establish the service cgroup.')
    root = check_path(host.paths.p('/sys/fs/cgroup' + group), 'directory')
    if not root.exists():
        return
    paths = [root / 'cgroup.procs']
    for count, path in enumerate(root.rglob('cgroup.procs')):
        require(count < 4096, 'cutover_stop_unproven', 'Service cgroup exceeds inspection bounds.')
        paths.append(path)
    for path in paths:
        check_path(path, 'file')
        require(path.is_file() and path.stat().st_size <= 65536 and not path.read_text().strip(),
                'cutover_stop_unproven', 'A service cgroup still owns processes or cannot be inspected.')


def prove_stopped(host, entry, *, roles=None, review=False):
    observations = {}
    for role, unit in selected_units(entry, roles=roles).items():
        result = host.run(['systemctl', 'show', unit,
                           '--property=LoadState,ActiveState,SubState,MainPID,ControlPID,ControlGroup,Slice,Job,Result,ExecMainCode,ExecMainStatus,ExecMainPID,ExecMainStartTimestampMonotonic,InvocationID',
                           '--no-pager'], timeout=5)
        value = dict(line.split('=', 1) for line in result.stdout.splitlines() if '=' in line)
        require(value.get('LoadState') == 'loaded' and value.get('ActiveState') in ('inactive', 'failed')
                and value.get('SubState') in ('dead', 'failed')
                and value.get('MainPID') == value.get('ControlPID') == '0'
                and value.get('Job') in ('0', ''), 'cutover_stop_unproven',
                'A role has not completed shutdown; a timeout is not exit proof.')
        group = value.get('ControlGroup')
        if group == '':
            require(value.get('Slice') == 'system.slice', 'cutover_stop_unproven', 'Unexpected role slice.')
            group = '/system.slice/' + unit
        _empty_cgroup(host, group)
        # Only a reboot after completed drain, before any candidate attempt,
        # can use a guard refusal as evidence. A retry may hide earlier work.
        progress = host.load_state(entry).get('cutover') or {}
        drain = progress.get('drain') or {}
        no_application = False
        if value.get('Result') in ('exit-code', 'start-limit-hit') and (
            value.get('ExecMainPID') == '0' and value.get('ExecMainStartTimestampMonotonic') == '0'
                and value.get('ExecMainCode') == '0' and value.get('ExecMainStatus') == '0'):
            import release
            no_application = ('candidate_start' in progress and progress['candidate_start'] is None
                              and not progress.get('drain_failed')
                              and set(drain.get('roles', {})) == set(instance_units(entry))
                              and all(item['complete'] for item in drain['roles'].values())
                              and drain['boot_id'] != release._boot_id(host))
        clean = value.get('Result') == 'success' and not (value.get('ExecMainCode') in ('2', '3')
                and value.get('ExecMainStatus') == '9')
        approved = False
        if not clean and not no_application and not review:
            import release
            approved = any(item['boot_id'] == release._boot_id(host) and item['observations'].get(role) == value
                           for item in progress.get('drain_resolutions', []))
        require(clean or no_application or review or approved, 'cutover_drain_review',
                'A role did not drain cleanly; operator review is required.')
        observations[role] = value
    return observations


def require_no_clients(host, entry):
    found = host.run(['getent', 'passwd', entry['name']], timeout=5).stdout.strip().split(':')
    require(len(found) == 7 and found[0] == entry['name'] and found[2].isdigit(),
            'cutover_stop_unproven', 'Cannot establish the instance process owner.')
    uid = found[2]
    proc = host.paths.p('/proc')
    require(proc.is_dir(), 'cutover_stop_unproven', 'Process inventory is unavailable.')
    for count, path in enumerate(proc.iterdir()):
        require(count < 131072, 'cutover_stop_unproven', 'Process inventory exceeds inspection bounds.')
        if not path.name.isdigit():
            continue
        try:
            status = (path / 'status').read_text()
        except FileNotFoundError:
            continue
        except OSError:
            raise DeployError('cutover_stop_unproven', 'A process owner could not be inspected.') from None
        owners = next((line.split()[1:] for line in status.splitlines() if line.startswith('Uid:')), None)
        require(owners is not None, 'cutover_stop_unproven', 'Process ownership is incomplete.')
        require(uid not in owners, 'cutover_clients_present', 'An instance-owned process remains outside verified shutdown.')


def require_quiescence(host, entry, operation_id):
    state = host.load_state(entry)
    active = state.get('cutover') or {}
    gate = state.get('queue_compatibility')
    require(valid_uuid(operation_id) and active.get('operation_id') == operation_id
            and gate is not None and validate(gate)['permission'] == 'closed',
            'cutover_stop_unproven', 'Snapshot or migration requires the matching fenced operation.')
    require(not active.get('drain_failed'), 'cutover_drain_review', 'Forced termination requires operator review.')
    prove_stopped(host, entry)
    require_no_clients(host, entry)
