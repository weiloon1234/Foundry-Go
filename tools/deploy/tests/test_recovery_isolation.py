"""Private recovery ownership and fail-closed isolation, without host actions."""
import fcntl
import os
from pathlib import Path
import subprocess
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch
import uuid

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'lib'))
from contracts import DeployError, HOST_PATHS
from host import Host, Paths
import operation_lock
import recovery_rehearsal


class RecoveryIsolationTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory(prefix='b24-', dir='/tmp')
        self.addCleanup(temporary.cleanup)
        self.host = Host(Paths(Path(temporary.name)))
        self.inventory = SimpleNamespace(units={'relay': 'foundry-fixture-relay.service'})
        self.properties = {'IPAddressDeny': '0.0.0.0/0 ::/0', 'IPAddressAllow': '127.0.0.0/8 ::1/128',
                           'ControlGroup': '/system.slice/foundry-fixture-relay.service', 'DropInPaths': ''}

    def observe(self):
        output = '\n'.join(k + '=' + v for k, v in self.properties.items())
        with patch.object(self.host, 'run', return_value=subprocess.CompletedProcess([], 0, output, '')):
            return recovery_rehearsal.isolation(self.host, self.inventory, 'relay')

    def test_loaded_loopback_policy_requires_attached_ingress_and_egress(self):
        with patch.object(recovery_rehearsal, 'attached_programs', return_value=[[17], [18]]) as query:
            self.assertEqual(self.observe()['programs'], [[17], [18]])
            query.assert_called_once_with(self.host, self.properties['ControlGroup'])
        with patch.object(recovery_rehearsal, 'attached_programs', side_effect=DeployError('rehearsal_isolation', 'No BPF')):
            with self.assertRaises(DeployError):
                self.observe()

    def test_override_missing_family_or_wrong_cgroup_never_reaches_attachment_proof(self):
        for key, value in (('IPAddressDeny', '0.0.0.0/0'), ('IPAddressAllow', '127.0.0.0/8'),
                           ('IPAddressAllow', '127.0.0.0/8 ::1/128 10.0.0.0/8'),
                           ('DropInPaths', '/etc/systemd/system/override.conf'),
                           ('ControlGroup', '/system.slice/another.service')):
            with self.subTest(key=key, value=value), patch.dict(self.properties, {key: value}), \
                    patch.object(recovery_rehearsal, 'attached_programs') as query:
                with self.assertRaises(DeployError) as raised:
                    self.observe()
                self.assertEqual(raised.exception.code, 'rehearsal_isolation')
                query.assert_not_called()

    def test_missing_cgroup_v2_refuses(self):
        with self.assertRaises(DeployError) as raised:
            recovery_rehearsal.attached_programs(self.host, self.properties['ControlGroup'])
        self.assertEqual(raised.exception.code, 'rehearsal_isolation')

    def test_descriptor_handoff_retains_one_exclusive_host_lock(self):
        request = {'action': 'recover-activate', 'operation_id': str(uuid.uuid4())}
        receiver = Host(self.host.paths)
        with self.host.lock('host'), patch.object(operation_lock, 'peer') as authenticated:
            with operation_lock.lend(self.host, request['operation_id']):
                with operation_lock.acquire(receiver, request):
                    self.assertIsNotNone(receiver.host_lock_fd)
                    self.assertEqual(os.fstat(self.host.host_lock_fd).st_ino, os.fstat(receiver.host_lock_fd).st_ino)
                    path = self.host.paths.p(HOST_PATHS['locks']) / 'host.lock'
                    with path.open('rb') as competitor:
                        with self.assertRaises(BlockingIOError):
                            fcntl.flock(competitor, fcntl.LOCK_EX | fcntl.LOCK_NB)
                self.assertIsNone(receiver.host_lock_fd)
            authenticated.assert_called_once()
            with path.open('rb') as competitor:
                with self.assertRaises(BlockingIOError):
                    fcntl.flock(competitor, fcntl.LOCK_EX | fcntl.LOCK_NB)
        with path.open('rb') as competitor:
            fcntl.flock(competitor, fcntl.LOCK_EX | fcntl.LOCK_NB)

    def test_missing_broker_refuses_start_but_cleanup_acquires_normal_lock(self):
        request = {'action': 'recover-activate', 'operation_id': str(uuid.uuid4())}
        with self.assertRaises(DeployError) as raised:
            with operation_lock.acquire(self.host, request):
                self.fail('Unowned startup')
        self.assertEqual(raised.exception.code, 'operation_locked')
        with operation_lock.acquire(self.host, request, cleanup=True):
            self.assertIsNotNone(self.host.host_lock_fd)

    def test_wrong_peer_receives_no_lock(self):
        request = {'action': 'recover-activate', 'operation_id': str(uuid.uuid4())}
        with self.host.lock('host'), patch.object(operation_lock, 'peer', side_effect=DeployError('operation_owner', 'Wrong unit')):
            with operation_lock.lend(self.host, request['operation_id']), self.assertRaises(DeployError):
                with operation_lock.acquire(Host(self.host.paths), request):
                    self.fail('Unverified peer acquired the host lock')
