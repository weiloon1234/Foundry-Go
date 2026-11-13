"""Digest-bound, forward-only host tool installation and protocol negotiation.

Call install_tools with the host mutation lock held. Its journal is deliberately
independent of application lifecycle state: tool upgrades never restart services.
"""
from __future__ import annotations

import ast
import copy
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import stat
import sys
import uuid

if __name__ == "__main__":
    sys.dont_write_bytecode = True
    sys.path.insert(0, str(Path(__file__).resolve().parent))

from contracts import (HOST_PATHS, MAX_JSON, DeployError, atomic_json, atomic_write, check_path,
                       now, parse_json, read_json, require, valid_uuid, DeploymentSelection,
                       FEATURE_CAPABILITIES, TOPOLOGY_SCHEMA, FEATURE_PROFILE_SCHEMA, ARCHIVE_SCHEMA)

READ_ACTIONS = frozenset({"hello", "inspect", "status", "report", "logs", "config-keys", "sync", "tools-plan", "recover-plan", "capacity-plan"})
UPGRADE_ACTIONS = frozenset({"tools-apply", "upgrade-resume"})
BASE_REQUIRED_FILES = frozenset({"protocol.json", "lib/contracts.py", "lib/host.py", "lib/upgrades.py",
                            "lib/provision.py", "lib/recover.py", "lib/release.py", "lib/runtime_config.py",
                            "lib/capacity.py", "lib/journal.py", "lib/maintenance.py",
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


# Cumulative requirements are selected from the inspected bundle's protocol,
# never from the running tools' generation or the host's selected generation.
GENERATION_11_FILES = frozenset({"lib/uploads.py", "server/templates/nginx-upload.conf"})
GENERATION_12_FILES = frozenset({"lib/feature_lifecycle.py", "lib/feature_network.py", "lib/feature_runtime.py",
    "server/feature_operation.py", "server/templates/admission.service", "lib/artifacts.py", "lib/admission.py", "lib/services.py",
                               "server/runtime_v1.py", "server/templates/custom.service"})
REQUIRED_FILES = BASE_REQUIRED_FILES | GENERATION_11_FILES | GENERATION_12_FILES


def required_files(generation: int) -> frozenset[str]:
    required = BASE_REQUIRED_FILES
    if generation >= 11:
        required |= GENERATION_11_FILES
    if generation >= 12:
        required |= GENERATION_12_FILES
    return required


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
    require(required_files(protocol(source)["generation"]) <= {item["path"] for item in inventory},
            "tools_incomplete", "The tool bundle is incomplete.")
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


def _upgrade_journals(host) -> list[dict]:
    journals = []
    directory = check_path(host.paths.p(HOST_PATHS["upgrades"]), "directory")
    if directory.exists():
        for operation in directory.iterdir():
            check_path(operation, "directory")
            intent = check_path(operation / "intent.json", "file")
            if intent.exists():
                journals.append(read_json(intent))
    return journals


def _upgrade_pending(host, registry: dict) -> bool:
    return bool(registry["tools"]["upgrade_operation"]) or any(
        journal.get("phase") != "complete" for journal in _upgrade_journals(host))


def _obsolete_bundles(host, active_digest: str) -> list[Path]:
    directory = host.check_control_directory(host.paths.tools / "tools")
    obsolete = []
    if directory.exists():
        for bundle in sorted(directory.iterdir()):
            require(re.fullmatch(r"[a-f0-9]{64}", bundle.name), "unsafe_path",
                    "Unexpected tool bundle entry; administrator review required.")
            host.check_control_directory(check_path(bundle, "directory"))
            if bundle.name != active_digest:
                # Validate the whole tree before deleting anything. A partial
                # deletion remains a canonical directory that a retry can finish.
                for path in bundle.rglob("*"):
                    check_path(path, "directory" if path.is_dir() else "file")
                    if path.is_dir():
                        host.check_control_directory(path)
                obsolete.append(bundle)
    return obsolete


def _retired_bundles(host) -> list[str]:
    path = check_path(host.paths.p(HOST_PATHS["retired_tools"]), "file")
    host.check_control_directory(path.parent)
    if not path.exists():
        return []
    info = path.stat()
    require(stat.S_IMODE(info.st_mode) == 0o600 and info.st_nlink == 1
            and (info.st_uid == 0 or host.paths.root != Path("/")),
            "invalid_retired_tools", "The retired tool ledger must be root-owned with mode 0600.")
    try:
        value = read_json(path)
    except DeployError:
        raise DeployError("invalid_retired_tools", "The retired tool ledger is invalid; administrator review required.") from None
    require(isinstance(value, dict) and set(value) == {"schema", "digests"}
            and type(value["schema"]) is int and value["schema"] == 1
            and isinstance(value["digests"], list)
            and all(isinstance(item, str) and re.fullmatch(r"[a-f0-9]{64}", item) for item in value["digests"])
            and value["digests"] == sorted(set(value["digests"])),
            "invalid_retired_tools", "The retired tool ledger is invalid; administrator review required.")
    return value["digests"]


def _sync_directory(path: Path) -> None:
    descriptor = os.open(path, os.O_RDONLY)
    try:
        os.fsync(descriptor)
    finally:
        os.close(descriptor)


def _check_upgrade_directory(host, operation: Path) -> None:
    require(valid_uuid(operation.name), "unsafe_path", "Unexpected upgrade journal entry; administrator review required.")
    host.check_control_directory(check_path(operation, "directory"))
    require(operation.stat().st_dev == operation.parent.stat().st_dev, "unsafe_path",
            "Upgrade journals must remain on their parent's filesystem.")
    for path in operation.rglob("*"):
        check_path(path, "directory" if path.is_dir() else "file")
        if path.is_dir():
            host.check_control_directory(path)
        else:
            require(path.stat().st_nlink == 1, "unsafe_path", "Upgrade journal files must not be hardlinked.")


def _deletion_directory(host, *, create=False) -> Path:
    directory = host.check_control_directory(host.paths.p(HOST_PATHS["upgrade_deletions"]))
    host.check_control_directory(directory.parent)
    if create and not directory.exists():
        directory.mkdir(mode=0o700)
        _sync_directory(directory.parent)
    if directory.exists():
        require(stat.S_IMODE(directory.stat().st_mode) == 0o700, "unsafe_path",
                "The upgrade deletion area must have mode 0700.")
    upgrades = host.check_control_directory(host.paths.p(HOST_PATHS["upgrades"]))
    if upgrades.exists():
        destination = directory if directory.exists() else directory.parent
        require(destination.stat().st_dev == upgrades.stat().st_dev, "unsafe_path",
                "The upgrade deletion area must share the journals' filesystem.")
    return directory


def _deleting_upgrade_journals(host) -> list[Path]:
    directory = _deletion_directory(host)
    operations = sorted(directory.iterdir()) if directory.exists() else []
    for operation in operations:
        # Rename into this private area records deletion ownership. An interrupted
        # rmtree need not leave intent.json or any other journal file behind.
        _check_upgrade_directory(host, operation)
    return operations


def _obsolete_upgrade_journals(host, active_digest: str) -> list[tuple[Path, dict]]:
    directory = host.check_control_directory(host.paths.p(HOST_PATHS["upgrades"]))
    obsolete = []
    if directory.exists():
        for operation in sorted(directory.iterdir()):
            _check_upgrade_directory(host, operation)
            intent = check_path(operation / "intent.json", "file")
            require(intent.exists(), "tools_upgrade_pending", "An upgrade journal has no intent; administrator review required.")
            journal = read_json(intent)
            require(isinstance(journal, dict) and journal.get("phase") == "complete",
                    "tools_upgrade_pending", "Complete the pending tool upgrade before pruning journals.")
            require(journal.get("schema") == 1 and journal.get("operation_id") == operation.name
                    and isinstance(journal.get("previous_tools"), dict)
                    and all(isinstance(item, str) and re.fullmatch(r"[a-f0-9]{64}", item)
                            for item in (journal.get("bundle_sha256"), journal["previous_tools"].get("bundle_sha256"))),
                    "invalid_tools", "Invalid completed upgrade journal; administrator review required.")
            if journal["bundle_sha256"] != active_digest:
                obsolete.append((operation, journal))
    return obsolete


def _prune_bundles(host) -> dict:
    """Run only in the selected bundle with the caller's inherited host lock."""
    require(host.host_lock_fd is not None, "tools_lock_required", "Tool pruning requires the host lock.")
    registry = host.read_registry()
    for entry in registry["instances"].values():
        if entry.get("topology", {}).get("schema") == 2 or "deployment_selection" in entry:
            from feature_lifecycle import refuse
            refuse("helper bundle pruning")
    require(not _upgrade_pending(host, registry), "tools_upgrade_pending",
            "Complete the pending tool upgrade before pruning bundles.")
    active = _installed(host, registry["tools"])
    pointer = host.paths.tools / "current"
    check_path(pointer.parent, "directory")
    require(pointer.is_symlink() and os.readlink(pointer) == "tools/" + active.name,
            "tools_pointer_conflict", "The selected tool pointer differs from the registry.")
    obsolete = _obsolete_bundles(host, active.name)
    journals = _obsolete_upgrade_journals(host, active.name)
    deleting = _deleting_upgrade_journals(host)
    require(not {path.name for path in deleting} & {path.name for path, _ in journals},
            "unsafe_path", "An upgrade operation appears in both journal areas; administrator review required.")
    retired = set(_retired_bundles(host))
    require(not deleting or host.paths.p(HOST_PATHS["retired_tools"]).exists(), "invalid_retired_tools",
            "The retired tool ledger is missing for queued journal deletions.")
    removed = {"removed_bundles": [], "removed_upgrade_journals": []}
    if deleting:
        # Finish both sides of an interrupted rename before removing its contents.
        _sync_directory(host.paths.p(HOST_PATHS["upgrades"]))
        _sync_directory(deleting[0].parent)
        for operation in deleting:
            shutil.rmtree(operation)
            _sync_directory(operation.parent)
            removed["removed_upgrade_journals"].append(operation.name)
    for _, journal in journals:
        retired.update((journal["bundle_sha256"], journal["previous_tools"]["bundle_sha256"]))
    retired.discard(active.name)
    if journals:
        # Durable rejection history precedes every deletion; retries can repeat it.
        payload = json.dumps({"schema": 1, "digests": sorted(retired)}, sort_keys=True) + "\n"
        require(len(payload.encode("utf-8")) <= MAX_JSON, "invalid_retired_tools",
                "The retired tool ledger exceeds its size limit; administrator review required.")
        ledger = host.paths.p(HOST_PATHS["retired_tools"])
        if not host.owned_write(ledger, payload, mode=0o600):
            # A prior process may have renamed these same bytes before dying
            # during the directory sync. Finish that barrier before deletion.
            _sync_directory(ledger.parent)
    deletion_directory = _deletion_directory(host, create=bool(journals))
    for bundle in obsolete:
        shutil.rmtree(bundle)
        removed["removed_bundles"].append(bundle.name)
    for operation, _ in journals:
        destination = deletion_directory / operation.name
        os.rename(operation, destination)
        _sync_directory(operation.parent)
        _sync_directory(deletion_directory)
        shutil.rmtree(destination)
        _sync_directory(deletion_directory)
        removed["removed_upgrade_journals"].append(operation.name)
    removed["removed_upgrade_journals"].sort()
    return removed


def prune_tools(host, digest: str) -> dict:
    """Run selected-bundle cleanup while retaining the caller's host lock."""
    require(host.host_lock_fd is not None, "tools_lock_required", "Tool pruning requires the host lock.")
    registry = host.read_registry()
    if any(e.get("topology", {}).get("schema") == 2 or "deployment_selection" in e for e in registry["instances"].values()):
        from feature_lifecycle import LATER
        return {"removed_bundles": [], "removed_upgrade_journals": [],
                "pruning_error": {"code": "capability_unavailable", "detail": f"Feature helper pruning requires {LATER}; all bundles retained."}}
    selected = _bundle_path(host, digest)
    # Preserve the ledger diagnostic at the caller boundary; child failures are redacted.
    _retired_bundles(host)
    result = host.run(["/usr/bin/python3", "-I", str(selected / "lib/upgrades.py"), "prune",
                       str(host.paths.root), str(host.host_lock_fd)],
                      pass_fds=(host.host_lock_fd,), timeout=1800)
    removed = parse_json(result.stdout)
    require(isinstance(removed, dict) and set(removed) == {"removed_bundles", "removed_upgrade_journals"}
            and isinstance(removed["removed_bundles"], list) and isinstance(removed["removed_upgrade_journals"], list)
            and all(isinstance(item, str) and re.fullmatch(r"[a-f0-9]{64}", item) and item != digest
                    for item in removed["removed_bundles"])
            and all(valid_uuid(item) for item in removed["removed_upgrade_journals"]),
            "invalid_tools", "Invalid tool pruning result.")
    return removed


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
            "capabilities": ["topology-v1", "registry-v2", "digest-bound-tool-upgrade"]}


def contract_versions() -> dict:
    """Parser support, not advertised runtime support. Generation has one owner."""
    return {"generation": protocol()["generation"], "topology": (1, TOPOLOGY_SCHEMA),
            "profile": (2, FEATURE_PROFILE_SCHEMA), "archive": (1, ARCHIVE_SCHEMA),
            "capabilities": tuple(sorted(FEATURE_CAPABILITIES))}


def check_instance_capabilities(entry: dict, client_generation: int) -> None:
    """Target-local sticky admission; rolling back application code cannot clear it."""
    if "deployment_selection" not in entry:
        return
    selection = DeploymentSelection.parse(entry["deployment_selection"])
    require(type(client_generation) is int and client_generation >= selection.minimum_generation,
            "instance_capability_required", "This instance requires a host-capability-aware client.")


def _check_feature_request(registry, request, action):
    profile = request.get("profile")
    topology = request.get("topology")
    requested = (isinstance(profile, dict) and profile.get("schema") == FEATURE_PROFILE_SCHEMA
                 or isinstance(topology, dict) and topology.get("schema") == TOPOLOGY_SCHEMA)
    supported = {"bootstrap-plan", "bootstrap-apply", "activate", "restart", "stop", "down", "status", "logs", "sync", "config-keys"}
    if requested and action not in supported:
        from feature_lifecycle import refuse
        refuse(action)
    if not registry:
        return
    instance_id = profile.get("instance_id") if isinstance(profile, dict) else None
    entries = registry["instances"]
    # Global writers/collectors cannot safely partially interpret a feature host.
    # Instance operations against an untouched legacy neighbor remain unchanged.
    host_wide = action in UPGRADE_ACTIONS | {"tools-plan", "capacity-plan", "capacity-apply", "report"}
    selected = entries.values() if host_wide or instance_id is None else [entries.get(instance_id, {})]
    client = request.get("client")
    generation = client.get("generation") if isinstance(client, dict) else None
    for entry in selected:
        check_instance_capabilities(entry, generation)
        if (entry.get("topology", {}).get("schema") == 2 or "deployment_selection" in entry) and action not in supported | UPGRADE_ACTIONS | {"tools-plan"}:
            from feature_lifecycle import refuse
            refuse(action)


def check_client(host, request: dict, action: str) -> None:
    """Gate ordinary mutations on negotiated protocol and pending upgrade ownership."""
    registry = host.read_registry(missing=True)
    _check_feature_request(registry, request, action)
    if action in READ_ACTIONS:
        return
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
    require(action in UPGRADE_ACTIONS or client["generation"] <= metadata["generation"],
            "host_upgrade_required", "Review and apply a host tool upgrade before this mutation.")


def check_upgrade_generation(installed: dict, tools_dir: Path | None = None) -> None:
    """Reject unsupported installed hosts before uploading or installing a candidate."""
    generation = installed.get("generation") if isinstance(installed, dict) else None
    require(type(generation) is int and generation >= protocol(tools_dir)["minimum_host_generation"],
            "tools_upgrade_floor", "The host must already run generation 9 tools before this upgrade.")


def tools_upgrade(host, registry: dict, tools_dir: Path) -> dict | None:
    """Describe changed/removed paths and compatibility impact without mutating the host."""
    for entry in registry["instances"].values():
        check_instance_capabilities(entry, protocol(tools_dir)["generation"])
    current = registry["tools"]
    check_upgrade_generation(current, tools_dir)
    source, _, digest = tool_bundle(tools_dir)
    target = protocol(source)
    require(target["generation"] >= current["generation"], "tools_downgrade", "Host tool generations cannot be downgraded.")
    _deletion_directory(host)
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
    retired = digest in _retired_bundles(host) or any(journal.get("phase") == "complete" and digest in
                  (journal["bundle_sha256"], journal["previous_tools"]["bundle_sha256"])
                  for journal in _upgrade_journals(host))
    require(digest == current["bundle_sha256"] or current["upgrade_operation"]
            or (not _bundle_path(host, digest).exists() and not retired),
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
            "upgrade": tools_upgrade(host, registry, tools_dir),
            "prune_bundles": ([] if _upgrade_pending(host, registry) else
                              [path.name for path in _obsolete_bundles(host, tool_bundle(tools_dir)[2])]),
            "prune_upgrade_journals": ([] if _upgrade_pending(host, registry) else
                                      sorted([path.name for path, _ in _obsolete_upgrade_journals(host, tool_bundle(tools_dir)[2])]
                                             + [path.name for path in _deleting_upgrade_journals(host)]))}
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
    require(host.host_lock_fd is not None, "tools_lock_required", "Tool installation requires the host lock.")
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
            return {"outcome": "unchanged", "bundle_sha256": digest,
                    **prune_tools(host, digest)}
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
        return {"outcome": "unchanged", "operation_id": operation, "bundle_sha256": digest,
                **prune_tools(host, digest)}
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
    from provision import _install_unit
    ready = [entry for entry in registry["instances"].values() if entry["provisioning"]["state"] == "ready"]
    for entry in ready:
        _install_unit(host, registry, entry, destination)
    if ready:
        # A prior process may have written the units but exited before reloading.
        host.run(["systemctl", "daemon-reload"])
    journal["phase"] = "complete"
    atomic_json(journal_path, journal, 0o600)
    registry["tools"]["upgrade_operation"] = None
    _save_registry(host, registry)
    return {"outcome": "succeeded", "operation_id": operation, "bundle_sha256": digest,
            **prune_tools(host, digest)}


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


if __name__ == "__main__":
    try:
        from host import Host, Paths
        require(len(sys.argv) == 4 and sys.argv[1] == "prune", "invalid_request", "Unknown tool operation.")
        host = Host(Paths(Path(sys.argv[2])))
        descriptor = int(sys.argv[3])
        lock = check_path(host.paths.p(HOST_PATHS["locks"]) / "host.lock", "file")
        info, inherited = lock.stat(), os.fstat(descriptor)
        require((info.st_dev, info.st_ino) == (inherited.st_dev, inherited.st_ino),
                "tools_lock_required", "Tool pruning requires the host lock.")
        fcntl.flock(descriptor, fcntl.LOCK_EX | fcntl.LOCK_NB)
        host.host_lock_fd = descriptor
        active = _installed(host, host.read_registry()["tools"])
        require(Path(__file__).resolve() == active / "lib/upgrades.py", "tools_version_conflict",
                "Tool pruning must run from the selected bundle.")
        print(json.dumps(_prune_bundles(host)))
    except (DeployError, OSError, ValueError, KeyError, TypeError):
        sys.stderr.write("Tool bundle pruning failed; inspect host paths and upgrade journals.\n")
        sys.exit(1)
