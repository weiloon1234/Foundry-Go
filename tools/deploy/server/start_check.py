#!/usr/bin/python3
"""Read-only systemd startup admission; never acquire the coordinator's locks."""
from pathlib import Path
import sys

sys.dont_write_bytecode = True
sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'lib'))
from compatibility import startup_check
from contracts import DeployError
from host import Host


def main():
    try:
        if len(sys.argv) != 3:
            return 1
        startup_check(Host(), sys.argv[1], sys.argv[2])
        return 0
    except (DeployError, OSError, ValueError, KeyError, TypeError):
        print('Foundry startup refused: inspect the deployment compatibility state.', file=sys.stderr)
        return 1


if __name__ == '__main__':
    raise SystemExit(main())
