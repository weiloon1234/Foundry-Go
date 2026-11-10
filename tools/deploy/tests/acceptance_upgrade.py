#!/usr/bin/env python3
"""Lead-only upgrade from the previous published tools on the disposable host."""
import argparse
import json
from pathlib import Path
import sys

sys.dont_write_bytecode = True
sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'lib'))
from contracts import digest
from topology import instance_units
from upgrades import protocol, tool_bundle
from acceptance_host import check_failed_units
from acceptance_roles import prove_running, second_archive
from acceptance_support import LAUNCHER, fixture, guard, request, run

TOOLS = Path(__file__).resolve().parents[1]


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


def check_report(host, report):
    failed = report['host']['failed_units']
    check_failed_units(host, failed)
    expected = [{'code': 'units_failed'}] if failed else []
    assert report['problems'] == expected, report['problems']


def upgrade(archive, old_tools):
    host, project, profile, entry = fixture()
    baseline = host.read_registry()
    assert baseline['tools']['generation'] == protocol(old_tools)['generation']
    assert baseline['tools']['bundle_sha256'] == tool_bundle(old_tools)[2]
    registration = baseline['instances'][profile['instance_id']]['client_generation']
    prove_running(host, entry)
    current = request(project, profile)
    plan = invoke('tools-plan', current, tools=TOOLS)
    assert plan['upgrade']['from_digest'] == baseline['tools']['bundle_sha256']
    assert plan['upgrade']['generation'] == protocol(TOOLS)['generation']
    assert baseline['tools']['bundle_sha256'] in plan['prune_bundles']
    applied = invoke('tools-apply', {**current, 'plan_token': plan['plan_token']}, tools=TOOLS)
    assert applied['removed_bundles'] == plan['prune_bundles']
    installed = host.read_registry()
    assert installed['tools']['bundle_sha256'] == tool_bundle(TOOLS)[2]
    assert installed['instances'][profile['instance_id']]['client_generation'] == registration
    active = host.paths.p(installed['tools']['active_path'])
    assert list(active.parent.iterdir()) == [active]
    repeated = invoke('tools-plan', current, tools=TOOLS)
    assert repeated['upgrade'] is None and repeated['prune_bundles'] == []
    assert tool_bundle(active)[2] == tool_bundle(TOOLS)[2]
    assert all(not (active / name).exists() for name in plan['upgrade']['removed_paths'])
    for unit in instance_units(entry).values():
        assert 'ExecStartPre=' not in Path('/etc/systemd/system', unit).read_text()
        loaded = host.run(['systemctl', 'show', unit, '--property=ExecStartPre,NeedDaemonReload', '--all']).stdout
        properties = dict(line.split('=', 1) for line in loaded.splitlines() if '=' in line)
        # systemd 255 omits an empty ExecStartPre even with --all.
        assert properties.get('NeedDaemonReload') == 'no' and properties.get('ExecStartPre', '') == ''
    candidate, version = second_archive(archive, project, profile)
    leftover = active.parent / ('f' * 64)
    leftover.mkdir()
    (leftover / 'prune-fixture.txt').write_text('acceptance-owned leftover\n')
    deployed = invoke('activate', {**current, 'archive': str(candidate), 'archive_sha256': digest(candidate)})
    assert deployed['removed_bundles'] == [leftover.name]
    assert list(active.parent.iterdir()) == [active]
    _, entry = host.require_identity(project, profile)
    assert host.load_state(entry)['current']['version'] == version
    prove_running(host, entry)
    check_report(host, invoke('report', current))
    print('PASS published tools upgrade: candidate inventory, loaded role units, healthy deployment and report', flush=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--archive', type=Path, required=True)
    parser.add_argument('--tools-root', type=Path, required=True)
    args = parser.parse_args()
    guard()
    upgrade(args.archive, args.tools_root)


if __name__ == '__main__':
    main()
