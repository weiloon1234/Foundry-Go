"""Pure-new-install package transactions with durable autostart suppression.

Callers hold the Foundry host lock. The private journal owns only packages that
were absent at approval; recovery never runs a global dpkg/apt repair.
"""
from __future__ import annotations

from collections.abc import Sequence
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import signal
import stat
import subprocess
import time
import uuid

from contracts import DeployError, HOST_PATHS, check_path, contract_path, digest, parse_json, read_json, require
from host import Host, LOCK_WAIT_SECONDS
from topology import DeploymentProfile, ExecutableBinding, ExecutableRole, PackageExecutable, Topology

POLICY = "/usr/sbin/policy-rc.d"
LOCKS = ("/var/lib/dpkg/lock-frontend", "/var/lib/dpkg/lock",
         "/var/lib/apt/lists/lock", "/var/cache/apt/archives/lock")
PACKAGE = re.compile(r"[a-z0-9][a-z0-9+.-]*(?::[a-z0-9]+)?")
VERSION = re.compile(r"[A-Za-z0-9.+:~_-]+")
ENVIRONMENT = {"DEBIAN_FRONTEND": "noninteractive", "NEEDRESTART_SUSPEND": "1"}
# All launch requirements appear in the reviewed bootstrap observation. Services
# with usable installed binaries are not pulled into an apt transaction.
REQUIREMENTS = {
    "ca-certificates": (), "openssl": ("/usr/bin/openssl",),
    "nginx": ("/usr/sbin/nginx",), "certbot": ("/usr/bin/certbot",),
    "python3-certbot-dns-cloudflare": (), "pgbackrest": ("/usr/bin/pgbackrest",),
    "rclone": ("/usr/bin/rclone",), "age": ("/usr/bin/age",),
    "redis-server": ("/usr/bin/redis-server",), "redis-tools": ("/usr/bin/redis-cli",),
}

# Exact distro conffiles whose activation is owned by framework renewal instead.
# Unknown cron payloads, including other paths in these packages, remain refused.
BUILTIN_CRON = {"certbot": ("/etc/cron.d/certbot",)}


def binary_available(host: Host, package: str) -> bool:
    """Use the same accepted binary paths for planning and service inspection."""
    binaries = REQUIREMENTS.get(package, ())
    return bool(binaries) and all(host.paths.p(path).is_file() for path in binaries)


def _inventory(host: Host) -> dict:
    result = host.run(["dpkg-query", "-W", "-f=${binary:Package}\t${Status}\t${Version}\n"])
    rows = {}
    for line in result.stdout.splitlines():
        parts = line.split("\t")
        require(len(parts) == 3 and PACKAGE.fullmatch(parts[0]), "package_inventory", "Cannot inspect dpkg package status.")
        name, status, version = parts
        rows[name] = {"status": status, "version": version}
    return rows


def _installed(inventory: dict, name: str) -> bool:
    return _record(inventory, name).get("status") == "install ok installed"


def _record(inventory: dict, name: str) -> dict:
    return inventory.get(_package_key(inventory, name), {})


def _package_key(inventory: dict, name: str) -> str:
    if name in inventory:
        return name
    matches = [key for key in inventory if key.split(":")[0] == name]
    require(len(matches) <= 1, "package_inventory", "Ambiguous multi-architecture package: " + name)
    return matches[0] if matches else name


LOCK_POLL_SECONDS = 5


def _locks(host: Host) -> list[str]:
    busy = []
    for name in LOCKS:
        path = check_path(host.paths.p(name), "file")
        if not path.exists():
            continue
        descriptor = os.open(path, os.O_RDWR | os.O_NOFOLLOW)
        try:
            try:
                fcntl.lockf(descriptor, fcntl.LOCK_EX | fcntl.LOCK_NB)
            except BlockingIOError:
                busy.append(name)
            else:
                fcntl.lockf(descriptor, fcntl.LOCK_UN)
        finally:
            os.close(descriptor)
    return busy


def ubuntu_major(host: Host) -> int:
    path = host.paths.p("/etc/os-release")
    if path.is_symlink():
        require(os.readlink(path) in {"../usr/lib/os-release", "/usr/lib/os-release"},
                "unsupported_host", "Unexpected operating-system metadata link.")
        path = host.paths.p("/usr/lib/os-release")
    check_path(path, "file")
    fields = dict(line.split("=", 1) for line in path.read_text().splitlines() if "=" in line)
    require(fields.get("ID", "").strip('"') == "ubuntu", "unsupported_host", "Only Ubuntu 24.04 and 26.04 are supported.")
    version = fields.get("VERSION_ID", "").strip('"')
    require(version in ("24.04", "26.04"), "unsupported_host", "Only Ubuntu 24.04 and 26.04 are supported.")
    return {"24.04": 16, "26.04": 18}[version]


def _required(host: Host, inventory: dict) -> list[str]:
    required = dict(REQUIREMENTS)
    major = ubuntu_major(host)
    required.update({f"postgresql-{major}": (f"/usr/lib/postgresql/{major}/bin/postgres",),
                     f"postgresql-client-{major}": (f"/usr/lib/postgresql/{major}/bin/psql",),
                     "postgresql-common": ("/usr/bin/pg_createcluster",)})
    return sorted(name for name, binaries in required.items()
                  if not _installed(inventory, name) and not (binary_available(host, name) if name in REQUIREMENTS
                          else binaries and all(host.paths.p(p).is_file() for p in binaries)))


def _journal_path(host: Host) -> Path:
    directory = host.check_control_directory(host.paths.p(HOST_PATHS["private"]))
    return check_path(directory / "packages.json", "file")


def _load(host: Host) -> dict | None:
    path = _journal_path(host)
    if not path.exists():
        return None
    info = path.stat()
    require(not info.st_mode & 0o077 and (info.st_uid == 0 or host.paths.root != Path("/")),
            "package_journal", "Package recovery journal must be root-owned and private.")
    value = read_json(path)
    require(isinstance(value, dict) and value.get("schema") == 1
            and value.get("state") in {"installing", "complete"}
            and isinstance(value.get("transaction"), dict) and isinstance(value.get("before"), dict),
            "package_journal", "Invalid package recovery journal.")
    require(value.get("transaction_sha256") == _digest(value["transaction"]),
            "package_journal", "Package transaction identity changed; restore its journal.")
    try:
        operation = str(uuid.UUID(value.get("operation_id", "")))
    except (ValueError, TypeError, AttributeError):
        raise DeployError("package_journal", "Invalid package operation identity.") from None
    require(operation == value["operation_id"]
            and value.get("policy") == f"#!/bin/sh\n# Foundry package transaction {operation}\nexit 101\n"
            and isinstance(value.get("defaults"), dict)
            and all(PACKAGE.fullmatch(name) and isinstance(version, str) and VERSION.fullmatch(version)
                    for name, version in value["transaction"].items()),
            "package_journal", "Invalid package recovery intent.")
    if "evidence" in value:
        supply_fields = ["evidence", "activation_before", "activation_units"]
        if "cron_defaults" in value:
            supply_fields.append("cron_defaults")
        require(value.get("supply_sha256") == _digest({key: value[key] for key in supply_fields})
                and all(UNIT.fullmatch(unit) for unit in value["activation_units"])
                and set(value.get("masked_defaults", [])) <= value["activation_units"].keys(),
                "package_journal", "Package supply or activation intent changed.")
    require(isinstance(value.get("default_activation", {}), dict)
            and all(UNIT.fullmatch(unit) and isinstance(record, dict)
                    and record.get("state") in ("publishing", "masked", "releasing", "released")
                    and isinstance(record.get("identity"), dict)
                    and set(record["identity"]) == {"device", "inode", "uid", "ctime_ns"}
                    and all(type(n) is int and n >= 0 for n in record["identity"].values())
                    for unit, record in value.get("default_activation", {}).items()),
            "package_journal", "Invalid default activation ownership record.")
    return value


def _save(host: Host, value: dict) -> None:
    path = _journal_path(host)
    path.parent.mkdir(parents=True, mode=0o700, exist_ok=True)
    require(not path.parent.stat().st_mode & 0o077, "package_journal", "Package journal directory must be private.")
    host.owned_write(path, json.dumps(value, sort_keys=True, indent=2) + "\n", mode=0o600)


def _digest(value) -> str:
    return hashlib.sha256(json.dumps(value, sort_keys=True, separators=(",", ":")).encode()).hexdigest()


def _refuse(names, reason="Package transaction is not a pure new install", changes=None):
    detail = reason + ": " + ", ".join(sorted(set(names)))
    if changes:
        # Name the installed packages apt would change so an operator can update the server first.
        detail += "; it would change installed packages: " + ", ".join(f"{name} {changes[name]}" for name in sorted(changes))
    raise DeployError("package_plan_refused", detail)


def _parse_simulation(output: str, inventory: dict, *, approved: dict | None = None) -> dict:
    """Reject every installed-package action, including Conf without Inst."""
    installs, configurations, changes = {}, {}, {}
    rejected = set()
    approval ={_package_key(inventory, name): version for name, version in approved.items()} if approved is not None else None
    for line in output.splitlines():
        match = re.match(r"^(Inst|Conf|Remv|Purg)\s+(\S+)(.*)$", line)
        if not match:
            continue
        action, name, detail = match.groups()
        require(PACKAGE.fullmatch(name), "package_simulation", "Malformed apt package action.")
        version = re.match(r"\s+\(([^\s)]+)(?:\s|\))", detail)
        previous = _record(inventory, name)
        expected = approval.get(_package_key(inventory, name)) if approval is not None else None
        resumable = (expected is not None
                     and previous.get("status") != "install ok installed"
                     and previous.get("version") == expected)
        if (action in {"Remv", "Purg"} or version is None
                or not VERSION.fullmatch(version[1])
                or (previous and previous.get("status") != "unknown ok not-installed" and not resumable)):
            rejected.add(name)
            if previous and previous.get("status") == "install ok installed" and action != "Conf":
                # Upgrade lines read "Inst name [old] (new ...)"; report only, never approve from this.
                target = re.search(r"\(([^\s)]+)", detail)
                changes[name] = (previous.get("version") or "?") + " -> " + (
                    "removed" if action in {"Remv", "Purg"} else target[1] if target else "?")
            continue
        target = version[1]
        if approval is not None and expected != target:
            rejected.add(name)
        collection = installs if action == "Inst" else configurations
        if name in collection:
            rejected.add(name)
        collection[name] = target
    for name, version in configurations.items():
        if installs.get(name) != version and (approval is None or approval.get(_package_key(inventory, name)) != version):
            rejected.add(name)
    if re.search(r"(?m)^(?!0 upgraded,)\d+ upgraded,|\b[1-9]\d* (?:to remove|reinstalled)", output):
        rejected.update(installs or {"apt-summary"})
    if rejected:
        _refuse(rejected, changes=changes)
    require(bool(installs or configurations), "package_simulation", "apt produced no approved package actions.")
    require(set(installs) <= set(configurations), "package_simulation", "apt did not plan configuration of every new package.")
    return dict(sorted((installs | configurations).items()))


def _simulate(host: Host, targets: Sequence[str], inventory: dict, approved=None) -> dict:
    args = ["apt-get", "-s", "--no-install-recommends", "--no-remove", "--no-upgrade", "install", *targets]
    result = host.run(args, env=ENVIRONMENT, check=False, timeout=120)
    if result.returncode:
        _refuse([target.split("=")[0] for target in targets], "apt cannot resolve a bounded new-install transaction")
    transaction = _parse_simulation(result.stdout, inventory, approved=approved)
    require(all(_package_key(transaction, target.split("=")[0]) in transaction for target in targets),
            "package_simulation", "apt omitted a requested package from the transaction.")
    return transaction


def role_requirements(topology: Topology, profile: DeploymentProfile) -> dict[str, list[str]]:
    """Direct package requirements, with every placed requesting role recorded."""
    placement = dict(profile.placement)
    require(set(placement) == {role.common.name for role in topology.roles},
            "package_roles", "Package placement does not cover the selected topology.")
    result = {}
    for role in topology.roles:
        if placement[role.common.name]:
            for name in role.common.packages:
                result.setdefault(name, []).append(role.common.name)
    return {name: sorted(roles) for name, roles in sorted(result.items())}


def executable_evidence(host: Host, reference: PackageExecutable, inventory=None) -> dict:
    """Resolve by explicit path and dpkg ownership, never by PATH lookup."""
    inventory = _inventory(host) if inventory is None else inventory
    require(_installed(inventory, reference.package), "package_executable", "Executable package is not installed.")
    contract_path(reference.path, absolute=True)
    # System-owned usrmerge links are accepted only via realpath followed by a
    # package-database ownership check of the final executable, never a user link.
    path = host.paths.p(reference.path)
    resolved = path.resolve(strict=True)
    require(resolved.is_relative_to(host.paths.root), "package_executable", "Package executable escapes the host root.")
    for candidate in (path, resolved):
        for parent in [candidate, *candidate.parents]:
            info = parent.lstat()
            require((info.st_uid == 0 or host.paths.root != Path("/"))
                    and (stat.S_ISLNK(info.st_mode) or not info.st_mode & 0o022),
                    "package_executable", "Package executable ancestry is writable or foreign-owned.")
            if parent == host.paths.root:
                break
    check_path(resolved, "file")
    info = resolved.stat()
    require(stat.S_ISREG(info.st_mode) and info.st_mode & 0o111 and not info.st_mode & 0o6022,
            "package_executable", "Package executable has unsafe type or permissions.")
    name = "/" + resolved.relative_to(host.paths.root).as_posix()
    result = host.run(["dpkg-query", "-S", name], check=False)
    owners = []
    for line in result.stdout.splitlines():
        owner, separator, owned = line.partition(": ")
        if separator and owned == name:
            owners.extend(owner.split(", "))
    require(result.returncode == 0 and len(owners) == 1
            and _package_key(inventory, owners[0]) == _package_key(inventory, reference.package),
            "package_executable", "Selected executable is not owned by its declared installed package.")
    return {"package": reference.package, "version": _record(inventory, reference.package)["version"],
            "path": name, "sha256": digest(resolved)}


def inspect(host: Host, topology: Topology | None = None, profile: DeploymentProfile | None = None,
            *, previous_requirements: Sequence[str] = ()) -> dict:
    """Read-only package plan, with owned incomplete installation considered first."""
    journal = _load(host)
    inventory, locks = _inventory(host), _locks(host)
    missing = _required(host, inventory)
    require((topology is None) == (profile is None), "package_roles", "Topology and placement must be supplied together.")
    roles = role_requirements(topology, profile) if topology is not None else {}
    missing = sorted(set(missing) | {name for name in roles if not _installed(inventory, name)})
    pending = journal is not None and journal["state"] == "installing"
    active = _transaction_active(host, journal)
    if pending and not locks and not active:
        _check_resume(inventory, journal)
    transaction = journal["transaction"] if pending else {}
    policy = host.paths.p(POLICY)
    if not pending and missing and not locks and not policy.exists() and not policy.is_symlink():
        _clean_inventory(inventory)
        transaction = _simulate(host, missing, inventory)
    evidence = journal.get("evidence", {}) if pending else _candidate_evidence(host, transaction) if transaction else {}
    executables = []
    if topology is not None:
        for role in topology.roles:
            if not dict(profile.placement)[role.common.name]:
                continue
            refs = [binding.executable for _, binding in role.common.bindings if isinstance(binding, ExecutableBinding)]
            if isinstance(role, ExecutableRole):
                refs.append(role.executable)
            for reference in refs:
                if isinstance(reference, PackageExecutable) and _installed(inventory, reference.package):
                    executables.append({"role": role.common.name, **executable_evidence(host, reference, inventory)})
    return {"installed": inventory, "missing": missing, "locks": locks, "role_requirements": roles,
            "retained_requirements": sorted(set(previous_requirements) - set(roles)),
            "evidence": evidence, "executables": executables,
            "activation": journal.get("activation_before", {}) if pending else _activation_snapshot(host),
            "policy_present": policy.exists() or policy.is_symlink(),
            "transaction_active": active,
            "state": "busy" if locks or active else "resume" if pending else "blocked" if policy.exists() or policy.is_symlink() else "ready",
            "transaction": transaction, "ubuntu_major": ubuntu_major(host)}


# Read authenticated apt indexes using tools present on minimal supported hosts.
# Do not bootstrap a Python apt binding before package authentication is available.
# Index hashes are checked against InRelease verified with the distro keyring;
# an arbitrary locally trusted repository cannot impersonate an Ubuntu origin.
_APT_EVIDENCE = r'''
import hashlib,io,json,pathlib,subprocess,sys,tempfile
from urllib.parse import urljoin

def command(args):
    with tempfile.TemporaryFile() as output:
        subprocess.run(args, stdout=output, stderr=subprocess.DEVNULL, check=True, timeout=60)
        if output.tell() > 4*1024**2:
            raise ValueError("metadata output too large")
        output.seek(0)
        return output.read()

def paragraphs(stream):
    record, key, size = {}, None, 0
    while True:
        # Real Ubuntu indexes carry Provides lines far above 64 KiB; the record bound applies.
        line = stream.readline(2*1024**2 + 1)
        if len(line) > 2*1024**2:
            raise ValueError("metadata line too large")
        if not line or line in (b"\n", b"\r\n"):
            if record:
                yield record
            record, key, size = {}, None, 0
            if not line:
                return
            continue
        size += len(line)
        if size > 2*1024**2:
            raise ValueError("metadata record too large")
        text = line.decode("utf-8").rstrip("\r\n")
        if text.startswith((" ", "\t")) and key is not None:
            record[key] += "\n" + text.strip()
        else:
            key, separator, value = text.partition(":")
            if not separator or key in record:
                raise ValueError("invalid metadata")
            record[key] = value.strip()

requested = json.loads(sys.argv[1])
native_architecture = command(["dpkg", "--print-architecture"]).decode("ascii").strip()
records, releases = {}, {}
targets = list(paragraphs(io.BytesIO(command(["apt-get", "indextargets"]))))
if len(targets) > 256 or len(requested) > 2048:
    raise ValueError("too many package indexes or requirements")
for target in targets:
    if target.get("Identifier") != "Packages":
        continue
    filename = pathlib.Path(target["Filename"])
    if filename.parent != pathlib.Path("/var/lib/apt/lists") or filename.is_symlink():
        raise ValueError("unsafe package index")
    process = subprocess.Popen(["/usr/lib/apt/apt-helper", "cat-file", str(filename)],
                               stdout=subprocess.PIPE, stderr=subprocess.DEVNULL)
    found, checksum, size = [], hashlib.sha256(), 0
    class IndexReader:
        def readline(self, limit):
            global size
            line = process.stdout.readline(limit)
            checksum.update(line)
            size += len(line)
            if size > 512*1024**2:
                raise ValueError("package index too large")
            return line
    try:
        for record in paragraphs(IndexReader()):
            name = record.get("Package", "")
            architecture = record.get("Architecture", "")
            for requested_name in (name, name + ":" + architecture):
                if requested_name == name and architecture not in (native_architecture, "all"):
                    continue
                if requested_name in requested and requested[requested_name] == record.get("Version"):
                    found.append((requested_name, record))
                    if len(found) > 4096:
                        raise ValueError("too many package matches")
        if process.wait(timeout=30):
            raise ValueError("cannot read package index")
    finally:
        process.stdout.close()
        if process.poll() is None:
            process.kill()
            process.wait()
    if not found:
        continue
    prefix, separator, rest = filename.name.partition("_dists_")
    if not separator:
        raise ValueError("unsupported package index")
    suite = rest.split("_", 1)[0]
    signed = filename.parent / (prefix + "_dists_" + suite + "_InRelease")
    if signed.is_symlink():
        raise ValueError("unsafe signed release")
    if str(signed) not in releases:
        cleartext = command(["gpgv", "--keyring", "/usr/share/keyrings/ubuntu-archive-keyring.gpg",
                             "--output", "-", str(signed)])
        releases[str(signed)] = next(paragraphs(io.BytesIO(cleartext)))
    release = releases[str(signed)]
    hashes = {}
    for line in release.get("SHA256", "").splitlines():
        if line.strip():
            value, length, name = line.split()
            hashes[name] = (value, int(length))
    if hashes.get(target["MetaKey"]) != (checksum.hexdigest(), size):
        raise ValueError("package index differs from signed release")
    origin = {"origin": release.get("Origin"), "label": release.get("Label"),
              "archive": release.get("Suite"), "codename": release.get("Codename"),
              "site": target.get("Site", ""), "trusted": target.get("Trusted") == "yes"}
    base, separator, _ = target["URI"].partition("/dists/")
    if not separator:
        raise ValueError("unsupported package URI")
    for name, record in found:
        package_path = record["Filename"]
        if not package_path.startswith("pool/") or ".." in pathlib.PurePosixPath(package_path).parts:
            raise ValueError("unsafe package URI")
        candidate = {"version": record["Version"], "architecture": record["Architecture"],
                     "sha256": record["SHA256"], "size": int(record["Size"])}
        if name in records and any(records[name][key] != value for key, value in candidate.items()):
            raise ValueError("ambiguous package bytes")
        entry = records.setdefault(name, {**candidate, "uris": [], "origins": [], "indexes": []})
        for key, value in (("uris", urljoin(base + "/", package_path)), ("origins", origin), ("indexes", str(filename))):
            if value not in entry[key]:
                entry[key].append(value)
print(json.dumps(records, sort_keys=True))
'''


def _validate_evidence(evidence, transaction, codename):
    from urllib.parse import urlsplit
    require(isinstance(evidence, dict) and set(evidence) == set(transaction),
            "package_origin", "Package origin evidence does not cover the full dependency transaction.")
    for name, record in evidence.items():
        require(isinstance(record, dict) and record.get("version") == transaction[name]
                and re.fullmatch(r"[a-f0-9]{64}", record.get("sha256", ""))
                and type(record.get("size")) is int and 0 < record["size"] <= 2 * 1024**3
                and record.get("architecture") in ("amd64", "arm64", "all"),
                "package_origin", "Package version/hash/size evidence is invalid.")
        origins = record.get("origins")
        require(isinstance(origins, list) and 1 <= len(origins) <= 32,
                "package_origin", "Package has no authenticated archive origin.")
        for origin in origins:
            require(origin.get("trusted") is True and origin.get("origin") == "Ubuntu" and origin.get("label") == "Ubuntu"
                    and origin.get("codename") == codename
                    and origin.get("archive") in {codename, codename + "-updates", codename + "-security", codename + "-backports"},
                    "package_origin", "Every dependency must come from the supported signed Ubuntu archive.")
        uris = record.get("uris")
        require(isinstance(uris, list) and 1 <= len(uris) <= 32, "package_origin", "Package has no bounded archive location.")
        for uri in uris:
            parsed = urlsplit(uri)
            site = parsed.hostname or ""
            require(parsed.scheme in ("http", "https") and not parsed.username and not parsed.password
                    and not parsed.query and not parsed.fragment and parsed.port in (None, 80, 443)
                    and bool(site), "package_origin", "Invalid signed-archive mirror location.")


def _candidate_evidence(host, transaction):
    if not transaction:
        return {}
    ubuntu_major(host)
    os_release = host.paths.p("/etc/os-release")
    if os_release.is_symlink():
        os_release = host.paths.p("/usr/lib/os-release")
    fields = dict(line.split("=", 1) for line in os_release.read_text().splitlines() if "=" in line)
    codename = fields.get("VERSION_CODENAME", "").strip('"')
    require(bool(re.fullmatch(r"[a-z]+", codename)), "package_origin", "Cannot establish the supported distro archive suite.")
    result = host.run(["/usr/bin/python3", "-c", _APT_EVIDENCE, json.dumps(transaction)], timeout=120)
    evidence = parse_json(result.stdout)
    _validate_evidence(evidence, transaction, codename)
    return evidence


UNIT = re.compile(r"[A-Za-z0-9_:,.@\\-]+\.(?:service|socket|timer|path|target)")
TEMPLATE_UNIT = re.compile(r"@\.(?:service|socket|timer|path|target)$")


def _activation_snapshot(host):
    names, templates = set(), {}
    for args in (["list-unit-files", "--no-legend", "--no-pager"],
                 ["list-units", "--all", "--plain", "--no-legend", "--no-pager"]):
        result = host.run(["systemctl", *args])
        for line in result.stdout.splitlines():
            parts = line.split()
            if parts and UNIT.fullmatch(parts[0]):
                names.add(parts[0])
                # `systemctl show` rejects template names such as autovt@.service.
                if TEMPLATE_UNIT.search(parts[0]) and args[0] == "list-unit-files" and len(parts) > 1:
                    templates[parts[0]] = parts[1]
    require(len(names) <= 4096, "package_activation", "Host activation inventory is too large.")
    records = {name: {"template_state": state} for name, state in templates.items()}
    shown = sorted(names - templates.keys())
    if not shown:
        return dict(sorted(records.items()))
    result = host.run(["systemctl", "show", *shown, "--no-pager", "-p", "Id", "-p", "LoadState",
                       "-p", "ActiveState", "-p", "SubState", "-p", "MainPID", "-p", "UnitFileState", "-p", "FragmentPath"])
    for block in result.stdout.strip().split("\n\n"):
        record = dict(line.split("=", 1) for line in block.splitlines() if "=" in line)
        name = record.pop("Id", None)
        require(name in names and record.get("LoadState") in ("loaded", "masked", "not-found")
                and record.get("ActiveState") and "UnitFileState" in record,
                "package_activation", "Cannot classify existing host activation.")
        records[name] = record
    # Aliases may share an Id. Keep their file-state observation independently.
    for name in names - records.keys():
        result = host.run(["systemctl", "is-enabled", name], check=False)
        require(result.stdout.strip() in ("alias", "enabled", "enabled-runtime", "linked", "linked-runtime", "masked", "masked-runtime", "static", "disabled", "indirect", "generated"),
                "package_activation", "Cannot classify an activation alias.")
        records[name] = {"alias_state": result.stdout.strip()}
    return dict(sorted(records.items()))


_DEB_UNITS = r'''
import hashlib,json,pathlib,subprocess,sys,tarfile,tempfile
process = subprocess.Popen(["dpkg-deb", "--fsys-tarfile", sys.argv[1]], stdout=subprocess.PIPE, stderr=subprocess.DEVNULL)
allowed_cron = set(json.loads(sys.argv[2]))
units, cron, total, count = set(), {}, 0, 0
try:
    conffiles = set()
    if allowed_cron:
        with tempfile.TemporaryFile() as control:
            subprocess.run(["dpkg-deb", "--ctrl-tarfile", sys.argv[1]], stdout=control,
                           stderr=subprocess.DEVNULL, check=True, timeout=30)
            if control.tell() > 4*1024**2:
                raise ValueError("oversized package control archive")
            control.seek(0)
            with tarfile.open(fileobj=control, mode="r:") as metadata:
                for item in metadata:
                    if item.name.removeprefix("./") == "conffiles":
                        if not item.isfile() or item.size > 65536:
                            raise ValueError("invalid conffile inventory")
                        conffiles.update(metadata.extractfile(item).read().decode("utf-8").splitlines())
    with tarfile.open(fileobj=process.stdout, mode="r|") as archive:
        for item in archive:
            count += 1
            total += item.size
            if count > 100000 or total > 8*1024**3 or item.issparse():
                raise ValueError("oversized package")
            name = item.name.removeprefix("./").rstrip("/")
            path = pathlib.PurePosixPath(name)
            if path.is_absolute() or ".." in path.parts:
                raise ValueError("unsafe package member")
            prefixes = ("lib/systemd/system/", "usr/lib/systemd/system/", "etc/systemd/system/")
            if any(name.startswith(p) for p in prefixes) and not item.isdir():
                units.add(path.name)
            if name.startswith("etc/init.d/") and not item.isdir():
                units.add(path.name + ".service")
            if name.startswith("etc/cron.") and not item.isdir():
                if ("/" + name not in allowed_cron or "/" + name not in conffiles
                        or not item.isfile() or item.size > 65536 or "/" + name in cron):
                    raise ValueError("unsupported cron activation")
                cron["/" + name] = hashlib.sha256(archive.extractfile(item).read()).hexdigest()
            if name.startswith("usr/share/dbus-1/system-services/") and not item.isdir():
                raise ValueError("unsupported activation")
    process.stdout.close()
    if process.wait(timeout=30):
        raise ValueError("cannot read package")
    print(json.dumps({"units": sorted(units), "cron": cron}))
finally:
    if process.poll() is None:
        process.kill()
        process.wait()
'''


def _prepare_payloads(host, transaction, evidence):
    """Acquire inert distro payloads, verify reviewed hashes, inspect default units."""
    targets = [name + "=" + version for name, version in transaction.items()]
    host.run(["apt-get", "-y", "--download-only", "--no-install-recommends", "--no-remove", "--no-upgrade",
              "-o", "APT::Get::AllowUnauthenticated=false", "install", *targets], env=ENVIRONMENT, timeout=1200)
    cache = host.paths.p("/var/cache/apt/archives")
    wanted = {record["sha256"]: name for name, record in evidence.items()}
    require(len(wanted) == len(evidence), "package_payload", "Ambiguous package hashes.")
    found, units, cron = set(), {}, {}
    for path in sorted(cache.glob("*.deb")):
        check_path(path, "file")
        if path.stat().st_size not in {record["size"] for record in evidence.values()}:
            continue
        checksum = digest(path)
        if checksum not in wanted:
            continue
        name = wanted[checksum]
        found.add(name)
        allowed_cron = BUILTIN_CRON.get(name, ()) if name in REQUIREMENTS else ()
        result = host.run(["/usr/bin/python3", "-c", _DEB_UNITS, str(path), json.dumps(allowed_cron)], timeout=120)
        inspected = parse_json(result.stdout)
        require(isinstance(inspected, dict) and set(inspected) == {"units", "cron"},
                "package_activation", "Invalid package activation inventory.")
        names = inspected["units"]
        require(isinstance(names, list) and len(names) <= 4096 and all(isinstance(unit, str) and UNIT.fullmatch(unit) for unit in names),
                "package_activation", "Package has unclassifiable activation payloads.")
        for unit in names:
            units.setdefault(unit, []).append(name)
        require(isinstance(inspected["cron"], dict) and set(inspected["cron"]) <= set(allowed_cron),
                "package_activation", "Package has an unclassified cron payload.")
        for location, checksum in inspected["cron"].items():
            require(location not in cron and isinstance(checksum, str) and re.fullmatch(r"[a-f0-9]{64}", checksum),
                    "package_activation", "Invalid or overlapping cron payload.")
            cron[location] = {"package": name, "sha256": checksum}
    require(found == set(transaction), "package_payload", "Downloaded packages differ from the reviewed transaction hashes.")
    return {unit: sorted(owners) for unit, owners in sorted(units.items())}, cron


def _cron_stub(journal):
    return "# Foundry package cron suppressed: " + journal["operation_id"] + "\n"


def _check_cron_stub(host, path, journal, *, links=1):
    check_path(path, "file")
    require(path.exists(), "package_activation", "Package cron suppression disappeared.")
    info = path.stat()
    require(path.read_text() == _cron_stub(journal) and stat.S_IMODE(info.st_mode) == 0o644
            and info.st_nlink == links and (info.st_uid == 0 or host.paths.root != Path("/")),
            "package_activation", "Package cron suppression changed; refusing to replace it.")


def _suppress_cron(host, journal):
    # The prior absence is recorded before this runs. --force-confold preserves
    # this inert local conffile during unpack; the distro version is .dpkg-dist.
    for location in journal.get("cron_defaults", {}):
        path = check_path(host.paths.p(location), "file")
        host.check_control_directory(path.parent)
        path.parent.mkdir(parents=True, exist_ok=True, mode=0o755)
        stage = check_path(path.parent / (".foundry-cron-" + journal["operation_id"]), "file")
        if not path.exists():
            if not stage.exists():
                host.owned_write(stage, _cron_stub(journal), mode=0o644)
            _check_cron_stub(host, stage, journal)
            os.link(stage, path, follow_symlinks=False)
            _sync_directory(path.parent)
        if stage.exists():
            _check_cron_stub(host, stage, journal, links=2)
            _check_cron_stub(host, path, journal, links=2)
            require(stage.samefile(path), "package_activation", "Cron suppression staging is foreign.")
            stage.unlink()
            _sync_directory(path.parent)
        _check_cron_stub(host, path, journal)


def _finish_cron(host, journal):
    for location, record in journal.get("cron_defaults", {}).items():
        path = check_path(host.paths.p(location), "file")
        _check_cron_stub(host, path, journal)
        preserved = check_path(path.with_name(path.name + ".dpkg-dist"), "file")
        require(preserved.is_file() and digest(preserved) == record["sha256"],
                "package_activation", "Preserved distro cron payload differs from its verified package.")


def _mask_identity(path):
    info = path.lstat()
    return {"device": info.st_dev, "inode": info.st_ino, "uid": info.st_uid,
            "ctime_ns": info.st_ctime_ns}


def _mask_anchor(path, operation):
    return path.parent / (".foundry-mask-" + operation + "-" + _digest(path.name))


def _publish_default_mask(host, journal, unit, path):
    stage = _mask_anchor(path, journal["operation_id"])
    record = journal.setdefault("default_activation", {}).get(unit)
    if record is None:
        require(not path.exists() and not path.is_symlink(),
                "package_activation", "A foreign default-unit mask or file appeared.")
        if unit not in journal.setdefault("masked_defaults", []):
            journal["masked_defaults"].append(unit)
            _save(host, journal)
        if not stage.exists() and not stage.is_symlink():
            stage.symlink_to("/dev/null")
        require(stage.is_symlink() and os.readlink(stage) == "/dev/null"
                and stage.lstat().st_nlink == 1
                and (stage.lstat().st_uid == 0 or host.paths.root != Path("/")),
                "package_activation", "Default-unit mask staging is foreign.")
        _sync_directory(path.parent)
        record = {"operation_id": journal["operation_id"], "identity": _mask_identity(stage), "state": "publishing"}
        journal["default_activation"][unit] = record
        _save(host, journal)
    require(unit in journal.get("masked_defaults", []),
            "package_activation", "Default-unit mask has no journaled intent.")
    if record["state"] == "publishing":
        require(record["operation_id"] == journal["operation_id"],
                "package_activation", "Default-unit mask belongs to another transaction.")
        # The operation's staging link is a durable ownership anchor. Keep it
        # until release is durable so its inode cannot be recycled while mask
        # recovery relies on it. Linking changes ctime; masked records also check
        # the final ctime.
        require(stage.is_symlink(), "package_activation", "Default-unit mask ownership anchor disappeared.")
        links = int(stage.is_symlink()) + int(path.is_symlink())
        for candidate in (stage, path):
            if candidate.exists() or candidate.is_symlink():
                identity = _mask_identity(candidate)
                require(candidate.is_symlink() and os.readlink(candidate) == "/dev/null"
                        and all(identity[key] == record["identity"][key]
                                for key in ("device", "inode", "uid"))
                        and candidate.lstat().st_nlink == links,
                        "package_activation", "A foreign default-unit mask appeared.")
        if not path.is_symlink():
            require(stage.is_symlink(), "package_activation", "Default-unit mask staging disappeared.")
            os.link(stage, path, follow_symlinks=False)
        _sync_directory(path.parent)
        record.update(identity=_mask_identity(path), state="masked")
        _save(host, journal)
    require(record["state"] == "masked" and path.is_symlink() and os.readlink(path) == "/dev/null"
            and record["identity"] == _mask_identity(path),
            "package_activation", "A foreign default-unit mask appeared.")


def _mask_defaults(host, journal):
    before, units = journal["activation_before"], journal["activation_units"]
    directory = host.check_control_directory(host.paths.p("/etc/systemd/system"))
    directory.mkdir(parents=True, exist_ok=True)
    for unit in units:
        if unit in before:
            continue
        if "@." in unit:
            prefix, suffix = unit.split("@.", 1)
            require(not any(n.startswith(prefix + "@") and n.endswith("." + suffix) for n in before),
                    "package_activation", "A new template would mask preexisting foreign instances.")
        _publish_default_mask(host, journal, unit, directory / unit)
    host.run(["systemctl", "daemon-reload"])


def _finish_activation(host, journal):
    before, current = journal["activation_before"], _activation_snapshot(host)
    require(all(current.get(name) == record for name, record in before.items()),
            "package_service_changed", "Preexisting host activation changed during installation; operator review required.")
    for name, record in current.items():
        if name in before:
            continue
        template = name.split("@", 1)[0] + "@." + name.rsplit(".", 1)[1] if "@" in name else name
        require(name in journal["activation_units"] or template in journal["activation_units"],
                "package_activation", "Unowned activation appeared during package installation.")
        # A template unit file (name@.service) is never itself running; its instances are checked by name.
        require("template_state" in record
                or (record.get("ActiveState") == "inactive" and record.get("MainPID", "0") == "0"),
                "package_autostart", "New package activation started unexpectedly; installation remains unresolved.")
        host.run(["systemctl", "disable", name])
    # Keep the owned persistent masks. Disabling alone cannot fence socket or
    # dependency/manual activation after a reboot or interrupted installation.
    for unit in journal.get("masked_defaults", []):
        path = host.paths.p("/etc/systemd/system") / unit
        require(path.is_symlink() and os.readlink(path) == "/dev/null"
                and _mask_identity(path) == journal["default_activation"][unit]["identity"],
                "package_activation", "Package replaced an owned default mask.")


def authorize_default_activation(host: Host, unit: str, claim: dict) -> None:
    """Lane A calls under the host lock immediately before enable --now nginx.

    Pass registry['shared']['nginx'] after its owner/installed_by_foundry checks
    and nginx configuration validation. This only releases the exact journaled
    mask; it does not enable/start services or release cron/timer suppression.
    Installation must be complete with policy restored and no package process or
    lock remaining. Repeated calls and retries after removal/reload are safe;
    missing or replaced masks without a recorded handoff are refused.
    """
    require(UNIT.fullmatch(unit) and claim.get("owner") == "foundry"
            and claim.get("installed_by_foundry") is True and claim.get("unit") == unit,
            "package_activation_owner", "Default activation requires its framework service owner's claim.")
    journal = _load(host)
    require(journal is not None and journal["state"] == "complete" and journal.get("policy_state") == "restored"
            and not _locks(host) and not _transaction_active(host, journal),
            "package_activation_pending", "Package installation must finish before default activation handoff.")
    path = host.paths.p("/etc/systemd/system") / unit
    host.check_control_directory(path.parent)
    record = journal.get("default_activation", {}).get(unit)
    if record is None:
        require(not path.is_symlink() or os.readlink(path) != "/dev/null",
                "package_activation_owner", "Cannot remove a foreign default-service mask.")
        return
    anchor = _mask_anchor(path, record["operation_id"])
    if path.exists() or path.is_symlink():
        require(record["state"] in ("masked", "releasing") and path.is_symlink()
                and os.readlink(path) == "/dev/null" and _mask_identity(path) == record["identity"],
                "package_activation_owner", "Default-service mask no longer matches the journal-owned inode.")
        record["state"] = "releasing"
        _save(host, journal)
        path.unlink()
        _sync_directory(path.parent)
    else:
        require(record["state"] in ("releasing", "released"),
                "package_activation_owner", "Default-service mask disappeared without an owned handoff.")
    if record["state"] != "released" and (anchor.exists() or anchor.is_symlink()):
        # Removing the mask changes the retained anchor's ctime. Its inode is
        # still pinned until released is durable; record its full identity now
        # so cleanup retries cannot adopt a replacement after inode reuse.
        identity = _mask_identity(anchor)
        require(anchor.is_symlink() and os.readlink(anchor) == "/dev/null"
                and anchor.lstat().st_nlink == 1
                and all(identity[key] == record["identity"][key] for key in ("device", "inode", "uid")),
                "package_activation_owner", "Default-service ownership anchor changed.")
        record["anchor_identity"] = identity
    host.run(["systemctl", "daemon-reload"])
    record["state"] = "released"
    _save(host, journal)
    if anchor.exists() or anchor.is_symlink():
        require(anchor.is_symlink() and os.readlink(anchor) == "/dev/null"
                and anchor.lstat().st_nlink == 1
                and _mask_identity(anchor) == record.get("anchor_identity"),
                "package_activation_owner", "Default-service ownership anchor changed; refusing cleanup.")
        anchor.unlink()
    _sync_directory(path.parent)


def _clean_inventory(inventory: dict) -> None:
    broken = [name for name, value in inventory.items()
              if value["status"] not in {"install ok installed", "deinstall ok config-files", "unknown ok not-installed"}]
    if broken:
        _refuse(broken, "Unrelated incomplete dpkg packages require operator repair")


def _service(host: Host, unit: str) -> dict:
    result = host.run(["systemctl", "show", unit, "-p", "LoadState", "-p", "ActiveState", "-p", "MainPID"], check=False)
    value = dict(line.split("=", 1) for line in result.stdout.splitlines() if "=" in line)
    require(value.get("LoadState") in {"loaded", "not-found", "masked"}
            and value.get("MainPID", "").isdigit() and bool(value.get("ActiveState")),
            "package_service_inspection", "Cannot inspect default service metadata safely.")
    return value


def _holds_state(path: Path) -> bool:
    # postgresql-common, a pgBackRest dependency, creates both PostgreSQL roots empty.
    return path.exists() and (not path.is_dir() or any(path.iterdir()))


def _defaults(host: Host) -> dict:
    clusters = {}
    root = host.paths.p("/etc/postgresql")
    for path in sorted(root.glob("*/*")):
        if not path.is_dir():
            continue
        require(path.parent.name.isdigit() and re.fullmatch(r"[A-Za-z0-9_-]+", path.name),
                "package_cluster_inventory", "Unexpected PostgreSQL cluster path.")
        name = path.parent.name + "/" + path.name
        clusters[name] = {"service": _service(host, f"postgresql@{path.parent.name}-{path.name}.service")}
    return {"clusters": clusters,
            "postgres_config": _holds_state(host.paths.p("/etc/postgresql")),
            "postgres_data": _holds_state(host.paths.p("/var/lib/postgresql")),
            "redis_config": _holds_state(host.paths.p("/etc/redis")),
            "redis_data": _holds_state(host.paths.p("/var/lib/redis")),
            "redis": _service(host, "redis-server.service")}


def _restore_policy(host: Host, journal: dict) -> None:
    path = check_path(host.paths.p(POLICY), "file")
    if path.exists():
        require(path.read_text() == journal["policy"] and path.stat().st_mode & 0o777 == 0o755
                and (path.stat().st_uid == 0 or host.paths.root != Path("/")),
                "package_policy_conflict", "Service policy changed; refusing to remove an unknown policy-rc.d.")
        path.unlink()
        _sync_directory(path.parent)
    journal["policy_state"] = "restored"
    _save(host, journal)


def _sync_directory(path: Path) -> None:
    descriptor = os.open(path, os.O_RDONLY | os.O_DIRECTORY)
    try:
        os.fsync(descriptor)
    finally:
        os.close(descriptor)


def _suppress(host: Host, journal: dict) -> None:
    path = check_path(host.paths.p(POLICY), "file")
    if path.exists():
        require(path.read_text() == journal["policy"] and path.stat().st_mode & 0o777 == 0o755
                and (path.stat().st_uid == 0 or host.paths.root != Path("/")),
                "package_policy_conflict", "An existing policy-rc.d blocks package installation.")
    else:
        stage = check_path(path.parent / (".foundry-policy-" + journal["operation_id"]), "file")
        if stage.exists():
            require(stage.read_text() == journal["policy"], "package_policy_conflict", "Package policy staging file changed.")
        host.owned_write(stage, journal["policy"], mode=0o755)
        try:
            os.link(stage, path, follow_symlinks=False)
            _sync_directory(path.parent)
        except FileExistsError:
            raise DeployError("package_policy_conflict", "An existing policy-rc.d blocks package installation.") from None
        finally:
            stage.unlink()
    journal["policy_state"] = "installed"
    _save(host, journal)


def _run_transaction(host: Host, args: list[str]) -> None:
    # Host.run bounds a parent process, whereas apt owns a process tree. Keep
    # suppression until every transaction child has been terminated on timeout.
    if host.paths.root != Path("/"):
        host.run(args, env=ENVIRONMENT, timeout=1200)
        return
    environment = {"PATH": "/usr/sbin:/usr/bin:/sbin:/bin", "LANG": "C.UTF-8", "LC_ALL": "C.UTF-8", **ENVIRONMENT}
    # The child cannot exec apt until its process group is durably claimed. This
    # closes the parent-SIGKILL window before apt acquires any dpkg lock.
    gateway = ("import os,sys; "
               "token=sys.stdin.buffer.read(1); "
               "sys.exit(125) if token != b'G' else os.execvpe(sys.argv[1],sys.argv[1:],os.environ)")
    process = subprocess.Popen(["/usr/bin/python3", "-c", gateway, *args], stdin=subprocess.PIPE, stdout=subprocess.DEVNULL,
                               stderr=subprocess.DEVNULL, env=environment, start_new_session=True)
    try:
        journal = _load(host)
        journal["process_group"] = process.pid
        _save(host, journal)
        process.stdin.write(b"G")
        process.stdin.flush()
        process.stdin.close()
        result = process.wait(timeout=1200)
    except BaseException:
        try:
            os.killpg(process.pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
        process.wait()
        raise DeployError("package_interrupted", "Package transaction interrupted; resume the recorded install.") from None
    require(result == 0, "package_install_failed", "Package transaction failed; resume the recorded install.")
    require(not _transaction_active(host, _load(host)), "package_locked", "A package child is still running; service suppression is retained.")


def _transaction_active(host: Host, journal: dict | None) -> bool:
    group = journal.get("process_group") if journal else None
    if group is None:
        return False
    require(type(group) is int and group > 1, "package_journal", "Invalid package process group.")
    # Include surviving maintainer-script children even after apt itself exits.
    for path in host.paths.p("/proc").glob("[0-9]*/stat"):
        try:
            fields = path.read_text().rsplit(")", 1)[1].split()
        except FileNotFoundError:
            continue
        require(len(fields) >= 3 and fields[2].isdigit(), "package_process", "Cannot inspect package process ownership.")
        if int(fields[2]) == group and fields[0] != "Z":
            return True
    return False


def _check_resume(inventory: dict, journal: dict) -> None:
    before, approved = journal["before"], journal["transaction"]
    owned = {_package_key(inventory, name): version for name, version in approved.items()}
    previous_owned = {_package_key(before, name) for name in approved}
    changed = [name for name, value in before.items() if _record(inventory, name) != value]
    # Baseline may include a dpkg not-installed row for an approved new package.
    changed = [name for name in changed if name not in previous_owned or before[name]["status"] != "unknown ok not-installed"]
    for name, value in inventory.items():
        if name in before and name not in owned:
            continue
        if value["status"] == "unknown ok not-installed":
            continue
        if name not in owned or value["version"] != owned[name]:
            changed.append(name)
    if changed:
        _refuse(changed, "Package inventory differs from the owned install transaction")


def _finish_defaults(host: Host, journal: dict) -> None:
    before, current = journal["defaults"], _defaults(host)
    for name, record in before["clusters"].items():
        require(current["clusters"].get(name) == record, "package_service_changed", "A preexisting PostgreSQL service changed during installation.")
    require(current["redis"] == before["redis"] or before["redis"]["LoadState"] == "not-found",
            "package_service_changed", "A preexisting Redis service changed during installation.")
    if before["redis"]["LoadState"] == "not-found" and current["redis"]["LoadState"] != "not-found":
        require(current["redis"]["MainPID"] == "0" and current["redis"]["ActiveState"] == "inactive",
                "package_autostart", "A newly available default Redis service started unexpectedly.")
    created = journal.setdefault("package_created_unused", [])
    for name, record in current["clusters"].items():
        if name in before["clusters"]:
            continue
        major, cluster = name.split("/")
        require(cluster == "main" and not before["postgres_config"] and not before["postgres_data"]
                and f"postgresql-{major}" in journal["transaction"],
                "package_cluster_conflict", "Unexpected cluster appeared during package installation; operator review required.")
        require(record["service"]["MainPID"] == "0" and record["service"]["ActiveState"] == "inactive",
                "package_autostart", "A package-created PostgreSQL cluster started unexpectedly; refusing bootstrap.")
        if name not in created:
            created.append(name)
            _save(host, journal)
        path = check_path(host.paths.p(f"/etc/postgresql/{major}/main/start.conf"), "file")
        host.owned_write(path, "# Foundry package-created unused cluster; preserved.\nmanual\n")
    if (not before["redis_config"] and not before["redis_data"]
            and before["redis"]["LoadState"] == "not-found" and "redis-server" in journal["transaction"]):
        require(current["redis"]["MainPID"] == "0" and current["redis"]["ActiveState"] == "inactive",
                "package_autostart", "Package-created Redis started unexpectedly; refusing bootstrap.")
        if "redis-server.service" not in created:
            created.append("redis-server.service")
            _save(host, journal)
        host.run(["systemctl", "disable", "redis-server.service"])


def install_missing(host: Host, names: Sequence[str], *, reviewed: dict | None = None) -> None:
    """Resume owned installation before new requests; never upgrade or uninstall.

    M3 passes inspect's full reviewed observation while holding its host lock.
    After installation it calls executable_evidence for newly available package
    executable bindings before admitting any service that consumes them.
    """
    require(not isinstance(names, (str, bytes)) and all(isinstance(n, str) and PACKAGE.fullmatch(n) for n in names),
            "package_names", "Package names must be literal distro package identifiers.")
    journal = _load(host)
    # Ubuntu's daily unattended-upgrades holds these for minutes on a healthy host.
    deadline = time.monotonic() + LOCK_WAIT_SECONDS
    while _locks(host) and time.monotonic() < deadline:
        time.sleep(LOCK_POLL_SECONDS)
    require(not _locks(host), "package_locked", "apt/dpkg is busy; retry after the current transaction exits.")
    require(not _transaction_active(host, journal), "package_locked", "The recorded package process tree is still running.")
    if journal and (journal["state"] == "installing" or journal.get("policy_state") != "restored"):
        _restore_policy(host, journal)
    inventory = _inventory(host)
    prepared = False
    if not journal or journal["state"] == "complete":
        _clean_inventory(inventory)
        missing = sorted(set(name for name in names if not _installed(inventory, name)))
        if not missing:
            return
        policy = host.paths.p(POLICY)
        require(not policy.exists() and not policy.is_symlink(), "package_policy_conflict", "An existing policy-rc.d blocks package installation.")
        approved = _simulate(host, missing, inventory)
        evidence = _candidate_evidence(host, approved)
        activation = _activation_snapshot(host)
        if reviewed is not None:
            require(reviewed.get("transaction") == approved and reviewed.get("evidence") == evidence
                    and reviewed.get("installed") == inventory and reviewed.get("activation") == activation,
                    "package_plan_changed", "Package transaction, origin or activation changed after approval.")
        units, cron = _prepare_payloads(host, approved, evidence)
        for location in cron:
            path = check_path(host.paths.p(location), "file")
            preserved = check_path(path.with_name(path.name + ".dpkg-dist"), "file")
            require(not path.exists() and not preserved.exists(),
                    "package_activation", "Preexisting cron configuration cannot be claimed by package installation.")
        require(_inventory(host) == inventory and _activation_snapshot(host) == activation,
                "package_plan_changed", "Host changed while acquiring inert package payloads.")
        prepared = True
        operation = str(uuid.uuid4())
        retained_activation = journal.get("default_activation", {}) if journal else {}
        journal = {"schema": 1, "operation_id": operation, "state": "installing", "requested": missing,
                   "before": inventory, "defaults": _defaults(host), "transaction": approved,
                   "evidence": evidence, "activation_before": activation, "activation_units": units,
                   "cron_defaults": cron, "default_activation": retained_activation,
                   "transaction_sha256": _digest(approved), "policy_state": "intent",
                   "policy": f"#!/bin/sh\n# Foundry package transaction {operation}\nexit 101\n"}
        journal["supply_sha256"] = _digest({key: journal[key] for key in
                                          ("evidence", "activation_before", "activation_units", "cron_defaults")})
        _save(host, journal)
    _check_resume(inventory, journal)
    if reviewed is not None:
        require(reviewed.get("transaction") == journal["transaction"] and reviewed.get("evidence") == journal.get("evidence"),
                "package_plan_changed", "Reviewed supply transaction differs from the recovery intent.")
    require("evidence" in journal, "package_journal", "Legacy incomplete package intent needs reconciliation before capability installation.")
    pending = {name: version for name, version in journal["transaction"].items() if not _installed(inventory, name)}
    if pending and not prepared:
        evidence = _candidate_evidence(host, pending)
        require(evidence == {name: journal["evidence"][name] for name in pending},
                "package_plan_changed", "Package origin/hash changed during recovery.")
        units, cron = _prepare_payloads(host, pending, evidence)
        require(all(unit in journal["activation_units"] and set(owners) <= set(journal["activation_units"][unit])
                    for unit, owners in units.items()), "package_plan_changed", "Package activation payload changed during recovery.")
        require(all(record == journal.get("cron_defaults", {}).get(path) for path, record in cron.items()),
                "package_plan_changed", "Package cron payload changed during recovery.")
    _suppress(host, journal)
    try:
        _mask_defaults(host, journal)
        _suppress_cron(host, journal)
        # A07: configure only unpacked packages belonging to this exact intent.
        partial = [name for name in journal["transaction"] if _record(inventory, name).get("status", "").split()[-1:]
                   in (["unpacked"], ["half-configured"], ["triggers-awaited"], ["triggers-pending"])]
        if partial:
            options = ["--force-confold"] if journal.get("cron_defaults") else []
            _run_transaction(host, ["dpkg", *options, "--configure", *sorted(partial)])
            inventory = _inventory(host)
            _check_resume(inventory, journal)
        targets = [name + "=" + version for name, version in journal["transaction"].items() if not _installed(inventory, name)]
        if targets:
            _simulate(host, targets, inventory, approved=journal["transaction"])
            options = ["-o", "Dpkg::Options::=--force-confold"] if journal.get("cron_defaults") else []
            _run_transaction(host, ["apt-get", "-y", "--no-download", "--no-install-recommends", "--no-remove", "--no-upgrade", *options,
                                    "-o", "APT::Get::AllowUnauthenticated=false", "install", *targets])
        inventory = _inventory(host)
        _check_resume(inventory, journal)
        require(all(_installed(inventory, name) for name in journal["transaction"]),
                "package_incomplete", "The owned package transaction remains incomplete.")
        _finish_defaults(host, journal)
        _finish_activation(host, journal)
        _finish_cron(host, journal)
        journal["state"] = "complete"
        _save(host, journal)
    finally:
        # A surviving package manager retains the policy and pending journal.
        if not _locks(host) and not _transaction_active(host, _load(host)):
            _restore_policy(host, journal)
    remaining = [name for name in names if not _installed(inventory, name)]
    if remaining:
        # Do not extend a recovered approval with unrelated dependencies.
        _refuse(remaining, "Owned install resumed; review a new plan for the remaining packages")
