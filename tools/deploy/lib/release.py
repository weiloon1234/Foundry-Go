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

from contracts import (DeployError, LAYOUT_VERSION, SCHEMA, VERSION_PATTERN,
                       check_path, digest, dump_env, now,
                       parse_env, parse_json, read_json, require, valid_timestamp, valid_uuid, instance_paths)
from runtime_config import (ENCRYPTION_KEYS, PIPELINE_KEYS, validate_runtime_environment, candidate_values,
                            rotation_changes)
from topology import instance_units
from host import OperationTimeout, wall_clock_budget
from redis_acl import (granted_redis_patterns, granted_redis_channels, reconcile_redis_grants,
                       redis_grant_patterns, redis_channel_patterns)
import backup

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
            destination.mkdir(mode=0o755, parents=True)
            for item, name in members:
                if not item.isfile():
                    continue
                target = destination / name
                check_path(target, "file")
                if name.startswith("private-source-maps/"):
                    # Private from the first write, including failed extraction.
                    (destination / "private-source-maps").mkdir(mode=0o700, exist_ok=True)
                target.parent.mkdir(parents=True, exist_ok=True)
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


def app(host, entry, release, command, env_file=None, extra=(), timeout=90):
    config = env_file or host.paths.instance_config(entry["name"]) / "app.env"
    check_path(config, "file")
    return host.run(["runuser", "-u", entry["name"], "--", str(release / "artisan"), command,
                     "--config", str(release / "config/deploy.toml"), "--env-file", str(config), *extra],
                    env={"WEB__PUBLIC_DIR": str(release / "public")}, timeout=timeout,
                    cwd=check_path(host.paths.data(entry["name"]), "directory"))


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
    for role, unit in instance_units(entry).items():
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


def stop_background(host, entry, timeout=100):
    units = instance_units(entry)
    background = [units[role] for role in ("scheduler", "worker") if role in units]
    if background:
        host.run(["systemctl", "stop", *background], timeout=timeout)


def _remaining(deadline, maximum=5):
    available = deadline - time.monotonic()
    if available <= 0:
        raise OperationTimeout("readiness budget exhausted")
    return min(maximum, available)


def prepare_worker(host, entry):
    """Select the explicit worker connection from the selected release/config."""
    if "worker" not in instance_units(entry):
        return
    version = current_version(host, entry)
    require(version is not None, "worker_configuration", "A selected release is required for the worker.")
    configuration = host.paths.instance_config(entry["name"])
    values = parse_env(check_path(configuration / "app.env", "file").read_text())
    key = "APP__WORKER__CONNECTION"
    if key + "_FILE" in values:
        from runtime_config import _read_value_file
        connection = _read_value_file(host, entry, values[key + "_FILE"])
    elif key in values:
        connection = values[key]
    else:
        with (release_path(host, entry, version) / "config/deploy.toml").open("rb") as source:
            settings = tomllib.load(source)
        connection = settings.get("app", {}).get("worker", {}).get("connection")
    require(isinstance(connection, str) and re.fullmatch(r"[a-z0-9][a-z0-9_.-]{0,127}", connection),
            "worker_configuration", "Declare app.worker.connection explicitly for the worker role.")
    host.owned_write(configuration / "worker.env", dump_env({"FOUNDRY_WORKER_CONNECTION": connection}),
                     mode=0o640, group=entry["name"])


def restart(host, entry, state=None):
    """HTTP and all declared background roles share one readiness deadline."""
    ensure_key_archive(host, entry, state if state is not None else host.load_state(entry))
    units = instance_units(entry)
    prepare_worker(host, entry)
    stop_background(host, entry)
    try:
        with wall_clock_budget(READINESS_SECONDS):
            deadline = time.monotonic() + READINESS_SECONDS
            host.run(["systemctl", "reset-failed", *units.values()], timeout=_remaining(deadline, 10))
            host.run(["systemctl", "restart", units["http"]], timeout=_remaining(deadline, 60))
            require(ready(host, entry, timeout=_remaining(deadline, READINESS_SECONDS), include_background=False),
                    "readiness_failed", "HTTP did not become ready; background roles remain stopped.")
            for role in ("worker", "scheduler"):
                if role in units:
                    host.run(["systemctl", "start", units[role]], timeout=_remaining(deadline, 60))
            require(ready(host, entry, timeout=_remaining(deadline, READINESS_SECONDS)),
                    "readiness_failed", "Required roles did not become ready within the activation deadline.")
    except OperationTimeout:
        raise DeployError("readiness_failed", "Required roles exhausted the activation deadline.") from None


def stop(host, entry):
    stop_background(host, entry)
    host.run(["systemctl", "stop", instance_units(entry)["http"]], timeout=60)


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


def ready(host, entry, timeout=90, include_background=True):
    try:
        with wall_clock_budget(timeout):
            return _ready(host, entry, time.monotonic() + timeout, include_background)
    except (OperationTimeout, DeployError, OSError, ValueError):
        return False


def _ready(host, entry, deadline, include_background=True):
    version = current_version(host, entry)
    if version is None:
        return False
    path = release_path(host, entry, version)
    verified_release(host, entry, read_json(path / "manifest.json"))
    units = instance_units(entry)
    if not include_background:
        units = {"http": units["http"]}
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
    tokens, stable_since = None, None
    while time.monotonic() < deadline:
        live = {role: service(host, unit, timeout=_remaining(deadline)) for role, unit in units.items()}
        if any(item["ActiveState"] == "failed" or item["NRestarts"] != "0" for item in live.values()):
            return False
        healthy = all(_matches_executable(item, path / "artisan") for item in live.values())
        if healthy:
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
    previous = state.get("last_attempt")
    require(not previous or previous.get("outcome") not in ("running", "needs_reconciliation"), "reconciliation_required", "An operation was interrupted; inspect status and reconcile before retrying.")


def begin(host, entry, state, action, version, metadata=None):
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


def restore_code(host, entry, state, current, previous, recovered_status):
    """Restore verified code only; never reverse database work."""
    verified_release(host, entry, current)
    if current_version(host, entry) != current["version"]:
        switch_to(host, entry, current["version"])
    state["current"], state["previous"] = current, previous
    restart(host, entry, state)
    state["status"] = recovered_status


def protect_maps(host, entry, release):
    source = release / "private-source-maps"
    host.run(["chown", "-R", "root:" + entry["name"], str(source)])
    for path in [source, *source.rglob("*")]:
        path.chmod(0o750 if path.is_dir() else 0o640)
    # The application can mutate its data directory. Never copy/chown there as
    # root: a concurrent symlink swap must not cross the service UID boundary.
    host.run(["runuser", "-u", entry["name"], "--", "python3", str(Path(__file__).resolve()),
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
    releases = host.paths.opt(entry["name"]) / "releases"
    candidates = sorted([p for p in releases.iterdir() if valid_version(p.name) and p.is_dir() and not p.is_symlink()], reverse=True)
    protected.update(p.name for p in candidates[:keep["server_releases"]])
    for path in candidates:
        if path.name not in protected:
            validate_manifest(read_json(path / "manifest.json"), entry)
            shutil.rmtree(check_path(path, "directory"))
    host.run(["runuser", "-u", entry["name"], "--", "python3", str(Path(__file__).resolve()),
              "prune-runtime-maps", str(host.paths.data(entry["name"]) / "private-source-maps"),
              *sorted(version for version in protected if version is not None)])
    for path in releases.glob(".stage-*"):
        if re.fullmatch(r"\.stage-[a-f0-9]{32}", path.name) and path.is_dir() and not path.is_symlink():
            shutil.rmtree(check_path(path, "directory"))


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
        staging = host.paths.opt(entry["name"]) / "releases" / (".stage-" + uuid.uuid4().hex)
        try:
            manifest = unpack_archive(Path(archive), staging, entry, checksum)
            version = manifest["version"]
            if state["current"] and state["current"]["version"] == version:
                require(state["current"] == manifest, "release_conflict", "An existing release ID has different content.")
                if state["status"] in ("healthy", "rolled_back"):
                    verified_release(host, entry, state["current"])
                    require(ready(host, entry, timeout=10), "readiness_failed", "Current release is no longer ready; inspect it before retrying.")
                    return {"ok": True, "unchanged": True, "instance": entry["name"], "version": version}
            release = release_path(host, entry, version)
            if release.exists():
                verified_release(host, entry, manifest)
            else:
                os.rename(staging, release)
            attempt = begin(host, entry, state, "deploy", version,
                            metadata={"recovery_operation_id": operation_id or str(uuid.uuid4())})
            prior_current, prior_previous = state["current"], state["previous"]
            switched = False
            try:
                native_runtime(host, entry)
                protect_maps(host, entry, release)
                app(host, entry, release, "config:check")
                redis_patterns = release_redis_inventory(host, entry, release)
                attempt["redis_inventory"] = redis_patterns
                phase(host, entry, state, "redis")
                widen_redis_grants(host, registry, entry, redis_patterns, state)
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
                host.run(["systemctl", "enable", *instance_units(entry).values()])
                restart(host, entry, state)
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
                state["status"] = "failed"
                try:
                    if failure_phase in ("switch", "health"):
                        # Rename may succeed before fsync or recording its
                        # return fails. Inspect the actual link before recovery.
                        switched = current_version(host, entry) == version
                    if switched and prior_current:
                        restore_code(host, entry, state, prior_current, prior_previous, "rolled_back")
                    elif switched:
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
                raise DeployError(code, "Deployment failed; status records the phase and recovery outcome. Inspect the instance before retrying.") from None
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
        if target == expected_current:
            require(ready(host, entry), "rollback_readiness", "Selected target is not ready.")
        original_current, original_previous = state["current"], state["previous"]
        resume_terminal_obligations(host, registry, entry, state)
        app(host, entry, release, "config:check")
        attempt = begin(host, entry, state, "rollback", target,
                        metadata={"operation_id": operation_id, "expected_current": expected_current})
        _save_operation(host, entry, attempt)
        if target == expected_current:
            state["status"] = "healthy"
            finish(host, entry, state, "succeeded")
            return {"ok": True, "unchanged": True, "version": target, "operation_id": operation_id}
        try:
            attempt["redis_inventory"] = release_redis_inventory(host, entry, release)
            phase(host, entry, state, "redis")
            widen_redis_grants(host, registry, entry, attempt["redis_inventory"], state)
            stop_background(host, entry)
            phase(host, entry, state, "switch")
            switch_to(host, entry, target)
            phase(host, entry, state, "health")
            restart(host, entry, state)
            state["current"], state["previous"] = manifest, original_current
            state["status"] = "rolled_back"
            queue_redis_narrowing(state, attempt["redis_inventory"])
            finish(host, entry, state, "succeeded")
        except (DeployError, OSError) as error:
            code = error.code if isinstance(error, DeployError) else "filesystem_failed"
            try:
                restore_code(host, entry, state, original_current, original_previous, "healthy")
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
        if attempt["action"] == "stop":
            stop(host, entry)
            state["status"] = "stopped"
            finish(host, entry, state, "reconciled")
            return {"ok": True, "status": "stopped", "version": active}
        if attempt["action"] == "deploy" and attempt["phase"] in ("validation", "redis", "backup"):
            resume_background_recovery(host, entry, state)
            resume_redis_transition(host, registry, entry, state)
            state["status"] = "failed"
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
                restart(host, entry, state)
                if (state.get("current") or {}).get("version") != active:
                    state["previous"], state["current"] = state["current"], manifest
                state["status"] = "healthy"
            else:
                state["status"] = "never_deployed"
        except (DeployError, OSError) as error:
            state["status"] = "failed"
            outcome = "failed_applied" if attempt["action"] == "config" and applied else "needs_reconciliation"
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


# Settings that validate only together (a key ring's ID and material) change in
# one validated revision; each change set stays small and explicit.
MAX_CONFIGURATION_CHANGES = 16


def _validate_changes(changes):
    require(isinstance(changes, dict) and 0 < len(changes) <= MAX_CONFIGURATION_CHANGES,
            "invalid_configuration", "Name between one and sixteen configuration keys to change together.")
    for key in changes:
        require(isinstance(key, str) and re.fullmatch(r"[A-Z][A-Z0-9_]*", key),
                "invalid_configuration", "Configuration key name is invalid.")
        logical = key.removesuffix("_FILE")
        require(not any(logical == owned or logical.startswith(owned + "__") for owned in PIPELINE_KEYS)
                and not key.startswith(("DEPLOY_", "FOUNDRY_", "VITE_")),
                "owned_configuration", "Identity, infrastructure and seed credentials cannot be edited by generic configuration commands.")
    dump_env({key: value for key, value in changes.items() if value is not None})


def config_set(host, project, profile, changes):
    _validate_changes(changes)
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
        path = _operation_path(host, entry, operation_id)
        state = host.load_state(entry)
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
    values = validate_runtime_environment(host, entry)
    state = host.load_state(entry)
    require_idle(state)
    check_live_state(host, entry, state)
    resume_terminal_obligations(host, registry, entry, state)
    manifest = state.get("current")
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
    prior_status = state["status"]
    roles_stopped = False
    attempt = begin(host, entry, state, "config", manifest["version"], metadata={
        "configuration_revision": entry["configuration_revision"] + 1,
        "configuration_sha256": hashlib.sha256(payload.encode()).hexdigest(),
        "previous_configuration_sha256": digest(path),
        "rotation_operation_id": rotation_operation_id})
    try:
        host.owned_write(candidate, payload, 0o640, group=entry["name"])
        app(host, entry, live, "config:check", candidate)
        require_configuration_keys(host, entry, live, candidate, list(changes))
        if updated == values:
            state["status"] = prior_status
            finish(host, entry, state, "succeeded")
            return {"ok": True, "unchanged": True, "configuration_revision": entry["configuration_revision"]}
        attempt["redis_inventory"] = release_redis_inventory(host, entry, live, candidate)
        phase(host, entry, state, "redis")
        widen_redis_grants(host, registry, entry, attempt["redis_inventory"], state)
        if state.get("current"):
            roles_stopped = True
            stop(host, entry)
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
        entry["configuration_revision"] = attempt["configuration_revision"]
        entry["updated_at"] = registry["updated_at"] = now()
        host.journal(registry, entry)
        state["configuration_revision"] = entry["configuration_revision"]
        attempt["configuration_applied"] = True
        phase(host, entry, state, "health")
        ensure_key_archive(host, entry, state)
        if state.get("current"):
            restart(host, entry, state)
            state["status"] = "healthy"
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
            state["status"] = prior_status if unchanged else "failed"
            # Repair the receipt only for an exact known selection. A failed
            # registry write stays unresolved; an unhealthy applied revision does not.
            if applied:
                entry["configuration_revision"] = attempt["configuration_revision"]
                host.journal(registry, entry)
                state["configuration_revision"] = entry["configuration_revision"]
            outcome = "failed_applied" if applied else "failed" if unchanged else "needs_reconciliation"
            if unchanged and roles_stopped:
                try:
                    restart(host, entry, state)
                    state["status"] = "healthy"
                except (DeployError, OSError):
                    state["status"] = "failed"
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
