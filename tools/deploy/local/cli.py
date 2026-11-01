#!/usr/bin/env python3
"""Local interactive entry point: public profiles, Docker builds and direct SSH."""
import argparse
from contextlib import contextmanager
import difflib
from datetime import datetime, timezone
import fcntl
import getpass
import hashlib
import json
import os
from pathlib import Path
import re
import shlex
import shutil
import subprocess
import sys
import tarfile
import tempfile
import uuid

TOOLS_ROOT = Path(__file__).resolve().parents[1]
ROOT: Path | None = None
sys.path.insert(0, str(TOOLS_ROOT / "lib"))
from edge import edge_findings

from contracts import (DeployError, PREFIX, SCHEMA, PROFILE_SCHEMA, LAYOUT_VERSION, VERSION_PATTERN, atomic_json, check_path, digest, identity, instance_name, now, read_json, slug,
                       validate_profile, validate_project)
from release import MAX_CONFIGURATION_CHANGES, validate_manifest
from topology import load_topology
from upgrades import protocol_metadata, tool_bundle

INSTALLED = f"/usr/local/sbin/{PREFIX}-host"
COMMANDS = {
    "bootstrap": "inspect and prepare a project/environment on a shared host",
    "deploy": "interactive deployment, status, rollback and configuration",
    "release": "build a local Docker release without contacting a server",
}
ACTIONS = ("deploy", "status", "rollback", "restart", "stop", "logs", "config", "reconcile", "report", "sync", "tools-plan", "tools-apply", "upgrade-resume")


class LocalError(Exception):
    pass


def say(message):
    print(message, file=sys.stderr)


def ask(label, default=None, secret=False):
    if not sys.stdin.isatty():
        raise LocalError(f"{label} needs a terminal; supply ENVIRONMENT/ACTION where supported or initialize missing settings interactively")
    prompt = label + (f" [{default}]" if default is not None else "") + ": "
    if secret:
        answer = getpass.getpass(prompt)
    else:
        print(prompt, end="", file=sys.stderr, flush=True)
        answer = input()
    return answer if answer else default or ""


def confirm(label):
    # Explicit automation approval still follows the printed, concrete plan.
    if os.environ.get("CONFIRM") == "yes":
        return
    if ask(label + " (type yes)") != "yes":
        raise LocalError("operation cancelled")


def execute(args, *, input=None, capture=True, timeout=None):
    try:
        result = subprocess.run(args, input=input, stdout=subprocess.PIPE if capture else sys.stderr,
                                stderr=subprocess.PIPE if capture else sys.stderr, timeout=timeout)
    except FileNotFoundError as error:
        raise LocalError(f"Required tool is unavailable: {args[0]}") from error
    except subprocess.TimeoutExpired as error:
        raise LocalError(f"Command timed out: {args[0]}; inspect state before retrying") from error
    if result.returncode:
        # Never echo command arguments, stdin, or arbitrary stderr: config values
        # travel only through stdin and remote failures may contain private data.
        raise LocalError(f"{args[0]} failed (exit {result.returncode}); inspect the connection or service with the operator account")
    return result.stdout.decode("utf-8") if capture else ""


@contextmanager
def lock(path):
    if path.is_symlink():
        raise LocalError("lock path must not be a symlink")
    check_path(path, "file")
    path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    descriptor = os.open(path, os.O_CREAT | os.O_RDWR | os.O_NOFOLLOW, 0o600)
    try:
        try:
            fcntl.flock(descriptor, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError as error:
            raise LocalError("another local deployment/build operation owns this lock") from error
        yield
    finally:
        os.close(descriptor)


def local_settings():
    path = ROOT / ".deploy" / "settings.json"
    value = read_json(path) if path.exists() else {"schema": SCHEMA, "hosts": {}}
    if not isinstance(value, dict) or value.get("schema") != SCHEMA or not isinstance(value.get("hosts"), dict):
        raise LocalError("invalid .deploy/settings.json")
    if any(not isinstance(item, dict) or set(item) != {"target", "key_path"}
           or any(not isinstance(v, str) for v in item.values()) for item in value["hosts"].values()):
        raise LocalError("invalid personal SSH settings")
    return value


def save_settings(settings):
    atomic_json(ROOT / ".deploy" / "settings.json", settings, mode=0o600)


def project_config(mode):
    path = ROOT / "deploy" / "project.json"
    value = read_json(path) if path.exists() else {"schema": SCHEMA, "id": None, "slug": None}
    if value == {"schema": SCHEMA, "id": None, "slug": None}:
        if mode != "bootstrap":
            raise LocalError("project identity is not initialized; run make bootstrap")
        if any((ROOT / "deploy" / "environments").glob("*.json")):
            raise LocalError("cannot create a new product identity while existing environment profiles remain; preserve the old identity or explicitly remove copied profiles first")
        suggestion = re.sub(r"[^a-z0-9-]", "-", ROOT.name.lower()).strip("-")[:20]
        value = {"schema": SCHEMA, "id": str(uuid.uuid4()), "slug": ask("Project slug (stable product identity)", suggestion)}
        validate_project(value)
        atomic_json(path, value)
        say("Created deploy/project.json; commit its identity with this product's code.")
    return validate_project(value)


def select_environment(mode, settings):
    directory = ROOT / "deploy" / "environments"
    existing = sorted(path.stem for path in directory.glob("*.json"))
    selected = os.environ.get("ENVIRONMENT")
    if not selected:
        say("Environments: " + (", ".join(existing) if existing else "none"))
        selected = ask("Environment", settings.get("last_environment", existing[0] if existing else "production"))
    if not slug(selected):
        raise LocalError("environment must be 1–20 lowercase letters, digits or hyphens, starting with a letter")
    if mode != "bootstrap" and selected not in existing:
        raise LocalError("environment does not exist; run make bootstrap to create it")
    settings["last_environment"] = selected
    save_settings(settings)
    return selected


def profile_config(environment, project):
    path = ROOT / "deploy" / "environments" / f"{environment}.json"
    if path.exists():
        profile = validate_profile(read_json(path), project)
        if profile["environment"] != environment:
            raise LocalError("profile environment does not match its filename")
        for sibling in path.parent.glob("*.json"):
            if sibling == path:
                continue
            other = validate_profile(read_json(sibling), project)
            if other["environment"] != sibling.stem:
                raise LocalError("an environment profile does not match its filename")
            if other["instance_id"] == profile["instance_id"]:
                raise LocalError("environment profiles share a copied instance UUID; create each environment with make bootstrap")
        return profile, False
    runtime = {"prod": "production", "uat": "staging", "dev": "development", "test": "testing"}.get(environment, environment)
    if runtime not in ("production", "staging", "testing", "development"):
        runtime = "staging"
    profile = {
        "schema": PROFILE_SCHEMA, "environment": environment, "instance_id": str(uuid.uuid4()),
        "host": {"address": ask("Server's canonical hostname or IP (not a personal SSH alias)"),
                 "port": int(ask("SSH port", "22")), "host_id": None},
        "runtime_environment": ask("Application runtime environment", runtime),
        "domain": ask("Public domain"), "certbot_email": ask("Certificate contact email"),
        "architecture": "amd64", "public_env": {},
        "edge_origin_lockdown": True,
        "retention": {"server_releases": 5, "local_releases": 3, "pre_migration_dumps": 7},
        "backup_policy": {"interval_hours": 24, "daily": 7, "weekly": 4, "monthly": 12},
    }
    validate_profile(profile, project)
    atomic_json(path, profile)
    say(f"Created deploy/environments/{environment}.json; architecture will be discovered during inspection.")
    return profile, True


def save_profile(profile):
    atomic_json(ROOT / "deploy" / "environments" / (profile["environment"] + ".json"), profile)


class SSH:
    def __init__(self, profile, settings):
        host = profile["host"]
        host_key = f"{host['address']}:{host['port']}"
        personal = settings["hosts"].get(host_key)
        if personal is None:
            target = ask("Personal SSH alias or user@host", host["address"])
            key = ask("Private key path (blank uses ssh-agent / SSH config)")
            personal = {"target": target, "key_path": key}
            settings["hosts"][host_key] = personal
            save_settings(settings)
        target = personal.get("target", "")
        if not re.fullmatch(r"[A-Za-z0-9_][A-Za-z0-9_.@:%+\[\]-]*", target):
            raise LocalError("invalid personal SSH target")
        self.base = ["ssh", "-o", "StrictHostKeyChecking=yes", "-o", "BatchMode=yes",
                     "-o", "ConnectTimeout=15", "-o", "ServerAliveInterval=15",
                     "-o", "ServerAliveCountMax=3", "-p", str(host["port"])]
        key_path = personal.get("key_path", "")
        if key_path:
            self.base += ["-i", str(Path(key_path).expanduser())]
        effective = execute(self.base + ["-G", target], timeout=20)
        options = dict(line.split(" ", 1) for line in effective.splitlines() if " " in line)
        if options.get("hostname", "").lower().strip("[]") != host["address"].lower().strip("[]") or options.get("port") != str(host["port"]):
            raise LocalError("personal SSH alias resolves to a different hostname/port than the committed profile")
        self.base += [target]

    def command(self, command, *, input=None, timeout=60):
        return execute(self.base + [command], input=input, timeout=timeout)

    def request(self, action, request, tools=None):
        request = {**request, **protocol_metadata(TOOLS_ROOT)}
        invocation = (f"python3 -B {shlex.quote(tools + '/server/manage.py')} {shlex.quote(action)}"
                      if tools else f"{shlex.quote(INSTALLED)} {shlex.quote(action)}")
        remote = f'if [ "$(id -u)" -eq 0 ]; then {invocation}; else sudo -n {invocation}; fi'
        try:
            result = subprocess.run(self.base + [remote], input=json.dumps(request).encode(),
                                    stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=7200)
        except subprocess.TimeoutExpired as error:
            raise LocalError("remote operation timed out; use status and reconcile before retrying") from error
        try:
            value = json.loads(result.stdout)
        except (ValueError, UnicodeDecodeError) as error:
            raise LocalError("server returned no valid JSON; verify Python 3, known SSH host keys and root or passwordless sudo access") from error
        if isinstance(value, dict) and "error" in value:
            detail = value["error"]
            raise LocalError(f"{detail.get('code', 'remote_error')}: {detail.get('detail', 'server operation failed')}")
        if result.returncode:
            raise LocalError("server operation failed; inspect status before retrying")
        if not isinstance(value, dict):
            raise LocalError("server response must be a JSON object")
        return value

    def report(self):
        try:
            result = subprocess.run(self.base + [f"{PREFIX}-host-report --pretty"],
                                    stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=60)
        except subprocess.TimeoutExpired as error:
            raise LocalError("host report exceeded its deadline") from error
        try:
            value = json.loads(result.stdout)
        except (ValueError, UnicodeDecodeError) as error:
            raise LocalError("server returned no valid report; verify the installed report command and SSH access") from error
        if not isinstance(value, dict):
            raise LocalError("server report must be a JSON object")
        return value, result.returncode

    @contextmanager
    def temporary(self):
        remote = self.command(f"umask 077; mktemp -d /tmp/{PREFIX}.XXXXXXXXXX").strip()
        if not re.fullmatch(rf"/tmp/{PREFIX}\.[A-Za-z0-9]{{10}}", remote):
            raise LocalError("unexpected remote staging path")
        try:
            yield remote
        finally:
            try:
                # Only remove this call's exact random directory, without sudo.
                self.command("rm -rf -- " + shlex.quote(remote))
            except LocalError:
                say("Remote staging cleanup failed; the operator should remove the reported temporary directory after reconciliation.")

    @contextmanager
    def staged_tools(self):
        with self.temporary() as remote, tempfile.TemporaryFile() as archive:
            with tarfile.open(fileobj=archive, mode="w") as tar:
                source, files, _ = tool_bundle(TOOLS_ROOT)
                for path in files:
                    tar.add(path, arcname=path.relative_to(source), recursive=False)
            archive.seek(0)
            self.command("tar -xf - -C " + shlex.quote(remote), input=archive.read())
            yield remote

    def upload(self, archive, remote):
        # Stream the completed archive over SSH; no shell expansion or scp alias ambiguity.
        with archive.open("rb") as source:
            command = "umask 077; cat > " + shlex.quote(remote)
            try:
                result = subprocess.run(self.base + [command], stdin=source, stdout=subprocess.PIPE,
                                        stderr=subprocess.PIPE, timeout=3600)
            except subprocess.TimeoutExpired as error:
                raise LocalError("release upload timed out before activation") from error
        if result.returncode:
            raise LocalError("release upload failed before activation")


def display(value):
    print(json.dumps(value, indent=2, sort_keys=True))


def bootstrap_marker(project, profile, host_id):
    return {"schema": SCHEMA, "project_id": project["id"], "project_slug": project["slug"],
            "instance_id": profile["instance_id"], "environment": profile["environment"],
            "host": {**profile["host"], "host_id": host_id}}


def pending_bootstrap(project, profile):
    path = ROOT / ".deploy" / "bootstrap" / f"{profile['instance_id']}.json"
    check_path(path, "file")
    if not path.exists():
        return path, None
    value = read_json(path)
    pending_host = value.get("host") if isinstance(value, dict) else None
    binding = profile["host"]["host_id"] or (pending_host.get("host_id") if isinstance(pending_host, dict) else None)
    try:
        validate_profile({**profile, "host": {**profile["host"], "host_id": binding}}, project)
    except DeployError as error:
        raise LocalError("invalid pending bootstrap identity; inspect .deploy/bootstrap") from error
    if not binding or value != bootstrap_marker(project, profile, binding):
        raise LocalError("pending bootstrap belongs to a different project, instance or host; inspect .deploy/bootstrap")
    return path, binding


def bootstrap(project, profile, settings, fresh):
    action = os.environ.get("ACTION") or ask("Bootstrap action (prepare / inspect)", "prepare")
    if action not in {"prepare", "inspect"}:
        raise LocalError("bootstrap ACTION must be prepare or inspect")
    request = {"project": project, "profile": profile, "topology": load_topology(ROOT)}
    ssh = SSH(profile, settings)
    with ssh.staged_tools() as remote:
        inspected = ssh.request("inspect", request, remote)
        pending_path, pending_binding = pending_bootstrap(project, profile)
        binding = profile["host"]["host_id"] or pending_binding
        if binding and inspected.get("host_id") is not None and inspected["host_id"] != binding:
            raise LocalError("server identity differs from the committed environment binding")
        if binding and inspected.get("host_id") is None and not pending_binding:
            raise LocalError("bound server has no registry and no matching pending bootstrap; refuse to duplicate its host identity")
        # A pending marker is written before the first committed binding, so a
        # crash between those writes can recover only on the same canonical host.
        if pending_binding:
            profile["host"]["host_id"] = pending_binding
        architecture = inspected.get("architecture")
        if fresh or profile["host"]["host_id"] is None:
            if architecture not in {"amd64", "arm64"}:
                raise LocalError("host inspection returned an unsupported architecture")
            profile["architecture"] = architecture
            save_profile(profile)
        elif architecture != profile["architecture"]:
            raise LocalError("server architecture differs from the environment profile")
        if action == "inspect":
            display({"project": project, "profile": profile, "host": inspected})
            return
        plan = ssh.request("bootstrap-plan", request, remote)
        for finding in plan.get("findings", []):
            say(f"{finding['kind']}: {finding['code']}: {finding['detail']}")
        if not plan.get("ok") or any(f.get("kind") == "stop" for f in plan.get("findings", [])):
            raise LocalError("bootstrap is blocked by the findings above")
        confirm(f"Prepare {instance_name(project, profile)} on {profile['host']['address']}")
        binding = inspected.get("host_id") or plan.get("host_id")
        if not binding:
            raise LocalError("bootstrap plan did not provide a host identity")
        if profile["host"]["host_id"] and profile["host"]["host_id"] != binding:
            raise LocalError("bootstrap plan changed the committed host identity")
        profile["host"]["host_id"] = binding
        validate_profile(profile, project)
        atomic_json(pending_path, bootstrap_marker(project, profile, binding), mode=0o600)
        save_profile(profile)
        result = ssh.request("bootstrap-apply", {**request, "confirm": True, "plan_token": plan.get("plan_token")}, remote)
        if result.get("host_id") != binding:
            raise LocalError("bootstrap returned a different host identity; inspect the server")
        pending_path.unlink()
        display(result)


def build_request(project, profile):
    revision = execute(["git", "-C", str(ROOT), "rev-parse", "HEAD"]).strip()
    dirty = bool(execute(["git", "-C", str(ROOT), "status", "--porcelain", "--untracked-files=normal"]).strip())
    version = datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%SZ") + "-" + uuid.uuid4().hex[:12]
    return {"profile": profile, "manifest": {
        "schema": SCHEMA, "layout_version": LAYOUT_VERSION, "project_id": project["id"],
        "instance_id": profile["instance_id"], "instance": instance_name(project, profile),
        "environment": profile["environment"], "architecture": profile["architecture"],
        "version": version, "commit": revision, "tree_state": "dirty" if dirty else "clean",
        "built_at": now(), "native_imaging": False,
    }}


def prune_releases(directory, keep, current, entry, protected=()):
    # Only complete, owned identity-scoped releases are retention candidates.
    complete = []
    for path in directory.iterdir():
        if path.is_symlink() or not path.is_dir() or not VERSION_PATTERN.fullmatch(path.name):
            continue
        try:
            manifest = validate_manifest(read_json(path / "manifest.json"), entry)
            archive = check_path(path / "app.tar.gz", "file")
            if manifest["version"] == path.name and archive.is_file():
                complete.append(path)
        except (DeployError, OSError):
            # Foreign, malformed and incomplete directories are not ours to
            # delete merely because their names resemble a release version.
            continue
    preserved = {current, *protected}
    preserved.update(path.name for path in sorted(complete, reverse=True)[:keep])
    for path in complete:
        if path.name not in preserved:
            shutil.rmtree(path)


def build(project, profile):
    limit = int(os.environ.get("DEPLOY_BUILDER_CACHE_MB", "12288"))
    if not 256 <= limit <= 1048576:
        raise LocalError("DEPLOY_BUILDER_CACHE_MB must be between 256 and 1048576")
    request = build_request(project, profile)
    version = request["manifest"]["version"]
    output = ROOT / "dist" / "releases" / project["id"] / profile["instance_id"]
    # A per-user shared lock protects Docker images and cache volumes across
    # cloned checkouts; a second per-instance lock protects local retention.
    shared_lock = Path(tempfile.gettempdir()).resolve() / f"{PREFIX}-builder-{os.getuid()}.lock"
    with lock(shared_lock), lock(ROOT / ".deploy" / (profile["instance_id"] + ".lock")):
        execute(["docker", "info"], timeout=30)
        go = re.search(r"^go ([0-9.]+)$", (ROOT / "go.mod").read_text(), re.MULTILINE)
        node = re.search(r"[0-9]+", read_json(ROOT / "web" / "package.json")["engines"]["node"])
        if not go or not node:
            raise LocalError("cannot resolve Go/Node versions from the existing build declarations")
        dockerfile = TOOLS_ROOT / "docker" / "builder.Dockerfile"
        fingerprint = hashlib.sha256(dockerfile.read_bytes() + go[1].encode() + node[0].encode()).hexdigest()[:12]
        image_name = f"{PREFIX}-builder-{project['id']}"
        image = f"{image_name}:{fingerprint}"
        label = f"{PREFIX}.project={project['id']}"
        builder = f"{PREFIX}-{project['id']}"
        builders = execute(["docker", "buildx", "ls", "--format", "{{.Name}}"]).splitlines()
        if builder in (name.rstrip(" *") for name in builders):
            # A killed process may have left this project's isolated builder.
            execute(["docker", "buildx", "rm", "--force", builder])
        images = execute(["docker", "image", "ls", "--format", "{{.Repository}}:{{.Tag}}", image_name]).splitlines()
        if image not in images:
            # A dedicated, short-lived BuildKit builder owns intermediate build
            # cache. Removing it never prunes another project's shared cache.
            execute(["docker", "buildx", "create", "--name", builder, "--driver", "docker-container"])
            try:
                execute(["docker", "buildx", "build", "--builder", builder, "--load", "--label", label,
                         "--build-arg", f"GO_VERSION={go[1]}", "--build-arg", f"NODE_MAJOR={node[0]}",
                         "-f", str(dockerfile), "-t", image, str(TOOLS_ROOT / "docker")], capture=False)
            finally:
                execute(["docker", "buildx", "rm", "--force", builder])
        check_path(output, "directory")
        release = output / version
        release.mkdir(parents=True, mode=0o700)
        temporary = ROOT / ".deploy" / f"build-{uuid.uuid4()}.json"
        atomic_json(temporary, request, mode=0o600)
        cache = f"{PREFIX}-cache-{project['id']}"
        try:
            execute(["docker", "run", "--rm", "--name", f"{PREFIX}-build-{project['id']}", "--label", label,
                     "--mount", f"type=bind,src={ROOT},dst=/src,readonly",
                     "--mount", f"type=bind,src={TOOLS_ROOT},dst=/foundry-deploy,readonly",
                     "--mount", f"type=bind,src={temporary},dst=/request.json,readonly",
                     "--mount", f"type=bind,src={release},dst=/out",
                     "--mount", f"type=volume,src={cache},dst=/cache",
                     "-e", f"VERSION={version}", "-e", f"ARCHITECTURE={profile['architecture']}",
                     image, "bash", "/foundry-deploy/local/build.sh"], capture=False)
        finally:
            temporary.unlink(missing_ok=True)
            # Check/empty only this project's cache while holding its global lock.
            try:
                execute(["docker", "run", "--rm", "--mount", f"type=volume,src={cache},dst=/cache",
                         "-e", f"CACHE_LIMIT_MB={limit}", image, "bash", "-c",
                         'if [ "$(du -sm /cache | cut -f1)" -gt "$CACHE_LIMIT_MB" ]; then go clean -cache -modcache; npm cache clean --force --loglevel=error; fi'], capture=False)
                images = execute(["docker", "image", "ls", "--filter", f"label={label}", "--format", "{{.Repository}}:{{.Tag}}"]).splitlines()
                for old in set(images):
                    if old.startswith(image_name + ":") and old != image:
                        execute(["docker", "image", "rm", old])
            except LocalError:
                say("Builder retention failed; inspect this project's Docker cache before the next build.")
            if not ((release / "app.tar.gz").is_file() and (release / "manifest.json").is_file()):
                shutil.rmtree(release)
        if not (release / "app.tar.gz").is_file():
            raise LocalError("builder did not produce a release archive")
        prune_releases(output, profile["retention"]["local_releases"], version,
                       {**identity(project, profile), "profile": profile}, known_releases(profile))
    return release / "app.tar.gz"


def state_cache(profile):
    return ROOT / ".deploy" / "releases" / (profile["instance_id"] + ".json")


def remember_releases(profile, response):
    state = response["state"]
    if state["instance_id"] != profile["instance_id"]:
        raise LocalError("server status returned a different instance identity")
    versions = [(state.get(key) or {}).get("version") for key in ("current", "previous")]
    if any(version is not None and not VERSION_PATTERN.fullmatch(version) for version in versions):
        raise LocalError("server status returned an invalid release version")
    atomic_json(state_cache(profile), {"schema": SCHEMA, "host_id": profile["host"]["host_id"],
                "instance_id": profile["instance_id"], "versions": versions}, mode=0o600)


def known_releases(profile):
    path = state_cache(profile)
    if not path.exists():
        return ()
    value = read_json(path)
    if value.get("schema") != SCHEMA or value.get("host_id") != profile["host"]["host_id"] or value.get("instance_id") != profile["instance_id"]:
        raise LocalError("local release status cache belongs to another instance")
    versions = value.get("versions")
    if not isinstance(versions, list) or len(versions) != 2 or any(version is not None and (not isinstance(version, str) or not VERSION_PATTERN.fullmatch(version)) for version in versions):
        raise LocalError("invalid local release status cache")
    return tuple(version for version in versions if version is not None)


def select_archive(project, profile):
    directory = ROOT / "dist" / "releases" / project["id"] / profile["instance_id"]
    selection = os.environ.get("RELEASE")
    if not selection and sys.stdin.isatty():
        versions = sorted((path.name for path in directory.glob("*") if VERSION_PATTERN.fullmatch(path.name)
                           and not path.is_symlink() and (path / "app.tar.gz").is_file()
                           and (path / "manifest.json").is_file()), reverse=True)
        if versions:
            say("Retained releases: " + ", ".join(versions))
            selection = ask("Release (build or retained version)", "build")
    if not selection or selection == "build":
        return build(project, profile)
    if not VERSION_PATTERN.fullmatch(selection):
        raise LocalError("RELEASE must be build or an exact retained release version")
    archive = check_path(directory / selection / "app.tar.gz", "file")
    manifest = read_json(archive.parent / "manifest.json")
    validate_manifest(manifest, {**identity(project, profile), "profile": profile})
    return archive


def deploy(project, profile, settings):
    action = os.environ.get("ACTION") or ask("Action (" + " / ".join(ACTIONS) + ")", "deploy")
    if action not in ACTIONS:
        raise LocalError("unsupported deployment action")
    if not profile["host"]["host_id"]:
        raise LocalError("environment is not bound to a bootstrapped host; run make bootstrap")
    ssh = SSH(profile, settings)
    request = {"project": project, "profile": profile}
    if action == "report":
        # The installed report is intentionally available without sudo.
        result, exit_code = ssh.report()
        if result.get("host", {}).get("host_id") != profile["host"]["host_id"]:
            if result.get("host", {}).get("host_id") is None:
                display(result)
            raise LocalError("host report cannot verify the committed environment binding")
        display(result)
        if exit_code:
            raise LocalError("host report is incomplete; review its structured problems")
        return
    if action == "sync":
        canonical = ssh.request("sync", request)
        local = {"profile": profile, "topology": load_topology(ROOT)}
        remote = {key: canonical[key] for key in local}
        difference = "".join(difflib.unified_diff(
            json.dumps(local, indent=2, sort_keys=True).splitlines(keepends=True),
            json.dumps(remote, indent=2, sort_keys=True).splitlines(keepends=True),
            fromfile="local", tofile="registered"))
        print(difference or "Local profile and topology match the registered instance.")
        return
    if action in {"tools-plan", "tools-apply", "upgrade-resume"}:
        with ssh.staged_tools() as remote:
            if action == "tools-plan":
                display(ssh.request(action, request, remote))
                return
            plan_token = os.environ.get("PLAN_TOKEN")
            operation = os.environ.get("OPERATION")
            if not plan_token or not operation:
                raise LocalError("tool apply/resume requires reviewed PLAN_TOKEN and stable OPERATION UUID")
            confirm("Apply the reviewed forward-only host tool upgrade")
            display(ssh.request(action, {**request, "plan_token": plan_token,
                                        "operation_id": operation}, remote))
        return
    status = ssh.request("status", request)
    remember_releases(profile, status)
    if action == "status":
        display(status)
    elif action == "deploy":
        say(f"Build and activate {instance_name(project, profile)} on {profile['host']['address']}; pending forward migrations and seeders run during activation.")
        archive = select_archive(project, profile)
        manifest = read_json(archive.parent / "manifest.json")
        say(json.dumps({key: manifest[key] for key in ("version", "commit", "tree_state", "architecture", "instance")}, indent=2))
        confirm("Activate this completed release, including its pending forward migrations")
        with ssh.temporary() as remote:
            destination = remote + "/app.tar.gz"
            ssh.upload(archive, destination)
            display(ssh.request("activate", {**request, "archive": destination, "archive_sha256": digest(archive)}))
            remember_releases(profile, ssh.request("status", request))
    elif action == "config":
        say(json.dumps(ssh.request("config-keys", request), indent=2))
        changes = {}
        key = ask("Configuration key")
        while key:
            if key in changes:
                raise LocalError("each configuration key can be named once per change")
            changes[key] = ask("New value (hidden; stored only on the server)", secret=True)
            if len(changes) == MAX_CONFIGURATION_CHANGES:
                break
            # Settings that validate only together (a key ring's ID and material) share one revision.
            key = ask("Another key to change in the same revision (empty to finish)", "")
        if not changes:
            raise LocalError("name a configuration key to change")
        confirm(f"Validate and set {', '.join(changes)} for {instance_name(project, profile)}")
        display(ssh.request("config-set", {**request, "values": changes, "confirm": True}))
    elif action == "logs":
        result = ssh.request("logs", request)
        if sys.stdin.isatty():
            print(result["logs"])
        else:
            display(result)
    else:
        say(json.dumps(status, indent=2))
        confirm(f"{action.capitalize()} {instance_name(project, profile)}")
        display(ssh.request(action, {**request, "confirm": True}))
        remember_releases(profile, ssh.request("status", request))


def resolve_app_root(argument: str | None) -> Path:
    """Resolve an explicit app root; framework location and cwd are never defaults."""
    value = argument or os.environ.get("FOUNDRY_APP_ROOT")
    if not value:
        raise LocalError("supply --app-root or FOUNDRY_APP_ROOT")
    root = Path(value).expanduser().resolve(strict=True)
    if not root.is_dir() or not (root / "go.mod").is_file():
        raise LocalError("app root must be a Go application directory containing go.mod")
    return root


def main():
    global ROOT
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--app-root", help="application directory (or FOUNDRY_APP_ROOT)")
    parser.add_argument("command", choices=[*COMMANDS, "help"], nargs="?", default="help")
    arguments = parser.parse_args()
    if arguments.command == "help":
        for command, description in COMMANDS.items():
            print(f"make {command:<10} {description}")
        print("Pass --app-root PATH or FOUNDRY_APP_ROOT; ENVIRONMENT and ACTION select the operation.")
        return
    ROOT = resolve_app_root(arguments.app_root)
    mode = arguments.command
    with lock(ROOT / ".deploy" / "operation.lock"):
        settings = local_settings()
        project = project_config(mode)
        environment = select_environment(mode, settings)
        profile, fresh = profile_config(environment, project)
        for finding in edge_findings(profile, TOOLS_ROOT / "lib"):
            if finding["kind"] == "warning":
                say(f"warning: {finding['code']}: {finding['detail']}")
        if mode == "bootstrap":
            bootstrap(project, profile, settings, fresh)
        elif mode == "release":
            archive = build(project, profile)
            display({"archive": str(archive)})
        else:
            deploy(project, profile, settings)


if __name__ == "__main__":
    try:
        main()
    except (LocalError, DeployError, ValueError, OSError, KeyError) as error:
        say(f"Deployment stopped: {error}")
        sys.exit(1)
    except KeyboardInterrupt:
        say("Interrupted. Inspect server status before repeating a remote operation.")
        sys.exit(130)
