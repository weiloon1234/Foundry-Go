"""Numbered-copy acceptance fixture contracts; no manager or container needed."""
import ast
import builtins
import copy
import errno
import hashlib
import http.server
import io
import json
import os
from pathlib import Path
import socket
import sys
import tarfile
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import MagicMock, Mock, patch

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
        self.assertEqual('stop', role.common.maintenance)
        raw = topology.projection()
        self.assertEqual(raw['secrets']['copy-token']['scope'], 'copy')
        self.assertEqual(raw['roles'][0]['bindings']['copies'], {'copies': {'role': scenario.COPIES_ROLE,
            'listener': 'control', 'scheme': 'http', 'secret': 'copy-token'}})
        self.assertEqual(dict(role.secret_files), {'COPY_TOKEN_FILE': 'copy-token'})
        self.assertIn('socket-provider', role.common.requires)
        dependencies = {role.common.name: role.common.requires for role in topology.roles}
        self.assertEqual(('socket-provider',), dependencies[scenario.COPIES_ROLE])
        self.assertEqual(('socket-provider',), dependencies['relay'])
        self.assertEqual((), dependencies['socket-provider'])
        self.assertEqual(('relay', 'wildcard'), dependencies['http'])
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
        self.assertEqual(18, upgrades.feature_generation(topology, {}, profile=profile))

    def test_count_variants_preserve_declarations_and_source_payload(self):
        original = digest(self.source)
        previous_topology = None
        original_profile = None
        for step, count in enumerate((3, 4, 2, 4, 2, 3)):
            version = '20261102T000000Z-' + str(step) * 40
            archive_path, _, profile, topology = self.build(count, version=version)
            if original_profile is None:
                original_profile = profile.projection()
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
        self.assertEqual(original_profile, profile.projection())

    def test_default_fixture_retains_singleton_recovery_contract(self):
        _, _, profile, topology = self.build(0)
        self.assertNotIn(scenario.COPIES_ROLE, dict(profile.placement))
        self.assertEqual(16, upgrades.feature_generation(topology, {}, profile=profile))

    def test_lane_c_hook_is_required_and_preserves_failures(self):
        inventory = Mock()
        inventory.identity.return_value = 'http identity'
        with patch.object(scenario.acceptance_copies_consumers, 'check', return_value={'private': 'path'}) as check:
            result = scenario.check_copy_consumers('host', 'entry', inventory, previous_paths={'retained': 'path'})
        check.assert_called_once_with('host', 'entry', inventory, consumer='http identity',
            binding_name='copies', producer=scenario.COPIES_ROLE, secret_variable='COPY_TOKEN_FILE',
            previous_paths={'retained': 'path'}, check_console=True)
        self.assertEqual(result, {'private': 'path'})
        with patch.object(scenario.acceptance_copies_consumers, 'check', side_effect=RuntimeError('consumer failed')), self.assertRaises(RuntimeError):
            scenario.check_copy_consumers('host', 'entry', inventory)


class CopiesProgramTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.cache = Path(temporary.name)
        self.provider = Mock()
        self.provider.recv.return_value = b'unix-ready'
        connection = MagicMock()
        connection.__enter__.return_value = self.provider
        self.create = Mock(wraps=tempfile.mkstemp)
        self.open = Mock(wraps=os.open)
        self.namespace = {
            'errno': errno, 'http': http, 'json': json, 'Path': Path,
            'os': SimpleNamespace(open=self.open, close=os.close, unlink=os.unlink,
                                  getpid=os.getpid, O_WRONLY=os.O_WRONLY),
            'tempfile': SimpleNamespace(mkstemp=self.create),
            'socket': SimpleNamespace(AF_UNIX=socket.AF_UNIX, socket=Mock(return_value=connection)),
            'cache': str(self.cache), 'state': self.cache / 'state', 'ordinal': 1,
            'unix_path': '/copies.sock', 'provider': '/provider.sock',
        }
        # Run the embedded handler itself without starting its sockets or threads.
        handler, = [node for node in ast.parse(scenario.COPIES_PROGRAM).body
                    if isinstance(node, ast.ClassDef) and node.name == 'Handler']
        exec(compile(ast.Module(body=[handler], type_ignores=[]), 'copies-handler', 'exec'), self.namespace)

    def request(self, path):
        handler = object.__new__(self.namespace['Handler'])
        handler.path = path
        handler.wfile = io.BytesIO()
        handler.send_response = Mock()
        handler.end_headers = Mock()
        handler.do_GET()
        return handler.send_response.call_args.args[0], json.loads(handler.wfile.getvalue())

    def test_ready_before_writer_starts_never_accesses_frame(self):
        self.create.side_effect = OSError(errno.EROFS, 'read-only filesystem')
        with patch.dict(self.namespace, Path=Mock(side_effect=AssertionError('readiness accessed frame'))):
            status, body = self.request('/ready')
        self.assertEqual(200, status)
        self.assertTrue(body['shared_read_only'])
        self.assertTrue(body['provider_ready'])
        self.assertNotIn('frame_ready', body)
        self.create.assert_called_once_with(prefix='.readiness-', dir=str(self.cache))
        self.open.assert_not_called()
        self.provider.connect.assert_called_once_with('/provider.sock')
        self.provider.sendall.assert_called_once_with(b'ping')
        self.assertEqual([], list(self.cache.iterdir()))

    def test_ready_requires_erofs_and_unix_ready(self):
        for error, reply in ((errno.EACCES, b'unix-ready'), (errno.ENOENT, b'unix-ready'),
                             (errno.EROFS, b'not-ready')):
            with self.subTest(error=error, reply=reply):
                self.create.side_effect = OSError(error, 'probe failed')
                self.provider.recv.return_value = reply
                status, _ = self.request('/ready')
                self.assertEqual(503, status)

    def test_ready_rejects_writable_cache_and_removes_new_probe_only(self):
        frame = self.cache / 'frame.txt'
        frame.write_bytes(b'prepared-media')
        status, body = self.request('/ready')
        self.assertEqual(503, status)
        self.assertFalse(body['shared_read_only'])
        self.assertEqual([frame], list(self.cache.iterdir()))
        self.assertEqual(b'prepared-media', frame.read_bytes())
        self.open.assert_not_called()

    def test_frame_requires_written_content_and_read_only_mount(self):
        frame = self.cache / 'frame.txt'
        self.open.side_effect = OSError(errno.EROFS, 'read-only filesystem')
        for content in (None, b'wrong', b'prepared-media'):
            with self.subTest(content=content):
                if content is not None:
                    frame.write_bytes(content)
                status, body = self.request('/frame')
                self.assertEqual(200 if content == b'prepared-media' else 503, status)
                self.assertEqual(content == b'prepared-media', body['frame_ready'])
                self.assertTrue(body['shared_read_only'])
        self.open.assert_called_with(frame, os.O_WRONLY)
        self.create.assert_not_called()

    def test_frame_rejects_writable_file_or_other_permission_errors(self):
        frame = self.cache / 'frame.txt'
        frame.write_bytes(b'prepared-media')
        for error in (None, errno.EACCES):
            with self.subTest(error=error):
                self.open.side_effect = OSError(error, 'probe failed') if error else None
                status, body = self.request('/frame')
                self.assertEqual(503, status)
                self.assertTrue(body['frame_ready'])
                self.assertFalse(body['shared_read_only'])
                self.assertEqual(b'prepared-media', frame.read_bytes())


if __name__ == '__main__':
    unittest.main()
