#!/usr/bin/env python3
"""Installed as foundry-host-report; no root access is required."""

import argparse
import json
import sys
from pathlib import Path

sys.dont_write_bytecode = True
sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "lib"))

from host import Host
from report import collect_report


def main():
    parser = argparse.ArgumentParser(description="Read the versioned deployment host report.")
    parser.add_argument("--pretty", action="store_true")
    args = parser.parse_args()
    result = collect_report(Host())
    print(json.dumps(result, indent=2 if args.pretty else None, separators=None if args.pretty else (",", ":"), allow_nan=False))
    return 1 if result["host"]["host_id"] is None else 0


if __name__ == "__main__":
    raise SystemExit(main())
