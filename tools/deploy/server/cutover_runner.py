#!/usr/bin/python3
"""Pinned, supervised executor. No SSH lifetime and no automatic reboot resume."""
from pathlib import Path
import sys

sys.dont_write_bytecode = True
sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'lib'))
from contracts import DeployError, valid_uuid
from host import Host
import release


def main():
    if len(sys.argv) != 3 or not all(valid_uuid(value) for value in sys.argv[1:]):
        return 1
    try:
        release.execute_cutover(Host(), sys.argv[1], sys.argv[2])
        return 0
    except (DeployError, OSError, ValueError, KeyError, TypeError):
        print('Cutover incomplete; inspect status and resume the same operation.', file=sys.stderr)
        return 1


if __name__ == '__main__':
    raise SystemExit(main())
