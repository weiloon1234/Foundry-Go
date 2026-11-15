"""Public offline action against application declarations, with all I/O exits closed."""
from contextlib import ExitStack, redirect_stderr, redirect_stdout
import io
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

from test_local import cli
from test_host_capabilities import bundle, declaration, profile as feature_profile
from test_uploads import profile as upload_profile
import runtime_config

FIXTURE = Path(__file__).resolve().parents[3] / "tests/fixtures/deploy-acceptance"


class OfflineValidationTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name).resolve()
        for directory in ("deploy", "config"):
            shutil.copytree(FIXTURE / directory, self.root / directory)
        shutil.copyfile(FIXTURE / "go.mod", self.root / "go.mod")

    def read(self, path):
        return json.loads((self.root / path).read_text())

    def write(self, path, value):
        destination = self.root / path
        destination.parent.mkdir(parents=True, exist_ok=True)
        destination.write_text(json.dumps(value))

    def invoke(self, *, environment=None, format="json", flags=()):
        stdout, stderr = io.StringIO(), io.StringIO()
        environ = {"ACTION": "validate", "FORMAT": format,
                   "FOUNDRY_SECRET_CONFIG_FILE": str(self.root / "must-not-read")}
        if environment is not None:
            environ["ENVIRONMENT"] = environment
        with ExitStack() as stack:
            stack.enter_context(patch.dict(os.environ, environ, clear=True))
            stack.enter_context(patch.object(sys, "argv", ["cli.py", "--app-root", str(self.root), "deploy", *flags]))
            stack.enter_context(patch.object(cli, "ROOT", None))
            for target in ("socket.socket", "socket.getaddrinfo", "subprocess.Popen"):
                stack.enter_context(patch(target, side_effect=AssertionError("offline action attempted external I/O")))
            for name in ("lock", "local_settings", "secret_input", "ask", "SSH", "build"):
                stack.enter_context(patch.object(cli, name, side_effect=AssertionError("offline action entered runtime")))
            stack.enter_context(patch.object(runtime_config, "_read_value_file", side_effect=AssertionError("private read")))
            stack.enter_context(redirect_stdout(stdout))
            stack.enter_context(redirect_stderr(stderr))
            status = cli.main()
        self.assertEqual(stderr.getvalue(), "")
        self.assertNotIn(str(self.root), stdout.getvalue())
        return status, json.loads(stdout.getvalue()) if format == "json" else stdout.getvalue()

    def failed(self, kind, code, **options):
        status, result = self.invoke(**options)
        self.assertEqual(status, 1, result)
        self.assertFalse(result["ok"])
        errors = [error["code"] for item in result["checks"] if item["kind"] == kind for error in item["errors"]]
        self.assertIn(code, errors, result)
        return result

    def feature(self):
        raw = declaration()
        self.write("deploy/topology.json", raw)
        self.write("deploy/files/relay.json", {"listen": {"address": "localhost", "port": 1}})
        for path in (self.root / "deploy/environments").glob("*.json"):
            value = self.read(path.relative_to(self.root))
            fields = feature_profile()
            value.update(schema=3, placement=fields["placement"], listeners=fields["listeners"])
            value["listeners"]["relay.ingress"]["dns"]["hostname"] = "relay.acceptance.invalid"
            self.write(path.relative_to(self.root), value)
        return raw

    def template(self, source='{"password":"{{secret.TOKEN_FILE}}"}'):
        raw = self.feature()
        raw["secrets"] = {"token": {}}
        raw["roles"][1]["secret_files"] = {"TOKEN_FILE": "token"}
        raw["roles"][1]["configs"]["settings"] = {
            "source": "deploy/files/relay.json", "mode": "rendered-template", "format": "json"}
        self.write("deploy/topology.json", raw)
        (self.root / "deploy/files/relay.json").write_text(source)
        return raw

    def test_acceptance_fixture_valid_all_profiles_without_state_or_network(self):
        before = {p.relative_to(self.root): p.read_bytes() for p in self.root.rglob("*") if p.is_file()}
        status, result = self.invoke()
        self.assertEqual(status, 0, result)
        self.assertEqual(set(result), {"schema", "action", "ok", "minimum_generation", "checks"})
        self.assertEqual((result["schema"], result["action"], result["ok"], result["minimum_generation"]), (1, "validate", True, 1))
        self.assertEqual([c["profile"] for c in result["checks"] if c["kind"] == "profile"], ["acceptance-lab", "acceptance"])
        self.assertTrue(any(c["path"] == "deploy/acceptance-lab-bindings.json" for c in result["checks"]))
        self.assertTrue(all(c["status"] == "passed" and not c["errors"] for c in result["checks"]))
        self.assertEqual(before, {p.relative_to(self.root): p.read_bytes() for p in self.root.rglob("*") if p.is_file()})
        self.assertFalse((self.root / ".deploy").exists())
        self.assertEqual(result, self.invoke()[1])

    def test_text_default_and_flags_select_one_profile(self):
        with patch.dict(os.environ, {}, clear=True), patch.object(sys, "argv", ["cli.py", "--app-root", str(self.root),
                "deploy", "--action", "validate", "--environment", "acceptance"]), redirect_stdout(io.StringIO()) as output:
            self.assertEqual(cli.main(), 0)
        self.assertIn("Declarations valid.", output.getvalue())
        self.assertNotIn("acceptance-lab", output.getvalue())
        self.assertEqual(self.invoke(flags=("--format", "json", "--environment", "acceptance"))[0], 0)

    def test_real_cli_exit_status_and_json(self):
        command = [sys.executable, "-B", str(Path(cli.__file__)), "--app-root", str(self.root),
                   "deploy", "--action", "validate", "--format", "json"]
        for valid in (True, False):
            if not valid:
                self.write("deploy/topology.json", {"schema": 1, "roles": []})
            result = subprocess.run(command, capture_output=True, text=True, timeout=15,
                                    env={"PATH": os.environ["PATH"], "PYTHONDONTWRITEBYTECODE": "1"})
            self.assertEqual(result.returncode, 0 if valid else 1, result.stderr)
            self.assertEqual(json.loads(result.stdout)["ok"], valid)
            self.assertEqual(result.stderr, "")

    def test_invalid_topology(self):
        self.write("deploy/topology.json", {"schema": 1, "roles": []})
        self.failed("topology", "invalid_topology")

    def test_invalid_project_does_not_hide_topology_failure(self):
        self.write("deploy/project.json", {"schema": 1, "id": "invalid", "slug": "app"})
        self.write("deploy/topology.json", {"schema": 1, "roles": []})
        result = self.failed("project", "invalid_project")
        self.assertTrue(any(c["kind"] == "topology" and c["status"] == "failed" for c in result["checks"]))
        self.assertIsNone(result["minimum_generation"])

    def test_invalid_profile(self):
        value = self.read("deploy/environments/acceptance.json")
        value["runtime_environment"] = "invalid"
        self.write("deploy/environments/acceptance.json", value)
        self.failed("profile", "invalid_profile")
        self.assertEqual(self.invoke(environment="acceptance-lab")[0], 0)

    def test_unknown_or_unsafe_profile_selector(self):
        self.failed("profile", "declaration_file", environment="missing")
        self.failed("selection", "invalid_profile", environment="../private")

    def test_profile_filename_and_duplicate_identity(self):
        value = self.read("deploy/environments/acceptance.json")
        value["environment"] = "staging"
        self.write("deploy/environments/acceptance.json", value)
        self.failed("profile", "invalid_profile")
        value["environment"] = "acceptance"
        value["instance_id"] = self.read("deploy/environments/acceptance-lab.json")["instance_id"]
        self.write("deploy/environments/acceptance.json", value)
        self.failed("profile", "invalid_profile")

    def test_invalid_template(self):
        self.template('{"password":"{{secret.MISSING_FILE}}"}')
        self.failed("service_configuration", "service_config_template")

    def test_templates_render_synthetic_values_without_secret_files(self):
        raw = self.template()
        for format, source in (("json", '{"password":"{{secret.TOKEN_FILE}}"}'),
                               ("ini", "password={{secret.TOKEN_FILE}}\n"),
                               ("yaml", "password: {{secret.TOKEN_FILE}}\n"),
                               ("text", "{{secret.TOKEN_FILE}}")):
            with self.subTest(format=format):
                raw["roles"][1]["configs"]["settings"]["format"] = format
                self.write("deploy/topology.json", raw)
                (self.root / "deploy/files/relay.json").write_text(source)
                status, result = self.invoke()
                self.assertEqual(status, 0, result)
                self.assertEqual(result["minimum_generation"], 14)
                self.assertNotIn("synthetic-secret", json.dumps(result))

    def test_invalid_json_pointer_binding(self):
        self.feature()
        self.write("deploy/files/relay.json", {"wrong": {}})
        self.failed("service_configuration", "service_config_binding")

    def test_missing_or_symlinked_template_source(self):
        self.template()
        path = self.root / "deploy/files/relay.json"
        path.unlink()
        self.failed("service_configuration", "declaration_file")
        path.symlink_to(self.root / "must-not-read")
        self.failed("service_configuration", "unsafe_path")

    def test_upload_route_overlap(self):
        value = self.read("deploy/environments/acceptance.json")
        value["uploads"] = {**upload_profile(), "routes": ["/api/{id}/media", "/api/123/media"]}
        self.write("deploy/environments/acceptance.json", value)
        self.failed("profile", "invalid_uploads")
        value["uploads"]["routes"] = ["/api/{id}/media"]
        self.write("deploy/environments/acceptance.json", value)
        self.assertEqual(self.invoke()[1]["minimum_generation"], 17)

    def test_copies_count_bounds(self):
        self.feature()
        value = self.read("deploy/environments/acceptance.json")
        for count in (-1, 129, True):
            with self.subTest(count=count):
                value["placement"]["relay"] = count
                self.write("deploy/environments/acceptance.json", value)
                self.failed("profile", "invalid_contract")
        value["placement"]["relay"] = 2
        value["listeners"]["relay.ingress"]["ports"] = [9443, 9444]
        self.write("deploy/environments/acceptance.json", value)
        status, result = self.invoke()
        self.assertEqual(status, 0, result)
        self.assertEqual(result["minimum_generation"], 18)

    def test_rehearsal_binding(self):
        value = self.read("deploy/acceptance-lab-bindings.json")
        value["storage_disks"] = []
        self.write("deploy/acceptance-lab-bindings.json", value)
        self.failed("rehearsal", "rehearsal_bindings")

    def test_explicit_rehearsal_binding_and_private_permissions_not_required(self):
        source = self.root / "deploy/acceptance-lab-bindings.json"
        source.rename(self.root / "deploy/lab.json")
        (self.root / "deploy/lab.json").chmod(0o644)
        status, result = self.invoke(environment="acceptance-lab", flags=("--bindings", "deploy/lab.json"))
        self.assertEqual(status, 0, result)
        self.assertTrue(any(c["kind"] == "rehearsal" and c["path"] == "deploy/lab.json" for c in result["checks"]))

    def test_proxy_and_managed_dns_declarations(self):
        raw = self.feature()
        raw["roles"][1]["listeners"]["ingress"]["proxy_protocol"] = "v1"
        self.write("deploy/topology.json", raw)
        self.assertEqual(self.invoke()[1]["minimum_generation"], 18)
        raw["roles"][1]["readiness"]["listener"] = "ingress"
        self.write("deploy/topology.json", raw)
        self.failed("topology", "invalid_topology")
        raw["roles"][1]["readiness"]["listener"] = "control"
        self.write("deploy/topology.json", raw)
        value = self.read("deploy/environments/acceptance.json")
        value["listeners"]["relay.ingress"]["dns"]["mode"] = "managed"
        self.write("deploy/environments/acceptance.json", value)
        self.assertEqual(self.invoke()[0], 1)

    def test_shared_directory_declaration(self):
        raw = self.feature()
        raw["shared_directories"] = {"render-cache": {"state": "disposable", "max_bytes": 1048576,
                                                     "writers": ["http"], "readers": ["relay"]}}
        self.write("deploy/topology.json", raw)
        self.assertEqual(self.invoke()[0], 0)
        raw["shared_directories"]["render-cache"]["readers"] = ["missing"]
        self.write("deploy/topology.json", raw)
        self.failed("topology", "invalid_topology")

    def test_artifact_and_bundle_declarations_without_acquiring_payloads(self):
        raw = self.feature()
        raw["artifacts"] = {"extension": bundle()}
        self.write("deploy/topology.json", raw)
        self.assertEqual(self.invoke()[0], 0)
        for platform in raw["artifacts"]["extension"]["platforms"]:
            raw["artifacts"]["extension"]["platforms"][platform] = {"source": {"release": {
                "path": "deploy/artifacts/output.tar.gz", "build": ["never-execute"]}}}
        self.write("deploy/topology.json", raw)
        self.assertEqual(self.invoke()[0], 0)
        raw["artifacts"]["extension"].pop("provenance")
        self.write("deploy/topology.json", raw)
        self.failed("topology", "invalid_topology")

    def test_malformed_files_have_typed_errors_without_content(self):
        (self.root / "deploy/topology.json").write_text('{"private": "do-not-print"')
        result = self.failed("topology", "invalid_json")
        self.assertNotIn("do-not-print", json.dumps(result))


if __name__ == "__main__":
    unittest.main()
