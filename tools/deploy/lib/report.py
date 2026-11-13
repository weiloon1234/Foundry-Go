"""Read-only host reporting. Never include configuration, command output or logs."""

from __future__ import annotations

import datetime as dt
import ipaddress
import os
import re
import shutil
import socket
import ssl
import time
import urllib.error
import urllib.request
import maintenance

from contracts import BACKUP_FRESH_SECONDS, BACKUP_POLICY, DeployError, INSTANCE_STATUSES, PREFIX, SCHEMA, SLUG_PATTERN, check_path, instance_paths, now, parse_json, valid_uuid
from topology import instance_units
from release import enabled_roles, readiness_body, service as role_service
from host import OperationTimeout as ReportBudgetExceeded, wall_clock_budget

REPORT_SCHEMA = 2
CHECK_TIMEOUT = 1.0
REPORT_BUDGET = 15.0
MAX_INSTANCES = 256
MAX_FAILED_UNITS = 32
PUBLIC_PORTS = frozenset({22, 80, 443})
RESOURCE_LOW_PERCENT = 10
DISK_LOW_BYTES = 2 * 1024 ** 3
CERTIFICATE_EXPIRING_SECONDS = 14 * 24 * 60 * 60
DOMAIN = re.compile(r"(?=.{1,253}\Z)[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?\Z")



def problem(problems, code, instance=None, *, detail=None):
    item = {"code": code}
    if instance:
        item["instance_id"] = instance
    if detail:
        item["detail"] = detail
    if item not in problems:
        problems.append(item)


def string(value, maximum=256):
    return value if isinstance(value, str) and len(value) <= maximum and not any(ord(c) < 32 for c in value) else None


def integer(value):
    return value if isinstance(value, int) and not isinstance(value, bool) and value >= 0 else None


def identifier(value):
    return value if valid_uuid(value) else None


def release(value):
    if not isinstance(value, dict):
        return None
    # The release manifest is extensible. Explicitly select public report fields.
    return {key: string(value.get(key)) for key in ("version", "commit", "tree_state", "built_at", "architecture")}


def attempt(value):
    if not isinstance(value, dict):
        return None
    result = {key: string(value.get(key)) for key in ("id", "action", "release", "started_at", "finished_at", "outcome", "phase")}
    # Failure details may contain command diagnostics; publish only the machine code.
    error = value.get("error")
    code = error
    result["error_code"] = code if isinstance(code, str) and len(code) <= 128 and re.fullmatch(r"[a-z][a-z0-9_]*", code) else None
    return result


def remaining(deadline):
    budget = deadline - time.monotonic()
    if budget <= 0:
        raise ReportBudgetExceeded("report budget exhausted")
    return min(CHECK_TIMEOUT, budget)


def probe(host, arguments, deadline, problems, code, check=True):
    """Run one read-only probe, keeping command failures out of the report."""
    try:
        return host.run(arguments, timeout=remaining(deadline), check=check)
    except ReportBudgetExceeded:
        raise
    except Exception:
        if code is not None:
            problem(problems, code)
        return None


def failed_units(host, problems, deadline):
    result = probe(host, ["systemctl", "list-units", "--state=failed", "--all", "--no-legend", "--plain"],
                   deadline, problems, "failed_units_unavailable")
    if result is None:
        return None
    units = []
    for line in result.stdout.splitlines():
        columns = line.split()
        if not columns:
            continue
        name = string(columns[0])
        if name is None:
            continue
        units.append(name)
        if len(units) == MAX_FAILED_UNITS:
            break
    if units:
        problem(problems, "units_failed")
    return units


def listener(value):
    address, separator, port = value.rpartition(":")
    if not separator or not port.isdigit():
        return None
    return address.strip("[]"), int(port)


def loopback(address):
    try:
        # ss prints systemd-resolved's stub listener as 127.0.0.53%lo.
        parsed = ipaddress.ip_address(address.split("%", 1)[0])
    except ValueError:
        return False
    if isinstance(parsed, ipaddress.IPv6Address) and parsed.ipv4_mapped:
        return parsed.ipv4_mapped.is_loopback
    return parsed.is_loopback


def network(host, problems, deadline, *, claims=()):
    response = probe(host, ["ss", "-H", "-ltn"], deadline, problems, "network_unavailable")
    listeners = None
    if response is not None:
        ports, unexpected = set(), False
        for line in response.stdout.splitlines():
            columns = line.split()
            if len(columns) < 4:
                continue
            observed = listener(columns[3])
            if observed is None:
                continue
            address, port = observed
            if loopback(address):
                continue
            ports.add(port)
            if port not in PUBLIC_PORTS and not any(c.socket.transport == "tcp" and c.socket.address == address
                                                    and c.socket.port == port for c in claims):
                unexpected = True
        listeners = sorted(ports)
        if unexpected:
            problem(problems, "unexpected_public_listener")
    firewall = None
    if os.geteuid() == 0:
        status = probe(host, ["ufw", "status"], deadline, problems, None, check=False)
        if status is not None and status.returncode == 0:
            lines = status.stdout.splitlines()
            if lines and lines[0].strip() in {"Status: active", "Status: inactive"}:
                firewall = lines[0].strip().split(":", 1)[1].strip()
    return {"public_listeners": listeners, "firewall": firewall}


def ssh(host, problems, deadline):
    result = {"password_authentication": None, "root_login": None}
    if os.geteuid() != 0:
        return result
    response = probe(host, ["sshd", "-T"], deadline, problems, None, check=False)
    if response is None or response.returncode != 0:
        return result
    for line in response.stdout.splitlines():
        fields = line.split(None, 1)
        if len(fields) != 2:
            continue
        key, value = fields[0], fields[1].strip()
        if key == "passwordauthentication":
            if value == "yes":
                result["password_authentication"] = True
            elif value == "no":
                result["password_authentication"] = False
        elif key == "permitrootlogin":
            result["root_login"] = string(value)
    if result["password_authentication"] is True:
        problem(problems, "ssh_password_login")
    return result


def certificate_problems(problems, instance_id, value):
    if not isinstance(value, dict):
        return
    if value.get("status") == "invalid":
        problem(problems, "certificate_invalid", instance_id)
        return
    if value.get("status") != "valid":
        return
    expires_at = value.get("expires_at")
    if not isinstance(expires_at, str):
        return
    try:
        expires = dt.datetime.fromisoformat(expires_at.replace("Z", "+00:00"))
    except ValueError:
        return
    if expires.tzinfo is None:
        return
    if expires - dt.datetime.now(dt.timezone.utc) < dt.timedelta(seconds=CERTIFICATE_EXPIRING_SECONDS):
        problem(problems, "certificate_expiring", instance_id)


def service(host, unit, deadline):
    values = role_service(host, unit, timeout=remaining(deadline))
    active = values.get("ActiveState")
    substate = values.get("SubState")
    load = values.get("LoadState")
    if load not in {"stub", "loaded", "not-found", "bad-setting", "error", "merged", "masked"}:
        raise ValueError("invalid unit load status")
    if active not in {"active", "inactive", "failed", "activating", "deactivating", "reloading", "maintenance", "refreshing"}:
        raise ValueError("invalid service status")
    pid = values.get("MainPID")
    restarts = values.get("NRestarts")
    return {"load": load, "active": active, "substate": string(substate, 64),
            "main_pid": int(pid) if isinstance(pid, (str, int)) and str(pid).isdigit() else None,
            "restarts": int(restarts) if isinstance(restarts, (str, int)) and str(restarts).isdigit() else None,
            "exe": string(values.get("exe"), 4096)}


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def readiness(port, deadline):
    if not isinstance(port, int) or isinstance(port, bool) or not 1024 <= port <= 65535:
        raise ValueError("invalid port")
    # Never honor operator proxy variables or follow a redirect off loopback.
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
    request = urllib.request.Request(f"http://127.0.0.1:{port}/health/ready", method="GET")
    try:
        with opener.open(request, timeout=remaining(deadline)) as response:
            payload = response.read(4097)
            try:
                body = parse_json(payload) if len(payload) <= 4096 else None
                ready = readiness_body(body)
            except (DeployError, ValueError, UnicodeError):
                ready = False
            return {"ready": response.status == 200 and ready, "http_status": response.status}
    except urllib.error.HTTPError as error:
        return {"ready": False, "http_status": error.code}


def certificate(domain, deadline):
    if not domain:
        return {"status": "unconfigured", "expires_at": None}
    if not isinstance(domain, str) or not DOMAIN.fullmatch(domain):
        raise ValueError("invalid domain")
    context = ssl.create_default_context()
    try:
        with socket.create_connection(("127.0.0.1", 443), timeout=remaining(deadline)) as connection:
            connection.settimeout(remaining(deadline))
            with context.wrap_socket(connection, server_hostname=domain) as tls:
                expires = ssl.cert_time_to_seconds(tls.getpeercert()["notAfter"])
                return {"status": "valid", "expires_at": dt.datetime.fromtimestamp(expires, dt.timezone.utc).isoformat(timespec="seconds").replace("+00:00", "Z")}
    except ssl.SSLCertVerificationError:
        return {"status": "invalid", "expires_at": None}


def resources(host, problems, result):
    result["cpu_count"] = os.cpu_count()
    try:
        result["load_average"] = list(os.getloadavg())
    except ReportBudgetExceeded:
        raise
    except (AttributeError, OSError):
        problem(problems, "load_unavailable")
    try:
        memory = {}
        for line in host.paths.p("/proc/meminfo").read_text().splitlines():
            key, value = line.split(":", 1)
            if key in {"MemTotal", "MemAvailable"}:
                memory[key] = int(value.strip().split()[0]) * 1024
        result["memory_total_bytes"] = memory["MemTotal"]
        result["memory_available_bytes"] = memory["MemAvailable"]
    except ReportBudgetExceeded:
        raise
    except (OSError, ValueError, KeyError, IndexError):
        problem(problems, "memory_unavailable")
    try:
        disk = shutil.disk_usage(host.paths.p("/"))
        result["disk_total_bytes"], result["disk_available_bytes"] = disk.total, disk.free
    except ReportBudgetExceeded:
        raise
    except OSError:
        problem(problems, "disk_unavailable")
    try:
        result["uptime_seconds"] = int(float(host.paths.p("/proc/uptime").read_text().split()[0]))
    except ReportBudgetExceeded:
        raise
    except (OSError, ValueError, IndexError):
        problem(problems, "uptime_unavailable")
    memory_total = result.get("memory_total_bytes")
    memory_available = result.get("memory_available_bytes")
    if memory_total and memory_available is not None and memory_available * 100 < memory_total * RESOURCE_LOW_PERCENT:
        problem(problems, "memory_low")
    disk_total = result.get("disk_total_bytes")
    disk_available = result.get("disk_available_bytes")
    if disk_total and disk_available is not None and (disk_available < DISK_LOW_BYTES
                                                      or disk_available * 100 < disk_total * RESOURCE_LOW_PERCENT):
        problem(problems, "disk_low")
    return result


def checked(problems, code, instance_id, function, *args):
    try:
        return function(*args)
    except ReportBudgetExceeded:
        raise
    except (DeployError, OSError, ValueError, KeyError):
        problem(problems, code, instance_id)
        return None


def backup_freshness(entry, problems, instance_id):
    """Project only F3's recorded completion receipts; never probe a repository."""
    value = entry.get("backup") if isinstance(entry.get("backup"), dict) else {}
    bundle = value.get("database") if isinstance(value.get("database"), dict) else {}
    point = bundle.get("database_point") if isinstance(bundle.get("database_point"), dict) else {}
    keys = bundle.get("key_archive") if isinstance(bundle.get("key_archive"), dict) else {}
    complete_at = string(value.get("last_complete_at"), 64)
    components_valid = (bundle.get("status") == point.get("status") == keys.get("status") == "complete"
                        and bundle.get("configuration_revision") == entry.get("configuration_revision"))
    if components_valid:
        try:
            completed = dt.datetime.fromisoformat(point["completed_at"].replace("Z", "+00:00"))
            if completed.tzinfo is None or completed > dt.datetime.now(dt.timezone.utc):
                raise ValueError("invalid component timestamp")
            complete_at = completed.isoformat()
        except (KeyError, TypeError, ValueError, AttributeError):
            components_valid = False
            problem(problems, "backup_state_invalid", instance_id)
    age = None
    if complete_at:
        try:
            completed = dt.datetime.fromisoformat(complete_at.replace("Z", "+00:00"))
            if completed.tzinfo is None:
                raise ValueError("completion timestamp lacks timezone")
            elapsed = (dt.datetime.now(dt.timezone.utc) - completed).total_seconds()
            if elapsed < 0:
                raise ValueError("completion timestamp is in the future")
            age = int(elapsed)
        except ValueError:
            complete_at = None
            problem(problems, "backup_state_invalid", instance_id)
    policy = value.get("policy") if isinstance(value.get("policy"), dict) else {}
    public_policy = {key: integer(policy.get(key)) for key in BACKUP_POLICY}
    configured = bool(value.get("destination_ref"))
    complete_id = string(value.get("last_complete_id"))
    fresh = bool(complete_id) and components_valid and age is not None and age < BACKUP_FRESH_SECONDS if configured else None
    outcome = value.get("last_attempt_outcome")
    allowed_outcomes = {"complete", "succeeded", "failed", "running", "unknown"}
    outcome = outcome if isinstance(outcome, str) and outcome in allowed_outcomes else None
    drill_outcome = value.get("restore_drill_outcome")
    drill_outcome = drill_outcome if isinstance(drill_outcome, str) and drill_outcome in allowed_outcomes else None
    if configured and not fresh:
        problem(problems, "backup_stale", instance_id)
    if outcome == "failed":
        problem(problems, "backup_failed", instance_id)
    return {"last_complete_id": complete_id, "last_complete_at": complete_at,
            "last_attempt_outcome": outcome, "age_seconds": age, "fresh": fresh,
            "retention_policy": public_policy, "last_restore_drill_at": string(value.get("last_restore_drill_at"), 64),
            "restore_drill_outcome": drill_outcome}


def instance_report(host, entry, deadline, problems, *, host_claims=()):
    name = entry.get("name")
    instance_id = identifier(entry.get("profile", {}).get("instance_id"))
    if not isinstance(name, str) or len(name) > 31 or not SLUG_PATTERN.fullmatch(name) or not instance_id:
        problem(problems, "instance_identity_invalid")
        return None
    profile = entry.get("profile") if isinstance(entry.get("profile"), dict) else {}
    provisioning = entry.get("provisioning") if isinstance(entry.get("provisioning"), dict) else {}
    state = None
    try:
        state = host.load_state(entry)
        if not isinstance(state, dict) or state.get("instance_id") != instance_id or state.get("schema") != SCHEMA:
            raise ValueError("state identity mismatch")
    except ReportBudgetExceeded:
        raise
    except Exception:
        # Host errors are deliberately not rendered: their text can carry paths/diagnostics.
        problem(problems, "instance_state_unavailable", instance_id)
        state = {}
    recorded_status = state.get("status")
    if recorded_status not in INSTANCE_STATUSES:
        recorded_status = "unknown"
        problem(problems, "instance_status_unknown", instance_id)
    rehearsal = provisioning.get("state") == "rehearsal"
    expected_unit = instance_units(entry)["http"]
    unit_status = checked(problems, "service_check_unavailable", instance_id, service, host, expected_unit, deadline)
    topology = {role: {"unit": unit, "state": checked(problems, "service_check_unavailable", instance_id, service, host, unit, deadline)}
                for role, unit in instance_units(entry).items() if role != "http" and not rehearsal}
    topology["http"] = {"unit": expected_unit, "state": unit_status}
    ready = None if recorded_status == "never_deployed" and not state.get("current") else checked(problems, "readiness_check_unavailable", instance_id, readiness, entry["allocations"]["http_port"], deadline)
    live_maintenance = None
    if os.geteuid() == 0:
        current_path = host.paths.opt(name) / "current"
        if current_path.is_symlink():
            live_maintenance = checked(problems, "maintenance_probe_unavailable", instance_id,
                                       maintenance.status, host, entry, current_path)
    elif ready is not None and not ready["ready"]:
        problem(problems, "maintenance_probe_unavailable", instance_id,
                detail="Run the report as root to distinguish maintenance from an outage.")
    if ready is not None and live_maintenance and live_maintenance["down"]:
        ready = {**ready, "status": "maintenance"}
    cert = None
    if not rehearsal:
        cert = checked(problems, "certificate_check_unavailable", instance_id, certificate, profile.get("domain"), deadline)
        certificate_problems(problems, instance_id, cert)
        deferred = entry.get("certificate", {}).get("renewal_deferred")
        if deferred:
            problem(problems, "certificate_renewal_unverified", instance_id, detail=string(deferred.get("detail"), 1024))
            if cert is not None:
                cert["renewal_deferred"] = {"checked_at": string(deferred.get("checked_at")),
                                            "detail": string(deferred.get("detail"), 1024)}
        import certificates
        effective = checked(problems, "certificate_effective_renewal_unverified", instance_id,
                            certificates.network_renewal_status, host, entry)
        if effective is not None:
            if cert is not None:
                cert["effective_renewal"] = effective
            if effective["state"] != "verified":
                problem(problems, "certificate_effective_renewal_unverified", instance_id)
    if recorded_status in {"stopped", "degraded"}:
        enabled = checked(problems, "service_enablement_unavailable", instance_id,
                          enabled_roles, host, entry, remaining(deadline))
        if enabled is not None:
            for role, item in topology.items():
                item["enabled"] = role in enabled
                if role in enabled and item["state"] and item["state"]["active"] == "inactive":
                    action = f"ACTION=stop ROLE={role}" if recorded_status == "degraded" else "ACTION=stop"
                    problem(problems, "stopped_role_enabled", instance_id,
                            detail=f"{role} ({item['unit']}) is inactive but enabled and will start on reboot. Run {action} again to keep it stopped.")
    if recorded_status == "degraded":
        problem(problems, "instance_degraded", instance_id)
    if state.get("current") and not rehearsal and recorded_status != "stopped":
        for item in topology.values():
            if item["state"] and item["state"]["active"] == "inactive":
                problem(problems, "role_inactive", instance_id)
    current, previous = release(state.get("current")), release(state.get("previous"))
    consistent = True
    if recorded_status in {"healthy", "rolled_back"} and (not current or not ready or (not ready["ready"] and ready.get("status") != "maintenance") or not unit_status or unit_status["active"] != "active"):
        consistent = False
    if recorded_status in {"healthy", "rolled_back"} and any(not item["state"] or item["state"]["active"] != "active" for item in topology.values()):
        consistent = False
    if recorded_status == "never_deployed" and (current or (unit_status and unit_status["active"] not in {"inactive", "failed"})):
        consistent = False
    if recorded_status in {"never_deployed", "stopped"} and any(item["state"] and item["state"]["active"] not in {"inactive", "failed"} for item in topology.values()):
        consistent = False
    if provisioning.get("state") == "ready" and unit_status and unit_status.get("load") != "loaded":
        consistent = False
    if provisioning.get("state") == "ready" and any(not item["state"] or item["state"].get("load") != "loaded" for item in topology.values()):
        consistent = False
    if state.get("configuration_revision") != entry.get("configuration_revision"):
        consistent = False
    current_path = host.paths.opt(name) / "current"
    expected_executable = None
    try:
        if current:
            expected = host.paths.opt(name) / "releases" / (current["version"] or "")
            # A matching dangling link or a release-directory symlink is stale
            # metadata, even if an already running process still serves traffic.
            check_path(expected, "directory")
            expected_executable = str(expected / "artisan")
            consistent = consistent and expected.is_dir() and current_path.is_symlink() and current_path.resolve(strict=True) == expected
        elif current_path.exists() or current_path.is_symlink():
            consistent = False
    except ReportBudgetExceeded:
        raise
    except (DeployError, OSError, RuntimeError):
        consistent = False
    for item in topology.values():
        observed = item["state"]
        if not observed or observed["active"] != "active":
            continue
        if not expected_executable or not integer(observed.get("main_pid")):
            code = "running_release_mismatch"
        elif observed.get("exe") is None:
            code = "running_release_unverified"
        elif observed["exe"] != expected_executable:
            code = "running_release_mismatch"
        else:
            continue
        consistent = False
        problem(problems, code, instance_id)
    if not consistent:
        problem(problems, "instance_state_inconsistent", instance_id)
    namespace = entry["allocations"].get("namespace", {})
    paths = instance_paths(name)
    registered_backup, recorded_backup = entry.get("backup"), state.get("backup")
    backup = backup_freshness({"configuration_revision": entry.get("configuration_revision"), "backup": {
        **(registered_backup if isinstance(registered_backup, dict) else {}),
        **(recorded_backup if isinstance(recorded_backup, dict) else {}),
    }}, problems, instance_id)
    result = {
        "instance_id": instance_id, "name": name, "environment": string(entry.get("environment")),
        "runtime_environment": string(profile.get("runtime_environment")), "domain": string(profile.get("domain")),
        "architecture": string(profile.get("architecture")), "port": integer(entry["allocations"]["http_port"]),
        "database": string(entry["allocations"].get("database")), "redis_user": string(entry["allocations"].get("redis_user")),
        "namespace": {key: string(namespace.get(key)) for key in ("application", "environment")},
        "unit": expected_unit, "paths": {key: paths[key] for key in ("application", "data")},
        "provisioning": {"state": string(provisioning.get("state")), "steps": [item for item in provisioning.get("completed_steps", []) if string(item, 64)] if isinstance(provisioning.get("completed_steps"), list) else []},
        "configuration_revision": integer(entry.get("configuration_revision")),
        "status": "unknown" if recorded_status in {"healthy", "rolled_back"} and not consistent else recorded_status,
        "recorded_status": recorded_status, "metadata_consistent": consistent,
        "current_release": current, "previous_release": previous, "last_attempt": attempt(state.get("last_attempt")),
        "service": unit_status, "services": topology, "readiness": ready, "maintenance": live_maintenance, "certificate": cert, "backup": backup,
        "created_at": string(entry.get("created_at")), "updated_at": string(entry.get("updated_at")),
    }
    if profile.get("schema") == 3:
        from topology import parse_profile, parse_topology
        result["network"] = checked(problems, "declared_network_unavailable", instance_id,
                                    lambda: declared_network(host, parse_profile(profile, parse_topology(entry["topology"], registered=True)),
                                                             entry=entry, host_claims=host_claims))
        if result["network"]:
            for finding in result["network"]["findings"]:
                problem(problems, finding["code"], instance_id)
            if result["network"]["unexpected_exposure"]:
                problem(problems, "unexpected_public_listener")
    return result


def claim_inventory(registry):
    """Validate the whole host's declarations before using any as exposure claims."""
    import listeners
    from contracts import require
    from topology import parse_profile, parse_topology
    require(isinstance(registry, dict) and isinstance(registry.get("instances"), dict),
            "invalid_registry", "Host listener inventory requires a complete registry.")
    claims = []
    for instance_id, entry in registry["instances"].items():
        if entry["profile"].get("schema") != 3:
            continue
        profile = parse_profile(entry["profile"], parse_topology(entry["topology"], registered=True))
        require(profile.instance_id == instance_id, "invalid_registry", "Listener claims belong to another instance.")
        selected = listeners.public_claims(profile)
        listeners.require_available(selected, claims, ())
        claims.extend(selected)
    return tuple(claims)


def declared_network(host, profile, *, entry=None, resolution=None, closure=None, dns_observations=(), host_claims=None):
    """Lane A may supply its selected resolution/closure; never read private envs.

    Without that selection a declared port is not silently accepted as owned.
    Historical closure records are labelled recorded, not a fresh start permit.
    """
    import certificates
    import edge
    import firewall
    import listeners
    claims = listeners.public_claims(profile)
    observed = listeners.observe_sockets(host)
    findings, owners = [], {}
    if host_claims is None:
        try:
            host_claims = claim_inventory(host.read_registry())
        except (DeployError, OSError, ValueError, KeyError, TypeError):
            host_claims = ()
            findings.append({"code": "host_listener_inventory_unverified"})
    inventory = tuple(dict.fromkeys((*host_claims, *claims)))
    unexpected = []
    try:
        owners["nginx"] = listeners.process_members(host, "nginx.service")
    except (DeployError, OSError, ValueError):
        findings.append({"code": "listener_owner_unverified"})
    edge_verified = False
    if resolution is not None and entry is not None:
        try:
            expected = edge.stream_projection(profile, resolution, entry["certificate"]["lineage"])
            directory = host.paths.p(edge.RUNTIME + "/" + profile.instance_id)
            selected = {name: content for name, content in expected.items() if (directory / name).exists()}
            if (directory / "http-open.conf").exists():
                selected["http-open.conf"] = '"" 1;\n'
            edge.observe_projection(host, profile.instance_id, resolution.roles, expected=selected)
            edge_verified = True
        except (DeployError, OSError, ValueError):
            findings.append({"code": "nginx_projection_unverified"})
    else:
        findings.append({"code": "nginx_projection_unverified"})
    for finding in listeners.ownership_findings(inventory, observed, owners, edge_verified=edge_verified):
        # Unclaimed exposure belongs to the host scanner. Other instances' socket
        # ownership is assessed with their own selected projection, not this one.
        if finding.get("instance_id") == profile.instance_id:
            findings.append(finding)
        elif finding["code"] == "unexpected_public_listener" and not (finding["transport"] == "tcp" and finding["port"] in PUBLIC_PORTS):
            unexpected.append(finding)
    policy = "unverified"
    if claims:
        try:
            firewall.observe(host, profile.instance_id, claims)
            policy = "closed"
        except (DeployError, OSError, ValueError):
            try:
                firewall.observe(host, profile.instance_id, claims, bindings=profile.listeners, opened=True)
                policy = "runtime_exposure"
            except (DeployError, OSError, ValueError):
                findings.append({"code": "firewall_policy_unverified"})
    answers = {o.hostname: o for o in dns_observations}
    for _, binding in profile.listeners:
        if binding.hostname not in answers:
            try:
                answers[binding.hostname] = listeners.observe_dns(binding)
            except ReportBudgetExceeded:
                raise
            except (DeployError, OSError, ValueError):
                findings.append({"code": "external_dns_observation_unavailable", "hostname": binding.hostname})
    dns = [listeners.external_dns(binding, answers.get(binding.hostname)) for _, binding in profile.listeners]
    for value in dns:
        findings.append({"code": "external_dns_proxy_mode_unverified", "hostname": value["hostname"]})
        if value["addresses_match"] is False:
            findings.append({"code": "external_dns_drift", "hostname": value["hostname"]})
    for name, binding in profile.listeners:
        if any(ipaddress.ip_network(cidr).prefixlen == 0 for cidr in binding.source_cidrs):
            findings.append({"code": "listener_broad_sources", "listener": name})
        findings.append({"code": "listener_origin_disclosed", "listener": name})
    tls = None
    if entry is not None:
        tls = certificates.inspect(host, entry)
        if tls["state"] != "ready":
            findings.append({"code": "listener_tls_unverified"})
            if tls.get("effective_renewal", {}).get("state") != "verified" and "effective_renewal" in tls:
                findings.append({"code": "certificate_effective_renewal_unverified"})
        elif resolution is not None:
            try:
                tls = {**tls, "effective": certificates.verify_network_consumers(host, entry)}
            except (DeployError, OSError, ValueError):
                findings.append({"code": "certificate_effective_renewal_unverified"})
    admission = {"recorded_stage": None, "scope": [], "observed_at": None, "current_proof": False}
    if closure is not None:
        from contracts import record_document
        record_document(closure)
        if {u.instance_id for u in closure.units} != {profile.instance_id}:
            raise ValueError("closure scope mismatch")
        admission.update(recorded_stage=closure.stage.value, observed_at=closure.observed_at,
                         scope=[{"role": u.role, "ordinal": u.ordinal} for u in closure.units])
        if closure.stage.value == "ingress_withdrawn":
            findings.append({"code": "application_admission_unverified"})
    return {"listeners": [{"role": c.role, "listener": c.listener, "ordinal": c.ordinal,
                            "transport": c.socket.transport, "family": c.socket.family, "address": c.socket.address,
                            "port": c.socket.port, "expected_owner": c.owner} for c in claims],
            "firewall": policy, "dns": dns, "tls": tls, "admission": admission, "findings": findings,
            "unexpected_exposure": unexpected}


def collect_report(host):
    """Collect on the main Unix thread, retaining partial results at the deadline."""
    deadline = time.monotonic() + REPORT_BUDGET
    problems = []
    result = {"schema": REPORT_SCHEMA, "generated_at": now(), "complete": True,
              "host": {"host_id": None, "hostname": None, "layout_version": None,
                       "registry_updated_at": None, "failed_units": None, "network": None, "ssh": None,
                       "resources": dict.fromkeys(("cpu_count", "load_average", "memory_total_bytes", "memory_available_bytes", "disk_total_bytes", "disk_available_bytes", "uptime_seconds"))},
              "shared_services": dict.fromkeys(("postgresql", "redis", "nginx")), "projects": [], "problems": problems}
    try:
        with wall_clock_budget(REPORT_BUDGET):
            collect_inventory(host, result, deadline)
    except ReportBudgetExceeded:
        problem(problems, "report_budget_exhausted")
    if time.monotonic() >= deadline:
        # A dependency can translate the alarm exception into its own I/O error.
        problem(problems, "report_budget_exhausted")
    result["projects"].sort(key=lambda p: (p["project"] or "", p["project_id"]))
    for project in result["projects"]:
        project["instances"].sort(key=lambda i: (i["environment"] or "", i["instance_id"]))
    result["complete"] = not problems
    return result


def collect_inventory(host, result, deadline):
    problems = result["problems"]
    result["host"]["hostname"] = string(checked(problems, "hostname_unavailable", None, socket.gethostname))
    resources(host, problems, result["host"]["resources"])
    result["host"]["failed_units"] = failed_units(host, problems, deadline)
    result["host"]["ssh"] = ssh(host, problems, deadline)
    try:
        registry = host.read_registry(missing=False)
        if not isinstance(registry, dict) or registry.get("schema") != 2 or registry.get("layout_version") != 2 or not identifier(registry.get("host_id")) or not isinstance(registry.get("instances"), dict):
            raise ValueError("invalid registry")
    except ReportBudgetExceeded:
        raise
    except Exception:
        problem(problems, "registry_unavailable")
        result["host"]["network"] = network(host, problems, deadline)
        return
    claims = checked(problems, "host_listener_inventory_unverified", None, claim_inventory, registry) or ()
    result["host"]["network"] = network(host, problems, deadline, claims=claims)
    result["host"].update({"host_id": registry["host_id"], "layout_version": registry["layout_version"], "registry_updated_at": string(registry.get("updated_at"))})
    for name in ("postgresql", "redis", "nginx"):
        unit = registry["shared"].get(name, {}).get("unit")
        if not unit:
            continue
        result["shared_services"][name] = checked(problems, "shared_service_check_unavailable", None, service, host, unit, deadline)
    projects = {}
    entries = list(registry["instances"].values())
    if len(entries) > MAX_INSTANCES:
        problem(problems, "instance_limit_exceeded")
    for entry in entries[:MAX_INSTANCES]:
        if not isinstance(entry, dict) or not identifier(entry.get("project_id")) or entry.get("host_id") != registry["host_id"]:
            problem(problems, "instance_identity_invalid")
            continue
        item = instance_report(host, entry, deadline, problems, host_claims=claims)
        if item is None:
            continue
        project = projects.get(entry["project_id"])
        if project is None:
            project = {"project_id": entry["project_id"], "project": string(entry["allocations"]["namespace"]["application"]), "instances": []}
            projects[entry["project_id"]] = project
            result["projects"].append(project)
        project["instances"].append(item)
