"""Owned Cloudflare DNS-01 lineages; no HTTP fallback or global Certbot edits.

Bootstrap stages private intake before provision. Existing credentials are immutable:
rotation needs an explicit operator procedure, never a speculative overwrite.
"""
from __future__ import annotations

import configparser
import errno
import json
import hashlib
from http.client import HTTPException
import os
from pathlib import Path
import re
import socket
import ssl
import stat
import sys
import tempfile
import time
import uuid
from urllib.error import HTTPError, URLError
from urllib.request import ProxyHandler, Request, build_opener, urlopen

if __name__ == "__main__":
    sys.path.insert(0, str(Path(__file__).resolve().parent))

from contracts import (DeployError, HOST_PATHS, RENEWAL_WARNING, check_path, contract_digest,
                       contract_name, dns_hostname, instance_paths, now, read_json, require, valid_uuid)
from host import Host, wall_clock_budget
import packages

STAGING_DIRECTORY = "https://acme-staging-v02.api.letsencrypt.org/directory"

CONFIG_DIR = "/etc/foundry/letsencrypt"
WORK_DIR = "/var/lib/foundry/letsencrypt"
LOGS_DIR = "/var/log/foundry/letsencrypt"
TEMPLATES = Path(__file__).resolve().parents[1] / "server/templates"
RELOAD_COMMANDS = (["/usr/sbin/nginx", "-t", "-q"], ["/usr/bin/systemctl", "reload", "nginx"])
RELOAD_HOOK = " && ".join(" ".join(command) for command in RELOAD_COMMANDS)
NETWORK_RELOAD_HOOK = RELOAD_HOOK + f" && /usr/bin/python3 -I {HOST_PATHS['current']}/lib/certificates.py verify-network-renewal"
REMEDIATION = ("Repair the owned lineage's DNS-01 credentials, renewal configuration and "
               "Foundry renewal timer, then retry; foreign lineages require explicit adoption or reissue.")


def _synthetic(host, entry):
    requested = os.environ.get("FOUNDRY_ACCEPTANCE_SYNTHETIC_CERT") == "1"
    fixture_lineage = entry["certificate"].get("zone_ref") == "acceptance-synthetic"
    if not requested and not fixture_lineage:
        return False
    profile = entry["profile"]
    require(requested and profile["runtime_environment"] == "testing"
            and profile["environment"].startswith("acceptance")
            and profile["domain"].endswith(".acceptance.invalid")
            and host.paths.p("/.dockerenv").is_file(),
            "certificate_fixture_forbidden", "Synthetic certificates require the explicit disposable acceptance container and testing profile.")
    directory = _private(host, host.paths.p("/etc/foundry/acceptance-ca"), directory=True)
    _private(host, directory / "ca.key")
    _private(host, directory / "ca.crt")
    return True


def _synthetic_issue(host, entry):
    require(_synthetic(host, entry), "certificate_fixture_forbidden", "Synthetic issuance is acceptance-only.")
    lineage = _lineage(entry)
    ca = host.paths.p("/etc/foundry/acceptance-ca")
    archive = check_path(host.paths.p(CONFIG_DIR + "/archive/" + lineage), "directory")
    live = check_path(host.paths.p(CONFIG_DIR + "/live/" + lineage), "directory")
    archive.mkdir(parents=True, mode=0o700, exist_ok=True)
    live.mkdir(parents=True, mode=0o700, exist_ok=True)
    generation = uuid.uuid4().hex
    with tempfile.TemporaryDirectory(prefix="issue-", dir=archive) as temporary:
        staging = Path(temporary)
        host.run(["openssl", "req", "-new", "-newkey", "rsa:2048", "-nodes",
                  "-subj", "/CN=" + lineage, "-keyout", str(staging / "privkey.pem"),
                  "-out", str(staging / "request.pem")], timeout=30)
        host.owned_write(staging / "extensions", "subjectAltName=" + ",".join("DNS:" + name for name in _hostnames(host, entry)) + "\n"
                         "basicConstraints=critical,CA:FALSE\nkeyUsage=critical,digitalSignature,keyEncipherment\n"
                         "extendedKeyUsage=serverAuth\n"
                         # Strict X.509 verifiers (Python 3.13+ on Ubuntu 26.04) require key identifiers.
                         "subjectKeyIdentifier=hash\nauthorityKeyIdentifier=keyid,issuer\n", mode=0o600)
        host.run(["openssl", "x509", "-req", "-in", str(staging / "request.pem"),
                  "-CA", str(ca / "ca.crt"), "-CAkey", str(ca / "ca.key"),
                  "-set_serial", "0x" + generation, "-days", "30", "-sha256",
                  "-extfile", str(staging / "extensions"), "-out", str(staging / "cert.pem")])
        host.owned_write(staging / "chain.pem", (ca / "ca.crt").read_text(), mode=0o600)
        host.owned_write(staging / "fullchain.pem", (staging / "cert.pem").read_text()
                         + (staging / "chain.pem").read_text(), mode=0o600)
        for name in ("cert", "chain", "fullchain", "privkey"):
            destination = archive / (name + generation + ".pem")
            host.owned_write(destination, (staging / (name + ".pem")).read_text(), mode=0o600)
            temporary_link = live / ("." + name + "-" + generation)
            temporary_link.symlink_to(destination)
            os.replace(temporary_link, live / (name + ".pem"))
    renewal = "\n".join(f"{name} = {live / filename}" for name, filename in (
        ("cert", "cert.pem"), ("privkey", "privkey.pem"), ("chain", "chain.pem"), ("fullchain", "fullchain.pem")))
    host.owned_write(_renewal_path(host, entry), renewal + "\n[renewalparams]\nauthenticator = acceptance-synthetic\n"
                     + "renew_hook = " + _reload_hook(host, entry) + "\n", mode=0o600)
    _identity_ready(host, entry)
    _renewal_ready(host, entry)
    for command in RELOAD_COMMANDS:
        host.run(command)
    if _network_claim_path(host, entry).exists():
        verify_network_postcondition(host, entry)


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


def _validate_token(token):
    require(isinstance(token, str) and re.fullmatch(r"[A-Za-z0-9_-]{20,256}", token),
            "certificate_credentials", "Supply the zone-scoped token through secrets.cloudflare_dns_token on SSH stdin.")


def _renewal_dry_run(host, entry):
    host.run(["certbot", "renew", "--cert-name", _lineage(entry), "--dry-run", "--run-deploy-hooks",
              "--no-random-sleep-on-renew"] + _options(), timeout=900)
    if _network_claim_path(host, entry).exists():
        verify_network_postcondition(host, entry)


def _staging_unreachable():
    try:
        with wall_clock_budget(30):
            with build_opener(ProxyHandler({})).open(STAGING_DIRECTORY, timeout=20) as response:
                return 500 <= response.status <= 599
    except HTTPError as error:
        error.close()
        return 500 <= error.code <= 599
    except URLError as error:
        reason = error.reason
        return (isinstance(reason, (ConnectionError, TimeoutError, socket.gaierror))
                or isinstance(reason, OSError) and reason.errno in {errno.ENETUNREACH, errno.EHOSTUNREACH})
    except (ConnectionError, TimeoutError, socket.gaierror):
        return True
    except (OSError, ValueError, HTTPException):
        return False


def _renewal_verified(entry):
    entry["certificate"]["last_renewal_check_at"] = now()
    entry["certificate"].pop("renewal_deferred", None)


def _defer_renewal(entry, *, unreachable):
    detail = ("Let's Encrypt staging is unreachable; renewal dry-run deferred." if unreachable else
              "Renewal dry-run failed while staging was reachable or its outage could not be established. " + REMEDIATION)
    entry["certificate"]["renewal_deferred"] = {"checked_at": now(), "detail": detail}
    print(RENEWAL_WARNING, file=sys.stderr)


def retry_deferred(host, registry, entry):
    if not entry["certificate"].get("renewal_deferred"):
        return
    try:
        _identity_ready(host, entry)
        _renewal_ready(host, entry)
        _renewal_dry_run(host, entry)
    except (DeployError, OSError, ValueError, HTTPException):
        _defer_renewal(entry, unreachable=_staging_unreachable())
    else:
        _renewal_verified(entry)
    host.journal(registry, entry)


def rotate_token(host, registry, entry, token):
    from runtime_config import update_bootstrap_secrets
    _validate_token(token)
    zone = _zone(entry)
    require(_resolve_zone(token, entry["profile"]["domain"]) == zone,
            "certificate_zone_invalid", "The replacement token must resolve the recorded Cloudflare zone.")
    affected = [item for item in registry["instances"].values() if item["certificate"].get("zone_ref") == zone]
    path = _credentials(host, entry)
    original = path.read_text()
    for item in affected:
        _identity_ready(host, item)
        _renewal_ready(host, item)
    try:
        host.owned_write(path, "dns_cloudflare_api_token = " + token + "\n", mode=0o600)
        for item in affected:
            _renewal_dry_run(host, item)
        update_bootstrap_secrets(host, affected, {"cloudflare_dns_token": token})
    except (DeployError, OSError):
        host.owned_write(path, original, mode=0o600)
        raise DeployError("certificate_token_rotation_failed", "DNS token verification or publication failed; the previous token was restored.") from None
    for item in affected:
        _renewal_verified(item)
    host.journal(registry)
    return {"ok": True, "instances": [item["name"] for item in affected]}


def _stage_credentials(host, registry, entry):
    """Consume root-only bootstrap stdin intake; never rotate a working token."""
    if _synthetic(host, entry):
        require(entry["certificate"].get("zone_ref") in (None, "acceptance-synthetic"),
                "certificate_ownership", REMEDIATION)
        if entry["certificate"].get("zone_ref") is None:
            entry["certificate"]["zone_ref"] = "acceptance-synthetic"
            host.journal(registry, entry)
        return
    intake = host.paths.instance_config(entry["name"]) / "bootstrap-secrets.json"
    if not intake.exists():
        return  # Already-configured lineages can operate from their zone reference.
    value = read_json(_private(host, intake))
    require(value.get("instance_id") == entry["profile"]["instance_id"],
            "certificate_ownership", "Private certificate intake belongs to another instance.")
    token = value.get("cloudflare_dns_token")
    _validate_token(token)
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
        if _private(host, path).read_text() != content:
            require(not any("certificate" in item["provisioning"]["completed_steps"]
                            and item["certificate"].get("zone_ref") in (None, _zone(entry))
                            for item in registry["instances"].values()),
                    "certificate_credentials_conflict", "The zone has working credentials; use ACTION=dns-token-rotate.")
            require(_resolve_zone(token, entry["profile"]["domain"]) == _zone(entry),
                    "certificate_credentials_conflict", "The corrected token must resolve the recorded Cloudflare zone.")
            host.owned_write(path, content, mode=0o600)
    else:
        host.owned_write(path, content, mode=0o600)


def _claim_path(host, entry):
    return check_path(host.paths.instance_config(entry["name"]) / "certificate.json", "file")


def _claim(entry):
    return {"instance_id": entry["profile"]["instance_id"], "domain": entry["profile"]["domain"],
            "lineage": _lineage(entry), "zone_ref": _zone(entry)}


def _renewal_path(host, entry):
    return check_path(host.paths.p(f"{CONFIG_DIR}/renewal/{_lineage(entry)}.conf"), "file")


def _options():
    return ["--config-dir", CONFIG_DIR, "--work-dir", WORK_DIR, "--logs-dir", LOGS_DIR,
            "--no-directory-hooks", "--config", CONFIG_DIR + "/cli.ini"]


def _directories(host, *, create=False):
    for directory in (CONFIG_DIR, WORK_DIR, LOGS_DIR):
        path = check_path(host.paths.p(directory), "directory")
        if create:
            path.mkdir(mode=0o700, parents=True, exist_ok=True)
        _private(host, path, directory=True)
    config = check_path(host.paths.p(CONFIG_DIR + "/cli.ini"), "file")
    content = "# Foundry-owned Certbot configuration; options are explicit.\n"
    if create and not config.exists():
        host.owned_write(config, content, mode=0o600)
    require(_private(host, config).read_text() == content, "certificate_renewal_invalid", REMEDIATION)


def _timer(entry):
    if entry["certificate"].get("zone_ref") == "acceptance-synthetic":
        return "foundry-cert-renew-" + entry["profile"]["instance_id"]
    return "foundry-certbot-renew"


def _units(entry, *, network=False):
    name = _timer(entry)
    command = "/usr/bin/certbot renew --quiet " + " ".join(_options())
    fixture_environment = ""
    if entry["certificate"].get("zone_ref") == "acceptance-synthetic":
        command = f"/usr/bin/python3 -I {HOST_PATHS['current']}/lib/certificates.py synthetic-renew {entry['profile']['instance_id']}"
        fixture_environment = "Environment=FOUNDRY_ACCEPTANCE_SYNTHETIC_CERT=1\n"
    result = {}
    for suffix in ("service", "timer"):
        content = (TEMPLATES / ("foundry-certbot-renew." + suffix)).read_text()
        postcondition = (f"ExecStartPost=/usr/bin/python3 -I {HOST_PATHS['current']}/lib/certificates.py verify-network-renewal\n"
                         if network else "")
        result[name + "." + suffix] = content.replace("@COMMAND@", command).replace("@ENVIRONMENT@", fixture_environment).replace("@POSTCONDITION@", postcondition)
    return result


def _timer_ready(host, entry, *, allow_legacy=False):
    required = _network_claim_path(host, entry).exists() and not allow_legacy
    for name, content in _units(entry, network=True).items():
        path = check_path(host.paths.p("/etc/systemd/system/" + name), "file")
        accepted = (content,) if required else (content, _units(entry)[name])
        require(path.exists() and path.read_text() in accepted and not path.stat().st_mode & 0o022
                and (path.stat().st_uid == 0 or host.paths.root != Path("/")),
                "certificate_renewal_invalid", REMEDIATION)
    timer = _timer(entry) + ".timer"
    for command in (["systemctl", "is-enabled", timer], ["systemctl", "is-active", timer]):
        require(host.run(command, check=False).returncode == 0, "certificate_renewal_invalid", REMEDIATION)


def _renewal_ready(host, entry, *, allow_legacy_hook=False):
    synthetic = _synthetic(host, entry)
    _directories(host)
    credentials = None if synthetic else _credentials(host, entry)
    path = _renewal_path(host, entry)
    require(path.exists(), "certificate_renewal_invalid", REMEDIATION)
    parser = configparser.ConfigParser(interpolation=None, strict=True)
    try:
        parser.read_string("[lineage]\n" + path.read_text())
        params = parser["renewalparams"]
        provider_ready = (params.get("authenticator") == "acceptance-synthetic" if synthetic else
                          params.get("authenticator") == "dns-cloudflare"
                          and params.get("dns_cloudflare_credentials") == str(credentials)
                          and params.get("dns_cloudflare_propagation_seconds") == "60")
        # Certbot persists --deploy-hook under the name renew_hook.
        require(provider_ready and (params.get("renew_hook") == _reload_hook(host, entry)
                                    or allow_legacy_hook and params.get("renew_hook") == RELOAD_HOOK)
                and not params.get("pre_hook") and not params.get("post_hook"),
                "certificate_renewal_invalid", REMEDIATION)
        live = host.paths.p(CONFIG_DIR + "/live/" + _lineage(entry))
        for key, name in (("cert", "cert.pem"), ("privkey", "privkey.pem"),
                          ("chain", "chain.pem"), ("fullchain", "fullchain.pem")):
            require(parser["lineage"].get(key) == str(live / name), "certificate_renewal_invalid", REMEDIATION)
    except (configparser.Error, KeyError, UnicodeError):
        raise DeployError("certificate_renewal_invalid", REMEDIATION) from None
    _timer_ready(host, entry, allow_legacy=allow_legacy_hook)


def certificate_ready(host: Host, entry: dict, *, hostnames=None) -> bool:
    """Check expiry and hostname separately (openssl otherwise drops a check)."""
    domain = entry["profile"]["domain"]
    certificate = host.paths.p(f"{CONFIG_DIR}/live/{domain}/fullchain.pem")
    if not certificate.exists():
        return False
    command = ["openssl", "x509", "-noout", "-in", str(certificate)]
    for arguments in (["-checkend", "0"], *(["-checkhost", name] for name in (hostnames or _hostnames(host, entry)))):
        try:
            result = host.run(command + arguments, check=False, timeout=10)
        except (OSError, DeployError):
            raise DeployError("certificate_invalid", "Existing certificate could not be inspected.") from None
        if result.returncode:
            raise DeployError("certificate_invalid", "Existing certificate is expired, malformed, or issued for another hostname. " + REMEDIATION)
    return True


def _identity_ready(host, entry, *, hostnames=None):
    require(read_json(_private(host, _claim_path(host, entry))) == _claim(entry),
            "certificate_ownership", REMEDIATION)
    lineage = _lineage(entry)
    live = host.paths.p(CONFIG_DIR + "/live/" + lineage)
    archive = check_path(host.paths.p(CONFIG_DIR + "/archive/" + lineage), "directory")
    # Certbot live files must resolve within this lineage's archive, not another site.
    check_path(live, "directory")
    for name in ("cert.pem", "chain.pem", "fullchain.pem", "privkey.pem"):
        path = live / name
        require(path.is_symlink() and path.resolve().parent == archive and path.resolve().is_file(),
                "certificate_ownership", REMEDIATION)
    require(certificate_ready(host, entry, hostnames=hostnames), "certificate_invalid", REMEDIATION)
    certificate = host.run(["openssl", "x509", "-in", str(live / "cert.pem"), "-pubkey", "-noout"])
    key = host.run(["openssl", "pkey", "-in", str(live / "privkey.pem"), "-pubout"])
    require(bool(certificate.stdout.strip()) and certificate.stdout == key.stdout,
            "certificate_invalid", REMEDIATION)
    host.run(["openssl", "verify", "-CApath", str(host.paths.p("/etc/ssl/certs")),
              "-untrusted", str(live / "chain.pem"), str(live / "cert.pem")])


def inspect(host: Host, entry: dict) -> dict:
    """Read-only deterministic observation. A dry-run is required by provision."""
    lineage = _lineage(entry)
    live = check_path(host.paths.p(CONFIG_DIR + "/live/" + lineage), "directory")
    check_path(host.paths.p(CONFIG_DIR + "/archive/" + lineage), "directory")
    if not live.exists() and not _renewal_path(host, entry).exists():
        return {"state": "missing", "lineage": lineage}
    try:
        _identity_ready(host, entry)
        _renewal_ready(host, entry)
    except (DeployError, OSError, ValueError):
        return {"state": "conflict", "lineage": lineage, "remediation": REMEDIATION}
    if _network_claim_path(host, entry).exists():
        status = network_renewal_status(host, entry)
        if status["state"] != "verified":
            return {"state": "conflict", "lineage": lineage, "effective_renewal": status, "remediation": REMEDIATION}
    return {"state": "ready", "lineage": lineage, "zone_ref": _zone(entry)}


def provision(host: Host, registry: dict, entry: dict) -> None:
    """Reconcile only owned DNS lineages; A03 applies on every reuse."""
    _stage_credentials(host, registry, entry)
    synthetic = _synthetic(host, entry)
    credentials = None if synthetic else _credentials(host, entry)
    observed = inspect(host, entry)
    require(observed["state"] != "conflict", "certificate_renewal_invalid", REMEDIATION)
    if not synthetic:
        packages.install_missing(host, ["certbot", "python3-certbot-dns-cloudflare"])
    if observed["state"] == "missing":
        _directories(host, create=True)
        claim = _claim_path(host, entry)
        if claim.exists():
            require(read_json(_private(host, claim)) == _claim(entry), "certificate_ownership", REMEDIATION)
        else:
            host.owned_write(claim, json.dumps(_claim(entry)) + "\n", mode=0o600)
        for name, content in _units(entry).items():
            path = host.paths.p("/etc/systemd/system/" + name)
            enforced = _units(entry, network=True)[name]
            require(not path.exists() or check_path(path).read_text() in (content, enforced),
                    "certificate_ownership", REMEDIATION)
            if path.exists() and path.read_text() == enforced:
                content = enforced
            host.owned_write(path, content)
        host.run(["systemctl", "daemon-reload"])
        host.run(["systemctl", "enable", "--now", _timer(entry) + ".timer"])
        if synthetic:
            _synthetic_issue(host, entry)
        else:
            host.run(["certbot", "certonly", "--non-interactive", "--agree-tos", "--dns-cloudflare",
                      "--dns-cloudflare-credentials", str(credentials), "--dns-cloudflare-propagation-seconds", "60",
                      "--cert-name", _lineage(entry), "--email", entry["profile"]["certbot_email"],
                      "--domain", entry["profile"]["domain"], "--deploy-hook", RELOAD_HOOK] + _options(), timeout=900)
    _identity_ready(host, entry)
    _renewal_ready(host, entry)
    try:
        if synthetic:
            # Exercise a second issuance and the real lineage/reload checks.
            _synthetic_issue(host, entry)
        else:
            # Certbot otherwise sleeps up to 8 minutes without a terminal, holding the host lock.
            _renewal_dry_run(host, entry)
    except (DeployError, OSError):
        if synthetic or not _staging_unreachable():
            raise DeployError("certificate_renewal_failed", "DNS-01 renewal dry-run failed. " + REMEDIATION) from None
        _defer_renewal(entry, unreachable=True)
    else:
        _renewal_verified(entry)
    if "certificate" not in entry["provisioning"]["completed_steps"]:
        entry["provisioning"]["completed_steps"].append("certificate")
    host.journal(registry, entry)


def _main(arguments):
    if arguments == ["verify-network-renewal"]:
        require(os.geteuid() == 0, "certificate_ownership", REMEDIATION)
        host = Host()
        # Certbot may run under the caller's lock. Write only lineage observation
        # receipts here, not registry/admission state or a second locking protocol.
        registry = host.read_registry()
        failed = False
        for entry in registry["instances"].values():
            if _network_claim_path(host, entry).exists():
                try:
                    verify_network_postcondition(host, entry)
                except DeployError:
                    failed = True
        require(not failed, "certificate_effective_renewal", "A selected TLS consumer failed effective renewal verification.")
        return
    require(os.geteuid() == 0 and len(arguments) == 2 and arguments[0] == "synthetic-renew"
            and valid_uuid(arguments[1]),
            "certificate_fixture_forbidden", "Unknown certificate fixture operation.")
    host = Host()
    with host.lock("host"), host.lock(arguments[1]):
        registry = host.read_registry()
        entry = registry["instances"].get(arguments[1])
        require(entry is not None and _synthetic(host, entry),
                "certificate_fixture_forbidden", "Synthetic renewal requires its registered acceptance instance.")
        _identity_ready(host, entry)
        _renewal_ready(host, entry)
        _synthetic_issue(host, entry)
        _renewal_verified(entry)
        host.journal(registry, entry)


def _network_claim_path(host, entry):
    return check_path(host.paths.p(instance_paths(entry["name"])["configuration"]) / "certificate-network.json", "file")


def _network_claim(host, entry):
    path = _network_claim_path(host, entry)
    if not path.exists():
        return None
    value = read_json(_private(host, path))
    require(set(value) == {"instance_id", "hostnames", "listeners"}
            and value["instance_id"] == entry["profile"]["instance_id"], "certificate_ownership", REMEDIATION)
    require(isinstance(value["hostnames"], list) and 1 <= len(value["hostnames"]) <= 100,
            "certificate_names", "The owned SAN inventory exceeds the certificate limit.")
    for name in value["hostnames"]:
        dns_hostname(name)
    from topology import PublicListenerBinding
    require(isinstance(value["listeners"], dict) and len(value["listeners"]) <= 256,
            "certificate_ownership", REMEDIATION)
    for binding in value["listeners"].values():
        require(PublicListenerBinding.parse(binding).hostname in value["hostnames"], "certificate_ownership", REMEDIATION)
    for key in value["listeners"]:
        name, separator, checksum = key.partition(":")
        require(separator and len(name.split(".")) == 2, "certificate_ownership", REMEDIATION)
        for part in name.split("."):
            contract_name(part)
        contract_digest(checksum)
    return value


def _hostnames(host, entry):
    claim = _network_claim(host, entry)
    return tuple(claim["hostnames"]) if claim else (entry["profile"]["domain"],)


def _reload_hook(host, entry):
    return NETWORK_RELOAD_HOOK if _network_claim_path(host, entry).exists() else RELOAD_HOOK


def _install_network_postcondition(host, entry):
    """Upgrade only the two known owned unit projections, preserving legacy sites."""
    changed = False
    for name, content in _units(entry, network=True).items():
        path = check_path(host.paths.p("/etc/systemd/system/" + name), "file")
        require(path.exists() and path.read_text() in (_units(entry)[name], content), "certificate_ownership", REMEDIATION)
        changed = host.owned_write(path, content) or changed
    if changed:
        host.run(["systemctl", "daemon-reload"])


def network_renewal_status(host, entry):
    if not _network_claim_path(host, entry).exists():
        return None
    path = _network_status_path(host, entry)
    if not path.exists():
        return {"state": "unverified", "checked_at": None}
    value = read_json(_private(host, path))
    require(set(value) == {"state", "checked_at"} and value["state"] in ("pending", "failed", "verified"),
            "certificate_ownership", REMEDIATION)
    from contracts import valid_timestamp
    require(valid_timestamp(value["checked_at"]), "certificate_ownership", REMEDIATION)
    return value


def verify_network_postcondition(host, entry):
    """Certbot exit 0 and hook success are insufficient: persist an independent result."""
    path = _network_status_path(host, entry)
    def record(state):
        host.owned_write(path, json.dumps({"state": state, "checked_at": now()}) + "\n", mode=0o600)
    record("pending")
    try:
        result = verify_network_consumers(host, entry)
    except (DeployError, OSError, ValueError):
        record("failed")
        raise DeployError("certificate_effective_renewal", "A selected TLS endpoint did not complete a handshake with the current leaf.") from None
    record("verified")
    return result


def _network_status_path(host, entry):
    return check_path(host.paths.p(instance_paths(entry["name"])["configuration"]) / "certificate-network-status.json", "file")


def ensure_hostnames(host, registry, entry, profiles, *, topologies):
    """Extend the lineage with TLS names from paired profiles and topologies.

    Call before opening TCP. Existing SANs are never removed, including on rollback.
    A durable intent survives an uncertain issuance and is retried with the same
    superset. This issues DNS-01 certificates; it never writes application A/AAAA.
    """
    from listeners import public_policies
    profiles, topologies = tuple(profiles), tuple(topologies)
    require(len(profiles) == len(topologies), "listener_topology_required", "Supply a topology for every certificate profile.")
    tls_bindings = []
    for profile, topology in zip(profiles, topologies):
        require(profile.instance_id == entry["profile"]["instance_id"], "certificate_ownership", REMEDIATION)
        policies = public_policies(profile, topology)
        tls_bindings.extend((name, binding, profile.projection()["listeners"][name])
                            for name, binding in profile.listeners if policies[name].tls == "edge")
    if not tls_bindings:
        return
    domain = entry["profile"]["domain"]
    _identity_ready(host, entry, hostnames=(domain,))
    _renewal_ready(host, entry, allow_legacy_hook=True)
    prior = _network_claim(host, entry)
    names = set(prior["hostnames"] if prior else (domain,))
    listeners = dict(prior["listeners"] if prior else {})
    for name, binding, raw in tls_bindings:
        names.add(binding.hostname)
        # Retain different address generations too; only currently selected TLS files are probed.
        key = name + ":" + hashlib.sha256(json.dumps(raw, sort_keys=True).encode()).hexdigest()
        listeners[key] = raw
    live = host.paths.p(CONFIG_DIR + "/live/" + _lineage(entry))
    sans = host.run(["openssl", "x509", "-in", str(live / "cert.pem"), "-noout", "-ext", "subjectAltName"], timeout=10).stdout
    for name in re.findall(r"DNS:([^,\s]+)", sans):
        names.add(dns_hostname(name))
    require(len(names) <= 100 and len(listeners) <= 256, "certificate_names", "The retained certificate inventory exceeds its bound.")
    if prior and set(prior["hostnames"]) == names and prior["listeners"] == listeners:
        try:
            _identity_ready(host, entry)
            _renewal_ready(host, entry)
        except DeployError:
            pass  # Resume an interrupted issuance with the same durable SAN intent.
        else:
            verify_network_postcondition(host, entry)
            return
    synthetic = _synthetic(host, entry)
    if synthetic:
        require(all(name.endswith(".acceptance.invalid") for name in names), "certificate_fixture_forbidden", REMEDIATION)
    else:
        token = _credentials(host, entry).read_text().strip().split(" = ", 1)[1]
        require(all(_resolve_zone(token, name) == _zone(entry) for name in names),
                "certificate_zone_invalid", "Every TCP hostname must belong to the lineage's explicitly scoped zone.")
    _install_network_postcondition(host, entry)
    host.owned_write(_network_claim_path(host, entry), json.dumps({"instance_id": entry["profile"]["instance_id"],
                     "hostnames": sorted(names), "listeners": listeners}, sort_keys=True) + "\n", mode=0o600)
    if synthetic:
        _synthetic_issue(host, entry)
    else:
        arguments = ["certbot", "certonly", "--non-interactive", "--agree-tos", "--dns-cloudflare",
                     "--dns-cloudflare-credentials", str(_credentials(host, entry)), "--dns-cloudflare-propagation-seconds", "60",
                     "--cert-name", _lineage(entry), "--email", entry["profile"]["certbot_email"],
                     "--renew-with-new-domains", "--deploy-hook", NETWORK_RELOAD_HOOK]
        if prior is None:
            arguments.append("--force-renewal")
        for name in sorted(names):
            arguments.extend(("--domain", name))
        host.run(arguments + _options(), timeout=900)
    _identity_ready(host, entry)
    _renewal_ready(host, entry)
    if not synthetic:
        _renewal_dry_run(host, entry)
    # A failed deploy hook does not necessarily make Certbot exit nonzero.
    verify_network_postcondition(host, entry)
    _renewal_verified(entry)
    host.journal(registry, entry)


def verify_network_consumers(host, entry):
    """Fresh TLS handshakes must serve the actual renewed leaf on HTTP and TCP.

    Probe only selected runtime projections. Closed/pending routes stay closed and
    are checked on opening instead. Loopback source is explicitly allowed by the
    owned stream template; public CIDRs are not widened for this health check.
    """
    from topology import PublicListenerBinding
    from edge import RUNTIME
    claim = _network_claim(host, entry)
    require(claim is not None, "certificate_ownership", REMEDIATION)
    _identity_ready(host, entry)
    _renewal_ready(host, entry)
    live = host.paths.p(CONFIG_DIR + "/live/" + _lineage(entry) + "/cert.pem")
    expected = hashlib.sha256(ssl.PEM_cert_to_DER_cert(live.read_text())).digest()
    runtime = host.paths.p(RUNTIME + "/" + entry["profile"]["instance_id"])
    targets = set()
    if (runtime / "http-open.conf").exists():
        targets.add((entry["profile"]["domain"], "127.0.0.1", 443))
    for key, raw in claim["listeners"].items():
        name = key.split(":", 1)[0]
        path = check_path(runtime / ("stream-" + name + ".conf"), "file")
        if not path.exists():
            continue
        binding = PublicListenerBinding.parse(raw)
        selected = path.read_text()
        for address in binding.public_addresses:
            for port in binding.ports:
                literal = ("[" + address + "]" if ":" in address else address) + ":" + str(port)
                if "    listen " + literal + " ssl" in selected:
                    targets.add((binding.hostname, address, port))
    context = ssl.create_default_context()
    budget = time.monotonic() + 30
    for hostname, address, port in sorted(targets):
        source = "::1" if ":" in address else "127.0.0.1"
        matched = False
        # Reload is graceful/asynchronous: require a new connection using the new leaf.
        deadline = min(budget, time.monotonic() + 10)
        while time.monotonic() < deadline:
            try:
                with socket.create_connection((address, port), timeout=1, source_address=(source, 0)) as connection:
                    with context.wrap_socket(connection, server_hostname=hostname) as secured:
                        matched = hashlib.sha256(secured.getpeercert(binary_form=True)).digest() == expected
            except (OSError, ssl.SSLError):
                matched = False
            if matched:
                break
            time.sleep(0.1)
        require(matched, "certificate_effective_renewal", "A selected HTTP/TCP TLS consumer did not serve the renewed certificate.")
    return {"state": "effective" if targets else "closed", "consumers": len(targets)}


if __name__ == "__main__":
    try:
        _main(sys.argv[1:])
    except (DeployError, OSError, ValueError, KeyError):
        sys.stderr.write("Foundry certificate operation failed; inspect the private host configuration.\n")
        sys.exit(1)
