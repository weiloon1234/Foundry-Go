"""Mixed-host package boundary. Implementation owner: lane F2.

No caller may install packages directly: an apt simulation must prove that only
new packages are installed, with package service starts durably suppressed.
"""
from __future__ import annotations

from host import Host

from collections.abc import Sequence


def inspect(host: Host) -> dict:
    """Read installed package/service metadata without changing the host.

    Return the public package inventory used by bootstrap planning. Never query
    foreign application data or authenticate to foreign database services.
    """
    raise NotImplementedError("F2: mixed-host package inventory")


def install_missing(host: Host, names: Sequence[str]) -> None:
    """Install missing packages only after a pure-new-install apt-get -s plan.

    Refuse upgrades, removals, reconfiguration, or an existing policy-rc.d.
    Journal a temporary deny-start policy before publication, use
    NEEDRESTART_SUSPEND=1, and restore the policy after success or interruption.
    Installed packages and foreign service processes must remain unchanged.
    """
    raise NotImplementedError("F2: guarded package installation")
