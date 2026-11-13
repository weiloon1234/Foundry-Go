#!/usr/bin/python3 -I
"""Source for the fixed /usr/local/lib/foundry/runtime-v1 entry point.

Only stdlib is loaded before bundle pinning. Installation and pin/dispatch are
intentionally unavailable until the lifecycle's helper-lifetime checkpoint.
Never resolve current, import a proposed bundle, or run a caller command here.
"""
import re
import sys
import uuid


def main(arguments):
    if len(arguments) != 7 or arguments[0] not in ("admit", "prepare", "dispatch", "cleanup"):
        return 1
    action, instance, role, ordinal, resolution, bundle, spec = arguments
    try:
        if any(str(uuid.UUID(value)) != value or uuid.UUID(value).version != 4 for value in (instance, resolution)):
            return 1
    except ValueError:
        return 1
    if (not re.fullmatch(r"[a-z][a-z0-9_-]{0,63}", role) or ordinal != "1" or
            any(not re.fullmatch(r"[a-f0-9]{64}", value) for value in (bundle, spec))):
        return 1
    sys.stderr.write("capability_unavailable: feature lifecycle dispatch is not installed\n")
    return 1


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
