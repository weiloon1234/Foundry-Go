#!/usr/bin/env python3
"""Local interactive entry point: public profiles, Docker builds and direct SSH."""
import argparse
from contextlib import closing, contextmanager
import difflib
from datetime import datetime, timezone
import fcntl
import getpass
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import re
import shlex
import shutil
import signal
import stat
import subprocess
import sys
import tarfile
import tempfile
import uuid

if sys.version_info < (3, 11):
    sys.exit("Foundry deploy tooling needs Python 3.11 or later; this python3 is " + sys.version.split()[0])

TOOLS_ROOT = Path(__file__).resolve().parents[1]
ROOT: Path | None = None
sys.path.insert(0, str(TOOLS_ROOT / "lib"))
from edge import edge_findings

from contracts import (BACKUP_POLICY, DeployError, PREFIX, RENEWAL_WARNING, SCHEMA, PROFILE_SCHEMA, LAYOUT_VERSION, VERSION_PATTERN, atomic_json, check_path, digest, identity, instance_name, now, parse_json, read_json, slug,
                       validate_profile, validate_project)
from release import MAX_CONFIGURATION_CHANGES, validate_manifest
from topology import load_topology
from upgrades import READ_ACTIONS, check_upgrade_generation, protocol_metadata, tool_bundle

INSTALLED = f"/usr/local/sbin/{PREFIX}-host"
COMMANDS = {
    "bootstrap": "inspect and prepare a project/environment on a server",
    "deploy": "interactive deployment, status, rollback, capacity and configuration",
    "release": "build a local Docker release without contacting a server",
}
ACTIONS = ("capacity", "deploy", "status", "rollback", "restart", "stop", "logs", "config", "keys rotate", "reconcile",
           "report", "sync", "tools-plan", "tools-apply", "upgrade-resume", "backup-configure", "backup-run",
           "restore-drill", "backup-keys-rotate", "backup-recipients-rotate", "dns-token-rotate")


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


def secret_input(name, label, *, structured=False, required=True):
    """Private input is never echoed or copied into local operation receipts."""
    filename = os.environ.get("FOUNDRY_SECRET_" + name.upper() + "_FILE")
    if filename:
        try:
            source = Path(filename).expanduser()
            if not source.is_file() or source.stat().st_size > 1024 * 1024:
                raise LocalError("secret input must be a regular file of at most 1 MiB")
            value = source.read_text(encoding="utf-8").strip()
        except (OSError, UnicodeError) as error:
            raise LocalError("cannot read private " + name + " input") from error
    elif required:
        value = ask(label, secret=True)
    else:
        return None
    if not value:
        raise LocalError("private " + name + " input is required")
    if structured:
        try:
            return parse_json(value)
        except DeployError as error:
            raise LocalError("private " + name + " input must be valid JSON") from error
    return value


def bootstrap_secrets(required, *, backup_only=False):
    result = {}
    for name, label, structured in (
        ("cloudflare_dns_token", "Cloudflare zone DNS token", False),
        ("app_storage", "Application R2 storage JSON (credentials and disks)", True),
        ("backup_s3", "R2 backup JSON (endpoint, region, bucket, access_key_id, secret_access_key)", True),
        ("age_recipients", "Age recovery recipients JSON array", True),
        ("encryption", "Encryption JSON (key_id and key)", True),
    ):
        if backup_only and name not in {"backup_s3", "age_recipients"}:
            continue
        value = secret_input(name, label, structured=structured, required=required and name != "encryption")
        if value is not None:
            result[name] = value
    for name, fields in (("app_storage", {"credentials", "disks"}),
                         ("backup_s3", {"endpoint", "region", "bucket", "access_key_id", "secret_access_key"}),
                         ("encryption", {"key_id", "key"})):
        if name in result and (not isinstance(result[name], dict) or set(result[name]) != fields):
            raise LocalError("private " + name + " input has invalid fields")
    recipients = result.get("age_recipients")
    if recipients is not None and (not isinstance(recipients, list) or not recipients
                                   or not all(isinstance(item, str) and item for item in recipients)):
        raise LocalError("age_recipients must be a nonempty JSON array of recipient strings")
    if backup_only:
        return result
    # Initial application settings the first deploy needs, such as seed credentials.
    configuration = secret_input("config", "Configuration values JSON", structured=True, required=False)
    if configuration is None and required and sys.stdin.isatty():
        supplied = ask("Initial application configuration JSON (hidden; blank for none)", secret=True)
        try:
            configuration = parse_json(supplied) if supplied else None
        except DeployError as error:
            raise LocalError("private configuration input must be valid JSON") from error
    if configuration is not None:
        if (not isinstance(configuration, dict) or not 1 <= len(configuration) <= MAX_CONFIGURATION_CHANGES
                or not all(isinstance(key, str) and isinstance(value, str) for key, value in configuration.items())):
            raise LocalError("initial configuration requires 1–16 string keys and values")
        result["configuration"] = configuration
    return result


def operation_uuid(value):
    try:
        operation = uuid.UUID(value)
    except ValueError as error:
        raise LocalError("OPERATION must be a UUIDv4") from error
    if operation.version != 4:
        raise LocalError("OPERATION must be a UUIDv4")
    return str(operation)


def operation_id(profile, action, arguments):
    """Persist public intent before SSH; an explicit UUID resumes lost responses."""
    supplied = os.environ.get("OPERATION")
    operation = operation_uuid(supplied) if supplied else str(uuid.uuid4())
    path = ROOT / ".deploy" / "operations" / (operation + ".json")
    intent = {"instance_id": profile["instance_id"], "host_id": profile["host"]["host_id"],
              "action": action, **arguments}
    if path.exists() and read_json(path) != intent:
        raise LocalError("OPERATION already identifies a different public request")
    atomic_json(path, intent, mode=0o600)
    say("Operation: " + operation + " (OPERATION=" + operation + " resumes this request)")
    return operation


def check_ssh_exit(returncode, stderr=None, *, mutation=True):
    if returncode == 255:
        # Matches only OpenSSH's own refusal lines; other stderr is never echoed.
        if stderr and re.search(rb"(?m)^\S+: Permission denied \(", stderr):
            raise LocalError("SSH login was refused. Check the private key saved for this server in "
                             ".deploy/settings.json, and that its public key is on the server.")
        if stderr and re.search(rb"(?m)^Host key verification failed\.", stderr):
            raise LocalError("The server's SSH host key is unknown or differs from the one recorded on this "
                             "computer, so this connection was refused. If you rebuilt this server, remove the "
                             "old key with ssh-keygen -R and its address. Otherwise, treat it as a possible attack.")
        detail = " The outcome of the mutation is unknown." if mutation else ""
        raise LocalError("SSH connection failed or dropped." + detail + " Run ACTION=status before retrying.")


def control_directory():
    # /tmp is deliberately short even on macOS; never inherit its long TMPDIR.
    directory = Path("/tmp") / ("foundry-ssh-" + str(os.getuid()))
    try:
        directory.mkdir(mode=0o700)
    except FileExistsError:
        pass
    info = directory.lstat()
    if not stat.S_ISDIR(info.st_mode) or info.st_uid != os.getuid() or stat.S_IMODE(info.st_mode) != 0o700:
        raise LocalError("SSH control directory must be owned by this user with mode 0700 and must not be a symlink")
    return directory


def execute(args, *, input=None, capture=True, timeout=None):
    try:
        result = subprocess.run(args, input=input, stdout=subprocess.PIPE if capture else sys.stderr,
                                stderr=subprocess.PIPE if capture else sys.stderr, timeout=timeout)
    except FileNotFoundError as error:
        raise LocalError(f"Required tool is unavailable: {args[0]}") from error
    except subprocess.TimeoutExpired as error:
        raise LocalError(f"Command timed out: {args[0]}; inspect state before retrying") from error
    if args[0] == "ssh":
        check_ssh_exit(result.returncode, result.stderr)
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
        "backup_policy": dict(BACKUP_POLICY),
    }
    validate_profile(profile, project)
    atomic_json(path, profile)
    say(f"Created deploy/environments/{environment}.json; architecture will be discovered during inspection.")
    return profile, True


def save_profile(profile):
    atomic_json(ROOT / "deploy" / "environments" / (profile["environment"] + ".json"), profile)


class SSH:
    def __init__(self, profile, settings, *, first_contact=False):
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
        # One target per session; leave room for OpenSSH's temporary socket suffix.
        control = control_directory() / uuid.uuid4().hex
        # Bootstrap and replacement-host recovery may record an unknown host key.
        # A changed key is always refused, and every other operation needs the recorded one.
        checking = "accept-new" if first_contact else "yes"
        self.base = ["ssh", "-o", "StrictHostKeyChecking=" + checking, "-o", "BatchMode=yes",
                     "-o", "ConnectTimeout=30", "-o", "ConnectionAttempts=3", "-o", "ServerAliveInterval=15",
                     "-o", "ServerAliveCountMax=8", "-o", "ControlMaster=auto",
                     "-o", "ControlPersist=120", "-o", "ControlPath=" + str(control), "-p", str(host["port"])]
        key_path = personal.get("key_path", "")
        if key_path:
            # The saved key is enough: no operator SSH config, agent or profile is needed.
            # UseKeychain exists only in Apple's OpenSSH, so IgnoreUnknown must precede it.
            self.base += ["-o", "IdentitiesOnly=yes", "-o", "IgnoreUnknown=UseKeychain", "-o", "UseKeychain=yes",
                          "-i", str(Path(key_path).expanduser())]
        effective = execute(self.base + ["-G", target], timeout=20)
        options = dict(line.split(" ", 1) for line in effective.splitlines() if " " in line)
        if options.get("hostname", "").lower().strip("[]") != host["address"].lower().strip("[]") or options.get("port") != str(host["port"]):
            raise LocalError("personal SSH alias resolves to a different hostname/port than the committed profile")
        self.base += [target]

    def close(self):
        try:
            execute(self.base[:-1] + ["-O", "exit", self.base[-1]], timeout=10)
        except (LocalError, OSError):
            pass

    def command(self, command, *, input=None, timeout=240):
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
        if RENEWAL_WARNING.encode() in (result.stderr or b"").splitlines():
            say(RENEWAL_WARNING)
        check_ssh_exit(result.returncode, result.stderr, mutation=action not in READ_ACTIONS)
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
                                    stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=240)
        except subprocess.TimeoutExpired as error:
            raise LocalError("host report exceeded its deadline") from error
        check_ssh_exit(result.returncode, result.stderr, mutation=False)
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
        check_ssh_exit(result.returncode, result.stderr)
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
    if action == "recover":
        return recover(project, profile, settings)
    if action not in {"prepare", "inspect"}:
        raise LocalError("bootstrap ACTION must be prepare, inspect or recover")
    request = {"project": project, "profile": profile, "topology": load_topology(ROOT)}
    with closing(SSH(profile, settings, first_contact=profile["host"]["host_id"] is None)) as ssh:
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
            secrets = bootstrap_secrets(fresh or pending_binding is not None or binding != inspected.get("host_id"))
            result = ssh.request("bootstrap-apply", {**request, "confirm": True, "plan_token": plan.get("plan_token"),
                                                    "secrets": secrets}, remote)
            if result.get("host_id") != binding:
                raise LocalError("bootstrap returned a different host identity; inspect the server")
            pending_path.unlink()
            display(result)


def recover(project, profile, settings):
    import copy
    import recover as recovery
    mode, address = os.environ.get("RECOVER_MODE"), os.environ.get("RECOVER_HOST")
    directory = os.environ.get("FOUNDRY_RECOVERY_DIR")
    if mode not in {"rehearsal", "takeover"} or not address or not directory:
        raise LocalError("recover requires RECOVER_HOST, RECOVER_MODE=rehearsal|takeover and FOUNDRY_RECOVERY_DIR")
    if not profile["host"]["host_id"]:
        raise LocalError("recover requires the existing instance profile and its old host identity")
    target = os.environ.get("TARGET_TIME") or None
    if target:
        target = recovery.backup._restore_target(target)
    pending = ROOT / ".deploy" / "recovery" / (profile["instance_id"] + ".json")
    with tempfile.TemporaryDirectory(prefix="foundry-recovery-", dir=Path(tempfile.gettempdir()).resolve()) as temporary:
        private_archive = Path(temporary) / "recovery.tar"
        recovery.folder_archive(directory, private_archive)
        private_archive.chmod(0o600)
        files = recovery.read_archive(private_archive)
        recovered = recovery.archive_configuration(project, profile, files, load_topology(ROOT))
        record = read_json(check_path(pending, "file")) if pending.exists() else None
        if record:
            selected = check_path(Path(record["release_archive"]), "file")
            selection = os.environ.get("RELEASE")
            if selection and selection != "build" and selection != selected.parent.name:
                raise LocalError("pending recovery uses a different release; resume with its original RELEASE")
        else:
            selected = select_archive(project, profile)
        intent = {"project_id": project["id"], "instance_id": profile["instance_id"], "mode": mode,
                  "address": address, "target_time": target, "archive_sha256": digest(selected),
                  "recovery_sha256": digest(private_archive)}
        if record:
            if record.get("intent") != intent:
                raise LocalError(f"this differs from the recovery recorded in {pending}. Repeat its inputs to resume it, "
                                 "or delete that file only if you are abandoning it and recreating the server")
        else:
            record = {"intent": intent, "operation_id": operation_uuid(os.environ["OPERATION"]) if os.environ.get("OPERATION") else str(uuid.uuid4()),
                      "host_id": str(uuid.uuid4()), "release_archive": str(selected.absolute())}
        replacement = copy.deepcopy(profile)
        replacement["host"].update(address=address, host_id=record["host_id"])
        validate_profile(replacement, project)
        if not pending.exists():
            atomic_json(pending, record, mode=0o600)
        request = {"project": project, "profile": replacement, "operation_id": record["operation_id"],
                   "mode": mode, "topology": recovered["topology"], "target_time": target,
                   "archive_sha256": intent["archive_sha256"]}
        say("Recovery operation: " + record["operation_id"] + "; repeat the same command to resume.")
        with closing(SSH(replacement, settings, first_contact=True)) as ssh, ssh.staged_tools() as remote:
            ssh.upload(private_archive, remote + "/recovery.tar")
            request["recovery_archive"] = remote + "/recovery.tar"
            plan = ssh.request("recover-plan", request, remote)
            display(plan)
            confirm(f"Recover {instance_name(project, profile)} on {address} in {mode} mode")
            old_gone = False
            if mode == "takeover":
                say("The old server must be stopped first: two servers archiving to one repository corrupt recovery.")
                old_gone = os.environ.get("CONFIRM_OLD_SERVER_GONE") == "yes"
                if not old_gone:
                    old_gone = ask("Type OLD SERVER STOPPED to confirm the old server is stopped") == "OLD SERVER STOPPED"
                if not old_gone:
                    raise LocalError("takeover cancelled: the old server must be stopped first")
            ssh.upload(selected, remote + "/release.tar.gz")
            result = ssh.request("recover-apply", {**request, "archive": remote + "/release.tar.gz",
                                 "plan_token": plan["plan_token"], "confirm": True, "old_server_gone": old_gone}, remote)
            if (result.get("ok") is not True or result.get("host_id") != record["host_id"]
                    or result.get("mode") != mode or result.get("operation_id") != record["operation_id"]):
                raise LocalError("recovery returned an unexpected identity; inspect its journal before retrying")
            if mode == "takeover":
                profile["host"].update(address=address, host_id=record["host_id"])
                save_profile(profile)
                say("Updated local profile host.address and host.host_id; commit that profile change. Point DNS at the new server.")
            pending.unlink()
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


def production_profile(profile):
    return profile["environment"] == "production" or profile["runtime_environment"] == "production"


def source_allowed(name):
    path = PurePosixPath(name)
    if path.is_absolute() or ".." in path.parts or "\\" in name or any(ord(c) < 32 for c in name):
        raise LocalError("source contains an unsafe path")
    excluded = {".git", ".deploy", "dist", "bin", ".aws", ".ssh", ".netrc", ".npmrc", ".codex",
                "storage", "node_modules", "go.work", "go.work.sum"}
    return not (any(part in excluded or part.startswith(".env") for part in path.parts)
                or path.suffix.lower() in {".pem", ".key", ".p12", ".pfx"}
                or name in {".claude/settings.local.json", "CLAUDE.local.md", "config/app.toml"}
                or name.startswith("public/build/")
                or name.startswith("config/") and name.endswith(".local.toml"))


def validate_source_links(names, links):
    """Resolve link components without touching targets or leaving the root."""
    for target in links.values():
        if not target or PurePosixPath(target).is_absolute() or "\\" in target or any(ord(c) < 32 for c in target):
            raise LocalError("source symlink must have a relative in-tree target")
    for name in names:
        if any(parent.as_posix() in links for parent in PurePosixPath(name).parents):
            raise LocalError("source entry has a symlink ancestor")
    for name in links:
        resolved, active = [], set()
        pending = [(part, False) for part in reversed(PurePosixPath(name).parts)]
        while pending:
            part, finished = pending.pop()
            if finished:
                active.remove(part)
            elif part in {"", "."}:
                continue
            elif part == "..":
                if not resolved:
                    raise LocalError("source symlink escapes the snapshot root")
                resolved.pop()
            else:
                resolved.append(part)
                link = "/".join(resolved)
                if link in links:
                    if link in active:
                        raise LocalError("source symlink chain contains a cycle")
                    active.add(link)
                    resolved.pop()
                    pending.append((link, True))
                    pending.extend((component, False) for component in reversed(links[link].split("/")))


def source_inventory():
    paths = execute(["git", "-C", str(ROOT), "ls-files", "--cached", "--others", "--exclude-standard", "-z"])
    result = {}
    for name in sorted(set(paths.split("\0")) - {""}):
        if not source_allowed(name):
            continue
        path = ROOT / name
        check_path(path.parent, "directory")
        try:
            details = path.lstat()
        except FileNotFoundError:
            continue
        if stat.S_ISLNK(details.st_mode):
            result[name] = {"symlink": os.readlink(path)}
        elif stat.S_ISREG(details.st_mode) and details.st_nlink == 1:
            check_path(path, "file")
            result[name] = {"sha256": digest(path), "mode": 0o755 if details.st_mode & 0o111 else 0o644}
        else:
            raise LocalError("source includes an unsupported hardlink or special file")
    validate_source_links(result, {name: details["symlink"] for name, details in result.items() if "symlink" in details})
    return result


def source_snapshot(project, profile, request, directory):
    """Capture under both build locks. Docker receives only this private snapshot."""
    manifest = request["manifest"]
    production = production_profile(profile)
    if production and manifest["tree_state"] != "clean":
        raise LocalError("production builds require a clean committed HEAD, including untracked files")
    entries = execute(["git", "-C", str(ROOT), "ls-files", "--stage"])
    if any(line.startswith("160000 ") for line in entries.splitlines()):
        raise LocalError("submodules need an explicit immutable build-input contract")
    archive, snapshot = directory / "source.tar", directory / "source"
    snapshot.mkdir(mode=0o700)
    if production:
        execute(["git", "-C", str(ROOT), "archive", "--format=tar", "--output=" + str(archive), manifest["commit"]])
        with tarfile.open(archive, "r:") as bundle:
            members = {}
            for member in bundle:
                if not source_allowed(member.name):
                    continue
                if not (member.isfile() or member.isdir() or member.issym()):
                    raise LocalError("source archive contains unsupported hardlinks or special files")
                name = PurePosixPath(member.name).as_posix()
                if name in members:
                    raise LocalError("source archive contains duplicate paths")
                members[name] = member
            links = {name: member.linkname for name, member in members.items() if member.issym()}
            validate_source_links(members, links)
            for name, member in members.items():
                destination = snapshot / name
                if member.isdir():
                    destination.mkdir(parents=True, exist_ok=True)
                elif member.issym():
                    destination.parent.mkdir(parents=True, exist_ok=True)
                    destination.symlink_to(member.linkname)
                else:
                    destination.parent.mkdir(parents=True, exist_ok=True)
                    with bundle.extractfile(member) as source, destination.open("xb") as output:
                        shutil.copyfileobj(source, output)
                    destination.chmod(0o755 if member.mode & 0o111 else 0o644)
        committed_project = read_json(snapshot / "deploy" / "project.json")
        committed_profile = read_json(snapshot / "deploy" / "environments" / (profile["environment"] + ".json"))
        if committed_project != project or committed_profile != profile:
            raise LocalError("production project/profile must match committed HEAD; commit the reviewed public declarations first")
        manifest["source_kind"] = "git_archive"
    else:
        before = source_inventory()
        with tarfile.open(archive, "x:", format=tarfile.PAX_FORMAT) as bundle:
            for name, details in before.items():
                destination = snapshot / name
                destination.parent.mkdir(parents=True, exist_ok=True)
                if "symlink" in details:
                    destination.symlink_to(details["symlink"])
                else:
                    check_path(ROOT / name, "file")
                    shutil.copyfile(ROOT / name, destination, follow_symlinks=False)
                    if destination.is_symlink() or digest(destination) != details["sha256"]:
                        raise LocalError("source changed during snapshot capture; retry after edits finish")
                    destination.chmod(details["mode"])
                info = bundle.gettarinfo(str(destination), name)
                info.uid = info.gid = info.mtime = 0
                info.uname = info.gname = ""
                if info.issym():
                    bundle.addfile(info)
                else:
                    with destination.open("rb") as source:
                        bundle.addfile(info, source)
        if source_inventory() != before:
            raise LocalError("source changed during snapshot capture; retry after edits finish")
        manifest["source_kind"] = "working_tree"
    for attributes in snapshot.rglob(".gitattributes"):
        if attributes.is_symlink():
            continue
        if re.search(r"filter\s*=\s*lfs", attributes.read_text()):
            raise LocalError("Git LFS inputs need an explicit immutable build-input contract")
    module = (snapshot / "go.mod").read_text()
    if re.search(r"=>\s*(?:\.{1,2}(?:/|\s|$)|/|[A-Za-z]:[\\/])", module):
        raise LocalError("local module replacements cannot be captured as immutable release inputs")
    after = build_request(project, profile)["manifest"]
    if (after["commit"], after["tree_state"]) != (manifest["commit"], manifest["tree_state"]):
        raise LocalError("HEAD or tree state changed during snapshot capture; retry after edits finish")
    manifest["source_sha256"] = digest(archive)
    manifest["topology_sha256"] = load_topology(snapshot)["sha256"]
    for path in snapshot.rglob("*"):
        if not path.is_symlink() and path.is_file():
            path.chmod(0o555 if path.stat().st_mode & 0o111 else 0o444)
    return snapshot


def recover_build_containers(project):
    """Client death is not container exit. Refuse any surviving writer."""
    for name in (f"{PREFIX}-build-{project['id']}", f"{PREFIX}-cache-clean-{project['id']}"):
        found = execute(["docker", "ps", "--all", "--filter", "name=^/" + name + "$", "--format", "{{.ID}}"], timeout=30).splitlines()
        if not found:
            continue
        if len(found) != 1:
            raise LocalError("ambiguous project build container ownership; cache retained")
        observed = json.loads(execute(["docker", "inspect", found[0]], timeout=30))[0]
        labels = observed.get("Config", {}).get("Labels") or {}
        operation = labels.get(f"{PREFIX}.operation", "")
        try:
            operation = str(uuid.UUID(operation))
        except ValueError as error:
            raise LocalError("build container has no matching durable ownership record; cache retained") from error
        record_path = ROOT / ".deploy" / "builds" / (operation + ".json")
        record = read_json(record_path) if record_path.is_file() else {}
        if (labels.get(f"{PREFIX}.project") != project["id"] or record.get("project_id") != project["id"]
                or record.get("operation_id") != operation or name not in record.get("containers", [])
                or observed.get("Name") != "/" + name):
            raise LocalError("build container ownership conflicts with this checkout; cache retained")
        state = observed.get("State", {})
        if state.get("Status") not in {"exited", "dead"} or state.get("Running"):
            raise LocalError("project build container is still running; wait for it or explicitly cancel it before retrying; cache retained")
        execute(["docker", "rm", found[0]], timeout=30)


def refuse_orphan_builder(project):
    builder = f"{PREFIX}-{project['id']}"
    builders = execute(["docker", "buildx", "ls", "--format", "{{.Name}}"], timeout=30).splitlines()
    if builder in (name.rstrip(" *") for name in builders):
        raise LocalError("project BuildKit builder survives a previous client; inspect its completion before removing it and retrying; cache retained")


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


def builder_inputs(snapshot):
    go = re.search(r"^go ([0-9.]+)$", (snapshot / "go.mod").read_text(), re.MULTILINE)
    node = re.search(r"[0-9]+", read_json(snapshot / "web" / "package.json")["engines"]["node"])
    if not go or not node:
        raise LocalError("cannot resolve Go/Node versions from the immutable build declarations")
    path = TOOLS_ROOT / "docker" / "build-inputs.json"
    pins = read_json(path)
    if pins.get("schema") != 1:
        raise LocalError("unsupported tools/deploy/docker/build-inputs.json schema")
    versions = {"ubuntu": "24.04", "go": go[1], "node": node[0]}
    images = {}
    for name, version in versions.items():
        checksum = pins.get(name, {}).get(version)
        if checksum is None:
            raise LocalError(f"{name} {version} needs a reviewed pin in Foundry-Go: tools/deploy/docker/build-inputs.json")
        if not isinstance(checksum, str) or not re.fullmatch(r"sha256:[0-9a-f]{64}", checksum):
            raise LocalError(f"invalid {name} {version} digest in tools/deploy/docker/build-inputs.json")
        images[name] = checksum
    dockerfile = TOOLS_ROOT / "docker" / "builder.Dockerfile"
    fingerprint = hashlib.sha256(dockerfile.read_bytes() + json.dumps(
        {"versions": versions, "images": images}, sort_keys=True).encode()).hexdigest()[:12]
    return versions, images, fingerprint


def build_source_epoch(manifest, profile):
    commit = manifest["commit"]
    if commit is None:
        if production_profile(profile):
            raise LocalError("production builds require a source commit")
        return 0
    value = execute(["git", "-C", str(ROOT), "show", "-s", "--format=%ct", commit], timeout=30).strip()
    if not re.fullmatch(r"[0-9]+", value):
        raise LocalError("cannot resolve source commit time")
    return int(value)


def build(project, profile):
    limit = int(os.environ.get("DEPLOY_BUILDER_CACHE_MB", "12288"))
    if not 256 <= limit <= 1048576:
        raise LocalError("DEPLOY_BUILDER_CACHE_MB must be between 256 and 1048576")
    output = ROOT / "dist" / "releases" / project["id"] / profile["instance_id"]
    shared_lock = Path(tempfile.gettempdir()).resolve() / f"{PREFIX}-builder-{os.getuid()}.lock"
    with lock(shared_lock), lock(ROOT / ".deploy" / (profile["instance_id"] + ".lock")):
        execute(["docker", "info"], timeout=30)
        recover_build_containers(project)
        refuse_orphan_builder(project)
        # Provenance is captured after exclusion and before any builder starts.
        request = build_request(project, profile)
        version = request["manifest"]["version"]
        operation = str(uuid.uuid4())
        records = ROOT / ".deploy" / "builds"
        workspace = records / operation
        check_path(workspace, "directory")
        workspace.mkdir(parents=True, mode=0o700)
        record_path = records / (operation + ".json")
        build_name = f"{PREFIX}-build-{project['id']}"
        cleanup_name = f"{PREFIX}-cache-clean-{project['id']}"
        record = {"operation_id": operation, "project_id": project["id"],
                  "instance_id": profile["instance_id"], "version": version,
                  "containers": [build_name, cleanup_name], "phase": "snapshot"}
        atomic_json(record_path, record, mode=0o600)
        snapshot = source_snapshot(project, profile, request, workspace)
        versions, inputs, fingerprint = builder_inputs(snapshot)
        request["images"] = inputs
        request["source_date_epoch"] = build_source_epoch(request["manifest"], profile)
        dockerfile = TOOLS_ROOT / "docker" / "builder.Dockerfile"
        image_name = f"{PREFIX}-builder-{project['id']}"
        image = f"{image_name}:{fingerprint}"
        label = f"{PREFIX}.project={project['id']}"
        operation_label = f"{PREFIX}.operation={operation}"
        builder = f"{PREFIX}-{project['id']}"
        record.update(phase="builder", builder=builder, image=image)
        atomic_json(record_path, record, mode=0o600)
        images = execute(["docker", "image", "ls", "--format", "{{.Repository}}:{{.Tag}}", image_name]).splitlines()
        if image not in images:
            execute(["docker", "buildx", "create", "--name", builder, "--driver", "docker-container"])
            # An interrupted command deliberately retains its builder and journal.
            execute(["docker", "buildx", "build", "--builder", builder, "--load", "--label", label,
                     "--build-arg", f"GO_VERSION={versions['go']}", "--build-arg", f"NODE_MAJOR={versions['node']}",
                     "--build-arg", f"UBUNTU_DIGEST={inputs['ubuntu']}",
                     "--build-arg", f"GO_DIGEST={inputs['go']}", "--build-arg", f"NODE_DIGEST={inputs['node']}",
                     "-f", str(dockerfile), "-t", image, str(TOOLS_ROOT / "docker")], capture=False)
            execute(["docker", "buildx", "rm", builder])
        request["manifest"]["builder_image_id"] = execute(
            ["docker", "image", "inspect", "--format", "{{.Id}}", image], timeout=30).strip()
        check_path(output, "directory")
        release = output / version
        release.mkdir(parents=True, mode=0o700)
        request_file = workspace / "request.json"
        atomic_json(request_file, request, mode=0o600)
        record["phase"] = "building"
        atomic_json(record_path, record, mode=0o600)
        cache = f"{PREFIX}-cache-{project['id']}"
        execute(["docker", "run", "--rm", "--name", build_name, "--label", label, "--label", operation_label,
                 "--mount", f"type=bind,src={snapshot},dst=/src,readonly",
                 "--mount", f"type=bind,src={TOOLS_ROOT},dst=/foundry-deploy,readonly",
                 "--mount", f"type=bind,src={request_file},dst=/request.json,readonly",
                 "--mount", f"type=bind,src={release},dst=/out",
                 "--mount", f"type=volume,src={cache},dst=/cache",
                 "-e", f"VERSION={version}", "-e", f"ARCHITECTURE={profile['architecture']}",
                 image, "bash", "/foundry-deploy/local/build.sh"], capture=False)
        # Even a successful client return must prove the named writer has exited.
        recover_build_containers(project)
        if not (release / "app.tar.gz").is_file() or not (release / "manifest.json").is_file():
            raise LocalError("builder did not produce a completed release archive")
        validate_manifest(read_json(release / "manifest.json"), {**identity(project, profile), "profile": profile})
        record["phase"] = "cache_cleanup"
        atomic_json(record_path, record, mode=0o600)
        execute(["docker", "run", "--rm", "--name", cleanup_name, "--label", label, "--label", operation_label,
                 "--mount", f"type=volume,src={cache},dst=/cache",
                 "-e", f"CACHE_LIMIT_MB={limit}", image, "bash", "-c",
                 'if [ "$(du -sm /cache | cut -f1)" -gt "$CACHE_LIMIT_MB" ]; then go clean -cache -modcache; npm cache clean --force --loglevel=error; fi'], capture=False)
        recover_build_containers(project)
        images = execute(["docker", "image", "ls", "--filter", f"label={label}", "--format", "{{.Repository}}:{{.Tag}}"]).splitlines()
        for old in set(images):
            if old.startswith(image_name + ":") and old != image:
                execute(["docker", "image", "rm", old])
        record["phase"] = "completed"
        atomic_json(record_path, record, mode=0o600)
        # Only completed writers release their input mounts and retention scope.
        shutil.rmtree(workspace)
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


def declared_role(role):
    if role not in {item["name"] for item in load_topology(ROOT)["roles"]}:
        raise LocalError("ROLE must name a role declared in deploy/topology.json")
    return role


def deploy(project, profile, settings):
    action = os.environ.get("ACTION") or ask("Action (" + " / ".join(ACTIONS) + ")", "deploy")
    if action not in ACTIONS:
        raise LocalError("unsupported deployment action")
    if not profile["host"]["host_id"]:
        raise LocalError("environment is not bound to a bootstrapped host; run make bootstrap")
    with closing(SSH(profile, settings)) as ssh:
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
        if action == "capacity":
            plan = ssh.request("capacity-plan", request)
            display(plan)
            if not plan["noop"]:
                confirm("Apply the capacity plan; the application sees a brief interruption")
            display(ssh.request("capacity-apply", {**request, "plan_token": plan["plan_token"], "confirm": True}))
            return
        if action in {"tools-plan", "tools-apply", "upgrade-resume"}:
            installed = ssh.request("hello", request)
            try:
                check_upgrade_generation(installed.get("tools"), TOOLS_ROOT)
            except DeployError as error:
                raise LocalError(f"{error.code}: {error.detail}") from None
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
        rotations = {
            "backup-keys-rotate": ("backup_s3", "Replacement backup R2 JSON", True),
            "backup-recipients-rotate": ("age_recipients", "Replacement age recipients JSON array", True),
            "dns-token-rotate": ("cloudflare_dns_token", "Replacement Cloudflare DNS token", False),
        }
        if action in rotations:
            name, label, structured = rotations[action]
            private = secret_input(name, label, structured=structured)
            scope = "all instances sharing the backup repository" if action == "backup-keys-rotate" else (
                "all instances sharing this Cloudflare zone" if action == "dns-token-rotate" else instance_name(project, profile))
            confirm(f"{action} for {scope}")
            display(ssh.request(action, {**request, "secrets": {name: private}, "confirm": True}))
            return
        if action in {"backup-configure", "backup-run", "restore-drill"}:
            arguments = {}
            if action == "backup-configure":
                arguments["secrets"] = bootstrap_secrets(False, backup_only=True)
            elif action == "backup-run":
                arguments["kind"] = os.environ.get("BACKUP_KIND", "full")
                if arguments["kind"] not in {"full", "diff"}:
                    raise LocalError("BACKUP_KIND must be full or diff")
                arguments["operation_id"] = operation_id(profile, action, {"kind": arguments["kind"]})
            else:
                arguments["backup_id"] = operation_uuid(os.environ.get("BACKUP_ID") or ask("Completed backup UUID"))
                if os.environ.get("TARGET_TIME"):
                    arguments["target_time"] = os.environ["TARGET_TIME"]
            confirm(f"{action} for {instance_name(project, profile)}")
            display(ssh.request(action, {**request, **arguments, "confirm": True}))
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
            changes = secret_input("config", "Configuration values JSON", structured=True, required=False)
            if changes is None:
                changes = {}
                key = ask("Configuration key")
                while key:
                    if key in changes:
                        raise LocalError("each configuration key can be named once per change")
                    changes[key] = ask("New value (hidden; stored only on the server)", secret=True)
                    if len(changes) == MAX_CONFIGURATION_CHANGES:
                        break
                    key = ask("Another key to change in the same revision (empty to finish)", "")
            if (not isinstance(changes, dict) or not 1 <= len(changes) <= MAX_CONFIGURATION_CHANGES
                    or not all(isinstance(key, str) and isinstance(value, str) for key, value in changes.items())):
                raise LocalError("configuration requires 1–16 string keys and values")
            confirm(f"Validate and set {', '.join(changes)} for {instance_name(project, profile)}")
            display(ssh.request("config-set", {**request, "secrets": {"configuration": changes}, "confirm": True}))
        elif action == "keys rotate":
            encryption = secret_input("encryption", "Encryption JSON (key_id and key)", structured=True, required=False)
            if encryption is None and sys.stdin.isatty():
                supplied = ask("New encryption key JSON (key_id and key; blank generates both on the server)", secret=True)
                if supplied:
                    try:
                        encryption = parse_json(supplied)
                    except DeployError as error:
                        raise LocalError("private encryption input must be valid JSON") from error
            if encryption is not None and (not isinstance(encryption, dict) or set(encryption) != {"key_id", "key"}):
                raise LocalError("provide the encryption key ID and material together")
            confirm(f"Rotate the encryption key and retain old keys for {instance_name(project, profile)}")
            operation = operation_id(profile, "keys-rotate", {})
            display(ssh.request("keys-rotate", {**request, "secrets": {"encryption": encryption} if encryption else {},
                                                "operation_id": operation, "confirm": True}))
        elif action == "rollback":
            previous_intent = {}
            supplied_operation = os.environ.get("OPERATION")
            if supplied_operation:
                operation = operation_uuid(supplied_operation)
                path = ROOT / ".deploy" / "operations" / (operation + ".json")
                if path.exists():
                    previous_intent = read_json(path)
            state = status["state"]
            target = os.environ.get("TARGET") or previous_intent.get("target")
            if not target:
                say("Observed previous release: " + str((state.get("previous") or {}).get("version")))
                target = ask("Exact rollback target release")
            expected = os.environ.get("EXPECTED_CURRENT") or previous_intent.get("expected_current") or (state.get("current") or {}).get("version")
            if not isinstance(target, str) or not VERSION_PATTERN.fullmatch(target) or not isinstance(expected, str) or not VERSION_PATTERN.fullmatch(expected):
                raise LocalError("rollback requires exact TARGET and EXPECTED_CURRENT release versions")
            arguments = {"target": target, "expected_current": expected}
            say(json.dumps(arguments, indent=2))
            confirm(f"Roll back {instance_name(project, profile)} to this target")
            operation = operation_id(profile, "rollback", arguments)
            display(ssh.request("rollback", {**request, **arguments, "operation_id": operation, "confirm": True}))
            remember_releases(profile, ssh.request("status", request))
        elif action == "logs":
            role = os.environ["ROLE"] if "ROLE" in os.environ else (ask("Role (http / scheduler / worker)", "http") if sys.stdin.isatty() else "http")
            role = declared_role(role)
            result = ssh.request("logs", {**request, "role": role})
            if sys.stdin.isatty():
                print(result["logs"])
            else:
                display(result)
        else:
            if action in ("restart", "stop") and "ROLE" in os.environ:
                request["role"] = declared_role(os.environ["ROLE"])
            say(json.dumps(status, indent=2))
            scope = request.get("role", "all roles")
            confirm(f"{action.capitalize()} {scope} for {instance_name(project, profile)}")
            display(ssh.request(action, {**request, "confirm": True}))
            remember_releases(profile, ssh.request("status", request))


def resolve_app_root(argument: str | None) -> Path:
    """Resolve an explicit app root; framework location and cwd are never defaults."""
    if not argument:
        raise LocalError("supply --app-root")
    root = Path(argument).expanduser().resolve(strict=True)
    if not root.is_dir() or not (root / "go.mod").is_file():
        raise LocalError("app root must be a Go application directory containing go.mod")
    return root


def main():
    global ROOT
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--app-root", help="application directory")
    for option in ("environment", "action", "role", "target", "expected-current", "operation"):
        parser.add_argument("--" + option)
    parser.add_argument("command", choices=[*COMMANDS, "help"], nargs="?", default="help")
    arguments = parser.parse_args()
    if arguments.command == "help":
        for command, description in COMMANDS.items():
            print(f"make {command:<10} {description}")
        print("Actions: " + ", ".join(ACTIONS))
        print("ENVIRONMENT and ACTION select the operation.")
        return
    for option in ("environment", "action", "role", "target", "expected_current", "operation"):
        value = getattr(arguments, option)
        if value is not None:
            os.environ[option.upper()] = value
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
            if os.environ.get("ACTION", "build") != "build":
                raise LocalError("release ACTION must be build")
            archive = build(project, profile)
            display({"archive": str(archive)})
        else:
            deploy(project, profile, settings)


def interrupted(_signal, _frame):
    # `go tool` forwards the terminal's SIGINT, so one Ctrl-C arrives twice. Cleanup must run once.
    signal.signal(signal.SIGINT, signal.SIG_IGN)
    raise KeyboardInterrupt


if __name__ == "__main__":
    signal.signal(signal.SIGINT, interrupted)
    try:
        main()
    except (LocalError, DeployError, ValueError, OSError, KeyError) as error:
        say(f"Deployment stopped: {error}")
        sys.exit(1)
    except KeyboardInterrupt:
        say("Interrupted. Inspect server status before repeating a remote operation.")
        sys.exit(130)
