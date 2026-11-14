"""Preview boundaries and final binding delivery; never invokes host services."""
from dataclasses import replace
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch, Mock
import sys

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "lib"))
import contracts as c
import feature_lifecycle as lifecycle
import feature_network as network
import runtime_config
import services
import upgrades
from host import Host, Paths
from test_services import inventory_fixture
from test_host_capabilities import CURRENT, OPERATION, closure


class FeaturePreviewTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.host = Host(Paths(Path(self.temporary.name)))
        self.inventory, self.entry, _ = inventory_fixture()
        self.entry["configuration_revision"] = 7

    def test_effective_environment_preserves_secrets_and_selects_exact_http_port(self):
        root = self.host.paths.instance_config(self.entry["name"])
        root.mkdir(parents=True)
        original = 'APP__HTTP__SERVER__ADDRESS=127.0.0.1:8100\nPRIVATE=keep-current\nKEY_FILE=/etc/secret\nAPP__SCHEDULER__ENABLED=true\nAPP__SCHEDULER__ENABLED_FILE=/old/scheduler\n'
        (root / "app.env").write_text(original)
        endpoints = (c.AllocatedEndpoint("http", "http", "127.0.0.1", 8200),)
        with patch.object(runtime_config, "validate_runtime_environment"):
            environments = runtime_config.effective_environments(self.host, self.entry, CURRENT, ("http",), endpoints)
        resolution = replace(self.inventory.resolution, environments=environments)
        path = runtime_config.effective_environment_path(self.host, self.entry, resolution)
        values = c.parse_env(path.read_text())
        self.assertEqual(values["APP__SCHEDULER__ENABLED"], "false")
        self.assertNotIn("APP__SCHEDULER__ENABLED_FILE", values)
        self.assertEqual(values["PRIVATE"], "keep-current")
        self.assertEqual(values["KEY_FILE"], "/etc/secret")
        self.assertEqual(values["APP__HTTP__SERVER__ADDRESS"], "127.0.0.1:8200")
        self.assertEqual(path.stat().st_mode & 0o777, 0o640)
        self.assertEqual((root / "app.env").read_text(), original)
        path.write_text("changed")
        with self.assertRaises(c.DeployError):
            runtime_config.effective_environment_path(self.host, self.entry, resolution)

    def test_descriptor_contains_final_frozen_bindings_but_no_environment_hash_or_values(self):
        spec = self.inventory.specs["http"]
        root = self.host.paths.instance_config(self.entry["name"])
        env = root / ("runtime/" + CURRENT + "/http/app.env")
        with patch.object(runtime_config, "effective_environment_path", return_value=env):
            location = runtime_config.write_binding_descriptor(self.host, self.entry, self.inventory.deployment,
                                                               self.inventory.resolution, spec)
        value = json.loads(self.host.paths.p(location).read_text())
        self.assertEqual(value["schema"], 2)
        self.assertEqual(value["bindings"], c.record_document(spec.bindings))
        self.assertEqual(value["unit"], {"instance_id": spec.identity.instance_id, "role": "http", "ordinal": 1})
        self.assertNotIn("environments", value)
        self.assertNotIn("private_revision", value)

    def test_network_absence_refuses_before_any_host_action(self):
        with patch.object(network.importlib, "import_module", return_value=object()), self.assertRaises(c.DeployError) as raised:
            lifecycle.request_operation(self.host, "activate", {}, {})
        self.assertEqual(raised.exception.code, "capability_unavailable")
        self.assertIn("lane C", raised.exception.detail)
        self.assertFalse(self.host.paths.state.exists())

    def test_closure_cannot_be_synthesized_from_partial_network_observations(self):
        evidence = c.ClosureEvidence.parse(closure())
        transition = Mock(operation_id=OPERATION, affected_units=evidence.units)
        resolution = Mock(id=CURRENT, endpoints=evidence.endpoints)
        owner = Mock()
        owner.feature_close.return_value = replace(evidence, units=())
        with patch.object(network, "capability", return_value=owner), self.assertRaises(c.DeployError):
            network.close(self.host, self.entry, transition, resolution, {})
        owner.feature_close.return_value = evidence
        with patch.object(network, "capability", return_value=owner):
            self.assertEqual(network.close(self.host, self.entry, transition, resolution, {}), evidence)

    def test_feature_retention_does_not_spawn_a_collector(self):
        self.host.host_lock_fd = 123
        registry = {"instances": {"feature": {"topology": {"schema": 2}}}}
        with patch.object(self.host, "read_registry", return_value=registry), patch.object(self.host, "run") as run:
            value = upgrades.prune_tools(self.host, "a" * 64)
        run.assert_not_called()
        self.assertEqual(value["removed_bundles"], [])
        self.assertEqual(value["pruning_error"]["code"], "capability_unavailable")
        self.assertIn("post-preview", value["pruning_error"]["detail"])

    def test_preview_refusals_name_later_release(self):
        for action in ("keys-rotate", "backup-keys-rotate"):
            with self.subTest(action=action), self.assertRaises(c.DeployError) as raised:
                upgrades._check_feature_request(None, {"profile": {"schema": 3}}, action)
            self.assertEqual(raised.exception.code, "capability_unavailable")
            self.assertIn("post-preview", raised.exception.detail)

    def test_recovery_and_backup_dispatch_require_generation_13(self):
        for action in ("backup-configure", "backup-run", "restore-drill", "recover-plan", "recover-apply"):
            for generation in (12, 13):
                request = {"profile": {"schema": 3}, "client": {"generation": generation}}
                if generation == 12:
                    with self.subTest(action=action), self.assertRaises(c.DeployError) as raised:
                        upgrades._check_feature_request(None, request, action)
                    self.assertEqual(raised.exception.code, "instance_capability_required")
                else:
                    upgrades._check_feature_request(None, request, action)

    def test_new_runtime_owners_are_required_generation_12_files(self):
        required = upgrades.required_files(12)
        for name in ("feature_lifecycle", "feature_network", "feature_runtime", "admission", "services", "artifacts", "operations"):
            self.assertIn("lib/" + name + ".py", required)
        for name in ("server/runtime_v1.py", "server/templates/admission.service", "server/templates/custom.service"):
            self.assertIn(name, required)
            self.assertNotIn(name, upgrades.required_files(11))

    def test_container_fixture_uses_frozen_archive_and_artifact_contracts(self):
        import io
        import tarfile
        import acceptance_feature
        from test_artifacts import elf
        from test_contracts import fixture
        from dataclasses import asdict
        project, profile, entry = fixture()
        profile["architecture"] = "arm64"
        manifest = {"project_id": project["id"], "instance_id": profile["instance_id"],
                    "instance": entry["name"], "environment": profile["environment"], "architecture": "arm64"}
        source = Path(self.temporary.name).resolve() / "legacy.tar.gz"
        destination = source.with_name("feature.tar.gz")
        files = {"manifest.json": json.dumps(manifest).encode(), "artisan": elf("linux-arm64"),
                 "config/deploy.toml": b'[app.services.storage.disks.attachments]\ndriver="r2"\n',
                 "public/index.html": b"fixture", "private-source-maps/test/a.map": b"{}"}
        with tarfile.open(source, "w:gz") as outgoing:
            for name, data in files.items():
                info = tarfile.TarInfo(name)
                info.size = len(data)
                outgoing.addfile(info, io.BytesIO(data))
        with patch.object(acceptance_feature, "read_json", return_value=profile):
            _, placed, topology, _ = acceptance_feature.feature_archive(source, destination, entry["host_id"])
        with tarfile.open(destination, "r:gz") as incoming:
            output = json.load(incoming.extractfile("manifest.json"))
        declaration = c.ReleaseDeclaration.parse(output["deployment"])
        self.assertEqual(declaration.topology_sha256, topology.sha256)
        self.assertEqual(placed["schema"], 3)
        self.assertEqual([p.artifact for p in declaration.artifacts], ["relay"])
        self.assertEqual(dict(topology.roles[0].common.bindings)["relay"].role, "relay")
        public = dict(topology.roles[0].common.bindings)["relay_public"]
        self.assertEqual((public.role, public.listener), ("relay", "control"))
        from topology import parse_profile
        import listeners
        policy = dict(topology.roles[1].listeners)["control"]
        self.assertEqual((policy.bind, policy.tls, policy.authentication), ("public", "none", True))
        claims = listeners.public_claims(parse_profile(placed, topology))
        self.assertEqual({claim.socket.family for claim in claims}, {4, 6})
        self.assertEqual({claim.socket.port for claim in claims}, {8554})
        self.assertEqual({claim.socket.address for claim in claims}, set(acceptance_feature.network.ADDRESSES))
        self.assertEqual(placed["domain"], f"{project['slug']}-{profile['environment']}.acceptance.invalid")

    def test_public_peer_probe_requires_relay_bytes_and_connection_failure(self):
        import acceptance_feature as scenario
        from types import SimpleNamespace
        for opened, code, output, passes in ((True, 0, scenario.RELAY_BODY, True),
                                            (True, 0, "wrong backend", False),
                                            (False, 2, "", True), (False, 1, "", False),
                                            (False, 0, "CONNECTED\n", False)):
            with self.subTest(opened=opened, code=code, output=output):
                host = Mock()
                host.run.return_value = SimpleNamespace(returncode=code, stdout=output)
                if passes:
                    scenario.peer_reachable(host, opened=opened)
                    self.assertEqual(host.run.call_count, 2)
                else:
                    with self.assertRaises(AssertionError):
                        scenario.peer_reachable(host, opened=opened)
                for call in host.run.call_args_list:
                    self.assertEqual(call.args[0][:4], ["ip", "netns", "exec", scenario.network.PEER])
                    self.assertIn(call.args[0][-3], scenario.network.ADDRESSES)

    def test_public_probe_executes_exact_relay_response_and_distinct_connect_failure(self):
        import acceptance_feature as scenario
        import io
        import socket
        from types import SimpleNamespace
        from unittest.mock import MagicMock
        connection = MagicMock()
        connection.makefile.return_value = io.BytesIO(b"HTTP/1.1 200 OK\r\nContent-Length: 19\r\n\r\nfeature-relay-ready")
        output = io.BytesIO()
        with patch.object(socket, "create_connection", return_value=connection), \
                patch.object(sys, "argv", ["probe", "203.0.113.10", "8554", "yes"]), \
                patch.object(sys, "stdout", SimpleNamespace(buffer=output)):
            exec(scenario.PEER_PROBE, {})
        self.assertEqual(output.getvalue(), scenario.RELAY_BODY.encode())
        self.assertIn(b"GET /ready HTTP/1.1\r\n", connection.sendall.call_args.args[0])
        with patch.object(socket, "create_connection", side_effect=ConnectionRefusedError), \
                patch.object(sys, "argv", ["probe", "203.0.113.10", "8554", "no"]), self.assertRaises(SystemExit) as failure:
            exec(scenario.PEER_PROBE, {})
        self.assertEqual(failure.exception.code, 2)

    def test_public_report_assertion_uses_instance_and_both_families(self):
        import acceptance_feature as scenario
        value = {"projects": [{"instances": [{"name": "fixture-acceptance", "network": {
            "listeners": [{"port": scenario.PORT, "address": address, "expected_owner": "nginx", "role": "relay",
                           "listener": "control", "tls": "none", "authentication": True}
                          for address in scenario.network.ADDRESSES], "findings": []}}]}]}
        scenario.assert_listener_report(value, {"name": "fixture-acceptance"})
        value["projects"][0]["instances"][0]["network"]["findings"] = [{"code": "nginx_projection_unverified"}]
        with self.assertRaises(AssertionError):
            scenario.assert_listener_report(value, {"name": "fixture-acceptance"})

    def test_transition_closes_before_migration_and_opens_only_after_all_readiness(self):
        from contextlib import ExitStack
        import release
        from test_admission import attempt
        from test_host_capabilities import BOOT, INVOCATION
        events = []
        running = attempt()
        running.update(action="deploy", operation_id=OPERATION)
        state = {"last_attempt": running, "previous": None, "current": {}, "history": []}
        evidence = c.ClosureEvidence.parse(closure())
        store = Mock()
        store.begin.side_effect = lambda *_: events.append("pending")
        store.commit.side_effect = lambda: events.append("committed")
        store._selection.return_value = (c.DeploymentSelection.parse(__import__("test_host_capabilities").selection()), None)
        store.runtime = self.host.paths.p("/run/foundry/admission/test")
        store.runtime.mkdir(parents=True)
        store.durable = self.host.paths.state / "admission/test"
        def finish(*_):
            events.append("finished")
            running["outcome"] = "succeeded"
        with ExitStack() as stack:
            stack.enter_context(patch.object(lifecycle.admission, "AdmissionStore", return_value=store))
            stack.enter_context(patch.object(lifecycle.admission, "boot_id", return_value=BOOT))
            stack.enter_context(patch.object(lifecycle, "current_executor", return_value=c.ProcessIdentity(123, 456, INVOCATION)))
            stack.enter_context(patch.object(self.host, "journal"))
            stack.enter_context(patch.object(self.host, "save_state"))
            stack.enter_context(patch.object(self.host, "run", side_effect=lambda argv, **kw: events.append(argv[1] + ":" + argv[2])))
            stack.enter_context(patch.object(services, "enabled_units", return_value=tuple(self.inventory.roles)))
            stack.enter_context(patch.object(services, "wait_ready", side_effect=lambda host, inv, name, deadline:
                                            events.append("ready:" + name) or True))
            stack.enter_context(patch.object(lifecycle, "stop_units", side_effect=lambda *_: events.append("stopped")))
            stack.enter_context(patch.object(network, "withdraw", return_value=None))
            stack.enter_context(patch.object(network, "close", side_effect=lambda *_: (events.append("closed"), evidence)[1]))
            stack.enter_context(patch.object(network, "open_public", side_effect=lambda *_: events.append("opened")))
            stack.enter_context(patch.object(runtime_config, "effective_environment_path", return_value=Path("/effective.env")))
            status = stack.enter_context(patch.object(release, "migration_status", return_value=True))
            stack.enter_context(patch.object(release, "app", side_effect=lambda *args, **kw: events.append(args[3])))
            stack.enter_context(patch.object(release, "switch_to", side_effect=lambda *_: events.append("switched")))
            stack.enter_context(patch.object(release, "finish", side_effect=finish))
            stack.enter_context(patch.object(lifecycle, "ready", side_effect=lambda host, entry, inventory, name, deadline: (events.append("ready:" + name), True)[1]))
            stack.enter_context(patch.object(services, "wait_ready_set", side_effect=lambda *args: (events.append("ready-set"), True)[1]))
            lifecycle.transition(self.host, {}, self.entry, self.inventory, state, running)
        status.assert_called_once_with(self.host, self.entry, release.release_path(self.host, self.entry, self.inventory.deployment.release), env_file=Path("/effective.env"))
        ordered = ["pending", "stopped", "closed", "migrate", "db:seed", "switched", "ready:http", "ready:relay", "ready:worker", "ready-set", "opened", "finished", "committed"]
        self.assertEqual(ordered, [event for event in events if event in ordered])

    def test_feature_worker_uses_frozen_arguments_without_legacy_worker_environment(self):
        content = services.render_unit(self.inventory, "worker", self.entry, "a" * 64)
        self.assertNotIn("EnvironmentFile=", content)
        self.assertIn("runtime-v1", content)
        self.assertIn("ExecCondition=+", content)

    def test_feature_maintenance_uses_existing_bounded_application_response(self):
        import maintenance
        import release
        import subprocess
        entry = {**self.entry, "topology": {"schema": 2}}
        for action in ("down", "up"):
            result = subprocess.CompletedProcess([], 0, json.dumps({"down": action == "down"}), "")
            with self.subTest(action=action), patch.object(release, "app", return_value=result) as app:
                self.assertEqual(maintenance.run(self.host, entry, Path("/unused"), action, ()),
                                 {"down": action == "down"})
            self.assertEqual(app.call_args.args[3], action)

    def test_maintenance_requires_pinned_executor_before_host_effects(self):
        for action in ("down", "up"):
            with self.subTest(action=action), patch.object(self.host, "run") as run, \
                    patch.object(self.host, "lock") as lock, self.assertRaises(c.DeployError) as raised:
                lifecycle.execute(self.host, {"action": action, "project": {}, "profile": self.entry["profile"]})
            self.assertEqual(raised.exception.code, "capability_unavailable")
            run.assert_not_called()
            lock.assert_not_called()
