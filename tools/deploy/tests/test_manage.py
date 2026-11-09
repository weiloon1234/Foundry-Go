"""Public manager boundaries using temporary files and injected process results."""
import copy
import importlib.util
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch
import uuid

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "lib"))
from contracts import DeployError
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

    def test_legacy_clients_remain_upgradeable_but_cannot_mutate_a_fenced_host(self):
        request = copy.deepcopy(self.request)
        request["client"]["generation"] = 4
        upgrades.check_client(self.host, request, "stop")
        state = self.host.load_state(self.entry)
        state["queue_compatibility"] = {"schema": 1, "floor": 3, "permission": "closed", "admitted": {}}
        self.host.save_state(self.entry, state)
        with self.assertRaises(DeployError) as error:
            upgrades.check_client(self.host, request, "stop")
        self.assertEqual("client_too_old", error.exception.code)

    def test_unknown_mutation_fields_are_rejected(self):
        with self.assertRaises(DeployError) as error:
            manager.execute(self.host, "stop", {**self.request, "unreviewed": True})
        self.assertEqual("invalid_request", error.exception.code)

    def test_cutover_and_restore_require_confirmation_and_forward_exact_intent(self):
        import release
        operation, token = str(uuid.uuid4()), 'a' * 64
        for prefix, implementation in (("cutover", "apply_cutover"), ("redis-restore", "apply_redis_restore")):
            for suffix in ("apply", "resume"):
                action = prefix + "-" + suffix
                request = {**self.request, "plan_token": token, "operation_id": operation}
                with patch.object(release, implementation, return_value={"ok": True}) as apply:
                    with self.assertRaises(DeployError) as error:
                        manager.execute(self.host, action, request)
                    self.assertEqual("confirmation_required", error.exception.code)
                    apply.assert_not_called()
                    manager.execute(self.host, action, {**request, "confirm": True})
                    self.assertEqual((token, operation), apply.call_args.args[-2:])
                    self.assertEqual({"resume": suffix == "resume"}, apply.call_args.kwargs)
                    with self.assertRaises(DeployError) as error:
                        manager.execute(self.host, action, {**request, "confirm": True, "role": "worker"})
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
        upgrades.install_tools(self.host, value, TOOLS)
        loaded = self.host.read_registry()
        self.assertEqual(2, loaded["schema"])
        response = manager.execute(self.host, "hello", {})
        self.assertEqual({"major": 2, "minor": 0}, response["protocol"])
        self.assertEqual({"minimum": upgrades.protocol()["minimum_client_generation"], "maximum": upgrades.protocol()["generation"]}, response["client_generation"])
        self.assertEqual(loaded["tools"]["bundle_sha256"], response["tools"]["bundle_sha256"])


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
