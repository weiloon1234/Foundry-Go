#!/usr/bin/python3
"""Owned operation-unit entrypoint; accepts only a durable UUID, never commands."""
from pathlib import Path
import sys

sys.dont_write_bytecode = True
sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "lib"))
from contracts import DeployError, atomic_json, contract_uuid
from admission import read_owned
from host import Host
import feature_lifecycle


def main(arguments):
    if len(arguments) != 1:
        return 1
    host = Host()
    operation = contract_uuid(arguments[0])
    directory = host.paths.state / "feature-operations" / operation
    request = read_owned(host, directory / "request.json", lambda value: value)
    if request.get("operation_id") != operation:
        return 1
    try:
        result = feature_lifecycle.execute(host, request)
    except (DeployError, OSError) as error:
        result = {"error": {"code": error.code if isinstance(error, DeployError) else "filesystem_failed",
                            "detail": error.detail if isinstance(error, DeployError) else "Inspect the owned operation journal."}}
    atomic_json(directory / "result.json", result, mode=0o600)
    return 1 if "error" in result else 0


if __name__ == "__main__":
    try:
        raise SystemExit(main(sys.argv[1:]))
    except Exception:
        print("feature_operation_failed: inspect the owned instance journal", file=sys.stderr)
        raise SystemExit(1)
