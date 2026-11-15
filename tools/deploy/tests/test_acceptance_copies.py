"""Numbered-copy acceptance fixture contracts; no manager or container needed."""
import builtins
import copy
import hashlib
import io
import json
from pathlib import Path
import sys
import tarfile
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import Mock, patch

import acceptance_feature as scenario
from contracts import ReleaseDeclaration, UnitIdentity, copy_socket_name, digest
import listeners
import services
from test_artifacts import elf
from test_contracts import fixture
from topology import parse_profile
import upgrades


class CopiesAcceptanceTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name).resolve()
        self.project, self.profile, entry = fixture()
        self.version = '20261101T000000Z-' + 'a' * 40
        self.source = self.root / 'source.tar.gz'
        manifest = {'project_id': self.project['id'], 'instance_id': self.profile['instance_id'],
                    'instance': entry['name'], 'environment': self.profile['environment'],
                    'architecture': 'amd64', 'version': self.version}
        files = {'manifest.json': json.dumps(manifest).encode(), 'artisan': elf(),
                 'config/deploy.toml': b'[app.services.storage.disks.attachments]\ndriver="r2"\n',
                 'public/index.html': b'fixture', 'private-source-maps/' + self.version + '/app.map': b'{}'}
        with tarfile.open(self.source, 'w:gz') as archive:
            for name, data in files.items():
                member = tarfile.TarInfo(name)
                member.size = len(data)
                archive.addfile(member, io.BytesIO(data))

    def build(self, count, *, version=None):
        output = self.root / f'copies-{count}.tar.gz'
        with patch.object(scenario, 'read_json', return_value=copy.deepcopy(self.profile)):
            project, profile, topology, _ = scenario.feature_archive(
                self.source, output, self.profile['host']['host_id'], confined=True, copies=count, version=version)
        return output, project, parse_profile(profile, topology), topology

    def test_three_copy_fixture_has_confined_ports_sockets_state_and_grants(self):
        _, _, profile, topology = self.build(3)
        role = next(role for role in topology.roles if role.common.name == scenario.COPIES_ROLE)
        self.assertEqual(3, dict(profile.placement)[scenario.COPIES_ROLE])
        self.assertEqual('loopback', role.common.confinement)
        self.assertTrue(dict(role.listeners)['control'].wildcard)
        self.assertEqual('http', role.readiness.kind)
        self.assertEqual('control', role.readiness.listener)
        self.assertEqual('copy_number', role.arguments[-1].kind)
        self.assertEqual('local-unprotected', role.state.kind)
        self.assertIn('socket-provider', role.common.requires)
        binding = topology.projection()['roles'][-1]['bindings']['socket']['endpoint']
        self.assertEqual({'role': 'socket-provider', 'listener': 'control', 'scheme': 'unix'}, binding)
        compile(role.arguments[2].value, 'copies-fixture', 'exec')
        compile(scenario.COPIES_PROGRAM, 'copies-program', 'exec')
        grants = topology.projection()['shared_directories']['render-cache']
        self.assertIn(scenario.COPIES_ROLE, grants['readers'])
        self.assertNotIn(scenario.COPIES_ROLE, grants['writers'])
        self.assertNotIn('socket-provider', grants['readers'] + grants['writers'])
        identities = tuple(UnitIdentity(profile.instance_id, scenario.COPIES_ROLE, n) for n in range(1, 4))
        pairs = tuple(pair for pair in listeners.private_listeners(topology, profile) if pair[0] in identities)
        allocated = listeners.allocate_private(profile.instance_id, pairs, (), (),
                                               wildcards=listeners.wildcard_listeners(topology))
        self.assertEqual({1, 2, 3}, {endpoint.ordinal for endpoint in allocated})
        self.assertEqual(3, len({endpoint.port for endpoint in allocated}))
        unix = [endpoint for endpoint in listeners.unix_endpoints(topology, profile) if endpoint.role == scenario.COPIES_ROLE]
        self.assertEqual([copy_socket_name('copies.sock', n) for n in range(1, 4)], [endpoint.name for endpoint in unix])
        self.assertEqual(3, len({services.ServicePaths.for_unit(identity, profile.instance_id).state for identity in identities}))
        self.assertEqual(17, upgrades.feature_generation(topology, {}, profile=profile))

    def test_count_variants_preserve_declarations_and_source_payload(self):
        original = digest(self.source)
        previous_topology = None
        for count in (3, 4, 2):
            version = '20261102T000000Z-' + str(count) * 40
            archive_path, _, profile, topology = self.build(count, version=version)
            if previous_topology is not None:
                self.assertEqual(previous_topology, topology.projection())
            previous_topology = topology.projection()
            with tarfile.open(archive_path, 'r:gz') as archive:
                manifest = json.load(archive.extractfile('manifest.json'))
                declaration = ReleaseDeclaration.parse(manifest['deployment'])
                self.assertEqual(count, manifest['deployment']['profile']['placement'][scenario.COPIES_ROLE])
                self.assertEqual(version, manifest['version'])
                self.assertEqual(b'{}', archive.extractfile('private-source-maps/' + version + '/app.map').read())
                self.assertNotIn('private-source-maps/' + self.version + '/app.map', archive.getnames())
                for member in declaration.inventory:
                    data = archive.extractfile(member.path).read()
                    self.assertEqual(member.sha256, hashlib.sha256(data).hexdigest())
                    self.assertEqual(member.size, len(data))
        self.assertEqual(original, digest(self.source))

    def test_default_fixture_retains_singleton_recovery_contract(self):
        _, _, profile, topology = self.build(0)
        self.assertNotIn(scenario.COPIES_ROLE, dict(profile.placement))
        self.assertEqual(16, upgrades.feature_generation(topology, {}, profile=profile))

    def test_lane_c_hook_is_optional_and_preserves_dependency_failures(self):
        with patch.dict(sys.modules, {'acceptance_copies_consumers': None}), patch('builtins.print') as output:
            scenario.check_copy_consumers('host', 'entry', 'inventory', phase='initial')
            self.assertIn('SKIP lane C', output.call_args.args[0])
        check = Mock()
        with patch.dict(sys.modules, {'acceptance_copies_consumers': SimpleNamespace(check=check)}):
            scenario.check_copy_consumers('host', 'entry', 'inventory', phase='shrink')
        check.assert_called_once_with('host', 'entry', 'inventory', phase='shrink')
        real_import = builtins.__import__
        def importing(name, *args, **kwargs):
            if name == 'acceptance_copies_consumers':
                raise ModuleNotFoundError('Missing lane C dependency', name='required_dependency')
            return real_import(name, *args, **kwargs)
        with patch('builtins.__import__', side_effect=importing), self.assertRaises(ModuleNotFoundError):
            scenario.check_copy_consumers('host', 'entry', 'inventory', phase='grow')


if __name__ == '__main__':
    unittest.main()
