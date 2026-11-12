#!/usr/bin/env python3
"""Build the dedicated fixture through the normal deploy builder; never contacts a host."""
import argparse
import importlib.util
import os
import shutil
import subprocess
import uuid
from pathlib import Path


DEPLOY = Path(__file__).resolve().parents[1]
FIXTURE = DEPLOY.parents[1] / 'tests/fixtures/deploy-acceptance'


MODULE = 'github.com/weiloon1234/Foundry-Go'


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


def build_fixture(architecture, archive_file):
    archive_file = Path(archive_file).expanduser().absolute()
    if archive_file.exists() or archive_file.is_symlink():
        raise ValueError('Use a new archive path file; existing files are never overwritten')
    if not archive_file.parent.is_dir():
        raise ValueError('The archive path file needs an existing parent directory')
    spec = importlib.util.spec_from_file_location('acceptance_deploy_cli', DEPLOY / 'local/cli.py')
    cli = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(cli)
    cli.ROOT = cli.resolve_app_root(str(FIXTURE))
    project = cli.validate_project(cli.read_json(FIXTURE / 'deploy/project.json'))
    profile = cli.read_json(FIXTURE / 'deploy/environments/acceptance.json')
    profile['architecture'] = architecture
    profile = cli.validate_profile(profile, project)
    if profile['runtime_environment'] != 'testing' or profile['environment'] != 'acceptance':
        raise ValueError('The fixture builder accepts only its disposable acceptance profile')
    with cli.lock(FIXTURE / '.deploy/operation.lock'):
        workspace = DEPLOY.parents[1] / '.cache/deploy-acceptance' / str(uuid.uuid4())
        workspace.mkdir(parents=True, mode=0o700)
        # Retain inputs on failure: a Docker writer may still own their mounts.
        snapshot, proxy, _ = prepare_candidate(cli, workspace)
        cli.ROOT = snapshot
        cli.execute = acceptance_execute(cli.execute, snapshot, proxy)
        archive = cli.build(project, profile)
    with archive_file.open('x') as stream:
        stream.write(str(archive) + '\n')
    print('Fixture archive: ' + str(archive))
    print('Archive path file: ' + str(archive_file))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--architecture', choices=('arm64', 'amd64'), default='arm64')
    parser.add_argument('--archive-file', required=True, type=Path,
                        help='New file in which to retain the completed archive path')
    arguments = parser.parse_args()
    build_fixture(arguments.architecture, arguments.archive_file)


if __name__ == '__main__':
    main()
