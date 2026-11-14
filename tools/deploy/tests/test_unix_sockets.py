"""Unix declaration, preparation and delivery contracts without containers."""
from dataclasses import replace
import copy
import os
from pathlib import Path
import socket
import stat
import sys
import tempfile
import time
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "lib"))

import contracts as c
import feature_maintenance
from host import Host, Paths
import services
import topology as t
import unix_sockets
import upgrades
from test_services import inventory_fixture
from test_host_capabilities import DIGEST, INSTANCE, CURRENT


def unix_inventory():
    old, entry, members = inventory_fixture()
    raw = old.topology.projection()
    provider = raw['roles'][1]
    provider.update(requires=[], configs={}, arguments=[{'listener_socket_path': 'control'}],
                    listeners={'control': {'transport': 'unix', 'socket': 'wl0'}},
                    readiness={'kind': 'unix', 'listener': 'control', 'timeout_seconds': 2},
                    environment={'XDG_RUNTIME_DIR': {'listener_socket_directory': 'control'}})
    for role in (raw['roles'][0], raw['roles'][2]):
        role.update(requires=['relay'], bindings={'compositor': {'endpoint': {
            'role': 'relay', 'listener': 'control', 'scheme': 'unix'}}})
    topology = t.parse_topology(raw)
    endpoint = c.UnixEndpoint.for_listener(INSTANCE, 'relay', 'control', 'wl0')
    specs = []
    for role, document in zip(topology.roles, raw['roles']):
        spec = old.specs[role.common.name]
        binding = c.ResolvedEndpointBinding('compositor', endpoint, 'unix')
        bindings = c.RoleBindings(role.common.name, (), () if role.common.name == 'relay' else (binding,), ())
        specs.append(replace(spec, declaration_sha256=c.canonical_digest(document), bindings=bindings,
                             arguments=(endpoint.path,) if role.common.name == 'relay' else (),
                             configuration_sha256=services.configuration_digest(role, ()) if role.common.name == 'relay' else DIGEST))
    deployment = replace(old.deployment, topology_sha256=topology.sha256, units=tuple(specs))
    resolution = replace(old.resolution, deployment_sha256=c.canonical_digest(c.record_document(deployment)),
                         endpoints=tuple(e for e in old.resolution.endpoints if e.role == 'http'), unix_endpoints=(endpoint,))
    return services.UnitInventory(old.name, topology, old.profile, deployment, resolution), entry


class UnixDeclarationsTests(unittest.TestCase):
    def test_round_trip_private_transport_and_explicit_references(self):
        inventory, _ = unix_inventory()
        role = inventory.roles['relay']
        self.assertEqual(dict(role.listeners)['control'].transport, 'unix')
        self.assertEqual(c.RuntimeResolution.parse(c.record_document(inventory.resolution)), inventory.resolution)
        self.assertEqual(c.Deployment.parse(c.record_document(inventory.deployment)), inventory.deployment)
        self.assertEqual(inventory.start_order(inventory.roles)[0], 'relay')
        self.assertEqual(len(inventory.resolution.endpoints), 1)
        self.assertEqual(inventory.profile.listeners, ())
        paths = services.ServicePaths.for_unit(inventory.specs['relay'].identity, CURRENT)
        endpoint = inventory.resolution.unix_endpoints[0]
        for field in ('directory', 'name', 'path'):
            argument = t.Argument.parse({'listener_socket_' + field: 'control'})
            self.assertEqual(services.resolve_argument(argument, role, inventory.specs['relay'], inventory.resolution,
                                                     paths, (), {}), getattr(endpoint, field))
            argument = t.Argument.parse({'binding_socket_' + field: 'compositor'})
            self.assertEqual(services.resolve_argument(argument, inventory.roles['http'], inventory.specs['http'],
                                                     inventory.resolution, paths, (), {}), getattr(endpoint, field))
        self.assertEqual(services.resolve_argument(t.Argument.parse({'binding': 'compositor'}), inventory.roles['http'],
            inventory.specs['http'], inventory.resolution, paths, (), {}), endpoint.path)

    def test_transport_mismatch_unsafe_names_and_missing_dependency_refuse(self):
        inventory, _ = unix_inventory()
        valid = inventory.topology.projection()
        mutations = [
            lambda r: r['roles'][1]['listeners']['control'].update(bind='public'),
            lambda r: r['roles'][1]['listeners']['control'].update(tls='none'),
            lambda r: r['roles'][1]['readiness'].update(kind='tcp'),
            lambda r: r['roles'][1]['readiness'].update(kind='http', path='/ready', status=200),
            lambda r: r['roles'][1]['readiness'].update(path='/ready'),
            lambda r: r['roles'][1].update(arguments=[{'listener_port': 'control'}]),
            lambda r: r['roles'][1].update(environment={'XDG_RUNTIME_DIR': {'listener_socket_directory': 'missing'}}),
            lambda r: r['roles'][0].update(requires=[]),
            lambda r: r['roles'][0]['bindings']['compositor']['endpoint'].update(scheme='http'),
            lambda r: r['roles'][1]['listeners'].update(other={'transport': 'unix', 'socket': 'wl0'}),
            lambda r: r['roles'][1].update(arguments=[{'binding_socket_path': 'missing'}]),
            lambda r: r['roles'][0]['bindings'].update(public={'public_listener': {'role': 'relay', 'listener': 'control'}}),
        ]
        for name in ('../wl0', 'a/b', '.hidden', '.', '..', '', 'a' * 33, 'wl 0', 'wl\x00', '雪'):
            mutations.append(lambda r, name=name: r['roles'][1]['listeners']['control'].update(socket=name))
        for mutate in mutations:
            raw = copy.deepcopy(valid)
            mutate(raw)
            with self.subTest(raw=raw), self.assertRaises(c.DeployError):
                t.parse_topology(raw)
        raw = copy.deepcopy(valid)
        raw['roles'][1]['listeners']['control'] = {'transport': 'tcp', 'bind': 'loopback'}
        with self.assertRaises(c.DeployError):
            t.parse_topology(raw)

    def test_generation_floor_is_sticky_and_old_hosts_request_upgrade(self):
        inventory, _ = unix_inventory()
        self.assertEqual(upgrades.feature_generation(inventory.topology, {}), 15)
        with self.assertRaisesRegex(c.DeployError, 'Upgrade.*generation 15'):
            upgrades.check_feature_generation({'tools': {'generation': 14}}, inventory.topology, {}, client_generation=15)
        with self.assertRaisesRegex(c.DeployError, 'client generation 15'):
            upgrades.check_feature_generation({'tools': {'generation': 15}}, inventory.topology, {}, client_generation=14)
        self.assertEqual(upgrades.feature_generation(inventory_fixture()[0].topology,
                         {'deployment_selection': {'minimum_generation': 15}}), 15)
        self.assertIn('lib/unix_sockets.py', upgrades.required_files(15))
        self.assertNotIn('lib/unix_sockets.py', upgrades.required_files(14))

    def test_existing_tcp_resolution_keeps_its_wire_shape(self):
        inventory, _, _ = inventory_fixture()
        wire = c.record_document(inventory.resolution)
        self.assertNotIn('unix_endpoints', wire)
        self.assertEqual(c.record_document(c.RuntimeResolution.parse(wire)), wire)

    def test_resolutions_refuse_cross_instance_and_conflicting_records(self):
        inventory, _ = unix_inventory()
        wire = c.record_document(inventory.resolution)
        wire['unix_endpoints'].append(copy.deepcopy(wire['unix_endpoints'][0]))
        with self.assertRaises(c.DeployError):
            c.RuntimeResolution.parse(wire)
        endpoint = replace(inventory.resolution.unix_endpoints[0], directory='/tmp', path='/tmp/wl0')
        with self.assertRaises(c.DeployError):
            c.UnixEndpoint.parse(c.record_document(endpoint))
        endpoint = c.UnixEndpoint.for_listener('00000000-0000-4000-8000-000000000002', 'relay', 'control', 'wl0')
        with self.assertRaises(c.DeployError):
            services.UnitInventory(inventory.name, inventory.topology, inventory.profile, inventory.deployment,
                                   replace(inventory.resolution, unix_endpoints=(endpoint,)))

    def test_unix_endpoint_counts_as_maintenance_prerequisite(self):
        inventory, _ = unix_inventory()
        raw = inventory.topology.projection()
        raw['roles'][1]['maintenance'] = 'stop'
        with self.assertRaisesRegex(c.DeployError, 'endpoint binding'):
            feature_maintenance.stopped_roles(t.parse_topology(raw))

    def test_provider_and_builtin_consumers_receive_only_socket_write_access(self):
        inventory, entry = unix_inventory()
        directory = inventory.resolution.unix_endpoints[0].directory
        for name in inventory.roles:
            rendered = services.render_unit(inventory, name, entry, DIGEST)
            writes = [line for line in rendered.splitlines() if line.startswith('ReadWritePaths=')]
            self.assertTrue(any(directory in line for line in writes))
            self.assertIn('ProtectSystem=strict', rendered)
            self.assertIn('PrivateTmp=yes', rendered)
            self.assertNotIn('Wants=' + inventory.units['relay'], rendered)
        self.assertIn('XDG_RUNTIME_DIR=' + directory, services.render_unit(inventory, 'relay', entry, DIGEST))


class UnixPreparationTests(unittest.TestCase):
    def setUp(self):
        # Keep real AF_UNIX paths below macOS's smaller sockaddr_un bound.
        temporary = tempfile.TemporaryDirectory(prefix='u', dir='/tmp')
        self.addCleanup(temporary.cleanup)
        self.host = Host(Paths(Path(temporary.name)))
        self.inventory, self.entry = unix_inventory()
        self.role = self.inventory.roles['relay']
        self.identity = self.inventory.specs['relay'].identity
        self.directory = self.host.paths.p(c.socket_directory(INSTANCE))
        self.path = self.directory / 'wl0'
        self.prepare()

    def prepare(self, role=None, *, remove_stale=False):
        unix_sockets.prepare(self.host, role or self.role, self.identity, os.getuid(), os.getgid(), remove_stale=remove_stale)

    def socket(self, name='wl0', *, listening=False):
        server = socket.socket(socket.AF_UNIX)
        self.addCleanup(server.close)
        server.bind(str(self.directory / name))
        if listening:
            server.listen(16)
        return server

    def test_directory_creation_verification_and_exact_stale_cleanup(self):
        info = self.directory.lstat()
        self.assertEqual((stat.S_IMODE(info.st_mode), info.st_uid, info.st_gid), (0o700, os.getuid(), os.getgid()))
        self.socket().close()
        self.socket('other').close()
        (self.directory / 'notes').write_text('keep')
        self.prepare(self.inventory.roles['http'], remove_stale=True)
        self.assertTrue(self.path.exists())
        self.prepare(remove_stale=True)
        self.assertFalse(self.path.exists())
        self.assertTrue((self.directory / 'other').exists())
        self.assertEqual((self.directory / 'notes').read_text(), 'keep')
        self.prepare(remove_stale=True)
        server = self.socket(listening=True)
        with self.assertRaisesRegex(c.DeployError, 'still accepting'):
            self.prepare(remove_stale=True)
        self.assertTrue(self.path.exists())
        server.close()
        self.prepare(remove_stale=True)
        self.socket(listening=True)

    def test_non_socket_leaves_and_directory_links_are_never_removed(self):
        target = self.directory / 'target'
        target.write_text('keep')
        for kind in ('file', 'link', 'directory'):
            if kind == 'file':
                self.path.write_text('keep')
            elif kind == 'link':
                self.path.symlink_to(target)
            else:
                self.path.mkdir()
            with self.subTest(kind=kind), self.assertRaises(c.DeployError):
                self.prepare(remove_stale=True)
            self.assertEqual(target.read_text(), 'keep')
            self.path.rmdir() if kind == 'directory' else self.path.unlink()
        target.unlink()
        self.directory.rmdir()
        self.directory.symlink_to(self.host.paths.root, target_is_directory=True)
        with self.assertRaises((OSError, c.DeployError)):
            self.prepare()
        self.assertTrue(self.directory.is_symlink())

    def test_wrong_directory_mode_or_owner_fails_without_repair(self):
        os.chmod(self.directory, 0o755)
        with self.assertRaises(c.DeployError):
            self.prepare()
        self.assertEqual(stat.S_IMODE(self.directory.stat().st_mode), 0o755)
        os.chmod(self.directory, 0o700)
        with self.assertRaises(c.DeployError):
            unix_sockets.prepare(self.host, self.role, self.identity, os.getuid() + 1, os.getgid())

    def test_real_unix_readiness_connects_and_respects_deadline(self):
        endpoint = replace(self.inventory.resolution.unix_endpoints[0], path=str(self.path))
        resolution = replace(self.inventory.resolution, unix_endpoints=(endpoint,))
        self.assertFalse(services.probe(self.role, resolution, time.monotonic() + 1))
        self.socket().close()
        self.assertFalse(services.probe(self.role, resolution, time.monotonic() + 1))
        self.prepare(remove_stale=True)
        self.socket(listening=True)
        self.assertTrue(services.probe(self.role, resolution, time.monotonic() + 1))
        self.assertFalse(services.probe(self.role, resolution, time.monotonic() - 1))


if __name__ == '__main__':
    unittest.main()
