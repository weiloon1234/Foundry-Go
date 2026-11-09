#!/usr/bin/env python3
"""Lead-only fast recovery slice on the disposable systemd acceptance host."""
import argparse
import copy
import hashlib
import io
import json
import re
from pathlib import Path
import sys
import tarfile
import uuid

sys.dont_write_bytecode = True
sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'lib'))
import compatibility
from contracts import digest, read_json
import cutover_contracts as contract
import redis_acl
import redis_snapshot
import release
from topology import instance_units
from upgrades import tool_bundle
from acceptance_roles import prove_running, second_archive, selected
from acceptance_diagnostics import unit_failure_details
from acceptance_support import LAUNCHER, WORK, fixture, guard, private_json, request, run, wait_for

TOOLS = Path(__file__).resolve().parents[1]
REVIEWER = 'hostdrill-acceptance'


def rewrite_readiness(source, destination, mode):
    """Create a test-owned archive variant; never edit a published release."""
    if mode not in ('healthy', 'fail', 'hold'):
        raise ValueError('Unknown fixture readiness mode')
    with tarfile.open(source, 'r:gz') as incoming:
        manifest = json.load(incoming.extractfile('manifest.json'))
        original = incoming.extractfile('config/deploy.toml').read().decode()
        configured, count = re.subn(r'(?m)^readiness = "(?:healthy|fail|hold)"$',
                                    'readiness = "' + mode + '"', original)
        if count != 1:
            raise ValueError('Archive must contain exactly one fixture readiness declaration')
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


def invoke(action, payload, *, tools=None, error=None, timeout=600):
    argv = [LAUNCHER, action] if tools is None else [sys.executable, '-B', str(tools / 'server/manage.py'), action]
    result = run(argv, input=json.dumps(payload), check=False, timeout=timeout)
    answer = json.loads(result.stdout)
    code = answer.get('error', {}).get('code')
    if error is not None:
        assert result.returncode != 0 and code == error, 'Expected refusal ' + error + ', observed ' + str(code)
    else:
        assert result.returncode == 0 and code is None, 'Host action failed: ' + action + ': ' + str(code)
    return answer


def hash_fields(value):
    assert isinstance(value, list) and len(value) % 2 == 0 and all(isinstance(item, str) for item in value)
    fields = dict(zip(value[::2], value[1::2]))
    assert len(fields) * 2 == len(value)
    return fields


def acknowledge_incident_executor(host, entry, operation, successor):
    """Retain evidence, then clear only this recovered test incident's failed unit."""
    archived = read_json(contract.private_path(host, entry, 'operation', operation))
    assert archived['result']['outcome'] == 'superseded'
    assert archived['result']['replacement_operation'] == successor
    assert archived['superseded_progress']['error'] == 'cutover_stop_unproven'
    progress = host.load_state(entry)['cutover']
    assert progress['operation_id'] == successor and progress['outcome'] == 'succeeded'
    assert progress['supersedes_operation'] == operation
    assert release.cutover_executor(host, entry, operation) == 'exited'
    unit = release.cutover_unit(entry, operation)
    observed = unit_failure_details(host, unit)
    assert observed == {'ActiveState': 'failed', 'Result': 'exit-code', 'ExecMainStatus': '1'}, \
        'Unexpected incident executor state: ' + json.dumps({unit: observed})
    evidence = {'unit': unit, **observed}
    print('Incident executor evidence before test-side reset: ' + json.dumps(evidence), flush=True)
    host.run(['systemctl', 'reset-failed', unit], timeout=5)
    def cleared():
        state = release.cutover_executor(host, entry, operation)
        return state == 'absent' or (state == 'exited' and unit_failure_details(host, unit)['ActiveState'] == 'inactive')
    wait_for(cleared, 'Incident executor failed state was not cleared: ' + unit, timeout=5)
    return evidence


class Recovery:
    def __init__(self, archive):
        self.host, self.project, self.profile, self.entry = fixture()
        self.archive = archive
        self.directory = WORK / ('hostdrill-' + uuid.uuid4().hex)
        self.directory.mkdir(mode=0o700)
        self.business = self.host.paths.data(self.entry['name']) / 'hostdrill'
        self.evidence = []

    def refresh(self):
        _, self.entry = self.host.require_identity(self.project, self.profile)

    def state(self):
        return self.host.load_state(self.entry)

    def call(self, action, **fields):
        return invoke(action, request(self.project, self.profile, **fields))

    def candidate(self, mode='healthy'):
        archive, version = second_archive(self.archive, self.project, self.profile)
        if mode != 'healthy':
            configured = self.directory / (version + '-' + mode + '.tar.gz')
            rewrite_readiness(archive, configured, mode)
            archive = configured
        return archive, version

    def plan(self, candidate, action='cutover-plan'):
        archive, version = candidate
        plan = self.call(action, archive=str(archive), archive_sha256=digest(archive))
        assert plan['applicable'] and plan['target'] == version
        assert {queue['queue'] for queue in plan['queues']} == {'first', 'second'}
        assert all(queue['layout'] == '3' for queue in plan['queues']), 'Fast slice requires existing layout 3'
        return plan

    def apply(self, plan, action='cutover-apply'):
        operation = str(uuid.uuid4())
        answer = self.call(action, operation_id=operation, plan_token=plan['plan_token'])
        assert answer['operation_id'] == operation and answer['outcome'] == 'running'
        return operation

    def wait_operation(self, operation, outcome='succeeded', timeout=300):
        def completed():
            state = self.state()
            progress = state.get('cutover', {})
            if progress.get('operation_id') != operation or progress.get('outcome') == 'running':
                return None
            if release.cutover_executor(self.host, self.entry, operation) == 'running':
                return None
            return state
        state = wait_for(completed, 'Operation did not reach ' + outcome + ': ' + operation, timeout=timeout)
        assert state['cutover']['outcome'] == outcome, 'Unexpected operation outcome: ' + str(state['cutover'].get('error'))
        return state

    def healthy(self, operation, plan):
        state = self.wait_operation(operation)
        progress = state['cutover']
        assert state['current']['version'] == plan['target']
        assert state['queue_compatibility']['floor'] == 3
        assert state['queue_compatibility']['permission'] == 'admitted'
        assert state['queue_compatibility']['admitted'][plan['target']] == plan['candidate_sha256']
        assert progress['rollback_prohibited'] and not progress['drain_failed']
        assert progress['candidate_start']['outcome'] == 'healthy'
        assert all(item['status'] == 'confirmed' for item in progress['queues'])
        assert progress['snapshot']['status'] == 'complete'
        self.refresh()
        prove_running(self.host, self.entry)
        assert set(release.enabled_roles(self.host, self.entry)) == set(instance_units(self.entry))
        return state

    def stopped(self, *, review=False):
        observations = compatibility.prove_stopped(self.host, self.entry, review=review)
        compatibility.require_no_clients(self.host, self.entry)
        return observations

    def record(self, scenario, operation, **fields):
        progress = self.state()['cutover']
        self.evidence.append({'scenario': scenario, 'operation_id': operation,
                              'outcome': progress['outcome'], 'phase': progress['phase'],
                              'floor': self.state()['queue_compatibility']['floor'], **fields})
        print('PASS ' + scenario, flush=True)

    def finish(self):
        private_json(self.directory / 'evidence.json', self.evidence)
        print('Recovery evidence: ' + str(self.directory / 'evidence.json'), flush=True)

    def foreign(self):
        self.refresh()
        units = ['foundry-acceptance-unrelated.service']
        for entry in self.host.read_registry()['instances'].values():
            if entry['name'] != self.entry['name']:
                units.extend(instance_units(entry).values())
        units.extend(value['unit'] for name, value in self.host.read_registry()['shared'].items()
                     if name in ('postgresql', 'redis'))
        return {unit: release._role_invocation(self.host, unit) for unit in units}

    def enqueue(self, queue, barrier=False):
        identity = str(uuid.uuid4())
        arguments = ['--queue', queue, '--id', identity]
        if barrier:
            arguments.append('--barrier')
        result = release.app(self.host, self.entry, selected(self.host, self.entry), 'fixture:enqueue', extra=arguments)
        assert json.loads(result.stdout)['id'] == identity
        return identity

    def marker(self, identity, suffix):
        return self.business / (identity + '.' + suffix)

    def wait_marker(self, identity, suffix, timeout=30):
        wait_for(lambda: self.marker(identity, suffix).is_file(), 'Worker did not write ' + suffix, timeout=timeout)

    def release_job(self, identity):
        self.marker(identity, 'release').touch(exist_ok=False)

    def wait_worker_stopping(self):
        unit = instance_units(self.entry)['worker']
        wait_for(lambda: release.service(self.host, unit)['ActiveState'] == 'deactivating',
                 'Worker did not enter graceful drain', timeout=12)

    def resolve(self, operation, reason):
        before = self.state()
        assert before['cutover']['drain_failed']
        answer = self.call('cutover-drain-resolve', operation_id=operation, reviewed_by=REVIEWER, reason=reason)
        resolution = answer['resolution']
        assert resolution['reviewed_by'] == REVIEWER and resolution['reason'] == reason
        state = self.state()
        assert state['cutover']['operation_id'] == operation and state['cutover']['hold']
        assert not state['cutover']['drain_failed'] and state['queue_compatibility']['permission'] == 'closed'
        self.stopped(review=True)
        return resolution

    def incident(self, old_tools):
        assert self.host.read_registry()['tools']['bundle_sha256'] == tool_bundle(old_tools)[2]
        assert self.state()['queue_compatibility']['floor'] == 3
        foreign = self.foreign()
        # A queue gate sends ordinary stop through the same broken proof, even
        # before a cutover exists. Restart restores the incident's running baseline.
        invoke('stop', request(self.project, self.profile), error='cutover_stop_unproven')
        self.stopped()
        self.call('restart')
        prove_running(self.host, self.entry)
        print('PASS v0.15.0 queue-gated stop refuses cutover_stop_unproven', flush=True)
        plan = self.plan(self.candidate())
        operation = self.apply(plan)
        state = self.wait_operation(operation, 'needs_reconciliation', timeout=270)
        assert state['cutover']['error'] == 'cutover_stop_unproven'
        assert state['queue_compatibility']['permission'] == 'closed'
        assert state['cutover']['snapshot_started'] is False and 'snapshot' not in state['cutover']
        self.stopped()
        # The normal upgrade entrypoint runs from the proposed staged bundle.
        # The installed old bundle is immutable and is never patched for this test.
        payload = request(self.project, self.profile)
        upgrade = invoke('tools-plan', payload, tools=TOOLS)
        assert upgrade['upgrade'] is not None
        invoke('tools-apply', {**payload, 'plan_token': upgrade['plan_token']}, tools=TOOLS)
        assert self.host.read_registry()['tools']['bundle_sha256'] == tool_bundle(TOOLS)[2]
        self.refresh()
        assert self.state()['cutover']['operation_id'] == operation
        self.stopped()
        candidate = self.candidate()
        invoke('cutover-abort-plan', request(self.project, self.profile, archive=str(candidate[0]),
                                            archive_sha256=digest(candidate[0]), reviewed_by=REVIEWER,
                                            reason='Floor three must prohibit abort'), error='cutover_abort_unsafe')
        forward = self.plan(candidate, 'cutover-forward-plan')
        successor = self.apply(forward, 'cutover-forward-apply')
        result = self.healthy(successor, forward)
        assert result['cutover']['supersedes_operation'] == operation
        assert read_json(contract.private_path(self.host, self.entry, 'operation', operation))['result']
        stopped = self.call('stop')
        assert stopped['contained'] and stopped['status'] == 'stopped'
        self.stopped()
        self.call('restart')
        prove_running(self.host, self.entry)
        print('PASS upgraded queue-gated stop and restart', flush=True)
        executor = acknowledge_incident_executor(self.host, self.entry, operation, successor)
        assert self.foreign() == foreign, 'Incident recovery restarted another instance or shared service'
        self.record('v0.15.0 stop proof, current upgrade, abort refusal and forward recovery', successor,
                    prior_operation=operation, abort_refusal='cutover_abort_unsafe',
                    old_stop_refusal='cutover_stop_unproven', upgraded_stop='stopped',
                    acknowledged_executor=executor)

    def successful_drain(self):
        foreign = self.foreign()
        barrier = self.enqueue('first', barrier=True)
        try:
            quick = self.enqueue('second')
            self.wait_marker(barrier, 'started')
            self.wait_marker(quick, 'completed')
            plan = self.plan(self.candidate())
            operation = self.apply(plan)
            self.wait_worker_stopping()
            assert not self.marker(barrier, 'completed').exists()
            assert 'snapshot' not in self.state()['cutover']
        finally:
            self.release_job(barrier)
        self.wait_marker(barrier, 'completed')
        state = self.healthy(operation, plan)
        _, journal, _ = redis_snapshot._snapshot_paths(self.host, self.entry, state['cutover']['snapshot']['snapshot_id'])
        assert read_json(journal)['captured_ns'] >= self.marker(barrier, 'completed').stat().st_mtime_ns
        after = self.enqueue('second')
        self.wait_marker(after, 'completed')
        assert self.foreign() == foreign
        self.record('two queues and real in-flight graceful drain', operation)

    def forced_drain(self):
        barrier = self.enqueue('first', barrier=True)
        try:
            self.wait_marker(barrier, 'started')
            plan = self.plan(self.candidate())
            operation = self.apply(plan)
            self.wait_worker_stopping()
            run(['systemctl', 'kill', '--kill-whom=all', '--signal=SIGKILL', instance_units(self.entry)['worker']])
            state = self.wait_operation(operation, 'needs_reconciliation')
            assert state['cutover']['drain_failed'] and state['cutover']['error'] == 'cutover_drain_review'
            assert state['cutover']['snapshot_started'] is False and 'snapshot' not in state['cutover']
            assert state['queue_compatibility']['permission'] == 'closed'
            self.stopped(review=True)
        finally:
            self.release_job(barrier)
        resolution = self.resolve(operation, 'Fixture worker was killed; release its owned barrier before retry')
        assert resolution['observations']['worker']['ExecMainStatus'] == '9'
        self.call('cutover-resume', operation_id=operation, plan_token=plan['plan_token'])
        self.healthy(operation, plan)
        self.wait_marker(barrier, 'completed', timeout=120)
        self.record('forced worker drain, audited resolution and explicit resume', operation)

    def forward_failure(self):
        plan = self.plan(self.candidate('fail'))
        operation = self.apply(plan)
        state = self.wait_operation(operation, 'needs_reconciliation')
        assert state['cutover']['candidate_start']['outcome'] == 'failed'
        assert state['cutover']['drain_failed'] and state['queue_compatibility']['permission'] == 'closed'
        snapshot = state['cutover']['snapshot']['snapshot_id']
        self.call('stop')
        self.resolve(operation, 'Fixture readiness deliberately refused; replace the immutable candidate')
        forward = self.plan(self.candidate(), 'cutover-forward-plan')
        successor = self.apply(forward, 'cutover-forward-apply')
        self.healthy(successor, forward)
        archived = read_json(contract.private_path(self.host, self.entry, 'operation', operation))
        assert archived['result']
        assert release._load_snapshot_binding(self.host, self.entry, snapshot)['evidence']['snapshot_id'] == snapshot
        self.record('readiness failure and reviewed forward replacement', successor, prior_operation=operation)

    def containment(self):
        for suffix in ('started', 'release'):
            (self.business / ('readiness.' + suffix)).unlink(missing_ok=True)
        plan = self.plan(self.candidate('hold'))
        operation = self.apply(plan)
        def starting():
            attempt = self.state()['cutover'].get('candidate_start')
            return (attempt and attempt['outcome'] == 'dispatched_or_unknown'
                    and (self.business / 'readiness.started').is_file()
                    and release.service(self.host, instance_units(self.entry)['http'])['ActiveState'] == 'active')
        wait_for(starting, 'Candidate never reached held readiness', timeout=90)
        result = self.call('stop')
        state = self.state()
        assert result['contained'] and result['next_action'] == 'cutover-drain-resolve'
        assert state['cutover']['operation_id'] == operation and state['cutover']['hold']
        assert state['cutover']['drain_failed'] and state['queue_compatibility']['permission'] == 'closed'
        assert state['queue_compatibility']['floor'] == 3
        assert release.cutover_executor(self.host, self.entry, operation) != 'running'
        assert release.enabled_roles(self.host, self.entry) == []
        self.stopped(review=True)
        self.call('stop')
        self.resolve(operation, 'Held fixture candidate was contained with no outstanding job work')
        (self.business / 'readiness.release').touch(exist_ok=False)
        self.call('cutover-resume', operation_id=operation, plan_token=plan['plan_token'])
        self.healthy(operation, plan)
        self.record('containment during held readiness, resolution and resume', operation)
        normal = self.call('stop')
        assert normal['status'] == 'stopped' and normal['next_action'] == 'restart'
        assert release.enabled_roles(self.host, self.entry) == []
        self.stopped()
        self.call('restart')
        prove_running(self.host, self.entry)
        self.record('normal completed release stop and restart', operation)

    def redis(self, *arguments):
        # Fixed Lua plus private stdin, including all keys and test values.
        return redis_acl._redis(self.host, 'EVAL', 'return redis.call(unpack(cjson.decode(ARGV[1])))',
                                '0', input=json.dumps(arguments))

    def restore(self):
        self.call('stop')
        self.stopped()
        inventory = release.inspect_job_queues(self.host, self.entry, selected(self.host, self.entry))
        keys = [key for target in contract.targets(inventory) for key in target['keys']]
        metadata = next(key for key in keys if key.endswith(':metadata'))
        extra = next(key for key in keys if key.endswith(':quarantine') and self.redis('EXISTS', key) == 0)
        expiry = int(self.redis('TIME')[0]) * 1000 + 3600000
        assert self.redis('PEXPIREAT', metadata, expiry) == 1
        baseline = hash_fields(self.redis('HGETALL', metadata))
        others = [entry for entry in self.host.read_registry()['instances'].values() if entry['name'] != self.entry['name']]
        prefix = next(prefix for prefix in others[0]['redis_grants']['keys'] if prefix.startswith('foundry:cache:'))
        sentinel = prefix + 'hostdrill:' + uuid.uuid4().hex
        assert self.redis('SET', sentinel, 'outside-owned-job-scopes', 'NX') == 'OK'
        plan = self.plan(self.candidate())
        operation = self.apply(plan)
        state = self.healthy(operation, plan)
        snapshot = state['cutover']['snapshot']['snapshot_id']
        self.call('stop')
        self.stopped()
        self.redis('HSET', metadata, 'hostdrill_modified', 'yes')
        self.redis('PERSIST', metadata)
        assert self.redis('SET', extra, 'planned-extra-key', 'NX') == 'OK'
        identities = list(WORK.glob('backup-*/replacement-age-identity.txt'))
        assert len(identities) == 1, 'Run exactly one existing backup/recipient rotation phase before fast recovery'
        identity = identities[0]
        restore_id = str(uuid.uuid4())
        restore_plan = self.call('redis-restore-plan', operation_id=restore_id, snapshot_id=snapshot,
                                 secrets={'age_identity': identity.read_text()})
        assert restore_plan['remove_keys'] >= 1 and restore_plan['restore_keys'] >= 1
        assert restore_plan['warning'] == 'restore_may_replay_external_side_effects'
        private = contract.private_path(self.host, self.entry, 'restore', restore_id).parent / 'operations' / restore_id
        assert private.stat().st_mode & 0o777 == 0o700 and not (private / 'restore.identity').exists()
        self.call('redis-restore-apply', operation_id=restore_id, plan_token=restore_plan['plan_token'])
        restored = self.wait_operation(restore_id)
        assert restored['cutover']['restore']['snapshot_id'] == snapshot
        assert restored['cutover']['safety_snapshot']['snapshot_id'] != snapshot
        assert restored['queue_compatibility']['floor'] == 3 and restored['queue_compatibility']['permission'] == 'closed'
        self.stopped()
        assert hash_fields(self.redis('HGETALL', metadata)) == baseline
        assert self.redis('PEXPIRETIME', metadata) == expiry
        assert self.redis('EXISTS', extra) == 0
        assert self.redis('GET', sentinel) == 'outside-owned-job-scopes'
        self.redis('DEL', sentinel)  # This exact key was created by this scenario.
        self.redis('PERSIST', metadata)  # Remove only the expiry this scenario added.
        self.record('real encrypted snapshot restore, exact expiry and outside-scope preservation', restore_id)
        plan = self.plan(self.candidate())
        operation = self.apply(plan)
        self.healthy(operation, plan)
        print('DEFERRED restore interruption/resume: no deterministic partial-write barrier in the fast slice', flush=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('phase', choices=('incident', 'fast'))
    parser.add_argument('--archive', type=Path, required=True)
    parser.add_argument('--old-tools', type=Path)
    args = parser.parse_args()
    guard()
    suite = Recovery(args.archive)
    if args.phase == 'incident':
        if args.old_tools is None:
            parser.error('incident requires --old-tools from the v0.15.0 tag')
        suite.incident(args.old_tools)
    else:
        before = suite.foreign()
        suite.successful_drain()
        suite.forced_drain()
        suite.forward_failure()
        suite.containment()
        suite.restore()
        assert suite.foreign() == before, 'Fast recovery changed a foreign/shared invocation'
    suite.finish()


if __name__ == '__main__':
    main()
