"""Replacement-host recovery. The private journal owns one immutable recovery intent."""
from __future__ import annotations

import hashlib
import copy
from dataclasses import replace
import io
import ipaddress
import json
from pathlib import Path
import re
import secrets
import tarfile
import uuid

import backup
import packages
import postgres
import provision
import redis_acl
import release
import runtime_config
import upgrades
from contracts import (DeployError, DeploymentSelection, PORT_FIRST, PORT_LAST, POSTGRES_PORT, RecoveryPublicBinding,
                       canonical_digest, record_document,
                       check_path, digest, dump_env, identity, instance_paths, now, parse_env,
                       parse_json, read_json, require, select_runtime_storage, valid_uuid,
                       validate_profile, validate_project)
from runtime_config import read_credentials, validate_runtime_environment, _read_value_file
from topology import ExecutableRole, parse_profile, parse_topology, validate_topology

JOURNAL = "/var/lib/foundry-recovery/operation.json"
PHASES = ("preflight", "resources", "database", "application", "release", "backup", "timers", "complete")


def replacement_binding(value, metadata, profile, operation_id, mode):
    """Bind an explicit address-only override to the archived descriptor and operation."""
    source = metadata["profile"]
    listeners = source["listeners"]
    require(mode != "rehearsal" or value is None, "recovery_public_binding",
            "A recovery rehearsal cannot accept public cutover bindings.")
    if mode == "rehearsal" or not listeners:
        require(value is None, "recovery_public_binding", "This recovery has no public listener override.")
        return None
    require(value is not None, "recovery_public_binding", "Supply explicit replacement addresses for every public listener.")
    binding = RecoveryPublicBinding.parse(value)
    require(binding.operation_id == operation_id and binding.source_instance_id == source["instance_id"]
            and binding.source_host_id == metadata["host_id"] == source["host"]["host_id"]
            and binding.target_host_id == profile["host"]["host_id"]
            and binding.deployment_sha256 == canonical_digest(metadata["feature_recovery"]["deployment"])
            and {item.name for item in binding.listeners} == set(listeners)
            and binding.dns_cutover == "external_pending",
            "recovery_public_binding", "Replacement binding differs from the source selection, target or operation.")
    hostnames = {}
    for item in binding.listeners:
        original = listeners[item.name]
        require(item.hostname == original["dns"]["hostname"] and item.ports == tuple(original["ports"])
                and set(item.old_addresses) == set(original["dns"]["public_addresses"])
                and item.source_binding_sha256 == canonical_digest(original)
                and not set(item.old_addresses) & set(item.replacement_addresses),
                "recovery_public_binding", "Replacement bindings must preserve listener identity and use new addresses.")
        addresses = (frozenset(item.old_addresses), frozenset(item.replacement_addresses))
        require(item.hostname not in hostnames or hostnames[item.hostname] == addresses,
                "recovery_public_binding", "Listeners sharing a hostname must request the same DNS records.")
        hostnames[item.hostname] = addresses
    return binding


def effective_profile(profile, binding):
    value = copy.deepcopy(profile)
    if binding is not None:
        for item in binding.listeners:
            value["listeners"][item.name]["dns"]["public_addresses"] = list(item.replacement_addresses)
    return value


def rehearsal_roles(inventory):
    selected = {"http"} | inventory.prerequisites({"http"})
    for name in sorted(selected - {"http"}):
        role = inventory.roles[name]
        require(isinstance(role, ExecutableRole) and role.recovery_rehearsal_safe,
                "recovery_rehearsal_dependency", f"HTTP recovery rehearsal requires unsafe dependency {name}; it remains stopped.")
    return inventory.start_order(selected)


def takeover_selection(inventory, snapshot):
    selection = DeploymentSelection.parse(snapshot["selection"])
    enabled = set(selection.enabled_roles) & inventory.roles.keys()
    holds = set(selection.maintenance_holds) & inventory.roles.keys()
    holds.update(name for name in enabled if inventory.prerequisites({name}) & set(selection.maintenance_holds))
    return inventory.start_order(enabled), tuple(sorted(holds)), inventory.start_order(enabled - holds)


def _feature_owner():
    """Lane A must implement closed preparation, admitted activation and private health."""
    import feature_lifecycle
    for name in ("prepare_recovery", "activate_recovery", "recovery_ready", "start_units", "stop_units"):
        require(callable(getattr(feature_lifecycle, name, None)), "capability_unavailable",
                f"Feature recovery requires lane A feature_lifecycle.{name}; no replacement services were started.")
    return feature_lifecycle


def _dns_result(host, journal):
    """Read DNS only after private takeover health; observations never prove traffic cutover."""
    from listeners import observe_dns
    raw = journal["intent"].get("recovery_public_binding")
    if raw is None:
        return {}
    require(journal["intent"]["mode"] == "takeover" and journal.get("old_server_stopped_confirmed_at")
            and journal.get("private_takeover_healthy_at"), "recovery_dns_not_ready",
            "DNS instructions require old-server-stopped confirmation and private takeover health.")
    binding = RecoveryPublicBinding.parse(raw)
    profile = parse_profile(journal["intent"]["effective_profile"], parse_topology(journal["intent"]["topology"], registered=True))
    observations = []
    for _, listener in profile.listeners:
        try:
            observations.append(observe_dns(listener))
        except (DeployError, OSError):
            pass
    observations = tuple(observations)
    matched = all(any(o.hostname == item.hostname and set(o.addresses) == set(item.replacement_addresses)
                      for o in observations) for item in binding.listeners)
    observed = replace(binding, observations=observations,
                       dns_cutover="external_observed" if matched else "external_pending")
    instructions = []
    for item in binding.listeners:
        for family, kind in ((4, "A"), (6, "AAAA")):
            old = sorted(a for a in item.old_addresses if ipaddress.ip_address(a).version == family)
            new = sorted(a for a in item.replacement_addresses if ipaddress.ip_address(a).version == family)
            if new:
                instruction = f"Set DNS-only {kind} {item.hostname}: replace {', '.join(old)} with {', '.join(new)}."
                if instruction not in instructions:
                    instructions.append(instruction)
    journal["dns_recovery"] = record_document(observed)
    if matched:
        journal.setdefault("dns_matched_at", now())
    _save(host, journal)
    return {"dns_cutover": observed.dns_cutover, "dns_recovery": journal["dns_recovery"],
            "dns_matched_at": journal.get("dns_matched_at"), "traffic_cutover": "operator_verification_required",
            "remaining_manual_steps": instructions}


def read_archive(path):
    """Read a bounded archive without extraction or trusting paths from its headers."""
    check_path(path, "file")
    require(path.stat().st_size <= backup.MAX_ARCHIVE_BYTES + 4 * 1024 * 1024,
            "recovery_archive", "Recovery archive exceeds its size limit.")
    files, size = {}, 0
    try:
        with tarfile.open(path, "r:") as archive:
            for member in archive:
                name = release.safe_member(member.name)
                require(name == member.name and name not in files and member.isfile()
                        and 0 <= member.size <= 16 * 1024 * 1024 and len(files) < 4096,
                        "recovery_archive", "Recovery members must be unique bounded regular files.")
                size += member.size
                require(size <= backup.MAX_ARCHIVE_BYTES, "recovery_archive", "Recovery archive is too large.")
                files[name] = archive.extractfile(member).read()
    except (tarfile.TarError, OSError, EOFError):
        raise DeployError("recovery_archive", "Cannot read recovery archive safely.") from None
    return files


def folder_archive(directory, destination):
    """Local-only: stream the operator's decrypted files, never an age identity."""
    directory = check_path(Path(directory).expanduser().absolute(), "directory")
    require(directory.is_dir(), "recovery_archive", "FOUNDRY_RECOVERY_DIR must be a decrypted archive directory.")
    with tarfile.open(destination, "w") as archive:
        size = 0
        for index, path in enumerate(sorted(directory.rglob("*"))):
            require(index < 8192, "recovery_archive", "Recovery folder contains too many members.")
            check_path(path)
            # Finder adds .DS_Store to any folder an operator opens.
            if path.is_dir() or path.name == ".DS_Store":
                continue
            require(path.is_file() and path.stat().st_size <= 16 * 1024 * 1024,
                    "recovery_archive", "Recovery folder members must be bounded regular files.")
            content = path.read_bytes()
            size += len(content)
            require(size <= backup.MAX_ARCHIVE_BYTES and b"AGE-SECRET-KEY-" not in content,
                    "recovery_archive", "Recovery folder is too large or contains an age private identity; keep it outside this folder.")
            item = tarfile.TarInfo(path.relative_to(directory).as_posix())
            item.mode, item.size = 0o600, len(content)
            archive.addfile(item, io.BytesIO(content))


def archive_configuration(project, profile, files, topology):
    expected = identity(project, profile)
    validate_project(project)
    validate_profile(profile, project)
    prefix = "instances/" + expected["name"] + "/"
    config = instance_paths(expected["name"])["configuration"]
    referenced = "files/" + config.lstrip("/") + "/"
    require(all(name in {"recovery/instance.json", "recovery/pgbackrest-repository.json"}
                or name.startswith(prefix) or name.startswith(referenced) for name in files),
            "recovery_identity", "Recovery archive contains another instance or an unexpected member.")
    required = {"recovery/pgbackrest-repository.json", *[prefix + name for name in
                ("app.env", "secrets.json", "bootstrap-secrets.json", "backup.json")]}
    require(required <= files.keys(), "recovery_members", "Recovery archive lacks required repository, environment, credentials or backup intake members.")
    for name, content in files.items():
        if name.startswith(referenced):
            member = prefix + name.removeprefix(referenced)
            require(member not in files or files[member] == content,
                    "recovery_members", "Duplicate recovery configuration sources disagree.")
    credentials = parse_json(files[prefix + "secrets.json"])
    intake = parse_json(files[prefix + "bootstrap-secrets.json"])
    settings = parse_json(files[prefix + "backup.json"])
    require(all(isinstance(item, dict) and item.get("instance_id") == profile["instance_id"]
                for item in (credentials, intake, settings)), "recovery_identity", "Recovery archive belongs to another instance ID.")
    require(all(re.fullmatch(r"[A-Za-z0-9_-]{32,128}", credentials.get(key, ""))
                for key in ("database_password", "redis_password")), "recovery_members", "Recovery credentials are invalid.")
    backup._recipients(settings.get("age_recipients"))
    from runtime_config import _encryption_pair
    require(isinstance(intake.get("encryption"), dict), "recovery_members", "Recovery encryption intake is missing.")
    _encryption_pair(intake["encryption"])
    repository = parse_json(files["recovery/pgbackrest-repository.json"])
    backup._repository(repository)
    require(re.fullmatch(r"[A-Za-z0-9_-]{43,128}", repository.get("cipher_pass", "")) is not None
            and valid_uuid(repository.get("host_id")) and isinstance(repository.get("cluster_token"), str),
            "recovery_members", "Recovery repository identity or cipher passphrase is missing.")
    try:
        values = parse_env(files[prefix + "app.env"].decode("utf-8"))
    except UnicodeError:
        raise DeployError("recovery_members", "Recovery environment must be UTF-8.") from None
    for key, value in values.items():
        if key.endswith("_FILE") and value:
            require(value.startswith(config + "/") and ".." not in Path(value).parts,
                    "recovery_members", "Recovery references must stay inside the instance configuration.")
            member = prefix + value.removeprefix(config + "/")
            alias = "files/" + value.lstrip("/")
            require(member in files or alias in files, "recovery_members", "A referenced recovery key/config file is missing.")
            require(member not in files or alias not in files or files[member] == files[alias],
                    "recovery_members", "Duplicate recovery configuration sources disagree.")
    address = values.get("APP__HTTP__SERVER__ADDRESS", "")
    match = re.fullmatch(r"127\.0\.0\.1:([0-9]+)", address)
    require(match is not None and PORT_FIRST <= int(match[1]) <= PORT_LAST,
            "recovery_members", "Recovered HTTP allocation must be a Foundry localhost port.")
    port = int(match[1])
    topology = validate_topology(topology, registered="sha256" in topology)
    metadata = parse_json(files["recovery/instance.json"]) if "recovery/instance.json" in files else None
    if metadata is not None:
        require(isinstance(metadata, dict) and metadata.get("project_id") == project["id"]
                and metadata.get("profile", {}).get("instance_id") == profile["instance_id"]
                and metadata.get("host_id") == repository["host_id"]
                and metadata.get("name") == expected["name"], "recovery_identity", "Recovery metadata belongs to another identity.")
        validate_profile(metadata["profile"], project)
        require(metadata.get("release_commit") is None or isinstance(metadata["release_commit"], str)
                and re.fullmatch(r"[a-f0-9]{7,64}", metadata["release_commit"]),
                "recovery_members", "Recovery metadata has an invalid running release commit.")
        topology = validate_topology(metadata.get("topology"), registered=True)
        allocation = metadata.get("allocations", {})
        require(all(allocation.get(key) == value for key, value in expected["allocations"].items())
                and allocation.get("http_port") == port and not {"uid", "gid"} & allocation.keys(),
                "recovery_identity", "Recovery metadata allocations differ from the preserved instance.")
        source_profile = metadata["profile"]
        require({key: value for key, value in source_profile.items() if key != "host"}
                == {key: value for key, value in profile.items() if key != "host"},
                "recovery_identity", "Use the archived instance profile when recovering this instance.")
    feature = None
    if profile.get("schema") == 3 or topology.get("schema") == 2:
        require(metadata is not None and "feature_recovery" in metadata,
                "recovery_descriptor_missing", "Feature recovery requires its archived selected deployment descriptor.")
        feature = backup.parse_feature_snapshot(metadata["feature_recovery"], metadata)
        require(metadata.get("release_commit") is not None, "recovery_descriptor_missing",
                "Feature recovery requires the archived running release identity.")
        for paths in metadata["feature_recovery"]["secret_paths"].values():
            for path in paths.values():
                require(prefix + path.removeprefix(config + "/") in files,
                        "recovery_members", "A selected service secret snapshot is missing.")
        require(all(prefix + path.removeprefix(config + "/") in files
                    for path in metadata["feature_recovery"].get("service_secret_files", [])),
                "recovery_members", "A current configuration-owner service secret snapshot is missing.")
    connections = parse_json(values.get("APP__SERVICES__DATABASE__CONNECTIONS", "{}"))
    primary = connections.get("primary", {}).get("primary", {})
    pg_port = primary.get("port")
    require(type(pg_port) is int and POSTGRES_PORT <= pg_port < POSTGRES_PORT + 100
            and primary.get("database") == expected["allocations"]["database"]
            and primary.get("user") == expected["allocations"]["database_role"],
            "recovery_identity", "Recovered database connection differs from the instance allocation.")
    return {"repository": repository, "values": values, "topology": topology, "http_port": port,
            "postgres_port": pg_port, "metadata": metadata, "prefix": prefix, "feature": feature}


def _load(host):
    path = check_path(host.paths.p(JOURNAL), "file")
    return read_json(backup._private(host, path)) if path.exists() else None


def _save(host, journal):
    backup._write_private(host, host.paths.p(JOURNAL), journal)


def _empty(host, name, *, resume_preflight=False, applying=False):
    require(host.read_registry(missing=True) is None, "recovery_not_empty", "Recovery requires an empty target without a Foundry registry.")
    allowed = set()
    if applying or resume_preflight:
        allowed.add(host.paths.p("/run/lock/foundry/host.lock"))
    if resume_preflight:
        allowed.add(packages._journal_path(host))
    roots = ("/etc/foundry", "/var/lib/foundry", "/var/log/foundry", "/opt/foundry", "/run/foundry", "/run/lock/foundry",
             "/usr/local/lib/foundry", "/usr/local/sbin/foundry-host", "/usr/local/bin/foundry-host-report")
    for root in roots:
        path = host.paths.p(root)
        if path.exists() or path.is_symlink():
            for item in [path, *path.rglob("*")] if path.is_dir() and not path.is_symlink() else [path]:
                require(not item.is_symlink() and (item in allowed or any(item in value.parents for value in allowed)),
                        "recovery_not_empty", "Target has Foundry resources; recreate an empty server.")
    for root, pattern in (("/etc/postgresql", "*/foundry"), ("/var/log/postgresql", "*foundry*"),
                          ("/usr/local/share/ca-certificates", "foundry*"),
                          ("/etc/nginx", "**/*foundry*"), ("/etc/sudoers.d", "*foundry*")):
        require(not list(host.paths.p(root).glob(pattern)), "recovery_not_empty", "Target has Foundry resources; recreate an empty server.")
    for root in ("/etc/systemd/system", "/run/systemd/system", "/run/systemd/transient", "/usr/lib/systemd/system", "/lib/systemd/system"):
        require(not list(host.paths.p(root).glob("*foundry*")), "recovery_not_empty", "Target has Foundry service resources.")
    for database in ("passwd", "group"):
        result = host.run(["getent", database])
        require(not any(line.split(":", 1)[0] == name or line.startswith("foundry-") or "foundry-host=" in line
                        for line in result.stdout.splitlines()), "recovery_not_empty", "Target has Foundry accounts or groups.")


def plan(host, project, profile, tools_dir, *, recovery_archive, topology, mode, operation_id,
         archive_sha256, target_time=None, applying=False, recovery_public_binding=None):
    validate_project(project)
    validate_profile(profile, project)
    require(mode in {"rehearsal", "takeover"}, "recovery_mode", "RECOVER_MODE must explicitly select rehearsal or takeover.")
    require(valid_uuid(operation_id) and valid_uuid(profile["host"]["host_id"]),
            "recovery_identity", "Recovery needs a stable operation and new host UUID.")
    require(isinstance(archive_sha256, str) and re.fullmatch(r"[a-f0-9]{64}", archive_sha256),
            "archive_checksum", "Recovery needs the selected release checksum.")
    target_time = backup._restore_target(target_time) if target_time is not None else None
    files = read_archive(Path(recovery_archive))
    require(all(b"AGE-SECRET-KEY-" not in value for value in files.values()),
            "recovery_archive", "Never send an age private identity to a recovery host.")
    recovered = archive_configuration(project, profile, files, topology)
    feature = recovered["feature"]
    binding = None
    roles = ()
    enabled, holds = (), ()
    if feature is not None:
        binding = replacement_binding(recovery_public_binding, recovered["metadata"], profile, operation_id, mode)
        coverage = backup.service_coverage(feature)
        unprotected = [s["role"] for s in coverage["service_state"] if s["kind"] == "local-unprotected"]
        require(mode != "takeover" or not unprotected, "service_state_unprotected",
                "Takeover requires explicit restoration evidence for local state: " + ", ".join(unprotected))
        if mode == "rehearsal":
            roles = rehearsal_roles(feature)
            enabled = roles
        else:
            enabled, holds, roles = takeover_selection(feature, recovered["metadata"]["feature_recovery"])
        _feature_owner()
    else:
        require(recovery_public_binding is None, "recovery_public_binding", "Legacy recovery has no custom public binding override.")
    require(profile["host"]["host_id"] != recovered["repository"]["host_id"],
            "recovery_identity", "Replacement host must have a new UUID.")
    major = packages.ubuntu_major(host)
    architecture = host.run(["dpkg", "--print-architecture"]).stdout.strip()
    require(architecture == profile["architecture"], "architecture_mismatch", "Recovery release architecture must match the target.")
    intent = {"project": project, "profile": profile, "topology": recovered["topology"], "mode": mode,
              "operation_id": operation_id, "archive_sha256": archive_sha256,
              "recovery_sha256": digest(Path(recovery_archive)), "target_time": target_time,
              "major": major, "bundle": upgrades.tool_bundle(tools_dir)[2]}
    if feature is not None:
        intent.update(recovery_public_binding=record_document(binding) if binding else None,
                      effective_profile=effective_profile(profile, binding), recovery_roles=list(roles),
                      recovery_enabled_roles=list(enabled), recovery_maintenance_holds=list(holds),
                      deployment_sha256=canonical_digest(recovered["metadata"]["feature_recovery"]["deployment"]))
    journal = _load(host)
    if not journal:
        require(not host.paths.p(JOURNAL).parent.exists(), "recovery_conflict", "An unclaimed recovery directory already exists.")
    if not journal or journal["phase"] == "preflight":
        listeners = provision._listeners(host)
        require(not {recovered["http_port"], recovered["postgres_port"], 55433, redis_acl.REDIS_PORT} & listeners,
                "recovery_port_conflict", "A recovered service port is already occupied on the target.")
    if journal:
        require(journal["intent"] == intent, "recovery_conflict", "Target is bound to a different recovery operation; recreate it.")
        require(journal["phase"] != "refused", "recovery_refused", journal.get("refusal", {}).get("detail", "Recovery was refused; recreate the server."))
        if journal["phase"] == "preflight":
            _empty(host, identity(project, profile)["name"], resume_preflight=True, applying=applying)
        else:
            registry = host.read_registry(missing=True)
            if registry is None:
                require(journal["phase"] == "resources", "recovery_conflict", "The recovery registry is missing; inspect the target before resuming.")
                _empty(host, identity(project, profile)["name"], resume_preflight=True, applying=applying)
            else:
                require(registry["host_id"] == profile["host"]["host_id"]
                        and (journal["phase"] == "resources" and not registry["instances"]
                             or set(registry["instances"]) == {profile["instance_id"]}
                             and registry["instances"][profile["instance_id"]].get("recovered_from", {}).get("operation_id") == operation_id),
                        "recovery_conflict", "Recovery registry ownership differs from its journal.")
    else:
        _empty(host, identity(project, profile)["name"], applying=applying)
    token = hashlib.sha256(json.dumps(intent, sort_keys=True, separators=(",", ":")).encode()).hexdigest()
    result = {"ok": True, "plan_token": token, "host_id": profile["host"]["host_id"], "mode": mode,
            "postgres_major": major, "phase": journal["phase"] if journal else "preflight",
            "steps": list(PHASES if mode == "takeover" else PHASES[:5] + ("complete",)),
            "notice": "Apply first installs pgBackRest (and required package dependencies), then verifies the repository. "
                      "If verification fails, only that package transaction and refusal journal remain; recreate the server.",
            "remaining_manual_steps": ["Point DNS at the new server."] if mode == "takeover" else [],
            "_intent": intent, "_recovered": recovered}
    if feature is not None:
        result.update(coverage=coverage, recovery_roles=list(roles),
                      recovery_enabled_roles=list(enabled), recovery_maintenance_holds=list(holds),
                      recovery_public_binding=intent["recovery_public_binding"],
                      remaining_manual_steps=["DNS instructions follow confirmed old-server stop and private takeover health."]
                      if binding else [])
    return result


def public_plan(value):
    return {key: item for key, item in value.items() if not key.startswith("_")}


def _verify_repository(host, journal, recovered):
    major = journal["intent"]["major"]
    directory = host.paths.p(JOURNAL).parent
    path = check_path(directory / "preflight.conf", "file")
    path.unlink(missing_ok=True)
    try:
        packages.install_missing(host, ["pgbackrest"])
        cluster = postgres._allocation(major, recovered["postgres_port"], "preflight", {})
        host.owned_write(path, backup._configuration(host, cluster, recovered["repository"], work=directory), mode=0o600)
        inventory = backup._info(host, config=path, inventory=True, user="root", allow_missing=True)
        backups = [item for item in inventory["backup"] if not item.get("error") and item.get("timestamp", {}).get("stop")]
        versions = ", ".join(sorted({str(item.get("version", "unknown")) for item in inventory.get("db", [])})) or "unavailable"
        require(backups, "recovery_no_backup", f"Repository PostgreSQL {versions} has no completed backup; target PostgreSQL {major}. "
                "Recreate the server on the Ubuntu release matching a repository with a completed backup (16: 24.04; 18: 26.04).")
        target = journal["intent"]["target_time"]
        if target:
            timestamp = backup.datetime.fromisoformat(target).timestamp()
            backups = [item for item in backups if item["timestamp"]["stop"] <= timestamp]
            require(backups, "recovery_target", "No completed backup precedes TARGET_TIME; recreate the server and choose a recoverable target.")
        selected = max(backups, key=lambda item: item["timestamp"]["stop"])
        databases = [item for item in inventory.get("db", [])
                     if item.get("id") == selected.get("database", {}).get("id")
                     and item.get("repo-key") == selected.get("database", {}).get("repo-key")]
        require(len(databases) == 1 and str(databases[0].get("version", "")).isdigit(),
                "recovery_repository", "Cannot prove the selected backup's PostgreSQL major; recreate the server.")
        version = int(databases[0]["version"])
        ubuntu = {16: "24.04", 18: "26.04"}.get(version, "a supported release for that major")
        require(version == major, "recovery_major_mismatch", f"Backup PostgreSQL {version} differs from target PostgreSQL {major}. "
                f"Recreate the server on Ubuntu {ubuntu} to match the backup.")
        require(re.fullmatch(backup.BACKUP_LABEL, selected.get("label", "")) is not None,
                "recovery_repository", "Repository returned an invalid backup label.")
        journal["source"] = {"label": selected["label"], "system_id": str(databases[0]["system-id"]),
                             "major": version, "size": selected.get("info", {}).get("size")}
    except DeployError as error:
        # Package journals remain resumable. A completed package transaction followed
        # by a repository refusal is terminal; it never authorizes more provisioning.
        if path.exists():
            journal.update(phase="refused", refusal={"code": error.code, "detail": error.detail})
            _save(host, journal)
        raise
    finally:
        path.unlink(missing_ok=True)


def _restore_runtime_access(host, entry, recovered):
    """Restore only declared runtime readers through the configuration ownership checks."""
    directory = host.paths.instance_config(entry["name"])
    canonical = instance_paths(entry["name"])["configuration"] + "/"
    paths = {value for key, value in recovered["values"].items() if key.endswith("_FILE") and value}
    snapshot = (recovered["metadata"] or {}).get("feature_recovery")
    if snapshot is not None:
        paths.update(snapshot.get("service_secret_files", []))
        paths.update(path for role in snapshot["secret_paths"].values() for path in role.values())
    private_names = {"secrets.json", "bootstrap-secrets.json", "backup.json", "certificate.json", "service-secrets.json"}
    require(all(path.startswith(canonical) and ".." not in Path(path).parts
                and Path(path).name not in private_names for path in paths),
            "recovery_members", "A runtime file reference cannot grant access to private configuration records.")
    group = runtime_config._instance_group(host, entry)
    for path in sorted(paths):
        target = check_path(directory / path.removeprefix(canonical), "file")
        host.owned_write(target, target.read_bytes(), mode=0o640, group=entry["name"] if group is None else group)
        _read_value_file(host, entry, str(target))
    target = check_path(directory / "app.env", "file")
    host.owned_write(target, target.read_bytes(), mode=0o640, group=entry["name"] if group is None else group)
    if snapshot is not None and (directory / "service-secrets.json").exists():
        snapshot_paths = getattr(runtime_config, "service_secret_snapshot_paths", None)
        require(callable(snapshot_paths), "capability_unavailable",
                "Feature secret recovery requires lane A runtime_config.service_secret_snapshot_paths.")
        observed = {Path(path) for path in snapshot_paths(host, entry)} - {directory / "service-secrets.json"}
        require(observed == {host.paths.p(path) for path in snapshot.get("service_secret_files", [])},
                "recovery_members", "Restored service secret inventory differs from its configuration-owner snapshot.")


def _resources(host, journal, recovered, files, tools_dir):
    intent = journal["intent"]
    project, profile = intent["project"], intent.get("effective_profile", intent["profile"])
    registry = host.read_registry(missing=True)
    if registry is None:
        registry = provision.new_registry(profile["host"]["host_id"], tools_dir)
        host.journal(registry)
    provision._ensure_roots(host)
    upgrades.install_tools(host, registry, tools_dir)
    if profile["instance_id"] not in registry["instances"]:
        entry = provision.new_entry(project, profile, recovered["topology"], recovered["http_port"], upgrades.protocol(tools_dir))
        if recovered["metadata"]:
            entry["configuration_revision"] = recovered["metadata"]["configuration_revision"]
        entry["recovered_from"] = {"host_id": recovered["repository"]["host_id"], "instance_id": profile["instance_id"],
                                   "mode": intent["mode"], "operation_id": intent["operation_id"],
                                   "release_commit": (recovered["metadata"] or {}).get("release_commit"),
                                   "backup_label": journal["source"]["label"], "target_time": intent["target_time"]}
        entry["provisioning"]["state"] = "rehearsal" if intent["mode"] == "rehearsal" else "preparing"
        registry["projects"][project["id"]] = {"slug": project["slug"], "created_at": now()}
        host.journal(registry, entry)
    entry = registry["instances"][profile["instance_id"]]
    major = intent["major"]
    required = [f"postgresql-{major}", f"postgresql-client-{major}", "postgresql-common", "redis-server", "redis-tools", "openssl", "ca-certificates"]
    if intent["mode"] == "takeover":
        required += ["nginx", "certbot", "python3-certbot-dns-cloudflare", "age", "rclone"]
        if not packages.binary_available(host, "nginx") and "nginx" not in registry["shared"]:
            registry["shared"]["nginx"] = {"owner": "foundry", "installed_by_foundry": True, "unit": "nginx.service"}
            host.journal(registry)
    packages.install_missing(host, required)
    # Restore credentials before the standard account/path owner: it must never
    # generate replacement database or Redis passwords for this instance.
    destination = host.paths.instance_config(entry["name"])
    destination.mkdir(parents=True, mode=0o750, exist_ok=True)
    host.owned_write(destination / "secrets.json", files[recovered["prefix"] + "secrets.json"], mode=0o600)
    provision._prepare_instance(host, registry, entry)
    canonical = instance_paths(entry["name"])["configuration"]
    snapshot = (recovered["metadata"] or {}).get("feature_recovery")
    for name, content in files.items():
        relative = None
        if name.startswith(recovered["prefix"]):
            relative = name.removeprefix(recovered["prefix"])
        elif name.startswith("files/" + canonical.lstrip("/") + "/"):
            relative = name.removeprefix("files/" + canonical.lstrip("/") + "/")
        if relative:
            target = check_path(destination / relative, "file")
            for parent in reversed(target.relative_to(destination).parents[:-1]):
                provision._mkdir(host, destination / parent, 0o750, "root", entry["name"])
            private = snapshot is not None or target.name in {"secrets.json", "bootstrap-secrets.json", "backup.json", "certificate.json"}
            host.owned_write(target, content, mode=0o600 if private else 0o640, group="root" if private else entry["name"])
    if snapshot is not None:
        path = host.paths.p(JOURNAL).parent / "source-deployment.json"
        if path.exists():
            require(read_json(backup._private(host, path)) == snapshot, "recovery_descriptor_changed",
                    "The immutable source descriptor differs from the recovery archive.")
        else:
            backup._write_private(host, path, snapshot)
        entry["recovery_public_binding"] = intent["recovery_public_binding"]
        entry["recovered_from"]["deployment_sha256"] = intent["deployment_sha256"]
        host.journal(registry, entry)
    if intent["mode"] == "rehearsal":
        values = dict(recovered["values"], APP__SCHEDULER__ENABLED="false")
        values.pop("APP__SCHEDULER__ENABLED_FILE", None)
        host.owned_write(destination / "app.env", dump_env(values), mode=0o640, group=entry["name"])
    _restore_runtime_access(host, entry, recovered)
    select_runtime_storage(registry, entry, recovered["values"], lambda value: _read_value_file(host, entry, str(host.paths.p(value))))
    provision._step(host, registry, entry, "bootstrap_secrets")


def _ready(host, journal):
    registry = host.read_registry()
    entry = registry["instances"][journal["intent"]["profile"]["instance_id"]]
    postgres.inspect(host, registry)
    if "deployment_sha256" in journal["intent"]:
        require(_feature_owner().recovery_ready(host, entry, journal) is True,
                "readiness_failed", "Recovered feature services have not passed private readiness and isolation checks.")
        if journal["intent"]["mode"] == "takeover":
            journal["private_takeover_healthy_at"] = now()
            _save(host, journal)
        return
    validate_runtime_environment(host, entry)
    state = host.load_state(entry)
    require((state.get("current") or {}).get("version") == journal["release"]["version"],
            "recovery_release_changed", "Recovered release differs from its journal; inspect before resuming.")
    release.verified_release(host, entry, state["current"])
    require(release.ready(host, entry, include_background=journal["intent"]["mode"] == "takeover"),
            "readiness_failed", "Recovered services are no longer ready; inspect them before resuming.")


def apply(host, project, profile, tools_dir, *, plan_token, recovery_archive, topology, mode,
          operation_id, archive, archive_sha256, target_time=None, old_server_gone=False, recovery_public_binding=None):
    require(mode != "takeover" or old_server_gone is True, "old_server_confirmation",
            "Stop the old server first: two servers archiving to one repository corrupt recovery. Confirm it is gone.")
    with host.lock("host"):
        approved = plan(host, project, profile, tools_dir, recovery_archive=recovery_archive, topology=topology,
                        mode=mode, operation_id=operation_id, archive_sha256=archive_sha256,
                        target_time=target_time, applying=True, recovery_public_binding=recovery_public_binding)
        require(isinstance(plan_token, str) and secrets.compare_digest(plan_token, approved["plan_token"]),
                "plan_changed", "Review a fresh recovery plan; its inputs have changed.")
        journal = _load(host)
        if journal is None:
            journal = {"intent": approved["_intent"], "phase": "preflight", "completed": [], "created_at": now(),
                       "cluster_token": secrets.token_hex(32), "backup_id": str(uuid.uuid4())}
            if mode == "takeover":
                journal["old_server_stopped_confirmed_at"] = now()
            _save(host, journal)
        recovered = approved["_recovered"]
        files = read_archive(Path(recovery_archive))
        if journal["phase"] == "complete":
            _ready(host, journal)
            if journal["intent"].get("recovery_public_binding") is not None:
                journal["result"].update(_dns_result(host, journal))
                _save(host, journal)
            return journal["result"]
        for phase in PHASES[:-1]:
            if phase in journal["completed"]:
                continue
            journal["phase"] = phase
            _save(host, journal)
            if phase == "preflight":
                _verify_repository(host, journal, recovered)
            elif phase == "resources":
                _resources(host, journal, recovered, files, tools_dir)
            else:
                registry = host.read_registry()
                entry = registry["instances"][profile["instance_id"]]
                if phase == "database":
                    postgres.recover_cluster(host, registry, entry, journal, recovered["repository"], recovered["postgres_port"])
                    if mode == "takeover":
                        backup._backrest(host, ["check"], timeout=300)
                elif phase == "application":
                    validate_runtime_environment(host, entry)
                    provision._step(host, registry, entry, "configuration")
                    redis_acl.prepare(host, registry)
                    redis_acl.provision(host, registry, entry, read_credentials(host, entry))
                    if recovered.get("feature") is not None:
                        _feature_owner().prepare_recovery(host, registry, entry, journal,
                                                          recovered["metadata"]["feature_recovery"],
                                                          archive, archive_sha256, tools_dir)
                    else:
                        provision._install_unit(host, registry, entry, tools_dir, http_only=mode == "rehearsal")
                    provision._step(host, registry, entry, "unit")
                    if mode == "takeover" and recovered.get("feature") is None:
                        provision._install_site(host, registry, entry, tools_dir)
                        provision._step(host, registry, entry, "site")
                elif phase == "release":
                    if recovered.get("feature") is not None:
                        journal["release"] = _feature_owner().activate_recovery(host, registry, entry, journal)
                        require(journal["release"].get("version") == recovered["feature"].deployment.release,
                                "recovery_release_changed", "Recovered feature release differs from its archived descriptor.")
                    else:
                        journal["release"] = release.activate_recovered(host, registry, entry, archive, archive_sha256,
                                                                       http_only=mode == "rehearsal")
                elif phase == "backup" and mode == "takeover":
                    _ready(host, journal)
                    journal["backup"] = backup.run_backup(host, entry, journal["backup_id"], "full")
                    provision._step(host, registry, entry, "backup")
                elif phase == "timers" and mode == "takeover":
                    _ready(host, journal)
                    backup._install_timers(host, registry)
            journal["completed"].append(phase)
            _save(host, journal)
        _ready(host, journal)
        registry = host.read_registry()
        entry = registry["instances"][profile["instance_id"]]
        entry["provisioning"]["state"] = "rehearsal" if mode == "rehearsal" else "ready"
        host.journal(registry, entry)
        result = {"ok": True, "mode": mode, "host_id": registry["host_id"], "instance": entry["name"],
                  "operation_id": operation_id, **journal["release"], "backup": journal.get("backup"),
                  "remaining_manual_steps": ["Point DNS at the new server."] if mode == "takeover" else [],
                  "profile_update": "Commit the updated local host.address and host.host_id." if mode == "takeover" else "Local profile unchanged; recreate this server to take over."}
        if recovered.get("feature") is not None:
            result["coverage"] = backup.service_coverage(recovered["feature"])
            result["host_ready"] = True
            if mode == "takeover":
                result["profile_update"] = copy.deepcopy(journal["intent"]["effective_profile"])
                result.update(_dns_result(host, journal))
        journal.update(phase="complete", result=result, completed_at=now())
        _save(host, journal)
        return result
