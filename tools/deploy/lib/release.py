"""Verified SSH release activation, instance-scoped operations and recovery.

Shared service provisioning stays in its owning modules. Releases reconcile
instance Redis grants through redis_acl and request recovery points/key archives from backup.
Configuration and key rotation share the same candidate validation and selection.
"""
from __future__ import annotations

import copy
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import re
import shutil
import sys
import tarfile
import time
import tomllib
import urllib.request
import uuid

from contracts import (DeployError, HOST_PATHS, LAYOUT_VERSION, SCHEMA, VERSION_PATTERN,
                       check_path, digest, dump_env, now,
                       parse_env, parse_json, read_json, require, valid_timestamp, valid_uuid, instance_paths,
                       reserve_runtime_storage, select_runtime_storage)
from runtime_config import (ENCRYPTION_KEYS, MAX_CONFIGURATION_CHANGES, PIPELINE_KEYS, validate_runtime_environment,
                            candidate_values, rotation_changes, validate_configuration_changes, _read_value_file)
from topology import instance_units, selected_units
from host import OperationTimeout, wall_clock_budget
from redis_acl import (granted_redis_patterns, granted_redis_channels, reconcile_redis_grants,
                       redis_grant_patterns, redis_channel_patterns)
import backup
import compatibility

MAX_ARCHIVE_BYTES = 2 * 1024**3
MAX_EXPANDED_BYTES = 8 * 1024**3
MAX_FILES = 100000


def valid_version(value):
    return isinstance(value, str) and VERSION_PATTERN.fullmatch(value) is not None


def validate_manifest(manifest, entry):
    require(isinstance(manifest, dict), "invalid_release", "Release manifest must be an object.")
    expected = {"schema": SCHEMA, "layout_version": LAYOUT_VERSION, "project_id": entry["project_id"],
                "instance_id": entry["profile"]["instance_id"], "instance": entry["name"], "environment": entry["environment"],
                "architecture": entry["profile"]["architecture"], "native_imaging": entry["profile"].get("native_imaging", False)}
    require(all(type(manifest.get(k)) is type(v) and manifest.get(k) == v for k, v in expected.items()), "release_identity", "Release identity, architecture or runtime requirements do not match this instance.")
    require(valid_version(manifest.get("version")) and manifest.get("tree_state") in ("clean", "dirty", "unknown")
            and valid_timestamp(manifest.get("built_at")), "invalid_release", "Invalid release version or provenance.")
    require(manifest.get("commit") is None or isinstance(manifest.get("commit"), str) and re.fullmatch(r"[a-f0-9]{7,64}", manifest["commit"]), "invalid_release", "Invalid source revision.")
    if entry["profile"]["runtime_environment"] == "production" or entry["environment"] == "production":
        require(manifest.get("tree_state") == "clean" and manifest.get("commit") is not None
                and manifest.get("source_kind") == "git_archive"
                and isinstance(manifest.get("source_sha256"), str)
                and re.fullmatch(r"[a-f0-9]{64}", manifest["source_sha256"]),
                "production_source", "Production requires a clean committed HEAD archive and source provenance.")
    if "topology_sha256" in manifest and "topology" in entry:
        require(manifest["topology_sha256"] == entry["topology"]["sha256"], "release_topology",
                "Release topology differs from the bootstrapped role declaration.")
    files = manifest.get("files")
    require(isinstance(files, dict) and 1 <= len(files) <= MAX_FILES, "invalid_release", "Invalid release file inventory.")
    for path, checksum in files.items():
        require(safe_member(path) == path and allowed_file(path) and isinstance(checksum, str)
                and re.fullmatch(r"[a-f0-9]{64}", checksum), "invalid_release", "Invalid release file path or checksum.")
    require({"artisan", "config/deploy.toml"}.issubset(files)
            and any(p.startswith("public/") for p in files)
            and any(p.startswith("private-source-maps/" + manifest["version"] + "/") for p in files),
            "invalid_release", "Release lacks its binary, configuration, portal assets or private source maps.")
    return manifest


def safe_member(name):
    path = PurePosixPath(name)
    require(not path.is_absolute() and ".." not in path.parts and "\\" not in name
            and not any(ord(c) < 32 for c in name), "unsafe_archive", "Archive contains an unsafe path.")
    return str(path)


def allowed_file(name):
    return name in ("artisan", "config/deploy.toml") or name.startswith(("public/", "private-source-maps/"))


def unpack_archive(archive, destination, entry, checksum):
    """Inspect every header before writing; no links, devices or extraction API."""
    check_path(archive, "file")
    require(archive.stat().st_size <= MAX_ARCHIVE_BYTES and isinstance(checksum, str)
            and re.fullmatch(r"[a-f0-9]{64}", checksum) and digest(archive) == checksum,
            "archive_checksum", "Upload is incomplete or differs from its expected checksum.")
    check_path(destination, "directory")
    require(not destination.exists(), "release_conflict", "Release staging path already exists.")
    try:
        with tarfile.open(archive, "r:gz") as bundle:
            members, names, total = [], set(), 0
            manifest = None
            for item in bundle:
                require(len(members) < MAX_FILES * 2, "unsafe_archive", "Archive has too many members.")
                name = safe_member(item.name)
                require(name not in names and (item.isfile() or item.isdir()), "unsafe_archive", "Archive has duplicate paths or unsupported entry types.")
                names.add(name)
                total += item.size
                require(total <= MAX_EXPANDED_BYTES, "unsafe_archive", "Archive expands beyond its size limit.")
                if item.isfile():
                    require(name == "manifest.json" or allowed_file(name), "unsafe_archive", "Release contains unexpected files.")
                    if name == "manifest.json":
                        require(item.size <= 4 * 1024 * 1024, "invalid_release", "Manifest exceeds size limit.")
                        manifest = parse_json(bundle.extractfile(item).read())
                else:
                    require(name in (".", "config", "public", "private-source-maps") or name.startswith(("public/", "private-source-maps/")), "unsafe_archive", "Unexpected release directory.")
                members.append((item, name))
            manifest = validate_manifest(manifest, entry)
            actual = {name for item, name in members if item.isfile() and name != "manifest.json"}
            require(actual == set(manifest["files"]), "release_inventory", "Archive files differ from the manifest.")
            # Explicit modes: the service account executes the release, and a
            # hardened root umask must not make it root-only.
            destination.mkdir(mode=0o755, parents=True)
            destination.chmod(0o755)
            for item, name in members:
                if not item.isfile():
                    continue
                target = destination / name
                check_path(target, "file")
                for parent in reversed(Path(name).parents[:-1]):
                    directory = destination / parent
                    # Source maps stay private from the first write, including failed extraction.
                    mode = 0o700 if parent.parts[0] == "private-source-maps" else 0o755
                    directory.mkdir(mode=0o700, exist_ok=True)
                    directory.chmod(mode)
                with bundle.extractfile(item) as source, target.open("xb") as output:
                    shutil.copyfileobj(source, output, length=1024 * 1024)
                target.chmod(0o755 if name == "artisan" else 0o600 if name.startswith("private-source-maps/") else 0o644)
                if name != "manifest.json":
                    require(digest(target) == manifest["files"][name], "release_checksum", "A release file differs from its manifest checksum.")
            return manifest
    except (tarfile.TarError, OSError, EOFError):
        raise DeployError("invalid_archive", "Cannot read the release archive safely.") from None


def release_path(host, entry, version):
    require(valid_version(version), "invalid_release", "Invalid release version.")
    return check_path(host.paths.opt(entry["name"]) / "releases" / version, "directory")


def verified_release(host, entry, manifest):
    validate_manifest(manifest, entry)
    path = release_path(host, entry, manifest["version"])
    require(read_json(path / "manifest.json") == manifest
            and all(digest(check_path(path / name, "file")) == checksum for name, checksum in manifest["files"].items()),
            "release_conflict", "Immutable release contents differ from their recorded checksums.")
    return path


def current_version(host, entry):
    opt = check_path(host.paths.opt(entry["name"]), "directory")
    link = opt / "current"
    if not link.exists() and not link.is_symlink():
        return None
    require(link.is_symlink(), "current_conflict", "Current release must be an owned release symlink.")
    target = os.readlink(link)
    require(target.startswith("releases/") and valid_version(target.removeprefix("releases/")), "current_conflict", "Current link points outside this instance's releases.")
    require(release_path(host, entry, target[9:]).is_dir(), "current_missing", "Current release directory is missing.")
    return target[9:]


def switch_to(host, entry, version):
    path = release_path(host, entry, version)
    compatibility.require_release(host, entry, read_json(path / "manifest.json"), selecting=True)
    _switch_pointer(host, entry, version)


def _switch_pointer(host, entry, version):
    release_path(host, entry, version)
    current_version(host, entry)  # refuse a foreign/non-symlink current
    opt = host.paths.opt(entry["name"])
    temporary = opt / (".current-" + uuid.uuid4().hex)
    try:
        temporary.symlink_to("releases/" + version)
        os.replace(temporary, opt / "current")
        fd = os.open(opt, os.O_RDONLY)
        try:
            os.fsync(fd)
        finally:
            os.close(fd)
    finally:
        if temporary.is_symlink():
            temporary.unlink()


def app(host, entry, release, command, env_file=None, extra=(), timeout=90, check=True):
    config = env_file or host.paths.instance_config(entry["name"]) / "app.env"
    check_path(config, "file")
    return host.run(["runuser", "-u", entry["name"], "--", str(release / "artisan"), command,
                     "--config", str(release / "config/deploy.toml"), "--env-file", str(config), *extra],
                    env={"WEB__PUBLIC_DIR": str(release / "public")}, timeout=timeout, check=check,
                    cwd=check_path(host.paths.data(entry["name"]), "directory"))


STORAGE_CONFIGURATION = ("APP__SERVICES__STORAGE__", "APP__SERVICES__CREDENTIALS")


def probe_storage(host, entry, release, env_file=None):
    """Prove every configured disk accepts a write, read and delete before it goes live."""
    result = app(host, entry, release, "deploy:storage-probe", env_file, timeout=300, check=False)
    if result.returncode == 2:
        raise DeployError("storage_probe_unavailable",
                          "Register deploy:storage-probe as the deployment guide shows and rebuild the release.")
    code, detail = "storage_probe_invalid", "Storage probe returned no valid report; no settings or release were switched. Run artisan deploy:storage-probe on the host to see why."
    try:
        report = parse_json(result.stdout)
    except (DeployError, TypeError):
        raise DeployError(code, detail) from None
    require(isinstance(report, dict) and set(report) == {"schema", "disks"}
            and type(report["schema"]) is int and report["schema"] == 1
            and isinstance(report["disks"], list) and len(report["disks"]) <= 64, code, detail)
    names, failures = set(), []
    for disk in report["disks"]:
        require(isinstance(disk, dict) and {"disk", "outcome"} <= set(disk)
                and set(disk) <= {"disk", "outcome", "stage", "code", "leftover_key"}, code, detail)
        name = disk["disk"]
        require(isinstance(name, str) and re.fullmatch(r"[a-z0-9][a-z0-9_.-]{0,127}", name) is not None
                and name not in names and disk["outcome"] in ("ok", "failed"), code, detail)
        names.add(name)
        if disk["outcome"] == "ok":
            require(set(disk) == {"disk", "outcome"}, code, detail)
            continue
        require(isinstance(disk.get("stage"), str) and disk["stage"] in ("write", "read", "delete")
                and isinstance(disk.get("code"), str)
                and re.fullmatch(r"storage_[a-z][a-z0-9_]{0,63}", disk["code"]) is not None, code, detail)
        leftover = disk.get("leftover_key")
        require("leftover_key" not in disk or (isinstance(leftover, str)
                and re.fullmatch(r"foundry-deploy-probe/[a-f0-9]{32}", leftover) is not None), code, detail)
        failure = f"{name}: stage {disk['stage']}, code {disk['code']}; check that disk's bucket and credentials"
        if leftover:
            failure += f"; leftover_key {leftover}"
        failures.append(failure)
    if failures:
        raise DeployError("storage_probe_failed", "Storage probe failed: " + "; ".join(failures) + ".")
    require(result.returncode == 0, code, detail)


def release_redis_inventory(host, entry, release, env_file=None):
    """One candidate invocation supplies both exact key and channel inventories."""
    inventory = parse_json(app(host, entry, release, "redis:keys", env_file).stdout)
    return {"patterns": redis_grant_patterns(entry, inventory),
            "channels": redis_channel_patterns(entry, inventory)}


def _redis_transition(host, registry, entry, inventory, state=None):
    if state is not None:
        state["last_attempt"].update(redis_target=inventory, redis_pending=True)
        host.save_state(entry, state)
    reconcile_redis_grants(host, registry, entry, inventory["patterns"], channels=inventory["channels"])
    if state is not None:
        state["last_attempt"]["redis_pending"] = False
        host.save_state(entry, state)


def resume_redis_transition(host, registry, entry, state):
    attempt = state.get("last_attempt") or {}
    if attempt.get("redis_pending"):
        _redis_transition(host, registry, entry, attempt["redis_target"], state)


def widen_redis_grants(host, registry, entry, inventory, state=None):
    target = {"patterns": sorted(set(granted_redis_patterns(entry)) | set(inventory["patterns"])),
              "channels": sorted(set(granted_redis_channels(entry)) | set(inventory["channels"]))}
    _redis_transition(host, registry, entry, target, state)


def narrow_redis_grants(host, registry, entry, inventory, state=None):
    try:
        _redis_transition(host, registry, entry, inventory, state)
        return []
    except (DeployError, OSError, KeyError, TypeError, ValueError):
        return ["redis_grants_not_narrowed"]


def queue_redis_narrowing(state, inventory):
    state["last_attempt"].update(redis_target=copy.deepcopy(inventory), redis_pending=True)


def cleanup_configuration_candidate(host, entry, state):
    attempt = state.get("last_attempt") or {}
    if attempt.get("action") != "config":
        return False
    candidate = check_path(host.paths.instance_config(entry["name"]) / "app.env.next", "file")
    if not candidate.exists():
        return False
    require(digest(candidate) == attempt["configuration_sha256"], "configuration_reconciliation",
            "Configuration candidate differs from its recorded intent.")
    candidate.unlink()
    directory = os.open(candidate.parent, os.O_RDONLY)
    try:
        os.fsync(directory)
    finally:
        os.close(directory)
    return True


def ensure_key_archive(host, entry, state):
    obligation = state.get("key_archive") or {}
    if not obligation.get("pending"):
        return False
    revision = obligation["configuration_revision"]
    attempt = read_configuration_intent(host, entry, obligation["attempt_id"], revision)
    selected = digest(host.paths.instance_config(entry["name"]) / "app.env")
    if selected == attempt["previous_configuration_sha256"] and entry["configuration_revision"] < revision:
        state.pop("key_archive")  # The candidate was never selected.
        host.save_state(entry, state)
        return True
    require(selected == attempt["configuration_sha256"] and entry["configuration_revision"] == revision,
            "configuration_reconciliation", "Key archival requires the recorded configuration revision.")
    try:
        backup.archive_keys(host, entry)
    finally:
        state["backup"] = copy.deepcopy(host.load_state(entry).get("backup", entry.get("backup", {})))
    obligation.update(pending=False, archived_at=now())
    host.save_state(entry, state)
    return True


def record_background_recovery(host, entry, state):
    prior = {}
    selected = state["last_attempt"].get("roles")
    if selected is None:
        selected = enabled_roles(host, entry)
    for role, unit in selected_units(entry, roles=selected).items():
        if role == "http":
            continue
        observed = service(host, unit)["ActiveState"]
        require(observed in ("active", "inactive", "failed"), "service_inspection",
                "Background role state is not stable; retry after it settles.")
        prior[role] = observed
    state["last_attempt"].update(prior_background=prior, role_recovery_pending=True)
    host.save_state(entry, state)


def resume_background_recovery(host, entry, state):
    attempt = state.get("last_attempt") or {}
    if not attempt.get("role_recovery_pending"):
        return False
    compatibility.require_current(host, entry)
    ensure_key_archive(host, entry, state)
    units = instance_units(entry)
    for role, prior in attempt["prior_background"].items():
        if prior == "active":
            host.run(["systemctl", "start", units[role]], timeout=60)
            require(service(host, units[role])["ActiveState"] == "active", "role_recovery",
                    "A previously running background role has not recovered.")
    attempt["role_recovery_pending"] = False
    host.save_state(entry, state)
    return True


def resume_terminal_obligations(host, registry, entry, state):
    require_idle(state)
    attempt = state.get("last_attempt") or {}
    if attempt.get("rotation_operation_id"):
        save_rotation_outcome(host, entry, attempt)
    changed = cleanup_configuration_candidate(host, entry, state)
    changed = ensure_key_archive(host, entry, state) or changed
    changed = resume_background_recovery(host, entry, state) or changed
    pending = bool(attempt.get("redis_pending"))
    resume_redis_transition(host, registry, entry, state)
    return changed or pending


def migration_status(host, entry, release):
    report = parse_json(app(host, entry, release, "migrate:status", extra=("--database", "primary")).stdout)
    require(isinstance(report, dict) and isinstance(report.get("targets"), list) and report["targets"], "migration_status", "Migration status did not return declared database targets.")
    pending = False
    for target in report["targets"]:
        require(isinstance(target, dict) and isinstance(target.get("migrations"), list) and not target.get("problems"), "migration_reconciliation", "Migration history needs reconciliation; inspect artisan migrate:status on the host.")
        for migration in target["migrations"]:
            require(isinstance(migration, dict) and migration.get("state") in ("pending", "applied"), "migration_reconciliation", "Migration history is incomplete or changed; administrator reconciliation required.")
            pending |= migration["state"] == "pending"
    return pending


def backup_database(host, entry, attempt_id):
    """Require a durable PITR recovery point before running migrations."""
    return backup.recovery_point(host, entry, attempt_id)


READINESS_SECONDS = 90
STABILITY_SECONDS = 5


def stop_background(host, entry, timeout=100, *, roles=None):
    units = selected_units(entry, roles=roles)
    background = [units[role] for role in ("scheduler", "worker") if role in units]
    if background:
        host.run(["systemctl", "stop", *background], timeout=timeout)


def _remaining(deadline, maximum=5):
    available = deadline - time.monotonic()
    if available <= 0:
        raise OperationTimeout("readiness budget exhausted")
    return min(maximum, available)


def worker_connection(host, entry, candidate):
    """Validate the tool's worker selection against a specific release."""
    if "worker" not in instance_units(entry):
        return
    configuration = host.paths.instance_config(entry["name"])
    values = parse_env(check_path(configuration / "app.env", "file").read_text())
    key = "APP__WORKER__CONNECTION"
    if key + "_FILE" in values:
        from runtime_config import _read_value_file
        connection = _read_value_file(host, entry, values[key + "_FILE"])
    elif key in values:
        connection = values[key]
    else:
        with (candidate / "config/deploy.toml").open("rb") as source:
            settings = tomllib.load(source)
        connection = settings.get("app", {}).get("worker", {}).get("connection")
    require(isinstance(connection, str) and re.fullmatch(r"[a-z0-9][a-z0-9_.-]{0,127}", connection),
            "worker_configuration", "Declare app.worker.connection explicitly for the worker role.")
    return connection


def prepare_worker(host, entry, candidate=None):
    if "worker" not in instance_units(entry):
        return
    if candidate is None:
        version = current_version(host, entry)
        require(version is not None, "worker_configuration", "A selected release is required for the worker.")
        candidate = release_path(host, entry, version)
    connection = worker_connection(host, entry, candidate)
    configuration = host.paths.instance_config(entry["name"])
    host.owned_write(configuration / "worker.env", dump_env({"FOUNDRY_WORKER_CONNECTION": connection}),
                     mode=0o640, group=entry["name"])


def enabled_roles(host, entry, timeout=5):
    """Read the systemd enablement that owns which declared roles should run."""
    result = []
    for role, unit in instance_units(entry).items():
        observed = host.run(["systemctl", "is-enabled", unit], check=False, timeout=timeout)
        status = observed.stdout.strip()
        require(status in {"enabled", "enabled-runtime", "disabled"},
                "service_enablement", "Cannot establish declared role enablement.")
        if status in {"enabled", "enabled-runtime"}:
            require(observed.returncode == 0, "service_enablement", "Cannot establish declared role enablement.")
            result.append(role)
    return result


def role_status(entry, roles, complete="healthy"):
    if not roles:
        return "stopped"
    return complete if set(roles) == set(instance_units(entry)) else "degraded"


def restart(host, entry, state=None, *, role="all", roles=None):
    """Explicit operator restarts enable; recorded role sets preserve enablement."""
    ensure_key_archive(host, entry, state if state is not None else host.load_state(entry))
    start_roles(host, entry, role=role, roles=roles, enable=roles is None)


def _prepare_units(host, units, deadline, enable):
    if enable:
        host.run(["systemctl", "enable", *units.values()], timeout=_remaining(deadline, 10))
    # systemd exits 1 for a unit it hasn't loaded, as on a first deploy.
    host.run(["systemctl", "reset-failed", *units.values()], timeout=_remaining(deadline, 10), check=False)


def start_roles(host, entry, *, http_only=False, role="all", roles=None, enable=True):
    """Start selected roles under one deadline, with HTTP ready before backgrounds."""
    compatibility.require_current(host, entry)
    units = selected_units(entry, role, roles=roles)
    if http_only:
        units = {"http": units["http"]}
    if not units:
        return
    if "worker" in units:
        prepare_worker(host, entry)
    if role != "all":
        try:
            with wall_clock_budget(READINESS_SECONDS):
                deadline = time.monotonic() + READINESS_SECONDS
                _prepare_units(host, units, deadline, enable)
                host.run(["systemctl", "restart", *units.values()], timeout=_remaining(deadline, 60))
                require(ready(host, entry, timeout=_remaining(deadline, READINESS_SECONDS), role=role),
                        "readiness_failed", "Selected role did not become ready within the activation deadline.")
        except OperationTimeout:
            raise DeployError("readiness_failed", "Selected role exhausted the activation deadline.") from None
        return
    stop_background(host, entry, roles=list(units))
    try:
        with wall_clock_budget(READINESS_SECONDS):
            deadline = time.monotonic() + READINESS_SECONDS
            _prepare_units(host, units, deadline, enable)
            if "http" in units:
                host.run(["systemctl", "restart", units["http"]], timeout=_remaining(deadline, 60))
                require(ready(host, entry, timeout=_remaining(deadline, READINESS_SECONDS), include_background=False),
                        "readiness_failed", "HTTP did not become ready; selected background roles remain stopped.")
            for name in ("worker", "scheduler"):
                if name in units:
                    host.run(["systemctl", "start", units[name]], timeout=_remaining(deadline, 60))
            require(ready(host, entry, timeout=_remaining(deadline, READINESS_SECONDS),
                          include_background=not http_only, roles=list(units)),
                    "readiness_failed", "Required roles did not become ready within the activation deadline.")
    except OperationTimeout:
        raise DeployError("readiness_failed", "Required roles exhausted the activation deadline.") from None


def stop(host, entry, *, role="all", roles=None, disable=False):
    units = selected_units(entry, role, roles=roles)
    if not units:
        return
    if disable:
        host.run(["systemctl", "disable", *units.values()], timeout=60)
    if role != "all":
        host.run(["systemctl", "stop", *units.values()], timeout=100)
        return
    stop_background(host, entry, roles=list(units))
    if "http" in units:
        host.run(["systemctl", "stop", units["http"]], timeout=60)


def _process(host, pid):
    """Read the kernel executable and reject a PID reused during inspection."""
    if not isinstance(pid, str) or not pid.isdigit() or int(pid) <= 0:
        return None
    proc = host.paths.p(f"/proc/{pid}")
    try:
        before = (proc / "stat").read_text().rsplit(") ", 1)[1].split()[19]
        exe = os.readlink(proc / "exe")
        info = (proc / "exe").stat()
        groups = (proc / "cgroup").read_text().splitlines()
        after = (proc / "stat").read_text().rsplit(") ", 1)[1].split()[19]
        if before != after or exe.endswith(" (deleted)") or not Path(exe).is_absolute():
            return None
        return {"exe": exe, "start": before, "device": info.st_dev, "inode": info.st_ino,
                "groups": [line.split(":", 2)[2] for line in groups]}
    except (OSError, IndexError, ValueError):
        return None


def service(host, unit, timeout=5):
    found = host.run(["systemctl", "show", unit,
                      "--property=LoadState,ActiveState,SubState,MainPID,NRestarts,ControlGroup", "--no-pager"],
                     check=False, timeout=timeout)
    if found.returncode:
        raise DeployError("service_inspection", "Required service could not be inspected.")
    values = dict(line.split("=", 1) for line in found.stdout.splitlines() if "=" in line)
    result = {"unit": unit, **{key: values.get(key) for key in
              ("LoadState", "ActiveState", "SubState", "MainPID", "NRestarts", "ControlGroup")}}
    process = _process(host, result["MainPID"])
    result.update(exe=process["exe"] if process else None, process=process)
    return result


def _matches_executable(observation, executable):
    process = observation.get("process")
    if not process or observation["LoadState"] != "loaded" or observation["ActiveState"] != "active":
        return False
    try:
        expected = executable.stat()
        return (process["exe"] == str(executable.resolve(strict=True))
                and (process["device"], process["inode"]) == (expected.st_dev, expected.st_ino)
                and observation["ControlGroup"] in process["groups"])
    except OSError:
        return False


def services(host, entry):
    result = {}
    selected = current_version(host, entry)
    executable = release_path(host, entry, selected) / "artisan" if selected else None
    for role, unit in instance_units(entry).items():
        try:
            observed = service(host, unit)
            observed["executable_matches"] = bool(executable and _matches_executable(observed, executable))
            observed.pop("process", None)
            result[role] = observed
        except (DeployError, OSError):
            result[role] = {"unit": unit, "LoadState": None, "ActiveState": "unknown", "SubState": None,
                            "MainPID": None, "NRestarts": None, "exe": None, "executable_matches": False}
    return result


def ready(host, entry, timeout=90, include_background=True, *, role="all", roles=None):
    try:
        with wall_clock_budget(timeout):
            return _ready(host, entry, time.monotonic() + timeout, include_background, role, roles)
    except (OperationTimeout, DeployError, OSError, ValueError):
        return False


def _ready(host, entry, deadline, include_background=True, role="all", roles=None):
    version = current_version(host, entry)
    if version is None:
        return False
    path = release_path(host, entry, version)
    verified_release(host, entry, read_json(path / "manifest.json"))
    units = selected_units(entry, role, roles=roles)
    if not units:
        return True
    if not include_background:
        units = {"http": units["http"]}
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
    tokens, stable_since = None, None
    while time.monotonic() < deadline:
        live = {role: service(host, unit, timeout=_remaining(deadline)) for role, unit in units.items()}
        if any(item["ActiveState"] == "failed" or item["NRestarts"] != "0" for item in live.values()):
            return False
        healthy = all(_matches_executable(item, path / "artisan") for item in live.values())
        if healthy and "http" in units:
            try:
                with opener.open(f"http://127.0.0.1:{entry['allocations']['http_port']}/health/ready", timeout=_remaining(deadline, 3)) as response:
                    payload = response.read(4097)
                    body = parse_json(payload) if len(payload) <= 4096 else None
                    healthy = response.status == 200 and isinstance(body, dict) and set(body) == {"ready"} and body["ready"] is True
                listeners = host.run(["ss", "-Hltpn", f"sport = :{entry['allocations']['http_port']}"],
                                     check=False, timeout=_remaining(deadline))
                healthy = (healthy and listeners.returncode == 0
                           and any(f"pid={live['http']['MainPID']}," in line
                                   and f"127.0.0.1:{entry['allocations']['http_port']}" in line
                                   for line in listeners.stdout.splitlines()))
            except (OSError, ValueError):
                healthy = False
        observed = tuple((role, item["MainPID"], (item.get("process") or {}).get("start"), item["NRestarts"])
                         for role, item in live.items())
        if tokens is not None and observed != tokens:
            return False
        if healthy:
            if stable_since is None:
                tokens, stable_since = observed, time.monotonic()
            if time.monotonic() - stable_since >= STABILITY_SECONDS:
                return True
        else:
            stable_since = None
        time.sleep(_remaining(deadline, 0.1))
    return False


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def native_runtime(host, entry):
    if not entry["profile"].get("native_imaging", False):
        return
    code = "import ctypes,ctypes.util; v=ctypes.CDLL(ctypes.util.find_library('vips') or 'libvips.so.42'); print(str(v.vips_version(0))+'.'+str(v.vips_version(1)))"
    result = host.run(["python3", "-c", code], check=False)
    parts = result.stdout.strip().split(".")
    require(result.returncode == 0 and len(parts) == 2 and all(p.isdigit() for p in parts)
            and int(parts[0]) == 8 and int(parts[1]) >= 18, "native_runtime_missing", "Native imaging requires a compatible prebuilt libvips 8.18+ ABI 42 runtime; the server will not build it.")


def require_idle(state):
    compatibility.require_mutation_idle(state)
    previous = state.get("last_attempt")
    require(not previous or previous.get("outcome") not in ("running", "needs_reconciliation"), "reconciliation_required", "An operation was interrupted; inspect status and reconcile before retrying.")


def begin(host, entry, state, action, version, metadata=None, *, persist=True):
    require_idle(state)
    attempt = {"id": time.strftime("%Y%m%dT%H%M%SZ", time.gmtime()) + "-" + uuid.uuid4().hex[:12],
               "action": action, "release": version, "started_at": now(), "finished_at": None,
               "outcome": "running", "phase": "validation", "error": None}
    if metadata:
        attempt.update(metadata)
    previous = state.get("last_attempt") or {}
    if previous.get("redis_pending"):
        attempt.update(redis_pending=True, redis_target=copy.deepcopy(previous["redis_target"]))
    state["last_attempt"] = attempt
    state["status"] = "deploying"
    if persist:
        host.save_state(entry, state)
    return attempt


def phase(host, entry, state, name):
    state["last_attempt"]["phase"] = name
    host.save_state(entry, state)


def finish(host, entry, state, outcome, error=None):
    state["last_attempt"].update(outcome=outcome, finished_at=now(), error=error)
    state["history"] = [*state["history"], copy.deepcopy(state["last_attempt"])][-50:]
    host.save_state(entry, state)
    attempt = state["last_attempt"]
    if attempt.get("operation_id"):
        _save_operation(host, entry, attempt)
    if attempt.get("rotation_operation_id"):
        save_rotation_outcome(host, entry, attempt)


def configuration_intent_path(host, entry, attempt_id):
    require(valid_version(attempt_id), "configuration_reconciliation", "Invalid configuration intent identity.")
    parent = host.paths.p(instance_paths(entry["name"])["private"])
    host.check_control_directory(parent)
    return check_path(parent / ("configuration-" + attempt_id + ".json"), "file")


def persist_configuration_intents(host, entry, state):
    """Keep secret-derived configuration hashes outside public state."""
    for attempt in [state.get("last_attempt"), *state.get("history", [])]:
        if not attempt or attempt.get("action") != "config":
            continue
        require(not attempt.get("configuration_redacted"), "configuration_reconciliation",
                "Private configuration intent is required before writing operation state.")
        path = configuration_intent_path(host, entry, attempt["id"])
        path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
        from contracts import atomic_json
        atomic_json(path, {key: attempt[key] for key in ("id", "configuration_revision", "configuration_sha256", "previous_configuration_sha256")}, mode=0o600)


def read_configuration_intent(host, entry, attempt_id, revision):
    path = configuration_intent_path(host, entry, attempt_id)
    require(path.exists() and path.stat().st_mode & 0o777 == 0o600
            and (host.paths.root != Path("/") or path.stat().st_uid == 0),
            "configuration_reconciliation", "Restore the private configuration intent before operating this instance.")
    intent = read_json(path)
    require(intent.get("id") == attempt_id and intent.get("configuration_revision") == revision,
            "configuration_reconciliation", "Private configuration intent identity differs.")
    return intent


def load_configuration_intents(host, entry, state):
    for attempt in [state.get("last_attempt"), *state.get("history", [])]:
        if not attempt or not attempt.get("configuration_redacted"):
            continue
        intent = read_configuration_intent(host, entry, attempt["id"], attempt["configuration_revision"])
        attempt.update({key: intent[key] for key in ("configuration_sha256", "previous_configuration_sha256")})
        attempt.pop("configuration_redacted")
    return state


def public_state(state):
    value = copy.deepcopy(state)
    for item in [value.get("last_attempt"), *value.get("history", [])]:
        if item:
            if item.get("action") == "config":
                item.pop("configuration_sha256", None)
                item.pop("previous_configuration_sha256", None)
                item["configuration_redacted"] = True
            if item.get("result") == "failed_applied":
                item["outcome"] = "failed_applied"
    return value


def _operation_path(host, entry, operation_id):
    require(valid_uuid(operation_id), "invalid_operation", "An operation UUIDv4 is required.")
    parent = host.paths.p(instance_paths(entry["name"])["private"])
    host.check_control_directory(parent)
    path = check_path(parent / ("operation-" + operation_id + ".json"), "file")
    if path.exists():
        info = path.stat()
        require(info.st_mode & 0o777 == 0o600 and (info.st_uid == 0 or host.paths.root != Path("/")),
                "operation_ownership", "Operation receipts must be root-owned and private.")
    return path


def _save_operation(host, entry, attempt):
    path = _operation_path(host, entry, attempt["operation_id"])
    path.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    host.owned_write(path,
                     json.dumps(attempt, sort_keys=True) + "\n", mode=0o600)


def check_live_state(host, entry, state):
    current = current_version(host, entry)
    require(current == (state.get("current") or {}).get("version"), "state_drift", "Recorded state differs from the live release; reconcile before mutation.")


def restore_code(host, entry, state, current, previous, recovered_status, *, roles=None):
    """Restore verified code only; never reverse database work."""
    compatibility.require_release(host, entry, current)
    verified_release(host, entry, current)
    if roles is not None and state["last_attempt"]["action"] == "deploy":
        stop(host, entry, roles=[name for name in instance_units(entry) if name not in roles])
    if current_version(host, entry) != current["version"]:
        switch_to(host, entry, current["version"])
    state["current"], state["previous"] = current, previous
    restart(host, entry, state, roles=roles)
    state["status"] = role_status(entry, roles, recovered_status) if roles is not None else recovered_status


def service_script(host):
    # Recovery runs from a root-only staged bundle. The service account reads the installed copy of the same bundle.
    return str(host.paths.p(HOST_PATHS["current"]) / "lib/release.py")


def protect_maps(host, entry, release):
    source = release / "private-source-maps"
    host.run(["chown", "-R", "root:" + entry["name"], str(source)])
    for path in [source, *source.rglob("*")]:
        path.chmod(0o750 if path.is_dir() else 0o640)
    # The application can mutate its data directory. Never copy/chown there as
    # root: a concurrent symlink swap must not cross the service UID boundary.
    host.run(["runuser", "-u", entry["name"], "--", "python3", service_script(host),
              "copy-runtime-maps", str(source), str(host.paths.data(entry["name"]) / "private-source-maps")])


def copy_runtime_maps(source, destination):
    source = check_path(source, "directory")
    destination = check_path(destination, "directory")
    destination.mkdir(mode=0o700, exist_ok=True)
    destination.chmod(0o700)
    for maps in source.iterdir():
        target = check_path(destination / maps.name, "directory")
        if not target.exists():
            shutil.copytree(maps, target)
            for path in [target, *target.rglob("*")]:
                path.chmod(0o700 if path.is_dir() else 0o600)


def prune_runtime_maps(destination, protected):
    """Bound copied maps using release retention, always as the service account."""
    require(all(valid_version(version) for version in protected), "invalid_release", "Invalid retained source-map version.")
    destination = check_path(destination, "directory")
    if not destination.exists():
        return
    for path in destination.iterdir():
        if valid_version(path.name) and path.name not in protected and path.is_dir() and not path.is_symlink():
            shutil.rmtree(check_path(path, "directory"))


def prune(host, entry, state):
    keep = entry["profile"]["retention"]
    protected = {(state.get(k) or {}).get("version") for k in ("current", "previous")}
    protected.add((state.get("last_attempt") or {}).get("release"))
    if state.get("cutover"):
        protected.update(state["cutover"].get(key) for key in ("source", "target"))
    import cutover_contracts as contract
    private = host.paths.p(instance_paths(entry["name"])["private"])
    for path in private.glob("cutover-plan-*.json"):
        plan = read_json(contract.private_path(host, entry, "plan", path.stem.removeprefix("cutover-plan-")))
        protected.add(plan.get("target", {}).get("version"))
    releases = host.paths.opt(entry["name"]) / "releases"
    candidates = sorted([p for p in releases.iterdir() if valid_version(p.name) and p.is_dir() and not p.is_symlink()], reverse=True)
    protected.update(p.name for p in candidates[:keep["server_releases"]])
    for path in candidates:
        if path.name not in protected:
            validate_manifest(read_json(path / "manifest.json"), entry)
            shutil.rmtree(check_path(path, "directory"))
    host.run(["runuser", "-u", entry["name"], "--", "python3", service_script(host),
              "prune-runtime-maps", str(host.paths.data(entry["name"]) / "private-source-maps"),
              *sorted(version for version in protected if version is not None)])
    for path in releases.glob(".stage-*"):
        if re.fullmatch(r"\.stage-[a-f0-9]{32}", path.name) and path.is_dir() and not path.is_symlink():
            shutil.rmtree(check_path(path, "directory"))


def publish_release(host, entry, staging, manifest):
    release = release_path(host, entry, manifest["version"])
    if release.exists():
        verified_release(host, entry, manifest)
    else:
        os.rename(staging, release)
    return release


def require_recovered_schema(host, entry, release):
    """The release's own status detects pending and unknown applied definitions."""
    try:
        require(not migration_status(host, entry, release), "recovery_schema_mismatch", "Pending migrations.")
    except DeployError:
        commit = entry.get("recovered_from", {}).get("release_commit")
        guidance = (f"Recover with the running release from commit {commit}." if commit else
                    "The running release commit is unknown in this archive; select the release that was running at the recovery target.")
        raise DeployError("recovery_schema_mismatch",
                          "Cannot prove an exact migration match with the restored database: pending, unknown applied, "
                          "changed or unreadable migration history is refused. Recovery runs no migrations or seeds. " + guidance) from None


def activate_recovered(host, registry, entry, archive, checksum, *, http_only=False):
    """Select recovered data's release without migrations, seeds or repository writes."""
    validate_runtime_environment(host, entry)
    staging = host.paths.opt(entry["name"]) / "releases" / (".stage-" + uuid.uuid4().hex)
    try:
        manifest = unpack_archive(Path(archive), staging, entry, checksum)
        release = publish_release(host, entry, staging, manifest)
        native_runtime(host, entry)
        protect_maps(host, entry, release)
        app(host, entry, release, "config:check")
        require_recovered_schema(host, entry, release)
        inventory = release_redis_inventory(host, entry, release)
        widen_redis_grants(host, registry, entry, inventory)
        switch_to(host, entry, manifest["version"])
        if not ready(host, entry, timeout=10, include_background=not http_only):
            start_roles(host, entry, http_only=http_only)
        state = host.load_state(entry)
        state.update(current=manifest, previous=None, status="healthy")
        host.save_state(entry, state)
        warnings = narrow_redis_grants(host, registry, entry, inventory)
        require(not warnings, "recovery_redis_pending", "Redis grant reconciliation must complete before recovery finishes.")
        return {"version": manifest["version"]}
    finally:
        if staging.exists():
            shutil.rmtree(staging)


def activate(host, project, profile, archive, checksum, operation_id=None):
    require(operation_id is None or valid_uuid(operation_id), "invalid_operation", "Deployment operation ID must be a UUIDv4.")
    _, entry = host.require_identity(project, profile)
    require(entry["provisioning"]["state"] == "ready", "bootstrap_incomplete", "Bootstrap must finish before deployment.")
    # The host lock covers the shared Redis ACL file and registry grant record,
    # in bootstrap's host -> instance order.
    with host.lock("host"), host.lock(entry["profile"]["instance_id"]):
        registry, entry = host.require_identity(project, profile)
        validate_runtime_environment(host, entry)
        state = host.load_state(entry)
        require_idle(state)
        check_live_state(host, entry, state)
        resume_terminal_obligations(host, registry, entry, state)
        import certificates
        certificates.retry_deferred(host, registry, entry)
        staging = host.paths.opt(entry["name"]) / "releases" / (".stage-" + uuid.uuid4().hex)
        try:
            manifest = unpack_archive(Path(archive), staging, entry, checksum)
            version = manifest["version"]
            prior_roles = enabled_roles(host, entry)
            if state["current"] and state["current"]["version"] == version:
                require(state["current"] == manifest, "release_conflict", "An existing release ID has different content.")
                if state["status"] in ("healthy", "rolled_back") and set(prior_roles) == set(instance_units(entry)):
                    compatibility.require_release(host, entry, state["current"])
                    verified_release(host, entry, state["current"])
                    require(ready(host, entry, timeout=10), "readiness_failed", "Current release is no longer ready; inspect it before retrying.")
                    return {"ok": True, "unchanged": True, "instance": entry["name"], "version": version}
            release = publish_release(host, entry, staging, manifest)
            attempt = begin(host, entry, state, "deploy", version,
                            metadata={"recovery_operation_id": operation_id or str(uuid.uuid4()), "roles": prior_roles})
            prior_current, prior_previous = state["current"], state["previous"]
            switched = False
            enabling_roles = False
            try:
                native_runtime(host, entry)
                protect_maps(host, entry, release)
                app(host, entry, release, "config:check")
                if entry.get("storage_bindings"):
                    probe_storage(host, entry, release)
                redis_patterns = release_redis_inventory(host, entry, release)
                attempt["redis_inventory"] = redis_patterns
                phase(host, entry, state, "redis")
                widen_redis_grants(host, registry, entry, redis_patterns, state)
                admit_normal_release(host, registry, entry, state, release, manifest)
                compatibility.require_release(host, entry, manifest, selecting=True)
                record_background_recovery(host, entry, state)
                stop_background(host, entry)
                if migration_status(host, entry, release):
                    phase(host, entry, state, "backup")
                    try:
                        attempt["backup"] = backup_database(host, entry, attempt["recovery_operation_id"])
                    finally:
                        state["backup"] = copy.deepcopy(host.load_state(entry).get("backup", entry.get("backup", {})))
                    attempt["role_recovery_pending"] = False
                    phase(host, entry, state, "migration")
                    app(host, entry, release, "migrate", extra=("--database", "primary"), timeout=900)
                attempt["role_recovery_pending"] = False
                phase(host, entry, state, "seed")
                app(host, entry, release, "db:seed", extra=("--database", "primary"), timeout=900)
                phase(host, entry, state, "switch")
                switch_to(host, entry, version)
                switched = True
                phase(host, entry, state, "health")
                restart(host, entry, state, roles=list(instance_units(entry)))
                enabling_roles = True
                host.run(["systemctl", "enable", *instance_units(entry).values()])
                if (state["current"] or {}).get("version") != version:
                    state["previous"] = state["current"]
                state["current"] = manifest
                state["status"] = "healthy"
                queue_redis_narrowing(state, redis_patterns)
                finish(host, entry, state, "succeeded")
            except (DeployError, OSError) as error:
                # An error during a migration may have committed work. It always
                # requires a fresh status inspection, never an automatic retry.
                failure_phase = state["last_attempt"]["phase"]
                code = error.code if isinstance(error, DeployError) else "filesystem_failed"
                if enabling_roles and set(prior_roles) != set(instance_units(entry)):
                    # Enable may have partially succeeded. Internal recovery cannot disable units.
                    if (state["current"] or {}).get("version") != version:
                        state["previous"] = state["current"]
                    state["current"] = manifest
                    state["status"] = "unknown"
                    finish(host, entry, state, "needs_reconciliation", "role_enablement_pending")
                    raise DeployError("role_enablement_pending", "The release passed readiness but role enablement or completion is uncertain; reconcile this deploy before another change.") from None
                state["status"] = role_status(entry, prior_roles, "failed") if prior_current else "failed"
                try:
                    if failure_phase in ("switch", "health"):
                        # Rename may succeed before fsync or recording its
                        # return fails. Inspect the actual link before recovery.
                        switched = current_version(host, entry) == version
                    if switched and prior_current:
                        restore_code(host, entry, state, prior_current, prior_previous, "rolled_back", roles=prior_roles)
                    elif switched:
                        stop(host, entry, roles=[name for name in instance_units(entry) if name not in prior_roles])
                        state["current"] = manifest  # first release has no previous code
                except (DeployError, OSError):
                    state["status"] = "unknown"
                    finish(host, entry, state, "needs_reconciliation", "code_recovery_failed")
                    raise DeployError("code_recovery_failed", "Code recovery could not be verified; inspect and reconcile the instance before retrying.") from None
                try:
                    resume_background_recovery(host, entry, state)
                except (DeployError, OSError):
                    pass  # The persisted obligation remains visible and retryable.
                finish(host, entry, state, "needs_reconciliation" if failure_phase in ("migration", "seed") else "failed", code)
                try:
                    prune(host, entry, state)
                except (DeployError, OSError):
                    pass  # retained diagnostics are safer than masking failure
                # A probe report holds no secrets, and naming the bad disk is what the operator needs.
                raise DeployError(code, error.detail if code.startswith("storage_probe_") else
                                  "Deployment failed; status records the phase and recovery outcome. Inspect the instance before retrying.") from None
            result = {"ok": True, "instance": entry["name"], "version": version}
            warnings = narrow_redis_grants(host, registry, entry, redis_patterns, state)
            try:
                prune(host, entry, state)
            except (DeployError, OSError):
                warnings.append("retention_incomplete")
            if warnings:
                result["warnings"] = warnings
            return result
        finally:
            if staging.exists():
                shutil.rmtree(staging)


def rollback(host, project, profile, target, expected_current, operation_id):
    require(valid_version(target) and valid_version(expected_current) and valid_uuid(operation_id),
            "invalid_rollback", "Rollback requires explicit target, expected current and an operation UUIDv4.")
    _, entry = host.require_identity(project, profile)
    with host.lock("host"), host.lock(entry["profile"]["instance_id"]):
        registry, entry = host.require_identity(project, profile)
        state = host.load_state(entry)
        compatibility.require_mutation_idle(state)
        target_manifest = read_json(release_path(host, entry, target) / "manifest.json")
        validate_manifest(target_manifest, entry)
        compatibility.require_release(host, entry, target_manifest, selecting=True)
        receipt = _operation_path(host, entry, operation_id)
        recorded = next((item for item in [state.get("last_attempt"), *reversed(state["history"])]
                         if item and item.get("operation_id") == operation_id), None)
        durable = read_json(receipt) if receipt.exists() else None
        prior = recorded if recorded and recorded["outcome"] not in ("running", "needs_reconciliation") else durable or recorded
        if prior:
            require(prior.get("action") == "rollback" and prior.get("release") == target
                    and prior.get("expected_current") == expected_current,
                    "operation_conflict", "Operation ID already belongs to a different request.")
            if prior["outcome"] in ("succeeded", "reconciled"):
                resume_terminal_obligations(host, registry, entry, state)
                if durable != prior:
                    _save_operation(host, entry, prior)
                return {"ok": True, "unchanged": True, "version": target, "operation_id": operation_id}
            require(prior["outcome"] not in ("running", "needs_reconciliation"),
                    "reconciliation_required", "Reconcile the interrupted rollback before retrying.")
            raise DeployError(prior.get("error") or "rollback_failed", "This rollback completed unsuccessfully; use a new operation ID.")
        require_idle(state)
        check_live_state(host, entry, state)
        validate_runtime_environment(host, entry)
        require(current_version(host, entry) == expected_current, "stale_current", "Current release differs from the expected release; refresh status.")
        manifest = read_json(release_path(host, entry, target) / "manifest.json")
        release = verified_release(host, entry, manifest)
        roles = enabled_roles(host, entry)
        if target == expected_current:
            require(ready(host, entry, roles=roles), "rollback_readiness", "Selected target is not ready.")
        original_current, original_previous = state["current"], state["previous"]
        resume_terminal_obligations(host, registry, entry, state)
        app(host, entry, release, "config:check")
        attempt = begin(host, entry, state, "rollback", target,
                        metadata={"operation_id": operation_id, "expected_current": expected_current, "roles": roles})
        _save_operation(host, entry, attempt)
        if target == expected_current:
            state["status"] = role_status(entry, roles)
            finish(host, entry, state, "succeeded")
            return {"ok": True, "unchanged": True, "version": target, "operation_id": operation_id}
        try:
            attempt["redis_inventory"] = release_redis_inventory(host, entry, release)
            phase(host, entry, state, "redis")
            widen_redis_grants(host, registry, entry, attempt["redis_inventory"], state)
            stop_background(host, entry, roles=roles)
            phase(host, entry, state, "switch")
            switch_to(host, entry, target)
            phase(host, entry, state, "health")
            restart(host, entry, state, roles=roles)
            state["current"], state["previous"] = manifest, original_current
            state["status"] = role_status(entry, roles, "rolled_back")
            queue_redis_narrowing(state, attempt["redis_inventory"])
            finish(host, entry, state, "succeeded")
        except (DeployError, OSError) as error:
            code = error.code if isinstance(error, DeployError) else "filesystem_failed"
            try:
                restore_code(host, entry, state, original_current, original_previous, "healthy", roles=roles)
            except (DeployError, OSError):
                state["status"] = "unknown"
                finish(host, entry, state, "needs_reconciliation", "code_recovery_failed")
                raise DeployError("code_recovery_failed", "Rollback recovery failed; reconcile before retrying.") from None
            finish(host, entry, state, "failed", code)
            raise DeployError(code, "Rollback failed; status records the original code's recovery outcome.") from None
        result = {"ok": True, "version": target, "operation_id": operation_id}
        warnings = narrow_redis_grants(host, registry, entry, attempt["redis_inventory"], state)
        if warnings:
            result["warnings"] = warnings
        return result


def reconcile(host, project, profile):
    _, entry = host.require_identity(project, profile)
    with host.lock("host"), host.lock(entry["profile"]["instance_id"]):
        registry, entry = host.require_identity(project, profile)
        state = host.load_state(entry)
        compatibility.require_mutation_idle(state)
        attempt = state.get("last_attempt")
        require(attempt is not None, "no_attempt", "There is no operation to reconcile.")
        if attempt["outcome"] not in ("running", "needs_reconciliation"):
            pending = resume_terminal_obligations(host, registry, entry, state)
            return {"ok": True, "unchanged": not pending, "status": state["status"],
                    "version": (state.get("current") or {}).get("version")}
        validate_runtime_environment(host, entry)
        candidate = None
        if attempt.get("release"):
            candidate = release_path(host, entry, attempt["release"])
            candidate = verified_release(host, entry, read_json(candidate / "manifest.json"))
        active = current_version(host, entry)
        manifest = read_json(release_path(host, entry, active) / "manifest.json") if active else None
        if manifest:
            verified_release(host, entry, manifest)
        else:
            require(not state.get("current"), "state_drift", "A recorded live release is missing.")
        if candidate and attempt["action"] == "deploy" and attempt["phase"] in ("migration", "seed", "switch", "health"):
            require(not migration_status(host, entry, candidate), "migration_reconciliation",
                    "Pending migrations remain; reconcile their outcome before starting roles.")
        if attempt["action"] == "stop" or (attempt["action"] == "restart" and attempt.get("role", "all") != "all"):
            role = attempt.get("role", "all")
            selected_units(entry, role)
            try:
                if attempt["action"] == "stop":
                    stop(host, entry, role=role, disable=True)
                    state["status"] = "stopped" if role == "all" else "degraded"
                else:
                    restart(host, entry, state, role=role)
                    state["status"] = "degraded"
            except (DeployError, OSError) as error:
                state["status"] = "degraded"
                finish(host, entry, state, "failed" if attempt["action"] == "restart" else "needs_reconciliation",
                       error.code if isinstance(error, DeployError) else "filesystem_failed")
                raise DeployError("reconciliation_health", "Selected role operation failed; inspect the recorded outcome before retrying.") from None
            finish(host, entry, state, "reconciled")
            return {"ok": True, "status": state["status"], "version": active}
        if attempt["action"] == "deploy" and attempt["phase"] in ("validation", "redis", "backup"):
            resume_background_recovery(host, entry, state)
            resume_redis_transition(host, registry, entry, state)
            state["status"] = role_status(entry, attempt["roles"], "failed") if active and "roles" in attempt else "failed"
            finish(host, entry, state, "failed", "deployment_not_applied")
            return {"ok": True, "status": state["status"], "version": active, "outcome": "failed"}
        applied = False
        if attempt["action"] == "config":
            path = host.paths.instance_config(entry["name"]) / "app.env"
            revision = attempt["configuration_revision"]
            current_hash = digest(path)
            applied = current_hash == attempt["configuration_sha256"]
            unchanged = current_hash == attempt["previous_configuration_sha256"]
            require(applied or unchanged, "configuration_reconciliation", "Configuration outcome is ambiguous; inspect private configuration.")
            require(entry["configuration_revision"] in (revision - 1, revision), "configuration_reconciliation", "Configuration revision changed unexpectedly.")
            if applied:
                entry["configuration_revision"] = revision
            select_runtime_storage(registry, entry, validate_runtime_environment(host, entry),
                                   lambda value: _read_value_file(host, entry, value))
            state["configuration_revision"] = entry["configuration_revision"]
            attempt["configuration_applied"] = applied
            host.journal(registry, entry)
            cleanup_configuration_candidate(host, entry, state)
        try:
            ensure_key_archive(host, entry, state)
            resume_redis_transition(host, registry, entry, state)
            inventory = attempt.get("redis_inventory")
            if inventory:
                widen_redis_grants(host, registry, entry, inventory, state)
            if active:
                # Always restart interrupted transitions: neither a matching link
                # nor a healthy listener proves the executable/config was selected.
                deploy_reached_target = (attempt["action"] == "deploy"
                                         and attempt["phase"] in ("switch", "health")
                                         and active == attempt["release"])
                roles = attempt.get("roles") if attempt["action"] in ("config", "rollback") else None
                if attempt["action"] == "deploy":
                    roles = (list(instance_units(entry)) if deploy_reached_target else
                             attempt.get("roles", list(instance_units(entry))))
                restart(host, entry, state, roles=roles)
                if deploy_reached_target:
                    host.run(["systemctl", "enable", *instance_units(entry).values()])
                if (state.get("current") or {}).get("version") != active:
                    state["previous"], state["current"] = state["current"], manifest
                state["status"] = role_status(entry, roles) if roles is not None else "healthy"
            else:
                state["status"] = "failed" if attempt["action"] == "deploy" else "never_deployed"
        except (DeployError, OSError) as error:
            retained = attempt.get("roles") if attempt["action"] in ("config", "rollback") else None
            state["status"] = role_status(entry, retained, "failed") if retained is not None else "failed"
            outcome = ("failed_applied" if attempt["action"] == "config" and applied else
                       "failed" if attempt["action"] == "restart" else "needs_reconciliation")
            finish(host, entry, state, outcome, error.code if isinstance(error, DeployError) else "filesystem_failed")
            raise DeployError("reconciliation_health", "Recovery could not establish healthy roles; inspect the recorded outcome.") from None
        rollback_applied = attempt["action"] != "rollback" or active == attempt["release"]
        if active:
            inventory = release_redis_inventory(host, entry, release_path(host, entry, active))
            queue_redis_narrowing(state, inventory)
        finish(host, entry, state, "reconciled" if rollback_applied else "failed",
               None if rollback_applied else "rollback_not_applied")
        result = {"ok": True, "status": state["status"], "version": active,
                  "outcome": "reconciled" if rollback_applied else "failed"}
        if active:
            warnings = narrow_redis_grants(host, registry, entry, inventory, state)
            if warnings:
                result["warnings"] = warnings
        return result


def require_configuration_keys(host, entry, release, env_file, keys):
    # The pinned loader ignores unrelated environment variables. Inspect the
    # validated candidate's own generated schema instead of maintaining a list.
    entries = parse_json(app(host, entry, release, "config:inspect", env_file).stdout)
    code, detail = "configuration_metadata", "Candidate configuration metadata is invalid; no settings were changed."
    require(isinstance(entries, list) and 0 < len(entries) <= 10000, code, detail)
    names = set()
    for item in entries:
        require(isinstance(item, dict) and set(item) == {"Name", "Source", "Secret"}
                and isinstance(item["Name"], str) and len(item["Name"]) <= 256
                and re.fullmatch(r"[a-z][a-z0-9_]*(?:\.[a-z][a-z0-9_]*)*", item["Name"]) is not None
                and isinstance(item["Source"], str) and 0 < len(item["Source"]) <= 1024
                and not any(ord(char) < 32 for char in item["Source"])
                and type(item["Secret"]) is bool, code, detail)
        name = item["Name"].replace(".", "__").upper()
        require(name not in names, code, detail)
        names.add(name)
    require(all(key.removesuffix("_FILE") in names for key in keys), "unknown_configuration", "The candidate does not declare this configuration key; no settings were changed.")


def config_set(host, project, profile, changes):
    validate_configuration_changes(changes)
    _, entry = host.require_identity(project, profile)
    with host.lock("host"), host.lock(entry["profile"]["instance_id"]):
        registry, entry = host.require_identity(project, profile)
        return _config_set(host, registry, entry, changes)


def save_rotation_outcome(host, entry, attempt):
    receipt = read_json(_operation_path(host, entry, attempt["rotation_operation_id"]))
    if receipt["outcome"] != "prepared":
        return receipt
    if attempt["outcome"] in ("running", "needs_reconciliation"):
        return receipt
    receipt["outcome"] = attempt["outcome"]
    receipt["error"] = attempt.get("error")
    if attempt["outcome"] == "reconciled" and not attempt.get("configuration_applied"):
        receipt.update(outcome="failed", error="rotation_not_applied")
    _save_operation(host, entry, receipt)
    return receipt


def rotation_result(receipt):
    if receipt["outcome"] not in ("succeeded", "reconciled"):
        raise DeployError(receipt.get("error") or "rotation_failed",
                          "This key rotation completed unsuccessfully; inspect status before a new operation.")
    return copy.deepcopy(receipt["result"])


def keys_rotate(host, project, profile, operation_id, encryption=None):
    require(valid_uuid(operation_id), "invalid_operation", "Key rotation requires a stable operation UUIDv4.")
    _, entry = host.require_identity(project, profile)
    with host.lock("host"), host.lock(entry["profile"]["instance_id"]):
        registry, entry = host.require_identity(project, profile)
        state = host.load_state(entry)
        path = _operation_path(host, entry, operation_id)
        if path.exists():
            receipt = read_json(path)
            require(receipt.get("action") == "keys-rotate" and
                    (encryption is None or encryption == {"key_id": receipt["changes"][ENCRYPTION_KEYS[0]],
                                                         "key": receipt["changes"][ENCRYPTION_KEYS[1]]}),
                    "operation_conflict", "Operation ID already belongs to a different request.")
            attempt = next((item for item in [state.get("last_attempt"), *reversed(state["history"])]
                            if item and item.get("rotation_operation_id") == operation_id), None)
            if attempt:
                receipt = save_rotation_outcome(host, entry, attempt)
            if receipt["outcome"] != "prepared":
                if receipt["outcome"] in ("succeeded", "reconciled"):
                    resume_terminal_obligations(host, registry, entry, state)
                return rotation_result(receipt)
            require(attempt is None, "reconciliation_required", "Reconcile the interrupted key rotation before retrying.")
            require(digest(host.paths.instance_config(entry["name"]) / "app.env") == receipt["previous_configuration_sha256"]
                    and entry["configuration_revision"] + 1 == receipt["result"]["configuration_revision"],
                    "operation_conflict", "Configuration changed since this rotation was prepared.")
        else:
            resume_terminal_obligations(host, registry, entry, state)
            changes = rotation_changes(host, entry, validate_runtime_environment(host, entry), encryption)
            receipt = {"operation_id": operation_id, "action": "keys-rotate", "outcome": "prepared",
                       "changes": changes,
                       "previous_configuration_sha256": digest(host.paths.instance_config(entry["name"]) / "app.env"),
                       "result": {"ok": True, "operation_id": operation_id,
                                  "configuration_revision": entry["configuration_revision"] + 1}}
            _save_operation(host, entry, receipt)
        _config_set(host, registry, entry, receipt["changes"], rotation_operation_id=operation_id)
        return rotation_result(read_json(path))


def _config_set(host, registry, entry, changes, rotation_operation_id=None):
    state = host.load_state(entry)
    values = validate_runtime_environment(host, entry)
    require_idle(state)
    check_live_state(host, entry, state)
    resume_terminal_obligations(host, registry, entry, state)
    manifest = state.get("current")
    if manifest is not None:
        compatibility.require_release(host, entry, manifest)
    if manifest is None:
        version = (state.get("last_attempt") or {}).get("release")
        require(version is not None, "no_release", "Upload the first candidate before editing application configuration.")
        manifest = read_json(release_path(host, entry, version) / "manifest.json")
    live = verified_release(host, entry, manifest)
    path = host.paths.instance_config(entry["name"]) / "app.env"
    # Even an unchanged request must reject unknown keys and conflicting aliases.
    updated = candidate_values(host, entry, values, changes)
    candidate = check_path(path.with_name("app.env.next"), "file")
    require(not candidate.exists(), "configuration_reconciliation", "A previous configuration candidate remains; reconcile it before retrying.")
    payload = dump_env(updated)
    roles = enabled_roles(host, entry)
    prior_status = role_status(entry, roles, state["status"]) if state.get("current") else state["status"]
    roles_stopped = False
    attempt = begin(host, entry, state, "config", manifest["version"], metadata={
        "configuration_revision": entry["configuration_revision"] + 1,
        "configuration_sha256": hashlib.sha256(payload.encode()).hexdigest(),
        "previous_configuration_sha256": digest(path),
        "rotation_operation_id": rotation_operation_id, "roles": roles})
    try:
        host.owned_write(candidate, payload, 0o640, group=entry["name"])
        app(host, entry, live, "config:check", candidate)
        require_configuration_keys(host, entry, live, candidate, list(changes))
        if updated == values:
            state["status"] = prior_status
            finish(host, entry, state, "succeeded")
            return {"ok": True, "unchanged": True, "configuration_revision": entry["configuration_revision"]}
        if any(key.startswith(STORAGE_CONFIGURATION) and updated.get(key) != values.get(key) for key in changes):
            probe_storage(host, entry, live, candidate)
        reserve_runtime_storage(registry, entry, updated, lambda value: _read_value_file(host, entry, value))
        host.journal(registry, entry)
        attempt["redis_inventory"] = release_redis_inventory(host, entry, live, candidate)
        phase(host, entry, state, "redis")
        widen_redis_grants(host, registry, entry, attempt["redis_inventory"], state)
        if state.get("current"):
            roles_stopped = True
            stop(host, entry, roles=roles)
        if any(key.startswith("APP__ENCRYPTION__") for key in changes):
            state["key_archive"] = {"configuration_revision": attempt["configuration_revision"],
                                    "attempt_id": attempt["id"], "pending": True}
        phase(host, entry, state, "configuration")
        os.replace(candidate, path)
        directory = os.open(path.parent, os.O_RDONLY)
        try:
            os.fsync(directory)
        finally:
            os.close(directory)
        require(digest(path) == attempt["configuration_sha256"], "configuration_reconciliation",
                "Published configuration differs from the validated candidate.")
        select_runtime_storage(registry, entry, validate_runtime_environment(host, entry),
                               lambda value: _read_value_file(host, entry, value))
        entry["configuration_revision"] = attempt["configuration_revision"]
        entry["updated_at"] = registry["updated_at"] = now()
        host.journal(registry, entry)
        state["configuration_revision"] = entry["configuration_revision"]
        attempt["configuration_applied"] = True
        phase(host, entry, state, "health")
        ensure_key_archive(host, entry, state)
        if state.get("current"):
            restart(host, entry, state, roles=roles)
            state["status"] = role_status(entry, roles)
        else:
            state["status"] = "never_deployed"
        queue_redis_narrowing(state, attempt["redis_inventory"])
        finish(host, entry, state, "succeeded")
        result = {"ok": True, "configuration_revision": entry["configuration_revision"]}
        warnings = narrow_redis_grants(host, registry, entry, attempt["redis_inventory"], state)
        if warnings:
            result["warnings"] = warnings
        return result
    except (DeployError, OSError) as error:
        code = error.code if isinstance(error, DeployError) else "filesystem_failed"
        if attempt["outcome"] == "running":
            selected = digest(path)
            unchanged = selected == attempt["previous_configuration_sha256"]
            applied = selected == attempt["configuration_sha256"]
            attempt["configuration_applied"] = applied
            state["status"] = prior_status if unchanged else role_status(entry, roles, "failed")
            # Repair the receipt only for an exact known selection. A failed
            # registry write stays unresolved; an unhealthy applied revision does not.
            if applied or (unchanged and "pending_storage_bindings" in entry):
                if applied:
                    entry["configuration_revision"] = attempt["configuration_revision"]
                select_runtime_storage(registry, entry, validate_runtime_environment(host, entry),
                                       lambda value: _read_value_file(host, entry, value))
                host.journal(registry, entry)
                state["configuration_revision"] = entry["configuration_revision"]
            outcome = "failed_applied" if applied else "failed" if unchanged else "needs_reconciliation"
            if unchanged and roles_stopped:
                try:
                    restart(host, entry, state, roles=roles)
                    state["status"] = role_status(entry, roles)
                except (DeployError, OSError):
                    state["status"] = role_status(entry, roles, "failed")
                    outcome = "needs_reconciliation"
            finish(host, entry, state, outcome, "configuration_readiness" if applied else code)
            if applied:
                raise DeployError("configuration_readiness", "Configuration was applied but is unhealthy; a corrective revision is allowed.") from None
        raise
    finally:
        # Keep unresolved candidate bytes as reconciliation evidence.
        if attempt["outcome"] not in ("running", "needs_reconciliation"):
            cleanup_configuration_candidate(host, entry, state)


if __name__ == "__main__":
    if len(sys.argv) == 4 and sys.argv[1] == "copy-runtime-maps":
        copy_runtime_maps(Path(sys.argv[2]), Path(sys.argv[3]))
    elif len(sys.argv) >= 3 and sys.argv[1] == "prune-runtime-maps":
        prune_runtime_maps(Path(sys.argv[2]), set(sys.argv[3:]))
    else:
        raise DeployError("invalid_operation", "Unknown release helper operation.")


def require_cutover_quiescence(host, entry, operation_id):
    """Shared M2 boundary used by the snapshot and cutover owners."""
    compatibility.require_quiescence(host, entry, operation_id)
    state = host.load_state(entry)
    _drain_continuity(host, entry, state)
    require(all(role['complete'] for role in state['cutover']['drain']['roles'].values()),
            'cutover_stop_unproven', 'Every role requires durable clean-drain completion.')
    _require_executor_alone(host, entry, operation_id)


def _require_executor_alone(host, entry, operation_id):
    unit = cutover_unit(entry, operation_id)
    result = host.run(['systemctl', 'show', unit, '--property=MainPID,ControlGroup', '--no-pager'], timeout=5)
    observed = dict(line.split('=', 1) for line in result.stdout.splitlines() if '=' in line)
    group = '/system.slice/' + unit
    require(observed.get('MainPID') == str(os.getpid()) and observed.get('ControlGroup') == group,
            'cutover_executor_unknown', 'Only the supervised operation executor may establish quiescence.')
    root = check_path(host.paths.p('/sys/fs/cgroup' + group), 'directory')
    require(root.is_dir(), 'cutover_executor_unknown', 'The operation cgroup is unavailable.')
    paths = [root / 'cgroup.procs']
    for count, path in enumerate(root.rglob('cgroup.procs')):
        require(count < 4096, 'cutover_executor_unknown', 'Operation cgroup exceeds inspection bounds.')
        paths.append(path)
    for path in paths:
        check_path(path, 'file')
        require(path.is_file() and path.stat().st_size <= 65536
                and set(path.read_text().split()) <= {str(os.getpid())},
                'cutover_executor_unknown', 'A previous operation child has not exited.')


def containment_stop(host, entry, state, role="all"):
    """Stop processes without treating incompatible current code as runnable."""
    selected_units(entry, role)
    progress = state.get("cutover")
    active = progress and progress.get("outcome") not in ("succeeded", "failed")
    if active:
        state["queue_compatibility"]["permission"] = "closed"
        progress["hold"] = True
    else:
        try:
            compatibility.require_current(host, entry)
        except DeployError:
            state["queue_compatibility"]["permission"] = "closed"
    state['status'] = 'unknown'
    host.save_state(entry, state)
    stop(host, entry, role=role, disable=True)
    compatibility.prove_stopped(host, entry, roles=list(selected_units(entry, role)))
    if active:
        state['status'] = 'unknown'
        next_action = cutover_next_action(state)
    else:
        roles = running_roles(host, entry)
        state['status'] = 'degraded' if roles else 'stopped'
        next_action = 'restart' if state['queue_compatibility']['permission'] == 'admitted' else 'cutover-plan'
    host.save_state(entry, state)
    return {"ok": True, "contained": True, "instance": entry["name"],
            "status": state['status'], "next_action": next_action,
            "old_release_start_allowed": state['queue_compatibility']['floor'] is None}


# Cutover lifecycle stays with release activation; wire validation has no side effects.
def inspect_job_queues(host, entry, candidate, *, allow_blocked=False):
    import cutover_contracts as contract
    result = app(host, entry, candidate, 'deploy:jobs-inspect', extra=('--format', 'json'), check=False)
    return contract.inventory(result.stdout, result.returncode, allow_blocked=allow_blocked)


def _cutover_fingerprint(host, entry):
    values = validate_runtime_environment(host, entry)
    referenced = {key: hashlib.sha256(_read_value_file(host, entry, value).encode()).hexdigest()
                  for key, value in values.items() if key.endswith('_FILE')}
    import cutover_contracts as contract
    return contract.sha({'environment': digest(host.paths.instance_config(entry['name']) / 'app.env'),
                         'files': referenced, 'revision': entry['configuration_revision']})


def _cutover_inspection(host, registry, entry, candidate):
    import cutover_contracts as contract
    inventory = inspect_job_queues(host, entry, candidate)
    contract.coverage(host, registry, entry, inventory)
    return inventory


def _cutover_public_plan(plan, token):
    import cutover_contracts as contract
    return {'schema': 1, 'plan_token': token, 'applicable': True, 'blockers': [],
            'forward_recovery': plan.get('forward_recovery'),
            'instance': plan['instance'], 'instance_id': plan['instance_id'],
            'host_id': plan['host_id'], 'source': (plan['source'] or {}).get('version'),
            'target': plan['target']['version'], 'candidate_sha256': plan['target']['files']['artisan'],
            'roles': plan['roles'], 'original_enabled_roles': plan['enabled_roles'], 'original_running_roles': plan['running_roles'],
            'queues': [{'connection': c['connection'], 'queue': q['queue'], 'layout': q['layout'], 'target_layout': 3}
                       for c in plan['inventory']['connections'] for q in c['queues']],
            'downtime': 'full; about one minute is a target, not a deadline', 'warning': contract.WARNING,
            'owner_attestation': contract.ATTESTATION, 'snapshot': 'after verified drain, encrypted and verified off-host',
            'database_migrations_pending': plan['database_pending'], 'seeders': True,
            'queue_cutover_required': bool(plan['inventory']['connections']), 'checked_at': plan['checked_at']}


def _blocked_cutover_plan(entry, manifest, inventory, blockers):
    import cutover_contracts as contract
    return {'schema': 1, 'applicable': False, 'plan_token': None, 'instance': entry['name'],
            'target': manifest['version'], 'candidate_sha256': manifest['files']['artisan'],
            'queues': [{'connection': c['connection'], 'queue': q['queue'], 'layout': q['layout'], 'eligible': q['eligible']}
                       for c in inventory['connections'] for q in c['queues']],
            'blockers': blockers, 'warning': contract.WARNING, 'owner_attestation': contract.ATTESTATION}


def _review_identity(reviewed_by, reason):
    require(all(isinstance(value, str) and value.strip() == value and 1 <= len(value) <= limit
                and all(ord(char) >= 32 for char in value)
                for value, limit in ((reviewed_by, 200), (reason, 2000))),
            'cutover_review_required', 'Supply REVIEWED_BY and REASON for the approved recovery decision.')


def _require_pre_dispatch(state):
    progress = state.get('cutover') or {}
    require(progress.get('action') == 'cutover' and progress.get('outcome') not in ('succeeded', 'failed')
            and not progress['rollback_prohibited'] and state['queue_compatibility']['floor'] is None
            and progress['phase'] in ('prepared', 'stopping')
            and progress.get('snapshot_started') is False
            and 'snapshot' not in progress and all(q['status'] == 'pending' for q in progress['queues'])
            and progress['database'] == progress['seed'] == 'not_started',
            'cutover_abort_unsafe', 'Abort/replan requires proof that snapshot and migration dispatch never began.')


def plan_cutover(host, project, profile, archive, checksum, *, forward=False, abort=False, reviewed_by=None, reason=None):
    import cutover_contracts as contract
    import upgrades
    _, entry = host.require_identity(project, profile)
    with host.lock('host'), host.lock(entry['profile']['instance_id']):
        registry, entry = host.require_identity(project, profile)
        require(registry['tools']['generation'] >= compatibility.GENERATION, 'host_upgrade_required', 'Install cutover-aware tools first.')
        tools = upgrades._installed(host, registry['tools'])
        state = host.load_state(entry)
        recovery = None
        if forward or abort:
            active = state.get('cutover')
            if abort:
                _review_identity(reviewed_by, reason)
                _require_pre_dispatch(state)
            require(active and active['outcome'] not in ('succeeded', 'failed')
                    and (abort or state['queue_compatibility']['floor'] == 3),
                    'cutover_forward_required', 'Forward recovery requires an unresolved layout-3 operation.')
            require(cutover_executor(host, entry, active['operation_id']) != 'running',
                    'operation_locked', 'Contain the previous executor before planning forward recovery.')
            _drain_continuity(host, entry, state)
            require(not active['drain_failed'], 'cutover_drain_review', 'Drain requires review before forward recovery.')
            old_record = read_json(contract.private_path(host, entry, 'operation', active['operation_id']))
            old_plan = (_load_cutover_plan(host, entry, old_record['plan_token']) if active['action'] == 'cutover'
                        else read_json(contract.private_path(host, entry, 'restore', active['operation_id'])))
            _observe_cutover_selection(host, entry, state, old_plan)
            recovery = {'operation_id': active['operation_id'], 'progress_sha256': contract.sha(active),
                        'database': active['database'], 'seed': active['seed'],
                        'warning': 'Replacement migrations and seeders run under explicit forward-recovery approval; prior side effects may have completed.'}
            if abort:
                recovery.update(kind='pre_dispatch_abort', reviewed_by=reviewed_by, reason=reason,
                                warning='Supersede before snapshot/migration dispatch; keep all queue data and the startup fence.')
        else:
            require_idle(state)
        check_live_state(host, entry, state)
        if not (forward or abort):
            resume_terminal_obligations(host, registry, entry, state)
        compatibility.require_guards(host, entry)
        staging = host.paths.opt(entry['name']) / 'releases' / ('.stage-' + uuid.uuid4().hex)
        try:
            manifest = unpack_archive(Path(archive), staging, entry, checksum)
            candidate = publish_release(host, entry, staging, manifest)
            native_runtime(host, entry)
            protect_maps(host, entry, candidate)
            app(host, entry, candidate, 'config:check')
            worker_connection(host, entry, candidate)
            if entry.get('storage_bindings'):
                probe_storage(host, entry, candidate)
            redis_inventory = release_redis_inventory(host, entry, candidate)
            source = state['current']
            old_inventory = release_redis_inventory(host, entry, verified_release(host, entry, source)) if source else {'patterns': [], 'channels': []}
            old_jobs = {p for p in old_inventory['patterns'] if p.startswith('foundry:jobs:')}
            new_jobs = {p for p in redis_inventory['patterns'] if p.startswith('foundry:jobs:')}
            registered_jobs = {p for p in granted_redis_patterns(entry) if p.startswith('foundry:jobs:')}
            require(not source or old_jobs == new_jobs, 'cutover_scope_changed', 'The cutover cannot rename or remove job storage scopes.')
            require(registered_jobs <= new_jobs, 'cutover_scope_changed', 'Registered historical job scopes are absent from the candidate.')
            widen_redis_grants(host, registry, entry, redis_inventory)
            inventory = inspect_job_queues(host, entry, candidate, allow_blocked=True)
            if not contract.applicable(inventory):
                return _blocked_cutover_plan(entry, manifest, inventory, inventory['blockers'])
            try:
                contract.coverage(host, registry, entry, inventory)
            except DeployError as error:
                if error.code not in ('cutover_unknown_queue', 'cutover_coverage', 'cutover_redis_ownership'):
                    raise
                return _blocked_cutover_plan(entry, manifest, inventory, [{'code': error.code, 'connection': None, 'queue': None}])
            if forward or abort:
                require(manifest != old_plan['target'], 'cutover_forward_artifact', 'Forward recovery must bind a replacement artifact.')
                require(abort or contract.inventory_identity(inventory) == old_plan['inventory_sha256'],
                        'cutover_scope_changed', 'Forward recovery must retain the validated queue inventory.')
            require({s['prefix'] + '*' for s in contract.scopes(inventory)} == new_jobs,
                    'cutover_scope_changed', 'Job inspection and Redis grants disagree about the queue scope.')
            if inventory['connections']:
                backup._settings(host, entry)
                backup._repository_settings(host)
                # Fail before downtime if the separately delivered capability is absent.
                _snapshot_module()
            original_roles = enabled_roles(host, entry)
            live = services(host, entry) if source else {}
            require(all(v['ActiveState'] in ('active', 'inactive', 'failed')
                        and (v['ActiveState'] != 'active' or v['executable_matches']) for v in live.values()),
                    'cutover_clients_unproven', 'Current role identities cannot be established.')
            plan = {'schema': 1, 'host_id': entry['host_id'], 'instance_id': profile['instance_id'], 'instance': entry['name'],
                    'source': source, 'previous': state['previous'], 'target': manifest, 'archive_sha256': checksum,
                    'configuration_revision': entry['configuration_revision'], 'configuration_fingerprint': _cutover_fingerprint(host, entry),
                    'topology_sha256': entry['topology']['sha256'], 'roles': list(instance_units(entry)),
                    'enabled_roles': original_roles, 'running_roles': [r for r, v in live.items() if v['ActiveState'] == 'active'],
                    'inventory': inventory, 'inventory_sha256': contract.inventory_identity(inventory),
                    'redis_inventory': redis_inventory, 'database_pending': migration_status(host, entry, candidate),
                    'tools_path': str(tools), 'tools_sha256': registry['tools']['bundle_sha256'],
                    'owner_attestation': contract.ATTESTATION, 'checked_at': now(),
                    'gate': copy.deepcopy(state.get('queue_compatibility')), 'nonce': str(uuid.uuid4()),
                    'forward_recovery': recovery}
            # A private random nonce prevents the public digest exposing configuration guesses.
            token = contract.sha(plan)
            contract.save_private(host, contract.private_path(host, entry, 'plan', token), plan)
            return _cutover_public_plan(plan, token)
        finally:
            if staging.exists():
                shutil.rmtree(staging)


def _snapshot_module():
    try:
        import redis_snapshot
        return redis_snapshot
    except ImportError:
        raise DeployError('cutover_snapshot_unavailable', 'Install the reviewed Redis snapshot capability before applying cutover.') from None


def _load_cutover_plan(host, entry, token):
    import cutover_contracts as contract
    path = contract.private_path(host, entry, 'plan', token)
    require(path.exists(), 'cutover_plan_missing', 'The reviewed cutover plan is missing.')
    plan = read_json(path)
    require(contract.sha(plan) == token and plan.get('schema') == 1 and plan.get('instance_id') == entry['profile']['instance_id']
            and plan.get('host_id') == entry['host_id'], 'cutover_plan_invalid', 'Cutover plan belongs to another instance.')
    return plan


def _verify_cutover_inputs(host, registry, entry, plan):
    import upgrades
    require(entry['configuration_revision'] == plan['configuration_revision']
            and _cutover_fingerprint(host, entry) == plan['configuration_fingerprint']
            and entry['topology']['sha256'] == plan['topology_sha256']
            and registry['tools']['bundle_sha256'] == plan['tools_sha256']
            and str(upgrades._installed(host, registry['tools'])) == plan['tools_path'],
            'cutover_plan_changed', 'Release configuration, topology or tools changed; do not reuse this plan.')
    compatibility.require_guards(host, entry)
    return verified_release(host, entry, plan['target'])


def cutover_unit(entry, operation_id):
    require(valid_uuid(operation_id), 'invalid_operation', 'A stable cutover UUIDv4 is required.')
    return f"foundry-cutover-{entry['profile']['instance_id']}-{operation_id}.service"


def cutover_executor(host, entry, operation_id):
    unit = cutover_unit(entry, operation_id)
    result = host.run(['systemctl', 'show', unit,
                       '--property=LoadState,ActiveState,SubState,MainPID,ControlPID,ControlGroup,Job,Transient,Description', '--no-pager'],
                      timeout=5, check=False)
    value = dict(line.split('=', 1) for line in result.stdout.splitlines() if '=' in line)
    require(result.returncode == 0 or value.get('LoadState') == 'not-found',
            'cutover_executor_unknown', 'Cannot establish the cutover executor outcome.')
    if value.get('LoadState') == 'not-found':
        compatibility._empty_cgroup(host, '/system.slice/' + unit)
        return 'absent'
    require(value.get('LoadState') == 'loaded' and value.get('Transient') == 'yes'
            and value.get('Description') == f"Foundry cutover {entry['profile']['instance_id']} {operation_id}",
            'cutover_executor_unknown', 'Unknown cutover executor unit.')
    if value.get('ActiveState') in ('activating', 'deactivating') or value.get('MainPID') not in ('0', None) or value.get('ControlPID') not in ('0', None):
        return 'running'
    require(value.get('ActiveState') in ('active', 'inactive', 'failed') and value.get('Job') in ('', '0')
            and value.get('MainPID') == value.get('ControlPID') == '0',
            'cutover_executor_unknown', 'The cutover executor still has pending work.')
    compatibility._empty_cgroup(host, value.get('ControlGroup') or '/system.slice/' + unit)
    return 'exited'


def _submit_cutover(host, entry, plan, operation_id):
    unit = cutover_unit(entry, operation_id)
    observed = cutover_executor(host, entry, operation_id)
    if observed == 'running':
        return {'ok': True, 'operation_id': operation_id, 'outcome': 'running', 'unit': unit}
    if observed == 'exited':
        host.run(['systemctl', 'stop', unit], timeout=15)
        host.run(['systemctl', 'reset-failed', unit], timeout=5, check=False)
    # Deterministic unit name and the executor's locks close duplicate-submission races.
    result = host.run(['systemd-run', '--unit=' + unit,
                      '--description=' + f"Foundry cutover {entry['profile']['instance_id']} {operation_id}",
                      '--no-block', '--quiet', '--property=Type=exec',
                      '--property=Restart=no', '--property=KillMode=control-group', '--property=RemainAfterExit=yes',
                      '--property=UMask=0077', '--property=TimeoutStopSec=100s',
                      '/usr/bin/python3', '-I', str(Path(plan['tools_path']) / 'server/cutover_runner.py'),
                      entry['profile']['instance_id'], operation_id], timeout=15, check=False)
    require(result.returncode == 0 or cutover_executor(host, entry, operation_id) == 'running',
            'cutover_executor_unknown', 'Submission outcome is unknown; resume the same operation.')
    return {'ok': True, 'operation_id': operation_id, 'outcome': 'running', 'unit': unit}


def _accept_cutover_state(host, entry, before, state, record):
    import cutover_contracts as contract
    record.update(accepted=False, prior_state_sha256=contract.sha(before), acceptance=copy.deepcopy(state))
    contract.save_private(host, contract.private_path(host, entry, 'operation', record['operation_id']), record)
    return _recover_cutover_acceptance(host, entry, before, record)


def _recover_cutover_acceptance(host, entry, state, record):
    """Finish an interrupted intent/state write without resetting dispatched work."""
    import cutover_contracts as contract
    if not record.get('accepted'):
        if (state.get('cutover') or {}).get('operation_id') != record['operation_id']:
            require(contract.sha(state) == record['prior_state_sha256'],
                    'cutover_state_invalid', 'State changed before operation acceptance; review a new plan.')
            state = copy.deepcopy(record['acceptance'])
            host.save_state(entry, state)
        superseded = record.get('superseded_progress')
        if superseded:
            path = contract.private_path(host, entry, 'operation', superseded['operation_id'])
            prior = read_json(path)
            prior['superseded_progress'] = superseded
            prior['result'] = {'ok': False, 'operation_id': superseded['operation_id'],
                              'outcome': 'superseded', 'replacement_operation': record['operation_id']}
            contract.save_private(host, path, prior)
        record['accepted'] = True
        record.pop('acceptance', None)
        record.pop('prior_state_sha256', None)
        contract.save_private(host, contract.private_path(host, entry, 'operation', record['operation_id']), record)
    require((state.get('cutover') or {}).get('operation_id') == record['operation_id'],
            'cutover_state_invalid', 'Accepted operation progress is missing.')
    return state


def apply_cutover(host, project, profile, token, operation_id, *, resume=False, forward=False, abort=False):
    import cutover_contracts as contract
    require(valid_uuid(operation_id) and contract.hash_value(token), 'invalid_operation', 'Apply requires reviewed PLAN_TOKEN and stable OPERATION.')
    _, entry = host.require_identity(project, profile)
    # A status/join does not wait behind the executor's long-lived mutation locks.
    observed = cutover_executor(host, entry, operation_id)
    record_path = contract.private_path(host, entry, 'operation', operation_id)
    if observed == 'running':
        recorded = read_json(record_path) if record_path.exists() else {}
        require(recorded.get('plan_token') == token and recorded.get('action') == 'cutover',
                'operation_conflict', 'Operation ID belongs to another request.')
        return {'ok': True, 'operation_id': operation_id, 'outcome': 'running'}
    with host.lock('host'), host.lock(profile['instance_id']):
        registry, entry = host.require_identity(project, profile)
        state = host.load_state(entry)
        plan = _load_cutover_plan(host, entry, token)
        recovery = plan.get('forward_recovery')
        abort_plan = bool(recovery and recovery.get('kind') == 'pre_dispatch_abort')
        require(resume or (bool(recovery) == (forward or abort) and abort_plan == abort),
                'cutover_forward_confirmation', 'Use the explicit recovery apply action matching this plan.')
        if record_path.exists():
            recorded = read_json(record_path)
            require(recorded.get('plan_token') == token and recorded.get('action') == 'cutover',
                    'operation_conflict', 'Operation ID belongs to another request.')
            if recorded.get('result'):
                return recorded['result']
            require(resume, 'cutover_pending', 'Use cutover-resume for the accepted operation.')
            _verify_cutover_inputs(host, registry, entry, plan)
            state = _recover_cutover_acceptance(host, entry, state, recorded)
            progress = state['cutover']
            if progress['outcome'] in ('succeeded', 'failed'):
                return _finish_cutover(host, entry, state, recorded, succeeded=progress['outcome'] == 'succeeded', error=progress['error'])
            require(not progress['drain_failed'], 'cutover_drain_review', 'Forced termination requires operator review.')
            _drain_continuity(host, entry, state)
            _verify_cutover_inputs(host, registry, entry, plan)
            progress['hold'] = False
            progress['outcome'] = 'running'
            host.save_state(entry, state)
        else:
            require(not resume, 'cutover_operation_missing', 'No accepted operation exists for this ID.')
            superseded = None
            if forward or abort:
                if abort:
                    _require_pre_dispatch(state)
                superseded = copy.deepcopy(state.get('cutover'))
                recovery = plan['forward_recovery']
                require(superseded and superseded['operation_id'] == recovery['operation_id']
                        and contract.sha(superseded) == recovery['progress_sha256'],
                        'cutover_plan_changed', 'The operation changed after forward-recovery review.')
                require(cutover_executor(host, entry, superseded['operation_id']) != 'running',
                        'operation_locked', 'The previous executor must exit before supersession.')
                _drain_continuity(host, entry, state)
                require(not superseded['drain_failed'], 'cutover_drain_review', 'Drain requires review.')
            else:
                require_idle(state)
            candidate = _verify_cutover_inputs(host, registry, entry, plan)
            require(state['current'] == plan['source'] and current_version(host, entry) == (plan['source'] or {}).get('version')
                    and enabled_roles(host, entry) == plan['enabled_roles']
                    and running_roles(host, entry) == plan['running_roles']
                    and state.get('queue_compatibility') == plan['gate'], 'cutover_plan_changed', 'Current release or role intent changed.')
            native_runtime(host, entry)
            app(host, entry, candidate, 'config:check')
            worker_connection(host, entry, candidate)
            if entry.get('storage_bindings'):
                probe_storage(host, entry, candidate)
            inventory = _cutover_inspection(host, registry, entry, candidate)
            require(contract.inventory_identity(inventory) == plan['inventory_sha256'], 'cutover_plan_changed', 'The queue inventory changed.')
            if inventory['connections']:
                _snapshot_module()
                backup._settings(host, entry)
                backup._repository_settings(host)
            before = copy.deepcopy(state)
            if superseded:
                state.pop('cutover')
                state['last_attempt'].update(outcome='failed', finished_at=now(),
                                             error='reviewed_pre_dispatch_abort' if abort else 'reviewed_forward_recovery')
            begin(host, entry, state, 'deploy', plan['target']['version'], metadata={'roles': plan['enabled_roles']}, persist=False)
            prohibited = any(q['layout'] == '3' for c in inventory['connections'] for q in c['queues']) or (plan['gate'] or {}).get('floor') == 3
            state['cutover'] = {'schema': 1, 'operation_id': operation_id, 'plan_token': token, 'action': 'cutover',
                                'source': (plan['source'] or {}).get('version'), 'target': plan['target']['version'],
                                'phase': 'prepared', 'outcome': 'running', 'started_at': now(), 'rollback_prohibited': prohibited,
                                'drain_failed': False, 'hold': False, 'database': 'not_started', 'seed': 'not_started', 'error': None,
                                'snapshot_started': False,
                                'queues': [{'connection': t['connection'], 'queue': t['queue'], 'status': 'pending'} for t in contract.targets(inventory)]}
            state['cutover']['drain'] = _new_drain(host, entry)
            if superseded:
                state['cutover']['supersedes_operation'] = superseded['operation_id']
                if superseded.get('drain_resolutions'):
                    state['cutover']['drain_resolutions'] = copy.deepcopy(superseded['drain_resolutions'])
            gate = copy.deepcopy(plan['gate']) if plan['gate'] else {'schema': 1, 'floor': None, 'permission': 'closed', 'admitted': {}}
            gate['permission'] = 'closed'
            if prohibited:
                gate['floor'] = 3
            state['queue_compatibility'] = gate
            # Intent first, then fenced progress. An incomplete acceptance cannot launch work.
            record = {'schema': 1, 'operation_id': operation_id, 'plan_token': token, 'action': 'cutover',
                      'snapshot_id': str(uuid.uuid4()), 'backup_operation_id': str(uuid.uuid4()),
                      'superseded_progress': superseded}
            _accept_cutover_state(host, entry, before, state, record)
    return _submit_cutover(host, entry, plan, operation_id)


def _cutover_phase(host, entry, state, phase_name):
    state['cutover']['phase'] = phase_name
    state['last_attempt']['phase'] = {'prepared': 'validation', 'stopping': 'redis', 'snapshot': 'backup',
                                     'queues': 'redis', 'database': 'migration', 'recovering': 'health',
                                     'complete': 'health'}.get(phase_name, phase_name)
    host.save_state(entry, state)


def running_roles(host, entry):
    observed = services(host, entry)
    require(all(v['ActiveState'] in ('active', 'inactive', 'failed')
                and (v['ActiveState'] != 'active' or v['executable_matches']) for v in observed.values()),
            'cutover_clients_unproven', 'Running role identities are not established.')
    return [role for role, value in observed.items() if value['ActiveState'] == 'active']


def _boot_id(host):
    value = check_path(host.paths.p('/proc/sys/kernel/random/boot_id'), 'file').read_text().strip()
    require(re.fullmatch('[a-f0-9]{8}(-[a-f0-9]{4}){3}-[a-f0-9]{12}', value),
            'cutover_stop_unproven', 'Host boot identity is unavailable.')
    return value


def _role_invocation(host, unit):
    result = host.run(['systemctl', 'show', unit, '--property=InvocationID', '--value'], timeout=5)
    value = result.stdout.strip()
    require(re.fullmatch('[a-f0-9]{32}|', value), 'cutover_stop_unproven', 'Role invocation identity is unavailable.')
    return value


def _new_drain(host, entry, *, starting=False):
    return {'boot_id': _boot_id(host), 'roles': {
        role: {'invocation_id': None if starting else _role_invocation(host, unit), 'complete': False}
        for role, unit in instance_units(entry).items()}}


def resolve_cutover_drain(host, project, profile, operation_id, reviewed_by, reason):
    """Record an explicit review of uncertain work; never infer a successful drain."""
    _review_identity(reviewed_by, reason)
    with host.lock('host'), host.lock(profile['instance_id']):
        _, entry = host.require_identity(project, profile)
        state = host.load_state(entry)
        progress = state.get('cutover') or {}
        require(progress.get('operation_id') == operation_id and progress.get('outcome') not in ('succeeded', 'failed'),
                'operation_conflict', 'Review must name the active unresolved operation.')
        require(cutover_executor(host, entry, operation_id) != 'running', 'operation_locked', 'Contain the executor before review.')
        require(state['queue_compatibility']['permission'] == 'closed', 'queue_start_fenced', 'Review requires closed startup permission.')
        compatibility.require_guards(host, entry)
        observations = compatibility.prove_stopped(host, entry, review=True)
        compatibility.require_no_clients(host, entry)
        boot = _boot_id(host)
        reviews = progress.setdefault('drain_resolutions', [])
        if reviews and all(reviews[-1][key] == value for key, value in
                           (('boot_id', boot), ('observations', observations), ('reviewed_by', reviewed_by), ('reason', reason))):
            return {'ok': True, 'operation_id': operation_id, 'resolution': reviews[-1], 'next_action': cutover_next_action(state)}
        require(progress['drain_failed'], 'cutover_review_not_required', 'No unresolved drain flag requires approval.')
        require(len(reviews) < 32, 'cutover_review_limit', 'Drain review history requires lead reconciliation.')
        resolution = {'reviewed_by': reviewed_by, 'reason': reason, 'recorded_at': now(),
                      'boot_id': boot, 'observations': observations}
        reviews.append(resolution)
        progress['drain'] = _new_drain(host, entry)
        for role in progress['drain']['roles'].values():
            role['complete'] = True
        progress.update(drain_failed=False, hold=True, outcome='needs_reconciliation')
        host.save_state(entry, state)
        return {'ok': True, 'operation_id': operation_id, 'resolution': resolution, 'next_action': cutover_next_action(state)}


def _drain_continuity(host, entry, state):
    progress = state['cutover']
    if progress['outcome'] in ('succeeded', 'failed'):
        return
    drain = progress.get('drain')
    require(drain and set(drain['roles']) == set(instance_units(entry)),
            'cutover_drain_review', 'Durable role drain evidence is missing; review is required.')
    if drain['boot_id'] != _boot_id(host) and not all(role['complete'] for role in drain['roles'].values()):
        progress['drain_failed'] = True
        host.save_state(entry, state)
        raise DeployError('cutover_drain_review', 'The host rebooted before clean drain was recorded.')


def _cutover_stop(host, entry, state):
    _drain_continuity(host, entry, state)
    require(not state['cutover']['drain_failed'], 'cutover_drain_review', 'Prior drain requires review.')
    _cutover_phase(host, entry, state, 'stopping')
    state['queue_compatibility']['permission'] = 'closed'
    host.save_state(entry, state)
    units = instance_units(entry)
    drain = state['cutover']['drain']
    for role, unit in units.items():
        observed = _role_invocation(host, unit)
        prior = drain['roles'][role]
        if not prior['complete'] and prior['invocation_id'] is not None and observed and prior['invocation_id'] != observed:
            state['cutover']['drain_failed'] = True
            host.save_state(entry, state)
            raise DeployError('cutover_drain_review', 'A role invocation changed before clean drain was recorded.')
        if prior['invocation_id'] is None:
            prior['invocation_id'] = observed
    host.save_state(entry, state)
    ordered = [units[r] for r in ('http', 'scheduler', 'worker') if r in units]
    host.run(['systemctl', 'stop', '--no-block', *ordered], timeout=10)
    deadline = time.monotonic() + 100
    while True:
        try:
            for role in units:
                compatibility.prove_stopped(host, entry, roles=[role])
                if not drain['roles'][role]['complete']:
                    drain['roles'][role]['complete'] = True
                    host.save_state(entry, state)
            compatibility.require_no_clients(host, entry)
            _require_executor_alone(host, entry, state['cutover']['operation_id'])
            return
        except DeployError as error:
            if error.code == 'cutover_drain_review':
                state['cutover']['drain_failed'] = True
                host.save_state(entry, state)
                raise
            if time.monotonic() >= deadline or error.code not in ('cutover_stop_unproven', 'cutover_clients_present'):
                raise
            time.sleep(min(0.2, max(0, deadline - time.monotonic())))


def _snapshot_request(entry, plan, record):
    import cutover_contracts as contract
    return {'schema': 1, 'operation_id': record['operation_id'], 'snapshot_id': record['snapshot_id'],
            'source_release': (plan['source'] or plan['target'])['version'],
            'configuration_revision': plan['configuration_revision'], 'inventory_sha256': plan['inventory_sha256'],
            'scopes': contract.scopes(plan['inventory'])}


def _save_snapshot_binding(host, entry, plan, record, evidence):
    import cutover_contracts as contract
    contract.snapshot_evidence(evidence, _snapshot_request(entry, plan, record), entry)
    value = {'inventory': plan['inventory'], 'evidence': evidence}
    path = contract.private_path(host, entry, 'snapshot', evidence['snapshot_id'])
    if path.exists():
        require(read_json(path) == value, 'operation_conflict', 'Snapshot binding cannot change.')
    else:
        contract.save_private(host, path, value)


def _load_snapshot_binding(host, entry, snapshot_id):
    import cutover_contracts as contract
    path = contract.private_path(host, entry, 'snapshot', snapshot_id)
    require(path.exists(), 'redis_restore_archive', 'The immutable snapshot inventory binding is missing.')
    value = read_json(path)
    contract.fields(value, 'inventory evidence', 'redis_restore_archive')
    inventory = contract.inventory(json.dumps(value['inventory']))
    evidence = value['evidence']
    request = {key: evidence[key] for key in ('operation_id', 'source_release', 'configuration_revision')}
    request.update(snapshot_id=snapshot_id, inventory_sha256=contract.inventory_identity(inventory), scopes=contract.scopes(inventory))
    contract.snapshot_evidence(evidence, request, entry)
    return value


def _finish_cutover(host, entry, state, record, *, succeeded, error=None):
    import cutover_contracts as contract
    progress = state['cutover']
    outcome = 'succeeded' if succeeded else 'failed'
    if progress['outcome'] != outcome:
        progress.update(outcome=outcome, phase='complete', error=error, completed_at=now())
        finish(host, entry, state, outcome, error)
    result = {'ok': succeeded, 'operation_id': progress['operation_id'], 'outcome': progress['outcome'],
              'version': (state['current'] or {}).get('version'), 'error': error}
    record['result'] = result
    contract.save_private(host, contract.private_path(host, entry, 'operation', progress['operation_id']), record)
    return result


def _cutover_failure(host, registry, entry, state, plan, record, error):
    progress = state['cutover']
    code = error.code if isinstance(error, DeployError) else 'cutover_execution_failed'
    progress['error_phase'] = progress['phase']
    progress.update(outcome='needs_reconciliation', error=code)
    state['queue_compatibility']['permission'] = 'closed'
    state['status'] = 'unknown'
    host.save_state(entry, state)
    try:
        _observe_cutover_selection(host, entry, state, plan)
    except (DeployError, OSError):
        return  # Keep the fence closed when even selected-artifact identity is uncertain.
    if progress['hold'] or progress['rollback_prohibited'] or progress['drain_failed'] or progress['database'] != 'not_started' or progress['seed'] != 'not_started':
        # Even candidate traffic must cease after a health failure. A failed stop stays uncertain.
        try:
            _cutover_stop(host, entry, state)
        except (DeployError, OSError):
            pass
        progress['outcome'] = 'needs_reconciliation'
        host.save_state(entry, state)
        return
    try:
        require_cutover_quiescence(host, entry, progress['operation_id'])
        _verify_cutover_inputs(host, registry, entry, plan)
        observed = _cutover_inspection(host, registry, entry, verified_release(host, entry, plan['target']))
        if any(q['layout'] == '3' for c in observed['connections'] for q in c['queues']):
            progress['rollback_prohibited'] = True
            state['queue_compatibility']['floor'] = 3
            host.save_state(entry, state)
            raise DeployError('queue_release_incompatible', 'Observed layout 3 forbids source recovery.')
        source = plan['source']
        if source:
            verified_release(host, entry, source)
            prepare_worker(host, entry, verified_release(host, entry, source))
            state['cutover']['drain'] = _new_drain(host, entry, starting=True)
            state['queue_compatibility']['admitted'] = {source['version']: source['files']['artisan']}
            state['queue_compatibility']['permission'] = 'admitted'
            host.save_state(entry, state)
            switch_to(host, entry, source['version'])
            state['current'], state['previous'] = source, plan['previous']
            restart(host, entry, state, roles=plan['running_roles'])
            require(enabled_roles(host, entry) == plan['enabled_roles'], 'service_enablement', 'Recovery changed original enablement.')
        state['status'] = role_status(entry, plan['running_roles']) if source else 'failed'
        _finish_cutover(host, entry, state, record, succeeded=False, error=code)
    except (DeployError, OSError):
        state['queue_compatibility']['permission'] = 'closed'
        progress['outcome'] = 'needs_reconciliation'
        host.save_state(entry, state)


def execute_cutover(host, instance_id, operation_id):
    import cutover_contracts as contract
    registry = host.read_registry()
    entry = registry['instances'].get(instance_id)
    require(entry is not None and valid_uuid(operation_id), 'invalid_operation', 'Unknown cutover instance or operation.')
    with host.lock('host'), host.lock(instance_id):
        registry = host.read_registry()
        entry = registry['instances'][instance_id]
        record = read_json(contract.private_path(host, entry, 'operation', operation_id))
        require(record.get('accepted') is True, 'cutover_state_invalid', 'Operation acceptance must finish before execution.')
        state = host.load_state(entry)
        progress = state.get('cutover') or {}
        require(progress.get('operation_id') == operation_id and record['plan_token'] == progress.get('plan_token'),
                'operation_conflict', 'The executor does not own the active operation.')
        if record.get('result'):
            return record['result']
        if progress['outcome'] in ('succeeded', 'failed'):
            return _finish_cutover(host, entry, state, record, succeeded=progress['outcome'] == 'succeeded', error=progress['error'])
        if record['action'] == 'redis-restore':
            return _execute_redis_restore(host, registry, entry, state, record)
        plan = _load_cutover_plan(host, entry, record['plan_token'])
        try:
            require(not progress['hold'], 'cutover_held', 'Containment stop requires explicit resume.')
            require(not progress['drain_failed'], 'cutover_drain_review', 'Forced termination requires operator review.')
            candidate = _verify_cutover_inputs(host, registry, entry, plan)
            worker_connection(host, entry, candidate)
            _cutover_stop(host, entry, state)
            inventory = _cutover_inspection(host, registry, entry, candidate)
            require(contract.inventory_identity(inventory) == plan['inventory_sha256'], 'cutover_plan_changed', 'The frozen queue inventory changed.')
            if any(q['layout'] == '3' for c in inventory['connections'] for q in c['queues']):
                progress['rollback_prohibited'] = True
                state['queue_compatibility']['floor'] = 3
                host.save_state(entry, state)
            require_cutover_quiescence(host, entry, operation_id)
            if progress['queues']:
                progress['snapshot_started'] = True
                _cutover_phase(host, entry, state, 'snapshot')
                request = _snapshot_request(entry, plan, record)
                if 'snapshot' not in progress:
                    require(not progress['rollback_prohibited'] or all(q['status'] == 'pending' for q in progress['queues']),
                            'cutover_snapshot_missing', 'An earlier migration may have run without retrievable snapshot evidence.')
                    evidence = _snapshot_module().capture(host, registry, entry, request)
                    contract.snapshot_evidence(evidence, request, entry)
                    require(evidence['captured_at'] >= progress['started_at'], 'cutover_snapshot_evidence', 'Snapshot predates this cutover window.')
                    _save_snapshot_binding(host, entry, plan, record, evidence)
                    progress['snapshot'] = evidence
                    host.save_state(entry, state)
                else:
                    contract.snapshot_evidence(progress['snapshot'], request, entry)
                    _save_snapshot_binding(host, entry, plan, record, progress['snapshot'])
                _cutover_phase(host, entry, state, 'queues')
                for item, target in zip(progress['queues'], contract.targets(inventory), strict=True):
                    if item['status'] == 'confirmed':
                        require(target['layout'] == '3', 'cutover_layout_drift', 'A confirmed queue no longer has layout 3.')
                        continue
                    require_cutover_quiescence(host, entry, operation_id)
                    progress['rollback_prohibited'] = True
                    state['queue_compatibility']['floor'] = 3
                    item['status'] = 'dispatched_or_unknown'
                    host.save_state(entry, state)
                    response = app(host, entry, candidate, 'jobs', extra=('migrate-layout', '--connection', target['connection'],
                                   '--queue', target['queue'], '--confirm', '--format', 'json'))
                    result = parse_json(response.stdout)
                    require(isinstance(result, dict) and set(result) == {'queue', 'migrated'}
                            and result['queue'] == target['queue'] and type(result['migrated']) is bool,
                            'cutover_migration_unknown', 'Queue migration returned no confirmed result.')
                    observed = _cutover_inspection(host, registry, entry, candidate)
                    require(contract.inventory_identity(observed) == plan['inventory_sha256']
                            and any(t['base'] == target['base'] and t['storage_id'] == target['storage_id'] and t['layout'] == '3'
                                    for t in contract.targets(observed)), 'cutover_migration_unknown', 'Queue layout 3 could not be confirmed.')
                    item['status'] = 'confirmed'
                    host.save_state(entry, state)
            require_cutover_quiescence(host, entry, operation_id)
            _cutover_phase(host, entry, state, 'database')
            pending = migration_status(host, entry, candidate)
            if progress['database'] == 'dispatched_or_unknown':
                require(not pending, 'migration_reconciliation', 'Database migration outcome needs reviewed reconciliation.')
            elif progress['database'] == 'not_started' and pending:
                try:
                    state['last_attempt']['backup'] = backup_database(host, entry, record['backup_operation_id'])
                finally:
                    state['backup'] = copy.deepcopy(host.load_state(entry).get('backup', {}))
                progress['database'] = 'dispatched_or_unknown'
                host.save_state(entry, state)
                app(host, entry, candidate, 'migrate', extra=('--database', 'primary'), timeout=900)
                require(not migration_status(host, entry, candidate), 'migration_reconciliation', 'Pending migrations remain after application.')
            elif progress['database'] == 'confirmed':
                require(not pending, 'migration_reconciliation', 'Previously confirmed database migrations changed.')
            progress['database'] = 'confirmed'
            host.save_state(entry, state)
            require_cutover_quiescence(host, entry, operation_id)
            _cutover_phase(host, entry, state, 'seed')
            require(progress['seed'] != 'dispatched_or_unknown', 'seed_reconciliation', 'Seeder outcome requires lead-reviewed forward recovery; it will not be repeated automatically.')
            if progress['seed'] == 'not_started':
                progress['seed'] = 'dispatched_or_unknown'
                host.save_state(entry, state)
                app(host, entry, candidate, 'db:seed', extra=('--database', 'primary'), timeout=900)
                progress['seed'] = 'confirmed'
                host.save_state(entry, state)
            require_cutover_quiescence(host, entry, operation_id)
            _cutover_phase(host, entry, state, 'switch')
            # Selecting a fenced candidate is the sole internal switch exception; no start is yet permitted.
            _select_cutover_candidate(host, entry, state, plan)
            _cutover_phase(host, entry, state, 'health')
            prepare_worker(host, entry, candidate)
            progress['drain'] = _new_drain(host, entry, starting=True)
            state['queue_compatibility']['admitted'] = {plan['target']['version']: plan['target']['files']['artisan']}
            state['queue_compatibility']['permission'] = 'admitted'
            host.save_state(entry, state)
            restart(host, entry, state, roles=plan['roles'])
            host.run(['systemctl', 'enable', *instance_units(entry).values()])
            if state['current'] != plan['target']:
                state['previous'] = plan['source']
            state['current'] = plan['target']
            state['status'] = 'healthy'
            queue_redis_narrowing(state, plan['redis_inventory'])
            result = _finish_cutover(host, entry, state, record, succeeded=True)
            warnings = narrow_redis_grants(host, registry, entry, plan['redis_inventory'], state)
            if warnings:
                result['warnings'] = warnings
            return result
        except (DeployError, OSError) as error:
            # Completion/receipt publication can be uncertain after healthy activation.
            # Reconcile the durable terminal state instead of undoing a completed start.
            if progress['outcome'] not in ('succeeded', 'failed'):
                _cutover_failure(host, registry, entry, state, plan, record, error)
            raise DeployError(error.code if isinstance(error, DeployError) else 'cutover_execution_failed',
                             'Cutover stopped; inspect its recorded phase and resume the same operation or request reviewed recovery.') from None


def _select_cutover_candidate(host, entry, state, plan):
    require(state['queue_compatibility']['permission'] == 'closed'
            and state['cutover']['database'] == state['cutover']['seed'] == 'confirmed'
            and all(q['status'] == 'confirmed' for q in state['cutover']['queues']),
            'cutover_start_unproven', 'Queue and database completion must precede candidate selection.')
    verified_release(host, entry, plan['target'])
    try:
        _switch_pointer(host, entry, plan['target']['version'])
    finally:
        _observe_cutover_selection(host, entry, state, plan)


def _observe_cutover_selection(host, entry, state, plan):
    version = current_version(host, entry)
    known = [item for item in (plan['source'], plan['target']) if item and item['version'] == version]
    require(known or version is None and plan['source'] is None,
            'cutover_selection_unknown', 'Selected release is outside the accepted operation.')
    selected = known[0] if known else None
    if selected:
        verified_release(host, entry, selected)
    if state['current'] != selected:
        state['current'] = selected
        if selected == plan['target']:
            state['previous'] = plan['source']
        host.save_state(entry, state)


def _observe_active_selection(host, entry, state):
    import cutover_contracts as contract
    active = state.get('cutover')
    if active:
        record = read_json(contract.private_path(host, entry, 'operation', active['operation_id']))
        plan = (_load_cutover_plan(host, entry, record['plan_token']) if active['action'] == 'cutover'
                else read_json(contract.private_path(host, entry, 'restore', active['operation_id'])))
        _observe_cutover_selection(host, entry, state, plan)


def cutover_status(host, entry):
    state = host.load_state(entry)
    progress = copy.deepcopy(state.get('cutover'))
    if progress is None:
        return None
    try:
        progress['executor'] = cutover_executor(host, entry, progress['operation_id'])
    except (DeployError, OSError):
        progress['executor'] = 'unknown'
    progress['next_action'] = cutover_next_action(state)
    progress['old_release_start_allowed'] = not progress['rollback_prohibited']
    return progress


def cutover_next_action(state):
    progress = state['cutover']
    if progress['drain_failed']:
        return 'cutover-drain-resolve'
    if progress['outcome'] not in ('succeeded', 'failed'):
        return 'redis-restore-resume' if progress['action'] == 'redis-restore' else 'cutover-resume'
    if state['queue_compatibility']['permission'] == 'closed':
        return 'cutover-plan'
    return 'restart' if state['status'] in ('stopped', 'degraded') else 'none'


def stop_cutover_executor(host, entry):
    progress = host.load_state(entry).get('cutover')
    if progress and progress['outcome'] not in ('succeeded', 'failed'):
        unit = cutover_unit(entry, progress['operation_id'])
        if cutover_executor(host, entry, progress['operation_id']) == 'running':
            host.run(['systemctl', 'stop', unit], timeout=110)
        require(cutover_executor(host, entry, progress['operation_id']) != 'running',
                'cutover_executor_unknown', 'The executor must exit before containment can take ownership.')


def admit_normal_release(host, registry, entry, state, candidate, manifest):
    """Ordinary activation cannot silently perform a coordinated layout transition."""
    import cutover_contracts as contract
    inventory = _cutover_inspection(host, registry, entry, candidate)
    physical = contract.targets(inventory)
    gate = state.get('queue_compatibility')
    if physical and state['current']:
        require(gate is not None and gate['floor'] == 3 and all(t['layout'] == '3' for t in physical),
                'queue_cutover_required', 'Review cutover-plan and apply it in the chosen downtime window.')
    if physical or gate:
        if gate is None:
            gate = {'schema': 1, 'floor': 3, 'permission': 'admitted', 'admitted': {}}
            state['queue_compatibility'] = gate
        require(gate['permission'] == 'admitted', 'queue_start_fenced', 'Resolve the recorded cutover before ordinary activation.')
        gate['admitted'][manifest['version']] = manifest['files']['artisan']
        host.save_state(entry, state)


def plan_redis_restore(host, project, profile, operation_id, snapshot_id, identity_text):
    """M4 private intake/lifecycle wrapper; M3 alone owns archive validation and restore."""
    import cutover_contracts as contract
    import upgrades
    require(valid_uuid(operation_id) and valid_uuid(snapshot_id) and isinstance(identity_text, str)
            and 0 < len(identity_text.encode()) <= 1024 * 1024, 'redis_restore_identity', 'Supply a snapshot UUID and private age identity.')
    _, entry = host.require_identity(project, profile)
    active = host.load_state(entry).get('cutover')
    if active:
        require(cutover_executor(host, entry, active['operation_id']) != 'running',
                'operation_locked', 'Contain the active executor before planning recovery.')
    with host.lock('host'), host.lock(profile['instance_id']):
        registry, entry = host.require_identity(project, profile)
        state = host.load_state(entry)
        active = state.get('cutover')
        require(not active or not active['drain_failed'], 'cutover_drain_review', 'Forced termination requires lead-reviewed recovery first.')
        if active:
            require(cutover_executor(host, entry, active['operation_id']) != 'running',
                    'operation_locked', 'Contain the active executor before planning recovery.')
            _drain_continuity(host, entry, state)
            _observe_active_selection(host, entry, state)
        path = contract.private_path(host, entry, 'restore', operation_id)
        if path.exists():
            saved = read_json(path)
            require(saved['snapshot_id'] == snapshot_id, 'operation_conflict', 'Restore operation identifies another snapshot.')
            return saved['module_plan']
        version = active['target'] if active else current_version(host, entry)
        require(version is not None, 'no_release', 'Restore planning requires a retained compatible candidate.')
        candidate = release_path(host, entry, version)
        manifest = read_json(candidate / 'manifest.json')
        verified_release(host, entry, manifest)
        compatibility.require_guards(host, entry)
        binding = _load_snapshot_binding(host, entry, snapshot_id)
        inventory = binding['inventory']
        contract.coverage(host, registry, entry, inventory)
        spaces = contract.scopes(inventory)
        require(bool(spaces), 'redis_restore_archive', 'This instance has no Redis job scope to restore.')
        operations = path.parent / 'operations'
        host.check_control_directory(operations)
        operations.mkdir(mode=0o700, exist_ok=True)
        intake = operations / operation_id
        host.check_control_directory(intake)
        intake.mkdir(mode=0o700, exist_ok=True)
        require(intake.stat().st_mode & 0o777 == 0o700 and (intake.stat().st_uid == 0 or host.paths.root != Path('/')),
                'redis_restore_identity', 'Restore intake must be an operation-owned private directory.')
        identity_file = check_path(intake / 'restore.identity', 'file')
        host.owned_write(identity_file, identity_text, mode=0o600)
        try:
            module_plan = _snapshot_module().plan_restore(host, registry, entry, {
                'schema': 1, 'operation_id': operation_id, 'snapshot_id': snapshot_id,
                'inventory_sha256': contract.inventory_identity(inventory), 'scopes': spaces,
                'identity_file': str(identity_file)})
            contract.fields(module_plan, 'schema operation_id snapshot scope_sha256 restore_keys remove_keys plan_token warning', 'redis_restore_archive')
            require(module_plan['schema'] == 1 and module_plan['operation_id'] == operation_id
                    and module_plan['snapshot']['snapshot_id'] == snapshot_id
                    and module_plan['snapshot']['instance_id'] == profile['instance_id']
                    and module_plan['snapshot'] == binding['evidence']
                    and module_plan['scope_sha256'] == contract.sha(spaces)
                    and contract.hash_value(module_plan['plan_token'])
                    and module_plan['warning'] == 'restore_may_replay_external_side_effects'
                    and all(type(module_plan[k]) is int and module_plan[k] >= 0 for k in ('restore_keys', 'remove_keys')),
                    'redis_restore_archive', 'Restore plan does not match the reviewed scope.')
            plan = {'schema': 1, 'action': 'redis-restore', 'operation_id': operation_id, 'snapshot_id': snapshot_id,
                    'module_plan': module_plan, 'host_id': entry['host_id'], 'instance_id': profile['instance_id'],
                    'source': state['current'], 'previous': state['previous'], 'target': manifest,
                    'configuration_revision': entry['configuration_revision'], 'configuration_fingerprint': _cutover_fingerprint(host, entry),
                    'topology_sha256': entry['topology']['sha256'], 'inventory': inventory,
                    'inventory_sha256': contract.inventory_identity(inventory), 'enabled_roles': enabled_roles(host, entry),
                    'tools_path': str(upgrades._installed(host, registry['tools'])), 'tools_sha256': registry['tools']['bundle_sha256'],
                    'replaces_operation': active['operation_id'] if active else None}
            contract.save_private(host, path, plan)
            return module_plan
        finally:
            # M3 retains its validated private plan/archive; never retain a decryption identity in M4 state.
            identity_file.unlink(missing_ok=True)


def apply_redis_restore(host, project, profile, token, operation_id, *, resume=False):
    import cutover_contracts as contract
    require(valid_uuid(operation_id) and contract.hash_value(token), 'invalid_operation', 'Restore requires reviewed PLAN_TOKEN and OPERATION.')
    _, entry = host.require_identity(project, profile)
    path = contract.private_path(host, entry, 'restore', operation_id)
    require(path.exists(), 'redis_restore_archive', 'The reviewed restore plan is missing.')
    plan = read_json(path)
    require(plan['module_plan']['plan_token'] == token, 'operation_conflict', 'Restore token belongs to another operation.')
    record_path = contract.private_path(host, entry, 'operation', operation_id)
    if cutover_executor(host, entry, operation_id) == 'running':
        return {'ok': True, 'operation_id': operation_id, 'outcome': 'running'}
    with host.lock('host'), host.lock(profile['instance_id']):
        registry, entry = host.require_identity(project, profile)
        state = host.load_state(entry)
        if record_path.exists():
            record = read_json(record_path)
            require(record.get('action') == 'redis-restore' and record['plan_token'] == token,
                    'operation_conflict', 'Operation ID identifies another request.')
            if record.get('result'):
                return record['result']
            require(resume, 'cutover_pending', 'Resume the same accepted restore operation.')
            _verify_cutover_inputs(host, registry, entry, plan)
            state = _recover_cutover_acceptance(host, entry, state, record)
            if state['cutover']['outcome'] in ('succeeded', 'failed'):
                progress = state['cutover']
                return _finish_cutover(host, entry, state, record, succeeded=progress['outcome'] == 'succeeded', error=progress['error'])
            require(not state['cutover']['drain_failed'], 'cutover_drain_review', 'Forced termination requires review.')
            _drain_continuity(host, entry, state)
            state['cutover'].update(hold=False, outcome='running')
            host.save_state(entry, state)
        else:
            require(not resume, 'cutover_operation_missing', 'There is no accepted restore to resume.')
            _observe_active_selection(host, entry, state)
            require(state['current'] == plan['source'] and state['previous'] == plan['previous'],
                    'redis_restore_stale_plan', 'Selected artifact changed since restore planning.')
            before = copy.deepcopy(state)
            active = state.get('cutover')
            require((active['operation_id'] if active else None) == plan['replaces_operation'],
                    'redis_restore_stale_plan', 'Active recovery changed since restore planning.')
            if active:
                _drain_continuity(host, entry, state)
                require(cutover_executor(host, entry, active['operation_id']) != 'running'
                        and not active['drain_failed'], 'operation_locked', 'The prior executor has not released ownership.')
                state.pop('cutover')
                if state.get('last_attempt'):
                    state['last_attempt'].update(outcome='failed', finished_at=now(), error='reviewed_restore')
            _verify_cutover_inputs(host, registry, entry, plan)
            begin(host, entry, state, 'deploy', plan['target']['version'], metadata={'roles': plan['enabled_roles']}, persist=False)
            state['queue_compatibility'] = {'schema': 1, 'floor': 3, 'permission': 'closed', 'admitted': {}}
            state['cutover'] = {'schema': 1, 'operation_id': operation_id, 'plan_token': token, 'action': 'redis-restore',
                                'source': (plan['source'] or {}).get('version'), 'target': plan['target']['version'],
                                'phase': 'prepared', 'outcome': 'running', 'started_at': now(), 'rollback_prohibited': True,
                                'drain_failed': False, 'hold': False, 'queues': [], 'database': 'not_started', 'seed': 'not_started', 'error': None}
            state['cutover']['drain'] = _new_drain(host, entry)
            record = {'schema': 1, 'action': 'redis-restore', 'operation_id': operation_id, 'plan_token': token,
                      'snapshot_id': str(uuid.uuid4()), 'restore_dispatched': False,
                      'superseded_progress': copy.deepcopy(active)}
            if active and active.get('drain_resolutions'):
                state['cutover']['drain_resolutions'] = copy.deepcopy(active['drain_resolutions'])
            _accept_cutover_state(host, entry, before, state, record)
    return _submit_cutover(host, entry, plan, operation_id)


def _execute_redis_restore(host, registry, entry, state, record):
    import cutover_contracts as contract
    operation_id = record['operation_id']
    plan = read_json(contract.private_path(host, entry, 'restore', operation_id))
    progress = state['cutover']
    try:
        require(not progress['hold'] and not progress['drain_failed'], 'cutover_drain_review', 'Restore is held for review.')
        _verify_cutover_inputs(host, registry, entry, plan)
        _cutover_stop(host, entry, state)
        require_cutover_quiescence(host, entry, operation_id)
        if 'safety_snapshot' not in progress:
            require(not record['restore_dispatched'], 'redis_restore_reconciliation', 'Original safety snapshot evidence is missing.')
            _cutover_phase(host, entry, state, 'snapshot')
            request = _snapshot_request(entry, plan, record)
            captured = _snapshot_module().capture(host, registry, entry, request)
            contract.snapshot_evidence(captured, request, entry)
            require(captured['captured_at'] >= progress['started_at'], 'cutover_snapshot_evidence', 'Safety snapshot predates the restore window.')
            _save_snapshot_binding(host, entry, plan, record, captured)
            progress['safety_snapshot'] = captured
            host.save_state(entry, state)
        _cutover_phase(host, entry, state, 'queues')
        request = {'schema': 1, 'operation_id': operation_id, 'plan_token': record['plan_token'],
                   'confirm': True, 'safety_snapshot': progress['safety_snapshot']}
        resume = record['restore_dispatched']
        record['restore_dispatched'] = True
        contract.save_private(host, contract.private_path(host, entry, 'operation', operation_id), record)
        module = _snapshot_module()
        result = (module.resume_restore if resume else module.apply_restore)(host, registry, entry, request)
        contract.fields(result, 'schema status operation_id snapshot_id safety_snapshot_id scope_sha256 completed_at restored_keys removed_keys', 'redis_restore_reconciliation')
        require(result['schema'] == 1 and result['status'] == 'complete' and result['operation_id'] == operation_id
                and result['snapshot_id'] == plan['snapshot_id'] and result['safety_snapshot_id'] == progress['safety_snapshot']['snapshot_id']
                and result['scope_sha256'] == plan['module_plan']['scope_sha256'] and valid_timestamp(result['completed_at'])
                and all(type(result[k]) is int and result[k] >= 0 for k in ('restored_keys', 'removed_keys')),
                'redis_restore_reconciliation', 'Restore result is not confirmed for this operation.')
        progress['restore'] = result
        state['status'] = 'unknown'
        return _finish_cutover(host, entry, state, record, succeeded=True)
    except (DeployError, OSError) as error:
        if progress['outcome'] not in ('succeeded', 'failed'):
            progress.update(outcome='needs_reconciliation', error=error.code if isinstance(error, DeployError) else 'redis_restore_reconciliation')
            state['queue_compatibility']['permission'] = 'closed'
            state['status'] = 'unknown'
            host.save_state(entry, state)
        raise
