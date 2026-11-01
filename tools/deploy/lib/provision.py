"""Bootstrap orchestration (F1); service owners are replaceable lane interfaces.

No default PostgreSQL/Redis service is adopted or restarted. F2/F3 stubs refuse
before mutation until the dedicated-service and DNS/backup owners are complete.
"""
from __future__ import annotations

import copy
import hashlib
import json
import os
from pathlib import Path
import re
import secrets
import uuid

from contracts import (DeployError, PREFIX, REDIS_UNIT, REGISTRY_SCHEMA, LAYOUT_VERSION, PORT_FIRST, PORT_LAST,
    atomic_json, check_path, dump_env, identity, instance_paths, marker, now, read_json, require,
    validate_profile, validate_project)
from topology import instance_units, validate_topology
from runtime_config import (CREDENTIAL_KEYS, read_credentials as _credentials, runtime_environment,
    validate_runtime_environment, initialize_bootstrap_secrets, _instance_group)
from edge import edge_findings, edge_template
import packages
import postgres
import redis_acl
import certificates
import backup
import upgrades

_marker = marker


def _journal(host, registry, entry=None):
    host.journal(registry, entry)


def _mkdir(host, path, mode=0o755, owner="root", group="root"):
    check_path(path, "directory")
    path.mkdir(parents=True, exist_ok=True)
    host.run(["chmod", f"{mode:05o}", str(path)])
    host.run(["chown", owner + ":" + group, str(path)])


def _owned_write(host, path, content, mode=0o644, owner="root", group="root"):
    return host.owned_write(path, content, mode, owner, group)


def _step(host, registry, entry, name):
    if name not in entry["provisioning"]["completed_steps"]:
        entry["provisioning"]["completed_steps"].append(name)
        _journal(host, registry, entry)


def _probe(host, args, **kwargs):
    try:
        return host.run(args, check=False, **kwargs)
    except (OSError, DeployError):
        return None


def _ok(result):
    return result is not None and result.returncode == 0


def _text(result):
    if not _ok(result):
        return ""
    out = result.stdout or ""
    return (out.decode() if isinstance(out, bytes) else out).strip()


def _read(path):
    check_path(path)
    return path.read_text(encoding="utf-8")


def _installed(host, package):
    return _text(_probe(host, ["dpkg-query", "-W", "-f=${Status}", package])) == "install ok installed"


def _listeners(host):
    result = _probe(host, ["ss", "-Hltn"])
    if not _ok(result):
        raise DeployError("provision_conflict", "listener inspection failed; refusing allocations")
    ports = set()
    for line in _text(result).splitlines():
        columns = line.split()
        if len(columns) < 4:
            raise DeployError("provision_conflict", "unrecognized listener output")
        match = re.search(r":([0-9]+)$", columns[3])
        if not match:
            raise DeployError("provision_conflict", "unrecognized listener address")
        ports.add(int(match.group(1)))
    return ports


def _account(host, entry):
    result = _probe(host, ["getent", "passwd", entry["name"]])
    if result is None or result.returncode not in (0, 2):
        raise DeployError("provision_conflict", "account inspection failed")
    fields = _text(result).split(":") if result.returncode == 0 else None
    if fields and (len(fields) != 7 or fields[0] != entry["name"]
                   or not all(value.isdigit() and int(value) > 0 for value in fields[2:4]) or fields[4] != _marker(entry).replace(":", "=") or fields[5] != instance_paths(entry["name"])["data"] or fields[6] != "/usr/sbin/nologin"):
        raise DeployError("provision_conflict", "system account ownership mismatch")
    if fields:
        _instance_group(host, entry, fields)
    return fields


def _site_checks(host, entry):
    name, domain = entry["name"], entry["profile"]["domain"].lower()
    site = host.paths.p(f"/etc/nginx/sites-available/{PREFIX}-{name}.conf")
    enabled = host.paths.p(f"/etc/nginx/sites-enabled/{PREFIX}-{name}.conf")
    check_path(site)
    if site.exists() and not _read(site).startswith("# " + _marker(entry) + "\n"):
        raise DeployError("provision_conflict", "nginx site has foreign ownership")
    if enabled.is_symlink():
        expected = f"../sites-available/{PREFIX}-{name}.conf"
        if os.readlink(enabled) != expected:
            raise DeployError("provision_conflict", "nginx enabled-site link has foreign ownership")
    elif enabled.exists():
        raise DeployError("provision_conflict", "nginx enabled-site path is not the owned link")
    # nginx -T expands includes, so custom conf.d paths and wildcard server
    # names are examined as well as the distribution sites directories.
    if _installed(host, "nginx") or _installed(host, "nginx-core"):
        result = host.run(["nginx", "-T"], check=False)
        if result.returncode:
            raise DeployError("provision_conflict", "nginx configuration inspection failed")
        files = re.split(r"(?m)^# configuration file (.+):$", result.stdout or "")
        for index in range(1, len(files), 2):
            current, content = files[index], files[index + 1]
            if current in {str(site), str(enabled)}:
                continue
            content = re.sub(r"(?m)#.*$", "", content)
            for declaration in re.finditer(r"\bserver_name\s+([^;]+);", content, re.S):
                for raw in declaration.group(1).split():
                    item = raw.strip("\"'").lower()
                    conflict = (item == domain or item.startswith("~") or "$" in item
                                or item.startswith("*.") and domain.endswith(item[1:])
                                or item.endswith(".*") and domain.startswith(item[:-1])
                                or item.startswith(".") and (domain == item[1:] or domain.endswith(item)))
                    if conflict:
                        raise DeployError("provision_conflict", "domain conflicts with an existing or uninspectable nginx server_name")
    _unit_checks(host, entry)


def _unit_checks(host, entry):
    for name in instance_units(entry).values():
        unit = host.paths.p(f"/etc/systemd/system/{name}")
        check_path(unit)
        if unit.exists() and not _read(unit).startswith("# " + _marker(entry) + "\n"):
            raise DeployError("provision_conflict", "service unit has foreign ownership")
        if unit.exists():
            info = unit.stat()
            require(not info.st_mode & 0o022 and (info.st_uid == 0 or host.paths.root != Path("/")),
                    "provision_conflict", "service unit ownership or permissions are unsafe")
        template = name.split("@", 1)[0] + "@.service"
        for directory in ("/etc/systemd/system", "/run/systemd/system", "/usr/lib/systemd/system", "/lib/systemd/system"):
            base = host.paths.p(directory)
            for candidate in (base / template, base / (template + ".d"), base / (name + ".d")):
                if candidate.exists() or candidate.is_symlink():
                    raise DeployError("provision_conflict", "service template or override has unregistered ownership")
            alternate = base / name
            if alternate != unit and (alternate.exists() or alternate.is_symlink()):
                raise DeployError("provision_conflict", "service unit has an alternate unregistered definition")


def _prepare_instance(host, registry, entry):
    name = entry["name"]
    if not _account(host, entry):
        host.run(["useradd", "--system", "--user-group", "--home-dir", instance_paths(name)["data"], "--no-create-home",
                  "--shell", "/usr/sbin/nologin", "--comment", _marker(entry).replace(":", "="), name])
    for path in (host.paths.opt(name), host.paths.opt(name) / "releases", host.paths.instance_state(name)):
        _mkdir(host, path)
    _mkdir(host, host.paths.data(name), 0o750, name, name)
    # Children of application-owned data are mutable by the running service.
    # Never perform privileged chmod/chown through that tree: even a path
    # checked beforehand could be exchanged for a symlink in between calls.
    host.run(["runuser", "-u", name, "--", "install", "-d", "-m", "0700", "--",
              *[str(host.paths.data(name) / child) for child in
                ("logs", "uploads")]])
    account = _account(host, entry)
    if account:
        entry["allocations"].update(uid=int(account[2]), gid=int(account[3]))
    _mkdir(host, host.paths.p(instance_paths(name)["private"]), 0o700)
    _mkdir(host, host.paths.instance_config(name), 0o750, "root", name)
    path = host.paths.instance_config(name) / "secrets.json"
    if not path.exists():
        if "secrets" in entry["provisioning"]["completed_steps"]:
            raise DeployError("credentials_missing", "Instance credentials disappeared; restore them before retrying.")
        value = {"instance_id": entry["profile"]["instance_id"]}
        value.update({key: secrets.token_urlsafe(48) for key in CREDENTIAL_KEYS})
        atomic_json(path, value, mode=0o600)
    credentials = _credentials(host, entry)
    _step(host, registry, entry, "secrets")
    return credentials


def _provision_environment(host, registry, entry, credentials):
    path = host.paths.instance_config(entry["name"]) / "app.env"
    if path.exists():
        check_path(path)
        if path.stat().st_mode & 0o007:
            raise DeployError("configuration_mode", "Instance environment must not be world-readable.")
    elif "configuration" in entry["provisioning"]["completed_steps"]:
        raise DeployError("configuration_missing", "Server-owned runtime configuration is missing; restore it explicitly.")
    else:
        _owned_write(host, path, dump_env(runtime_environment(entry, credentials)), 0o640, "root", entry["name"])
    _step(host, registry, entry, "configuration")


def _template(tools_dir, name, entry, *, postgres_unit=""):
    """Render one owned template; caller supplies the registered Foundry cluster unit."""
    content = (Path(tools_dir) / "server" / "templates" / name).read_text()
    replacements = {"@MARKER@": _marker(entry), "@SERVICE@": entry["name"], "@DOMAIN@": entry["profile"]["domain"], "@PORT@": str(entry["allocations"]["http_port"])}
    replacements.update(edge_template(entry, Path(tools_dir) / "lib"))
    replacements["@POSTGRES_UNIT@"] = postgres_unit
    replacements["@REDIS_UNIT@"] = REDIS_UNIT
    replacements["@STATE_DIRECTORY@"] = f"{PREFIX}/apps/{entry['name']}"
    replacements["@RUNTIME_DIRECTORY@"] = f"{PREFIX}/{entry['profile']['instance_id']}"
    replacements["@HTTP_UNIT@"] = instance_units(entry)["http"]
    replacements.update({"@" + key.upper() + "@": value for key, value in instance_paths(entry["name"]).items()})
    if "@PROXY@" in content:
        content = content.replace("@PROXY@", (Path(tools_dir) / "server/templates/nginx-proxy.conf").read_text().rstrip())
    if "@UPGRADE_MAP@" in content:
        content = content.replace("@UPGRADE_MAP@", (Path(tools_dir) / "server/templates/nginx-upgrade.conf").read_text().rstrip())
    replacements["@VARIABLE@"] = PREFIX.replace("-", "_") + "_" + entry["allocations"]["database"] + "_upgrade"
    for key, value in replacements.items():
        content = content.replace(key, value)
    return content


def _install_unit(host, registry, entry, tools_dir):
    postgres_unit = registry["shared"]["postgresql"]["unit"]
    require(bool(re.fullmatch(r"postgresql@[0-9]+-foundry\.service", postgres_unit)), "postgres_identity", "Only the Foundry PostgreSQL cluster may be a dependency.")
    _unit_checks(host, entry)
    changed = False
    units = instance_units(entry)
    for role, name in units.items():
        unit = host.paths.p(f"/etc/systemd/system/{name}")
        content = _template(tools_dir, f"foundry-{role}@.service", entry, postgres_unit=postgres_unit)
        changed = _owned_write(host, unit, content) or changed
    if changed:
        host.run(["systemctl", "daemon-reload"])
    # Enable without starting: the first release is activated by make deploy.
    host.run(["systemctl", "enable", *units.values()])


def inspect_host(host):
    """Read public host identity and platform without touching foreign services."""
    registry = host.read_registry(missing=True)
    os_release = _read(host.paths.p("/etc/os-release"))
    values = dict(line.split("=", 1) for line in os_release.splitlines() if "=" in line)
    architecture = _text(_probe(host, ["dpkg", "--print-architecture"]))
    return {"schema": 2, "host_id": registry["host_id"] if registry else None,
            "os": values.get("ID", "").strip('"'), "os_version": values.get("VERSION_ID", "").strip('"'),
            "architecture": architecture, "listeners": sorted(_listeners(host))}


def new_registry(host_id, tools_dir):
    """Construct the schema-2 registry before acquiring any runtime resources."""
    metadata = upgrades.protocol(tools_dir)
    checksum = upgrades.tool_bundle(tools_dir)[2]
    from contracts import HOST_PATHS
    return {"schema": REGISTRY_SCHEMA, "layout_version": LAYOUT_VERSION, "host_id": host_id,
            "revision": 1, "created_at": now(), "updated_at": now(), "shared": {}, "projects": {}, "instances": {},
            "tools": {"framework_version": metadata["framework_version"], "generation": metadata["generation"],
                      "bundle_sha256": checksum, "protocol_min": "2.0", "protocol_max": "2.0",
                      "active_path": HOST_PATHS["tools"] + "/tools/" + checksum, "upgrade_operation": None}}


def new_entry(project, profile, topology, port, metadata):
    """Create one canonical allocation record; UID/GID await account creation."""
    entry = identity(project, profile)
    entry["allocations"].update(uid=None, gid=None, http_port=port)
    entry.update(host_id=profile["host"]["host_id"], profile=copy.deepcopy(profile), topology=copy.deepcopy(topology),
        framework_version=metadata["framework_version"], client_generation=metadata["generation"],
        configuration_revision=1, provisioning={"state": "preparing", "completed_steps": []},
        redis_grants={"keys": [], "channels": [], "inventory_sha256": None, "release": None,
                      "configuration_revision": 1, "pending_operation": None},
        certificate={"lineage": profile["domain"], "zone_ref": None, "last_renewal_check_at": None},
        backup={"policy": profile.get("backup_policy", {}), "destination_ref": None, "last_complete_id": None},
        created_at=now(), updated_at=now())
    return entry


def bootstrap_plan(host, project, profile, tools_dir=None, *, topology):
    """Bind platform, service observations, identity and topology to one digest.

    F2/F3 inspect functions must be read-only and return public findings; missing
    implementations raise NotImplementedError before any registry/resource write.
    """
    tools_dir = Path(tools_dir) if tools_dir else Path(__file__).resolve().parents[1]
    validate_project(project)
    validate_profile(profile, project)
    topology = validate_topology(topology, registered="sha256" in topology)
    snapshot = inspect_host(host)
    require(snapshot["os"] == "ubuntu" and snapshot["os_version"] in {"24.04", "26.04"}, "unsupported_host", "Ubuntu 24.04 or 26.04 is required.")
    require(snapshot["architecture"] == profile["architecture"], "architecture_mismatch", "Profile architecture differs from host.")
    registry = host.read_registry(missing=True)
    host_id = registry["host_id"] if registry else profile["host"]["host_id"] or str(uuid.uuid4())
    require(profile["host"]["host_id"] in (None, host_id), "host_mismatch", "Host UUID differs from the bound profile.")
    bound = copy.deepcopy(profile)
    bound["host"]["host_id"] = host_id
    registry = registry or new_registry(host_id, tools_dir)
    existing = registry["instances"].get(profile["instance_id"])
    if existing:
        require(existing["project_id"] == project["id"] and existing["name"] == identity(project, profile)["name"], "identity_mismatch", "Instance UUID belongs to a different identity.")
        require(existing["topology"] == topology, "topology_change_required", "Topology changes require a release plan (F1).")
        entry = copy.deepcopy(existing)
        entry["profile"] = bound
    else:
        used = {e["allocations"]["http_port"] for e in registry["instances"].values()}
        used.update(snapshot["listeners"])
        port = next((p for p in range(PORT_FIRST, PORT_LAST + 1) if p not in used), None)
        require(port is not None, "ports_exhausted", "No free HTTP allocation remains.")
        entry = new_entry(project, bound, topology, port, upgrades.protocol(tools_dir))
    candidate = copy.deepcopy(registry)
    candidate["projects"].setdefault(project["id"], {"slug": project["slug"], "created_at": now()})
    require(candidate["projects"][project["id"]]["slug"] == project["slug"], "identity_mismatch", "Project UUID cannot be silently renamed.")
    candidate["instances"][profile["instance_id"]] = entry
    from contracts import validate_registry
    validate_registry(candidate)
    _account(host, entry)
    for path in entry["allocations"]["paths"].values():
        target = check_path(host.paths.p(path), "directory")
        require(existing is not None or not target.exists(), "provision_conflict", "Unregistered instance path already exists.")
    _site_checks(host, entry)
    observations = {"packages": packages.inspect(host), "postgresql": postgres.inspect(host, registry),
                    "redis": redis_acl.inspect(host, registry), "certificate": certificates.inspect(host, entry)}
    findings = edge_findings(profile, tools_dir / "lib")
    upgrade = upgrades.tools_upgrade(host, registry, tools_dir)
    if upgrade and upgrade["from_digest"] is not None:
        findings.append({"kind": "stop", "code": "host_upgrade_required", "detail": "Apply a separate reviewed tool upgrade first."})
    digest_input = {"host": snapshot, "profile": bound, "topology": topology, "project": project,
                    "revision": registry["revision"], "observations": observations, "bundle": upgrades.tool_bundle(tools_dir)[2]}
    token = hashlib.sha256(json.dumps(digest_input, sort_keys=True, separators=(",", ":")).encode()).hexdigest()
    return {"ok": not upgrade or upgrade["from_digest"] is None, "host": snapshot, "host_id": host_id, "entry": entry,
            "findings": findings, "observations": observations, "plan_token": token}


def _install_site(host, registry, entry, tools_dir):
    """Use F3 DNS-01 issuance before publishing this instance's owned HTTPS site."""
    _site_checks(host, entry)
    certificates.provision(host, registry, entry)
    name = entry["name"]
    site = host.paths.p(f"/etc/nginx/sites-available/{PREFIX}-{name}.conf")
    enabled = host.paths.p(f"/etc/nginx/sites-enabled/{PREFIX}-{name}.conf")
    _mkdir(host, site.parent)
    _mkdir(host, enabled.parent)
    previous = site.read_bytes() if site.exists() else None
    existed = enabled.is_symlink()
    changed = _owned_write(host, site, _template(tools_dir, "nginx-https.conf", entry))
    if not existed:
        enabled.symlink_to(f"../sites-available/{PREFIX}-{name}.conf")
    if not _ok(_probe(host, ["nginx", "-t", "-q"])):
        if previous is None:
            site.unlink()
        else:
            _owned_write(host, site, previous)
        if not existed:
            enabled.unlink()
        raise DeployError("nginx_invalid", "nginx rejected the site; prior file restored.")
    if changed or not existed or "site" not in entry["provisioning"]["completed_steps"]:
        host.run(["systemctl", "reload", "nginx"])


def bootstrap_apply(host, project, profile, tools_dir, plan_token=None, *, topology, private_secrets=None):
    """Replan under the host lock, then call the F2/F3 owners in dependency order."""
    require(profile["host"]["host_id"] is not None, "host_binding_required", "Persist the proposed host UUID before apply.")
    with host.lock("host"):
        plan = bootstrap_plan(host, project, profile, tools_dir, topology=topology)
        require(plan["ok"], "bootstrap_blocked", "Bootstrap plan contains blocking findings.")
        require(isinstance(plan_token, str) and secrets.compare_digest(plan_token, plan["plan_token"]), "plan_changed", "Review a fresh bootstrap plan.")
        with host.lock(profile["instance_id"]):
            registry = host.read_registry(missing=True) or new_registry(profile["host"]["host_id"], tools_dir)
            entry = plan["entry"]
            upgrades.install_tools(host, registry, tools_dir)
            registry["projects"].setdefault(project["id"], {"slug": project["slug"], "created_at": now()})
            _journal(host, registry, entry)
            try:
                packages.install_missing(host, plan["observations"]["packages"].get("missing", []))
                postgres.prepare(host, registry)
                redis_acl.prepare(host, registry)
                _prepare_instance(host, registry, entry)
                credentials = initialize_bootstrap_secrets(host, entry, private_secrets if private_secrets is not None else getattr(host, "secrets", {}))
                _step(host, registry, entry, "bootstrap_secrets")
                postgres.provision(host, registry, entry, credentials)
                redis_acl.provision(host, registry, entry, credentials)
                _provision_environment(host, registry, entry, credentials)
                backup.configure(host, registry, entry)
                _step(host, registry, entry, "backup")
                _install_unit(host, registry, entry, tools_dir)
                _step(host, registry, entry, "unit")
                _install_site(host, registry, entry, tools_dir)
                _step(host, registry, entry, "site")
                entry["provisioning"]["state"] = "ready"
                _journal(host, registry, entry)
                return {"ok": True, "host_id": registry["host_id"], "instance": entry["name"], "provisioning": "ready"}
            except (DeployError, NotImplementedError, OSError, ValueError):
                entry["provisioning"]["state"] = "failed"
                _journal(host, registry, entry)
                raise
