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
import subprocess
import time
import uuid

from contracts import DeployError, HOST_PATHS, check_path, read_json, require
from host import Host, LOCK_WAIT_SECONDS

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
    return value


def _save(host: Host, value: dict) -> None:
    path = _journal_path(host)
    path.parent.mkdir(parents=True, mode=0o700, exist_ok=True)
    require(not path.parent.stat().st_mode & 0o077, "package_journal", "Package journal directory must be private.")
    host.owned_write(path, json.dumps(value, sort_keys=True, indent=2) + "\n", mode=0o600)


def _digest(value) -> str:
    return hashlib.sha256(json.dumps(value, sort_keys=True, separators=(",", ":")).encode()).hexdigest()


def _refuse(names, reason="Package transaction is not a pure new install"):
    raise DeployError("package_plan_refused", reason + ": " + ", ".join(sorted(set(names))))


def _parse_simulation(output: str, inventory: dict, *, approved: dict | None = None) -> dict:
    """Reject every installed-package action, including Conf without Inst."""
    installs, configurations = {}, {}
    rejected = set()
    approval = {_package_key(inventory, name): version for name, version in approved.items()} if approved is not None else None
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
        _refuse(rejected)
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


def inspect(host: Host) -> dict:
    """Read-only package plan, with owned incomplete installation considered first."""
    journal = _load(host)
    inventory, locks = _inventory(host), _locks(host)
    missing = _required(host, inventory)
    pending = journal is not None and journal["state"] == "installing"
    active = _transaction_active(host, journal)
    if pending and not locks and not active:
        _check_resume(inventory, journal)
    transaction = journal["transaction"] if pending else {}
    policy = host.paths.p(POLICY)
    if not pending and missing and not locks and not policy.exists() and not policy.is_symlink():
        _clean_inventory(inventory)
        transaction = _simulate(host, missing, inventory)
    return {"installed": inventory, "missing": missing, "locks": locks,
            "policy_present": policy.exists() or policy.is_symlink(),
            "transaction_active": active,
            "state": "busy" if locks or active else "resume" if pending else "blocked" if policy.exists() or policy.is_symlink() else "ready",
            "transaction": transaction, "ubuntu_major": ubuntu_major(host)}


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


def install_missing(host: Host, names: Sequence[str]) -> None:
    """Resume the owned transaction before considering any new package request."""
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
    if not journal or journal["state"] == "complete":
        _clean_inventory(inventory)
        missing = sorted(set(name for name in names if not _installed(inventory, name)))
        if not missing:
            return
        policy = host.paths.p(POLICY)
        require(not policy.exists() and not policy.is_symlink(), "package_policy_conflict", "An existing policy-rc.d blocks package installation.")
        approved = _simulate(host, missing, inventory)
        operation = str(uuid.uuid4())
        journal = {"schema": 1, "operation_id": operation, "state": "installing", "requested": missing,
                   "before": inventory, "defaults": _defaults(host), "transaction": approved,
                   "transaction_sha256": _digest(approved), "policy_state": "intent",
                   "policy": f"#!/bin/sh\n# Foundry package transaction {operation}\nexit 101\n"}
        _save(host, journal)
    _check_resume(inventory, journal)
    _suppress(host, journal)
    try:
        # A07: configure only unpacked packages belonging to this exact intent.
        partial = [name for name in journal["transaction"] if _record(inventory, name).get("status", "").split()[-1:]
                   in (["unpacked"], ["half-configured"], ["triggers-awaited"], ["triggers-pending"])]
        if partial:
            _run_transaction(host, ["dpkg", "--configure", *sorted(partial)])
            inventory = _inventory(host)
            _check_resume(inventory, journal)
        targets = [name + "=" + version for name, version in journal["transaction"].items() if not _installed(inventory, name)]
        if targets:
            _simulate(host, targets, inventory, approved=journal["transaction"])
            _run_transaction(host, ["apt-get", "-y", "--no-install-recommends", "--no-remove", "--no-upgrade", "install", *targets])
        inventory = _inventory(host)
        _check_resume(inventory, journal)
        require(all(_installed(inventory, name) for name in journal["transaction"]),
                "package_incomplete", "The owned package transaction remains incomplete.")
        _finish_defaults(host, journal)
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
