"""Verified SSH release activation, instance-scoped operations and recovery.

Provisioning is deliberately absent from this module. A release cannot install
packages, change shared services, rotate credentials or replace deployment tools.
The one exception is its own instance's Redis key grants: they follow the
release's verified `redis:keys` inventory through provisioning's owned reconcile.
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
import urllib.request
import uuid

from contracts import (DeployError, LAYOUT_VERSION, SCHEMA, VERSION_PATTERN,
                       atomic_write, check_path, digest, dump_env, now, parse_env,
                       parse_json, read_json, require, valid_timestamp)
from runtime_config import PIPELINE_KEYS, validate_runtime_environment
from topology import instance_units
from host import OperationTimeout, wall_clock_budget
from redis_acl import granted_redis_patterns, reconcile_redis_grants, redis_grant_patterns
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
    stop(host, entry)
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


def release_redis_patterns(host, entry, release):
    """Require the release's exact inventory; v2 has no broad legacy grants."""
    return redis_grant_patterns(entry, parse_json(app(host, entry, release, "redis:keys").stdout))


def widen_redis_grants(host, registry, entry, patterns):
    """Before a switch, grant what both the running and the next release need."""
    granted = granted_redis_patterns(entry)
    if not set(patterns) <= set(granted):
        reconcile_redis_grants(host, registry, entry, [*granted, *patterns])


def narrow_redis_grants(host, registry, entry, patterns):
    """After a verified switch; a failure leaves the wider grant for the next deploy."""
    if sorted(granted_redis_patterns(entry)) == sorted(set(patterns)) and not entry["provisioning"].get("redis_acl_pending"):
        return []
    try:
        reconcile_redis_grants(host, registry, entry, patterns)
        return []
    except (DeployError, OSError, KeyError, TypeError, ValueError):
        # The verified switch already succeeded; never turn this into its failure.
        return ["redis_grants_not_narrowed"]


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
    """Require F3's durable PITR recovery point before running migrations."""
    return backup.recovery_point(host, entry, attempt_id)


def restart(host, entry):
    units = instance_units(entry)
    background = [units[role] for role in ("scheduler", "worker") if role in units]
    if background:
        host.run(["systemctl", "stop", *background], timeout=100)
    host.run(["systemctl", "reset-failed", *units.values()], check=False)
    host.run(["systemctl", "restart", units["http"]], timeout=60)
    if ready(host, entry, include_background=False):
        for role in ("worker", "scheduler"):
            if role in units:
                host.run(["systemctl", "start", units[role]], timeout=60)


def stop(host, entry):
    units = instance_units(entry)
    # Stop admission/maintenance and drain workers before stopping HTTP.
    background = [units[role] for role in ("scheduler", "worker") if role in units]
    if background:
        host.run(["systemctl", "stop", *background], timeout=100)
    host.run(["systemctl", "stop", units["http"]], timeout=60)


def services(host, entry):
    """Bounded, payload-free live topology for the status command."""
    result = {}
    for role, unit in instance_units(entry).items():
        found = host.run(["systemctl", "show", unit, "--property=LoadState,ActiveState,SubState,MainPID,NRestarts"], check=False, timeout=5)
        values = dict(line.split("=", 1) for line in found.stdout.splitlines() if "=" in line)
        result[role] = {"unit": unit, **{key: values.get(key) for key in ("LoadState", "ActiveState", "SubState", "MainPID", "NRestarts")}}
    return result


def ready(host, entry, timeout=90, include_background=True):
    try:
        with wall_clock_budget(timeout):
            deadline = time.monotonic() + timeout
            if not _ready(host, entry, deadline):
                return False
            if not include_background:
                return True
            while time.monotonic() < deadline:
                live = services(host, entry)
                children = [live[role] for role in ("worker", "scheduler") if role in live]
                if any(item["ActiveState"] == "failed" or item["NRestarts"] not in ("0", None) for item in children):
                    return False
                if all(item["ActiveState"] == "active" and (item["MainPID"] or "").isdigit() and int(item["MainPID"]) > 0 for item in children):
                    return True
                time.sleep(min(0.1, max(0, deadline - time.monotonic())))
            return False
    except (OperationTimeout, DeployError, OSError):
        return False


def _ready(host, entry, deadline):
    def remaining(maximum):
        available = deadline - time.monotonic()
        if available <= 0:
            raise OperationTimeout("readiness budget exhausted")
        return min(maximum, available)

    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
    initial = host.run(["systemctl", "show", instance_units(entry)["http"], "--property=NRestarts", "--value"], check=False, timeout=remaining(5)).stdout.strip()
    if not initial.isdigit():
        return False
    while time.monotonic() < deadline:
        result = host.run(["systemctl", "show", instance_units(entry)["http"], "--property=ActiveState", "--property=NRestarts", "--property=MainPID"], check=False, timeout=remaining(5))
        state = dict(line.split("=", 1) for line in result.stdout.splitlines() if "=" in line)
        if result.returncode or state.get("ActiveState") == "failed" or state.get("NRestarts", initial) != initial:
            return False
        try:
            with opener.open(f"http://127.0.0.1:{entry['allocations']['http_port']}/health/ready", timeout=remaining(3)) as response:
                payload = response.read(4097)
                body = parse_json(payload) if len(payload) <= 4096 else None
                if (state.get("ActiveState") == "active" and response.status == 200
                        and isinstance(body, dict) and set(body) == {"ready"} and body["ready"] is True):
                    pid = state.get("MainPID", "")
                    listeners = host.run(["ss", "-Hltpn", f"sport = :{entry['allocations']['http_port']}"], check=False, timeout=remaining(5))
                    if pid.isdigit() and int(pid) > 0 and listeners.returncode == 0 and f"pid={pid}," in listeners.stdout:
                        return True
        except OperationTimeout:
            raise
        except (OSError, ValueError):
            pass
        time.sleep(remaining(1))
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


def check_live_state(host, entry, state):
    current = current_version(host, entry)
    require(current == (state.get("current") or {}).get("version"), "state_drift", "Recorded state differs from the live release; reconcile before mutation.")


def restore_code(host, entry, state, current, previous, recovered_status):
    """Restore verified code only; never reverse database work."""
    verified_release(host, entry, current)
    if current_version(host, entry) != current["version"]:
        switch_to(host, entry, current["version"])
    state["current"], state["previous"] = current, previous
    restart(host, entry)
    state["status"] = recovered_status if ready(host, entry) else "failed"


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
    backups = host.paths.instance_state(entry["name"]) / "backups"
    if backups.exists():
        dumps = sorted((p for p in backups.iterdir() if p.suffix in (".dump", ".partial")
                        and valid_version(p.stem) and p.is_file() and not p.is_symlink()), reverse=True)
        for path in dumps[keep["pre_migration_dumps"]:]:
            if path.stem != (state.get("last_attempt") or {}).get("id"):
                path.unlink()


def activate(host, project, profile, archive, checksum):
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
            attempt = begin(host, entry, state, "deploy", version)
            prior_current, prior_previous = state["current"], state["previous"]
            switched = False
            try:
                native_runtime(host, entry)
                protect_maps(host, entry, release)
                app(host, entry, release, "config:check")
                phase(host, entry, state, "redis")
                redis_patterns = release_redis_patterns(host, entry, release)
                widen_redis_grants(host, registry, entry, redis_patterns)
                if migration_status(host, entry, release):
                    phase(host, entry, state, "backup")
                    attempt["backup"] = backup_database(host, entry, attempt["id"])
                    phase(host, entry, state, "migration")
                    app(host, entry, release, "migrate", extra=("--database", "primary"), timeout=900)
                phase(host, entry, state, "seed")
                app(host, entry, release, "db:seed", extra=("--database", "primary"), timeout=900)
                phase(host, entry, state, "switch")
                switch_to(host, entry, version)
                switched = True
                phase(host, entry, state, "health")
                host.run(["systemctl", "enable", *instance_units(entry).values()])
                restart(host, entry)
                require(ready(host, entry), "readiness_failed", "Candidate did not become ready.")
                if (state["current"] or {}).get("version") != version:
                    state["previous"] = state["current"]
                state["current"] = manifest
                state["status"] = "healthy"
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
                finish(host, entry, state, "needs_reconciliation" if failure_phase in ("migration", "seed") else "failed", code)
                try:
                    prune(host, entry, state)
                except (DeployError, OSError):
                    pass  # retained diagnostics are safer than masking failure
                raise DeployError(code, "Deployment failed; status records the phase and recovery outcome. Inspect the instance before retrying.") from None
            result = {"ok": True, "instance": entry["name"], "version": version}
            warnings = narrow_redis_grants(host, registry, entry, redis_patterns)
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


def rollback(host, project, profile):
    _, entry = host.require_identity(project, profile)
    with host.lock("host"), host.lock(entry["profile"]["instance_id"]):
        registry, entry = host.require_identity(project, profile)
        validate_runtime_environment(host, entry)
        state = host.load_state(entry)
        require_idle(state)
        check_live_state(host, entry, state)
        previous = state.get("previous")
        require(previous is not None, "no_previous_release", "There is no previous successful release.")
        release = verified_release(host, entry, previous)
        original_current, original_previous = state["current"], state["previous"]
        verified_release(host, entry, original_current)
        app(host, entry, release, "config:check")
        # Rollback is compatible code only; no migrations or database reversal.
        begin(host, entry, state, "rollback", previous["version"])
        try:
            phase(host, entry, state, "redis")
            redis_patterns = release_redis_patterns(host, entry, release)
            widen_redis_grants(host, registry, entry, redis_patterns)
            phase(host, entry, state, "switch")
            switch_to(host, entry, previous["version"])
            restart(host, entry)
            require(ready(host, entry), "rollback_readiness", "Rollback failed its readiness check.")
            state["current"], state["previous"] = previous, state["current"]
            state["status"] = "rolled_back"
            finish(host, entry, state, "succeeded")
            result = {"ok": True, "version": previous["version"]}
            warnings = narrow_redis_grants(host, registry, entry, redis_patterns)
            if warnings:
                result["warnings"] = warnings
            return result
        except (DeployError, OSError) as error:
            code = error.code if isinstance(error, DeployError) else "filesystem_failed"
            try:
                restore_code(host, entry, state, original_current, original_previous, "healthy")
            except (DeployError, OSError):
                state["status"] = "unknown"
                finish(host, entry, state, "needs_reconciliation", "code_recovery_failed")
                raise DeployError("code_recovery_failed", "Rollback recovery could not be verified; inspect and reconcile the instance before retrying.") from None
            finish(host, entry, state, "failed", code)
            raise DeployError(code, "Rollback failed; status records the original code's recovery outcome.") from None


def reconcile(host, project, profile):
    _, entry = host.require_identity(project, profile)
    with host.lock("host"), host.lock(entry["profile"]["instance_id"]):
        registry, entry = host.require_identity(project, profile)
        validate_runtime_environment(host, entry)
        state = host.load_state(entry)
        attempt = state.get("last_attempt")
        require(attempt is not None, "no_attempt", "There is no operation to reconcile.")
        candidate = release_path(host, entry, attempt["release"]) if attempt.get("release") else None
        if candidate is not None:
            require(candidate.is_dir(), "release_missing", "The attempted release is missing; restore its verified archive before reconciliation.")
            candidate = verified_release(host, entry, read_json(candidate / "manifest.json"))
        active = current_version(host, entry)
        manifest = None
        if active:
            manifest = read_json(release_path(host, entry, active) / "manifest.json")
            verified_release(host, entry, manifest)
        else:
            require(not state.get("current"), "state_drift", "A recorded live release is missing; administrator inspection required.")
        # Only these deployment phases can follow database writes. A first
        # candidate interrupted during configuration validation must remain
        # recoverable even while an application setting is still missing.
        if candidate and attempt["action"] == "deploy" and attempt["phase"] in ("migration", "seed", "switch", "health"):
            migration_status(host, entry, candidate)
        if attempt.get("action") == "config":
            path = host.paths.instance_config(entry["name"]) / "app.env"
            revision = attempt.get("configuration_revision")
            current_hash = digest(path)
            applied = current_hash == attempt.get("configuration_sha256")
            unchanged = current_hash == attempt.get("previous_configuration_sha256")
            require(type(revision) is int and (applied or unchanged), "configuration_reconciliation", "Configuration outcome is ambiguous; inspect private configuration before reconciliation.")
            require(entry["configuration_revision"] in (revision - 1, revision), "configuration_reconciliation", "Configuration revision changed unexpectedly.")
            # A crash before rename leaves the previous file intact. Preserve
            # the monotonic registry revision and acknowledge that exact outcome.
            if applied:
                entry["configuration_revision"] = revision
            state["configuration_revision"] = entry["configuration_revision"]
            attempt["configuration_applied"] = applied
            host.journal(registry, entry)
            if active:
                restart(host, entry)  # an interrupted change may precede restart
            candidate = path.with_name("app.env.next")
            check_path(candidate, "file")
            if candidate.exists():
                require(digest(candidate) == attempt["configuration_sha256"], "configuration_reconciliation", "Configuration candidate differs from its recorded intent.")
                candidate.unlink()
        if active:
            require(ready(host, entry, timeout=10), "reconciliation_health", "Live code is not ready; inspect the service before acknowledging recovery.")
            if (state.get("current") or {}).get("version") != active:
                state["previous"], state["current"] = state["current"], manifest
            state["status"] = "healthy"
        else:
            state["status"] = "never_deployed"
        finish(host, entry, state, "reconciled")
        return {"ok": True, "status": state["status"], "version": active}


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
    require(all(key in names for key in keys), "unknown_configuration", "The candidate does not declare this configuration key; no settings were changed.")


# Settings that validate only together (a key ring's ID and material) change in
# one validated revision; each change set stays small and explicit.
MAX_CONFIGURATION_CHANGES = 16


def config_set(host, project, profile, changes):
    require(isinstance(changes, dict) and 0 < len(changes) <= MAX_CONFIGURATION_CHANGES, "invalid_configuration", "Name between one and sixteen configuration keys to change together.")
    for key in changes:
        require(isinstance(key, str) and key not in PIPELINE_KEYS and not key.startswith(("DEPLOY_", "VITE_")), "owned_configuration", "Identity, infrastructure and seed credentials cannot be edited by generic configuration commands.")
    dump_env(changes)
    _, entry = host.require_identity(project, profile)
    # Shared registry revision and instance env are reconciled under both locks,
    # using the same host -> instance ordering as bootstrap.
    with host.lock("host"), host.lock(entry["profile"]["instance_id"]):
        registry, entry = host.require_identity(project, profile)
        values = validate_runtime_environment(host, entry)
        state = host.load_state(entry)
        require_idle(state)
        check_live_state(host, entry, state)
        manifest = state.get("current")
        if manifest is None:
            # A first candidate may require an additional application setting.
            # Validate against those retained immutable bytes without starting
            # an unactivated release or uploading private settings with code.
            version = (state.get("last_attempt") or {}).get("release")
            require(version is not None, "no_release", "Upload the first candidate with deploy before editing application configuration.")
            manifest = read_json(release_path(host, entry, version) / "manifest.json")
        live = verified_release(host, entry, manifest)
        path = host.paths.instance_config(entry["name"]) / "app.env"
        if all(values.get(key) == value for key, value in changes.items()):
            return {"ok": True, "unchanged": True}
        values.update(changes)
        candidate = path.with_name("app.env.next")
        require(not candidate.exists(), "configuration_reconciliation", "A previous configuration candidate remains; inspect it before retrying.")
        payload = dump_env(values)
        prior_status = state["status"]
        # Durable intent precedes even candidate creation/validation, so every
        # interruption can reconcile the previous or target bytes explicitly.
        begin(host, entry, state, "config", manifest["version"], metadata={
            "configuration_revision": entry["configuration_revision"] + 1,
            "configuration_sha256": hashlib.sha256(payload.encode()).hexdigest(),
            "previous_configuration_sha256": digest(path)})
        try:
            host.owned_write(candidate, payload, 0o640, group=entry["name"])
            app(host, entry, live, "config:check", candidate)
            require_configuration_keys(host, entry, live, candidate, list(changes))
            phase(host, entry, state, "configuration")
            os.replace(candidate, path)
            directory = os.open(path.parent, os.O_RDONLY)
            try:
                os.fsync(directory)
            finally:
                os.close(directory)
            entry["configuration_revision"] += 1
            entry["updated_at"] = registry["updated_at"] = now()
            host.journal(registry, entry)
            state["configuration_revision"] = entry["configuration_revision"]
            if state.get("current"):
                restart(host, entry)
                state["status"] = "healthy" if ready(host, entry) else "failed"
            else:
                state["status"] = "never_deployed"
            healthy = state["status"] in ("healthy", "never_deployed")
            finish(host, entry, state, "succeeded" if healthy else "failed", None if healthy else "configuration_readiness")
            require(healthy, "configuration_readiness", "Configuration was saved but readiness failed; correct it explicitly.")
            return {"ok": True, "configuration_revision": entry["configuration_revision"]}
        except (DeployError, OSError) as error:
            if state["last_attempt"]["outcome"] == "running":
                unchanged = digest(path) == state["last_attempt"]["previous_configuration_sha256"]
                state["status"] = prior_status if unchanged else "failed"
                finish(host, entry, state, "failed" if unchanged else "needs_reconciliation",
                       error.code if isinstance(error, DeployError) else "filesystem_failed")
            raise
        finally:
            if candidate.exists():
                candidate.unlink()


if __name__ == "__main__":
    if len(sys.argv) == 4 and sys.argv[1] == "copy-runtime-maps":
        copy_runtime_maps(Path(sys.argv[2]), Path(sys.argv[3]))
    elif len(sys.argv) >= 3 and sys.argv[1] == "prune-runtime-maps":
        prune_runtime_maps(Path(sys.argv[2]), set(sys.argv[3:]))
    else:
        raise DeployError("invalid_operation", "Unknown release helper operation.")
