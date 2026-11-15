"""Rehearsal storage gates and public evidence; no container execution."""
import copy
import json
import os
from pathlib import Path
import subprocess
import sys
import unittest
from unittest.mock import Mock, patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "lib"))
import rehearsal_storage as storage
import runtime_config
from contracts import DeployError


class RehearsalStorageTests(unittest.TestCase):
    def setUp(self):
        self.profile = {"runtime_environment": "testing", "environment": "acceptance-local",
                        "domain": "app.acceptance.invalid"}
        self.entry = {"profile": self.profile}
        self.token = "a" * 32
        self.disks = {"uploads": {"driver": "s3_compatible", "cloud": {
            "conditional_write_mode": storage.MODE, "compatible": {"single_request_conditions": True}}}}

    def guard(self):
        return patch.multiple(storage, read_json=Mock(return_value={"run_id": self.token, "profile": self.profile}),
                              check_path=Mock(return_value=storage.CONTEXT))

    def test_marker_requires_run_identity_and_acceptance_profile(self):
        with patch.dict(os.environ, {}, clear=True):
            with self.assertRaises(DeployError) as raised:
                storage.validate_disks(self.disks, self.entry)
            self.assertEqual(raised.exception.code, "rehearsal_storage_configuration")
            self.assertEqual(storage.environment(self.entry), {})
        with patch.dict(os.environ, {storage.RUN_ID: self.token}), self.guard(), patch.object(Path, "is_file", return_value=True):
            storage.validate_disks(self.disks, self.entry)
            self.assertEqual(storage.environment(self.entry), {storage.RUN_ID: self.token})
            self.assertEqual(storage.unit_environment(self.entry), f"Environment={storage.RUN_ID}={self.token}\n")
            for field, value in (("runtime_environment", "production"), ("environment", "production"), ("domain", "app.example.com")):
                entry = copy.deepcopy(self.entry)
                entry["profile"][field] = value
                with self.subTest(field=field), self.assertRaises(DeployError):
                    storage.validate_disks(self.disks, entry)
        with patch.dict(os.environ, {storage.RUN_ID: self.token}), self.guard(), patch.object(Path, "is_file", return_value=False):
            with self.assertRaises(DeployError):
                storage.validate_disks(self.disks, self.entry)

    def test_bootstrap_intake_refuses_marker_without_rehearsal(self):
        with patch.dict(os.environ, {}, clear=True), patch.object(runtime_config, "read_credentials", return_value={}):
            with self.assertRaises(DeployError) as raised:
                runtime_config.initialize_bootstrap_secrets(Mock(), self.entry, {"app_storage": {
                    "credentials": {"rehearsal": {"mode": "static"}}, "disks": self.disks}})
            self.assertEqual(raised.exception.code, "rehearsal_storage_configuration")

    def test_builtins_and_custom_units_receive_guarded_identity(self):
        import services
        from test_services import inventory_fixture
        from test_host_capabilities import DIGEST
        inventory, entry, _ = inventory_fixture()
        entry["profile"].update(self.profile)
        context = {"run_id": self.token, "profile": entry["profile"]}
        with patch.dict(os.environ, {storage.RUN_ID: self.token}), patch.object(storage, "read_json", return_value=context), \
                patch.object(storage, "check_path", return_value=storage.CONTEXT), patch.object(Path, "is_file", return_value=True):
            for identity in inventory.units:
                content = services.render_unit(inventory, identity, entry, DIGEST)
                self.assertIn(storage.RUN_ID + "=" + self.token, content)
                self.assertEqual(content.count(storage.RUN_ID + "="), 1)

    def test_transient_operation_passes_identity_to_executor_and_cleanup(self):
        import operations
        from test_host_capabilities import OPERATION, DIGEST
        host = Mock()
        host.run.return_value = subprocess.CompletedProcess([], 0, "", "")
        with patch.dict(os.environ, {storage.RUN_ID: self.token}), self.guard(), \
                patch.object(Path, "is_file", return_value=True), \
                patch.object(operations, "directory", return_value=Path("/unused")), \
                patch.object(operations.admission, "AdmissionStore"), \
                patch.object(operations.admission, "read_owned", return_value={"ok": True}):
            request = {"operation_id": OPERATION, "profile": {**self.profile, "instance_id": "fixture"}}
            with patch.object(storage, "read_json", return_value={"run_id": self.token, "profile": request["profile"]}):
                self.assertEqual(operations.launch(host, request, {"bundle_sha256": DIGEST}), {"ok": True})
        self.assertIn("--setenv=" + storage.RUN_ID + "=" + self.token, host.run.call_args.args[0])

    def test_generic_file_override_cannot_issue_marker(self):
        host = Mock()
        with patch.object(runtime_config, "validate_keyring_change"), \
                patch.object(runtime_config, "_read_value_file", return_value=json.dumps(self.disks)):
            with self.assertRaises(DeployError) as raised:
                runtime_config.candidate_values(host, self.entry, {}, {"APP__SERVICES__STORAGE__DISKS_FILE": "/private/value"})
            self.assertEqual(raised.exception.code, "rehearsal_storage_configuration")
        host.owned_write.assert_not_called()

    def test_identity_alone_does_not_change_disks(self):
        disks = {"uploads": {"driver": "r2"}}
        original = copy.deepcopy(disks)
        with patch.dict(os.environ, {storage.RUN_ID: self.token}):
            storage.validate_disks(disks, self.entry)
        self.assertEqual(disks, original)

    def test_generic_configuration_and_archive_reject_markers_even_in_rehearsal(self):
        values = [self.disks, {"app": {"services": {"storage": {"disks": self.disks}}}},
                  {"APP__SERVICES__STORAGE__DISKS": json.dumps(self.disks)},
                  {"APP__SERVICES__STORAGE__DISKS__UPLOADS__CLOUD__CONDITIONAL_WRITE_MODE": storage.MODE}]
        with patch.dict(os.environ, {storage.RUN_ID: self.token}), self.guard():
            for value in values:
                with self.subTest(value=value), self.assertRaises(DeployError) as raised:
                    storage.reject_marker(value)
                self.assertEqual(raised.exception.code, "rehearsal_storage_configuration")
            with self.assertRaises(DeployError):
                runtime_config.validate_configuration_changes(values[2], self.entry)

    def test_report_selects_only_safe_fields_and_counts_across_restarts(self):
        startup = {"event": "foundry_rehearsal_storage", "disk": "uploads", "runtime_cap_bytes": 8388608,
                   "max_object_bytes": 1073741824, "exceeding_policy_maxima": [
                       {"name": "members.documents", "max_bytes": 524288000, "secret": "private"}],
                   "key": "private/key", "credentials": "private"}
        refusal = {"event": "foundry_rehearsal_storage_refusal", "disk": "uploads", "runtime_cap_bytes": 8388608,
                   "refusals": 1, "key": "private/key"}
        output = "\n".join(json.dumps({"MESSAGE": json.dumps(record)}) for record in [startup, refusal, startup, refusal])
        host = Mock()
        host.run.return_value = subprocess.CompletedProcess([], 0, output, "")
        rows = storage.collect(host, {"http": "app.service"})
        self.assertEqual(rows, [{"disk": "uploads", "runtime_cap_bytes": 8388608, "max_object_bytes": 1073741824,
                                "exceeding_policy_maxima": [{"name": "members.documents", "max_bytes": 524288000}],
                                "policy_observation": "observed", "runtime_refusals_observed": 2}])
        self.assertNotIn("private", json.dumps(rows))
        host.run.return_value.stdout = json.dumps({"MESSAGE": json.dumps(refusal)})
        self.assertEqual(storage.collect(host, {"http": "app.service"})[0]["policy_observation"], "pending")
