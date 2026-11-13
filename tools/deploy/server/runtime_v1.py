#!/usr/bin/python3 -I
"""Fixed runtime-v1 trampoline. Only the standard library runs before pinning.

Preview retains every feature helper bundle. A later prune implementation must
honour these invocation pins before it can retire that conservative policy.
"""
import fcntl
import json
import os
from pathlib import Path
import re
import stat
import sys
import tempfile
import uuid

ROOT = Path("/usr/local/lib/foundry")
STATE = Path("/var/lib/foundry/admission")
PINS = Path("/run/foundry/runtime-pins")


def identifier(value):
    return str(uuid.UUID(value)) == value and uuid.UUID(value).version == 4


def owned(path):
    for part in (path, *path.parents):
        info = part.lstat()
        if stat.S_ISLNK(info.st_mode) or info.st_uid != 0 or info.st_mode & 0o022:
            raise ValueError("unowned runtime input")
    return path


def pairs(items):
    result = {}
    for key, value in items:
        if key in result:
            raise ValueError("duplicate runtime field")
        result[key] = value
    return result


def read(path):
    owned(path)
    with path.open("rb") as stream:
        data = stream.read(4 * 1024 * 1024 + 1)
    if len(data) > 4 * 1024 * 1024:
        raise ValueError("runtime input too large")
    return json.loads(data, object_pairs_hook=pairs, parse_constant=lambda _: (_ for _ in ()).throw(ValueError()))


def publish(path, value):
    path.parent.mkdir(parents=True, exist_ok=True, mode=0o755)
    owned(path.parent)
    fd, name = tempfile.mkstemp(dir=path.parent)
    try:
        with os.fdopen(fd, "w") as stream:
            json.dump(value, stream, sort_keys=True)
            stream.flush()
            os.fchmod(stream.fileno(), 0o444)
            os.fsync(stream.fileno())
        os.replace(name, path)
        fd = os.open(path.parent, os.O_RDONLY)
        try:
            os.fsync(fd)
        finally:
            os.close(fd)
    finally:
        if os.path.exists(name):
            os.unlink(name)


def main(arguments):
    try:
        if len(arguments) != 7 or arguments[0] not in ("admit", "prepare", "dispatch", "cleanup", "boot", "open"):
            return 1
        action, instance, role, ordinal, resolution, bundle, spec = arguments
        if (not all(identifier(v) for v in (instance, resolution)) or ordinal != "1" or
                not re.fullmatch(r"[a-z][a-z0-9_-]{0,63}", role) or
                any(not re.fullmatch(r"[a-f0-9]{64}", v) for v in (bundle, spec))):
            return 1
        invocation = os.environ.get("INVOCATION_ID", "")
        if not re.fullmatch(r"[a-f0-9]{32}", invocation) or (action == "dispatch") == (os.geteuid() == 0):
            return 1
        key = PINS / instance / role / (invocation + ".json")
        if action == "cleanup" and not key.exists():
            return 0
        if os.geteuid() == 0:
            PINS.mkdir(parents=True, exist_ok=True, mode=0o755)
            owned(PINS)
            fd = os.open(PINS / "registration.lock", os.O_CREAT | os.O_RDWR | os.O_NOFOLLOW, 0o600)
            try:
                fcntl.flock(fd, fcntl.LOCK_EX)
                if not key.exists():
                    if action not in ("admit", "boot", "open"):
                        return 1
                    generation = read(STATE / instance / "generations" / (spec + "-" + bundle + ".json"))
                    if generation["unit"] != {"instance_id": instance, "role": role, "ordinal": 1} or generation["resolution_id"] != resolution:
                        return 1
                    proc = Path("/proc/self/stat").read_text().rsplit(") ", 1)[1].split()
                    pin = {"schema": 1, "boot_id": Path("/proc/sys/kernel/random/boot_id").read_text().strip(),
                           "unit": generation["unit"], "process": {"pid": os.getpid(), "start_ticks": int(proc[19]), "invocation_id": invocation},
                           "bundle_sha256": bundle, "unit_spec_sha256": spec, "resolution_id": resolution,
                           "helper_format": 1, "cleanup_id": None}
                    publish(key, {"helper": pin, "launch_file": generation["launch_file"]})
            finally:
                os.close(fd)
        pinned = read(key)
        pin = pinned["helper"]
        if (pin["unit"] != {"instance_id": instance, "role": role, "ordinal": 1} or
                pin["boot_id"] != Path("/proc/sys/kernel/random/boot_id").read_text().strip() or
                pin["process"]["invocation_id"] != invocation or pin["helper_format"] != 1):
            return 1
        checksum = pin["bundle_sha256"]
        if not re.fullmatch(r"[a-f0-9]{64}", checksum):
            return 1
        library = owned(ROOT / "tools" / checksum / "lib")
        owned(library / "feature_runtime.py")
        sys.path.insert(0, str(library))
        from feature_runtime import run
        return run(action, pin, read(Path(pinned["launch_file"])))
    except Exception:
        # Inputs and child failures may contain private paths; no arbitrary formatting.
        sys.stderr.write("feature_runtime_refused: inspect the owned deployment journal\n")
        return 1


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
