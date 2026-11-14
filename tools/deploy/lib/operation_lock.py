"""Transfer an already-held host flock to a pinned recovery executor and cleanup.

SCM_RIGHTS duplicates the same open file description: the lock survives either
process dying. The caller waits without performing host work while it is lent.
"""
from array import array
from contextlib import contextmanager
import fcntl
import os
from pathlib import Path
import socket
import stat
import struct
import threading

import admission
from contracts import HOST_PATHS, DeployError, contract_uuid, require


def socket_path(host, operation):
    return host.paths.p('/run/foundry/operation-locks/' + contract_uuid(operation) + '.sock')


def peer(host, connection, operation):
    pid, uid, _ = struct.unpack('3i', connection.getsockopt(socket.SOL_SOCKET, socket.SO_PEERCRED, 12))
    require(uid == 0, 'operation_owner', 'Only the root operation executor may borrow the host lock.')
    group = '/foundry-operation-' + operation + '.service'
    lines = host.paths.p(f'/proc/{pid}/cgroup').read_text().splitlines()
    require(any(line.split(':', 2)[-1].endswith(group) for line in lines),
            'operation_owner', 'Host lock recipient is outside its pinned operation unit.')


@contextmanager
def lend(host, operation):
    require(host.host_lock_fd is not None, 'operation_locked', 'Recovery must retain its host lock.')
    path = socket_path(host, operation)
    admission.protected_directory(host, path.parent, 0o700)
    # A stale socket is never a live owner. The host lock serializes its lifetime.
    if path.exists():
        require(stat.S_ISSOCK(path.lstat().st_mode), 'operation_owner', 'Unexpected lock handoff path.')
        path.unlink()
    server = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
    stopping = threading.Event()
    borrowed = os.dup(host.host_lock_fd)
    def serve():
        while not stopping.is_set():
            try:
                connection, _ = server.accept()
            except socket.timeout:
                continue
            except OSError:
                return
            with connection:
                try:
                    connection.settimeout(5)
                    peer(host, connection, operation)
                    connection.sendmsg([b'host-lock-v1'], [(socket.SOL_SOCKET, socket.SCM_RIGHTS, array('i', [borrowed]))])
                except (DeployError, OSError):
                    pass  # No descriptor is sent to an unverified peer.
    try:
        server.bind(str(path))
        path.chmod(0o600)
        server.listen(2)
        server.settimeout(.2)
        thread = threading.Thread(target=serve, daemon=True)
        thread.start()
        try:
            yield
        finally:
            stopping.set()
            thread.join(timeout=6)
            require(not thread.is_alive(), 'operation_owner', 'Host lock handoff has not terminated.')
    finally:
        server.close()
        path.unlink(missing_ok=True)
        os.close(borrowed)


@contextmanager
def acquire(host, request, *, cleanup=False):
    if request['action'] != 'recover-activate':
        with host.lock('host'):
            yield
        return
    path = socket_path(host, request['operation_id'])
    received = []
    try:
        with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as connection:
            connection.settimeout(5)
            connection.connect(str(path))
            payload, ancillary, flags, _ = connection.recvmsg(32, socket.CMSG_SPACE(array('i').itemsize))
            for level, kind, data in ancillary:
                if level == socket.SOL_SOCKET and kind == socket.SCM_RIGHTS:
                    descriptors = array('i')
                    descriptors.frombytes(data)
                    received.extend(descriptors)
            require(payload == b'host-lock-v1' and flags == 0 and len(received) == 1,
                    'operation_owner', 'Invalid host lock handoff.')
    except OSError:
        for fd in received:
            os.close(fd)
        if not cleanup:
            raise DeployError('operation_locked', 'Recovery host-lock owner is unavailable; startup refused.') from None
        # After a disconnected parent/executor exits, independent cleanup owns
        # the same normal lock. It may never skip serialization.
        with host.lock('host'):
            yield
        return
    try:
        expected = host.paths.p(HOST_PATHS['locks']) / 'host.lock'
        info, source = os.fstat(received[0]), expected.stat()
        require(stat.S_ISREG(info.st_mode) and (info.st_dev, info.st_ino) == (source.st_dev, source.st_ino)
                and (info.st_uid == 0 or host.paths.root != Path('/')) and not info.st_mode & 0o077,
                'operation_owner', 'Borrowed descriptor is not the protected host lock.')
        fcntl.flock(received[0], fcntl.LOCK_EX | fcntl.LOCK_NB)
        host.host_lock_fd = received[0]
        yield
    finally:
        host.host_lock_fd = None
        for fd in received:
            os.close(fd)
