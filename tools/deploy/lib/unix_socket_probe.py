"""Unprivileged Linux socket probe; launched only through the instance account."""
from enum import IntEnum
import errno
import os
import select
import socket
import stat
import struct
import sys


class Result(IntEnum):
    READY = 0
    STALE = 3
    LIVE = 4
    UNSAFE = 5


# Linux UAPI, also on supported Ubuntu Python versions without the named constant.
SO_PEERPIDFD = getattr(socket, "SO_PEERPIDFD", 77)


def peer_matches(connection, uid, group):
    """Inspect the connected peer, keeping its kernel identity through inspection."""
    pid, peer_uid, _ = struct.unpack("3i", connection.getsockopt(socket.SOL_SOCKET, socket.SO_PEERCRED, 12))
    if peer_uid != uid or pid <= 0 or not group.startswith("/") or group == "/" or ".." in group.split("/"):
        return False
    peer = connection.getsockopt(socket.SOL_SOCKET, SO_PEERPIDFD)
    try:
        exited = select.poll()
        exited.register(peer, select.POLLIN)
        if exited.poll(0):
            return False
        with open(f"/proc/{pid}/cgroup", "rb") as source:
            data = source.read(65537)
        if len(data) > 65536:
            return False
        groups = [line[3:] for line in data.decode("utf-8").splitlines() if line.startswith("0::")]
        belongs = any(current == group or current.startswith(group + "/") for current in groups)
        # A pidfd obtained from the socket, rather than pidfd_open(pid), prevents
        # a recycled numeric PID from being mistaken for the connected peer.
        return belongs and not exited.poll(0)
    finally:
        os.close(peer)


def probe(directory, name, uid, gid, timeout, *, group="", expected=None):
    """Pin the no-follow leaf after privilege drop; never reconnect by its name."""
    if (uid <= 0 or gid <= 0 or os.geteuid() != uid or os.getegid() != gid or
            timeout <= 0 or timeout > 300 or not name or name != os.path.basename(name) or name in (".", "..")):
        return Result.UNSAFE
    try:
        parent = os.open(directory, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
        try:
            owner = os.fstat(parent)
            if (owner.st_uid, owner.st_gid, stat.S_IMODE(owner.st_mode)) != (uid, gid, 0o700):
                return Result.UNSAFE
            before = os.stat(name, dir_fd=parent, follow_symlinks=False)
            identity = (before.st_dev, before.st_ino)
            if not stat.S_ISSOCK(before.st_mode) or expected is not None and identity != expected:
                return Result.UNSAFE
            leaf = os.open(name, os.O_PATH | os.O_NOFOLLOW, dir_fd=parent)
            try:
                pinned = os.fstat(leaf)
                if not stat.S_ISSOCK(pinned.st_mode) or (pinned.st_dev, pinned.st_ino) != identity:
                    return Result.UNSAFE
                with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as connection:
                    connection.settimeout(timeout)
                    try:
                        connection.connect(f"/proc/self/fd/{leaf}")
                    except OSError as error:
                        return Result.STALE if error.errno == errno.ECONNREFUSED else Result.UNSAFE
                    current = os.stat(name, dir_fd=parent, follow_symlinks=False)
                    if not stat.S_ISSOCK(current.st_mode) or (current.st_dev, current.st_ino) != identity:
                        return Result.UNSAFE
                    if not group:
                        return Result.LIVE
                    return Result.READY if peer_matches(connection, uid, group) else Result.UNSAFE
            finally:
                os.close(leaf)
        finally:
            os.close(parent)
    except (OSError, ValueError, AttributeError, struct.error):
        return Result.UNSAFE


def main(arguments):
    if len(arguments) != 9:
        return Result.UNSAFE
    uid, gid, directory, name, seconds, group, device, inode, mode = arguments
    try:
        if mode not in ("readiness", "stale") or (mode == "readiness") != bool(group):
            return Result.UNSAFE
        expected = (int(device), int(inode)) if mode == "stale" else None
        return probe(directory, name, int(uid), int(gid), float(seconds), group=group, expected=expected)
    except ValueError:
        return Result.UNSAFE


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
