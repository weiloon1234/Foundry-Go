"""Disposable instance tmpfs mounts and role-local filesystem views."""
import json
import os
from pathlib import Path
import stat

from contracts import (ResolvedSharedDirectory, check_path, require,
                       shared_directory_path, shared_directory_root)


def bindings(topology, instance, role):
    return tuple(ResolvedSharedDirectory(name, shared_directory_path(instance, name), directory.access(role), directory.max_bytes)
                 for name, directory in topology.shared_directories if directory.access(role))


def sandbox(topology, instance, role):
    from services import systemd_word
    # Hide the entire host tree, including retired declarations. Bind sources are
    # resolved in the host namespace, then exposed inside this private empty tree.
    lines = ["TemporaryFileSystem=" + systemd_word(shared_directory_root(instance) + ":ro")]
    for binding in bindings(topology, instance, role):
        writable = binding.access == "read-write"
        path = systemd_word(binding.path)
        lines += [("BindPaths=" if writable else "BindReadOnlyPaths=") + path,
                  ("ReadWritePaths=" if writable else "ReadOnlyPaths=") + path]
    return "\n".join(lines) + "\n"


def _source(instance, name):
    return "foundry-shared-" + instance + "-" + name


def _mount(host, path):
    result = host.run(["findmnt", "--json", "--mountpoint", str(path), "--output", "TARGET,SOURCE,FSTYPE,OPTIONS"],
                      check=False, timeout=5)
    require(result.returncode in (0, 1) and len(result.stdout) <= 16384,
            "shared_directory_mount", "Cannot inspect the shared directory mount.")
    if result.returncode == 1:
        require(not result.stdout.strip(), "shared_directory_mount", "Ambiguous shared directory mount.")
        return None
    try:
        records = json.loads(result.stdout)["filesystems"]
        require(len(records) == 1 and records[0]["target"] == str(path),
                "shared_directory_mount", "Shared directory must be an exact mountpoint.")
        return records[0]
    except (ValueError, KeyError, TypeError):
        require(False, "shared_directory_mount", "Invalid shared directory mount observation.")


def _verify_mount(record, instance, name):
    require(record is not None and record.get("source") == _source(instance, name) and record.get("fstype") == "tmpfs"
            and {"rw", "nosuid", "nodev", "noexec"} <= set(record.get("options", "").split(",")),
            "shared_directory_mount", "Shared directory must be its owned, restricted tmpfs mount.")


def _inode_limit(directory):
    return min(65536, directory.max_bytes // 4096)


def _verify_directory(path, directory, uid, gid):
    fd = os.open(path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        info, capacity = os.fstat(fd), os.fstatvfs(fd)
        require(info.st_uid == uid and info.st_gid == gid and stat.S_IMODE(info.st_mode) == 0o700,
                "shared_directory_owner", "Shared directory must belong to the instance account with mode 0700.")
        require(capacity.f_blocks * capacity.f_frsize == directory.max_bytes
                and capacity.f_files == _inode_limit(directory),
                "shared_directory_limit", "Shared directory filesystem limits differ from the declaration.")
        return capacity
    finally:
        os.close(fd)


def prepare(host, topology, instance, uid, gid):
    """Called under the instance's shared-directory preparation lock, as host root.

    The privileged ExecStartPre prefix bypasses service filesystem namespaces.
    Never overlay an occupied directory or adopt an unrelated mount.
    """
    from admission import protected_directory
    require(type(uid) is int and type(gid) is int and uid >= 0 and gid >= 0
            and (host.paths.root != Path("/") or uid > 0 and gid > 0),
            "service_account", "Shared directories require the instance account.")
    protected_directory(host, host.paths.p(shared_directory_root(instance)))
    for name, directory in topology.shared_directories:
        path = check_path(host.paths.p(shared_directory_path(instance, name)), "directory")
        record = _mount(host, path)
        options = (f"size={directory.max_bytes},nr_inodes={_inode_limit(directory)},"
                   f"uid={uid},gid={gid},mode=0700,nosuid,nodev,noexec")
        if record is None:
            protected_directory(host, path, 0o700)
            require(not any(path.iterdir()), "shared_directory_occupied", "Refusing to mount over existing files.")
            host.run(["mount", "-t", "tmpfs", "-o", options, _source(instance, name), str(path)], timeout=10)
        else:
            _verify_mount(record, instance, name)
            # Validate ownership before allowing an explicit declaration resize.
            info = path.lstat()
            require(info.st_uid == uid and info.st_gid == gid and stat.S_IMODE(info.st_mode) == 0o700,
                    "shared_directory_owner", "Shared directory ownership has drifted.")
            capacity = os.statvfs(path)
            if capacity.f_blocks * capacity.f_frsize != directory.max_bytes or capacity.f_files != _inode_limit(directory):
                host.run(["mount", "-t", "tmpfs", "-o", "remount," + options, _source(instance, name), str(path)], timeout=10)
        _verify_mount(_mount(host, path), instance, name)
        _verify_directory(path, directory, uid, gid)


def coverage(topology, instance):
    return [{"name": name, "path": shared_directory_path(instance, name), "state": "disposable",
             "backed_up": False, "restored": False, "max_bytes": directory.max_bytes, "bound": "tmpfs",
             "writers": list(directory.writers), "readers": list(directory.readers)}
            for name, directory in topology.shared_directories]


def discard(host, topology, instance, uid, gid):
    """Discard only declared caches after the lifecycle owner proves every role stopped.

    No lazy/forced unmount and no recursive deletion: a busy or foreign mount fails
    closed. Interrupted cleanup is replayable against the empty underlying root.
    """
    from admission import protected_path
    protected_path(host, host.paths.p(shared_directory_root(instance)), "directory")
    for name, _ in topology.shared_directories:
        path = check_path(host.paths.p(shared_directory_path(instance, name)), "directory")
        if not path.exists():
            continue
        record = _mount(host, path)
        if record is not None:
            _verify_mount(record, instance, name)
            info = path.lstat()
            require(info.st_uid == uid and info.st_gid == gid and stat.S_IMODE(info.st_mode) == 0o700,
                    "shared_directory_owner", "Refusing to discard a directory with unexpected ownership.")
            host.run(["umount", str(path)], timeout=10)
        require(_mount(host, path) is None, "shared_directory_mount", "Shared directory is still mounted.")
        protected_path(host, path, "directory").rmdir()


def report(host, topology, instance, uid, gid):
    result = coverage(topology, instance)
    for item, (name, directory) in zip(result, topology.shared_directories):
        from contracts import DeployError
        try:
            from admission import protected_path
            protected_path(host, host.paths.p(shared_directory_root(instance)), "directory")
            path = check_path(host.paths.p(item["path"]), "directory")
            _verify_mount(_mount(host, path), instance, name)
            capacity = _verify_directory(path, directory, uid, gid)
            item.update(status="verified", used_bytes=(capacity.f_blocks - capacity.f_bfree) * capacity.f_frsize)
        except (DeployError, OSError):
            item.update(status="unverified", finding="shared_directory_unverified")
    return result
