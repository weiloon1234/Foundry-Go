"""Shared release review preserves interactive requests and rehearsal secrecy."""
import copy
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
import uuid
from unittest.mock import MagicMock, patch

from test_local import cli, fixture
import rehearsal_host
from contracts import DeployError, digest
from runtime_config import service_secret_requirements
from topology import parse_profile, parse_topology
import build_acceptance


class ReleaseReviewTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name).resolve()
        self.archive = self.root / "app.tar.gz"
        self.archive.write_bytes(b"archive")
        self.project, self.profile = fixture()
        self.operation = str(uuid.uuid4())
        self.context = {"project": self.project, "profile": self.profile,
                        "activation_operation_id": self.operation, "archive_sha256": digest(self.archive)}
        (self.root / "secrets.json").write_text('{"configuration":{}}')

    def activate(self, responses):
        calls = []
        def action(name, context, **arguments):
            self.assertEqual(context, self.context)
            calls.append((name, copy.deepcopy(arguments)))
            result = responses[len(calls) - 1]
            if isinstance(result, Exception):
                raise result
            return result
        with patch.object(rehearsal_host, "WORK", self.root), patch.object(rehearsal_host, "action", side_effect=action):
            result = rehearsal_host.phase("activate", self.context)
        return result, calls

    def test_feature_and_schema_three_review_before_activate_with_stable_identity(self):
        for schema, instance in ((2, {"deployment_selection": {}}), (3, {})):
            with self.subTest(schema=schema):
                self.profile["schema"] = schema
                plan = {"ok": True, "plan_token": "reviewed", "operation_id": self.operation, "unit_changes": ["relay@1", "relay@2"]}
                result, calls = self.activate([{"instance": instance}, plan, {"ok": True}])
                self.assertEqual([name for name, _ in calls], ["status", "release-plan", "activate"])
                expected = {"archive": str(self.archive), "archive_sha256": digest(self.archive), "operation_id": self.operation}
                self.assertEqual(calls[1][1], expected)
                self.assertEqual(calls[2][1], {**expected, "plan_token": "reviewed"})
                self.assertEqual(result["release_plan"], plan)
                self.assertEqual(result["operation_id"], self.operation)

    def test_non_feature_activation_has_no_release_plan(self):
        result, calls = self.activate([{"instance": {}}, {"ok": True}])
        self.assertEqual([name for name, _ in calls], ["status", "activate"])
        self.assertNotIn("plan_token", calls[-1][1])
        self.assertNotIn("release_plan", result)
        self.assertTrue(result["ok"])

    def requirements(self):
        root = build_acceptance.FIXTURE / "deploy/rehearsal"
        topology = parse_topology(json.loads((root / "topology.json").read_text()))
        profile = parse_profile(json.loads((root / "environments/acceptance-run.json").read_text()), topology)
        return service_secret_requirements(topology, profile, {})

    def test_copy_secrets_are_resolved_from_each_public_requirement_without_app_settings(self):
        requirements = self.requirements()
        self.assertEqual(len(requirements), 2)
        configuration = {row["key"]: "private-canary-" + str(index) for index, row in enumerate(requirements)}
        (self.root / "secrets.json").write_text(json.dumps({"configuration": {**configuration, "APP__PRIVATE": "app-canary"}}))
        plan = {"ok": True, "plan_token": "reviewed", "required_service_secrets": [{**row, "present": True} for row in requirements]}
        result, calls = self.activate([{"instance": {"deployment_selection": {}}},
                                      {"ok": False, "required_service_secrets": requirements}, plan, {"ok": True}])
        self.assertEqual([name for name, _ in calls], ["status", "release-plan", "release-plan", "activate"])
        self.assertEqual(calls[2][1], {**calls[1][1], "secrets": {"configuration": configuration}})
        self.assertNotIn("secrets", calls[3][1])
        self.assertNotIn("canary", json.dumps(result))
        self.assertTrue(result["ok"])

    def test_missing_copy_secret_reports_only_its_name_and_never_activates(self):
        requirements = self.requirements()
        (self.root / "secrets.json").write_text(json.dumps({"configuration": {requirements[0]["key"]: "secret-canary"}}))
        result, calls = self.activate([{"instance": {"deployment_selection": {}}},
                                      {"ok": False, "required_service_secrets": requirements}])
        self.assertEqual(result["error"], "rehearsal_service_secrets_missing")
        self.assertEqual(result["missing_service_secrets"], [requirements[1]["key"]])
        self.assertNotIn("canary", json.dumps(result))
        self.assertNotIn("activate", [name for name, _ in calls])

    def test_bootstrap_also_checks_public_copy_requirements_before_mutation(self):
        plan = {"plan_token": "bootstrap", "observations": {"required_service_secrets": self.requirements()}}
        (self.root / "plan.json").write_text(json.dumps(plan))
        with patch.object(rehearsal_host, "WORK", self.root), patch.object(rehearsal_host, "action") as action:
            with self.assertRaises(rehearsal_host.MissingServiceSecrets) as raised:
                rehearsal_host.phase("bootstrap-apply", self.context)
        action.assert_not_called()
        result = rehearsal_host.error_result(raised.exception)
        self.assertEqual(result["error"], "rehearsal_service_secrets_missing")
        self.assertEqual(result["missing_service_secrets"], sorted(row["key"] for row in self.requirements()))

    def test_incomplete_review_and_failed_activation_keep_safe_public_evidence(self):
        plan = {"ok": True, "plan_token": "reviewed", "unit_changes": ["relay@1"]}
        result, _ = self.activate([{"instance": {"deployment_selection": {}}}, plan,
                                  DeployError("activation_failed", "private-error-canary")])
        self.assertEqual(result["error"], "activation_failed")
        self.assertEqual(result["release_plan"], plan)
        self.assertNotIn("canary", json.dumps(result))
        incomplete = {"ok": False, "required_service_secrets": []}
        result, calls = self.activate([{"instance": {"deployment_selection": {}}}, incomplete, incomplete])
        self.assertEqual(result["error"], "rehearsal_release_review")
        self.assertEqual(result["release_plan"], incomplete)
        self.assertNotIn("activate", [name for name, _ in calls])

    def test_host_transport_accepts_incomplete_review_but_rejects_failed_actions(self):
        response = {"ok": False, "required_service_secrets": self.requirements()}
        with patch.object(rehearsal_host, "run", return_value=subprocess.CompletedProcess([], 0, json.dumps(response))):
            self.assertEqual(rehearsal_host.action("release-plan", self.context), response)
            with self.assertRaises(DeployError):
                rehearsal_host.action("activate", self.context)
        with patch.object(rehearsal_host, "run", return_value=subprocess.CompletedProcess([], 1, json.dumps(response))):
            with self.assertRaises(DeployError):
                rehearsal_host.action("release-plan", self.context)

    def test_client_requests_secret_prompt_display_and_confirmation_are_unchanged(self):
        manifest = dict(version="version", commit="commit", tree_state="dirty", architecture="amd64", instance="abc-prod")
        (self.root / "manifest.json").write_text(json.dumps(manifest))
        requirements = self.requirements()
        configuration = {row["key"]: "client-secret-canary" for row in requirements}
        for feature, complete in ((False, True), (True, True), (True, False)):
            with self.subTest(feature=feature, complete=complete):
                remote = MagicMock()
                remote.temporary.return_value.__enter__.return_value = "/remote"
                calls = []
                plan = {"ok": complete, "required_service_secrets": requirements, **({"plan_token": "reviewed"} if complete else {})}
                def request(name, payload):
                    calls.append((name, copy.deepcopy(payload)))
                    if name == "status":
                        return {"instance": {"deployment_selection": {}} if feature else {}}
                    if name == "release-plan":
                        return plan if "secrets" in payload else {"ok": False, "required_service_secrets": requirements}
                    return {"ok": True}
                remote.request.side_effect = request
                with patch.dict(os.environ, {"ACTION": "deploy", "OPERATION": self.operation}, clear=True), \
                        patch.object(cli, "SSH", return_value=remote), patch.object(cli, "select_archive", return_value=self.archive), \
                        patch.object(cli, "check_release_tools"), patch.object(cli, "remember_releases"), \
                        patch.object(cli, "say") as say, patch.object(cli, "display") as display, \
                        patch.object(cli, "confirm") as confirm, patch.object(cli, "secret_input", return_value=configuration) as secret:
                    if complete:
                        cli.deploy(self.project, self.profile, {})
                    else:
                        with self.assertRaisesRegex(cli.LocalError, "^Release review is incomplete$"):
                            cli.deploy(self.project, self.profile, {})
                expected = ["status"] + (["release-plan", "release-plan"] if feature else []) + (["activate", "status"] if complete else [])
                self.assertEqual([name for name, _ in calls], expected)
                self.assertEqual(say.call_args_list[0].args, (f"Build and activate {cli.instance_name(self.project, self.profile)} on {self.profile['host']['address']}; pending forward migrations and seeders run during activation.",))
                self.assertEqual(say.call_args_list[1].args, (json.dumps(manifest, indent=2),))
                if feature:
                    secret.assert_called_once_with("config", "Service secret configuration JSON", structured=True, required=False)
                    self.assertEqual(calls[2][1], {**calls[1][1], "secrets": {"configuration": configuration}})
                    self.assertEqual(display.call_args_list[0].args, (plan,))
                else:
                    secret.assert_not_called()
                if complete:
                    confirm.assert_called_once_with("Activate this completed release, including its pending forward migrations")
                    activation = next(payload for name, payload in calls if name == "activate")
                    self.assertEqual(activation.get("plan_token"), "reviewed" if feature else None)
                    self.assertNotIn("secrets", activation)
                    self.assertEqual(display.call_args_list[-1].args, ({"ok": True},))
                else:
                    confirm.assert_not_called()
                self.assertNotIn("canary", str(display.call_args_list) + str(say.call_args_list))


if __name__ == "__main__":
    unittest.main()
