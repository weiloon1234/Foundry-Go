"""Generation-5 finished queue cutover cleanup. Step 2 deletes this shim.

Call mutations under the existing host/instance locks. External cleanup precedes
state publication, so a failed command or crash retains the durable retry trigger.
Read-only loads and upgrade planning never remove records or change units.
"""
import os

from contracts import check_path, instance_paths, read_json, require, valid_uuid


BRIDGE_FILES = frozenset({"lib/compatibility.py", "lib/cutover_contracts.py",
                          "server/start_check.py", "server/cutover_runner.py"})

EXECUTOR_PROPERTIES = 'ActiveState,SubState,Result,MainPID,ControlPID,Job'


def require_finished(state):
    progress = state.get('cutover')
    gate = state.get('queue_compatibility')
    require(progress is None or isinstance(progress, dict)
            and progress.get('outcome') == 'succeeded'
            and not progress.get('hold') and not progress.get('drain_failed'),
            'state_cleanup_pending', 'Finish the recorded queue operation on generation 5 before upgrading.')
    require(gate is None or isinstance(gate, dict) and gate.get('permission') == 'admitted',
            'state_cleanup_pending', 'Resolve the closed queue gate on generation 5 before upgrading.')


def _records(host, entry):
    private = check_path(host.paths.p(instance_paths(entry['name'])['private']), 'directory')
    host.check_control_directory(private)
    records = []
    for kind in ('plan', 'operation', 'snapshot'):
        records.extend(check_path(path, 'file') for path in private.glob('cutover-' + kind + '-*.json'))
    return private, sorted(records)


def executor_finished(observed):
    active = observed.get('ActiveState')
    return ((active in ('inactive', 'failed') or active == 'active' and observed.get('SubState') == 'exited')
            and observed.get('MainPID') == '0' and observed.get('ControlPID') == '0'
            and observed.get('Job') in ('', '0'))


def _finished_units(host, entry):
    instance_id = entry['profile']['instance_id']
    require(valid_uuid(instance_id), 'invalid_state', 'Invalid instance identity.')
    prefix = 'foundry-cutover-' + instance_id + '-'
    result = host.run(['systemctl', 'list-units', '--all', '--type=service', '--no-legend', '--plain', '--no-pager'], timeout=10)
    finished = {}
    for line in result.stdout.splitlines():
        fields = line.split()
        if not fields or not fields[0].startswith(prefix) or not fields[0].endswith('.service'):
            continue
        unit = fields[0]
        if not valid_uuid(unit[len(prefix):-len('.service')]):
            continue
        result = host.run(['systemctl', 'show', unit, '--property=' + EXECUTOR_PROPERTIES, '--no-pager'], timeout=5)
        observed = dict(line.split('=', 1) for line in result.stdout.splitlines() if '=' in line)
        require(executor_finished(observed),
                'state_cleanup_pending', 'A queue operation executor still has pending work.')
        if observed['ActiveState'] in ('failed', 'active'):
            finished[unit] = observed['ActiveState']
    return finished


def check_upgrade(host, entry):
    """Refuse unfinished work before removing its executors and startup checks."""
    state = host.load_state(entry)
    require_finished(state)
    _, records = _records(host, entry)
    if records or 'cutover' in state or 'queue_compatibility' in state:
        _finished_units(host, entry)


def remove_finished_state(host, entry, state):
    require_finished(state)
    path = check_path(host.paths.instance_state(entry["name"]) / "state.json", "file")
    recorded = read_json(path) if path.exists() else {}
    require_finished(recorded)
    private, records = _records(host, entry)
    changed = any(key in value for key in ('cutover', 'queue_compatibility')
                  for value in (state, recorded)) or bool(records)
    if not changed:
        return False
    for unit, active in _finished_units(host, entry).items():
        host.run(['systemctl', 'reset-failed' if active == 'failed' else 'stop', unit], timeout=5)
    for path in records:
        path.unlink(missing_ok=True)
    if private.exists():
        directory = os.open(private, os.O_RDONLY)
        try:
            os.fsync(directory)
        finally:
            os.close(directory)
    strip_fields(state)
    return True


def strip_fields(state):
    state.pop('cutover', None)
    state.pop('queue_compatibility', None)
