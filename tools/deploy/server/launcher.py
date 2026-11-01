#!/usr/bin/python3 -B
"""Stable journal-v1 launcher. Upgrades never overwrite an executing tool bundle."""
import json
import os
from pathlib import Path
import re
import sys
import uuid


def read_record(path):
    for parent in [*reversed(path.parents), path]:
        info = parent.lstat()
        if parent.is_symlink() or info.st_uid != 0 or info.st_mode & 0o022:
            raise ValueError()
    with path.open("rb") as stream:
        payload = stream.read(4 * 1024 * 1024 + 1)
    if len(payload) > 4 * 1024 * 1024:
        raise ValueError()
    return json.loads(payload)


def main():
    # Installation renders these values from contracts.HOST_PATHS. Importing the
    # active bundle here would execute code before validating its pointer.
    root = Path("@TOOLS_ROOT@")
    try:
        action = sys.argv[1] if len(sys.argv) > 1 else "hello"
        registry = Path("@REGISTRY@")
        public = read_record(registry)
        record = public["tools"]
        pending = record["upgrade_operation"]
        if pending and action not in {"hello", "inspect", "status", "report", "logs", "tools-plan", "tools-apply", "upgrade-resume"}:
            print(json.dumps({"error": {"code": "tools_upgrade_pending", "detail": "Resume the approved tool upgrade before other mutations."}}))
            return 1
        pointer = root / "current"
        target = os.readlink(pointer)
        if not re.fullmatch(r"tools/[a-f0-9]{64}", target):
            raise ValueError()
        bundle = root / target
        for path in [*reversed(bundle.parents), bundle, bundle / "server", bundle / "server/manage.py"]:
            if path.is_symlink() or path.stat().st_uid != 0 or path.stat().st_mode & 0o022:
                raise ValueError()
        if not pending and str(bundle) != record["active_path"]:
            raise ValueError()
        if pending:
            if str(uuid.UUID(pending, version=4)) != pending:
                raise ValueError()
            intent = read_record(Path("@UPGRADES@") / pending / "intent.json")
            if (intent["schema"] != 1 or intent["operation_id"] != pending or intent["host_id"] != public["host_id"]
                    or target.split("/")[1] not in {intent["bundle_sha256"], intent["previous_tools"]["bundle_sha256"]}):
                raise ValueError()
        os.execv(sys.executable, [sys.executable, "-B", str(bundle / "server/manage.py"), action, *sys.argv[2:]])
    except (OSError, ValueError, KeyError, TypeError):
        print(json.dumps({"error": {"code": "tools_launcher_invalid", "detail": "The active tool pointer or registry requires administrator reconciliation."}}))
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
