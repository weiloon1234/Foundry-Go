"""Real plan/apply/lifecycle/supply/network owners with only machine I/O simulated.

The fake never executes commands. apt authentication, database/certificate providers
and HTTP responses are fixtures; all public lane B/C calls retain their signatures.
"""
from dataclasses import asdict
import hashlib
import ipaddress
import io
import json
import os
from pathlib import Path
import subprocess
import shutil
import shlex
import importlib.util
from types import SimpleNamespace
import sys
import tarfile
import tempfile
import time
import unittest
from unittest.mock import Mock, patch
import uuid

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "lib"))
import admission
from listeners import observe_dns as actual_observe_dns
import artifacts
import certificates
import contracts as c
import edge
import feature_lifecycle as lifecycle
import feature_network
import feature_runtime
import firewall
from host import Host, Paths
import listeners
import operations
import packages
from packages import _activation_snapshot as actual_activation_snapshot
import provision
import release
import report
import services
from services import probe as actual_probe
import upgrades
from topology import parse_profile, parse_topology
from test_artifacts import elf
from test_contracts import fixture
from test_manage import manager
from test_packages import installed, simulation
from test_redis_acl import RedisServiceHost
import redis_acl

TOOLS = Path(__file__).resolve().parents[1]
BOOT = "00000000-0000-4000-8000-000000000001"
DEFAULT_UNITS = {"nginx.service": ["nginx"], "redis-server.service": ["redis-server"],
                 "redis-server@.service": ["redis-server"], "postgresql.service": ["postgresql-common"],
                 "postgresql@.service": ["postgresql-common"], "certbot.service": ["certbot"], "certbot.timer": ["certbot"]}


class WiringHost(Host):
    def __init__(self, root):
        super().__init__(Paths(root))
        self.commands, self.started = [], []
        self.account, self.nginx = None, False
        self.account_sequence = 0
        self.worker = 90
        self.sequence = 100
        self.running = {}
        self.enabled = set()
        self.failed = set()
        self.public_profile = None
        self.firewall_live = {}
        self.starting = None
        self.package_inventory = {name: installed() for name in (*packages.REQUIREMENTS, "postgresql-16",
                                  "postgresql-client-16", "postgresql-common", "jq") if name not in {"nginx", "redis-server", "postgresql-common", "certbot"}}
        self.candidates = {name: "1" for name in ("nginx", "nginx-common", "libnginx-mod-stream", "redis-server", "postgresql-common", "certbot")}
        self.redis_host = RedisServiceHost(root)
        self.owned_write(self.paths.p("/etc/os-release"), 'ID=ubuntu\nVERSION_ID="24.04"\n')
        self.owned_write(self.paths.p("/proc/sys/kernel/random/boot_id"), BOOT)
        self.owned_write(self.paths.p("/usr/bin/jq"), elf("linux-arm64"), 0o755)

    def proc(self, pid, invocation, executable=None, unit=None, alive=True):
        root = self.paths.p(f"/proc/{pid}")
        self.owned_write(root / "stat", f"{pid} (fixture) " + " ".join(["S" if alive else "Z"] + ["0"] * 18 + ["42"]))
        self.owned_write(root / "environ", b"INVOCATION_ID=" + invocation.encode() + b"\0")
        if executable:
            (root / "exe").unlink(missing_ok=True)
            (root / "exe").symlink_to(executable)
            self.owned_write(root / "cgroup", "0::/system.slice/" + unit + "\n")
            self.owned_write(self.paths.p("/sys/fs/cgroup/system.slice/" + unit + "/cgroup.procs"), str(pid))
        return c.ProcessIdentity(pid, 42, invocation)

    def start(self, unit):
        registry = self.read_registry()
        entry = next(iter(registry["instances"].values()))
        _, selection, selected, _ = lifecycle.load(self, entry)
        role = next(role for role, name in selected.units.items() if name == unit)
        text = self.paths.p("/etc/systemd/system/" + unit).read_text()
        arguments = shlex.split(next(line.split("=", 1)[1].lstrip("+") for line in text.splitlines() if line.startswith("ExecCondition=")))
        phase, instance, declared_role, ordinal, resolution, bundle, spec = arguments[-7:]
        generation = c.read_json(admission.AdmissionStore(self, instance).durable / "generations" / (spec + "-" + bundle + ".json"))
        assert generation["resolution_id"] == resolution and generation["unit"] == {"instance_id": instance, "role": role, "ordinal": 1}
        inventory, launch = lifecycle.load_resolution(self, entry, resolution)
        self.sequence += 2
        invocation = format(self.sequence, "032x")
        process = self.proc(self.sequence, invocation)
        pin = c.HelperInvocation(admission.boot_id(self), inventory.specs[role].identity, process, bundle,
                                c.canonical_digest(c.record_document(inventory.specs[role])), inventory.resolution.id, 1, None)
        self.owned_write(self.paths.p("/proc/self/cgroup"), "0::/system.slice/" + unit)
        for action in ("admit", "prepare"):
            feature_runtime.run(action, c.record_document(pin), launch)
        self.proc(process.pid, invocation, alive=False)
        main = self.proc(process.pid + 1, invocation, self.paths.p(inventory.specs[role].executable), unit)
        self.running[unit] = (main, inventory, role)
        self.starting = unit
        feature_runtime.run("running", c.record_document(pin), launch)
        self.starting = None
        self.started.append(role)

    def nginx_config(self):
        paths = [self.paths.p("/etc/nginx/nginx.conf"), self.paths.p(edge.STREAM_INCLUDE)]
        paths += list(self.paths.p("/etc/nginx/sites-available").glob("*"))
        paths += list(self.paths.p(edge.RUNTIME).glob("*/*.conf"))
        result = []
        for path in paths:
            if not path.is_file():
                continue
            name = "/" + path.relative_to(self.paths.root).as_posix()
            if "/sites-available/" in name:
                name = str(path)
            content = path.read_text().replace("/etc/nginx/sites-enabled/*", str(self.paths.p("/etc/nginx/sites-available/*")))
            result.append("# configuration file " + name + ":\n" + content)
        return "\n".join(result)

    def run(self, args, **kwargs):
        args = list(map(str, args))
        self.commands.append(args)
        output, code = "", 0
        if self.public_profile and args[0] in ("ufw", "iptables", "ip6tables", "iptables-save", "ip6tables-save", "iptables-restore", "ip6tables-restore", "nft", "ip"):
            from network_firewall_fixture import live_rules
            family = 6 if args[0].startswith("ip6") else 4
            if args == ["ufw", "status"]:
                output = "Status: active\n"
            elif args == ["ufw", "reload"]:
                self.firewall_live = {f: live_rules(self.public_profile.instance_id, f) for f in (4, 6)}
            elif args[-1] == "--version":
                output = "iptables v1.8.10 (legacy)\n"
            elif args[0].endswith("-save"):
                output = self.firewall_live[family]
            elif args[0].endswith("-restore"):
                rules = [tuple(line.split()[2:]) for line in kwargs["input"].splitlines() if line.startswith("-A ")]
                self.firewall_live[family] = live_rules(self.public_profile.instance_id, family, rules)
            elif args[0] == "nft":
                output = '{"nftables": []}'
            elif args[:4] == ["ip", "-j", "address", "show"]:
                output = json.dumps([{"addr_info": [{"local": a} for a in self.public_profile.listeners[0][1].public_addresses]}])
            else:
                output = "[]"
            return subprocess.CompletedProcess(args, 0, output, "")
        if (args[:3] == ["systemctl", "show", redis_acl.REDIS_UNIT]
                or args == ["systemctl", "enable", "--now", redis_acl.REDIS_UNIT]
                or args[0] in ("/usr/bin/redis-server", "redis-cli", "install")
                or args[0] in ("getent", "useradd") and args[-1] == "foundry-redis"
                or args[:4] == ["ss", "-H", "-ltn", "sport"]):
            return self.redis_host.run(args, **kwargs)
        if args[0] == "dpkg-query":
            if args[1] == "-S":
                output = "jq: " + args[2]
            elif "-f=${Conffiles}" in args:
                output = " /etc/nginx/nginx.conf " + hashlib.md5(self.paths.p("/etc/nginx/nginx.conf").read_bytes()).hexdigest()
            elif "-f=${Status}" in args:
                code = 0 if args[-1] in self.package_inventory else 1
                output = "install ok installed" if code == 0 else ""
            else:
                output = "\n".join(f"{name}\t{value['status']}\t{value['version']}" for name, value in self.package_inventory.items())
        elif args == ["dpkg", "--print-architecture"]:
            output = "arm64"
        elif args[:2] == ["apt-get", "-s"]:
            output = simulation({n: v for n, v in self.candidates.items() if n not in self.package_inventory})
        elif args[0] == "apt-get":
            self.package_inventory.update({n: installed(v) for n, v in self.candidates.items()})
            self.owned_write(self.paths.p("/etc/nginx/nginx.conf"), "include /etc/nginx/modules-enabled/*.conf;\nhttp { include /etc/nginx/sites-enabled/*; }\n")
            self.owned_write(self.paths.p("/usr/sbin/nginx"), b"fixture", 0o755)
            self.owned_write(self.paths.p("/usr/lib/systemd/system/redis-server@.service"), "# distro template\n")
        elif args[:2] == ["getent", "passwd"]:
            output, code = (self.account, 0) if self.account else ("", 2)
        elif args[0] == "useradd":
            name = args[-1]
            comment = args[args.index("--comment") + 1]
            self.account = f"{name}:x:{os.getuid() + self.account_sequence}:{os.getgid() + self.account_sequence}:{comment}:{c.instance_paths(name)['data']}:/usr/sbin/nologin"
            self.account_sequence += 1
        elif args[0] == "nginx":
            if "-T" in args:
                output = self.nginx_config()
        elif args[0] == "systemd-run":
            operation = args[-2]
            owner = c.read_json(operations.directory(self, operation) / "owner.json")
            pin = {**owner, "boot_id": BOOT, "invocation_id": uuid.uuid4().hex}
            self.operation_pin = pin
            self.proc(os.getpid(), pin["invocation_id"])
            with patch.dict(os.environ, INVOCATION_ID=pin["invocation_id"]):
                code = operations.run("operation", pin)
                cleanup = operations.run("operation-cleanup", pin)
            code = code or cleanup
        elif args[0] == "systemctl":
            action = args[1]
            unit = next((arg for arg in args[2:] if not arg.startswith("-")), "")
            loaded = (unit in self.running or unit in ("nginx", "nginx.service") and self.nginx
                      or self.paths.p("/etc/systemd/system/" + unit).is_file())
            if action == "is-active":
                active = self.nginx if unit in ("nginx", "nginx.service") else unit in self.running
                output, code = ("active", 0) if active else ("failed" if unit in self.failed else "inactive", 3 if loaded else 4)
            elif action == "is-failed":
                output, code = ("failed", 0) if unit in self.failed else ("active" if unit in self.running else "inactive", 1 if loaded else 4)
            elif action == "enable" and "--now" in args and args[-1] == "nginx":
                assert not self.paths.p("/etc/systemd/system/nginx.service").is_symlink()
                self.nginx = True
            elif action == "enable":
                self.enabled.update(args[2:])
            elif action == "disable":
                self.enabled.difference_update(args[2:])
            elif action == "is-enabled":
                output, code = ("enabled", 0) if unit in self.enabled else (("disabled", 1) if loaded else ("not-found", 4))
            elif action == "reload":
                self.worker += 1
            elif action == "start" and unit.startswith("foundry-"):
                self.start(unit)
            elif action == "stop":
                code = 0 if loaded else 5
                record = self.running.pop(unit, None)
                if record:
                    self.proc(record[0].pid, record[0].invocation_id, alive=False)
                    self.owned_write(self.paths.p("/sys/fs/cgroup/system.slice/" + unit + "/cgroup.procs"), "")
            elif action == "show":
                if unit.startswith("foundry-operation-"):
                    pin = self.operation_pin
                    values = {"Type": "exec", "KillMode": "control-group", "Restart": "no", "Transient": "yes", "DropInPaths": ""}
                    for directive, phase in (("ExecStart", "operation"), ("ExecStopPost", "operation-cleanup")):
                        values[directive] = "{ argv[]=" + " ".join(operations.command(phase, pin["operation_id"], pin["bundle_sha256"])) + " ; }"
                else:
                    current = self.running.get(unit)
                    values = {"Id": unit, "LoadState": "loaded" if loaded else "not-found", "ActiveState": "active" if current else "inactive",
                              "SubState": "running" if current else "dead", "MainPID": str(current[0].pid) if current else "0",
                              "ControlPID": "0", "Job": "0", "ControlGroup": "/system.slice/" + unit if current else "",
                              "NRestarts": "0", "InvocationID": current[0].invocation_id if current else ""}
                    if unit == self.starting:
                        values.update(ActiveState="activating", SubState="start-post")
                output = "\n".join(k + "=" + v for k, v in values.items())
            elif action not in ("disable", "daemon-reload", "enable", "reset-failed"):
                raise AssertionError(args)
        elif args[0] == "ss":
            rows = []
            if "-u" not in args:
                family = 6 if "-6" in args else 4
                if self.public_profile:
                    for claim in listeners.public_claims(self.public_profile):
                        path = self.paths.p(edge.RUNTIME + "/" + claim.instance_id + "/stream-" + claim.role + "." + claim.listener + ".conf")
                        if path.exists() and claim.socket.family == family:
                            rows.append(f'LISTEN 0 128 {claim.socket.address}:{claim.socket.port} 0.0.0.0:* users:(("nginx",pid={self.worker},fd=3))')
                for process, inventory, role in self.running.values():
                    for endpoint in inventory.resolution.endpoints:
                        if endpoint.role == role and ipaddress.ip_address(endpoint.address).version == family:
                            rows.append(f'LISTEN 0 128 {endpoint.address}:{endpoint.port} 0.0.0.0:* users:(("app",pid={process.pid},fd=3))')
            output = "\n".join(rows)
        elif args[0] == "runuser":
            if "redis:keys" in args:
                output = "[]"
            elif "config:inspect" in args:
                output = json.dumps([{"Name": "app.test.token", "Source": "environment", "Secret": True}])
            elif "maintenance:status" in args:
                output = json.dumps({"down": getattr(self, "maintenance_down", False)})
            elif "down" in args or "up" in args:
                self.maintenance_down = "down" in args
                output = json.dumps({"down": self.maintenance_down})
            elif "migrate:status" in args:
                output = '{"targets":[{"problems":[],"migrations":[]}]}'
        elif args[0] == "journalctl":
            output = "fixture service log"
        elif len(args) > 3 and args[3] == "prune":
            output = json.dumps(upgrades._prune_bundles(self))
        elif args[0] not in ("chown", "chmod"):
            raise AssertionError(args)
        result = subprocess.CompletedProcess(args, code, output, "")
        return self.check_result(result, check=kwargs.get("check", True))


class FeatureWiringFixture(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.enterContext(patch.object(listeners, "observe_dns", side_effect=lambda binding: c.DNSObservation(binding.hostname, tuple(binding.public_addresses), c.now(), "recursive")))
        self.host = WiringHost(Path(temporary.name))
        self.project, self.profile, _ = fixture()
        self.archive = self.host.paths.root / "feature.tar.gz"
        self.build_archive()
        self.envelope = upgrades.protocol_metadata(TOOLS)
        # External providers and kernel-only observations, not orchestration seams.
        for module, name, options in (
            (packages, "_locks", {"return_value": []}),
            (packages, "_candidate_evidence", {"side_effect": lambda h, txn: {n: {"version": v, "sha256": "a" * 64} for n, v in txn.items()}}),
            (packages, "_prepare_payloads", {"return_value": (DEFAULT_UNITS, {})}),
            (packages, "_activation_snapshot", {"return_value": {}}),
            (provision.postgres, "inspect", {"return_value": {"state": "ready"}}),
            (certificates, "inspect", {"return_value": {"state": "planned"}}),
            (provision.postgres, "prepare", {"side_effect": self.prepare_postgres}),
            (provision.postgres, "prepare_tls", {"return_value": {"ca": self.archive, "cert": self.host.paths.p("/etc/os-release"), "key": self.host.paths.p("/etc/os-release")}}),
            (provision.postgres, "record_trust_probe", {}),
            (redis_acl, "_verify_runtime", {}),
            (provision.postgres, "provision", {"side_effect": lambda h, r, e, credentials: provision._step(h, r, e, "database")}),
            (provision.redis_acl, "provision", {"side_effect": lambda h, r, e, credentials: provision._step(h, r, e, "redis")}),
            (provision.backup, "configure", {"side_effect": lambda h, r, e: provision._step(h, r, e, "backup")}),
            (certificates, "provision", {"side_effect": lambda h, r, e: provision._step(h, r, e, "certificate")}),
            (release, "reconcile_redis_grants", {}),
            (edge, "_worker_identities", {"side_effect": lambda h: ((h.worker, 42),)}),
            (listeners, "process_members", {"side_effect": lambda h, u: frozenset({h.worker}) if u == "nginx.service" else frozenset({h.running[u][0].pid})}),
            (services, "probe", {"side_effect": self.probe}),
            (operations, "Host", {"return_value": self.host}),
            (feature_runtime, "Host", {"return_value": self.host}),
        ):
            self.enterContext(patch.object(module, name, autospec=True, **options))
        self.enterContext(patch.object(release, "STABILITY_SECONDS", 0))
        runtime_os = Mock(wraps=os)
        runtime_os.geteuid.return_value = 0
        self.enterContext(patch.object(feature_runtime, "os", runtime_os))

    def probe(self, role, resolution, deadline, *, paused=False):
        down = getattr(self.host, "maintenance_down", False)
        response = Mock()
        response.status = 503 if down and role.common.name == "http" else 200
        response.read.return_value = (b'{"outcome":"rejected"}' if response.status == 503 else
                                      b'{"status":"ok"}' if paused else b'{"ready":true}')
        connection = Mock()
        connection.getresponse.return_value = response
        with patch.object(services.http.client, "HTTPConnection", return_value=connection):
            return actual_probe(role, resolution, deadline, paused=paused)

    def prepare_postgres(self, host, registry, **kwargs):
        registry["shared"]["postgresql"] = {"unit": "postgresql@16-foundry.service", "port": 55432}

    def build_archive(self, *, public=False, public_binding=False, secrets=False, templates=False, independent=False, worker=False, scheduler=False, endpoint_binding=False, rehearsal_safe=False, maintenance=None, version="20261114T000000Z-abcdef12"):
        binary = elf("linux-arm64")
        checksum = hashlib.sha256(binary).hexdigest()
        self.topology = parse_topology({"schema": 2, "roles": [
            {"name": "http", "kind": "builtin", "requires": [] if independent else ["relay"], "packages": ["jq"],
             "bindings": {**({} if independent and not endpoint_binding else {"relay": {"endpoint": {"role": "relay", "listener": "api", "scheme": "http"}}}),
                          "checksum": {"executable": {"package": "jq", "path": "/usr/bin/jq"}},
                          **({"relay_public": {"public_listener": {"role": "relay", "listener": "api"}}} if public_binding else {})}},
            {"name": "relay", "kind": "executable", "executable": {"artifact": "relay", "entry": "server"},
             "arguments": [{"listener_address": "api"}, {"listener_port": "api"}],
             **({"secret_files": {"TOKEN_FILE": "relay-token"}} if secrets else {}),
             **({"configs": {"private": {"source": "deploy/files/private.yaml", "mode": "rendered-template",
                    "format": "yaml", "path": "relay.yaml", "directory_environment": "XDG_CONFIG_HOME"}}} if templates else {}),
             **({"recovery_rehearsal_safe": True} if rehearsal_safe else {}),
             "listeners": {"api": ({"transport": "tcp", "bind": "public", "tls": "none", "authentication": True}
                                    if public else {"transport": "tcp", "bind": "loopback"})},
             "readiness": {"kind": "http", "listener": "api", "path": "/ready", "status": 200},
             "restart": {"policy": "on-failure", "delay_seconds": 5, "burst": 5},
             "limits": {"memory_bytes": 268435456, "cpu_percent": 100, "tasks": 64},
             "maintenance": maintenance or ("stop" if independent else "serve"), "state": {"kind": "disposable"}},
            *([{"name": "worker", "kind": "builtin"}] if worker else []),
            *([{"name": "scheduler", "kind": "builtin"}] if scheduler else [])],
            **({"secrets": {"relay-token": {"prompt": "Relay token"}}} if secrets else {}),
            "artifacts": {"relay": {"version": "1", "format": "file", "executables": {"server": "relay"},
                "platforms": {"linux-arm64": {"url": "https://example.invalid/relay", "sha256": checksum}}}}})
        self.profile.update(schema=3, placement={"http": 1, "relay": 1, **({"worker": 1} if worker else {}),
                                              **({"scheduler": 1} if scheduler else {})}, listeners={})
        if public:
            from test_host_capabilities import profile
            self.profile["listeners"] = {"relay.api": profile()["listeners"]["relay.ingress"]}
        self.profile = parse_profile(self.profile, self.topology).projection()
        payload = self.host.paths.root / "relay.bin"
        payload.write_bytes(binary)
        members = artifacts.inspect_payload(dict(self.topology.artifacts)["relay"], "linux-arm64", payload, artifacts.ExpansionBudget())
        path = "deploy/artifacts/relay.bin"
        self.version = version
        files = {"artisan": binary, "config/deploy.toml": b'[project]\n[app.worker]\nconnection = "target-queue"\n', "public/index.html": b"fixture",
                 "private-source-maps/" + self.version + "/a.map": b"{}", path: binary}
        if templates:
            files["deploy/files/private.yaml"] = b"token: {{secret.TOKEN_FILE}}\n"
        kinds = artifacts.declared_release_files(self.topology, (), (path,))
        declaration = c.ReleaseDeclaration.parse({"schema": 2, "topology": self.topology.projection(), "profile": self.profile,
            "topology_sha256": self.topology.sha256, "application_executables": {},
            "inventory": [{"path": n, "sha256": hashlib.sha256(v).hexdigest(), "size": len(v),
                           "kind": artifacts.release_member_kind(n, kinds)} for n, v in sorted(files.items())],
            "artifacts": [{"artifact": "relay", "platform": "linux-arm64", "path": path, "sha256": checksum,
                           "members": [asdict(m) for m in members]}]})
        manifest = {"schema": 2, "layout_version": 2, "project_id": self.project["id"], "instance_id": self.profile["instance_id"],
            "instance": "abc-production", "environment": "production", "architecture": "arm64", "native_imaging": False,
            "version": self.version, "commit": "abcdef12", "tree_state": "clean", "built_at": c.now(),
            "source_kind": "git_archive", "source_sha256": "a" * 64, "deployment": declaration.projection(),
            "topology_sha256": self.topology.sha256, "files": {n: hashlib.sha256(v).hexdigest() for n, v in files.items()}}
        files["manifest.json"] = json.dumps(manifest).encode()
        with tarfile.open(self.archive, "w:gz") as stream:
            for name, data in files.items():
                item = tarfile.TarInfo(name)
                item.size = len(data)
                stream.addfile(item, io.BytesIO(data))

    def request(self, action, **kwargs):
        if action == "activate" and "plan_token" not in kwargs:
            kwargs.setdefault("operation_id", str(uuid.uuid4()))
            reviewed = self.request("release-plan", **kwargs)
            self.assertTrue(reviewed["ok"])
            kwargs["plan_token"] = reviewed["plan_token"]
        return manager.execute(self.host, action, {**self.envelope, "project": self.project, "profile": self.profile, **kwargs})


class FeatureWiringTests(FeatureWiringFixture):
    def test_systemctl_status_codes_and_check_semantics(self):
        self.host.owned_write(self.host.paths.p("/etc/systemd/system/foundry-absent.service"), "[Service]\nExecStart=/bin/true\n")
        for action, output, code in (("is-enabled", "disabled", 1), ("is-active", "inactive", 3),
                                     ("is-failed", "inactive", 1)):
            args = ["systemctl", action, "foundry-absent.service"]
            result = self.host.run(args, check=False)
            self.assertEqual((result.stdout, result.returncode), (output, code))
            with self.assertRaises(c.DeployError):
                self.host.run(args)
        self.host.failed.add("foundry-absent.service")
        self.assertEqual(self.host.run(["systemctl", "is-failed", "foundry-absent.service"]).stdout, "failed")
        for action in ("is-enabled", "is-active", "is-failed"):
            self.assertEqual(self.host.run(["systemctl", action, "missing.service"], check=False).returncode, 4)

    def test_initial_validation_failure_cleans_up_and_allows_fresh_review(self):
        self.build_archive(public=True)
        self.configure_firewall()
        bootstrap = self.request("bootstrap-plan", topology=self.topology.projection())
        self.request("bootstrap-apply", topology=self.topology.projection(),
                     plan_token=bootstrap["plan_token"], confirm=True)
        operation = str(uuid.uuid4())
        run = self.host.run
        failed = False
        def fail_observation(args, **kwargs):
            nonlocal failed
            if not failed and args[:2] == ["systemctl", "show"] and "--property=Id,LoadState,ActiveState,MainPID,ControlPID,ControlGroup,Job" in args:
                failed = True
                return self.host.check_result(subprocess.CompletedProcess(args, 1, "secret-output", "secret-error"),
                                              check=kwargs.get("check", True))
            return run(args, **kwargs)
        with patch.object(self.host, "run", side_effect=fail_observation), self.assertRaises(c.DeployError):
            self.request("activate", archive=str(self.archive), archive_sha256=c.digest(self.archive), operation_id=operation)
        self.assertTrue(failed)
        self.assertEqual(self.host.started, [])
        root = operations.directory(self.host, operation)
        commands = c.read_json(root / "operation-commands.json")
        self.assertEqual(len(commands), 1)
        self.assertEqual(commands[0]["command"], "systemctl")
        self.assertEqual(commands[0]["subcommand"], "show")
        self.assertEqual(commands[0]["exit_status"], 1)
        self.assertEqual(len(commands[0]["units"]), 1)
        self.assertNotIn("secret-", json.dumps(commands))
        self.assertEqual((root / "operation-commands.json").stat().st_mode & 0o777, 0o600)
        self.assertTrue((root / "cleanup-closure.json").exists())
        _, entry = self.host.require_identity(self.project, self.profile)
        store = admission.AdmissionStore(self.host, self.profile["instance_id"])
        store.require_cleanup_clear()
        self.assertEqual(self.host.load_state(entry)["last_attempt"]["outcome"], "failed")
        self.assertEqual(c.read_json(store.durable / "cleanup" / (operation + ".json"))["status"], "complete")
        self.assertTrue(self.request("activate", archive=str(self.archive), archive_sha256=c.digest(self.archive))["ok"])

    def executor_review(self, concurrent=False, preexisting=False):
        self.build_archive(public=True)
        self.configure_firewall()
        bootstrap = self.request("bootstrap-plan", topology=self.topology.projection())
        self.request("bootstrap-apply", topology=self.topology.projection(),
                     plan_token=bootstrap["plan_token"], confirm=True)
        operation = str(uuid.uuid4())
        arguments = {"archive": str(self.archive), "archive_sha256": c.digest(self.archive),
                     "operation_id": operation}
        # Exercise the real package snapshot adapter and both review calls. The
        # kernel fake exposes the transient service only once systemd-run owns it.
        machine_run = self.host.run
        def run(args, **kwargs):
            pin = getattr(self.host, "operation_pin", None)
            rows = {}
            if pin is not None or preexisting:
                record = {"LoadState": "loaded", "ActiveState": "active" if pin else "inactive",
                          "SubState": "running" if pin else "dead", "MainPID": "321" if pin else "0",
                          "UnitFileState": "transient", "FragmentPath": "private-canary"}
                rows["foundry-operation-" + operation + ".service"] = record
                if concurrent and pin is not None:
                    rows["foundry-operation-" + str(uuid.UUID(int=123)) + ".service"] = record
            output = None
            if args[:2] == ["systemctl", "list-unit-files"]:
                output = ""
            elif args[:2] == ["systemctl", "list-units"]:
                output = "\n".join(rows)
            elif args[:2] == ["systemctl", "show"] and "-p" in args:
                output = "\n\n".join("Id=" + name + "\n" + "\n".join(k + "=" + v for k, v in rows[name].items())
                                      for name in args[2:args.index("--no-pager")])
            if output is not None:
                return subprocess.CompletedProcess(args, 0, output, "")
            return machine_run(args, **kwargs)
        import feature_plan
        with patch.object(self.host, "run", side_effect=run), \
                patch.object(packages, "_activation_snapshot", wraps=actual_activation_snapshot), \
                patch.object(feature_plan, "reviewed", wraps=feature_plan.reviewed) as checks:
            plan = self.request("release-plan", **arguments)
            if concurrent or preexisting:
                with self.assertRaises(c.DeployError) as error:
                    self.request("activate", **arguments, plan_token=plan["plan_token"])
                self.assertEqual(error.exception.code, "stale_plan")
                self.assertIn("packages", error.exception.detail)
                self.assertNotIn("private-canary", error.exception.detail)
                root = operations.directory(self.host, operation)
                diagnostic = c.read_json(root / "stale-plan.json")
                self.assertEqual(diagnostic, {"observation_keys": ["packages"]})
                self.assertEqual(c.read_json(root / "result.json")["error"]["detail"], error.exception.detail)
                self.assertEqual(self.host.started, [])
            else:
                self.assertTrue(self.request("activate", **arguments, plan_token=plan["plan_token"])["ok"])
            self.assertEqual(checks.call_count, 2)
            self.assertNotIn("pin", checks.call_args_list[0].kwargs)
            self.assertEqual(checks.call_args_list[1].kwargs["pin"], self.host.operation_pin)

    def test_executor_review_accepts_only_its_own_transient_unit(self):
        self.executor_review()

    def test_executor_review_rejects_concurrent_activation_with_names_only(self):
        self.executor_review(concurrent=True)

    def test_executor_review_does_not_hide_changes_to_a_preexisting_unit(self):
        self.executor_review(preexisting=True)

    def test_first_public_release_plans_without_dns_and_dns_changes_do_not_gate_apply(self):
        import socket
        self.build_archive(public=True)
        self.configure_firewall()
        with patch.object(listeners, 'observe_dns', wraps=actual_observe_dns), \
                patch('host.resolve_addresses', side_effect=socket.gaierror(-2, 'missing')):
            bootstrap = self.request('bootstrap-plan', topology=self.topology.projection())
        self.assertEqual(bootstrap['observations']['dns'][0]['state'], 'unresolved')
        with patch.object(listeners, 'observe_dns', wraps=actual_observe_dns), \
                patch('host.resolve_addresses', side_effect=subprocess.TimeoutExpired('resolver', 2)):
            self.request('bootstrap-apply', topology=self.topology.projection(), plan_token=bootstrap['plan_token'], confirm=True)
        operation = str(uuid.uuid4())
        with patch.object(listeners, 'observe_dns', wraps=actual_observe_dns), \
                patch('host.resolve_addresses', side_effect=socket.gaierror(-2, 'missing')):
            plan = self.request('release-plan', archive=str(self.archive), archive_sha256=c.digest(self.archive), operation_id=operation)
        self.assertTrue(plan['ok'])
        self.assertEqual(plan['observations']['dns'][0]['state'], 'unresolved')
        self.assertEqual(plan['observations']['dns'][0]['instructions'],
                         [{'name': self.profile['listeners']['relay.api']['dns']['hostname'],
                           'type': 'A', 'address': '203.0.113.10', 'proxied': False}])
        with patch.object(listeners, 'observe_dns', wraps=actual_observe_dns), \
                patch('host.resolve_addresses', return_value=['203.0.113.77']):
            result = self.request('activate', archive=str(self.archive), archive_sha256=c.digest(self.archive),
                                  operation_id=operation, plan_token=plan['plan_token'])
        self.assertTrue(result['ok'])

    def test_secret_template_rotation_restart_and_rollback_keep_private_output_and_floor(self):
        self.build_archive(secrets=True, independent=True)
        key = "FOUNDRY_SERVICE_SECRET__RELAY_TOKEN"
        plan = self.request("bootstrap-plan", topology=self.topology.projection())
        self.request("bootstrap-apply", topology=self.topology.projection(), plan_token=plan["plan_token"],
                     confirm=True, secrets={"configuration": {key: "initial-canary"}})
        self.request("activate", archive=str(self.archive), archive_sha256=c.digest(self.archive))
        old_version = self.version
        self.build_archive(secrets=True, templates=True, independent=True, version="20261114T010000Z-abcdef13")
        self.request("activate", archive=str(self.archive), archive_sha256=c.digest(self.archive))
        def selected_template(expected):
            _, entry = self.host.require_identity(self.project, self.profile)
            _, selected, inventory, launch = lifecycle.load(self.host, entry)
            self.assertEqual(selected.minimum_generation, 14)
            paths = services.ServicePaths.for_unit(inventory.specs["relay"].identity, inventory.resolution.id)
            path = self.host.paths.p(paths.config("private", "rendered-template", "relay.yaml"))
            self.assertEqual(json.loads(path.read_text().removeprefix("token: ")), expected)
            self.assertEqual(path.stat().st_mode & 0o777, 0o640)
            view = (self.host.paths.p(paths.base) / "current").resolve()
            dispatch = c.read_json(view / "dispatch.json")
            self.assertEqual(dispatch["environment"]["XDG_CONFIG_HOME"], paths.base + "/current/config/private")
            for public in (json.dumps(dispatch), json.dumps(launch), json.dumps(self.host.read_registry()),
                           json.dumps(self.request("status")), json.dumps(self.host.commands),
                           self.host.paths.p("/etc/systemd/system/" + inventory.units["relay"]).read_text()):
                self.assertNotIn(expected, public)
            return path.resolve(), inventory
        first, before = selected_template("initial-canary")
        http = self.host.running[before.units["http"]]
        self.host.started.clear()
        with patch.object(release.backup, "archive_keys"):
            self.request("config-set", secrets={"configuration": {key: 'replacement-canary"#;]雪'}}, confirm=True)
        second, _ = selected_template('replacement-canary"#;]雪')
        self.assertNotEqual(first, second)
        self.assertEqual(self.host.started, ["relay"])
        self.assertEqual(self.host.running[before.units["http"]], http)
        self.request("restart", operation_id=str(uuid.uuid4()))
        third, _ = selected_template('replacement-canary"#;]雪')
        self.assertNotEqual(second, third)
        self.rollback_target(old_version)
        _, entry = self.host.require_identity(self.project, self.profile)
        self.assertEqual(entry["deployment_selection"]["minimum_generation"], 14)
        with self.assertRaisesRegex(c.DeployError, "generation 14"):
            upgrades.check_instance_capabilities(entry, 13)

    def test_service_secret_rotation_retains_http_and_private_values(self):
        import runtime_config
        self.build_archive(secrets=True, independent=True)
        key = "FOUNDRY_SERVICE_SECRET__RELAY_TOKEN"
        plan = self.request("bootstrap-plan", topology=self.topology.projection())
        self.assertFalse(plan["observations"]["required_service_secrets"][0]["present"])
        self.request("bootstrap-apply", topology=self.topology.projection(), plan_token=plan["plan_token"],
                     confirm=True, secrets={"configuration": {key: "initial-canary"}})
        self.request("activate", archive=str(self.archive), archive_sha256=c.digest(self.archive))
        _, entry = self.host.require_identity(self.project, self.profile)
        _, _, before, before_launch = lifecycle.load(self.host, entry)
        http = self.host.running[before.units["http"]]
        self.host.started.clear()
        with patch.object(release.backup, "archive_keys") as archive:
            self.request("config-set", secrets={"configuration": {key: "replacement-canary"}}, confirm=True)
        archive.assert_called_once()
        _, entry = self.host.require_identity(self.project, self.profile)
        _, selected, after, launch = lifecycle.load(self.host, entry)
        self.assertEqual(self.host.started, ["relay"])
        self.assertEqual(self.host.running[before.units["http"]], http)
        self.assertEqual(before.specs["http"], after.specs["http"])
        self.assertEqual(selected.admission.disposition, c.AdmissionDisposition.COMMITTED)
        self.assertEqual(entry["configuration_revision"], 2)
        self.assertNotEqual(before_launch["secret_paths"]["relay"], launch["secret_paths"]["relay"])
        self.assertEqual(runtime_config._read_value_file(self.host, entry, launch["secret_paths"]["relay"]["TOKEN_FILE"]), "replacement-canary")
        public = json.dumps(self.request("status"))
        self.assertNotIn("replacement-canary", public)
        self.assertNotIn(hashlib.sha256(b"replacement-canary\n").hexdigest(), public)
        self.assertNotIn("replacement-canary", json.dumps(self.host.commands))

    def run_coordinator(self, unit):
        # Exercise runtime-v1's actual generation/pinning boundary, with only its
        # machine paths and root ownership mapped to this test-owned filesystem.
        module_spec = importlib.util.spec_from_file_location("test_runtime_coordinator", TOOLS / "server/runtime_v1.py")
        runtime = importlib.util.module_from_spec(module_spec)
        module_spec.loader.exec_module(runtime)
        content = self.host.paths.p("/etc/systemd/system/" + unit).read_text()
        arguments = shlex.split(next(line.split("=", 1)[1] for line in content.splitlines() if line.startswith("ExecStart=")))[-7:]
        self.host.owned_write(self.host.paths.p("/proc/self/cgroup"), "0::/system.slice/" + unit)
        self.host.owned_write(self.host.paths.p("/proc/self/stat"), "22 (coordinator) " + " ".join(["S"] + ["0"] * 18 + ["42"]))
        def mapped(value):
            path = Path(value)
            return self.host.paths.p(str(path)) if path.is_absolute() and not path.is_relative_to(self.host.paths.root) else path
        with patch.object(runtime, "ROOT", self.host.paths.p("/usr/local/lib/foundry")), \
                patch.object(runtime, "STATE", self.host.paths.state / "admission"), \
                patch.object(runtime, "PINS", self.host.paths.p("/run/foundry/runtime-pins")), \
                patch.object(runtime, "Path", side_effect=mapped), \
                patch.object(runtime, "owned", side_effect=lambda path: admission.protected_path(self.host, path, "file" if path.is_file() else "directory")), \
                patch.object(runtime, "pin_directory", side_effect=lambda path: admission.protected_directory(self.host, path)), \
                patch.object(runtime, "os", SimpleNamespace(**{**vars(os), "geteuid": lambda: 0})), \
                patch.dict(os.environ, INVOCATION_ID=uuid.uuid4().hex):
            self.assertEqual(runtime.main(arguments), 0)

    def test_reboot_after_selective_rotation_uses_per_role_generations(self):
        self.test_service_secret_rotation_retains_http_and_private_values()
        _, entry = self.host.require_identity(self.project, self.profile)
        store, _, inventory, _ = lifecycle.load(self.host, entry)
        lifecycle.stop_units(self.host, inventory)
        self.host.owned_write(self.host.paths.p("/proc/sys/kernel/random/boot_id"), str(uuid.uuid4()))
        feature_network.prepare_boot(self.host)
        self.run_coordinator("foundry-admission-" + self.profile["instance_id"] + ".service")
        for name in inventory.start_order(services.enabled_units(self.host, inventory.units)):
            self.host.start(inventory.units[name])
        self.run_coordinator("foundry-admission-open-" + self.profile["instance_id"] + ".service")
        self.assertEqual(set(self.host.running), set(inventory.units.values()))
        self.assertTrue(services.wait_ready_set(self.host, inventory, inventory.roles, time.monotonic() + 1))

    def test_retained_automatic_restart_is_refused_during_pending_rotation(self):
        self.test_service_secret_rotation_retains_http_and_private_values()
        _, entry = self.host.require_identity(self.project, self.profile)
        _, _, inventory, _ = lifecycle.load(self.host, entry)
        withdraw = feature_network.withdraw
        attempted = []
        def observe_pending(*args, **kwargs):
            result = withdraw(*args, **kwargs)
            before = self.host.running[inventory.units["http"]]
            with self.assertRaises(c.DeployError):
                self.host.start(inventory.units["http"])
            self.assertEqual(self.host.running[inventory.units["http"]], before)
            attempted.append(True)
            return result
        with patch.object(feature_network, "withdraw", side_effect=observe_pending), patch.object(release.backup, "archive_keys"):
            self.request("config-set", secrets={"configuration": {"FOUNDRY_SERVICE_SECRET__RELAY_TOKEN": "pending-canary"}}, confirm=True)
        self.assertTrue(attempted)

    def test_whole_restart_after_selective_rotation_uses_normal_validation_permits(self):
        self.test_service_secret_rotation_retains_http_and_private_values()
        before = len(self.host.started)
        result = self.request("restart", operation_id=str(uuid.uuid4()))
        self.assertTrue(result["ok"])
        self.assertEqual(set(self.host.started[before:]), {"http", "relay"})
        _, entry = self.host.require_identity(self.project, self.profile)
        self.assertEqual(lifecycle.load(self.host, entry)[1].admission.disposition, c.AdmissionDisposition.COMMITTED)

    def test_release_review_collects_new_secret_before_selection(self):
        import runtime_config
        plan = self.request("bootstrap-plan", topology=self.topology.projection())
        self.request("bootstrap-apply", topology=self.topology.projection(), plan_token=plan["plan_token"], confirm=True)
        self.build_archive(secrets=True)
        operation = str(uuid.uuid4())
        arguments = {"archive": str(self.archive), "archive_sha256": c.digest(self.archive), "operation_id": operation}
        draft = self.request("release-plan", **arguments)
        self.assertFalse(draft["ok"])
        self.assertEqual(self.host.started, [])
        key = "FOUNDRY_SERVICE_SECRET__RELAY_TOKEN"
        reviewed = self.request("release-plan", **arguments, secrets={"configuration": {key: "review-canary"}})
        _, entry = self.host.require_identity(self.project, self.profile)
        self.assertEqual(runtime_config.service_secret_snapshot(self.host, entry), {})
        self.assertNotIn("review-canary", json.dumps(reviewed))
        with patch.object(release.backup, "archive_keys"):
            result = self.request("activate", **arguments, plan_token=reviewed["plan_token"])
        self.assertTrue(result["ok"])
        _, entry = self.host.require_identity(self.project, self.profile)
        self.assertEqual(entry["configuration_revision"], 2)
        self.assertEqual(set(runtime_config.service_secret_snapshot(self.host, entry)), {"relay-token"})

    def test_review_rejects_changed_enablement_before_stopping(self):
        plan = self.request("bootstrap-plan", topology=self.topology.projection())
        self.request("bootstrap-apply", topology=self.topology.projection(), plan_token=plan["plan_token"], confirm=True)
        self.request("activate", archive=str(self.archive), archive_sha256=c.digest(self.archive))
        arguments = {"archive": str(self.archive), "archive_sha256": c.digest(self.archive), "operation_id": str(uuid.uuid4())}
        reviewed = self.request("release-plan", **arguments)
        _, entry = self.host.require_identity(self.project, self.profile)
        _, _, inventory, _ = lifecycle.load(self.host, entry)
        self.host.enabled.remove(inventory.units["http"])
        before = dict(self.host.running)
        with self.assertRaises(c.DeployError) as error:
            self.request("activate", **arguments, plan_token=reviewed["plan_token"])
        self.assertEqual(error.exception.code, "stale_plan")
        self.assertEqual(self.host.running, before)

    def rollback_target(self, target, *, topology=None):
        arguments = {"target": target, "expected_current": self.request("status")["live_version"], "operation_id": str(uuid.uuid4())}
        reviewed = self.request("release-plan", **arguments, **({"topology": topology} if topology else {}))
        result = self.request("rollback", **arguments, plan_token=reviewed["plan_token"])
        self.profile = result["profile_update"]
        before = list(self.host.started)
        self.assertEqual(self.request("release-plan", **arguments), reviewed)
        self.assertEqual(self.request("rollback", **arguments, plan_token=reviewed["plan_token"]), result)
        self.assertEqual(self.host.started, before)
        return result

    def test_feature_rollback_preserves_rotated_credentials_and_allocates_fresh_ports(self):
        import runtime_config
        self.test_service_secret_rotation_retains_http_and_private_values()
        target = self.version
        self.build_archive(secrets=True, independent=True, version="20261115T000000Z-abcdef12")
        self.request("activate", archive=str(self.archive), archive_sha256=c.digest(self.archive))
        _, entry = self.host.require_identity(self.project, self.profile)
        _, _, previous, _ = lifecycle.load(self.host, entry)
        snapshots = runtime_config.service_secret_snapshot(self.host, entry)
        commands = len(self.host.commands)
        self.rollback_target(target)
        _, entry = self.host.require_identity(self.project, self.profile)
        _, _, current, launch = lifecycle.load(self.host, entry)
        self.assertEqual(current.deployment.release, target)
        self.assertEqual(entry["configuration_revision"], 2)
        self.assertEqual(runtime_config.service_secret_snapshot(self.host, entry), snapshots)
        self.assertFalse(set(previous.resolution.endpoints) & set(current.resolution.endpoints))
        self.assertFalse(any("migrate" in command or "db:seed" in command for command in self.host.commands[commands:]))

    def test_legacy_rollback_requires_target_topology_and_preserves_current_secrets(self):
        import runtime_config
        self.test_service_secret_rotation_retains_http_and_private_values()
        _, entry = self.host.require_identity(self.project, self.profile)
        feature = self.host.load_state(entry)["current"]
        target = "20261112T000000Z-abcdef12"
        topology = parse_topology({"schema": 1, "roles": [{"name": "http"}]})
        manifest = {**feature, "schema": 1, "version": target, "topology_sha256": topology.sha256}
        manifest.pop("deployment")
        manifest["files"] = {key.replace(feature["version"], target): value for key, value in feature["files"].items() if not key.startswith("deploy/")}
        path = release.release_path(self.host, entry, target)
        source = release.release_path(self.host, entry, feature["version"])
        for name in manifest["files"]:
            self.host.owned_write(path / name, (source / name.replace(target, feature["version"])).read_bytes(), mode=0o755 if name == "artisan" else 0o644)
        self.host.owned_write(path / "manifest.json", json.dumps(manifest))
        with self.assertRaises(c.DeployError) as error:
            self.rollback_target(target)
        self.assertEqual(error.exception.code, "rollback_topology_required")
        with self.assertRaises(c.DeployError) as error:
            self.rollback_target(target, topology={"schema": 1, "roles": [{"name": "http"}, {"name": "worker"}]})
        self.assertEqual(error.exception.code, "release_topology")
        secret = runtime_config.service_secret_snapshot(self.host, entry)
        self.rollback_target(target, topology=topology.projection())
        _, entry = self.host.require_identity(self.project, self.profile)
        store, selection, inventory, launch = lifecycle.load(self.host, entry)
        self.assertEqual(inventory.deployment.archive_schema, 1)
        self.assertEqual(set(inventory.roles), {"http"})
        self.assertEqual(runtime_config.service_secret_snapshot(self.host, entry), secret)
        self.assertEqual(selection.admission.disposition, c.AdmissionDisposition.COMMITTED)
        descriptor = c.read_json(self.host.paths.p(launch["descriptors"]["http"]))
        self.assertEqual(descriptor["schema"], 1)
        self.assertNotIn("bindings", descriptor)
        self.assertFalse(any("relay" in unit for unit in self.host.running))
        self.assertTrue((store.durable / "removed-enablement.json").exists())
        self.rollback_target(feature["version"])
        _, entry = self.host.require_identity(self.project, self.profile)
        _, _, restored, _ = lifecycle.load(self.host, entry)
        self.assertEqual(set(restored.roles), {"http", "relay"})
        self.assertEqual(runtime_config.service_secret_snapshot(self.host, entry), secret)
        self.assertIn(restored.units["relay"], self.host.enabled)


    def test_reactivating_previous_archive_uses_current_configuration(self):
        self.test_service_secret_rotation_retains_http_and_private_values()
        retained = self.archive.with_name("retained.tar.gz")
        shutil.copyfile(self.archive, retained)
        target = self.version
        self.build_archive(secrets=True, independent=True, version="20261115T000000Z-abcdef12")
        self.request("activate", archive=str(self.archive), archive_sha256=c.digest(self.archive))
        commands = len(self.host.commands)
        self.request("activate", archive=str(retained), archive_sha256=c.digest(retained))
        self.assertEqual(self.request("status")["live_version"], target)
        self.assertFalse(any("migrate" in cmd or "db:seed" in cmd for cmd in self.host.commands[commands:]))

    def test_application_configuration_and_failed_applied_correction(self):
        import runtime_config
        self.test_service_secret_rotation_retains_http_and_private_values()
        with patch.object(release.backup, "archive_keys"), patch.object(services, "wait_ready", return_value=False):
            with self.assertRaises(c.DeployError):
                self.request("config-set", secrets={"configuration": {"APP__TEST__TOKEN": "new-app-canary"}}, confirm=True)
        _, entry = self.host.require_identity(self.project, self.profile)
        self.assertEqual(self.host.load_state(entry)["last_attempt"]["outcome"], "failed_applied")
        self.assertEqual(runtime_config.validate_runtime_environment(self.host, entry)["APP__TEST__TOKEN"], "new-app-canary")
        store, selection, _, _ = lifecycle.load(self.host, entry)
        self.assertEqual(selection.admission.disposition, c.AdmissionDisposition.PENDING)
        store.require_cleanup_clear()
        self.assertTrue((store.durable / "configuration-hold.json").exists())
        with patch.object(release.backup, "archive_keys"):
            self.request("config-set", secrets={"configuration": {"APP__TEST__TOKEN": "corrected-app-canary"}}, confirm=True)
        self.assertFalse((store.durable / "configuration-hold.json").exists())
        self.assertEqual(lifecycle.load(self.host, entry)[1].admission.disposition, c.AdmissionDisposition.COMMITTED)

    def failed_configuration_for_rollback(self):
        self.test_service_secret_rotation_retains_http_and_private_values()
        target = self.version
        self.build_archive(secrets=True, independent=True, version="20261115T000000Z-abcdef12")
        self.request("activate", archive=str(self.archive), archive_sha256=c.digest(self.archive))
        with patch.object(release.backup, "archive_keys"), patch.object(services, "wait_ready", return_value=False):
            with self.assertRaises(c.DeployError):
                self.request("config-set", secrets={"configuration": {"APP__TEST__TOKEN": "held-canary"}}, confirm=True)
        self.assertFalse(self.host.running)
        arguments = {"target": target, "expected_current": self.version, "operation_id": str(uuid.uuid4())}
        reviewed = self.request("release-plan", **arguments)
        self.assertEqual(set(reviewed["running_roles"]), {"http", "relay"})
        return {**arguments, "plan_token": reviewed["plan_token"]}

    def test_reviewed_rollback_starts_held_configuration_before_clearing_hold(self):
        arguments = self.failed_configuration_for_rollback()
        _, entry = self.host.require_identity(self.project, self.profile)
        store, _, _, _ = lifecycle.load(self.host, entry)
        hold = store.durable / "configuration-hold.json"
        observed = []
        ready = services.wait_ready_set
        def check(*args, **kwargs):
            self.assertTrue(hold.exists())
            observed.append(set(args[2]))
            return ready(*args, **kwargs)
        with patch.object(services, "wait_ready_set", side_effect=check):
            self.request("rollback", **arguments)
        self.assertIn({"http", "relay"}, observed)
        self.assertFalse(hold.exists())
        self.assertEqual(set(role for _, _, role in self.host.running.values()), {"http", "relay"})
        self.assertEqual(lifecycle.load(self.host, entry)[1].admission.disposition, c.AdmissionDisposition.COMMITTED)

    def test_reviewed_rollback_readiness_failure_keeps_configuration_hold(self):
        arguments = self.failed_configuration_for_rollback()
        _, entry = self.host.require_identity(self.project, self.profile)
        store, _, _, _ = lifecycle.load(self.host, entry)
        before = c.read_json(store.durable / "configuration-hold.json")
        with patch.object(services, "wait_ready", return_value=False), self.assertRaises(c.DeployError):
            self.request("rollback", **arguments)
        self.assertEqual(c.read_json(store.durable / "configuration-hold.json"), before)
        self.assertEqual(lifecycle.load(self.host, entry)[1].admission.disposition, c.AdmissionDisposition.PENDING)
        self.assertFalse(self.host.running)

    def test_rollback_restores_removed_worker_using_target_configuration(self):
        self.build_archive(worker=True, independent=True)
        target = self.version
        plan = self.request("bootstrap-plan", topology=self.topology.projection())
        self.request("bootstrap-apply", topology=self.topology.projection(), plan_token=plan["plan_token"], confirm=True)
        self.request("activate", archive=str(self.archive), archive_sha256=c.digest(self.archive))
        current_profile = dict(self.profile)
        self.build_archive(independent=True, version="20261115T000000Z-abcdef12")
        self.profile = current_profile
        result = self.request("activate", archive=str(self.archive), archive_sha256=c.digest(self.archive))
        self.profile = result["profile_update"]
        self.assertNotIn("worker", {r["name"] for r in self.request("status")["instance"]["topology"]["roles"]})
        self.rollback_target(target)
        _, entry = self.host.require_identity(self.project, self.profile)
        _, _, inventory, _ = lifecycle.load(self.host, entry)
        self.assertIn(inventory.units["worker"], self.host.enabled)
        self.assertIn(inventory.units["worker"], self.host.running)
        args = inventory.specs["worker"].arguments
        self.assertEqual(args[args.index("--connection") + 1], "target-queue")

    def test_rotation_preserves_enabled_but_stopped_service(self):
        self.test_service_secret_rotation_retains_http_and_private_values()
        _, entry = self.host.require_identity(self.project, self.profile)
        _, _, inventory, _ = lifecycle.load(self.host, entry)
        self.host.run(["systemctl", "stop", inventory.units["relay"]])
        self.host.started.clear()
        with patch.object(release.backup, "archive_keys"):
            result = self.request("config-set", secrets={"configuration": {"FOUNDRY_SERVICE_SECRET__RELAY_TOKEN": "next-canary"}}, confirm=True)
        self.assertEqual(self.host.started, [])
        self.assertEqual(result["status"], "degraded")
        self.assertIn(inventory.units["relay"], self.host.enabled)

    def test_maintenance_holds_retain_http_and_honor_disable_on_up(self):
        import feature_maintenance
        self.build_archive(independent=True)
        plan = self.request("bootstrap-plan", topology=self.topology.projection())
        self.request("bootstrap-apply", topology=self.topology.projection(), plan_token=plan["plan_token"], confirm=True)
        self.request("activate", archive=str(self.archive), archive_sha256=c.digest(self.archive))
        _, entry = self.host.require_identity(self.project, self.profile)
        _, _, inventory, _ = lifecycle.load(self.host, entry)
        before = self.host.running[inventory.units["http"]]
        self.request("down", confirm=True)
        _, entry = self.host.require_identity(self.project, self.profile)
        store, selection, inventory, _ = lifecycle.load(self.host, entry)
        marker = self.host.paths.p(feature_maintenance.marker_path(inventory.specs["relay"].identity))
        self.assertTrue(marker.is_file())
        self.assertEqual(selection.maintenance_holds, ("relay",))
        self.assertIn(inventory.units["relay"], self.host.enabled)
        self.assertNotIn(inventory.units["relay"], self.host.running)
        self.assertEqual(self.host.running[inventory.units["http"]], before)
        content = self.host.paths.p("/etc/systemd/system/" + inventory.units["relay"]).read_text()
        self.assertIn("ConditionPathExists=!" + feature_maintenance.marker_path(inventory.specs["relay"].identity), content)
        self.host.run(["systemctl", "disable", inventory.units["relay"]])
        self.request("up", confirm=True)
        self.assertFalse(marker.exists())
        self.assertNotIn(inventory.units["relay"], self.host.running)
        self.assertNotIn(inventory.units["relay"], self.host.enabled)
        self.assertEqual(self.host.running[inventory.units["http"]], before)
        store.require_cleanup_clear()

    def test_direct_artisan_pause_reconciles_on_status_and_unavailable_store_retains_holds(self):
        import feature_maintenance
        self.build_archive(independent=True)
        plan = self.request("bootstrap-plan", topology=self.topology.projection())
        self.request("bootstrap-apply", topology=self.topology.projection(), plan_token=plan["plan_token"], confirm=True)
        self.request("activate", archive=str(self.archive), archive_sha256=c.digest(self.archive))
        self.host.maintenance_down = True
        result = self.request("status")
        self.assertTrue(result["services"]["relay"]["maintenance_held"])
        _, entry = self.host.require_identity(self.project, self.profile)
        _, _, inventory, _ = lifecycle.load(self.host, entry)
        with patch("maintenance.status", return_value=None):
            self.request("status")
        self.assertNotIn(inventory.units["relay"], self.host.running)
        self.host.maintenance_down = False
        self.request("status")
        self.assertIn(inventory.units["relay"], self.host.running)
        self.assertEqual(feature_maintenance.record(self.host, entry)["holds"], [])

    def test_failed_up_keeps_shared_pause_off_and_allows_explicit_retry_after_cleanup(self):
        self.build_archive(independent=True)
        plan = self.request("bootstrap-plan", topology=self.topology.projection())
        self.request("bootstrap-apply", topology=self.topology.projection(), plan_token=plan["plan_token"], confirm=True)
        self.request("activate", archive=str(self.archive), archive_sha256=c.digest(self.archive))
        self.request("down", confirm=True)
        with patch.object(services, "wait_ready_set", return_value=False), self.assertRaises(c.DeployError):
            self.request("up", confirm=True)
        self.assertFalse(self.host.maintenance_down)
        _, entry = self.host.require_identity(self.project, self.profile)
        store, _, inventory, _ = lifecycle.load(self.host, entry)
        store.require_cleanup_clear()
        self.assertNotIn(inventory.units["relay"], self.host.running)
        self.assertEqual(self.host.load_state(entry)["last_attempt"]["outcome"], "failed")
        self.request("up", confirm=True)
        self.assertIn(inventory.units["relay"], self.host.running)

    def test_deploy_during_maintenance_keeps_hold_and_up_restores_new_release(self):
        self.build_archive(independent=True)
        plan = self.request("bootstrap-plan", topology=self.topology.projection())
        self.request("bootstrap-apply", topology=self.topology.projection(), plan_token=plan["plan_token"], confirm=True)
        self.request("activate", archive=str(self.archive), archive_sha256=c.digest(self.archive))
        self.request("down", confirm=True)
        self.build_archive(independent=True, version="20261115T000000Z-abcdef12")
        self.request("activate", archive=str(self.archive), archive_sha256=c.digest(self.archive))
        _, entry = self.host.require_identity(self.project, self.profile)
        _, selection, inventory, _ = lifecycle.load(self.host, entry)
        self.assertEqual(selection.maintenance_holds, ("relay",))
        self.assertNotIn(inventory.units["relay"], self.host.running)
        self.request("up", confirm=True)
        self.assertEqual(self.host.running[inventory.units["relay"]][1].deployment.release, self.version)

    def test_maintenance_without_stopped_roles_retains_entire_graph(self):
        plan = self.request("bootstrap-plan", topology=self.topology.projection())
        self.request("bootstrap-apply", topology=self.topology.projection(), plan_token=plan["plan_token"], confirm=True)
        self.request("activate", archive=str(self.archive), archive_sha256=c.digest(self.archive))
        running = dict(self.host.running)
        self.request("down", confirm=True)
        self.assertTrue(self.host.maintenance_down)
        self.request("up", confirm=True)
        self.assertFalse(self.host.maintenance_down)
        self.assertEqual(self.host.running, running)

    def test_maintenance_endpoint_binding_names_consumer_provider_and_binding_before_deployment(self):
        self.build_archive(independent=True)
        plan = self.request("bootstrap-plan", topology=self.topology.projection())
        self.request("bootstrap-apply", topology=self.topology.projection(), plan_token=plan["plan_token"], confirm=True)
        self.request("activate", archive=str(self.archive), archive_sha256=c.digest(self.archive))
        running = dict(self.host.running)
        self.build_archive(independent=True, endpoint_binding=True, version="20261115T000000Z-abcdef12")
        with self.assertRaises(c.DeployError) as error:
            self.request("release-plan", archive=str(self.archive), archive_sha256=c.digest(self.archive), operation_id=str(uuid.uuid4()))
        self.assertEqual(error.exception.code, "maintenance_graph")
        self.assertIn("Role http", error.exception.detail)
        self.assertIn("endpoint binding relay", error.exception.detail)
        self.assertIn("stopped role relay", error.exception.detail)
        self.assertIn('maintenance: "continue"', error.exception.detail)
        self.assertIn("remove binding relay", error.exception.detail)
        self.assertEqual(self.host.running, running)

    def test_executable_binding_to_same_file_as_stopped_role_is_not_a_maintenance_dependency(self):
        import feature_maintenance
        self.build_archive(independent=True)
        value = self.topology.projection()
        value["roles"][0]["bindings"]["relay-cli"] = {"executable": {"artifact": "relay", "entry": "server"}}
        self.assertEqual(feature_maintenance.stopped_roles(parse_topology(value)), {"relay"})
        value["roles"][0]["bindings"]["relay"] = {"endpoint": {"role": "relay", "listener": "api", "scheme": "http"}}
        value["roles"][1]["maintenance"] = "continue"
        parsed = parse_topology(value)
        self.assertEqual(feature_maintenance.stopped_roles(parsed), set())
        self.assertEqual(parsed.projection(), value)

    def test_maintenance_rejects_continuing_consumer_of_stopped_provider(self):
        import feature_maintenance
        value = self.topology.projection()
        next(role for role in value["roles"] if role["name"] == "relay")["maintenance"] = "stop"
        with self.assertRaises(c.DeployError) as error:
            feature_maintenance.stopped_roles(parse_topology(value))
        self.assertEqual(error.exception.code, "maintenance_graph")

    def test_plan_apply_activate_report_logs_restart_and_boot_use_current_owners(self):
        plan = self.request("bootstrap-plan", topology=self.topology.projection())
        self.assertIn("libnginx-mod-stream", plan["observations"]["packages"]["missing"])
        self.assertNotIn("libnginx-mod-stream", self.topology.roles[0].common.packages)
        self.assertTrue(self.request("bootstrap-apply", topology=self.topology.projection(), plan_token=plan["plan_token"], confirm=True)["ok"])
        registry, entry = self.host.require_identity(self.project, self.profile)
        site = self.host.paths.p("/etc/nginx/sites-available/foundry-" + entry["name"] + ".conf")
        self.assertIn("# Foundry runtime admission", site.read_text())
        self.assertNotIn("proxy_pass", site.read_text())
        provision._site_checks(self.host, entry)
        self.assertEqual(packages._load(self.host)["default_activation"]["nginx.service"]["state"], "released")
        self.assertTrue(self.host.redis_host.active)
        masks = packages._load(self.host)["default_activation"]
        self.assertEqual(masks["redis-server@.service"]["state"], "released")
        for unit in set(DEFAULT_UNITS) - {"nginx.service", "redis-server@.service"}:
            self.assertEqual(masks[unit]["state"], "masked")
            self.assertEqual(os.readlink(self.host.paths.p("/etc/systemd/system/" + unit)), "/dev/null")
        with patch.object(certificates, "ensure_hostnames", wraps=certificates.ensure_hostnames) as names:
            self.assertTrue(self.request("activate", archive=str(self.archive), archive_sha256=c.digest(self.archive), operation_id=str(uuid.uuid4()))["ok"])
        names.assert_called_once()
        profiles, topologies = names.call_args.args[3], names.call_args.kwargs["topologies"]
        self.assertEqual(len(profiles), len(topologies))
        self.assertEqual(profiles[0].projection(), self.profile)
        self.assertEqual(topologies[0], self.topology)
        registry, entry = self.host.require_identity(self.project, self.profile)
        store, selected, inventory, _ = lifecycle.load(self.host, entry)
        self.assertEqual(selected.admission.disposition, c.AdmissionDisposition.COMMITTED)
        self.assertEqual(self.host.started, ["relay", "http"])
        self.assertTrue(provision._ready_configuration(self.host, registry, entry, TOOLS))
        self.assertTrue(services.wait_ready_set(self.host, inventory, inventory.roles, time.monotonic() + 1))
        network = lifecycle.network_report(self.host, entry, report.claim_inventory(registry))
        self.assertNotIn("nginx_projection_unverified", [f["code"] for f in network["findings"]])
        with patch.object(report, "readiness", autospec=True, return_value={"ready": True, "http_status": 200}), \
                patch.object(report, "certificate", autospec=True, return_value={"status": "valid", "expires_at": None}):
            result = self.request("report")
        reported = result["projects"][0]["instances"][0]
        self.assertEqual(reported["network"], network)
        self.assertTrue(reported["metadata_consistent"])
        self.assertNotIn("running_release_mismatch", [p["code"] for p in result["problems"]])
        self.assertEqual(self.request("logs", role="relay")["logs"], "fixture service log")
        self.assertIn(inventory.units["relay"], self.host.commands[-1])
        descriptor = c.read_json(self.host.paths.opt(entry["name"]) / "runtime" / inventory.resolution.id / "http.json")
        self.assertEqual(descriptor["bindings"]["endpoints"][0]["name"], "relay")
        self.assertTrue(self.request("restart", operation_id=str(uuid.uuid4()))["ok"])
        self.assertEqual(self.host.started, ["relay", "http", "relay", "http"])
        registry, entry = self.host.require_identity(self.project, self.profile)
        lifecycle.stop_units(self.host, inventory)
        self.host.owned_write(self.host.paths.p("/proc/sys/kernel/random/boot_id"), str(uuid.uuid4()))
        feature_network.prepare_boot(self.host)
        lifecycle.boot(self.host, entry, inventory)
        # systemd schedules enabled workloads after the boot closure unit.
        for name in inventory.start_order(services.enabled_units(self.host, inventory.units)):
            self.host.start(inventory.units[name])
        lifecycle.open_boot(self.host, entry, inventory)
        self.assertEqual(self.host.started[-2:], ["relay", "http"])
        boot_unit = self.host.paths.p("/etc/systemd/system/foundry-admission-" + self.profile["instance_id"] + ".service")
        self.assertIn("After=network-online.target ufw.service nginx.service", boot_unit.read_text())
        opener = boot_unit.with_name("foundry-admission-open-" + self.profile["instance_id"] + ".service")
        self.assertIn("Requires=nginx.service " + boot_unit.name, opener.read_text())
        dropin = self.host.paths.p("/etc/systemd/system/nginx.service.d/foundry-edge-boot.conf")
        self.assertIn("Requires=" + feature_network.BOOT_UNIT, dropin.read_text())
        dropin.write_text(dropin.read_text() + "# drift\n")
        self.assertFalse(provision._ready_configuration(self.host, registry, entry, TOOLS))
        with self.assertRaises(c.DeployError) as drift:
            self.request("bootstrap-plan", topology=self.topology.projection())
        self.assertEqual(drift.exception.code, "capability_unavailable")
        # Shared pre-nginx cleanup cannot depend on one instance's valid admission.
        (store.durable / "selection.json").unlink()
        feature_network.prepare_boot(self.host)
        self.assertFalse(list(self.host.paths.p(edge.RUNTIME + "/" + self.profile["instance_id"]).glob("*.conf")))

    def configure_firewall(self):
        from network_firewall_fixture import ROOT, rule_file, live_rules
        placed = parse_profile(self.profile, self.topology)
        self.host.public_profile = placed
        self.host.firewall_live = {family: live_rules(placed.instance_id, family) for family in (4, 6)}
        self.host.owned_write(self.host.paths.p("/etc/default/ufw"),
                              'IPV6=yes\nDEFAULT_INPUT_POLICY="DROP"\nDEFAULT_FORWARD_POLICY="DROP"\nDEFAULT_OUTPUT_POLICY="ACCEPT"\nMANAGE_BUILTINS=no\n')
        self.host.owned_write(self.host.paths.p("/etc/ufw/ufw.conf"), "ENABLED=yes\n")
        for family, suffix in ((4, ""), (6, "6")):
            for stem in ("before", "user", "after"):
                self.host.owned_write(self.host.paths.p(f"/etc/ufw/{stem}{suffix}.rules"), rule_file(stem, family=family))
        for name in ("ufw-init", "ufw-init-functions"):
            self.host.owned_write(self.host.paths.p("/usr/lib/ufw/" + name), (ROOT / name).read_text())

    def test_running_public_instance_rebootstrap_is_unchanged(self):
        self.build_archive(public=True)
        self.configure_firewall()
        plan = self.request("bootstrap-plan", topology=self.topology.projection())
        self.request("bootstrap-apply", topology=self.topology.projection(), plan_token=plan["plan_token"], confirm=True)
        self.request("activate", archive=str(self.archive), archive_sha256=c.digest(self.archive), operation_id=str(uuid.uuid4()))
        before = dict(self.host.firewall_live)
        self.host.commands.clear()
        with patch.object(certificates, "inspect", return_value={"state": "ready"}):
            plan = self.request("bootstrap-plan", topology=self.topology.projection())
            result = self.request("bootstrap-apply", topology=self.topology.projection(), plan_token=plan["plan_token"], confirm=True)
        self.assertTrue(result["unchanged"])
        self.assertEqual(self.host.firewall_live, before)
        self.assertNotIn(["ufw", "reload"], self.host.commands)
        self.assertFalse(any(command[0].endswith("-restore") for command in self.host.commands))
        self.assertEqual(plan["observations"]["network"]["persistent_projection"], {})
        for changed, expected in ((before[4].replace("203.0.113.0/24", "0.0.0.0/0"), "firewall_drift"),
                                  (before[4].replace("COMMIT", "-A INPUT -p tcp --dport 9443 -j ACCEPT\nCOMMIT"), "firewall_foreign_allow")):
            self.host.firewall_live[4] = changed
            with self.assertRaises(c.DeployError) as error:
                self.request("bootstrap-plan", topology=self.topology.projection())
            self.assertEqual(error.exception.code, expected)
        self.assertNotIn(["ufw", "reload"], self.host.commands)

    def test_deployed_generation_12_profile_can_plan_apply_and_resume_current_upgrade(self):
        from contextlib import ExitStack
        import runtime_config
        source = self.host.paths.root / 'preview-tools'
        shutil.copytree(TOOLS, source, ignore=shutil.ignore_patterns('__pycache__'))
        metadata = c.read_json(source / 'protocol.json')
        metadata['generation'] = 12
        c.atomic_json(source / 'protocol.json', metadata)
        # Construct deployed preview state using its prior capability floor and
        # immutable generation-12 bundle. The upgrade uses current guards unchanged.
        with ExitStack() as stack:
            stack.enter_context(patch.object(runtime_config, 'scheduler_environment', return_value={}))
            for module in (lifecycle, upgrades):
                stack.enter_context(patch.object(module, 'FEATURE_LIFECYCLE_GENERATION', 12))
            for module, path in ((lifecycle, 'lib/feature_lifecycle.py'), (upgrades, 'lib/upgrades.py'),
                                 (provision, 'lib/provision.py'), (manager, 'server/manage.py')):
                stack.enter_context(patch.object(module, '__file__', str(source / path)))
            self.envelope = upgrades.protocol_metadata(source)
            plan = self.request('bootstrap-plan', topology=self.topology.projection())
            self.request('bootstrap-apply', topology=self.topology.projection(), plan_token=plan['plan_token'], confirm=True)
            self.request('activate', archive=str(self.archive), archive_sha256=c.digest(self.archive))
        _, entry = self.host.require_identity(self.project, self.profile)
        store, selected, inventory, _ = lifecycle.load(self.host, entry)
        self.assertEqual(selected.minimum_generation, 12)
        self.assertEqual(self.host.read_registry()['tools']['generation'], 12)
        # Preview units were deliberately disabled after startup.
        self.host.run(['systemctl', 'disable', *inventory.units.values()])
        (store.durable / 'enablement.json').unlink()
        before = dict(self.host.running)
        self.envelope = upgrades.protocol_metadata(TOOLS)
        plan = self.request('tools-plan')
        operation = str(uuid.uuid4())
        result = self.request('tools-apply', plan_token=plan['plan_token'], operation_id=operation)
        self.assertEqual(result['outcome'], 'succeeded')
        self.assertEqual(self.host.read_registry()['tools']['generation'], upgrades.protocol()['generation'])
        self.assertEqual(self.host.running, before)
        self.assertFalse(set(inventory.units.values()) & self.host.enabled)
        self.assertEqual(self.request('upgrade-resume', plan_token=plan['plan_token'],
                                     operation_id=operation)['outcome'], 'unchanged')
        old_environment = runtime_config.effective_environment_path(self.host, entry, inventory.resolution)
        self.assertNotIn(runtime_config.SCHEDULER_ENABLED, c.parse_env(old_environment.read_text()))
        reviewed = self.request('release-plan', archive=str(self.archive), archive_sha256=c.digest(self.archive),
                                operation_id=str(uuid.uuid4()))
        self.assertTrue(reviewed['ok'])
        _, entry = self.host.require_identity(self.project, self.profile)
        candidate, _ = lifecycle.load_resolution(self.host, entry, reviewed['candidate_resolution'])
        candidate_env = runtime_config.effective_environment_path(self.host, entry, candidate.resolution, candidate=True)
        self.assertEqual(c.parse_env(candidate_env.read_text())[runtime_config.SCHEDULER_ENABLED], 'false')
        self.assertNotIn(runtime_config.SCHEDULER_ENABLED, c.parse_env(old_environment.read_text()))
        with patch.object(release.backup, 'archive_keys'):
            self.request('config-set', secrets={'configuration': {'APP__TEST__TOKEN': 'preview-upgrade'}}, confirm=True)
        _, entry = self.host.require_identity(self.project, self.profile)
        values = runtime_config.validate_runtime_environment(self.host, entry)
        self.assertEqual(values[runtime_config.SCHEDULER_ENABLED], 'false')
        self.assertEqual(values['APP__TEST__TOKEN'], 'preview-upgrade')

    def test_second_bootstrap_after_tool_upgrade_retains_verified_boot_pin(self):
        plan = self.request("bootstrap-plan", topology=self.topology.projection())
        self.request("bootstrap-apply", topology=self.topology.projection(), plan_token=plan["plan_token"], confirm=True)
        pin = self.host.read_registry()["tools"]["bundle_sha256"]
        boot = self.host.paths.p("/etc/systemd/system/" + feature_network.BOOT_UNIT)
        before = boot.read_bytes()
        source = self.host.paths.root / "upgraded-tools"
        shutil.copytree(TOOLS, source, ignore=shutil.ignore_patterns("__pycache__"))
        changed = source / "lib/services.py"
        changed.write_text(changed.read_text() + "\n# reviewed upgrade fixture\n")
        upgrade = upgrades.plan_upgrade(self.host, source)
        self.assertEqual(upgrades.apply_upgrade(self.host, source, upgrade["plan_token"], str(uuid.uuid4()))["outcome"], "succeeded")
        self.assertNotEqual(self.host.read_registry()["tools"]["bundle_sha256"], pin)
        first = self.host.read_registry()["instances"][self.profile["instance_id"]]
        self.assertTrue(feature_network.bootstrap_ready(self.host, first, source, upgrades.tool_bundle(source)[2]))
        self.project, self.profile, _ = fixture(project_slug="second", host_id=self.profile["host"]["host_id"])
        self.build_archive(public=True)
        self.configure_firewall()
        self.host.account = None
        plan = provision.bootstrap_plan(self.host, self.project, self.profile, source, topology=self.topology.projection())
        self.assertTrue(provision.bootstrap_apply(self.host, self.project, self.profile, source,
                         plan_token=plan["plan_token"], topology=self.topology.projection())["ok"])
        self.assertEqual(boot.read_bytes(), before)
        self.assertTrue((self.host.paths.tools / "tools" / pin / "manifest.json").exists())

    def test_network_preparation_checks_owners_before_any_network_mutation(self):
        plan = self.request("bootstrap-plan", topology=self.topology.projection())
        self.request("bootstrap-apply", topology=self.topology.projection(), plan_token=plan["plan_token"], confirm=True)
        bundle = self.host.read_registry()["tools"]["bundle_sha256"]
        self.project, self.profile, _ = fixture(project_slug="second", host_id=self.profile["host"]["host_id"])
        self.build_archive(public=True)
        self.configure_firewall()
        placed = parse_profile(self.profile, self.topology)
        review = feature_network.plan(self.host, self.topology, placed, {})
        entry = provision.new_entry(self.project, self.profile, {**self.topology.projection(), "sha256": self.topology.sha256},
                                    8101, upgrades.protocol(TOOLS))
        before = dict(self.host.firewall_live)
        paths = ["/etc/systemd/system/" + feature_network.BOOT_UNIT,
                 "/etc/systemd/system/nginx.service.d/foundry-edge-boot.conf", services.RUNTIME_ENTRY,
                 edge.STREAM_INCLUDE, "/etc/nginx/nginx.conf",
                 c.HOST_PATHS["tools"] + "/tools/" + bundle + "/lib/services.py"]
        for name in paths:
            with self.subTest(path=name):
                path = self.host.paths.p(name)
                original = path.read_bytes()
                path.write_bytes(original + b"\n# unreviewed change\n")
                try:
                    with patch.object(firewall, "install_closed", wraps=firewall.install_closed) as install, \
                            patch.object(edge, "prepare_boot", wraps=edge.prepare_boot) as boot, self.assertRaises(c.DeployError):
                        feature_network.prepare(self.host, entry, review, TOOLS, bundle)
                    install.assert_not_called()
                    boot.assert_not_called()
                    self.assertEqual(self.host.firewall_live, before)
                finally:
                    path.write_bytes(original)
        directory = self.host.paths.p(edge.RUNTIME + "/" + placed.instance_id)
        directory.mkdir(parents=True)
        route = directory / "a.conf"
        route.write_text("# existing route\n")
        (directory / "z.conf").symlink_to(route)
        with patch.object(firewall, "install_closed", wraps=firewall.install_closed) as install, self.assertRaises(c.DeployError):
            feature_network.prepare(self.host, entry, review, TOOLS, bundle)
        install.assert_not_called()
        self.assertEqual(route.read_text(), "# existing route\n")

    def test_bootstrap_resumes_reserved_redis_template_handoff(self):
        plan = self.request("bootstrap-plan", topology=self.topology.projection())
        authorize = packages.authorize_default_activation
        token = None

        def interrupt(host, unit, claim):
            nonlocal token
            if unit == redis_acl.REDIS_TEMPLATE:
                self.assertIsNotNone(host.host_lock_fd)
                registry = host.read_registry()
                shared, private = redis_acl._service_claim(host, registry)
                redis_acl._owned_service(shared, private)
                token = shared["ownership_token"]
                self.assertEqual(claim, {**shared, "unit": unit, "installed_by_foundry": True})
                raise c.DeployError("interrupted", "Before Redis template handoff")
            return authorize(host, unit, claim)

        with patch.object(packages, "authorize_default_activation", side_effect=interrupt), self.assertRaisesRegex(c.DeployError, "Before Redis"):
            self.request("bootstrap-apply", topology=self.topology.projection(), plan_token=plan["plan_token"], confirm=True)
        self.assertTrue(packages.default_activation_pending(self.host, redis_acl.REDIS_TEMPLATE))
        self.assertFalse(self.host.redis_host.active)
        plan = self.request("bootstrap-plan", topology=self.topology.projection())
        self.assertTrue(self.request("bootstrap-apply", topology=self.topology.projection(), plan_token=plan["plan_token"], confirm=True)["ok"])
        self.assertEqual(self.host.read_registry()["shared"]["redis"]["ownership_token"], token)
        self.assertTrue(self.host.redis_host.active)
        self.assertFalse(packages.default_activation_pending(self.host, redis_acl.REDIS_TEMPLATE))

    def test_public_listener_plan_reviews_literal_firewall_projection_and_binds_apply(self):
        from test_host_capabilities import declaration, profile
        from network_firewall_fixture import rule_file
        import firewall
        declared = declaration()
        declared["roles"][1]["listeners"]["ingress"]["tls"] = "none"
        topology = parse_topology(declared)
        placed = profile()
        placed["instance_id"] = self.profile["instance_id"]
        placed = parse_profile(placed, topology)
        for family, suffix in ((4, ""), (6, "6")):
            self.host.owned_write(self.host.paths.p("/etc/ufw/before" + suffix + ".rules"), rule_file("before", family=family))
        self.host.owned_write(self.host.paths.p("/etc/nginx/nginx.conf"), "include /etc/nginx/modules-enabled/*.conf;\nhttp {}\n")
        evidence = firewall.FirewallObservation(placed.instance_id, BOOT, True, "a" * 64, (9443,),
                                               tuple(claim.socket for claim in listeners.public_claims(placed)))
        with patch.object(firewall, "observe", autospec=True, return_value=evidence) as observe:
            review = feature_network.plan(self.host, topology, placed, {})
        observe.assert_called_once_with(self.host, placed.instance_id, listeners.public_claims(placed), integrated=False)
        self.assertEqual(review["firewall"], evidence.sha256)
        for family, suffix in ((4, ""), (6, "6")):
            self.assertEqual(review["persistent_projection"]["/etc/ufw/before" + suffix + ".rules"],
                             firewall.persistent_projection(rule_file("before", family=family), placed.instance_id, family=family))
        findings = edge.edge_findings(placed.projection(), TOOLS / "lib", topology=topology)
        self.assertIn("listener_unencrypted", [f["code"] for f in findings])
        entry = provision.new_entry(self.project, placed.projection(), {**topology.projection(), "sha256": topology.sha256},
                                    8100, upgrades.protocol(TOOLS))
        with patch.object(firewall, "install_closed", autospec=True, return_value=evidence) as install:
            feature_network.prepare(self.host, entry, review, TOOLS, upgrades.tool_bundle(TOOLS)[2])
        install.assert_called_once_with(self.host, placed.instance_id, listeners.public_claims(placed), reviewed_sha256=evidence.sha256)
