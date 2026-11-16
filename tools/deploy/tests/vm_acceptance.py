#!/usr/bin/env python3
"""Real operator CLI acceptance on a checksum-pinned, disposable KVM guest."""
import argparse
from contextlib import contextmanager
from dataclasses import dataclass
import errno
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import secrets
import select
import shlex
import shutil
import signal
import socket
import stat
import subprocess
import sys
import tempfile
import time
import tomllib
import uuid

DEPLOY = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(DEPLOY / "local"))
sys.path.insert(0, str(DEPLOY / "lib"))
import containers
from build_acceptance import FIXTURE, acceptance_execute, prepare_candidate
from contracts import atomic_json, instance_name
from disposable import ACCEPTANCE_CA, VM_MARKER
from topology import instance_units, parse_profile, parse_topology, validate_topology


class Failure(RuntimeError):
    pass


@dataclass
class Guest:
    directory: Path
    port: int
    config: Path
    address: str
    ssh: list
    qemu: object = None

    @property
    def serial(self):
        return self.directory / "serial.log"


def json_documents(output):
    """CLI review/apply output contains multiple JSON objects and progress lines."""
    decoder = json.JSONDecoder()
    documents, end = [], 0
    for match in re.finditer(r"(?m)^\{", output):
        if match.start() < end:
            continue
        try:
            value, length = decoder.raw_decode(output[match.start():])
        except ValueError:
            raise Failure("CLI printed an incomplete JSON document") from None
        documents.append(value)
        end = match.start() + length
    return documents


def completed_full_backup(receipt, expected_kind, *, mode):
    from backup import BACKUP_LABEL
    try:
        point, keys = receipt["database_point"], receipt["key_archive"]
        if (mode not in ("standard", "feature") or expected_kind not in ("full", "recovery")
                or (receipt["status"], point["status"], keys["status"], receipt["kind"]) != (
                    "complete", "complete", "complete", expected_kind)
                or ("restore_point" in receipt) != (expected_kind == "recovery")
                or expected_kind == "recovery" and not re.fullmatch(r"foundry_[a-f0-9]{32}", receipt["restore_point"])):
            raise ValueError
        if mode == "feature":
            coverage, key_coverage = receipt["coverage"], keys["coverage"]
            if (coverage["complete_instance_coverage"] is not True
                    or key_coverage["complete_instance_coverage"] is not False
                    or set(coverage["supported_components"]) != {"postgresql", "encrypted_configuration", "selected_deployment"}
                    or set(key_coverage["supported_components"]) != {"encrypted_configuration", "selected_deployment"}
                    or coverage["findings"] != [] or key_coverage["findings"] != []
                    or not isinstance(coverage["service_state"], list)
                    or coverage["service_state"] != key_coverage["service_state"]
                    or not re.fullmatch(r"[a-f0-9]{64}", receipt["deployment_sha256"])
                    or receipt["deployment_sha256"] != keys["deployment_sha256"]):
                raise ValueError
        if not all(receipt.get(key) for key in ("backup_id", "pgbackrest_label", "completed_at")) or (
                point["pgbackrest_label"] != receipt["pgbackrest_label"] or not keys["object_key"]
                or not re.fullmatch(BACKUP_LABEL, receipt["pgbackrest_label"])
                or not receipt["pgbackrest_label"].endswith("F")):
            raise ValueError
        return receipt
    except (KeyError, TypeError, ValueError, AttributeError):
        raise Failure("Full database backup and encrypted key archive did not both complete") from None


def initial_full_backup(state, mode):
    receipt = completed_full_backup(state.get("backup", {}).get("database"),
                                    "full" if mode == "feature" else "recovery", mode=mode)
    if state["backup"].get("last_complete_id") != receipt["backup_id"]:
        raise Failure("Initial deploy did not record its full backup")
    if mode == "feature":
        if state["backup"].get("initial_operation_id") != receipt["backup_id"]:
            raise Failure("Feature initial backup differs from its recorded operation")
    else:
        # Standard activation already took a pre-migration recovery point, so
        # initial_backup skips creating a second operation after deployment.
        attempt = state.get("last_attempt") or {}
        if (attempt.get("recovery_operation_id") != receipt["backup_id"]
                or attempt.get("backup") != receipt):
            raise Failure("Standard initial backup differs from its deployment recovery point")
    return receipt


def check_reports(operator, privileged, mode, instance, host_id):
    # report.instance derives these findings from an unreadable feature selection;
    # standard instances can instead lack permission to read /proc/<pid>/exe.
    expected = ({"runtime_selection_unavailable", "running_release_mismatch", "instance_state_inconsistent",
                 "declared_network_unavailable", "instance_degraded"} if mode == "feature" else
                {"running_release_unverified", "instance_state_inconsistent"})
    problems = operator.get("problems")
    if not isinstance(problems, list):
        raise Failure("CLI report lacks structured problems")
    codes = {item.get("code") for item in problems}
    if (codes != expected and not (mode == "standard" and not codes)
            or any(item.get("instance_id") != instance for item in problems)
            or operator.get("complete") is not (not problems)):
        raise Failure("CLI report differs from the exact unprivileged limitations")
    if (privileged.get("complete") is not True or privileged.get("problems") != []
            or any(report.get("host", {}).get("host_id") != host_id for report in (operator, privileged))):
        raise Failure("Privileged report is incomplete, has problems, or reports a different host")


def repository_snapshot(rows):
    result = {}
    for row in rows:
        if (not isinstance(row.get("Path"), str) or row["Path"] in result
                or not row.get("Hashes") or not any(row["Hashes"].values())
                or not isinstance(row.get("Size"), int) or not row.get("ModTime")):
            raise Failure("Repository listing lacks unique paths or object fingerprints")
        result[row["Path"]] = {key: row[key] for key in ("Path", "Size", "ModTime", "Hashes")}
    if not result:
        raise Failure("Backup repository listing is empty")
    return result


def repository_unchanged(before, after, archive_id, timeline):
    from backup import STANZA
    if not re.fullmatch(r"\d+-\d+", archive_id) or type(timeline) is not int or not 0 < timeline <= 0xffffffff:
        raise Failure("Source archive identity or timeline is invalid")
    if any(after.get(path) != row for path, row in before.items()):
        raise Failure("Rehearsal changed or removed an existing repository object")
    prefix = re.escape(f"foundry/archive/{STANZA}/{archive_id}/")
    source_wal = re.compile(prefix + rf"(?P<segment>{timeline:08X}[0-9A-F]{{8}})/(?P=segment)[0-9A-F]{{8}}-[0-9a-f]{{40}}\.gz")
    added = after.keys() - before.keys()
    if any(not source_wal.fullmatch(path) for path in added):
        raise Failure("Rehearsal added an object outside A's archive and timeline")
    return len(added)


def recovery_result(output, mode, *, topology_mode):
    documents = json_documents(output)
    if len(documents) != 2:
        raise Failure("Recovery did not print exactly one plan and one result")
    plan, result = documents
    if (plan.get("ok") is not True or not isinstance(plan.get("plan_token"), str)
            or not plan["plan_token"] or result.get("ok") is not True or result.get("mode") != mode
            or plan.get("mode") != mode or plan.get("host_id") != result.get("host_id")
            or not result.get("host_id") or not result.get("operation_id")):
        raise Failure("Recovery plan token or completed identity is missing")
    if mode == "takeover":
        completed_full_backup(result.get("backup"), "full", mode=topology_mode)
    elif result.get("backup") is not None:
        raise Failure("Rehearsal unexpectedly produced a backup")
    return result


def unpack_recovery(archive, destination, private_values):
    # Reuse the production archive reader's bounded, regular-file-only validation.
    # Write members ourselves so tar cannot restore permissive modes or links.
    from recover import read_archive
    files = read_archive(archive)
    destination.mkdir(mode=0o700)
    for name, content in files.items():
        path = destination / name
        path.parent.mkdir(parents=True, mode=0o700, exist_ok=True)
        path.write_bytes(content)
        path.chmod(0o600)
        for parent in path.parents:
            if parent == destination.parent:
                break
            parent.chmod(0o700)
        text = content.decode("utf-8", errors="replace")
        private_values.append(text)
        # Secret JSON leaves and env assignments must redact even when a diagnostic
        # prints an individual value rather than its original file representation.
        def remember(value):
            if isinstance(value, dict):
                for item in value.values():
                    remember(item)
            elif isinstance(value, list):
                for item in value:
                    remember(item)
            elif isinstance(value, str) and value:
                private_values.append(value)
        try:
            remember(json.loads(text))
        except ValueError:
            for line in text.splitlines():
                if "=" in line:
                    private_values.append(line.split("=", 1)[1].strip().strip('\"').strip("'"))
    return files


def recovery_fixture(snapshot):
    """Add a database marker endpoint only to the task-owned build copy."""
    path = snapshot / "internal/app/builder.go"
    source = path.read_text()
    needle = 'HTTP(healthRoutes).'
    if source.count(needle) != 1:
        raise Failure("Fixture HTTP registration no longer matches the recovery recipe")
    source = source.replace(needle, 'HTTP(healthRoutes, recoveryMarkerRoutes).')
    path.write_text(source)
    # acceptance_execute takes Git's file inventory from FIXTURE. Add code to an
    # existing inventoried file so the immutable build snapshot includes it.
    marker = snapshot / "internal/app/health.go"
    health = marker.read_text()
    imported = '\t"github.com/weiloon1234/Foundry-Go/application"\n'
    if health.count(imported) != 1:
        raise Failure("Fixture health imports no longer match the recovery recipe")
    health = health.replace(imported, imported + '\t"github.com/weiloon1234/Foundry-Go/database"\n')
    marker.write_text(health + '''
func recoveryMarkerRoutes(services application.Services) ([]http.RouteRegistration, error) {
    db, err := services.Database()
    if err != nil { return nil, err }
    path := http.StaticPath("/acceptance/recovery-marker")
    write := http.DefineEndpoint(
        http.DefineRoute(http.RouteSpec{ID: "acceptance.recovery.write", Method: http.POST, Access: http.Public}, path, http.BodyLimit(256)),
        http.EmptyQuery(), http.JSONBody(HealthLiveJSON()), http.EmptyResponse(204))
    read := http.DefineEndpoint(
        http.DefineRoute(http.RouteSpec{ID: "acceptance.recovery.read", Method: http.GET, Access: http.Public}, path),
        http.EmptyQuery(), http.EmptyBody(), http.JSONResponse(200, HealthLiveJSON()))
    return []http.RouteRegistration{
        write.Handle(func(ctx context.Context, in http.Input[http.NoPath, http.NoQuery, HealthLive]) (http.NoContent, error) {
            if len(in.Body.Status) == 0 || len(in.Body.Status) > 128 { return http.NoContent{}, http.BadRequest }
            _, err := db.Exec(ctx, "INSERT INTO acceptance_fixture (name,value) VALUES ('vm-recovery',$1)", in.Body.Status)
            if err != nil { return http.NoContent{}, http.Unavailable.WithCause(err) }
            return http.NoContent{}, nil
        }),
        read.Handle(func(ctx context.Context, _ healthInput) (HealthLive, error) {
            var value string
            err := database.ScanOne(ctx, db, "SELECT value FROM acceptance_fixture WHERE name='vm-recovery'", nil, &value)
            if err != nil { return HealthLive{}, http.Unavailable.WithCause(err) }
            return HealthLive{Status: value}, nil
        }),
    }, nil
}
''')
    return path, marker


def load_cli():
    spec = importlib.util.spec_from_file_location("vm_operator_cli", DEPLOY / "local/cli.py")
    cli = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(cli)
    return cli


def cli_entry(arguments):
    # The only builder adaptation is the existing candidate-module proxy and
    # original fixture Git provenance. SSH and CLI dispatch are unchanged.
    snapshot, proxy, *arguments = arguments
    cli = load_cli()
    cli.execute = acceptance_execute(cli.execute, Path(snapshot), Path(proxy))
    sys.argv = [str(DEPLOY / "local/cli.py"), "--app-root", snapshot, *arguments]
    try:
        return cli.main()
    except (cli.LocalError, cli.DeployError, ValueError, OSError, KeyError) as error:
        message = f"Deployment stopped: {error}"
        cli.say(message)
        refusal = os.environ.get("FOUNDRY_VM_CLI_REFUSAL_FILE")
        if refusal and isinstance(error, (cli.LocalError, cli.DeployError)):
            # This private channel originates at the CLI exception boundary.
            # Never select a lookalike line from Docker/npm/Go stderr.
            atomic_json(Path(refusal), {"refusal": message}, mode=0o600)
        return 1


def free_port():
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        return sock.getsockname()[1]


def redact(value, private_values):
    value = re.sub(r"-----BEGIN [^-]*PRIVATE KEY-----.*?(?:-----END [^-]*PRIVATE KEY-----|\Z)",
                   "[private key withheld]", value, flags=re.S)
    value = re.sub(r"(?m)^.*(?:ssh-ed25519|ssh-rsa|ecdsa-sha2-|AGE-SECRET-KEY-).*$",
                   "[key material withheld]", value)
    for secret in sorted(set(private_values), key=len, reverse=True):
        if secret:
            value = value.replace(secret, "[private value withheld]")
    return value


def reviewed_prompt(output, feature):
    """Require a complete printed review before accepting the terminal prompt."""
    if not output.rstrip().endswith("(type yes):"):
        return False
    decoder = json.JSONDecoder()
    for match in re.finditer(r"(?m)^\{", output):
        try:
            value, _ = decoder.raw_decode(output[match.start():])
        except ValueError:
            continue
        if feature:
            if value.get("ok") is True and isinstance(value.get("plan_token"), str) and value["plan_token"]:
                return True
        elif {"version", "commit", "tree_state", "architecture", "instance"} <= value.keys():
            return True
    return False


def wait_control_socket(path, *, alive, timeout=240, margin=30, minimum=150):
    """Observe natural ControlPersist exit locally, then leave logind a margin."""
    started = time.monotonic()
    absent_since = None
    while True:
        now = time.monotonic()
        if not alive():
            raise Failure("Interactive CLI exited during the SSH idle pause")
        if path.exists() or path.is_symlink():
            absent_since = None
        elif absent_since is None:
            absent_since = now
        if absent_since is not None and now - absent_since >= margin and now - started >= minimum:
            return
        if now - started >= timeout:
            raise Failure("SSH control socket did not disappear with the required idle margin")
        time.sleep(0.2)


def interactive_command(argv, *, env, stream, feature, before_confirm, timeout):
    """Run the unmodified CLI on a PTY; retain output privately for Run.command."""
    master, slave = os.openpty()
    process = None
    try:
        process = subprocess.Popen(argv, stdin=slave, stdout=slave, stderr=slave,
                                   env=env, start_new_session=True)
        os.close(slave)
        slave = None
        deadline = time.monotonic() + timeout
        output = bytearray()
        confirmed = False
        while True:
            if time.monotonic() >= deadline:
                raise Failure("Interactive CLI deadline exceeded")
            readable, _, _ = select.select([master], [], [], 0.2)
            if readable:
                try:
                    chunk = os.read(master, 65536)
                except OSError as error:
                    if error.errno != errno.EIO:
                        raise
                    chunk = b""  # Linux PTY EOF after the slave closes.
                if not chunk:
                    break
                stream.write(chunk.decode("utf-8", errors="replace"))
                output.extend(chunk)
                if len(output) > 4 * 1024 * 1024:
                    raise Failure("Interactive CLI output limit exceeded")
                if not confirmed and reviewed_prompt(output.decode("utf-8", errors="replace"), feature):
                    before_confirm(lambda: process.poll() is None)
                    if time.monotonic() >= deadline:
                        raise Failure("Interactive CLI deadline exceeded before confirmation")
                    os.write(master, b"yes\n")
                    confirmed = True
        process.wait(timeout=max(0.1, deadline - time.monotonic()))
        if not confirmed:
            raise Failure("Interactive CLI exited without a complete review and confirmation prompt")
        return process.returncode
    finally:
        if process is not None:
            # Retain ownership of the entire child group even on timeout/EOF.
            try:
                os.killpg(process.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
            process.wait()
        os.close(master)
        if slave is not None:
            os.close(slave)


def failed_deploy_state(output, mode, failed, good, operation):
    """Reject unrelated failures, ambiguous identities and unproven recovery."""
    try:
        state = json.loads(output)["state"]
        attempt = state["last_attempt"]
        current = state["current"]["version"]
        expected = ("unknown", "needs_reconciliation", "operation_cleanup_pending", failed) if mode == "feature" else (
            "rolled_back", "failed", "readiness_failed", good)
        if (state["status"], attempt["outcome"], attempt["error"], current) != expected or (
                attempt["action"], attempt["phase"], attempt["release"]) != ("deploy", "health", failed):
            raise ValueError
        if mode == "feature" and attempt["operation_id"] != operation:
            raise ValueError
        return current
    except (ValueError, KeyError, TypeError):
        raise Failure("Failed deploy status does not prove the expected health failure/recovery") from None


def cloud_config(public_key):
    # No acceptance packages or service suppression in the guest. Missing
    # droplet stock packages are installed by cloud-init, before operator work.
    stock = """set -eu
for package in fwupd packagekit; do
  if dpkg-query -W -f='${Status}' "$package" 2>/dev/null | grep -qx 'install ok installed'; then
    echo "STOCK $package present"
  else
    echo "STOCK $package absent; installing Ubuntu package to mirror droplet"
    missing="$missing $package"
  fi
done
apt-get update -qq
if [ -n "$missing" ]; then DEBIAN_FRONTEND=noninteractive apt-get install -y $missing; fi
"""
    flags = "FOUNDRY_DISPOSABLE_HOST=1\nFOUNDRY_ACCEPTANCE_SYNTHETIC_CERT=1\n"
    return {
        "users": [{"name": "acceptance-operator", "shell": "/bin/bash", "lock_passwd": True,
                   "sudo": "ALL=(ALL) NOPASSWD:ALL", "ssh_authorized_keys": [public_key]}],
        "ssh_pwauth": False, "disable_root": True,
        "ssh": {"emit_keys_to_console": False},
        "write_files": [
            {"path": VM_MARKER, "owner": "root:root", "permissions": "0600", "content": "disposable acceptance VM\n"},
            {"path": "/etc/environment", "append": True, "content": "\n" + flags},
            {"path": "/etc/sudoers.d/acceptance-operator-env", "owner": "root:root", "permissions": "0440",
             "content": 'Defaults:acceptance-operator env_keep += "FOUNDRY_DISPOSABLE_HOST FOUNDRY_ACCEPTANCE_SYNTHETIC_CERT"\n'},
            {"path": "/etc/systemd/system.conf.d/foundry-acceptance.conf", "content":
             "[Manager]\nDefaultEnvironment=FOUNDRY_DISPOSABLE_HOST=1\n"},
            # pgBackRest verifies only DNS names, so the runner's S3 stand-in needs one.
            {"path": "/etc/hosts", "append": True, "content": "\n10.0.2.2 s3\n"},
        ],
        "manage_etc_hosts": False,
        "runcmd": [["systemctl", "daemon-reexec"], ["sh", "-c", 'missing=""\n' + stock]],
    }


def fixture_declarations(snapshot, mode, port, *, project_id=None, recovery=False):
    project = json.loads((FIXTURE / "deploy/project.json").read_text())
    project["id"] = project_id or str(uuid.uuid4())
    atomic_json(snapshot / "deploy/project.json", project)
    profile = json.loads((FIXTURE / "deploy/environments/acceptance.json").read_text())
    profile["instance_id"] = str(uuid.uuid4())
    profile.update(architecture="amd64", edge_origin_lockdown=False,
                   domain="deploy-vm.acceptance.invalid")
    profile["host"] = {"address": "127.0.0.1", "port": port, "host_id": None}
    if mode == "feature":
        topology = json.loads((snapshot / "deploy/rehearsal/topology.json").read_text())
        if recovery:
            for role in topology["roles"]:
                if role["name"] == "relay":
                    role["recovery_rehearsal_safe"] = True
        placement = json.loads((snapshot / "deploy/rehearsal/environments/acceptance-run.json").read_text())["placement"]
        profile.update(schema=3, placement=placement, listeners={})
    else:
        topology = json.loads((FIXTURE / "deploy/topology.json").read_text())
    atomic_json(snapshot / "deploy/topology.json", topology)
    topology = validate_topology(topology)
    atomic_json(snapshot / "deploy/environments/acceptance.json", profile)
    return project, profile, topology


class Run:
    def __init__(self, private, logs, scenario="operator"):
        self.private, self.logs = private, logs
        self.values = []
        self.resources = containers.Resources(private)
        self.guests = []
        self.active = None
        self.scenario = scenario
        self.candidates = []
        self.project_id = str(uuid.uuid4())
        self.mode = "setup"

    @property
    def ssh(self):
        return self.active.ssh if self.active else None

    @property
    def log_prefix(self):
        return self.active.directory.name if self.active else self.mode

    def note(self, message):
        print(message, flush=True)
        with (self.logs / "steps.log").open("a") as stream:
            stream.write(message + "\n")

    @contextmanager
    def phase(self, name):
        label = f"{self.scenario}/{self.mode}: {self.log_prefix}/{name}"
        started = time.monotonic()
        outcome = "failed"
        self.note(f"== {label}: started ==")
        try:
            yield
            outcome = "completed"
        finally:
            self.note(f"TIMING {label}: {outcome} elapsed_seconds={time.monotonic() - started:.1f}")

    def command(self, name, argv, *, timeout=600, input=None, env=None, publish=False, check=True,
                delayed=None, combined=False, cli_refusal=False):
        refusal_file = self.private / "cli-refusal.json"
        if cli_refusal:
            if publish:
                raise Failure("CLI refusal publication cannot publish raw command output")
            refusal_file.unlink(missing_ok=True)
            env = {**(os.environ if env is None else env), "FOUNDRY_VM_CLI_REFUSAL_FILE": str(refusal_file)}
        self.note(f"== {self.scenario}/{self.mode}: {self.log_prefix}/{name} ==")
        output = self.private / "command-output"
        with output.open("w+") as stream, (self.private / "command-error").open("w+") as errors:
            process = None
            try:
                if delayed is not None:
                    code = interactive_command([str(x) for x in argv], env=env, stream=stream,
                        feature=self.mode == "feature", before_confirm=delayed, timeout=timeout)
                else:
                    process = subprocess.Popen([str(x) for x in argv], stdin=subprocess.PIPE,
                                               stdout=stream, stderr=errors, text=True,
                                               env=env, start_new_session=True)
                    process.communicate(input, timeout=timeout)
                    code = process.returncode
            except BaseException as error:
                if process is not None:
                    try:
                        os.killpg(process.pid, signal.SIGKILL)
                    except ProcessLookupError:
                        pass
                    process.wait()
                if publish:
                    stream.seek(0)
                    errors.seek(0)
                    (self.logs / f"{self.log_prefix}-{name}.log").write_text(redact(stream.read() + errors.read(), self.values))
                reason = str(error) if isinstance(error, Failure) else "interrupted/deadline"
                self.note(f"FAIL {self.scenario}/{self.mode}: {name}: {reason}")
                raise
            stream.seek(0)
            value = stream.read()
            errors.seek(0)
            diagnostics = errors.read()
        self.note(f"{name}: exit={code}")
        if code and cli_refusal and refusal_file.exists():
            receipt = json.loads(refusal_file.read_text())
            message = receipt.get("refusal")
            if (set(receipt) != {"refusal"} or not isinstance(message, str)
                    or not message.startswith("Deployment stopped: ") or len(message) > 65536):
                raise Failure("Invalid private CLI refusal receipt; output withheld")
            # Redact before selecting a line so multiline secrets stay indivisible.
            line = redact(message, self.values).splitlines()[0]
            if len(line) > 4096 or any(ord(character) < 32 for character in line):
                raise Failure("Invalid private CLI refusal line; output withheld")
            self.note(line)
            (self.logs / f"{self.log_prefix}-{name}-refusal.log").write_text(line + "\n")
        if publish:
            (self.logs / f"{self.log_prefix}-{name}.log").write_text(redact(value + diagnostics, self.values))
        if check and code:
            # Only CLI designed-safe diagnostics and public boot/install output
            # opt into publishing. Never emit raw argv, private stdin or app logs.
            if publish:
                print(redact((value + diagnostics)[-12000:], self.values), file=sys.stderr)
            raise Failure(f"{name}: exit={code}")
        return code, value + diagnostics if combined else value

    def remote(self, name, command, **kwargs):
        return self.command(name, [*self.ssh, command], **kwargs)

    def stop_vm(self, guest=None):
        guest = guest or self.active
        if guest is None:
            return
        if guest.qemu is not None:
            guest.qemu.terminate()
            try:
                guest.qemu.wait(timeout=15)
            except subprocess.TimeoutExpired:
                guest.qemu.kill()
                guest.qemu.wait()
            guest.qemu = None
        self.guests.remove(guest)
        if self.active is guest:
            self.active = None
        if guest.serial.exists():
            value = guest.serial.read_text(errors="replace")
            (self.logs / f"{guest.directory.name}-serial.log").write_text(redact(value, self.values))
        shutil.rmtree(guest.directory)

    def stop_vms(self):
        failures = []
        for guest in list(self.guests):
            try:
                self.active = guest
                self.diagnostics()
                self.stop_vm()
            except Exception:
                failures.append("QEMU/serial")
        return failures

    def cleanup_builders(self):
        # These IDs were generated inside this run's private fixture copies.
        # Stop daemon-owned writers before deleting their source mounts.
        failures = []
        for candidate in self.candidates:
            project_file = candidate / "fixture/deploy/project.json"
            if not project_file.exists():
                continue
            project = json.loads(project_file.read_text())
            owner = candidate / "vm-project-id"
            if not owner.exists() or owner.read_text() != project["id"]:
                continue
            project_id = project["id"]
            try:
                result = containers.run(["docker", "ps", "-aq", "--filter", "label=foundry.project=" + project_id])
                for identifier in result.stdout.splitlines():
                    containers.run(["docker", "rm", "-f", "-v", identifier])
                builders = containers.run(["docker", "buildx", "ls", "--format", "{{.Name}}"])
                builder = "foundry-" + project_id + "-amd64"
                if builder in (name.rstrip(" *") for name in builders.stdout.splitlines()):
                    containers.run(["docker", "buildx", "rm", "--force", builder])
                images = containers.run(["docker", "image", "ls", "-q", "--filter", "label=foundry.project=" + project_id])
                for identifier in set(images.stdout.splitlines()):
                    containers.run(["docker", "image", "rm", identifier])
                volumes = containers.run(["docker", "volume", "ls", "-q"])
                cache = "foundry-cache-" + project_id + "-amd64"
                if cache in volumes.stdout.splitlines():
                    containers.run(["docker", "volume", "rm", cache])
            except (OSError, RuntimeError):
                failures.append(project_id)
        return failures

    def cleanup(self):
        failures = []
        for name, action in (("QEMU/serial", self.stop_vms), ("builders", self.cleanup_builders),
                             ("S3", self.resources.cleanup)):
            try:
                failures.extend(action() or [])
            except Exception:
                failures.append(name)
        return failures

    def diagnostics(self):
        if self.ssh:
            try:
                self.remote("units", "systemctl list-units --state=running --no-pager; "
                            "systemctl list-units --failed --no-pager", timeout=30, publish=True, check=False)
                # Package-manager activity explains plan drift on a fresh cloud host.
                self.remote("apt-activity", "ps -eo pid,etime,cmd | grep -E '[a]pt|[d]pkg|[u]nattended|[p]ackagekit|[f]wupd'; "
                            "sudo -n tail -n 60 /var/log/apt/history.log; "
                            "sudo -n journalctl --no-pager -n 80 -u apt-daily.service -u apt-daily-upgrade.service "
                            "-u unattended-upgrades.service -u packagekit.service -u fwupd.service",
                            timeout=60, publish=True, check=False)
                # The CLI deliberately hides failed host commands; this disposable guest may show
                # them, redacted with every value this run generated.
                self.remote("host-failure", "sudo -n python3 -c " + shlex.quote(
                            "import json\nr = json.load(open('/etc/foundry/registry.json'))\n"
                            "for i in r.get('instances', {}).values():\n"
                            "    p = i.get('provisioning', {})\n"
                            "    print(i.get('name'), p.get('state'), p.get('completed_steps'), p.get('failed_step'))\n")
                            + "; sudo -n journalctl --no-pager -p warning -n 120; "
                            "sudo -n sh -c 'tail -n 80 /var/log/pgbackrest/*.log /var/log/postgresql/*.log 2>/dev/null'",
                            timeout=60, publish=True, check=False)
                # Read-only repository reachability: endpoint lines only, never keys or cipher settings.
                self.remote("backup-repository", "sudo -n grep -E '^repo1-(s3-endpoint|storage-port|s3-bucket|s3-uri-style)=' "
                            "/etc/foundry/pgbackrest/foundry.conf; "
                            "sudo -n runuser -u foundry-postgres -- pgbackrest --config=/etc/foundry/pgbackrest/foundry.conf "
                            "--stanza=foundry --log-level-console=detail info; "
                            "sudo -n ls -la /etc/foundry/pgbackrest /var/lib/foundry/backup 2>&1 | head -20; "
                            "port=$(sudo -n sed -n 's/^repo1-storage-port=//p' /etc/foundry/pgbackrest/foundry.conf); "
                            "curl -sS -o /dev/null -w 'curl %{http_code} %{ssl_verify_result}\\n' \"https://s3:$port/\"",
                            timeout=90, publish=True, check=False)
            except Exception:
                self.note("Systemd diagnostic collector unavailable")

    def boot(self, image, key, mode, *, name=None, address="127.0.0.1"):
        if len(self.guests) >= 2:
            raise Failure("Refusing to run more than two VMs")
        self.mode = mode
        guest = self.private / (mode + "-" + name if name else mode)
        guest.mkdir(mode=0o700)
        port = free_port()
        (guest / "user-data").write_text("#cloud-config\n" + json.dumps(cloud_config(key.with_suffix(".pub").read_text().strip())))
        (guest / "meta-data").write_text(json.dumps({"instance-id": "foundry-" + uuid.uuid4().hex,
                                                   "local-hostname": "foundry-acceptance"}))
        self.command("seed", ["cloud-localds", guest / "seed.img", guest / "user-data", guest / "meta-data"])
        self.command("overlay", ["qemu-img", "create", "-f", "qcow2", "-F", "qcow2", "-b", image, guest / "disk.qcow2", "32G"])
        config = guest / "ssh_config"
        config.write_text(f"Host *\n  UserKnownHostsFile {guest / 'known_hosts'}\n  GlobalKnownHostsFile /dev/null\n")
        ssh = [shutil.which("ssh"), "-F", str(config), "-o", "StrictHostKeyChecking=accept-new",
                    "-o", "BatchMode=yes", "-o", "IdentitiesOnly=yes", "-o", "ConnectTimeout=5",
                    "-i", str(key), "-p", str(port), "acceptance-operator@" + address]
        self.active = Guest(guest, port, config, address, ssh)
        self.guests.append(self.active)
        with self.phase("boot"):
            self.launch()
        with self.phase("cloud-init+upgrade"):
            self.remote("cloud-init", "sudo -n cloud-init status --wait --long", timeout=900, publish=True)
            # Prepare the host as the deployment guide tells operators: current packages, then reboot if required.
            self.remote("host-upgrade", "sudo -n env DEBIAN_FRONTEND=noninteractive sh -c "
                        "'apt-get -o DPkg::Lock::Timeout=600 update && apt-get -o DPkg::Lock::Timeout=600 -y upgrade'",
                        timeout=1500, publish=True)
            if self.remote("reboot-required", "test -e /var/run/reboot-required && echo yes || echo no",
                           publish=True)[1].strip() == "yes":
                subprocess.run([*self.ssh, "sudo -n systemctl reboot"], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=30)
                self.active.qemu.wait(timeout=300)  # -no-reboot turns the guest reboot into a QEMU exit.
                self.launch()
                self.remote("settle", "systemctl is-system-running --wait || true", timeout=600, publish=True)
            self.remote("stock-packages", "sudo -n grep '^STOCK ' /var/log/cloud-init-output.log; "
                        "dpkg-query -W fwupd packagekit ubuntu-pro-client; "
                        "systemctl list-timers --all --no-pager", publish=True)
        return self.active

    def launch(self):
        guest = self.active
        with guest.serial.open("a") as console:
            guest.qemu = subprocess.Popen(["qemu-system-x86_64", "-enable-kvm", "-cpu", "host", "-smp", "2", "-m", "4096",
                "-display", "none", "-monitor", "none", "-serial", "stdio", "-no-reboot",
                "-drive", f"file={guest.directory / 'disk.qcow2'},if=virtio,format=qcow2",
                "-drive", f"file={guest.directory / 'seed.img'},if=virtio,format=raw,readonly=on",
                "-netdev", f"user,id=net0,hostfwd=tcp:{guest.address}:{guest.port}-:22", "-device", "virtio-net-pci,netdev=net0"],
                stdin=subprocess.DEVNULL, stdout=console, stderr=console)
        self.note("Waiting up to 300s for guest SSH (readiness only)")
        deadline = time.monotonic() + 300
        while time.monotonic() < deadline:
            if guest.qemu.poll() is not None:
                raise Failure(f"QEMU exited before SSH: exit={guest.qemu.returncode}")
            result = subprocess.run([*self.ssh, "true"], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=10)
            if result.returncode == 0:
                return
            time.sleep(2)
        raise Failure("Guest SSH readiness deadline exceeded")

    def trust(self, certs):
        # Stage via private SSH stdin; no CA keys in cloud-init or serial output.
        self.remote("ca-directory", "sudo -n install -d -m 0700 " + shlex.quote(ACCEPTANCE_CA))
        for name in ("ca.crt", "ca.key"):
            self.remote("stage-" + name, "sudo -n sh -c " + shlex.quote(
                "umask 077; cat > " + shlex.quote(ACCEPTANCE_CA + "/" + name)), input=(certs / name).read_text())
        self.remote("trust-ca", "sudo -n install -m 0644 " + shlex.quote(ACCEPTANCE_CA + "/ca.crt") + " "
                    "/usr/local/share/ca-certificates/acceptance-ca.crt && sudo -n update-ca-certificates")

    def operator(self, snapshot, proxy, key, storage, *, repository=None, certs=None):
        cli = load_cli()
        guest, port, config = self.active.directory, self.active.port, self.active.config
        project, profile, topology = fixture_declarations(snapshot, self.mode, port, project_id=self.project_id,
                                                         recovery=self.scenario == "recovery")
        (snapshot.parent / "vm-project-id").write_text(project["id"])
        settings = {"schema": 1, "hosts": {f"127.0.0.1:{port}": {
            "target": "acceptance-operator@127.0.0.1", "key_path": str(key)}}}
        atomic_json(snapshot / ".deploy/settings.json", settings, mode=0o600)
        # A private OpenSSH config isolates known_hosts without changing SSH's
        # implementation or touching the developer's ~/.ssh configuration.
        binary = guest / "bin"
        binary.mkdir()
        (binary / "ssh").write_text("#!/bin/sh\nexec " + shlex.join([shutil.which("ssh"), "-F", str(config)]) + ' "$@"\n')
        (binary / "ssh").chmod(0o700)
        env = {**os.environ, "PATH": str(binary) + os.pathsep + os.environ["PATH"],
               "ENVIRONMENT": "acceptance", "CONFIRM": "yes"}
        # The CLI refuses LINES/FOLLOW outside logs, as it would for an operator's shell.
        for name in ("LINES", "FOLLOW"):
            env.pop(name, None)
        from acceptance_host import storage_secrets
        from runtime_config import service_secret_requirements
        disks = tomllib.loads((snapshot / "config/deploy.toml").read_text())["app"]["services"]["storage"]["disks"]
        private = storage_secrets(disks, storage, storage["recipient"])
        private["cloudflare_dns_token"] = "acceptance-synthetic-token"
        parsed = parse_topology(topology, registered=True)
        if self.mode == "feature":
            requirements = service_secret_requirements(parsed, parse_profile(profile, parsed), {})
            private["configuration"] = {row["key"]: secrets.token_urlsafe(32) for row in requirements}
        for name, value in private.items():
            if not value:
                continue
            name = "config" if name == "configuration" else name
            path = guest / (name + ".secret")
            path.write_text(value if isinstance(value, str) else json.dumps(value))
            path.chmod(0o600)
            if isinstance(value, str):
                self.values.append(value)
            elif name == "config":
                self.values.extend(value.values())
            env["FOUNDRY_SECRET_" + name.upper() + "_FILE"] = str(path)

        def invoke(command, action, *, label=None, delay=False, fails=False, refusal=None, **extra):
            # Entry point and normal process exit are exercised, including CLI
            # validation, plans, upload and every real SSH/sudo request.
            name = label or action
            environment = {**env, "ACTION": action, **extra}
            paused = None
            if delay:
                environment.pop("CONFIRM", None)
                controls = cli.control_directory()
                existing = set(controls.iterdir())

                def paused(alive):
                    sockets = set(controls.iterdir()) - existing
                    if len(sockets) != 1 or not stat.S_ISSOCK(next(iter(sockets)).lstat().st_mode):
                        raise Failure("Expected exactly one live CLI SSH control socket at review")
                    self.note(f"== {self.scenario}/{self.mode}: {name}-reviewed-pause ==")
                    self.note("Waiting for natural SSH ControlPersist exit plus 30s; no SSH probes during pause")
                    started = time.monotonic()
                    wait_control_socket(next(iter(sockets)), alive=alive)
                    if set(controls.iterdir()) - existing:
                        raise Failure("CLI SSH control socket reappeared before confirmation")
                    self.note(f"== {self.scenario}/{self.mode}: {name}-confirm-after-idle ==")
                    self.note(f"SSH control socket absent; confirming after {time.monotonic() - started:.1f}s")

            phase = extra.get("RECOVER_MODE") or ("backup" if action == "backup-run" else name)
            with self.phase(phase):
                code, output = self.command(name,
                    [sys.executable, "-B", __file__, "--cli", snapshot, proxy, command],
                    env=environment, timeout=1800, delayed=paused, check=not (fails or refusal), combined=bool(fails or refusal),
                    publish=action not in ("logs", "build"), cli_refusal=action == "build")
            if fails and (code != 1 or not re.search(r"(?m)^Deployment stopped: readiness_failed: ", output)
                          or "Traceback (most recent call last)" in output):
                raise Failure("Bad release did not exit with the designed readiness_failed refusal")
            if refusal and (code != 1 or not re.search(r"(?m)^Deployment stopped: " + re.escape(refusal) + ": ", output)
                            or "Traceback (most recent call last)" in output):
                raise Failure("CLI did not return the designed " + refusal + " refusal")
            return output

        with self.phase("bootstrap"):
            invoke("bootstrap", "inspect")
            invoke("bootstrap", "prepare")
        profile = cli.read_json(snapshot / "deploy/environments/acceptance.json")
        units = instance_units({"name": instance_name(project, profile), "profile": profile, "topology": topology})
        versions = []

        def build(label):
            invoke("release", "build", label="build-" + label)
            releases = snapshot / "dist/releases" / project["id"] / profile["instance_id"]
            manifests = sorted(releases.glob("*/manifest.json"), key=lambda p: p.parent.name)
            manifest = json.loads(manifests[-1].read_text())
            version = manifest["version"]
            if version in versions:
                raise Failure("Build did not produce a distinct release")
            versions.append(version)
            return version

        def healthy(label, version, *, expected="healthy"):
            status = json.loads(invoke("deploy", "status", label="status-" + label))
            if status["state"]["status"] != expected or status["state"]["current"]["version"] != version:
                raise Failure("Deployed release is not healthy/current")
            for unit in units.values():
                _, output = self.remote("role-" + label + "-" + unit, "systemctl show " + shlex.quote(unit)
                    + " -p ActiveState -p SubState -p MainPID", publish=True)
                state = dict(line.split("=", 1) for line in output.splitlines() if "=" in line)
                if state.get("ActiveState") != "active" or state.get("SubState") != "running" or int(state.get("MainPID", "0")) <= 0:
                    raise Failure("Placed role is not running: " + unit)
            invoke("deploy", "logs", label="logs-" + label, LINES="10")
            report = json.loads(invoke("deploy", "report", label="report-" + label))
            privileged = json.loads(self.remote("report-root-" + label,
                "sudo -n /usr/local/bin/foundry-host-report --pretty", timeout=240, publish=True)[1])
            current_profile = json.loads((snapshot / "deploy/environments/acceptance.json").read_text())
            check_reports(report, privileged, self.mode, profile["instance_id"], current_profile["host"]["host_id"])
            return status

        if self.scenario == "recovery":
            version = build("recovery")
            invoke("deploy", "deploy", label="deploy-source", RELEASE=version)
            state = healthy("source", version)["state"]
            initial = initial_full_backup(state, self.mode)
            self.recovery(snapshot, key, env, settings, invoke, healthy, profile,
                          version, initial, repository, certs)
            return

        for number in (1, 2):
            version = build(str(number))
            invoke("deploy", "deploy", label=f"deploy-{number}", RELEASE=version, delay=number == 2)
            healthy(str(number), version)
            self.diagnostics()
        self.note(f"PASS {self.scenario}/{self.mode}: delayed interactive deploy after all CLI SSH sessions expired")

        # A supported fixture configuration fails HTTP readiness in both modes.
        # Only the private build copy changes; retained good releases keep healthy config.
        configuration = snapshot / "config/deploy.toml"
        original = configuration.read_text()
        if tomllib.loads(original)["fixture"]["readiness"] != "healthy" or original.count('readiness = "healthy"') != 1:
            raise Failure("Fixture readiness input no longer matches the failure recipe")
        self.note(f"== {self.scenario}/{self.mode}: prepare-readiness-failure ==")
        try:
            configuration.write_text(original.replace('readiness = "healthy"', 'readiness = "fail"'))
            failed = build("unready")
        finally:
            configuration.write_text(original)
        operation = str(uuid.uuid4())
        invoke("deploy", "deploy", label="deploy-unready", RELEASE=failed, OPERATION=operation, fails=True)
        observed = invoke("deploy", "status", label="status-failed")
        current = failed_deploy_state(observed, self.mode, failed, versions[1], operation)
        self.note(f"== {self.scenario}/{self.mode}: reconcile-failed-deploy ==")
        reconciled = json.loads(invoke("deploy", "reconcile"))
        expected = "stopped" if self.mode == "feature" else "rolled_back"
        if reconciled.get("ok") is not True or reconciled.get("status") != expected or reconciled.get("version") != current:
            raise Failure("Reconcile did not preserve the expected release/state")
        if self.mode == "feature":
            if reconciled.get("cleanup_reconciled") is not True:
                raise Failure("Feature cleanup was not reconciled")
        elif reconciled.get("unchanged") is not True:
            raise Failure("Reconcile after automatic code restoration was not a no-op")
        status = json.loads(invoke("deploy", "status", label="status-reconciled"))["state"]
        if status["status"] != expected or status["current"]["version"] != current:
            raise Failure("Status disagrees with reconciliation")
        # Standard deploy auto-restores the second release; explicitly roll back
        # one further good release. Feature cleanup retains the failed selection.
        target = versions[1] if self.mode == "feature" else versions[0]
        invoke("deploy", "rollback", label="rollback-recovery", TARGET=target,
               EXPECTED_CURRENT=status["current"]["version"], OPERATION=str(uuid.uuid4()),
               delay=self.mode == "feature")
        healthy("recovered", target, expected="healthy" if self.mode == "feature" else "rolled_back")
        if self.mode == "feature":
            self.note(f"PASS {self.scenario}/{self.mode}: delayed interactive recovery rollback after all CLI SSH sessions expired")
        self.note(f"PASS {self.scenario}/{self.mode}: failed deploy, reconcile, rollback, healthy roles, logs and report")
        plan = json.loads(invoke("deploy", "tools-plan"))
        if plan["upgrade"] is not None or plan["prune_bundles"] or plan["prune_upgrade_journals"]:
            invoke("deploy", "tools-apply", PLAN_TOKEN=plan["plan_token"], OPERATION=str(uuid.uuid4()))
        self.note(f"PASS {self.scenario}/{self.mode}: real CLI bootstrap, three rebuilt releases, recovery and tools plan")

    def recovery(self, snapshot, key, env, settings, invoke, healthy, profile,
                 version, initial, repository, certs):
        from acceptance_backup import rclone
        source = self.active
        profile_path = snapshot / "deploy/environments/acceptance.json"
        source_profile = json.loads(profile_path.read_text())
        instance = profile["instance_id"]

        def listing():
            self.note(f"== recovery/{self.mode}: repository-inventory ==")
            try:
                output = rclone(repository, "lsjson", "acceptance:" + repository["backup_bucket"],
                    "--recursive", "--files-only", "--hash", "--ca-cert", str(certs / "ca.crt"), "--retries", "1")
            except RuntimeError:
                raise Failure("Repository inventory failed (private output withheld)") from None
            return repository_snapshot(json.loads(output.stdout))

        def postgres_state():
            sql = ("SELECT json_build_object('archive_mode', current_setting('archive_mode'), "
                   "'in_recovery', pg_is_in_recovery(), 'timeline', timeline_id) FROM pg_control_checkpoint();")
            script = ("import sys\nsys.path.insert(0, '/usr/local/lib/foundry/current/lib')\n"
                      "from host import Host\nimport postgres\nhost = Host()\n"
                      f"print(postgres._psql(host, host.read_registry(), {sql!r}).stdout)")
            return json.loads(self.remote("postgres-archiving", "sudo -n python3 -c " + shlex.quote(script), timeout=60)[1])

        def fetch(label, backup, destination):
            self.note(f"== recovery/{self.mode}: {label} ==")
            try:
                rclone(repository, "copyto", f"acceptance:{repository['backup_bucket']}/{backup['key_archive']['object_key']}",
                       str(destination), "--ca-cert", str(certs / "ca.crt"), "--retries", "1")
            except RuntimeError:
                raise Failure(label + ": repository download failed (private output withheld)") from None
            destination.chmod(0o600)
            if not destination.stat().st_size:
                raise Failure(label + ": encrypted key archive is empty")
            self.note(label + ": exit=0")

        def registry():
            script = ("import json\nr = json.load(open('/etc/foundry/registry.json'))\n"
                      f"e = r['instances'][{instance!r}]\n"
                      "print(json.dumps({'host_id': r['host_id'], 'state': e['provisioning']['state'], "
                      "'http_port': e['allocations']['http_port'], 'recovered_from': e.get('recovered_from')}))")
            return json.loads(self.remote("registry", "sudo -n python3 -c " + shlex.quote(script))[1])

        def marker_read(expected):
            port = registry()["http_port"]
            result = self.remote("marker-read", f"curl --fail --silent --show-error --max-time 15 "
                                 f"http://127.0.0.1:{port}/acceptance/recovery-marker")[1]
            if json.loads(result).get("status") != expected:
                raise Failure("Restored localhost application marker differs from VM A")

        marker = "vm-recovery-" + self.mode + "-" + uuid.uuid4().hex
        self.values.append(marker)
        port = registry()["http_port"]
        self.remote("marker-write", "curl --fail --silent --show-error --max-time 15 --header 'Content-Type: application/json' --data-binary @- "
                    f"http://127.0.0.1:{port}/acceptance/recovery-marker", input=json.dumps({"status": marker}))
        marker_read(marker)
        receipt = completed_full_backup(json.loads(invoke("deploy", "backup-run", BACKUP_KIND="full")), "full", mode=self.mode)
        if receipt["backup_id"] == initial["backup_id"]:
            raise Failure("Explicit full backup reused the initial deploy backup")
        self.note(f"PASS recovery/{self.mode}: A initial full backup, unique application write, explicit full/key backup")

        # Same operator procedure as the deployment guide: repository download,
        # local age decryption, then a private extracted folder. No guest key reads.
        export = self.private / (self.mode + "-recovery")
        export.mkdir(mode=0o700)
        encrypted, archive = export / "keys.age", export / "recovery.tar"
        fetch("fetch-encrypted-recovery-archive", receipt, encrypted)
        self.command("decrypt-recovery", ["age", "--decrypt", "-i", self.private / "age-identity",
                                         "-o", archive, encrypted])
        archive.chmod(0o600)
        recovery_dir = export / "decrypted"
        unpack_recovery(archive, recovery_dir, self.values)
        # Recovery must source all credentials from the downloaded folder.
        for name in list(env):
            if name.startswith("FOUNDRY_SECRET_"):
                del env[name]
        self.note(f"PASS recovery/{self.mode}: repository archive decrypted locally into private FOUNDRY_RECOVERY_DIR")

        def target(name, address):
            guest = self.boot(self.private / "base.img", key, self.mode, name=name, address=address)
            self.trust(certs)
            self.remote("empty-target", "test ! -e /etc/foundry/registry.json && test ! -e /usr/bin/pgbackrest")
            settings["hosts"][f"{address}:{guest.port}"] = {
                "target": "acceptance-operator@" + address, "key_path": str(key)}
            atomic_json(snapshot / ".deploy/settings.json", settings, mode=0o600)
            binary = guest.directory / "bin"
            binary.mkdir(mode=0o700)
            (binary / "ssh").write_text("#!/bin/sh\nexec " + shlex.join(
                [shutil.which("ssh"), "-F", str(guest.config)]) + ' "$@"\n')
            (binary / "ssh").chmod(0o700)
            env["PATH"] = str(binary) + os.pathsep + os.environ["PATH"]
            # RECOVER_HOST changes only the address; the CLI intentionally uses
            # the profile port. Stage that transport input before reviewing recovery.
            staged = json.loads(json.dumps(source_profile))
            staged["host"]["port"] = guest.port
            atomic_json(profile_path, staged)
            return guest, staged

        # Prevent an unrelated scheduled full/diff backup from changing the baseline.
        # A stays live and continues normal WAL archiving throughout the rehearsal.
        for kind in ("full", "diff"):
            unit = f"foundry-backup-{kind}-cluster"
            self.remote("pause-source-" + kind + "-timer", f"sudo -n systemctl stop {unit}.timer && "
                f"test \"$(systemctl show {unit}.service -p ActiveState --value)\" = inactive")
        source_postgres = postgres_state()
        if source_postgres["archive_mode"] != "on" or source_postgres["in_recovery"] is not False:
            raise Failure("Source A is not a live archiving primary")
        from backup import STANZA
        inventory = json.loads(self.remote("source-archive-identity", "sudo -n runuser -u foundry-postgres -- "
            "pgbackrest --config=/etc/foundry/pgbackrest/foundry.conf " + f"--stanza={STANZA} --output=json info")[1])
        if len(inventory) != 1 or inventory[0]["name"] != STANZA or len(inventory[0]["archive"]) != 1:
            raise Failure("Fresh source does not have exactly one stanza/archive identity")
        archive_id = inventory[0]["archive"][0]["id"]
        before = listing()
        rehearsal, staged = target("b", "127.0.0.2")
        if source.qemu.poll() is not None:
            raise Failure("Source A exited before rehearsal")
        result = recovery_result(invoke("bootstrap", "recover", label="recover-rehearsal",
            RECOVER_HOST=rehearsal.address, RECOVER_MODE="rehearsal", FOUNDRY_RECOVERY_DIR=str(recovery_dir),
            RELEASE=version), "rehearsal", topology_mode=self.mode)
        observed = registry()
        self.check_recovered(observed, result, source_profile, "rehearsal")
        if postgres_state()["archive_mode"] != "off":
            raise Failure("Rehearsal PostgreSQL has archiving enabled")
        marker_read(marker)
        if json.loads(profile_path.read_text()) != staged:
            raise Failure("Rehearsal changed the local profile")
        # Point a private profile at B solely to prove the normal deploy path refuses it.
        bound = json.loads(json.dumps(staged))
        bound["host"].update(address=rehearsal.address, host_id=result["host_id"])
        atomic_json(profile_path, bound)
        try:
            invoke("deploy", "deploy", label="deploy-rehearsal-refused", RELEASE=version,
                   refusal="rehearsal_read_only")
        finally:
            atomic_json(profile_path, source_profile)
        self.remote("rehearsal-no-timers", "test -z \"$(find /etc/systemd/system -maxdepth 1 -name 'foundry-backup-*.timer' -print)\"")
        # Confirm A is still serving the original data after B completed.
        self.active = source
        marker_read(marker)
        if postgres_state() != source_postgres:
            raise Failure("Source A changed timeline or archiving state during rehearsal")
        added = repository_unchanged(before, listing(), archive_id, source_postgres["timeline"])
        self.note(f"PASS recovery/{self.mode}: B archive_mode=off; repository unchanged except {added} A timeline WAL objects")
        self.active = rehearsal
        self.diagnostics()
        self.stop_vm(rehearsal)
        self.note(f"PASS recovery/{self.mode}: B rehearsal, localhost data, registry refusal, unchanged profile; B destroyed, A alive")

        self.note(f"== recovery/{self.mode}: stop-old-server-A ==")
        source_process = source.qemu
        self.stop_vm(source)
        if source_process.poll() is None:
            raise Failure("Old server A is still running; refusing takeover")
        takeover, staged = target("c", "127.0.0.3")
        result = recovery_result(invoke("bootstrap", "recover", label="recover-takeover",
            RECOVER_HOST=takeover.address, RECOVER_MODE="takeover", CONFIRM_OLD_SERVER_GONE="yes",
            FOUNDRY_RECOVERY_DIR=str(recovery_dir), RELEASE=version), "takeover", topology_mode=self.mode)
        self.check_recovered(registry(), result, source_profile, "takeover")
        expected = json.loads(json.dumps(staged))
        expected["host"].update(address=takeover.address, host_id=result["host_id"])
        if json.loads(profile_path.read_text()) != expected:
            raise Failure("Takeover did not update the local profile to C")
        marker_read(marker)
        state = healthy("takeover", version)["state"]
        final_backup = completed_full_backup(state.get("backup", {}).get("database"), "full", mode=self.mode)
        if (final_backup["backup_id"] != result["backup"]["backup_id"]
                or final_backup["backup_id"] in (initial["backup_id"], receipt["backup_id"])
                or state["backup"].get("last_complete_id") != final_backup["backup_id"]):
            raise Failure("Takeover did not record its own completed full/key backup")
        fetch("verify-takeover-key-archive-upload", final_backup, export / "takeover.age")
        for timer in ("foundry-backup-full-cluster.timer", "foundry-backup-diff-cluster.timer"):
            self.remote("timer-" + timer, f"systemctl is-enabled {timer} && systemctl is-active {timer}", publish=True)
        self.note(f"PASS recovery/{self.mode}: C takeover, healthy roles, surviving data, clean status/report, full/key backup, timers and profile")

    @staticmethod
    def check_recovered(observed, result, source_profile, mode):
        expected = "rehearsal" if mode == "rehearsal" else "ready"
        origin = observed.get("recovered_from") or {}
        if (observed.get("state") != expected or observed.get("host_id") != result["host_id"]
                or result["host_id"] == source_profile["host"]["host_id"]
                or origin.get("host_id") != source_profile["host"]["host_id"]
                or origin.get("instance_id") != source_profile["instance_id"]
                or origin.get("operation_id") != result["operation_id"] or origin.get("mode") != mode):
            raise Failure("Replacement registry does not prove the expected recovery identity/state")


def image_pin(ubuntu):
    inputs = json.loads((DEPLOY / "docker/vm-inputs.json").read_text())
    if inputs.get("schema") != 1 or ubuntu not in inputs["ubuntu"]:
        raise Failure("No reviewed VM image pin for Ubuntu " + ubuntu)
    pin = inputs["ubuntu"][ubuntu]
    if (pin["architecture"] != "amd64" or not re.fullmatch(r"[a-f0-9]{64}", pin["sha256"])
            or not re.fullmatch(r"\d{8}", pin["serial"])
            or not pin["url"].startswith("https://cloud-images.ubuntu.com/")
            or "/release-" + pin["serial"] + "/" not in pin["url"]):
        raise Failure("Invalid reviewed VM image pin")
    return pin


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--ubuntu", required=True)
    parser.add_argument("--topology", choices=("feature", "standard", "both"), default="both")
    parser.add_argument("--scenario", choices=("operator", "recovery"), default="operator")
    args = parser.parse_args()
    pin = image_pin(args.ubuntu)
    if sys.platform != "linux" or os.uname().machine != "x86_64" or not os.access("/dev/kvm", os.R_OK | os.W_OK):
        raise Failure("Requires Linux amd64 and readable/writable /dev/kvm; no emulation fallback. Use the GitHub Actions VM job.")
    for program in ("qemu-system-x86_64", "qemu-img", "cloud-localds", "ssh", "ssh-keygen", "curl", "docker", "go", "gofmt", "openssl", "age", "age-keygen", "rclone"):
        if shutil.which(program) is None:
            raise Failure("Missing runner prerequisite: " + program)
    logs = Path(os.environ["ACCEPTANCE_LOG_DIR"]).absolute() if os.environ.get("ACCEPTANCE_LOG_DIR") else Path(tempfile.mkdtemp(prefix="foundry-vm-logs-"))
    if os.environ.get("ACCEPTANCE_LOG_DIR"):
        logs.mkdir(mode=0o700)  # Refuse reuse of another run's artifacts.
    print("Acceptance logs: " + str(logs), flush=True)
    os.umask(0o077)
    with tempfile.TemporaryDirectory(prefix="foundry-vm-") as directory:
        private = Path(directory)
        run = Run(private, logs, args.scenario)
        try:
            run.command("download", ["curl", "--fail", "--location", "--max-time", "600", "--output", private / "base.img", pin["url"]], publish=True)
            with (private / "base.img").open("rb") as image:
                checksum = hashlib.file_digest(image, "sha256").hexdigest()
            if checksum != pin["sha256"]:
                raise Failure("Ubuntu image SHA-256 mismatch; refusing to boot")
            (logs / "image.txt").write_text(json.dumps(pin, indent=2) + "\nverified_sha256=" + checksum + "\n")
            key = private / "operator-key"
            run.command("ssh-key", ["ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", key])
            certs = private / "certs"
            containers.create_ca(certs, addresses=("127.0.0.1", "10.0.2.2"))
            access, secret = "acceptance-" + secrets.token_hex(8), secrets.token_hex(32)
            run.values.extend([access, secret])
            volume = run.resources.reserve("volume", "s3")
            run.command("s3-volume", ["docker", "volume", "create", "--label", run.resources.label, volume])
            s3 = run.resources.reserve("container", "s3")
            run.command("s3-create", ["docker", "create", "--name", s3, "--label", run.resources.label,
                "--user", "0:0", "--publish", "127.0.0.1::9000", "--memory=512m", "--cpus=1",
                "-e", "ROOT_ACCESS_KEY_ID", "-e", "ROOT_SECRET_ACCESS_KEY",
                "--mount", f"type=volume,source={volume},target=/data", containers.S3_IMAGE,
                "--cert", "/certs/public.crt", "--key", "/certs/private.key", "--port", ":9000", "--region", "us-east-1", "posix", "/data"],
                env={**os.environ, "ROOT_ACCESS_KEY_ID": access, "ROOT_SECRET_ACCESS_KEY": secret})
            server_certs = private / "server-certs"
            server_certs.mkdir()
            for name in ("public.crt", "private.key"):
                shutil.copyfile(certs / name, server_certs / name)
            run.command("s3-certs", ["docker", "cp", server_certs, s3 + ":/certs"])
            run.command("s3-start", ["docker", "start", s3])
            _, address = run.command("s3-port", ["docker", "port", s3, "9000/tcp"])
            port = int(address.strip().removeprefix("127.0.0.1:"))
            storage = {"endpoint": f"https://127.0.0.1:{port}", "access_key": access, "secret_key": secret}
            from acceptance_backup import rclone
            deadline = time.monotonic() + 180
            while True:
                try:
                    rclone(storage, "lsd", "acceptance:", "--ca-cert", str(certs / "ca.crt"),
                           "--retries", "1", "--low-level-retries", "1", "--contimeout", "5s", "--timeout", "5s", timeout=10)
                    break
                except RuntimeError:
                    if time.monotonic() >= deadline:
                        raise Failure("S3 readiness deadline exceeded") from None
                    time.sleep(1)
            run.command("age-key", ["age-keygen", "-o", private / "age-identity"])
            _, recipient = run.command("age-recipient", ["age-keygen", "-y", private / "age-identity"])
            candidate = private / ("candidate-" + uuid.uuid4().hex)
            candidate.mkdir()
            run.candidates.append(candidate)
            # Reuse the same packaged source and warm builder across fresh VMs.
            run.command("candidate", [sys.executable, "-B", __file__, "--candidate", candidate], timeout=900)
            snapshot, proxy = candidate / "fixture", candidate / "candidate/proxy"
            if args.scenario == "recovery":
                run.command("format-recovery-fixture", ["gofmt", "-w", *recovery_fixture(snapshot)])
            modes = ("feature", "standard") if args.topology == "both" else (args.topology,)
            for mode in modes:
                started = time.monotonic()
                for kind in ("app", "backup"):
                    storage[kind + "_bucket"] = kind + "-" + secrets.token_hex(8)
                    rclone(storage, "mkdir", "acceptance:" + storage[kind + "_bucket"], "--ca-cert", str(certs / "ca.crt"), "--retries", "1")
                run.boot(private / "base.img", key, mode, name="a" if args.scenario == "recovery" else None)
                run.trust(certs)
                run.operator(snapshot, proxy, key,
                             {**storage, "endpoint": f"https://s3:{port}", "recipient": recipient.strip()},
                             repository=storage, certs=certs)
                run.stop_vm()
                run.note(f"PASS {args.scenario}/{mode}: sequence completed in {(time.monotonic() - started) / 60:.1f} minutes")
        except Failure as error:
            run.note(f"FAIL {run.scenario}/{run.mode}: {error}")
            raise
        finally:
            signal.signal(signal.SIGTERM, signal.SIG_IGN)
            signal.signal(signal.SIGINT, signal.SIG_IGN)
            # Diagnostics cannot replace the failing action's original exit.
            failures = run.cleanup()
            if failures:
                run.note("FAIL cleanup: task-owned resource cleanup failed: " + ", ".join(failures))
                if sys.exc_info()[0] is None:
                    raise Failure("Resource cleanup failed")


def interrupted(number, _frame):
    raise SystemExit(128 + number)


if __name__ == "__main__":
    signal.signal(signal.SIGTERM, interrupted)
    signal.signal(signal.SIGINT, interrupted)
    try:
        if sys.argv[1:2] == ["--cli"]:
            sys.exit(cli_entry(sys.argv[2:]))
        elif sys.argv[1:2] == ["--candidate"]:
            prepare_candidate(load_cli(), Path(sys.argv[2]))
        else:
            main()
    except (Failure, OSError, ValueError, RuntimeError) as error:
        print("VM acceptance stopped: " + str(error), file=sys.stderr)
        sys.exit(1)
