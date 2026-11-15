"""Instance-owned runtime sockets under protected framework ancestry."""
import os
from pathlib import Path
import pwd
import time
import stat

from contracts import DeployError, copy_socket_name, open_directory, require, socket_directory
from unix_socket_probe import Result
from topology import ExecutableRole, EndpointBinding, CopiesBinding


def uses_sockets(role):
    return (isinstance(role, ExecutableRole) and any(l.transport == "unix" for _, l in role.listeners)
            or any(isinstance(b, (EndpointBinding, CopiesBinding)) and b.scheme == "unix" for _, b in role.common.bindings))


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
    with open_directory(path.parent) as parent:
        try:
            os.mkdir(path.name, mode=0o700, dir_fd=parent)
            created = True
        except FileExistsError:
            created = False
        with open_directory(path.name, dir_fd=parent) as fd:
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
                    _remove_stale(host, fd, path, copy_socket_name(listener.socket, identity.ordinal), uid, gid)


def _run_probe(host, directory, name, uid, gid, timeout, *, group="", expected=None):
    """Host.run owns the bounded child process group, including cancellation."""
    try:
        account = pwd.getpwuid(uid)
        if uid <= 0 or gid <= 0 or account.pw_gid != gid or timeout <= 0:
            return Result.UNSAFE
        source = Path(__file__).with_name("unix_socket_probe.py").read_text(encoding="utf-8")
        result = host.run(["runuser", "-u", account.pw_name, "--", "/usr/bin/python3", "-I", "-c",
                           source, str(uid), str(gid),
                           str(directory), name, str(timeout), group,
                           *(str(n) for n in (expected or (0, 0))), "stale" if expected is not None else "readiness"],
                          timeout=timeout, check=False)
        return Result(result.returncode) if not result.stdout and not result.stderr else Result.UNSAFE
    except (DeployError, OSError, KeyError, ValueError):
        return Result.UNSAFE


def ready(host, endpoint, uid, gid, unit, deadline):
    try:
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            return False
        observed = host.run(["systemctl", "show", unit, "--property=ControlGroup", "--value"],
                            check=False, timeout=min(5, remaining))
        group = observed.stdout.strip()
        if observed.returncode or not group.startswith("/") or group == "/" or ".." in group.split("/"):
            return False
        return _run_probe(host, host.paths.p(endpoint.directory), endpoint.name, uid, gid,
                          min(5, deadline - time.monotonic()), group=group) == Result.READY
    except (DeployError, OSError):
        return False


def _remove_stale(host, directory_fd, directory, name, uid, gid):
    try:
        info = os.stat(name, dir_fd=directory_fd, follow_symlinks=False)
    except FileNotFoundError:
        return
    require(stat.S_ISSOCK(info.st_mode), "service_socket_type", "Declared socket path is occupied by a non-socket; retain it for inspection.")
    result = _run_probe(host, directory, name, uid, gid, 1, expected=(info.st_dev, info.st_ino))
    require(result != Result.LIVE, "service_socket_busy", "Declared socket is still accepting connections.")
    require(result == Result.STALE, "service_socket_busy", "Socket liveness is uncertain; retain it for inspection.")
    current = os.stat(name, dir_fd=directory_fd, follow_symlinks=False)
    require(stat.S_ISSOCK(current.st_mode) and (info.st_dev, info.st_ino) == (current.st_dev, current.st_ino),
            "service_socket_changed", "Socket changed during preparation; retain it for inspection.")
    os.unlink(name, dir_fd=directory_fd)
