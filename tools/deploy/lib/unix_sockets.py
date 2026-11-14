"""Instance-owned runtime sockets under protected framework ancestry."""
import errno
import os
from pathlib import Path
import socket
import stat

from contracts import require, socket_directory
from topology import ExecutableRole, EndpointBinding


def uses_sockets(role):
    return (isinstance(role, ExecutableRole) and any(l.transport == "unix" for _, l in role.listeners)
            or any(isinstance(b, EndpointBinding) and b.scheme == "unix" for _, b in role.common.bindings))


def prepare(host, role, identity, uid, gid, *, remove_stale=False):
    """Verify the shared directory; unlink only this provider's stale socket leaves.

    The instance cannot replace the directory because its parent is protected.
    Leaf operations use that opened directory and never follow links.
    """
    if not uses_sockets(role):
        return
    from admission import protected_directory
    require(type(uid) is int and type(gid) is int and uid >= 0 and gid >= 0
            and (host.paths.root != Path("/") or uid > 0 and gid > 0),
            "service_account", "Sockets require the unprivileged instance account.")
    path = host.paths.p(socket_directory(identity.instance_id))
    protected_directory(host, path.parent)
    parent = os.open(path.parent, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        try:
            os.mkdir(path.name, mode=0o700, dir_fd=parent)
            created = True
        except FileExistsError:
            created = False
        fd = os.open(path.name, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=parent)
        try:
            if created:
                os.fchown(fd, uid, gid)
                os.fchmod(fd, 0o700)
            info = os.fstat(fd)
            require(info.st_uid == uid and info.st_gid == gid and stat.S_IMODE(info.st_mode) == 0o700,
                    "service_socket_directory", "Socket directory must belong to the instance account with mode 0700.")
            if remove_stale and isinstance(role, ExecutableRole):
                for _, listener in role.listeners:
                    if listener.transport != "unix":
                        continue
                    _remove_stale(fd, path, listener.socket)
        finally:
            os.close(fd)
    finally:
        os.close(parent)


def _remove_stale(directory_fd, directory, name):
    try:
        info = os.stat(name, dir_fd=directory_fd, follow_symlinks=False)
    except FileNotFoundError:
        return
    require(stat.S_ISSOCK(info.st_mode), "service_socket_type", "Declared socket path is occupied by a non-socket; retain it for inspection.")
    with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as connection:
        connection.settimeout(1)
        try:
            connection.connect(str(directory / name))
        except OSError as error:
            require(error.errno == errno.ECONNREFUSED, "service_socket_busy", "Socket liveness is uncertain; retain it for inspection.")
        else:
            require(False, "service_socket_busy", "Declared socket is still accepting connections.")
    current = os.stat(name, dir_fd=directory_fd, follow_symlinks=False)
    require(stat.S_ISSOCK(current.st_mode) and (info.st_dev, info.st_ino) == (current.st_dev, current.st_ino),
            "service_socket_changed", "Socket changed during preparation; retain it for inspection.")
    os.unlink(name, dir_fd=directory_fd)
