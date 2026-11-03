#!/usr/bin/env python3
"""Lead-only real PostgreSQL/Redis sizing and explicit resize acceptance."""
import importlib.util
from pathlib import Path
import sys
from unittest.mock import patch

sys.dont_write_bytecode = True
TOOLS = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(TOOLS / 'lib'))
from acceptance_support import fixture, guard, request, service
import capacity
import postgres
import redis_acl
import release


def verify_settings(host, memory):
    registry = host.read_registry()
    assert registry['shared']['postgresql']['capacity'] == capacity.postgres(memory)
    assert registry['shared']['redis']['capacity'] == capacity.redis(memory)
    # These compare pg_settings/CONFIG GET to the persisted values, including
    # PostgreSQL's normalized memory units, durability, TLS and Redis persistence.
    postgres.inspect(host, registry)
    redis_acl.inspect(host, registry)
    redis_acl._verify_runtime(host, registry['shared']['redis']['capacity'])


def verify(host, memory):
    verify_settings(host, memory)
    for entry in host.read_registry()['instances'].values():
        assert release.ready(host, entry), 'Instance failed readiness after capacity apply'


def main():
    guard()
    host, project, profile, _ = fixture()
    memory = capacity.memory_mib(host)
    verify(host, memory)
    spec = importlib.util.spec_from_file_location('acceptance_capacity_manage', TOOLS / 'server/manage.py')
    manage = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(manage)
    payload = request(project, profile)
    units = [host.read_registry()['shared'][name]['unit'] for name in ('postgresql', 'redis')]
    original = {unit: service(unit) for unit in units}
    # The disposable harness caps this host at 4 GiB; exercise both shrink and
    # grow, with the same action routing and verifiers as the installed launcher.
    target = 1950 if memory != 1950 else 3900
    assert memory >= target, 'Acceptance requires at least 1950 MiB of available RAM'
    with patch.object(capacity, 'memory_mib', return_value=target):
        plan = manage.execute(host, 'capacity-plan', payload)
        assert not plan['noop'] and set(plan['restart_units']) == set(units)
        result = manage.execute(host, 'capacity-apply', {**payload, 'plan_token': plan['plan_token']})
        assert result['ok']
        verify(host, target)
    resized = {unit: service(unit) for unit in units}
    assert all(resized[unit] != original[unit] for unit in units), 'Both services must restart'
    plan = manage.execute(host, 'capacity-plan', payload)
    assert not plan['noop']
    assert manage.execute(host, 'capacity-apply', {**payload, 'plan_token': plan['plan_token']})['ok']
    verify(host, memory)
    restored = {unit: service(unit) for unit in units}
    assert all(restored[unit] != resized[unit] for unit in units)
    plan = manage.execute(host, 'capacity-plan', payload)
    assert plan['noop'] and not plan['restart_units']
    assert manage.execute(host, 'capacity-apply', {**payload, 'plan_token': plan['plan_token']})['noop']
    assert restored == {unit: service(unit) for unit in units}, 'No-op must not restart services'
    print('PASS RAM sizing: effective settings, shrink/grow, service restarts, every instance ready, no-op')


if __name__ == '__main__':
    main()
