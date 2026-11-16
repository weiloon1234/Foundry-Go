"""Acceptance host identity; environment switches alone never grant access."""
import os
from pathlib import Path
import stat

VM_MARKER = "/etc/foundry-disposable-acceptance-vm"
ACCEPTANCE_CA = "/etc/foundry/acceptance-ca"


def disposable_host(root=Path("/")):
    root = Path(root)
    try:
        if stat.S_ISREG((root / ".dockerenv").lstat().st_mode):
            return True
    except OSError:
        pass
    if os.environ.get("FOUNDRY_DISPOSABLE_HOST") != "1":
        return False
    try:
        marker = (root / VM_MARKER.lstrip("/")).lstat()
    except OSError:
        return False
    return (stat.S_ISREG(marker.st_mode) and marker.st_uid == 0
            and stat.S_IMODE(marker.st_mode) == 0o600)
