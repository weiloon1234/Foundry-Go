"""Digest-bound, forward-only host tool installation and protocol negotiation.

Call install_tools with the host mutation lock held. Its journal is deliberately
independent of application lifecycle state: tool upgrades never restart services.
"""
from __future__ import annotations

import ast
import copy
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import uuid

from contracts import (HOST_PATHS, DeployError, atomic_json, atomic_write, check_path,
                       now, read_json, require, valid_uuid)

READ_ACTIONS = frozenset({"hello", "inspect", "status", "report", "logs", "config-keys", "sync", "tools-plan", "recover-plan", "capacity-plan"})
UPGRADE_ACTIONS = frozenset({"tools-apply", "upgrade-resume"})
REQUIRED_FILES = frozenset({"protocol.json", "lib/contracts.py", "lib/host.py", "lib/upgrades.py",
                            "lib/provision.py", "lib/recover.py", "lib/release.py", "lib/runtime_config.py",
                            "lib/topology.py", "lib/report.py", "server/manage.py",
                            "server/report.py", "server/launcher.py", "lib/packages.py",
                            "lib/postgres.py", "lib/redis_acl.py", "lib/edge.py",
                            "lib/certificates.py", "lib/backup.py", "lib/cloudflare-ranges.json",
                            "server/templates/foundry-http@.service",
                            "server/templates/foundry-scheduler@.service",
                            "server/templates/foundry-worker@.service",
                            "server/templates/redis-server@foundry.service",
                            "server/templates/foundry-backup.service",
                            "server/templates/foundry-backup.timer",
                            "server/templates/nginx-http.conf", "server/templates/nginx-https.conf",
                            "server/templates/nginx-proxy.conf", "server/templates/nginx-upgrade.conf"})


def _hash(value: object) -> str:
    return hashlib.sha256(json.dumps(value, sort_keys=True, separators=(",", ":"), allow_nan=False).encode()).hexdigest()


def protocol(tools_dir: Path | None = None) -> dict:
    """Read the bundle's single generation/protocol authority; no app pin is inferred."""
    value = read_json(Path(tools_dir or Path(__file__).resolve().parents[1]) / "protocol.json")
    require(isinstance(value, dict) and set(value) == {"protocol", "generation", "minimum_host_generation",
                                                    "minimum_client_generation", "framework_version"},
            "invalid_protocol", "The tool protocol declaration is invalid.")
    require(value["protocol"] == {"major": 2, "minor": 0}
            and all(type(value[key]) is int and value[key] >= 1 for key in
                    ("generation", "minimum_host_generation", "minimum_client_generation"))
            and value["minimum_client_generation"] <= value["generation"]
            and value["minimum_host_generation"] <= value["generation"]
            and isinstance(value["framework_version"], str) and bool(value["framework_version"]),
            "invalid_protocol", "Unsupported tool protocol declaration.")
    return value


def _inventory(tools_dir: Path) -> tuple[Path, list[Path], list[dict]]:
    source = check_path(Path(tools_dir), "directory")
    files = sorted([source / "protocol.json", *source.glob("lib/*.py"), *source.glob("lib/*.json"),
                    *source.glob("server/*.py"), *source.glob("server/templates/*")])
    require(len(files) <= 512, "tools_too_large", "The tool bundle has too many files.")
    inventory, total = [], 0
    for path in files:
        check_path(path, "file")
        require(path.exists(), "tools_incomplete", "The tool bundle is incomplete.")
        size = path.stat().st_size
        total += size
        require(size <= 4 * 1024 * 1024 and total <= 32 * 1024 * 1024,
                "tools_too_large", "The tool bundle exceeds its size limit.")
        inventory.append({"path": path.relative_to(source).as_posix(), "size": size, "mode": 0o644,
                          "sha256": hashlib.sha256(path.read_bytes()).hexdigest()})
    required = REQUIRED_FILES if protocol(source)["generation"] >= 3 else REQUIRED_FILES - {"lib/recover.py"}
    if protocol(source)["generation"] >= 5:
        required = required | {"lib/compatibility.py", "server/start_check.py", "lib/cutover_contracts.py", "server/cutover_runner.py"}
    require(required <= {item["path"] for item in inventory}, "tools_incomplete", "The tool bundle is incomplete.")
    return source, files, inventory


def tool_bundle(tools_dir: Path) -> tuple[Path, list[Path], str]:
    """Inventory bounded regular host files; digest covers canonical paths, sizes, modes and bytes."""
    source, files, inventory = _inventory(tools_dir)
    protocol(source)
    return source, files, _hash({"schema": 1, "files": inventory})


def protocol_metadata(tools_dir: Path | None = None) -> dict:
    """Build the protocol/client request envelope from the selected framework bundle."""
    source = Path(tools_dir or Path(__file__).resolve().parents[1])
    metadata = protocol(source)
    return {"protocol": metadata["protocol"], "client": {"framework_version": metadata["framework_version"],
            "generation": metadata["generation"], "bundle_sha256": tool_bundle(source)[2]}}


def _bundle_path(host, digest: str) -> Path:
    require(isinstance(digest, str) and re.fullmatch(r"[a-f0-9]{64}", digest), "invalid_tools", "Invalid tool bundle digest.")
    return host.paths.tools / "tools" / digest


def _installed(host, record: dict) -> Path:
    path = _bundle_path(host, record["bundle_sha256"])
    require(record["active_path"] == HOST_PATHS["tools"] + "/tools/" + record["bundle_sha256"],
            "tools_version_conflict", "Registered tool path is not canonical.")
    require(path.exists() and tool_bundle(path)[2] == record["bundle_sha256"],
            "tools_version_conflict", "Installed tool content differs from its registered digest.")
    manifest = read_json(path / "manifest.json")
    require(manifest == {"schema": 1, "files": _inventory(path)[2]},
            "tools_version_conflict", "Installed tool manifest differs from its inventory.")
    for directory in [path, *[p for p in path.rglob("*") if p.is_dir()]]:
        host.check_control_directory(directory)
    for item in manifest["files"]:
        info = (path / item["path"]).stat()
        require(stat.S_IMODE(info.st_mode) == item["mode"] and
                (info.st_uid == 0 or host.paths.root != Path("/")),
                "tools_ownership", "Installed tools have unexpected ownership or permissions.")
    return path


def _bundle_directory(path: Path) -> None:
    # Instance accounts and the public report execute the bundle; a hardened
    # root umask must not make it root-only.
    check_path(path, "directory")
    path.mkdir(parents=True, exist_ok=True)
    os.chmod(path, 0o755)


def _active_record(tools_dir: Path, digest: str, operation: str | None) -> dict:
    metadata = protocol(tools_dir)
    return {"framework_version": metadata["framework_version"], "generation": metadata["generation"],
            "bundle_sha256": digest, "protocol_min": "2.0", "protocol_max": "2.0",
            "active_path": HOST_PATHS["tools"] + "/tools/" + digest, "upgrade_operation": operation}


def hello(host) -> dict:
    """Return bounded, public host negotiation data even for an unsupported client."""
    registry = host.read_registry(missing=True)
    metadata = protocol()
    record = registry["tools"] if registry else None
    if record and (_bundle_path(host, record["bundle_sha256"]) / "manifest.json").exists():
        metadata = protocol(_bundle_path(host, record["bundle_sha256"]))
    return {"host_id": registry["host_id"] if registry else None, "tools": copy.deepcopy(record),
            "protocol": metadata["protocol"], "supported_protocols": [{"major": 2, "minor_min": 0, "minor_max": 0}],
            "client_generation": {"minimum": metadata["minimum_client_generation"], "maximum": metadata["generation"]},
            "archive_schema": 1, "inspection_schema": 1,
            "capabilities": ["queue-layout-cutover-v1", "topology-v1", "registry-v2", "digest-bound-tool-upgrade"]}


def check_client(host, request: dict, action: str) -> None:
    """Gate ordinary mutations on negotiated protocol and pending upgrade ownership."""
    if action in READ_ACTIONS:
        return
    registry = host.read_registry(missing=True)
    if registry and action in UPGRADE_ACTIONS:
        import compatibility
        for entry in registry["instances"].values():
            compatibility.require_upgrade_idle(host, entry, host.load_state(entry))
    if registry and action != "capacity-apply":
        import capacity
        capacity.require_idle(host)
    if registry and action not in {"recover-plan", "recover-apply"}:
        for entry in registry["instances"].values():
            require(entry.get("recovered_from", {}).get("mode") != "rehearsal",
                    "rehearsal_read_only", "Rehearsals cannot be mutated or promoted; recreate the server for takeover.")
            require(not entry.get("recovered_from") or entry["provisioning"]["state"] == "ready",
                    "recovery_pending", "Resume the recorded recovery before other mutations.")
    if registry and registry["tools"]["upgrade_operation"] and action not in UPGRADE_ACTIONS | {"recover-apply"}:
        raise DeployError("tools_upgrade_pending", "Resume the approved tool upgrade before other mutations.")
    metadata = protocol()
    if registry and (_bundle_path(host, registry["tools"]["bundle_sha256"]) / "manifest.json").exists():
        metadata = protocol(_bundle_path(host, registry["tools"]["bundle_sha256"]))
    client, wire = request.get("client"), request.get("protocol")
    require(isinstance(client, dict) and type(client.get("generation")) is int and isinstance(wire, dict),
            "invalid_request", "Mutations require an explicit client generation and protocol.")
    require(wire.get("major") == metadata["protocol"]["major"] and
            type(wire.get("minor")) is int and 0 <= wire["minor"] <= metadata["protocol"]["minor"] and
            client["generation"] >= metadata["minimum_client_generation"],
            "client_too_old", "Bump your Foundry-Go pin to a supported deploy generation.")
    if registry and any(host.load_state(entry).get("queue_compatibility") for entry in registry["instances"].values()):
        require(client["generation"] >= 5, "client_too_old", "Queue-fenced hosts require a cutover-aware deployment client.")
    require(action in UPGRADE_ACTIONS or client["generation"] <= metadata["generation"],
            "host_upgrade_required", "Review and apply a host tool upgrade before this mutation.")


def tools_upgrade(host, registry: dict, tools_dir: Path) -> dict | None:
    """Describe changed/removed paths and compatibility impact without mutating the host."""
    source, _, digest = tool_bundle(tools_dir)
    current = registry["tools"]
    target = protocol(source)
    require(target["generation"] >= current["generation"], "tools_downgrade", "Host tool generations cannot be downgraded.")
    retained_releases = []
    for instance_id, entry in registry["instances"].items():
        require(target["minimum_client_generation"] <= entry["client_generation"] <= target["generation"],
                "tools_incompatible", "The proposed tool generation would strand a registered application pin.")
        releases = check_path(host.paths.opt(entry["name"]) / "releases", "directory")
        if releases.exists():
            from release import validate_manifest
            for release in sorted(releases.iterdir()):
                check_path(release, "directory")
                manifest = validate_manifest(read_json(release / "manifest.json"), entry)
                retained_releases.append({"instance_id": instance_id, "release": manifest["version"], "schema": manifest["schema"]})
    installed = _bundle_path(host, current["bundle_sha256"])
    fresh = not installed.exists() and not registry["instances"] and not current["upgrade_operation"]
    require(digest == current["bundle_sha256"] or current["upgrade_operation"]
            or not _bundle_path(host, digest).exists(),
            "tools_downgrade", "A retired installed tool bundle cannot be selected again.")
    if not fresh and not current["upgrade_operation"]:
        _installed(host, current)
    if digest == current["bundle_sha256"] and not current["upgrade_operation"] and not fresh:
        return None
    old = ({item["path"]: item for item in _inventory(installed)[2]}
           if installed.exists() and not current["upgrade_operation"] else {})
    new = {item["path"]: item for item in _inventory(source)[2]}
    effects = []
    if registry.get("shared", {}).get("backup"):
        import backup
        import postgres
        cluster = registry["shared"]["postgresql"]
        if (postgres.archive_reload_required(cluster, backup._archive_command())
                or postgres.hba_reload_required(host, registry)):
            effects.append({"service": "postgresql", "unit": cluster["unit"], "action": "reload"})
    return {"from_digest": current["bundle_sha256"] if not fresh else None, "bundle_sha256": digest,
            "generation": target["generation"], "changed_paths": sorted(k for k in new if new[k] != old.get(k)),
            "removed_paths": sorted(old.keys() - new.keys()), "instances": sorted(registry["instances"]),
            "retained_releases": retained_releases,
            "migration_steps": [], "service_effects": effects, "helper_architecture": None}


def verify_tools(host, registry: dict, tools_dir: Path, allow_upgrade: bool = False) -> tuple[Path, list[Path], str]:
    """Validate candidate/current content; differing tools require digest-bound plan approval."""
    bundle = tool_bundle(tools_dir)
    change = tools_upgrade(host, registry, tools_dir)
    require(change is None or change["from_digest"] is None or allow_upgrade,
            "tools_version_conflict", "Review and apply the tool upgrade plan before replacing host tools.")
    return bundle


def plan_upgrade(host, tools_dir: Path) -> dict:
    """Bind approval to host identity, registry revision, current digest and exact effects."""
    registry = host.read_registry()
    plan = {"schema": 1, "host_id": registry["host_id"], "revision": registry["revision"],
            "upgrade": tools_upgrade(host, registry, tools_dir)}
    plan["plan_token"] = _hash(plan)
    return plan


def _save_registry(host, registry: dict) -> None:
    registry["revision"] += 1
    registry["updated_at"] = now()
    host.write_registry(registry)


def _select(host, digest: str) -> None:
    pointer = host.paths.p(HOST_PATHS["current"])
    check_path(pointer.parent, "directory")
    require(not pointer.exists() or pointer.is_symlink(), "tools_pointer_conflict", "The tool pointer is not an owned symlink.")
    if pointer.is_symlink():
        require(re.fullmatch(r"tools/[a-f0-9]{64}", os.readlink(pointer)), "tools_pointer_conflict", "The tool pointer target is unsafe.")
    temporary = pointer.parent / (".current-" + uuid.uuid4().hex)
    temporary.symlink_to("tools/" + digest)
    try:
        os.replace(temporary, pointer)
        descriptor = os.open(pointer.parent, os.O_RDONLY)
        try:
            os.fsync(descriptor)
        finally:
            os.close(descriptor)
    finally:
        temporary.unlink(missing_ok=True)


def install_tools(host, registry: dict, tools_dir: Path, allow_upgrade: bool = False,
                  operation_id: str | None = None) -> dict:
    """Install/resume an approved immutable bundle under the caller's host lock.

    The private intent precedes every write; retries accept only its original
    target digest. Interrupted publication always completes forward.
    """
    source, files, digest = verify_tools(host, registry, tools_dir, allow_upgrade)
    current = registry["tools"]
    pending = current["upgrade_operation"]
    operation = pending or operation_id or str(uuid.uuid4())
    require(valid_uuid(operation), "invalid_operation", "Tool upgrades require a UUIDv4 operation ID.")
    require(not pending or operation_id in (None, pending), "tools_upgrade_pending", "Another tool upgrade must be resumed first.")
    journal_dir = check_path(host.paths.p(HOST_PATHS["upgrades"]) / operation, "directory")
    journal_path = journal_dir / "intent.json"
    if journal_path.exists():
        journal = read_json(journal_path)
        require(journal["schema"] == 1 and journal["operation_id"] == operation
                and journal["phase"] in {"intent", "staged", "selected", "complete"}
                and journal["host_id"] == registry["host_id"] and journal["bundle_sha256"] == digest,
                "tools_upgrade_pending", "The pending upgrade is bound to another tool bundle.")
        require(pending or journal["phase"] == "complete" or journal["registry_sha256"] == _hash(registry),
                "plan_changed", "The registry changed before the upgrade intent was claimed.")
    else:
        require(not pending, "tools_upgrade_journal_missing", "The pending upgrade journal is missing.")
        if current["bundle_sha256"] == digest and _bundle_path(host, digest).exists():
            _installed(host, current)
            return {"outcome": "unchanged", "bundle_sha256": digest}
        journal_dir.mkdir(parents=True, exist_ok=True, mode=0o700)
        os.chmod(journal_dir, 0o700)
        journal = {"schema": 1, "operation_id": operation, "host_id": registry["host_id"],
                   "bundle_sha256": digest, "previous_tools": copy.deepcopy(current), "phase": "intent",
                   "registry_sha256": _hash(registry)}
        atomic_json(journal_dir / "registry-before.json", registry, 0o600)
        atomic_json(journal_path, journal, 0o600)
    if journal["phase"] == "complete":
        require(current["bundle_sha256"] == digest, "tools_upgrade_conflict", "Completed upgrade state differs.")
        _installed(host, current)
        if pending:
            registry["tools"]["upgrade_operation"] = None
            _save_registry(host, registry)
        return {"outcome": "unchanged", "operation_id": operation, "bundle_sha256": digest}
    if not pending:
        current["upgrade_operation"] = operation
        _save_registry(host, registry)
    destination = check_path(_bundle_path(host, digest), "directory")
    for directory in (host.paths.tools, host.paths.tools / "tools", destination):
        _bundle_directory(directory)
    for path in files:
        if path.suffix == ".py":
            try:
                ast.parse(path.read_bytes(), filename=str(path.relative_to(source)))
            except (SyntaxError, ValueError, UnicodeError):
                raise DeployError("tools_syntax_invalid", "A candidate Python source is invalid.") from None
        relative = path.relative_to(source)
        for parent in reversed(relative.parents[:-1]):
            _bundle_directory(destination / parent)
        target = check_path(destination / relative, "file")
        if target.exists():
            require(target.read_bytes() == path.read_bytes(), "tools_version_conflict", "The staged immutable bundle has drifted.")
        atomic_write(target, path.read_bytes(), 0o644)
    atomic_json(destination / "manifest.json", {"schema": 1, "files": _inventory(source)[2]})
    target_record = _active_record(source, digest, operation)
    _installed(host, target_record)
    journal["phase"] = "staged"
    atomic_json(journal_path, journal, 0o600)
    launcher = host.paths.p(HOST_PATHS["launcher"])
    candidate_launcher = ((source / "server/launcher.py").read_text()
                          .replace("@TOOLS_ROOT@", HOST_PATHS["tools"])
                          .replace("@REGISTRY@", HOST_PATHS["registry"])
                          .replace("@UPGRADES@", HOST_PATHS["upgrades"])).encode()
    if launcher.exists():
        require(check_path(launcher, "file").read_bytes() == candidate_launcher,
                "launcher_bridge_required", "Changing the stable launcher requires a bridge release.")
    atomic_write(launcher, candidate_launcher, 0o755)
    report_wrapper = ('#!/bin/sh\nexec /usr/bin/python3 -B "' + HOST_PATHS["current"] + '/server/report.py" "$@"\n')
    report = host.paths.p(HOST_PATHS["report"])
    if report.exists():
        require(check_path(report, "file").read_text() == report_wrapper,
                "report_command_ownership", "The report command has unexpected content.")
    atomic_write(report, report_wrapper, 0o755)
    _select(host, digest)
    registry["tools"] = target_record
    _save_registry(host, registry)
    journal["phase"] = "selected"
    atomic_json(journal_path, journal, 0o600)
    _installed(host, registry["tools"])
    if registry.get("shared", {}).get("backup"):
        # Execute the selected bundle's implementation, not this old process's imports.
        host.run(["/usr/bin/python3", "-I", str(destination / "lib/backup.py"), "rebind"], timeout=1800)
        registry = host.read_registry()
    if protocol(source)["generation"] >= 5:
        from provision import _install_unit
        for entry in registry["instances"].values():
            if entry["provisioning"]["state"] == "ready":
                _install_unit(host, registry, entry, destination)
    journal["phase"] = "complete"
    atomic_json(journal_path, journal, 0o600)
    registry["tools"]["upgrade_operation"] = None
    _save_registry(host, registry)
    return {"outcome": "succeeded", "operation_id": operation, "bundle_sha256": digest}


def apply_upgrade(host, tools_dir: Path, plan_token: str, operation_id: str) -> dict:
    """Recheck plan approval under the host lock, or resume its recorded target digest."""
    require(valid_uuid(operation_id), "invalid_operation", "Tool upgrades require a UUIDv4 operation ID.")
    with host.lock("host"):
        registry = host.read_registry()
        pending = registry["tools"]["upgrade_operation"]
        if not pending:
            previous = host.paths.p(HOST_PATHS["upgrades"]) / operation_id / "intent.json"
            if previous.exists():
                journal = read_json(previous)
                require(journal["phase"] == "complete" and journal["bundle_sha256"] == tool_bundle(tools_dir)[2],
                        "tools_upgrade_conflict", "This operation ID belongs to a different tool upgrade.")
            else:
                require(plan_upgrade(host, tools_dir)["plan_token"] == plan_token,
                        "plan_changed", "Tool upgrade preconditions changed; review a new plan.")
        else:
            require(pending == operation_id, "tools_upgrade_pending", "Resume the existing tool upgrade operation.")
        return install_tools(host, registry, tools_dir, True, operation_id)
