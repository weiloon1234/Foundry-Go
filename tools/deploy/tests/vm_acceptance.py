#!/usr/bin/env python3
"""Real operator CLI acceptance on a checksum-pinned, disposable KVM guest."""
import argparse
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import secrets
import shlex
import shutil
import signal
import socket
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
from disposable import VM_MARKER
from topology import instance_units, parse_profile, parse_topology, validate_topology


class Failure(RuntimeError):
    pass


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
        cli.say(f"Deployment stopped: {error}")
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
        "users": [{"name": "foundry-operator", "shell": "/bin/bash", "lock_passwd": True,
                   "sudo": "ALL=(ALL) NOPASSWD:ALL", "ssh_authorized_keys": [public_key]}],
        "ssh_pwauth": False, "disable_root": True,
        "ssh": {"emit_keys_to_console": False},
        "write_files": [
            {"path": VM_MARKER, "owner": "root:root", "permissions": "0600", "content": "disposable acceptance VM\n"},
            {"path": "/etc/environment", "append": True, "content": "\n" + flags},
            {"path": "/etc/sudoers.d/foundry-acceptance-env", "owner": "root:root", "permissions": "0440",
             "content": 'Defaults:foundry-operator env_keep += "FOUNDRY_DISPOSABLE_HOST FOUNDRY_ACCEPTANCE_SYNTHETIC_CERT"\n'},
            {"path": "/etc/systemd/system.conf.d/foundry-acceptance.conf", "content":
             "[Manager]\nDefaultEnvironment=FOUNDRY_DISPOSABLE_HOST=1\n"},
        ],
        "runcmd": [["systemctl", "daemon-reexec"], ["sh", "-c", 'missing=""\n' + stock]],
    }


def fixture_declarations(snapshot, mode, port, *, project_id=None):
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
        placement = json.loads((snapshot / "deploy/rehearsal/environments/acceptance-run.json").read_text())["placement"]
        profile.update(schema=3, placement=placement, listeners={})
    else:
        topology = json.loads((FIXTURE / "deploy/topology.json").read_text())
    atomic_json(snapshot / "deploy/topology.json", topology)
    topology = validate_topology(topology)
    atomic_json(snapshot / "deploy/environments/acceptance.json", profile)
    return project, profile, topology


class Run:
    def __init__(self, private, logs):
        self.private, self.logs = private, logs
        self.values = []
        self.resources = containers.Resources(private)
        self.qemu = None
        self.candidates = []
        self.project_id = str(uuid.uuid4())
        self.ssh = None
        self.mode = "setup"
        self.serial = None

    def note(self, message):
        print(message, flush=True)
        with (self.logs / "steps.log").open("a") as stream:
            stream.write(message + "\n")

    def command(self, name, argv, *, timeout=600, input=None, env=None, publish=False, check=True):
        self.note(f"== {self.mode}: {name} ==")
        output = self.private / "command-output"
        with output.open("w+") as stream, (self.private / "command-error").open("w+") as errors:
            process = subprocess.Popen([str(x) for x in argv], stdin=subprocess.PIPE,
                                       stdout=stream, stderr=errors, text=True,
                                       env=env, start_new_session=True)
            try:
                process.communicate(input, timeout=timeout)
            except BaseException:
                try:
                    os.killpg(process.pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass
                process.wait()
                self.note(f"FAIL {name}: interrupted/deadline; exit={process.returncode}")
                raise
            stream.seek(0)
            value = stream.read()
            errors.seek(0)
            diagnostics = errors.read()
        self.note(f"{name}: exit={process.returncode}")
        if publish:
            (self.logs / f"{self.mode}-{name}.log").write_text(redact(value + diagnostics, self.values))
        if check and process.returncode:
            # Only CLI designed-safe diagnostics and public boot/install output
            # opt into publishing. Never emit raw argv, private stdin or app logs.
            if publish:
                print(redact((value + diagnostics)[-12000:], self.values), file=sys.stderr)
            raise Failure(f"{name}: exit={process.returncode}")
        return process.returncode, value

    def remote(self, name, command, **kwargs):
        return self.command(name, [*self.ssh, command], **kwargs)

    def stop_vm(self):
        if self.qemu is not None:
            self.qemu.terminate()
            try:
                self.qemu.wait(timeout=15)
            except subprocess.TimeoutExpired:
                self.qemu.kill()
                self.qemu.wait()
            self.qemu = None
        if self.serial is not None:
            value = self.serial.read_text(errors="replace")
            (self.logs / f"{self.mode}-serial.log").write_text(redact(value, self.values))
            self.serial = None
        self.ssh = None

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
        self.diagnostics()
        failures = []
        for name, action in (("QEMU/serial", self.stop_vm), ("builders", self.cleanup_builders),
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
                            "sudo -n runuser -u postgres -- pgbackrest --config=/etc/foundry/pgbackrest/foundry.conf "
                            "--stanza=foundry --log-level-console=info info; "
                            "port=$(sudo -n sed -n 's/^repo1-storage-port=//p' /etc/foundry/pgbackrest/foundry.conf); "
                            "curl -sS -o /dev/null -w 'curl %{http_code} %{ssl_verify_result}\\n' \"https://10.0.2.2:$port/\"",
                            timeout=90, publish=True, check=False)
            except Exception:
                self.note("Systemd diagnostic collector unavailable")

    def boot(self, image, key, mode):
        self.mode = mode
        guest = self.private / mode
        guest.mkdir(mode=0o700)
        port = free_port()
        (guest / "user-data").write_text("#cloud-config\n" + json.dumps(cloud_config(key.with_suffix(".pub").read_text().strip())))
        (guest / "meta-data").write_text(json.dumps({"instance-id": "foundry-" + uuid.uuid4().hex,
                                                   "local-hostname": "foundry-acceptance"}))
        self.command("seed", ["cloud-localds", guest / "seed.img", guest / "user-data", guest / "meta-data"])
        self.command("overlay", ["qemu-img", "create", "-f", "qcow2", "-F", "qcow2", "-b", image, guest / "disk.qcow2", "32G"])
        self.serial = guest / "serial.log"
        config = guest / "ssh_config"
        config.write_text(f"Host *\n  UserKnownHostsFile {guest / 'known_hosts'}\n  GlobalKnownHostsFile /dev/null\n")
        self.ssh = [shutil.which("ssh"), "-F", str(config), "-o", "StrictHostKeyChecking=accept-new",
                    "-o", "BatchMode=yes", "-o", "IdentitiesOnly=yes", "-o", "ConnectTimeout=5",
                    "-i", str(key), "-p", str(port), "foundry-operator@127.0.0.1"]
        self.launch(guest, port)
        self.remote("cloud-init", "sudo -n cloud-init status --wait --long", timeout=900, publish=True)
        # Prepare the host as the deployment guide tells operators: current packages, then reboot if required.
        self.remote("host-upgrade", "sudo -n env DEBIAN_FRONTEND=noninteractive sh -c "
                    "'apt-get -o DPkg::Lock::Timeout=600 update && apt-get -o DPkg::Lock::Timeout=600 -y upgrade'",
                    timeout=1500, publish=True)
        if self.remote("reboot-required", "test -e /var/run/reboot-required && echo yes || echo no",
                       publish=True)[1].strip() == "yes":
            subprocess.run([*self.ssh, "sudo -n systemctl reboot"], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=30)
            self.qemu.wait(timeout=300)  # -no-reboot turns the guest reboot into a QEMU exit.
            self.launch(guest, port)
            self.remote("settle", "systemctl is-system-running --wait || true", timeout=600, publish=True)
        self.remote("stock-packages", "sudo -n grep '^STOCK ' /var/log/cloud-init-output.log; "
                    "dpkg-query -W fwupd packagekit ubuntu-pro-client; "
                    "systemctl list-timers --all --no-pager", publish=True)
        return guest, port, config

    def launch(self, guest, port):
        with self.serial.open("a") as console:
            self.qemu = subprocess.Popen(["qemu-system-x86_64", "-enable-kvm", "-cpu", "host", "-smp", "2", "-m", "4096",
                "-display", "none", "-monitor", "none", "-serial", "stdio", "-no-reboot",
                "-drive", f"file={guest / 'disk.qcow2'},if=virtio,format=qcow2",
                "-drive", f"file={guest / 'seed.img'},if=virtio,format=raw,readonly=on",
                "-netdev", f"user,id=net0,hostfwd=tcp:127.0.0.1:{port}-:22", "-device", "virtio-net-pci,netdev=net0"],
                stdin=subprocess.DEVNULL, stdout=console, stderr=console)
        self.note("Waiting up to 300s for guest SSH (readiness only)")
        deadline = time.monotonic() + 300
        while time.monotonic() < deadline:
            if self.qemu.poll() is not None:
                raise Failure(f"QEMU exited before SSH: exit={self.qemu.returncode}")
            result = subprocess.run([*self.ssh, "true"], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=10)
            if result.returncode == 0:
                return
            time.sleep(2)
        raise Failure("Guest SSH readiness deadline exceeded")

    def trust(self, certs):
        # Stage via private SSH stdin; no CA keys in cloud-init or serial output.
        self.remote("ca-directory", "sudo -n install -d -m 0700 /etc/foundry/acceptance-ca")
        for name in ("ca.crt", "ca.key"):
            self.remote("stage-" + name, "sudo -n sh -c " + shlex.quote(
                "umask 077; cat > /etc/foundry/acceptance-ca/" + name), input=(certs / name).read_text())
        self.remote("trust-ca", "sudo -n install -m 0644 /etc/foundry/acceptance-ca/ca.crt "
                    "/usr/local/share/ca-certificates/foundry-acceptance.crt && sudo -n update-ca-certificates")

    def operator(self, snapshot, proxy, guest, port, config, key, storage):
        cli = load_cli()
        project, profile, topology = fixture_declarations(snapshot, self.mode, port, project_id=self.project_id)
        (snapshot.parent / "vm-project-id").write_text(project["id"])
        settings = {"schema": 1, "hosts": {f"127.0.0.1:{port}": {
            "target": "foundry-operator@127.0.0.1", "key_path": str(key)}}}
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

        def invoke(command, action, *, label=None, **extra):
            # Entry point and normal process exit are exercised, including CLI
            # validation, plans, upload and every real SSH/sudo request.
            _, output = self.command(label or action,
                [sys.executable, "-B", __file__, "--cli", snapshot, proxy, command],
                env={**env, "ACTION": action, **extra}, timeout=1800,
                publish=action not in ("logs", "build"))
            return output

        invoke("bootstrap", "inspect")
        invoke("bootstrap", "prepare")
        profile = cli.read_json(snapshot / "deploy/environments/acceptance.json")
        units = instance_units({"name": instance_name(project, profile), "profile": profile, "topology": topology})
        previous = None
        for number in (1, 2):
            invoke("release", "build", label=f"build-{number}")
            releases = snapshot / "dist/releases" / project["id"] / profile["instance_id"]
            manifests = sorted(releases.glob("*/manifest.json"), key=lambda p: p.parent.name)
            manifest = json.loads(manifests[-1].read_text())
            version = manifest["version"]
            if version == previous:
                raise Failure("Second build did not produce a distinct release")
            invoke("deploy", "deploy", label=f"deploy-{number}", RELEASE=version)
            status = json.loads(invoke("deploy", "status", label=f"status-{number}"))
            if status["state"]["status"] != "healthy" or status["state"]["current"]["version"] != version:
                raise Failure("Deployed release is not healthy/current")
            for unit in units.values():
                _, output = self.remote("role-" + unit, "systemctl show " + shlex.quote(unit)
                    + " -p ActiveState -p SubState -p MainPID", publish=True)
                state = dict(line.split("=", 1) for line in output.splitlines() if "=" in line)
                if state.get("ActiveState") != "active" or state.get("SubState") != "running" or int(state.get("MainPID", "0")) <= 0:
                    raise Failure("Placed role is not running: " + unit)
            invoke("deploy", "logs", label=f"logs-{number}", LINES="10")
            invoke("deploy", "report", label=f"report-{number}")
            self.diagnostics()
            previous = version
        plan = json.loads(invoke("deploy", "tools-plan"))
        if plan["upgrade"] is not None or plan["prune_bundles"] or plan["prune_upgrade_journals"]:
            invoke("deploy", "tools-apply", PLAN_TOKEN=plan["plan_token"], OPERATION=str(uuid.uuid4()))
        self.note("PASS real CLI bootstrap, two rebuilt deployments, healthy roles, logs, report and tools plan")


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
    args = parser.parse_args()
    pin = image_pin(args.ubuntu)
    if sys.platform != "linux" or os.uname().machine != "x86_64" or not os.access("/dev/kvm", os.R_OK | os.W_OK):
        raise Failure("Requires Linux amd64 and readable/writable /dev/kvm; no emulation fallback. Use the GitHub Actions VM job.")
    for program in ("qemu-system-x86_64", "qemu-img", "cloud-localds", "ssh", "ssh-keygen", "curl", "docker", "go", "openssl", "age-keygen", "rclone"):
        if shutil.which(program) is None:
            raise Failure("Missing runner prerequisite: " + program)
    logs = Path(os.environ["ACCEPTANCE_LOG_DIR"]).absolute() if os.environ.get("ACCEPTANCE_LOG_DIR") else Path(tempfile.mkdtemp(prefix="foundry-vm-logs-"))
    if os.environ.get("ACCEPTANCE_LOG_DIR"):
        logs.mkdir(mode=0o700)  # Refuse reuse of another run's artifacts.
    print("Acceptance logs: " + str(logs), flush=True)
    os.umask(0o077)
    with tempfile.TemporaryDirectory(prefix="foundry-vm-") as directory:
        private = Path(directory)
        run = Run(private, logs)
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
            modes = ("feature", "standard") if args.topology == "both" else (args.topology,)
            for mode in modes:
                for kind in ("app", "backup"):
                    storage[kind + "_bucket"] = kind + "-" + secrets.token_hex(8)
                    rclone(storage, "mkdir", "acceptance:" + storage[kind + "_bucket"], "--ca-cert", str(certs / "ca.crt"), "--retries", "1")
                guest, ssh_port, config = run.boot(private / "base.img", key, mode)
                run.trust(certs)
                run.operator(snapshot, proxy, guest, ssh_port, config, key,
                             {**storage, "endpoint": f"https://10.0.2.2:{port}", "recipient": recipient.strip()})
                run.stop_vm()
                shutil.rmtree(guest)
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
