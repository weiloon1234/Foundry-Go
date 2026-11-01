"""Cloudflare DNS-01 certificate boundary. Implementation owner: lane F3."""
from __future__ import annotations

from host import Host
from contracts import DeployError


def inspect(host: Host, entry: dict) -> dict:
    """Inspect the owned lineage, hostname, renewal timer, and nginx reload hook.

    Return public findings only. Existing lineages require matching identity and
    DNS-01 policy; possession of an unrelated certificate never authorizes reuse.
    """
    raise NotImplementedError("F3: DNS-01 lineage and renewal inspection")


def provision(host: Host, registry: dict, entry: dict) -> None:
    """Issue/reconcile this instance's Cloudflare DNS-01 lineage and renewal checks.

    Read a zone-scoped token from its root-only reference under /etc/foundry;
    preserve its privacy, verify the reload hook, and run the lineage renewal
    dry-run before journaling completion. Never use an HTTP-01 fallback.
    """
    raise NotImplementedError("F3: Cloudflare DNS-01 certificate provisioning")


def certificate_ready(host: Host, entry: dict) -> bool:
    """Check an existing certificate's expiry and hostname with separate probes.

    Return False for an absent certificate; reject an expired/malformed/wrong-name
    certificate. Certbot's managed live/archive symlinks are expected here. F3
    lineage inspection must additionally prove ownership and DNS-01 renewal policy.
    """
    domain = entry["profile"]["domain"]
    certificate = host.paths.p(f"/etc/letsencrypt/live/{domain}/fullchain.pem")
    if not certificate.exists():
        return False
    command = ["openssl", "x509", "-noout", "-in", str(certificate)]
    for arguments in (["-checkend", "0"], ["-checkhost", domain]):
        try:
            result = host.run(command + arguments, check=False, timeout=10)
        except (OSError, DeployError):
            raise DeployError("certificate_invalid", "Existing certificate could not be inspected.") from None
        if result.returncode:
            raise DeployError("certificate_invalid", "Existing certificate is expired, malformed, or issued for another hostname.")
    return True
