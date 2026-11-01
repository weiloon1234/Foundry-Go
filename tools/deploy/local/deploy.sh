#!/usr/bin/env bash
# The caller supplies --app-root or FOUNDRY_APP_ROOT to the shared CLI.
set -euo pipefail
TOOLS_ROOT=$(cd "$(dirname "$0")/.." && pwd)
exec python3 -B "$TOOLS_ROOT/local/cli.py" "$@"
