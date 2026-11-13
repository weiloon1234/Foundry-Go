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
                                     (c.AllocatedEndpoint("http", "http", "127.0.0.1", 8200),
                                      c.AllocatedEndpoint("relay", "control", "127.0.0.1", 8201)),
                                     tuple(c.EffectiveEnvironment(name, "runtime/" + CURRENT + "/" + name + "/app.env", OTHER)
                                           for name in ("http", "relay", "worker")), DIGEST, DIGEST, DIGEST)
    return services.UnitInventory(entry["name"], parsed, placed, deployment, resolution), entry, members


class ServiceInventoryTests(unittest.TestCase):
    def setUp(self):
        self.inventory, self.entry, self.members = inventory_fixture()

    def test_inventory_and_orders_include_builtins_and_custom_singleton(self):
        self.assertEqual(self.inventory.units, {"http": "foundry-http@abc-production.service",
                         "relay": "foundry-app-abc-production-relay@1.service", "worker": "foundry-worker@abc-production.service"})
        self.assertEqual(self.inventory.start_order({"http", "relay", "worker"}), ("http", "relay", "worker"))
        self.assertEqual(self.inventory.stop_order({"http", "relay", "worker"}), ("worker", "relay", "http"))
        with self.assertRaises(c.DeployError):
            self.inventory.start_order({"worker"})

    def test_named_operations_reject_transitive_dependents_and_unready_prerequisites(self):
        active = services.RoleState(True, True, False, False, True)
        states = {name: active for name in self.inventory.roles}
        for action in ("stop", "restart"):
            with self.assertRaisesRegex(c.DeployError, "worker"):
                self.inventory.named_operation(action, {"relay"}, states)
            with self.assertRaisesRegex(c.DeployError, "relay, worker"):
                self.inventory.named_operation(action, {"http"}, states)
        self.assertEqual(self.inventory.named_operation("stop", {"worker"}, states), ("worker",))
        for bad in (replace(active, ready=False), replace(active, held=True), replace(active, pending=True),
                    replace(active, enabled=False)):
            with self.subTest(bad=bad), self.assertRaises(c.DeployError):
                self.inventory.named_operation("restart", {"worker"}, {**states, "relay": bad})
        self.assertEqual(self.inventory.named_operation("restart", {"worker"}, states), ("worker",))
        # Running dependents remain relevant even after external disablement.
        states["worker"] = replace(active, enabled=False)
        with self.assertRaises(c.DeployError):
            self.inventory.named_operation("stop", {"relay"}, states)
        states["worker"] = replace(active, enabled=False, running=False)
        self.assertEqual(self.inventory.named_operation("stop", {"relay"}, states), ("relay",))

    def test_tool_start_waits_for_each_prerequisite_under_one_deadline(self):
        events = []
        deadline = time.monotonic() + 10
        def start(name, until):
            self.assertEqual(until, deadline)
            events.append(("start", name))
        def ready(name, until):
            self.assertEqual(until, deadline)
            events.append(("ready", name))
            return name != "relay"
        with self.assertRaises(c.DeployError):
            self.inventory.start_in_order(self.inventory.roles, deadline, start, ready)
        self.assertEqual(events, [("start", "http"), ("ready", "http"), ("start", "relay"), ("ready", "relay")])

    def test_custom_render_has_only_fixed_helpers_hardening_limits_and_ordering(self):
        content = services.render_unit(self.inventory, "relay", self.entry, DIGEST)
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
        list_directives = {"Environment", "ReadWritePaths", "BindReadOnlyPaths"}
        for role in self.inventory.roles:
            content = services.render_unit(self.inventory, role, self.entry, DIGEST)
            directives = dict(line.split("=", 1) for line in content.splitlines() if "=" in line and not line.startswith("#"))
            for name, value in directives.items():
                if name not in list_directives and not name.startswith("Exec"):
                    self.assertFalse(value.startswith('"'), name)
            self.assertTrue(directives["WorkingDirectory"].startswith("/"))
            if role == "relay":
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
        content = services.render_unit(self.inventory, "http", self.entry, DIGEST)
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
        spec = self.inventory.specs["relay"]
        self.inventory.specs["relay"] = replace(spec, arguments=("wrong",))
        with self.assertRaises(c.DeployError):
            services.render_unit(self.inventory, "relay", self.entry, DIGEST)

    def test_release2_capabilities_remain_rejected(self):
        raw = self.inventory.topology.projection()
        for field in ("wrapper", "instances"):
            with self.assertRaises(c.DeployError) as error:
                t.parse_topology({**raw, "roles": [raw["roles"][0], {**raw["roles"][1], field: {}}]})
            self.assertEqual(error.exception.code, "unsupported_capability")
        profile = self.inventory.profile.projection()
        profile["placement"]["relay"] = 2
        with self.assertRaises(c.DeployError) as error:
            t.parse_profile(profile, self.inventory.topology)
        self.assertEqual(error.exception.code, "unsupported_capability")
        with self.assertRaises(c.DeployError):
            c.UnitIdentity.parse({"instance_id": INSTANCE, "role": "relay", "ordinal": 2})

    def test_runtime_entry_refuses_every_action_without_importing_bundle(self):
        path = Path(__file__).resolve().parents[1] / "server/runtime_v1.py"
        spec = importlib.util.spec_from_file_location("runtime_v1", path)
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        for action in ("admit", "prepare", "dispatch", "cleanup", "arbitrary-command"):
            with patch.object(module.sys, "stderr"):
                self.assertNotEqual(0, module.main([action, INSTANCE, "relay", "1", CURRENT, DIGEST, OTHER]))
        self.assertNotEqual(0, module.main([]))

    def test_probe_is_local_bounded_and_never_follows_redirects(self):
        role = self.inventory.roles["relay"]
        connection = MagicMock()
        response = connection.getresponse.return_value
        response.status = 302
        with patch.object(services.http.client, "HTTPConnection", return_value=connection) as client:
            self.assertFalse(services.probe(role, self.inventory.resolution, time.monotonic() + 1))
            self.assertEqual(client.call_args.args, ("127.0.0.1", 8201))
            self.assertEqual(connection.request.call_args.args[:2], ("GET", "/ready"))
            response.status, response.read.return_value = 200, b"ok"
            self.assertTrue(services.probe(role, self.inventory.resolution, time.monotonic() + 1))
            response.read.return_value = b"x" * 65537
            self.assertFalse(services.probe(role, self.inventory.resolution, time.monotonic() + 1))
        self.assertFalse(services.probe(role, self.inventory.resolution, time.monotonic() - 1))

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
            spec = self.inventory.specs["relay"]
            executable = host.paths.p(spec.executable)
            executable.parent.mkdir(parents=True)
            executable.write_bytes(b"native")
            executable.chmod(0o755)
            self.inventory.specs["relay"] = replace(spec, executable_sha256=hashlib.sha256(b"native").hexdigest())
            observed = {"LoadState": "loaded", "ActiveState": "active", "NRestarts": "0", "MainPID": "20",
                        "ControlGroup": "/unit", "process": {"start": "42"}}
            def wait():
                return services.wait_ready(host, self.inventory, "relay", time.monotonic() + .5)
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
                        if unit == self.inventory.units["http"] and dependent_probed:
                            if failure == "identity":
                                return {**observed, "MainPID": "21"}
                            if failure == "restart":
                                return {**observed, "NRestarts": "1"}
                        return observed
                    def probe(role, resolution, deadline):
                        nonlocal dependent_probed
                        if role.common.name == "worker":
                            dependent_probed = True
                        return not (failure == "probe" and dependent_probed and role.common.name == "http")
                    with patch.object(release, "service", side_effect=inspect), \
                            patch.object(release, "_matches_executable", return_value=True), \
                            patch.object(release, "STABILITY_SECONDS", .02), \
                            patch.object(services, "readiness_probe", return_value=None), \
                            patch.object(services, "probe", side_effect=probe):
                        result = services.wait_ready_set(host, self.inventory, self.inventory.roles, time.monotonic() + .25)
                    self.assertEqual(result, failure is None)
                    self.assertTrue(dependent_probed)


class ServicePreparationTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory(dir=Path(tempfile.gettempdir()).resolve())
        self.addCleanup(temporary.cleanup)
        self.host = Host(Paths(temporary.name))
        self.inventory, self.entry, self.members = inventory_fixture()
        self.store = admission.AdmissionStore(self.host, INSTANCE)
        boot = self.host.paths.p("/proc/sys/kernel/random/boot_id")
        boot.parent.mkdir(parents=True)
        boot.write_text(BOOT)
        selected = selection()
        selected["enabled_roles"] = list(self.inventory.roles)
        selected["admission"]["deployment_sha256"] = self.inventory.resolution.deployment_sha256
        self.store.record_operation(attempt())
        self.store.begin(c.DeploymentSelection.parse(selected), self.inventory.resolution)
        evidence = closure()
        evidence["units"] = [{"instance_id": INSTANCE, "role": name, "ordinal": 1} for name in self.inventory.roles]
        from dataclasses import asdict
        evidence["endpoints"] = [asdict(e) for e in self.inventory.resolution.endpoints]
        self.store.record_boot_closure(c.ClosureEvidence.parse(evidence))
        self.store.record_operation(attempt("succeeded"))
        self.store.commit()
        self.role, self.spec = self.inventory.roles["relay"], self.inventory.specs["relay"]
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
