#!/usr/bin/env python3
"""Lead-only generation-5 to generation-6 upgrade on the disposable host."""
import argparse
import copy
import hashlib
import io
import json
from pathlib import Path
import re
import sys
import tarfile
import uuid

sys.dont_write_bytecode = True
sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'lib'))
from contracts import digest, instance_paths, read_json
import release
from state_cleanup import EXECUTOR_PROPERTIES, executor_finished
from topology import instance_units
from upgrades import protocol_metadata, tool_bundle
from acceptance_roles import prove_running, second_archive
from acceptance_support import LAUNCHER, WORK, fixture, guard, request, run, wait_for

TOOLS = Path(__file__).resolve().parents[1]


def failing_readiness(source, destination):
    """Repack a fixture-only readiness failure without modifying its binary."""
    with tarfile.open(source, 'r:gz') as incoming:
        manifest = json.load(incoming.extractfile('manifest.json'))
        original = incoming.extractfile('config/deploy.toml').read().decode()
        configured, count = re.subn(r'(?m)^readiness = "healthy"$', 'readiness = "fail"', original)
        assert count == 1, 'The upgrade phase requires the deploy-acceptance fixture'
        config = configured.encode()
        manifest['files']['config/deploy.toml'] = hashlib.sha256(config).hexdigest()
        replacements = {'config/deploy.toml': config, 'manifest.json': json.dumps(manifest).encode()}
        with tarfile.open(destination, 'x:gz') as outgoing:
            for original_member in incoming:
                member = copy.copy(original_member)
                member.pax_headers = dict(member.pax_headers)
                if member.name in replacements:
                    payload = replacements[member.name]
                    member.size = len(payload)
                    outgoing.addfile(member, io.BytesIO(payload))
                else:
                    outgoing.addfile(member, incoming.extractfile(original_member) if member.isfile() else None)


def invoke(action, payload, *, tools=None, error=None):
    argv = [LAUNCHER, action] if tools is None else [sys.executable, '-B', str(tools / 'server/manage.py'), action]
    result = run(argv, input=json.dumps(payload), check=False, timeout=1200)
    answer = json.loads(result.stdout)
    code = answer.get('error', {}).get('code')
    if error:
        assert result.returncode != 0 and code == error, 'Expected ' + error + ', observed ' + str(code)
    else:
        assert result.returncode == 0 and code is None, 'Host action failed: ' + action + ': ' + str(code)
    return answer


def executor_unit(entry, operation):
    return 'foundry-cutover-' + entry['profile']['instance_id'] + '-' + operation + '.service'


def executor_state(host, unit):
    result = host.run(['systemctl', 'show', unit, '--property=' + EXECUTOR_PROPERTIES + ',ExecMainStatus', '--no-pager'], check=False)
    return dict(line.split('=', 1) for line in result.stdout.splitlines() if '=' in line)


def loaded_executors(host, entry):
    prefix = 'foundry-cutover-' + entry['profile']['instance_id'] + '-'
    result = host.run(['systemctl', 'list-units', '--all', '--type=service', '--no-legend', '--plain', '--no-pager'])
    return [fields[0] for line in result.stdout.splitlines() if (fields := line.split())
            and fields[0].startswith(prefix) and fields[0].endswith('.service')]


def completed(host, entry, operation, outcome):
    def finished():
        state = host.load_state(entry)
        progress = state.get('cutover', {})
        if progress.get('operation_id') != operation or progress.get('outcome') == 'running':
            return None
        if not executor_finished(executor_state(host, executor_unit(entry, operation))):
            return None
        return state
    state = wait_for(finished, 'Historical operation did not finish', timeout=300)
    assert state['cutover']['outcome'] == outcome, str(state['cutover'].get('error'))
    return state


def upgrade(archive, old_tools):
    host, project, profile, entry = fixture()
    assert host.read_registry()['tools']['generation'] == 5
    assert host.read_registry()['tools']['bundle_sha256'] == tool_bundle(old_tools)[2]
    old = {**request(project, profile), **protocol_metadata(old_tools)}
    current = request(project, profile)
    assert host.load_state(entry)['queue_compatibility']['floor'] == 3
    for unit in instance_units(entry).values():
        assert 'ExecStartPre=' in Path('/etc/systemd/system', unit).read_text()
    invoke('restart', current, error='host_upgrade_required')

    # Use the published executor to leave a real failed, superseded unit record.
    failed_archive, _ = second_archive(archive, project, profile)
    failing = WORK / ('unhealthy-' + uuid.uuid4().hex + '.tar.gz')
    failing_readiness(failed_archive, failing)
    plan = invoke('cutover-plan', {**old, 'archive': str(failing), 'archive_sha256': digest(failing)})
    assert {q['queue'] for q in plan['queues']} == {'first', 'second'}
    failed = str(uuid.uuid4())
    invoke('cutover-apply', {**old, 'plan_token': plan['plan_token'], 'operation_id': failed})
    state = completed(host, entry, failed, 'needs_reconciliation')
    assert state['cutover']['candidate_start']['outcome'] == 'failed'
    invoke('stop', old)
    invoke('cutover-drain-resolve', {**old, 'operation_id': failed, 'reviewed_by': 'upgrade-acceptance',
                                   'reason': 'Fixture readiness deliberately failed; replace the candidate'})
    healthy_archive, healthy_version = second_archive(archive, project, profile)
    plan = invoke('cutover-forward-plan', {**old, 'archive': str(healthy_archive), 'archive_sha256': digest(healthy_archive)})
    successor = str(uuid.uuid4())
    invoke('cutover-forward-apply', {**old, 'plan_token': plan['plan_token'], 'operation_id': successor})
    state = completed(host, entry, successor, 'succeeded')
    assert state['current']['version'] == healthy_version
    assert state['cutover']['supersedes_operation'] == failed
    protected_source = state['cutover']['source']
    # A normal generation-5 deployment adds the second admitted version and
    # leaves the completed operation protecting its old source and target.
    ordinary, _ = second_archive(archive, project, profile)
    invoke('activate', {**old, 'archive': str(ordinary), 'archive_sha256': digest(ordinary)})
    state = host.load_state(entry)
    assert (host.paths.opt(entry['name']) / 'releases' / protected_source).is_dir()
    gate = state['queue_compatibility']
    assert gate['floor'] == 3 and gate['permission'] == 'admitted'
    assert len(gate['admitted']) == 2
    failed_unit = executor_unit(entry, failed)
    failed_state = executor_state(host, failed_unit)
    assert tuple(failed_state[key] for key in ('ActiveState', 'Result', 'ExecMainStatus')) == ('failed', 'exit-code', '1')
    assert executor_finished(failed_state)
    successful_state = executor_state(host, executor_unit(entry, successor))
    assert (successful_state['ActiveState'], successful_state['SubState'], successful_state['Result']) == ('active', 'exited', 'success')
    assert executor_finished(successful_state)
    private = host.paths.p(instance_paths(entry['name'])['private'])
    records = [path for kind in ('plan', 'operation', 'snapshot') for path in private.glob('cutover-' + kind + '-*.json')]
    assert all(any(path.name.startswith('cutover-' + kind + '-') for path in records) for kind in ('plan', 'operation', 'snapshot'))
    # The live host retained its original generation-3 application registration.
    with host.lock('host'):
        registry = host.read_registry()
        registry['instances'][profile['instance_id']]['client_generation'] = 3
        host.write_registry(registry)
    before = (host.paths.instance_state(entry['name']) / 'state.json').read_bytes()
    invoke('report', current)
    assert before == (host.paths.instance_state(entry['name']) / 'state.json').read_bytes()
    assert all(path.exists() for path in records)

    # The published validator must accept all four generation-5 bridge files.
    run([sys.executable, '-I', '-c',
         "import sys; from pathlib import Path; sys.path.insert(0, sys.argv[1]); import upgrades; upgrades.tool_bundle(Path(sys.argv[2]))",
         str(old_tools / 'lib'), str(TOOLS)])
    plan = invoke('tools-plan', current, tools=TOOLS)
    assert plan['upgrade']['generation'] == 6
    invoke('tools-apply', {**current, 'plan_token': plan['plan_token']}, tools=TOOLS)
    assert host.read_registry()['tools']['bundle_sha256'] == tool_bundle(TOOLS)[2]
    assert host.read_registry()['instances'][profile['instance_id']]['client_generation'] == 3
    for unit in instance_units(entry).values():
        assert 'ExecStartPre=' not in Path('/etc/systemd/system', unit).read_text()
        loaded = host.run(['systemctl', 'show', unit, '--property=ExecStartPre,NeedDaemonReload']).stdout
        properties = dict(line.split('=', 1) for line in loaded.splitlines() if '=' in line)
        assert properties.get('NeedDaemonReload') == 'no' and not properties.get('ExecStartPre')
    # State may already be stripped by new-code backup rebinding during apply.
    candidate, _ = second_archive(archive, project, profile)
    invoke('activate', {**current, 'archive': str(candidate), 'archive_sha256': digest(candidate)})
    _, entry = host.require_identity(project, profile)
    state = host.load_state(entry)
    assert 'cutover' not in state and 'queue_compatibility' not in state
    assert all(not path.exists() for path in records)
    wait_for(lambda: not loaded_executors(host, entry), 'Historical executor units remain loaded', timeout=10)
    prove_running(host, entry)
    # One more release puts every pre-upgrade source outside current/previous and retention.
    candidate, _ = second_archive(archive, project, profile)
    invoke('activate', {**current, 'archive': str(candidate), 'archive_sha256': digest(candidate)})
    assert not (host.paths.opt(entry['name']) / 'releases' / protected_source).exists()
    assert not (host.paths.opt(entry['name']) / 'releases' / healthy_version).exists()
    prove_running(host, entry)
    assert not loaded_executors(host, entry), 'Historical executor units remain loaded'
    report = invoke('report', current)
    assert not report['host']['failed_units']
    assert not report['problems'], report['problems']
    print('PASS published v0.15.3 upgrade: state, private records, unloaded executors, loaded role units, retention and report', flush=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--archive', type=Path, required=True)
    parser.add_argument('--tools-root', type=Path, required=True)
    args = parser.parse_args()
    guard()
    upgrade(args.archive, args.tools_root)


if __name__ == '__main__':
    main()
