"""The unprivileged helper's Linux syscall boundary, including deterministic races."""
import errno
import io
import os
import pwd
import tempfile
from pathlib import Path
import socket
import stat
import struct
import sys
import unittest
from types import SimpleNamespace
from unittest.mock import MagicMock, patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'lib'))
import unix_socket_probe as probe


class UnixSocketProbeTests(unittest.TestCase):
    def setUp(self):
        self.uid, self.gid, self.pid = 1001, 1002, 42
        self.group = '/system.slice/provider.service'
        self.directory = SimpleNamespace(st_mode=stat.S_IFDIR | 0o700, st_uid=self.uid, st_gid=self.gid)
        self.leaf = SimpleNamespace(st_mode=stat.S_IFSOCK | 0o600, st_dev=1, st_ino=2)
        self.connection = MagicMock()
        self.connection.__enter__.return_value = self.connection
        self.connection.getsockopt.side_effect = [struct.pack('3i', self.pid, self.uid, self.gid), 12]
        self.poll = MagicMock()
        self.poll.poll.return_value = []
        self.enterContext(patch.object(probe.os, 'geteuid', return_value=self.uid))
        self.enterContext(patch.object(probe.os, 'getegid', return_value=self.gid))
        self.enterContext(patch.object(probe.os, 'O_PATH', getattr(os, 'O_PATH', 0x200000), create=True))
        self.open = self.enterContext(patch.object(probe.os, 'open', side_effect=[10, 11]))
        self.stat = self.enterContext(patch.object(probe.os, 'stat', return_value=self.leaf))
        self.fstat = self.enterContext(patch.object(probe.os, 'fstat', side_effect=[self.directory, self.leaf]))
        self.close = self.enterContext(patch.object(probe.os, 'close'))
        self.socket = self.enterContext(patch.object(probe.socket, 'socket', return_value=self.connection))
        self.enterContext(patch.object(probe.socket, 'SO_PEERCRED', getattr(socket, 'SO_PEERCRED', 17), create=True))
        self.enterContext(patch.object(probe.select, 'poll', return_value=self.poll, create=True))
        self.read = self.enterContext(patch('builtins.open', side_effect=lambda *a, **k: io.BytesIO(('0::' + self.group + '\n').encode())))

    def invoke(self, **kwargs):
        return probe.probe('/instance/sockets', 'wl0', self.uid, self.gid, .2, **{'group': self.group, **kwargs})

    def test_connected_peer_uid_and_unit_accept_with_pinned_inode_and_pidfd(self):
        self.assertEqual(self.invoke(), probe.Result.READY)
        self.assertEqual(self.open.call_args_list[0].args[1], os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
        self.assertEqual(self.open.call_args_list[1].args, ('wl0', os.O_PATH | os.O_NOFOLLOW))
        self.assertEqual(self.open.call_args_list[1].kwargs, {'dir_fd': 10})
        self.connection.connect.assert_called_once_with('/proc/self/fd/11')
        self.connection.settimeout.assert_called_once_with(.2)
        self.read.assert_called_once_with('/proc/42/cgroup', 'rb')
        self.assertEqual([call.args[0] for call in self.close.call_args_list], [12, 11, 10])
        self.assertTrue(all(call.kwargs == {'dir_fd': 10, 'follow_symlinks': False} for call in self.stat.call_args_list))

    def test_peer_uid_mismatch_is_not_ready(self):
        self.connection.getsockopt.side_effect = [struct.pack('3i', self.pid, self.uid + 1, self.gid)]
        self.assertEqual(self.invoke(), probe.Result.UNSAFE)
        self.read.assert_not_called()

    def test_peer_in_different_unit_is_not_ready_even_with_same_uid(self):
        self.read.side_effect = lambda *a, **k: io.BytesIO(b'0::/system.slice/provider.service-foreign\n')
        self.assertEqual(self.invoke(), probe.Result.UNSAFE)

    def test_peer_in_nested_unit_cgroup_is_accepted(self):
        self.read.side_effect = lambda *a, **k: io.BytesIO((f'0::{self.group}/child\n').encode())
        self.assertEqual(self.invoke(), probe.Result.READY)

    def test_peer_exit_during_inspection_cannot_validate_reused_pid(self):
        self.poll.poll.side_effect = [[], [(12, 1)]]
        self.assertEqual(self.invoke(), probe.Result.UNSAFE)

    def test_unavailable_peer_pidfd_fails_closed(self):
        self.connection.getsockopt.side_effect = [struct.pack('3i', self.pid, self.uid, self.gid), OSError(errno.ENOPROTOOPT, 'unsupported')]
        self.assertEqual(self.invoke(), probe.Result.UNSAFE)
        self.read.assert_not_called()

    def test_symlink_swap_between_stat_and_open_is_rejected_before_connection(self):
        link = SimpleNamespace(st_mode=stat.S_IFLNK | 0o777, st_dev=1, st_ino=3)
        self.fstat.side_effect = [self.directory, link]
        self.assertEqual(self.invoke(), probe.Result.UNSAFE)
        self.socket.assert_not_called()

    def test_replacement_after_open_uses_pinned_socket_and_does_not_report_ready(self):
        link = SimpleNamespace(st_mode=stat.S_IFLNK | 0o777, st_dev=1, st_ino=3)
        self.stat.side_effect = [self.leaf, link]
        self.assertEqual(self.invoke(), probe.Result.UNSAFE)
        self.connection.connect.assert_called_once_with('/proc/self/fd/11')
        self.connection.getsockopt.assert_not_called()

    def test_stale_probe_requires_the_exact_snapshot_inode(self):
        self.assertEqual(self.invoke(group='', expected=(1, 999)), probe.Result.UNSAFE)
        self.socket.assert_not_called()

    def test_connection_refused_is_the_only_stale_result(self):
        self.connection.connect.side_effect = OSError(errno.ECONNREFUSED, 'refused')
        self.assertEqual(self.invoke(group='', expected=(1, 2)), probe.Result.STALE)

    def test_permission_denial_is_not_stale(self):
        self.connection.connect.side_effect = PermissionError(errno.EACCES, 'denied')
        self.assertEqual(self.invoke(group='', expected=(1, 2)), probe.Result.UNSAFE)

    def test_live_cleanup_probe_never_authorizes_unlink(self):
        self.assertEqual(self.invoke(group='', expected=(1, 2)), probe.Result.LIVE)

    def test_missing_privilege_drop_refuses_before_opening_directory(self):
        with patch.object(probe.os, 'geteuid', return_value=0):
            self.assertEqual(self.invoke(), probe.Result.UNSAFE)
        self.open.assert_not_called()

    def test_root_account_is_always_refused(self):
        self.uid = self.gid = 0
        with patch.object(probe.os, 'geteuid', return_value=0), patch.object(probe.os, 'getegid', return_value=0):
            self.assertEqual(self.invoke(), probe.Result.UNSAFE)
        self.open.assert_not_called()

    def test_directory_must_retain_instance_owner_and_private_mode(self):
        self.directory.st_uid += 1
        self.assertEqual(self.invoke(), probe.Result.UNSAFE)
        self.socket.assert_not_called()


@unittest.skipUnless(sys.platform == 'linux', 'requires Linux O_PATH and SO_PEERPIDFD')
class LinuxSocketProbeTests(unittest.TestCase):
    def test_real_pinned_socket_credentials_and_symlink_swap_after_privilege_drop(self):
        from host import Host
        with tempfile.TemporaryDirectory(prefix='unix-probe-', dir='/tmp') as temporary:
            root = Path(temporary)
            root.chmod(0o755)
            account = pwd.getpwnam('nobody') if os.getuid() == 0 else pwd.getpwuid(os.getuid())
            owned = root / 'owned'
            owned.mkdir(mode=0o700)
            os.chown(owned, account.pw_uid, account.pw_gid)
            script = r"""
import importlib.util, os, socket, sys
from unittest.mock import patch
source, directory, uid, gid = sys.argv[1:]
spec = importlib.util.spec_from_file_location('probe', source)
probe = importlib.util.module_from_spec(spec)
spec.loader.exec_module(probe)
uid, gid = int(uid), int(gid)
if os.geteuid() == 0:
    os.setgroups([])
    os.setgid(gid)
    os.setuid(uid)
assert os.geteuid() == uid != 0
path = directory + '/provider'
victim = directory + '/other-instance'
with socket.socket(socket.AF_UNIX) as provider, socket.socket(socket.AF_UNIX) as other:
    provider.bind(path)
    provider.listen(16)
    other.bind(victim)
    other.listen(16)
    assert probe.probe(directory, 'provider', uid, gid, .2) == probe.Result.LIVE
    with socket.socket(socket.AF_UNIX) as client:
        client.connect(path)
        # Verify real credentials; a different uid is rejected before cgroup inspection.
        assert not probe.peer_matches(client, uid + 1, '/system.slice/fixture.service')
        # Obtain a real peer pidfd while inspecting the kernel-owned proc record.
        assert not probe.peer_matches(client, uid, '/system.slice/absent-fixture.service')
    assert probe.probe(directory, 'provider', uid, gid, .2, group='/system.slice/absent-fixture.service') == probe.Result.UNSAFE
    class Swap(socket.socket):
        def connect(self, address):
            os.unlink(path)
            os.symlink(victim, path)
            return super().connect(address)
    with patch.object(probe.socket, 'socket', Swap):
        assert probe.probe(directory, 'provider', uid, gid, .2) == probe.Result.UNSAFE
    other.settimeout(.05)
    try:
        other.accept()
    except TimeoutError:
        pass
    else:
        raise AssertionError('followed substituted symlink')
    assert probe.probe(directory, 'provider', uid, gid, .2) == probe.Result.UNSAFE
    os.unlink(path)
with socket.socket(socket.AF_UNIX) as stale:
    stale.bind(path)
assert probe.probe(directory, 'provider', uid, gid, .2) == probe.Result.STALE
"""
            result = Host().run([sys.executable, '-I', '-c', script, str(Path(probe.__file__).resolve()),
                                 str(owned), str(account.pw_uid), str(account.pw_gid)], timeout=5, check=False)
            self.assertEqual(result.returncode, 0, result.stderr)


if __name__ == '__main__':
    unittest.main()
