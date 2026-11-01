"""Owned Cloudflare DNS-01 lineages; no HTTP fallback or global Certbot edits.

F1 stages private intake before provision. Existing credentials are immutable:
rotation needs an explicit operator procedure, never a speculative overwrite.
"""
from __future__ import annotations

import configparser
import json
from pathlib import Path
import re
import stat
from urllib.error import URLError
from urllib.request import Request, urlopen

from contracts import DeployError, check_path, now, read_json, require
from host import Host, wall_clock_budget
import packages

RELOAD_HOOK = "/usr/sbin/nginx -t -q && /usr/bin/systemctl reload nginx"
REMEDIATION = ("Repair the owned lineage's DNS-01 credentials, renewal configuration and "
               "Foundry renewal timer, then retry; foreign lineages require explicit adoption or reissue.")


def _lineage(entry):
    # The frozen nginx templates and registry allocate domain-named lineages.
    domain = entry["profile"]["domain"]
    require(entry["certificate"]["lineage"] == domain, "certificate_ownership", REMEDIATION)
    return domain


def _private(host, path, *, directory=False):
    path = check_path(path, "directory" if directory else "file")
    require(path.exists(), "certificate_credentials", "Stage the zone-scoped Cloudflare token before issuance.")
    info = path.stat()
    require(stat.S_IMODE(info.st_mode) == (0o700 if directory else 0o600)
            and (info.st_uid == 0 or host.paths.root != Path("/")),
            "certificate_credentials", "Cloudflare credentials must be root-owned: directory 0700, file 0600.")
    return path


def _zone(entry):
    zone = entry["certificate"].get("zone_ref")
    require(isinstance(zone, str) and re.fullmatch(r"[a-z0-9][a-z0-9.-]{0,252}", zone)
            and ".." not in zone, "certificate_zone_required", "Bind an explicit Cloudflare zone before issuance.")
    return zone


def _credentials(host, entry):
    directory = host.paths.p("/etc/foundry/cloudflare/zones")
    _private(host, directory, directory=True)
    path = _private(host, directory / (_zone(entry) + ".ini"))
    text = path.read_text()
    require(re.fullmatch(r"dns_cloudflare_api_token = [A-Za-z0-9_-]{20,256}\n", text) is not None,
            "certificate_credentials", "Cloudflare token file is invalid; repair it privately.")
    return path


def _resolve_zone(token, domain):
    request = Request("https://api.cloudflare.com/client/v4/zones?per_page=50&status=active",
                      headers={"Authorization": "Bearer " + token, "Accept": "application/json"})
    try:
        with wall_clock_budget(30):
            with urlopen(request, timeout=20) as response:
                body = response.read(65537)
        require(len(body) <= 65536, "certificate_zone_invalid", "Cloudflare zone response exceeds its bound.")
        result = json.loads(body)
        require(isinstance(result, dict), "certificate_zone_invalid", "Cloudflare returned an invalid zone response.")
        zones = result.get("result", [])
        require(result.get("success") is True and isinstance(zones, list) and len(zones) == 1
                and isinstance(result.get("result_info"), dict)
                and result.get("result_info", {}).get("total_pages") == 1,
                "certificate_zone_invalid", "Use a token scoped to exactly one active Cloudflare zone, with Zone Read and DNS Edit.")
        zone = zones[0]
        require(isinstance(zone, dict) and re.fullmatch(r"[a-f0-9]{32}", zone.get("id", ""))
                and isinstance(zone.get("name"), str)
                and (domain == zone["name"] or domain.endswith("." + zone["name"])),
                "certificate_zone_invalid", "The scoped Cloudflare zone does not own the profile hostname.")
        return zone["id"]
    except (URLError, OSError, ValueError, TypeError, KeyError):
        raise DeployError("certificate_zone_invalid", "Cloudflare zone lookup failed; verify token scope privately.") from None


def _stage_credentials(host, registry, entry):
    """Consume F1's root-only stdin intake; never rotate a working token."""
    intake = host.paths.instance_config(entry["name"]) / "bootstrap-secrets.json"
    if not intake.exists():
        return  # Already-configured lineages can operate from their zone reference.
    value = read_json(_private(host, intake))
    require(value.get("instance_id") == entry["profile"]["instance_id"],
            "certificate_ownership", "Private certificate intake belongs to another instance.")
    token = value.get("cloudflare_dns_token")
    require(isinstance(token, str) and re.fullmatch(r"[A-Za-z0-9_-]{20,256}", token),
            "certificate_credentials", "Supply the zone-scoped token through secrets.cloudflare_dns_token on SSH stdin.")
    if entry["certificate"].get("zone_ref") is None:
        entry["certificate"]["zone_ref"] = _resolve_zone(token, entry["profile"]["domain"])
        host.journal(registry, entry)
    directory = host.paths.p("/etc/foundry/cloudflare/zones")
    check_path(directory, "directory")
    directory.mkdir(parents=True, exist_ok=True, mode=0o700)
    _private(host, directory, directory=True)
    path = directory / (_zone(entry) + ".ini")
    content = "dns_cloudflare_api_token = " + token + "\n"
    if path.exists():
        require(_private(host, path).read_text() == content, "certificate_credentials_conflict",
                "The zone has working credentials; use an explicit zone credential rotation procedure.")
    else:
        host.owned_write(path, content, mode=0o600)


def _claim_path(host, entry):
    return check_path(host.paths.instance_config(entry["name"]) / "certificate.json", "file")


def _claim(entry):
    return {"instance_id": entry["profile"]["instance_id"], "domain": entry["profile"]["domain"],
            "lineage": _lineage(entry), "zone_ref": _zone(entry)}


def _renewal_path(host, entry):
    return check_path(host.paths.p(f"/etc/letsencrypt/renewal/{_lineage(entry)}.conf"), "file")


def _timer(entry):
    return "foundry-cert-renew-" + entry["profile"]["instance_id"]


def _units(entry):
    name = _timer(entry)
    return {
        name + ".service": ("# Foundry owned DNS-01 renewal\n[Unit]\nDescription=Foundry certificate renewal\n"
                            "[Service]\nType=oneshot\nUMask=0077\n"
                            f"ExecStart=/usr/bin/certbot renew --cert-name {_lineage(entry)} --quiet\n"
                            "TimeoutStartSec=1800\n"),
        name + ".timer": ("# Foundry owned DNS-01 renewal\n[Unit]\nDescription=Foundry certificate renewal timer\n"
                          "[Timer]\nOnCalendar=*-*-* 00,12:00:00 UTC\nRandomizedDelaySec=3600\n"
                          "Persistent=true\n[Install]\nWantedBy=timers.target\n"),
    }


def _timer_ready(host, entry):
    for name, content in _units(entry).items():
        path = check_path(host.paths.p("/etc/systemd/system/" + name), "file")
        require(path.exists() and path.read_text() == content and not path.stat().st_mode & 0o022
                and (path.stat().st_uid == 0 or host.paths.root != Path("/")),
                "certificate_renewal_invalid", REMEDIATION)
    timer = _timer(entry) + ".timer"
    for command in (["systemctl", "is-enabled", timer], ["systemctl", "is-active", timer]):
        require(host.run(command, check=False).returncode == 0, "certificate_renewal_invalid", REMEDIATION)


def _renewal_ready(host, entry):
    credentials = _credentials(host, entry)
    path = _renewal_path(host, entry)
    require(path.exists(), "certificate_renewal_invalid", REMEDIATION)
    parser = configparser.ConfigParser(interpolation=None, strict=True)
    try:
        parser.read_string("[lineage]\n" + path.read_text())
        params = parser["renewalparams"]
        require(params.get("authenticator") == "dns-cloudflare"
                and params.get("dns_cloudflare_credentials") == str(credentials)
                and params.get("dns_cloudflare_propagation_seconds") == "60"
                and params.get("deploy_hook") == RELOAD_HOOK,
                "certificate_renewal_invalid", REMEDIATION)
        live = host.paths.p("/etc/letsencrypt/live/" + _lineage(entry))
        for key, name in (("cert", "cert.pem"), ("privkey", "privkey.pem"),
                          ("chain", "chain.pem"), ("fullchain", "fullchain.pem")):
            require(parser["lineage"].get(key) == str(live / name), "certificate_renewal_invalid", REMEDIATION)
    except (configparser.Error, KeyError, UnicodeError):
        raise DeployError("certificate_renewal_invalid", REMEDIATION) from None
    _timer_ready(host, entry)


def certificate_ready(host: Host, entry: dict) -> bool:
    """Check expiry and hostname separately (openssl otherwise drops a check)."""
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
            raise DeployError("certificate_invalid", "Existing certificate is expired, malformed, or issued for another hostname. " + REMEDIATION)
    return True


def _identity_ready(host, entry):
    require(read_json(_private(host, _claim_path(host, entry))) == _claim(entry),
            "certificate_ownership", REMEDIATION)
    lineage = _lineage(entry)
    live = host.paths.p("/etc/letsencrypt/live/" + lineage)
    archive = check_path(host.paths.p("/etc/letsencrypt/archive/" + lineage), "directory")
    # Certbot live files must resolve within this lineage's archive, not another site.
    check_path(live, "directory")
    for name in ("cert.pem", "chain.pem", "fullchain.pem", "privkey.pem"):
        path = live / name
        require(path.is_symlink() and path.resolve().parent == archive and path.resolve().is_file(),
                "certificate_ownership", REMEDIATION)
    require(certificate_ready(host, entry), "certificate_invalid", REMEDIATION)
    certificate = host.run(["openssl", "x509", "-in", str(live / "cert.pem"), "-pubkey", "-noout"])
    key = host.run(["openssl", "pkey", "-in", str(live / "privkey.pem"), "-pubout"])
    require(bool(certificate.stdout.strip()) and certificate.stdout == key.stdout,
            "certificate_invalid", REMEDIATION)
    host.run(["openssl", "verify", "-CApath", str(host.paths.p("/etc/ssl/certs")),
              "-untrusted", str(live / "chain.pem"), str(live / "cert.pem")])


def inspect(host: Host, entry: dict) -> dict:
    """Read-only deterministic observation. A dry-run is required by provision."""
    lineage = _lineage(entry)
    live = host.paths.p("/etc/letsencrypt/live/" + lineage)
    if not live.exists() and not _renewal_path(host, entry).exists():
        return {"state": "missing", "lineage": lineage}
    try:
        _identity_ready(host, entry)
        _renewal_ready(host, entry)
    except (DeployError, OSError, ValueError):
        return {"state": "conflict", "lineage": lineage, "remediation": REMEDIATION}
    return {"state": "ready", "lineage": lineage, "zone_ref": _zone(entry)}


def provision(host: Host, registry: dict, entry: dict) -> None:
    """Reconcile only owned DNS lineages; A03 applies on every reuse."""
    _stage_credentials(host, registry, entry)
    credentials = _credentials(host, entry)
    observed = inspect(host, entry)
    require(observed["state"] != "conflict", "certificate_renewal_invalid", REMEDIATION)
    packages.install_missing(host, ["certbot", "python3-certbot-dns-cloudflare"])
    if observed["state"] == "missing":
        claim = _claim_path(host, entry)
        if claim.exists():
            require(read_json(_private(host, claim)) == _claim(entry), "certificate_ownership", REMEDIATION)
        else:
            host.owned_write(claim, json.dumps(_claim(entry)) + "\n", mode=0o600)
        for name, content in _units(entry).items():
            path = host.paths.p("/etc/systemd/system/" + name)
            require(not path.exists() or check_path(path).read_text() == content,
                    "certificate_ownership", REMEDIATION)
            host.owned_write(path, content)
        host.run(["systemctl", "daemon-reload"])
        host.run(["systemctl", "enable", "--now", _timer(entry) + ".timer"])
        host.run(["certbot", "certonly", "--non-interactive", "--agree-tos", "--dns-cloudflare",
                  "--dns-cloudflare-credentials", str(credentials), "--dns-cloudflare-propagation-seconds", "60",
                  "--cert-name", _lineage(entry), "--email", entry["profile"]["certbot_email"],
                  "--domain", entry["profile"]["domain"], "--deploy-hook", RELOAD_HOOK], timeout=900)
    _identity_ready(host, entry)
    _renewal_ready(host, entry)
    try:
        host.run(["certbot", "renew", "--cert-name", _lineage(entry), "--dry-run", "--run-deploy-hooks"], timeout=900)
    except DeployError:
        raise DeployError("certificate_renewal_failed", "DNS-01 renewal dry-run failed. " + REMEDIATION) from None
    entry["certificate"]["last_renewal_check_at"] = now()
    if "certificate" not in entry["provisioning"]["completed_steps"]:
        entry["provisioning"]["completed_steps"].append("certificate")
    host.journal(registry, entry)
