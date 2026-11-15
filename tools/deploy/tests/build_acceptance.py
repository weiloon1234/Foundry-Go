#!/usr/bin/env python3
"""Build the dedicated fixture through the normal deploy builder; never contacts a host."""
import argparse
import importlib.util
import os
import secrets
import shutil
import subprocess
import uuid
from pathlib import Path


DEPLOY = Path(__file__).resolve().parents[1]
FIXTURE = DEPLOY.parents[1] / 'tests/fixtures/deploy-acceptance'


MODULE = 'github.com/weiloon1234/Foundry-Go'
ENVIRONMENTS = ('acceptance', 'acceptance-lab', 'acceptance-run')


def prepare_candidate(cli, workspace):
    """Package the checkout, then rewrite only an isolated fixture copy."""
    version = 'v0.0.0-acceptance.' + workspace.name
    package = workspace / 'candidate'
    environment = {**os.environ, 'GOWORK': 'off'}
    go = os.environ.get('GO', 'go')
    subprocess.run([go, 'run', '.', '-root', str(DEPLOY.parents[1]), '-out', str(package),
                    '-version', version], cwd=DEPLOY.parent / 'release', env=environment,
                   check=True, timeout=600)
    cli.ROOT = FIXTURE
    inventory = cli.source_inventory()
    snapshot = workspace / 'fixture'
    for name, details in inventory.items():
        destination = snapshot / name
        destination.parent.mkdir(parents=True, exist_ok=True)
        if 'symlink' in details:
            destination.symlink_to(details['symlink'])
        else:
            shutil.copyfile(FIXTURE / name, destination, follow_symlinks=False)
            if destination.is_symlink() or cli.digest(destination) != details['sha256']:
                raise ValueError('Fixture changed during acceptance capture')
            destination.chmod(details['mode'])
    if cli.source_inventory() != inventory:
        raise ValueError('Fixture changed during acceptance capture')
    proxy = package / 'proxy'
    environment.update(GOPROXY=proxy.as_uri() + ',https://proxy.golang.org',
                       GONOSUMDB=MODULE, GONOPROXY='none', GOPRIVATE='', GOSUMDB='sum.golang.org')
    subprocess.run([go, 'mod', 'edit', '-dropreplace=' + MODULE, '-require=' + MODULE + '@' + version],
                   cwd=snapshot, env=environment, check=True, timeout=30)
    subprocess.run([go, 'mod', 'tidy'], cwd=snapshot, env=environment, check=True, timeout=600)
    return snapshot, proxy, environment


def acceptance_execute(execute, snapshot, proxy):
    """Keep original Git provenance and expose the proxy only to the build container."""
    def run(args, **kwargs):
        args = list(args)
        if args[:3] == ['git', '-C', str(snapshot)]:
            args[2] = str(FIXTURE)
        if args[:2] == ['docker', 'run'] and args[-2:] == ['bash', '/foundry-deploy/local/build.sh']:
            args[2:2] = ['--mount', f'type=bind,src={proxy},dst=/acceptance-proxy,readonly',
                         '-e', 'GOPROXY=file:///acceptance-proxy,https://proxy.golang.org',
                         '-e', 'GONOSUMDB=' + MODULE, '-e', 'GONOPROXY=none',
                         '-e', 'GOPRIVATE=', '-e', 'GOSUMDB=sum.golang.org']
        return execute(args, **kwargs)
    return run


def fixture_profile(cli, architecture, environment):
    if environment not in ENVIRONMENTS:
        raise ValueError('Select an explicit fixture acceptance profile')
    project = cli.validate_project(cli.read_json(FIXTURE / 'deploy/project.json'))
    declarations = FIXTURE / 'deploy/rehearsal' if environment == 'acceptance-run' else FIXTURE / 'deploy'
    profile = cli.read_json(declarations / 'environments' / (environment + '.json'))
    if environment != 'acceptance' and profile['architecture'] != architecture:
        raise ValueError('Rehearsal architecture must match its explicit fixture profile')
    profile['architecture'] = architecture
    profile = cli.validate_profile(profile, project)
    if profile['runtime_environment'] != 'testing' or profile['environment'] != environment:
        raise ValueError('The fixture builder accepts only its disposable acceptance profiles')
    if environment != 'acceptance':
        from rehearsal import acceptance_profile
        acceptance_profile(project, profile, cli.read_json(declarations / 'topology.json'))
    return project, profile


def prepare_rehearsal(snapshot, profile):
    """Install rehearsal declarations only in the privately packaged fixture copy."""
    from contracts import atomic_json, read_json
    from runtime_config import service_secret_requirements
    from topology import parse_profile, parse_topology
    declarations = snapshot / 'deploy/rehearsal'
    topology = parse_topology(read_json(declarations / 'topology.json'))
    parsed = parse_profile(profile, topology)
    shutil.copyfile(declarations / 'topology.json', snapshot / 'deploy/topology.json')
    atomic_json(snapshot / 'deploy/environments' / (profile['environment'] + '.json'), profile)
    bindings = read_json(declarations / 'bindings.json')
    bindings['configuration'] = {row['key']: secrets.token_urlsafe(32)
                                 for row in service_secret_requirements(topology, parsed, {})}
    atomic_json(snapshot / '.deploy/rehearsal-bindings.json', bindings, mode=0o600)


def build_fixture(architecture, archive_file, environment='acceptance', *, app_root_file=None):
    archive_file = Path(archive_file).expanduser().absolute()
    if archive_file.exists() or archive_file.is_symlink():
        raise ValueError('Use a new archive path file; existing files are never overwritten')
    if not archive_file.parent.is_dir():
        raise ValueError('The archive path file needs an existing parent directory')
    if app_root_file is not None:
        app_root_file = Path(app_root_file).expanduser().absolute()
        if app_root_file == archive_file or app_root_file.exists() or app_root_file.is_symlink() or not app_root_file.parent.is_dir():
            raise ValueError('Use a new app-root path file in an existing directory')
    spec = importlib.util.spec_from_file_location('acceptance_deploy_cli', DEPLOY / 'local/cli.py')
    cli = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(cli)
    cli.ROOT = cli.resolve_app_root(str(FIXTURE))
    project, profile = fixture_profile(cli, architecture, environment)
    with cli.lock(FIXTURE / '.deploy/operation.lock'):
        workspace = DEPLOY.parents[1] / '.cache/deploy-acceptance' / str(uuid.uuid4())
        workspace.mkdir(parents=True, mode=0o700)
        # Retain inputs on failure: a Docker writer may still own their mounts.
        snapshot, proxy, _ = prepare_candidate(cli, workspace)
        if environment == 'acceptance-run':
            prepare_rehearsal(snapshot, profile)
        cli.ROOT = snapshot
        cli.execute = acceptance_execute(cli.execute, snapshot, proxy)
        archive = cli.build(project, profile)
    with archive_file.open('x') as stream:
        stream.write(str(archive) + '\n')
    if app_root_file is not None:
        with app_root_file.open('x') as stream:
            stream.write(str(snapshot) + '\n')
    print('Fixture archive: ' + str(archive))
    print('Archive path file: ' + str(archive_file))
    print('Fixture app root: ' + str(snapshot))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--architecture', choices=('arm64', 'amd64'), default='arm64')
    parser.add_argument('--environment', choices=ENVIRONMENTS, default='acceptance')
    parser.add_argument('--archive-file', required=True, type=Path,
                        help='New file in which to retain the completed archive path')
    parser.add_argument('--app-root-file', type=Path, help='New file in which to retain the isolated fixture root')
    arguments = parser.parse_args()
    build_fixture(arguments.architecture, arguments.archive_file, arguments.environment, app_root_file=arguments.app_root_file)


if __name__ == '__main__':
    main()
