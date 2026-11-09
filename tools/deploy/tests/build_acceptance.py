#!/usr/bin/env python3
"""Build the dedicated fixture through the normal deploy builder; never contacts a host."""
import argparse
import importlib.util
from pathlib import Path


DEPLOY = Path(__file__).resolve().parents[1]
FIXTURE = DEPLOY.parents[1] / 'tests/fixtures/deploy-acceptance'


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
