#!/usr/bin/env bash
# Linux/KVM only. The Python owner tears down resources on failure and signals.
set -euo pipefail
umask 077
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
export PYTHONDONTWRITEBYTECODE=1
exec python3 -B "$here/vm_acceptance.py" "$@"
