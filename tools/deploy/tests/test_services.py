"""Single-instance service inventory/preparation without installed units or mounts."""
from dataclasses import replace
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import sys
import subprocess
import tempfile
import time
import unittest
from unittest.mock import patch, MagicMock

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "lib"))
from contracts import unit_selection_names
import admission
import contracts as c
from host import Host, Paths
import runtime_config
import services
import topology as t
from test_admission import attempt, helper
from test_contracts import fixture
from test_host_capabilities import (INSTANCE, OPERATION, BOOT, CURRENT, DIGEST, OTHER, bundle, declaration, selection, closure)

CONFIG = b'{"listen":{"address":"", "port":0}, "saved":"original"}'


def inventory_fixture():
    raw = declaration()
    raw["roles"][1]["listeners"].pop("ingress")
    raw["roles"][1]["requires"] = ["http"]
    raw["roles"][2]["bindings"] = {}
    raw["roles"][1]["configs"]["settings"]["mode"] = "recreate"
    raw["roles"][1]["configs"]["seed"] = {"source": "deploy/files/seed.txt", "mode": "seed-once"}
    raw["roles"][1]["configs"]["fixed"] = {"source": "deploy/files/fixed.txt", "mode": "immutable"}
    _, profile, entry = fixture()
    profile = {**profile, "schema": 3, "instance_id": INSTANCE, "placement": {"http": 1, "relay": 1, "worker": 1}, "listeners": {}}
    entry["profile"] = {**entry["profile"], "instance_id": INSTANCE}
    parsed = t.parse_topology(raw)
    placed = t.parse_profile(profile, parsed)
    members = tuple(c.PayloadMember(path, hashlib.sha256(data).hexdigest(), len(data), "config") for path, data in
                    (("deploy/files/relay.json", CONFIG), ("deploy/files/seed.txt", b"seed"), ("deploy/files/fixed.txt", b"fixed")))
    units = []
    for role, original in zip(parsed.roles, raw["roles"]):
        name = role.common.name
        paths = services.ServicePaths.for_unit(c.UnitIdentity(INSTANCE, name, 1), CURRENT)
        units.append(c.UnitSpec(c.UnitIdentity(INSTANCE, name, 1), "/opt/foundry/app/bin/" + name, DIGEST,
                               ("--config", paths.config("settings", "recreate")) if name == "relay" else (),
                               c.canonical_digest(original), c.RoleBindings(name, (), (), ()),
                               services.configuration_digest(role, members) if name == "relay" else OTHER))
    deployment = c.Deployment(INSTANCE, "20261113T000000Z-abcdef12", 2, parsed.sha256,
                              c.canonical_digest(profile), tuple(units))
    resolution = c.RuntimeResolution(CURRENT, c.canonical_digest(c.record_document(deployment)), 7,
                                     tuple(role.common.name for role in parsed.roles),
                                     (c.AllocatedEndpoint("http", "http", "127.0.0.1", 8200, 1),
                                      c.AllocatedEndpoint("relay", "control", "127.0.0.1", 8201, 1)),
                                     tuple(c.EffectiveEnvironment(name, "runtime/" + CURRENT + "/" + name + "/app.env", OTHER)
                                           for name in ("http", "relay", "worker")), DIGEST, DIGEST, DIGEST)
    return services.UnitInventory(entry["name"], parsed, placed, deployment, resolution), entry, members


class ServiceInventoryTests(unittest.TestCase):
    def setUp(self):
        self.inventory, self.entry, self.members = inventory_fixture()

    def test_inventory_and_orders_include_builtins_and_custom_singleton(self):
        self.assertEqual(self.inventory.units, {self.inventory.identity("http"): "foundry-http@abc-production.service",
                         self.inventory.identity("relay"): "foundry-app-abc-production-relay@1.service", self.inventory.identity("worker"): "foundry-worker@abc-production.service"})
        self.assertEqual(self.inventory.start_order(self.inventory.select(roles=['http', 'relay', 'worker'])), self.inventory.select(roles=["http", "relay", "worker"]))
        self.assertEqual(self.inventory.stop_order(self.inventory.select(roles=['http', 'relay', 'worker'])), tuple(reversed(self.inventory.select(roles=["http", "relay", "worker"]))))
        with self.assertRaises(c.DeployError):
            self.inventory.start_order(self.inventory.select(roles=['worker']))

    def test_named_operations_reject_transitive_dependents_and_unready_prerequisites(self):
        active = services.RoleState(True, True, False, False, True)
        states = {name: active for name in self.inventory.units}
        for action in ("stop", "restart"):
            with self.assertRaisesRegex(c.DeployError, "worker"):
                self.inventory.named_operation(action, self.inventory.select(roles=['relay']), states)
            with self.assertRaisesRegex(c.DeployError, "relay, worker"):
                self.inventory.named_operation(action, self.inventory.select(roles=['http']), states)
        self.assertEqual(self.inventory.named_operation("stop", self.inventory.select(roles=['worker']), states), self.inventory.copies("worker"))
        for bad in (replace(active, ready=False), replace(active, held=True), replace(active, pending=True),
                    replace(active, enabled=False)):
            with self.subTest(bad=bad), self.assertRaises(c.DeployError):
                self.inventory.named_operation("restart", self.inventory.select(roles=['worker']), {**states, self.inventory.identity("relay"): bad})
        self.assertEqual(self.inventory.named_operation("restart", self.inventory.select(roles=['worker']), states), self.inventory.copies("worker"))
        # Running dependents remain relevant even after external disablement.
        states[self.inventory.identity("worker")] = replace(active, enabled=False)
        with self.assertRaises(c.DeployError):
            self.inventory.named_operation("stop", self.inventory.select(roles=['relay']), states)
        states[self.inventory.identity("worker")] = replace(active, enabled=False, running=False)
        self.assertEqual(self.inventory.named_operation("stop", self.inventory.select(roles=['relay']), states), self.inventory.copies("relay"))

    def test_tool_start_waits_for_each_prerequisite_under_one_deadline(self):
        events = []
        deadline = time.monotonic() + 10
        def start(name, until):
            self.assertEqual(until, deadline)
            events.extend(("start", identity.role) for identity in name)
        def ready(name, until):
            self.assertEqual(until, deadline)
            events.extend(("ready", identity.role) for identity in name)
            return all(identity.role != "relay" for identity in name)
        with self.assertRaises(c.DeployError):
            self.inventory.start_in_order(self.inventory.units, deadline, start, ready)
        self.assertEqual(events, [("start", "http"), ("ready", "http"), ("start", "relay"), ("ready", "relay")])

    def test_custom_render_has_only_fixed_helpers_hardening_limits_and_ordering(self):
        content = services.render_unit(self.inventory, self.inventory.identity("relay"), self.entry, DIGEST)
        for expected in ("Type=exec", "KillMode=control-group", "NoNewPrivileges=yes", "ProtectSystem=strict",
                         "PrivateTmp=yes", "PrivateDevices=yes", "CapabilityBoundingSet=\n", "MemoryMax=268435456",
                         "CPUQuota=100%", "TasksMax=64", "LimitNOFILE=1024", "ExecCondition=+",
                         "ExecStartPre=+", "runtime-v1", "ExecStopPost=+", "foundry-http@abc-production.service"):
            self.assertIn(expected, content)
        for forbidden in ("Requires=", "Wants=", "BindsTo=", "PartOf=", "@ACCOUNT@", "/current/artisan", "sh -c"):
            self.assertNotIn(forbidden, content)
        self.assertNotIn("ExecStart=+", content)
        self.assertIn("User=abc-production", content)

    def test_scalar_unit_paths_are_unquoted_and_word_lists_remain_quoted(self):
        list_directives = {"Environment", "ReadWritePaths", "ReadOnlyPaths", "BindPaths", "BindReadOnlyPaths", "TemporaryFileSystem"}
        for role in self.inventory.units:
            content = services.render_unit(self.inventory, role, self.entry, DIGEST)
            directives = dict(line.split("=", 1) for line in content.splitlines() if "=" in line and not line.startswith("#"))
            for name, value in directives.items():
                if name not in list_directives and not name.startswith("Exec"):
                    self.assertFalse(value.startswith('"'), name)
            self.assertTrue(directives["WorkingDirectory"].startswith("/"))
            if role.role == "relay":
                expected = services.ServicePaths.for_unit(self.inventory.specs[role].identity, CURRENT).base
                self.assertEqual(directives["WorkingDirectory"], expected)
                self.assertTrue(directives["ReadWritePaths"].startswith('"'))
            self.assertTrue(directives["ExecStart"].startswith('"'))

    def test_fixed_runtime_pin_ancestry_is_searchable_under_service_umask(self):
        import importlib.util
        path = Path(__file__).resolve().parents[1] / "server/runtime_v1.py"
        spec = importlib.util.spec_from_file_location("test_runtime_pin_modes", path)
        runtime = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(runtime)
        with tempfile.TemporaryDirectory() as root:
            target = Path(root).resolve() / "pins/instance/role"
            previous = os.umask(0o077)
            try:
                with patch.object(runtime, "owned"), patch.object(runtime.os, "fchown") as owner:
                    runtime.pin_directory(target)
            finally:
                os.umask(previous)
            for directory in (target, target.parent, target.parent.parent):
                self.assertEqual(directory.stat().st_mode & 0o777, 0o755)
            self.assertEqual(owner.call_count, 3)
            self.assertTrue(all(call.args[1:] == (0, 0) for call in owner.call_args_list))

    def test_builtin_guards_reuse_existing_policy(self):
        content = services.render_unit(self.inventory, self.inventory.identity("http"), self.entry, DIGEST)
        self.assertIn("TimeoutStopSec=45s", content)
        self.assertIn("SuccessExitStatus=130", content)
        self.assertIn("ExecCondition=+", content)
        self.assertIn("ExecStartPre=+", content)
        self.assertNotIn("ExecStart=+", content)
        self.assertNotIn("@UPLOAD_WRITE_PATHS@", content)

    def test_argv_is_literal_and_does_not_expand_systemd_or_shell(self):
        self.assertEqual(services.command_line(["", "$HOME ${X}", "%n", 'a"b', "x\\y"]),
                         '"" "$$HOME $${X}" "%%n" "a\\"b" "x\\\\y"')
        self.assertEqual(services.systemd_word("X=$HOME %n"), '"X=$HOME %%n"')
        with self.assertRaises(c.DeployError):
            services.systemd_word("bad\nExecStart=/bin/false")
        spec = self.inventory.specs[self.inventory.identity("relay")]
        self.inventory.specs[self.inventory.identity("relay")] = replace(spec, arguments=("wrong",))
        with self.assertRaises(c.DeployError):
            services.render_unit(self.inventory, self.inventory.identity("relay"), self.entry, DIGEST)

    def test_copies_are_profile_placement_while_wrapper_and_instances_remain_invalid(self):
        raw = self.inventory.topology.projection()
        for field in ("wrapper", "instances"):
            with self.assertRaises(c.DeployError) as error:
                t.parse_topology({**raw, "roles": [raw["roles"][0], {**raw["roles"][1], field: {}}]})
            self.assertEqual(error.exception.code, "unsupported_capability")
        profile = self.inventory.profile.projection()
        profile["placement"]["relay"] = 2
        self.assertEqual(dict(t.parse_profile(profile, self.inventory.topology).placement)["relay"], 2)
        self.assertEqual(c.UnitIdentity.parse({"instance_id": INSTANCE, "role": "relay", "ordinal": 2}).ordinal, 2)

    def test_runtime_entry_refuses_every_action_without_importing_bundle(self):
        path = Path(__file__).resolve().parents[1] / "server/runtime_v1.py"
        spec = importlib.util.spec_from_file_location("runtime_v1", path)
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        for action in ("admit", "prepare", "dispatch", "cleanup", "arbitrary-command"):
            with patch.object(module.sys, "stderr"):
                self.assertNotEqual(0, module.main([action, INSTANCE, "relay", "1", CURRENT, DIGEST, OTHER]))
        self.assertNotEqual(0, module.main([]))

    def test_runtime_entry_dispatches_the_pinned_numbered_copy(self):
        import feature_runtime
        path = Path(__file__).resolve().parents[1] / "server/runtime_v1.py"
        spec = importlib.util.spec_from_file_location("runtime_copy", path)
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        for ordinal in (1, 2, 128):
            invocation = replace(helper("relay"), unit=c.UnitIdentity(INSTANCE, "relay", ordinal))
            pinned = {"helper": c.record_document(invocation), "launch_file": "/launch.json"}
            with patch.dict(os.environ, {"INVOCATION_ID": invocation.process.invocation_id}), \
                    patch.object(module.os, "geteuid", return_value=1000), \
                    patch.object(module, "owned", side_effect=lambda path: path), \
                    patch.object(module, "read", side_effect=[pinned, {"launch": True}]) as read, \
                    patch.object(Path, "read_text", return_value=BOOT), \
                    patch.object(feature_runtime, "run", return_value=0) as run:
                self.assertEqual(module.main(["dispatch", INSTANCE, "relay", str(ordinal), CURRENT, DIGEST, OTHER]), 0)
            self.assertEqual(read.call_args_list[0].args[0], module.PINS / INSTANCE / invocation.unit.record_key / (invocation.process.invocation_id + ".json"))
            self.assertEqual(run.call_args.args, ("dispatch", c.record_document(invocation), {"launch": True}))

    def test_retained_dispatch_uses_prepared_view_without_private_admission_reads(self):
        import feature_runtime
        invocation = helper("relay", spec=c.canonical_digest(c.record_document(self.inventory.specs[self.inventory.identity("relay")])))
        launch = {"deployment": c.record_document(self.inventory.deployment),
                  "resolution": c.record_document(self.inventory.resolution),
                  "topology": self.inventory.topology.projection(), "profile": self.inventory.profile.projection()}
        entry = {**self.entry, "profile": self.inventory.profile.projection()}
        entry["profile"]["placement"]["relay"] = 3
        host = MagicMock()
        host.read_registry.return_value = {"instances": {INSTANCE: entry}}
        with patch.object(admission, "AdmissionStore", side_effect=PermissionError("private")):
            selected = feature_runtime.selected(host, c.record_document(invocation), launch, dispatch=True)
            self.assertEqual(selected[3], self.inventory.specs[invocation.unit])
            with self.assertRaises(PermissionError):
                feature_runtime.selected(host, c.record_document(invocation), launch)

    def test_probe_is_local_bounded_and_never_follows_redirects(self):
        role = self.inventory.roles["relay"]
        connection = MagicMock()
        response = connection.getresponse.return_value
        response.status = 302
        with patch.object(services, "_probe_socket"), patch.object(services.http.client, "HTTPConnection", return_value=connection) as client:
            self.assertFalse(services.probe(Host(), self.inventory, self.inventory.identity(role.common.name), time.monotonic() + 1))
            self.assertEqual(client.call_args.args, ("127.0.0.1", 8201))
            self.assertEqual(connection.request.call_args.args[:2], ("GET", "/ready"))
            response.status, response.read.return_value = 200, b"ok"
            self.assertTrue(services.probe(Host(), self.inventory, self.inventory.identity(role.common.name), time.monotonic() + 1))
            response.read.return_value = b"x" * 65537
            self.assertFalse(services.probe(Host(), self.inventory, self.inventory.identity(role.common.name), time.monotonic() + 1))
        self.assertFalse(services.probe(Host(), self.inventory, self.inventory.identity(role.common.name), time.monotonic() - 1))

    def test_paused_http_uses_liveness_without_weakening_normal_readiness(self):
        role = self.inventory.roles["http"]
        connection = MagicMock()
        response = connection.getresponse.return_value
        response.status, response.read.return_value = 200, b'{"status":"ok"}'
        with patch.object(services, "_probe_socket"), patch.object(services.http.client, "HTTPConnection", return_value=connection):
            self.assertTrue(services.probe(Host(), self.inventory, self.inventory.identity(role.common.name), time.monotonic() + 1, paused=True))
            self.assertEqual(connection.request.call_args.args[:2], ("GET", "/health/live"))
            self.assertFalse(services.probe(Host(), self.inventory, self.inventory.identity(role.common.name), time.monotonic() + 1))
            self.assertEqual(connection.request.call_args.args[:2], ("GET", "/health/ready"))

    def test_maintenance_response_requires_shared_pause_and_builtin_http(self):
        connection = MagicMock()
        response = connection.getresponse.return_value
        response.read.return_value = b'{"outcome":"rejected"}'
        with patch.object(services, "_probe_socket"), patch.object(services.http.client, "HTTPConnection", return_value=connection):
            for status in (503, 500, 302):
                response.status = status
                self.assertEqual(services.probe(Host(), self.inventory, self.inventory.identity("http"),
                                               time.monotonic() + 1, paused=True), status == 503)
                self.assertFalse(services.probe(Host(), self.inventory, self.inventory.identity("http"), time.monotonic() + 1))
                self.assertFalse(services.probe(Host(), self.inventory, self.inventory.identity("relay"),
                                                time.monotonic() + 1, paused=True))

    def test_release_carried_bundle_keeps_payload_digest_and_platform_checks(self):
        raw = declaration()
        raw["artifacts"] = {"extension": bundle()}
        raw["artifacts"]["extension"]["platforms"]["linux-arm64"] = {
            "source": {"release": {"path": "deploy/artifacts/extension.tar.gz"}}}
        raw["roles"][1]["artifact_mounts"] = {"plugin": {"artifact": "extension", "bundle": "plugin",
                                                          "area": "config", "path": "plugins/extension"}}
        topology = t.parse_topology(raw)
        role = topology.roles[1]
        paths = services.ServicePaths.for_unit(c.UnitIdentity(INSTANCE, "relay", 1), CURRENT)
        payload = c.ArtifactPayload("extension", "linux-arm64", "deploy/artifacts/extension.tar.gz", OTHER,
                                   (c.PayloadMember("plugin/extension.so", DIGEST, 6, "module"),))
        roots = {"extension": ("/opt/foundry/pinned", OTHER)}
        self.assertEqual(services.bundle_views(role, paths, topology, "arm64", roots, (payload,))[0].artifact_sha256, OTHER)
        for bad in (replace(payload, sha256=DIGEST), replace(payload, platform="linux-amd64")):
            with self.assertRaises(c.DeployError):
                services.bundle_views(role, paths, topology, "arm64", roots, (bad,))

    def test_bundle_views_select_exact_architecture_and_confined_export(self):
        raw = declaration()
        raw["artifacts"] = {"extension": bundle()}
        raw["roles"][1]["artifact_mounts"] = {"plugin": {"artifact": "extension", "bundle": "plugin",
                                                          "area": "config", "path": "plugins/extension"}}
        topology = t.parse_topology(raw)
        role = topology.roles[1]
        paths = services.ServicePaths.for_unit(c.UnitIdentity(INSTANCE, "relay", 1), CURRENT)
        payload = c.ArtifactPayload("extension", "linux-arm64", "deploy/artifacts/extension.tar.gz", OTHER,
                                   (c.PayloadMember("plugin/extension.so", DIGEST, 6, "module"),
                                    c.PayloadMember("plugin/data.txt", OTHER, 9, "data")))
        views = services.bundle_views(role, paths, topology, "arm64", {"extension": ("/opt/foundry/pinned", OTHER)}, (payload,))
        self.assertEqual(views[0].source, "/opt/foundry/pinned/plugin")
        self.assertEqual(views[0].destination, paths.base + "/view/config/plugins/extension")
        self.assertEqual([member.path for member in views[0].members], ["extension.so", "data.txt"])
        with self.assertRaises(c.DeployError):
            services.bundle_views(role, paths, topology, "amd64", {"extension": ("/opt/foundry/pinned", OTHER)}, (payload,))
        with self.assertRaises(c.DeployError):
            services.validate_views(role, paths, (replace(views[0], destination="/usr/lib/plugins"),))

    def test_readiness_rejects_foreign_socket_restart_and_changed_process(self):
        import release
        with tempfile.TemporaryDirectory(dir=Path(tempfile.gettempdir()).resolve()) as root:
            host = Host(Paths(root))
            spec = self.inventory.specs[self.inventory.identity("relay")]
            executable = host.paths.p(spec.executable)
            executable.parent.mkdir(parents=True)
            executable.write_bytes(b"native")
            executable.chmod(0o755)
            self.inventory.specs[self.inventory.identity("relay")] = replace(spec, executable_sha256=hashlib.sha256(b"native").hexdigest())
            observed = {"LoadState": "loaded", "ActiveState": "active", "NRestarts": "0", "MainPID": "20",
                        "ControlGroup": "/unit", "process": {"start": "42"}}
            def wait():
                return services.wait_ready(host, self.inventory, self.inventory.identity("relay"), time.monotonic() + .5)
            with patch.object(release, "service", return_value=observed) as inspect, \
                    patch.object(release, "_matches_executable", return_value=True), \
                    patch.object(release, "STABILITY_SECONDS", 0), \
                    patch.object(host, "run", return_value=subprocess.CompletedProcess([], 0,
                        'LISTEN 0 128 127.0.0.1:8201 0.0.0.0:* users:(("relay",pid=20,fd=3))', '')) as sockets, \
                    patch.object(services, "probe", return_value=True):
                self.assertTrue(wait())
                observed["NRestarts"] = "1"
                self.assertFalse(wait())
                observed["NRestarts"] = "0"
                sockets.return_value = subprocess.CompletedProcess([], 0,
                        'LISTEN 0 128 127.0.0.1:8201 0.0.0.0:* users:(("other",pid=21,fd=3))', '')
                self.assertFalse(wait())
                sockets.return_value = subprocess.CompletedProcess([], 0,
                        'LISTEN 0 128 127.0.0.1:8201 0.0.0.0:* users:(("relay",pid=20,fd=3))', '')
                inspect.side_effect = [observed, {**observed, "process": {"start": "43"}}]
                with patch.object(release, "STABILITY_SECONDS", .2):
                    self.assertFalse(wait())

    def test_numbered_unix_socket_remains_literal_in_argv_and_ss_readiness(self):
        import release
        from test_copies import numbered_inventory
        inventory, _, _ = numbered_inventory(128)
        identity = inventory.identity("relay", 128)
        endpoint = c.UnixEndpoint.for_listener(INSTANCE, "relay", "control", "x" * 32, ordinal=128)
        inventory.resolution = replace(inventory.resolution, unix_endpoints=(endpoint,), endpoints=())
        inventory.roles["relay"] = replace(inventory.roles["relay"],
            readiness=t.Probe.parse({"kind": "unix", "listener": "control"}))
        self.assertEqual(services.command_line([endpoint.path]), '"' + endpoint.path + '"')
        with tempfile.TemporaryDirectory() as root:
            host = Host(Paths(Path(root).resolve()))
            spec = inventory.specs[identity]
            executable = host.paths.p(spec.executable)
            executable.parent.mkdir(parents=True)
            executable.write_bytes(b"native")
            executable.chmod(0o755)
            inventory.specs[identity] = replace(spec, executable_sha256=c.digest(executable))
            observed = {"ActiveState": "active", "NRestarts": "0", "MainPID": "20", "process": {"start": "42"}}
            with patch.object(release, "service", return_value=observed), \
                    patch.object(release, "_matches_executable", return_value=True), \
                    patch.object(release, "STABILITY_SECONDS", 0), \
                    patch.object(services, "probe", return_value=True), \
                    patch.object(host, "run", return_value=subprocess.CompletedProcess([], 0,
                        'u_str LISTEN 0 128 ' + endpoint.path + ' 123 * 0 users:(("relay",pid=20,fd=3))', '')) as sockets:
                self.assertTrue(services.wait_ready(host, inventory, identity, time.monotonic() + .5))
            self.assertEqual(sockets.call_args.args[0], ["ss", "-Hxlpn"])

    def test_grouped_start_keeps_each_units_timeout_and_stability_proof(self):
        import feature_lifecycle as lifecycle
        import release
        from types import SimpleNamespace
        inventory = self.inventory
        short, slow = inventory.identity("http"), inventory.identity("relay")
        inventory.roles = {name: replace(role, common=replace(role.common, requires=()))
                           for name, role in inventory.roles.items()}
        inventory.resolution = replace(inventory.resolution, endpoints=())
        with tempfile.TemporaryDirectory() as root:
            host = Host(Paths(Path(root).resolve()))
            for identity, spec in inventory.specs.items():
                path = host.paths.p(spec.executable)
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_bytes(b"native")
                path.chmod(0o755)
                inventory.specs[identity] = replace(spec, executable_sha256=c.digest(path))
            for slow_start, slow_ready, failure in ((0, .45, None), (.6, 1.25, None),
                    (0, 1.3, "timeout"), (0, .45, "peer_identity"), (0, .45, "peer_probe")):
                with self.subTest(slow_start=slow_start, slow_ready=slow_ready, failure=failure):
                    began, observed = time.monotonic(), set()
                    def inspect(host, unit, **kwargs):
                        start = began + (0 if unit == inventory.units[short] else slow_start)
                        pid = "99" if failure == "peer_identity" and unit == inventory.units[short] and time.monotonic() - began >= .35 else "20"
                        return {"ActiveState": "active", "NRestarts": "0", "MainPID": pid,
                                "process": {"start": "42"},
                                "ExecMainStartTimestampMonotonic": str(int(start * 1_000_000)) if time.monotonic() >= start else "0"}
                    def probe(host, inventory, identity, deadline):
                        observed.add(identity)
                        return (time.monotonic() - began < .35 if failure == "peer_probe" else True) if identity == short else time.monotonic() - began >= slow_ready
                    def contract(role):
                        return SimpleNamespace(timeout_seconds=.4 if role.common.name == "http" else 1.2, listener="unused")
                    with patch.object(lifecycle, "prepare_shared_directories"), \
                            patch.object(services, "enabled_units", return_value=tuple(inventory.units)), \
                            patch.object(host, "read_registry", return_value={"instances": {}}), \
                            patch.object(host, "run", return_value=subprocess.CompletedProcess([], 0, "", "")) as commands, \
                            patch.object(release, "service", side_effect=inspect), \
                            patch.object(release, "_matches_executable", return_value=True), \
                            patch.object(release, "STABILITY_SECONDS", .1), \
                            patch.object(services, "readiness_probe", side_effect=contract), \
                            patch.object(services, "probe", side_effect=probe):
                        if failure:
                            with self.assertRaises(c.DeployError):
                                lifecycle.start_units(host, self.entry, inventory, (short, slow), began + 3)
                        else:
                            lifecycle.start_units(host, self.entry, inventory, (short, slow), began + 3)
                            self.assertGreaterEqual(time.monotonic() - began, slow_ready + .1)
                        self.assertEqual(commands.call_args_list[0].args[0],
                            ["systemctl", "start", "--no-block", inventory.units[short], inventory.units[slow]])
                        self.assertEqual(observed, {short, slow})

    def test_complete_readiness_set_rechecks_prerequisite_after_dependent_probe(self):
        import release
        with tempfile.TemporaryDirectory(dir=Path(tempfile.gettempdir()).resolve()) as root:
            host = Host(Paths(root))
            for name, spec in self.inventory.specs.items():
                path = host.paths.p(spec.executable)
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_bytes(b"native")
                path.chmod(0o755)
                self.inventory.specs[name] = replace(spec, executable_sha256=c.digest(path))
            for failure in ("identity", "probe", "restart", None):
                with self.subTest(failure=failure):
                    observed = {"ActiveState": "active", "NRestarts": "0", "MainPID": "20", "process": {"start": "42"}}
                    dependent_probed = False
                    def inspect(host, unit, **kwargs):
                        if unit == self.inventory.units[self.inventory.identity("http")] and dependent_probed:
                            if failure == "identity":
                                return {**observed, "MainPID": "21"}
                            if failure == "restart":
                                return {**observed, "NRestarts": "1"}
                        return observed
                    def probe(host, inventory, name, deadline):
                        role = inventory.roles[name.role]
                        nonlocal dependent_probed
                        if role.common.name == "worker":
                            dependent_probed = True
                        return not (failure == "probe" and dependent_probed and role.common.name == "http")
                    with patch.object(host, "read_registry", return_value={"instances": {}}), \
                            patch.object(release, "service", side_effect=inspect), \
                            patch.object(release, "_matches_executable", return_value=True), \
                            patch.object(release, "STABILITY_SECONDS", .02), \
                            patch.object(services, "readiness_probe", return_value=None), \
                            patch.object(services, "probe", side_effect=probe):
                        result = services.wait_ready_set(host, self.inventory, self.inventory.units, time.monotonic() + .25)
                    self.assertEqual(result, failure is None)
                    self.assertTrue(dependent_probed)


class ServicePreparationTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory(dir=Path(tempfile.gettempdir()).resolve())
        self.addCleanup(temporary.cleanup)
        self.host = Host(Paths(temporary.name))
        self.inventory, self.entry, self.members = inventory_fixture()
        import subprocess
        entry = {**self.entry, "topology": t.validate_topology(self.inventory.topology.projection()),
                 "profile": self.inventory.profile.projection()}
        registry = patch.object(self.host, "read_registry", return_value={"instances": {INSTANCE: entry}})
        registry.start()
        self.addCleanup(registry.stop)
        command = patch.object(self.host, "run", return_value=subprocess.CompletedProcess([], 0, "enabled", ""))
        command.start()
        self.addCleanup(command.stop)
        self.store = admission.AdmissionStore(self.host, INSTANCE)
        boot = self.host.paths.p("/proc/sys/kernel/random/boot_id")
        boot.parent.mkdir(parents=True)
        boot.write_text(BOOT)
        selected = selection()
        selected["enabled_roles"] = list(self.inventory.roles)
        selected["admission"]["deployment_sha256"] = self.inventory.resolution.deployment_sha256
        self.store.record_operation(attempt())
        self.host.owned_write(self.host.paths.opt(self.entry["name"]) / "runtime" / self.inventory.resolution.id / "launch.json",
                              json.dumps({"deployment": c.record_document(self.inventory.deployment)}), mode=0o640)
        self.store.begin(c.DeploymentSelection.parse(selected), self.inventory.resolution)
        evidence = closure()
        evidence["units"] = [{"instance_id": INSTANCE, "role": name, "ordinal": 1} for name in self.inventory.roles]
        from dataclasses import asdict
        evidence["endpoints"] = [asdict(e) for e in self.inventory.resolution.endpoints]
        self.store.record_boot_closure(c.ClosureEvidence.parse(evidence))
        self.store.record_operation(attempt("succeeded"))
        self.store.commit()
        self.role, self.spec = self.inventory.roles["relay"], self.inventory.specs[self.inventory.identity("relay")]
        self.paths = services.ServicePaths.for_unit(self.spec.identity, CURRENT)
        self.root = self.host.paths.p("/opt/foundry/release")
        for name, data in (("relay.json", CONFIG), ("seed.txt", b"seed"), ("fixed.txt", b"fixed")):
            path = self.root / "deploy/files" / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(data)
        self.invocation = helper("relay", spec=c.canonical_digest(c.record_document(self.spec)))

    def prepare(self, invocation=None, **kwargs):
        invocation = invocation or self.invocation
        self.store.admit(invocation)
        return runtime_config.prepare_service(self.host, self.store, invocation, self.role, self.spec,
                    self.root, self.members, uid=os.geteuid(), gid=os.getegid(), **kwargs)

    def test_private_unit_umask_preserves_searchable_config_ancestry(self):
        previous = os.umask(0o077)
        try:
            view = self.prepare()
        finally:
            os.umask(previous)
        for directory in (view, view.parent, view.parent.parent, view / "config", view / "config/fixed"):
            self.assertEqual(directory.stat().st_mode & 0o777, 0o755)
            self.assertEqual(directory.stat().st_uid, os.geteuid())
        for directory in (view / "runtime", view / "config/settings",
                          self.host.paths.p(self.paths.state) / "seeds/seed"):
            self.assertEqual(directory.stat().st_mode & 0o777, 0o700)
            self.assertEqual(directory.stat().st_uid, os.geteuid())
        self.assertEqual((view / "config/fixed/value").read_bytes(), b"fixed")
        self.assertEqual((view / "config/settings/value").stat().st_mode & 0o777, 0o600)

    def test_each_invocation_recreates_configuration_but_seed_once_keeps_edits(self):
        first = self.prepare()
        actual = json.loads((first / "config/settings/value").read_text())
        self.assertEqual(actual["listen"], {"address": "127.0.0.1", "port": 8201})
        (first / "config/settings/value").write_text("edited")
        seed = self.host.paths.p(self.paths.config("seed", "seed-once"))
        seed.write_text("retained")
        next_start = replace(self.invocation, process=replace(self.invocation.process, invocation_id="e" * 32))
        second = self.prepare(next_start)
        self.assertNotEqual(first, second)
        self.assertEqual(json.loads((second / "config/settings/value").read_text()), actual)
        self.assertEqual(seed.read_text(), "retained")
        self.assertEqual((first / "config/settings/value").read_text(), "edited")
        self.assertEqual((second / "config/fixed/value").stat().st_mode & 0o777, 0o444)
        self.assertEqual((second / "baselines/settings").stat().st_mode & 0o777, 0o444)
        self.assertEqual((second / "config/settings/value").stat().st_mode & 0o777, 0o600)
        self.assertEqual(os.readlink(self.host.paths.p(self.paths.base) / "current"), "invocations/" + "e" * 32)
        self.assertEqual(self.prepare(next_start), second)

    def test_numbered_copies_have_independent_config_views_seeds_and_admission(self):
        from test_copies import numbered_inventory
        inventory, entry, members = numbered_inventory()
        entry = {**entry, "profile": inventory.profile.projection(), "topology": t.validate_topology(inventory.topology.projection())}
        self.host.read_registry.return_value = {"instances": {INSTANCE: entry}}
        selected, _ = self.store._selection()
        selected = replace(selected, minimum_generation=18,
                           enabled_roles=unit_selection_names(inventory.units),
                           admission=replace(selected.admission, disposition=c.AdmissionDisposition.PENDING,
                               deployment_sha256=inventory.resolution.deployment_sha256))
        self.store.record_operation(attempt())
        self.host.owned_write(self.host.paths.opt(entry["name"]) / "runtime" / inventory.resolution.id / "launch.json",
                              json.dumps({"deployment": c.record_document(inventory.deployment)}), mode=0o640)
        self.store.begin(selected, inventory.resolution)
        evidence = c.ClosureEvidence.parse(closure())
        self.store.record_boot_closure(replace(evidence, units=tuple(inventory.units), endpoints=inventory.resolution.endpoints))
        self.store.record_operation(attempt("succeeded"))
        self.store.commit()
        seeds, views, receipts = [], [], []
        for identity in inventory.copies("relay"):
            spec = inventory.specs[identity]
            invocation = replace(helper("relay", spec=c.canonical_digest(c.record_document(spec))), unit=identity)
            paths = services.ServicePaths.for_unit(identity, inventory.resolution.id)
            self.store.admit(invocation)
            view = runtime_config.prepare_service(self.host, self.store, invocation, inventory.roles[identity.role], spec,
                self.root, members, uid=os.geteuid(), gid=os.getegid())
            port = next(e.port for e in inventory.resolution.endpoints if (e.role, e.ordinal) == (identity.role, identity.ordinal))
            self.assertEqual(json.loads((view / "config/settings/value").read_text())["listen"]["port"], port)
            seed = self.host.paths.p(paths.config("seed", "seed-once"))
            seed.write_text(str(identity.ordinal))
            seeds.append(seed)
            views.append(view)
            receipts.append(self.store._receipt(invocation))
        self.assertEqual(len(set(views)), 3)
        self.assertEqual(len(set(receipts)), 3)
        self.assertEqual([seed.read_text() for seed in seeds], ["1", "2", "3"])

    def test_denied_start_does_not_prepare_any_files(self):
        self.store.record_operation(attempt(operation=OPERATION))
        with self.assertRaises(c.DeployError):
            self.prepare()
        self.assertFalse(self.host.paths.p(self.paths.base).exists())

    def test_preload_skeleton_exists_without_preparing_or_admitting_a_start(self):
        runtime_config.prepare_unit_paths(self.host, self.role, self.spec, self.inventory.resolution)
        for suffix in ("invocations", "view/config", "view/data"):
            path = self.host.paths.p(self.paths.base) / suffix
            self.assertTrue(path.is_dir())
            self.assertFalse(path.stat().st_mode & 0o022)
        self.assertFalse((self.host.paths.p(self.paths.base) / "current").exists())
        self.assertFalse((self.store.runtime / "admitted").exists())

    def test_failed_source_does_not_publish_or_reuse_partial_output(self):
        path = self.root / "deploy/files/relay.json"
        path.write_text("tampered")
        with self.assertRaises(c.DeployError):
            self.prepare()
        self.assertFalse((self.host.paths.p(self.paths.base) / "current").is_symlink())
        path.write_bytes(CONFIG)
        with self.assertRaises(c.DeployError) as error:
            self.prepare()
        self.assertEqual(error.exception.code, "service_preparation_retained")

    def test_seed_symlink_never_overwrites_target(self):
        self.prepare()
        seed = self.host.paths.p(self.paths.config("seed", "seed-once"))
        target = seed.with_name("outside")
        target.write_text("preserved")
        seed.unlink()
        seed.symlink_to(target)
        next_start = replace(self.invocation, process=replace(self.invocation.process, invocation_id="e" * 32))
        with self.assertRaises(c.DeployError):
            self.prepare(next_start)
        self.assertEqual(target.read_text(), "preserved")
        self.assertEqual(os.readlink(self.host.paths.p(self.paths.base) / "current"), "invocations/" + self.invocation.process.invocation_id)

    def test_retained_views_are_bounded_without_deleting_live_views(self):
        with patch.object(runtime_config, "MAX_SERVICE_VIEWS", 1):
            first = self.prepare()
            next_start = replace(self.invocation, process=replace(self.invocation.process, invocation_id="e" * 32))
            with self.assertRaises(c.DeployError):
                self.prepare(next_start)
            self.assertTrue(first.exists())

    def test_bundle_mountpoints_are_protected_and_tampering_is_rejected(self):
        root = self.host.paths.p("/opt/foundry/artifacts/extension")
        root.mkdir(parents=True)
        module, data = root / "module.so", root / "data.txt"
        module.write_bytes(b"module")
        data.write_bytes(b"companion")
        members = tuple(c.PayloadMember(p.name, hashlib.sha256(p.read_bytes()).hexdigest(), p.stat().st_size,
                                       "module" if p == module else "data") for p in (module, data))
        view = services.BundleView("extension", "/opt/foundry/artifacts/extension", self.paths.base + "/view/config/plugins/extension", DIGEST, members)
        runtime_config.prepare_bundle_views(self.host, (view,))
        destination = self.host.paths.p(view.destination)
        self.assertEqual(destination.stat().st_mode & 0o777, 0o755)
        self.assertFalse(destination.stat().st_mode & 0o022)
        self.assertFalse(list(destination.iterdir()))  # Namespace binding, never a writable copy.
        for mutate in (lambda: data.write_bytes(b"changed"), lambda: data.unlink(), lambda: data.chmod(0o666)):
            data.write_bytes(b"companion")
            mutate()
            with self.assertRaises(c.DeployError):
                runtime_config.prepare_bundle_views(self.host, (view,))
        data.chmod(0o644)
        data.unlink()
        data.symlink_to(module)
        with self.assertRaises(c.DeployError):
            runtime_config.prepare_bundle_views(self.host, (view,))


if __name__ == "__main__":
    unittest.main()
