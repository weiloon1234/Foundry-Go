"""Real plan/apply/lifecycle/supply/network owners with only machine I/O simulated.

The fake never executes commands. apt authentication, database/certificate providers
and HTTP responses are fixtures; all public lane B/C calls retain their signatures.
"""
from dataclasses import asdict
import hashlib
import io
import json
import os
from pathlib import Path
import subprocess
import shutil
import sys
import tarfile
import tempfile
import time
import unittest
from unittest.mock import Mock, patch
import uuid

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "lib"))
import admission
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
import provision
import release
import report
import services
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
        _, selection, inventory, launch = lifecycle.load(self, entry)
        role = next(role for role, name in inventory.units.items() if name == unit)
        self.sequence += 2
        invocation = format(self.sequence, "032x")
        process = self.proc(self.sequence, invocation)
        pin = c.HelperInvocation(admission.boot_id(self), inventory.specs[role].identity, process, registry["tools"]["bundle_sha256"],
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
            unit = args[2] if len(args) > 2 else ""
            if action == "is-active":
                code = 0 if self.nginx else 3
            elif action == "enable" and "--now" in args and args[-1] == "nginx":
                assert not self.paths.p("/etc/systemd/system/nginx.service").is_symlink()
                self.nginx = True
            elif action == "enable":
                self.enabled.update(args[2:])
            elif action == "disable":
                self.enabled.difference_update(args[2:])
            elif action == "is-enabled":
                output, code = ("enabled", 0) if unit in self.enabled else ("disabled", 1)
            elif action == "reload":
                self.worker += 1
            elif action == "start" and unit.startswith("foundry-"):
                self.start(unit)
            elif action == "stop":
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
                    values = {"Id": unit, "LoadState": "loaded", "ActiveState": "active" if current else "inactive",
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
            if "-6" not in args and "-u" not in args:
                if self.public_profile:
                    for claim in listeners.public_claims(self.public_profile):
                        path = self.paths.p(edge.RUNTIME + "/" + claim.instance_id + "/stream-" + claim.role + "." + claim.listener + ".conf")
                        if path.exists():
                            rows.append(f'LISTEN 0 128 {claim.socket.address}:{claim.socket.port} 0.0.0.0:* users:(("nginx",pid={self.worker},fd=3))')
                for process, inventory, role in self.running.values():
                    for endpoint in inventory.resolution.endpoints:
                        if endpoint.role == role:
                            rows.append(f'LISTEN 0 128 {endpoint.address}:{endpoint.port} 0.0.0.0:* users:(("app",pid={process.pid},fd=3))')
            output = "\n".join(rows)
        elif args[0] == "runuser":
            if "redis:keys" in args:
                output = "[]"
            elif "migrate:status" in args:
                output = '{"targets":[{"problems":[],"migrations":[]}]}'
        elif args[0] == "journalctl":
            output = "fixture service log"
        elif len(args) > 3 and args[3] == "prune":
            output = json.dumps(upgrades._prune_bundles(self))
        elif args[0] not in ("chown", "chmod"):
            raise AssertionError(args)
        return subprocess.CompletedProcess(args, code, output, "")


class FeatureWiringTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
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
            (services, "probe", {"return_value": True}),
            (operations, "Host", {"return_value": self.host}),
            (feature_runtime, "Host", {"return_value": self.host}),
        ):
            self.enterContext(patch.object(module, name, autospec=True, **options))
        self.enterContext(patch.object(release, "STABILITY_SECONDS", 0))
        runtime_os = Mock(wraps=os)
        runtime_os.geteuid.return_value = 0
        self.enterContext(patch.object(feature_runtime, "os", runtime_os))

    def prepare_postgres(self, host, registry, **kwargs):
        registry["shared"]["postgresql"] = {"unit": "postgresql@16-foundry.service", "port": 55432}

    def build_archive(self, *, public=False):
        binary = elf("linux-arm64")
        checksum = hashlib.sha256(binary).hexdigest()
        self.topology = parse_topology({"schema": 2, "roles": [
            {"name": "http", "kind": "builtin", "requires": ["relay"], "packages": ["jq"],
             "bindings": {"relay": {"endpoint": {"role": "relay", "listener": "api", "scheme": "http"}},
                          "checksum": {"executable": {"package": "jq", "path": "/usr/bin/jq"}}}},
            {"name": "relay", "kind": "executable", "executable": {"artifact": "relay", "entry": "server"},
             "arguments": [{"listener_address": "api"}, {"listener_port": "api"}],
             "listeners": {"api": ({"transport": "tcp", "bind": "public", "tls": "none", "authentication": True}
                                    if public else {"transport": "tcp", "bind": "loopback"})},
             "readiness": {"kind": "http", "listener": "api", "path": "/ready", "status": 200},
             "restart": {"policy": "on-failure", "delay_seconds": 5, "burst": 5},
             "limits": {"memory_bytes": 268435456, "cpu_percent": 100, "tasks": 64},
             "maintenance": "stop", "state": {"kind": "disposable"}}],
            "artifacts": {"relay": {"version": "1", "format": "file", "executables": {"server": "relay"},
                "platforms": {"linux-arm64": {"url": "https://example.invalid/relay", "sha256": checksum}}}}})
        self.profile.update(schema=3, placement={"http": 1, "relay": 1}, listeners={})
        if public:
            from test_host_capabilities import profile
            self.profile["listeners"] = {"relay.api": profile()["listeners"]["relay.ingress"]}
        self.profile = parse_profile(self.profile, self.topology).projection()
        payload = self.host.paths.root / "relay.bin"
        payload.write_bytes(binary)
        members = artifacts.inspect_payload(dict(self.topology.artifacts)["relay"], "linux-arm64", payload, artifacts.ExpansionBudget())
        path = "deploy/artifacts/relay.bin"
        self.version = "20261114T000000Z-abcdef12"
        files = {"artisan": binary, "config/deploy.toml": b"[project]\n", "public/index.html": b"fixture",
                 "private-source-maps/" + self.version + "/a.map": b"{}", path: binary}
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
        return manager.execute(self.host, action, {**self.envelope, "project": self.project, "profile": self.profile, **kwargs})

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
