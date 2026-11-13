"""Public manager boundaries using temporary files and injected process results."""
import copy
import importlib.util
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch
import uuid

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "lib"))
from contracts import DeployError, HOST_PATHS, MAX_JSON, atomic_json, read_json
from host import Host, Paths
import upgrades
from test_contracts import fixture, registry

TOOLS = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location("deployment_manager", TOOLS / "server/manage.py")
manager = importlib.util.module_from_spec(spec)
spec.loader.exec_module(manager)


class ManagerTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.host = Host(Paths(Path(temporary.name).resolve()))
        self.project, self.profile, self.entry = fixture()
        self.host.write_registry(registry(self.entry))
        self.request = {**upgrades.protocol_metadata(), "project": self.project, "profile": self.profile}

    def test_mutations_reject_newer_and_older_clients_before_commands(self):
        with patch.object(self.host, "run") as run:
            for generation, code in ((0, "client_too_old"), (upgrades.protocol()["generation"] + 1, "host_upgrade_required")):
                request = copy.deepcopy(self.request)
                request["client"]["generation"] = generation
                with self.assertRaises(DeployError) as error:
                    manager.execute(self.host, "stop", request)
                self.assertEqual(code, error.exception.code)
            run.assert_not_called()


    def test_generation_nine_client_can_deploy_on_current_host(self):
        import release
        value = self.host.read_registry()
        value['tools'] = upgrades._active_record(TOOLS, upgrades.tool_bundle(TOOLS)[2], None)
        self.assertEqual(upgrades.protocol()['generation'], value['tools']['generation'])
        self.host.write_registry(value)
        request = {**self.request, 'client': {**self.request['client'], 'generation': 9},
                   'archive': '/staged/app.tar.gz', 'archive_sha256': 'a' * 64}
        with patch.object(release, 'activate', return_value={'ok': True}) as activate:
            self.assertEqual({'ok': True}, manager.execute(self.host, 'activate', request))
        activate.assert_called_once()
        self.assertEqual('/staged/app.tar.gz', activate.call_args.args[3])

    def test_feature_gate_is_target_local_and_precedes_lifecycle_dispatch(self):
        import release
        from test_host_capabilities import selection
        _, _, feature = fixture(project_slug="feature", host_id=self.entry["host_id"], port=8101)
        feature["deployment_selection"] = selection(feature["profile"]["instance_id"])
        value = registry(self.entry, feature)
        value["tools"] = upgrades._active_record(TOOLS, upgrades.tool_bundle(TOOLS)[2], None)
        self.host.write_registry(value)
        request = {**self.request, "client": {**self.request["client"], "generation": 9},
                   "archive": "/staged/app.tar.gz", "archive_sha256": "a" * 64}
        for generation in (9, 10, 11):
            request["client"]["generation"] = generation
            with patch.object(release, "activate", return_value={"ok": True}) as activate:
                self.assertEqual({"ok": True}, manager.execute(self.host, "activate", request))
                activate.assert_called_once()
            feature_request = {**self.request, "profile": feature["profile"],
                               "client": {**self.request["client"], "generation": generation}}
            with patch.object(self.host, "run") as run:
                for action in ("status", "activate", "config-set", "restart", "rollback"):
                    with self.subTest(generation=generation, action=action), self.assertRaises(DeployError) as caught:
                        manager.execute(self.host, action, feature_request)
                    self.assertEqual(caught.exception.code, "instance_capability_required")
                run.assert_not_called()

    def test_unknown_mutation_fields_are_rejected(self):
        with self.assertRaises(DeployError) as error:
            manager.execute(self.host, "stop", {**self.request, "unreviewed": True})
        self.assertEqual("invalid_request", error.exception.code)


    def test_rotation_requires_and_dispatches_stable_operation_id(self):
        import release
        with self.assertRaises(DeployError) as missing:
            manager.execute(self.host, "keys-rotate", {**self.request, "confirm": True})
        self.assertEqual("invalid_operation", missing.exception.code)
        operation = str(uuid.uuid4())
        with patch.object(release, "keys_rotate", return_value={"ok": True}) as rotate:
            manager.execute(self.host, "keys-rotate", {**self.request, "confirm": True, "operation_id": operation})
        self.assertEqual((operation, None), rotate.call_args.args[-2:])

    def test_bound_host_mismatch_rejects_read_requests(self):
        request = copy.deepcopy(self.request)
        request["profile"]["host"]["host_id"] = str(uuid.uuid4())
        with self.assertRaises(DeployError) as error:
            manager.execute(self.host, "inspect", request)
        self.assertEqual("host_mismatch", error.exception.code)

    def test_sync_does_not_mutate_registry_or_adopt_local_profile(self):
        before = self.host.paths.registry.read_bytes()
        request = copy.deepcopy(self.request)
        request["profile"]["certbot_email"] = "new@example.invalid"
        response = manager.execute(self.host, "sync", request)
        self.assertEqual(self.profile, response["profile"])
        self.assertEqual(before, self.host.paths.registry.read_bytes())

    def test_real_bundle_installs_into_schema_two_registry_and_hello(self):
        from provision import new_registry
        self.host.paths.registry.unlink()
        value = new_registry(str(uuid.uuid4()), TOOLS)
        with self.host.lock("host"):
            upgrades.install_tools(self.host, value, TOOLS)
        loaded = self.host.read_registry()
        self.assertEqual(2, loaded["schema"])
        response = manager.execute(self.host, "hello", {})
        self.assertEqual({"major": 2, "minor": 0}, response["protocol"])
        self.assertEqual({"minimum": upgrades.protocol()["minimum_client_generation"], "maximum": upgrades.protocol()["generation"]}, response["client_generation"])
        self.assertEqual(loaded["tools"]["bundle_sha256"], response["tools"]["bundle_sha256"])

    def test_plan_apply_and_resume_finish_deletions_without_intent_in_real_child(self):
        from provision import new_registry
        self.host.paths.registry.unlink()
        value = new_registry(self.entry["host_id"], TOOLS)
        with self.host.lock('host'):
            installed = upgrades.install_tools(self.host, value, TOOLS)
        area = self.host.paths.p(HOST_PATHS['upgrade_deletions'])
        area.mkdir(mode=0o700)
        atomic_json(self.host.paths.p(HOST_PATHS['retired_tools']),
                    {'schema': 1, 'digests': ['a' * 64]}, 0o600)
        for action in ('tools-apply', 'upgrade-resume'):
            with self.subTest(action=action):
                queued = area / str(uuid.uuid4())
                queued.mkdir(mode=0o700)
                # A prior rmtree already removed intent.json; only deletion
                # ownership (the private directory) needs to survive.
                (queued / 'registry-before.json').write_text('{}')
                plan = manager.execute(self.host, 'tools-plan', self.request)
                self.assertEqual([queued.name], plan['prune_upgrade_journals'])
                result = manager.execute(self.host, action, {**self.request,
                    'operation_id': installed['operation_id'], 'plan_token': plan['plan_token']})
                self.assertEqual('unchanged', result['outcome'])
                self.assertEqual([queued.name], result['removed_upgrade_journals'])
                self.assertFalse(queued.exists())

    def test_selected_bundle_prunes_in_a_real_child_with_inherited_lock(self):
        from provision import new_registry
        value = new_registry(str(uuid.uuid4()), TOOLS)
        with self.host.lock("host"):
            upgrades.install_tools(self.host, value, TOOLS)
            leftover = self.host.paths.tools / "tools" / ("b" * 64)
            leftover.mkdir()
            (leftover / "old.py").write_text("# retired\n")
            result = upgrades.install_tools(self.host, self.host.read_registry(), TOOLS)
            self.assertEqual([leftover.name], result["removed_bundles"])
            self.assertFalse(leftover.exists())
            # The child's exit must not release the parent's shared lock description.
            import fcntl
            import os
            from contracts import HOST_PATHS
            descriptor = os.open(self.host.paths.p(HOST_PATHS["locks"]) / "host.lock", os.O_WRONLY)
            try:
                with self.assertRaises(BlockingIOError):
                    fcntl.flock(descriptor, fcntl.LOCK_EX | fcntl.LOCK_NB)
            finally:
                os.close(descriptor)
        self.assertIsNone(self.host.host_lock_fd)


    def test_backup_requests_dispatch_under_the_registered_identity(self):
        import backup
        operation = str(uuid.uuid4())
        with patch.object(backup, "run_backup", return_value={"backup_id": operation}) as run:
            result = manager.execute(self.host, "backup-run", {**self.request, "confirm": True,
                                     "operation_id": operation, "kind": "diff"})
        self.assertEqual(operation, result["backup_id"])
        self.assertEqual((operation, "diff"), run.call_args.args[-2:])
        with patch.object(backup, "restore_drill", return_value={"status": "complete"}) as restore:
            manager.execute(self.host, "restore-drill", {**self.request, "confirm": True, "backup_id": operation})
        self.assertEqual(operation, restore.call_args.args[-1])
        self.assertEqual({"target_time": None, "target_inclusive": True, "retain": False}, restore.call_args.kwargs)


    def test_role_is_validated_for_logs_restart_and_stop_before_commands(self):
        with patch.object(self.host, "run") as run:
            for action in ("logs", "restart", "stop"):
                for role in ("undeclared", [], None):
                    with self.assertRaises(DeployError) as error:
                        manager.execute(self.host, action, {**self.request, "role": role})
                    self.assertEqual("invalid_role", error.exception.code)
            run.assert_not_called()

    def test_logs_default_and_requested_lines_use_registered_units(self):
        from topology import selected_units
        for lines in (None, 1, 2000):
            for role in (None, "http"):
                request = {**self.request}
                if lines is not None:
                    request["lines"] = lines
                if role is not None:
                    request["role"] = role
                with self.subTest(lines=lines, role=role), \
                        patch.object(self.host, "run", return_value=subprocess.CompletedProcess([], 0, "fixture\n")) as run:
                    result = manager.execute(self.host, "logs", request)
                self.assertEqual({"instance": self.entry["name"], "logs": "fixture\n"}, result)
                units = selected_units(self.entry, role or "all").values()
                self.assertEqual(["journalctl", *[arg for unit in units for arg in ("--unit", unit)],
                                  f"--lines={lines if lines is not None else 100}", "--no-pager", "--output=short-iso"],
                                 run.call_args.args[0])
                self.assertEqual({"timeout": 15}, run.call_args.kwargs)

    def test_logs_rejects_non_integer_and_out_of_range_lines_before_commands(self):
        with patch.object(self.host, "run") as run:
            for lines in (0, -1, 2001, "100", "", None, True, False, 100.0, [], {}):
                with self.subTest(lines=lines), self.assertRaises(DeployError) as error:
                    manager.execute(self.host, "logs", {**self.request, "lines": lines})
                self.assertEqual("invalid_lines", error.exception.code)
            run.assert_not_called()

    def test_logs_request_fields_and_local_only_follow_are_rejected_before_commands(self):
        with patch.object(self.host, "run") as run:
            for action, fields in (("logs", {"follow": True}), ("logs", {"unreviewed": 1}),
                                   ("status", {"lines": 100}), ("stop", {"lines": 100}),
                                   ("hello", {"follow": False})):
                with self.subTest(action=action, fields=fields), self.assertRaises(DeployError) as error:
                    manager.execute(self.host, action, {**self.request, **fields})
                self.assertEqual("invalid_request", error.exception.code)
            run.assert_not_called()

    def test_logs_response_bound_counts_json_escaping_and_trailing_newline(self):
        envelope = len(json.dumps({"instance": self.entry["name"], "logs": ""}, separators=(",", ":")).encode()) + 1
        for character in ("a", '"', "\n", "é", "😀"):
            escaped_size = len(json.dumps(character)) - 2
            count = (MAX_JSON - envelope) // escaped_size
            for size, fits in ((count, True), (count + 1, False)):
                output = character * size
                with self.subTest(character=character, fits=fits), \
                        patch.object(self.host, "run", return_value=subprocess.CompletedProcess([], 0, output)):
                    if fits:
                        result = manager.execute(self.host, "logs", {**self.request, "lines": 2000})
                        self.assertEqual(output, result["logs"])
                        self.assertLessEqual(len(json.dumps(result, separators=(",", ":")).encode()) + 1, MAX_JSON)
                    else:
                        with self.assertRaises(DeployError) as error:
                            manager.execute(self.host, "logs", {**self.request, "lines": 1})
                        self.assertEqual("logs_too_large", error.exception.code)
                        self.assertNotIn(output[:100], error.exception.detail)

    def test_feature_maintenance_refuses_both_directions_without_mutation(self):
        from test_host_capabilities import declaration, profile
        from topology import validate_topology
        value = profile()
        self.profile.update(schema=3, placement=value["placement"], listeners=value["listeners"])
        self.entry["topology"] = validate_topology(declaration())
        self.host.write_registry(registry(self.entry))
        before = self.host.paths.registry.read_bytes()
        for action in ("down", "up"):
            with self.subTest(action=action), patch.object(self.host, "run") as run, \
                    patch.object(self.host, "save_state") as save, self.assertRaises(DeployError) as raised:
                manager.execute(self.host, action, {**self.request, "confirm": True})
            self.assertEqual(raised.exception.code, "capability_unavailable")
            self.assertIn("post-preview", raised.exception.detail)
            run.assert_not_called()
            save.assert_not_called()
            self.assertEqual(self.host.paths.registry.read_bytes(), before)

    def test_maintenance_requires_confirmation_and_rejects_up_parameters(self):
        with patch.object(self.host, "run") as run:
            for action in ("down", "up"):
                with self.assertRaises(DeployError) as error:
                    manager.execute(self.host, action, self.request)
                self.assertEqual("confirmation_required", error.exception.code)
            for field in ("message", "retry", "allow", "except", "bypass"):
                with self.assertRaises(DeployError) as error:
                    manager.execute(self.host, "up", {**self.request, "confirm": True, field: None})
                self.assertEqual("invalid_request", error.exception.code)
            for values in ({"message": "bad\n"}, {"retry": True}, {"retry": 86401},
                           {"allow": ["not-an-ip"]}, {"except": ["get /health"]},
                           {"except": ["GET /a/../b"]}, {"bypass": "yes"}):
                with self.assertRaises(DeployError) as error:
                    manager.execute(self.host, "down", {**self.request, "confirm": True, **values})
                self.assertEqual("invalid_maintenance", error.exception.code)
            run.assert_not_called()

    def test_maintenance_requires_a_current_release(self):
        for action in ("down", "up"):
            with self.assertRaises(DeployError) as error:
                manager.execute(self.host, action, {**self.request, "confirm": True})
            self.assertEqual("no_release", error.exception.code)

    def test_generation_seven_maintenance_record_is_dropped_on_load_and_save(self):
        state = self.host.load_state(self.entry)
        path = self.host.paths.instance_state(self.entry["name"]) / "state.json"
        for value in ({"down": True, "since": "2026-11-11T00:00:00Z"}, None, {"obsolete": True}):
            with self.subTest(value=value):
                atomic_json(path, {**state, "maintenance": value})
                loaded = self.host.load_state(self.entry)
                self.assertEqual(state, loaded)
                self.host.save_state(self.entry, loaded)
                self.assertNotIn("maintenance", read_json(path))
