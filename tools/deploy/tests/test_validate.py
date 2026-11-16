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

from test_local import cli, metadata
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

    def invoke(self, *, environment=None, format="json", flags=(), action="validate", menu=None):
        stdout, stderr = io.StringIO(), io.StringIO()
        environ = {"FORMAT": format,
                   "FOUNDRY_SECRET_CONFIG_FILE": str(self.root / "must-not-read")}
        if action is not None:
            environ["ACTION"] = action
        if environment is not None:
            environ["ENVIRONMENT"] = environment
        with ExitStack() as stack:
            stack.enter_context(patch.dict(os.environ, environ, clear=True))
            stack.enter_context(patch.object(sys, "argv", ["cli.py", "--app-root", str(self.root), "deploy", *flags]))
            stack.enter_context(patch.object(cli, "ROOT", None))
            for target in ("socket.socket", "socket.getaddrinfo", "subprocess.Popen"):
                stack.enter_context(patch(target, side_effect=AssertionError("offline action attempted external I/O")))
            for name in ("lock", "local_settings", "secret_input", "SSH", "build", "remember_releases"):
                stack.enter_context(patch.object(cli, name, side_effect=AssertionError("offline action entered runtime")))
            def answer(label, default=None):
                self.assertTrue(label.startswith("Action ("), label)
                self.assertIsNotNone(menu, "unexpected prompt")
                return menu
            ask = stack.enter_context(patch.object(cli, "ask", side_effect=answer))
            stack.enter_context(patch.object(runtime_config, "_read_value_file", side_effect=AssertionError("private read")))
            stack.enter_context(redirect_stdout(stdout))
            stack.enter_context(redirect_stderr(stderr))
            status = cli.main()
            self.assertEqual(ask.call_count, 1 if menu is not None else 0)
        self.assertEqual(stderr.getvalue(), "")
        self.assertNotIn(str(self.root), stdout.getvalue())
        return status, json.loads(stdout.getvalue()) if format == "json" else stdout.getvalue()

    def forbid_open(self, path):
        stack = ExitStack()
        for module, name in (("builtins", "open"), ("io", "open"), ("os", "open")):
            original = getattr(__import__(module), name)
            def guarded(filename, *args, _original=original, **kwargs):
                if not isinstance(filename, int):
                    self.assertNotEqual(Path(filename), path, "private or linked declaration was opened")
                    if kwargs.get("dir_fd") is not None:
                        expected = path.lstat()
                        actual = os.stat(filename, dir_fd=kwargs["dir_fd"], follow_symlinks=False)
                        self.assertNotEqual((actual.st_dev, actual.st_ino), (expected.st_dev, expected.st_ino),
                                            "private or linked declaration was opened relative to a directory")
                return _original(filename, *args, **kwargs)
            stack.enter_context(patch(module + "." + name, side_effect=guarded))
        return stack

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

    def test_rehearsal_maintenance_graph_is_checked_offline(self):
        topology = self.read("deploy/rehearsal/topology.json")
        self.write("deploy/topology.json", topology)
        self.write("deploy/environments/acceptance-run.json",
                   self.read("deploy/rehearsal/environments/acceptance-run.json"))
        status, result = self.invoke(environment="acceptance-run")
        self.assertEqual(status, 0, result)
        self.assertTrue(all(item["status"] == "passed" for item in result["checks"]))

        topology["roles"][1]["maintenance"] = "stop"
        self.write("deploy/topology.json", topology)
        result = self.failed("topology", "maintenance_graph", environment="acceptance-run")
        check = next(item for item in result["checks"] if item["kind"] == "topology")
        self.assertEqual(check["errors"], [{"code": "maintenance_graph", "detail":
            'Role http continues but requires stopped role relay; declare relay maintenance: "continue", '
            'or remove that requires prerequisite from http.'}])
        self.assertTrue(all(item["status"] == "skipped" for item in result["checks"]
                            if item["kind"] in {"profile", "release"}))
        self.assertEqual(self.read("deploy/topology.json"), topology)

    def test_config_tree_validates_every_source_and_builds_from_snapshot(self):
        raw = self.feature()
        tree = self.read("deploy/rehearsal/topology.json")["roles"][1]["configs"]["fixture-home"]
        raw["roles"][1]["configs"] = {"settings": tree}
        raw["roles"][1]["secret_files"] = {"TOKEN_FILE": "token"}
        raw["secrets"] = {"token": {}}
        # Use an existing package executable so this source inventory test needs no compiler.
        raw["roles"][1]["packages"] = ["coreutils"]
        raw["roles"][1]["executable"] = {"package": "coreutils", "path": "/usr/bin/sha256sum"}
        raw.pop("application_executables")
        self.write("deploy/topology.json", raw)
        status, result = self.invoke()
        self.assertEqual(status, 0, result)
        self.assertEqual(result["minimum_generation"], 19)
        sources = {file["source"] for file in tree["files"].values()}
        checked = {item["path"] for item in result["checks"] if item["kind"] == "service_configuration"}
        self.assertEqual(checked, sources)
        request = {"profile": self.read("deploy/environments/acceptance.json")}
        stage = self.root / "stage"
        metadata.prepare(request, self.root, stage)
        self.assertEqual({path.relative_to(stage).as_posix() for path in stage.rglob("*") if path.is_file()}, sources)
        for source in sorted(sources):
            path = self.root / source
            self.assertEqual(path.read_bytes(), (stage / source).read_bytes())
            data = path.read_bytes()
            path.unlink()
            self.failed("service_configuration", "declaration_file")
            with self.assertRaises(cli.DeployError) as caught:
                metadata.prepare(request, self.root, self.root / ("missing-" + path.name))
            self.assertEqual(caught.exception.code, "build_source")
            path.write_bytes(data)

    def test_every_action_selector_dispatches_before_local_or_remote_state(self):
        for options in ({}, {"action": None, "menu": "validate"}, {"action": "", "menu": "validate"},
                        {"action": None, "flags": ("--action", "validate")},
                        {"action": "deploy", "flags": ("--action=validate",)},
                        {"action": None, "flags": ("--act", "validate")}):
            with self.subTest(options=options):
                status, result = self.invoke(**options)
                self.assertEqual(status, 0, result)
                self.assertTrue(result["ok"])
                self.assertFalse((self.root / ".deploy").exists())

    def test_remote_dispatch_refuses_validate_without_opening_ssh(self):
        with patch.dict(os.environ, {"ACTION": "validate"}), \
                patch.object(cli, "SSH", side_effect=AssertionError("SSH opened")), \
                self.assertRaisesRegex(cli.LocalError, "before loading deployment state"):
            cli.deploy(None, None, None)

    def test_makefile_example_stdout_is_exact_json(self):
        shutil.copyfile(Path(cli.__file__).with_name("Makefile.example"), self.root / "Makefile")
        tools = self.root / "test-tools"
        tools.mkdir()
        go = tools / "go"
        # Exercise the actual make recipe and Python entry point without running Go.
        go.write_text('#!' + sys.executable + '\nimport os, sys\n'
                      'os.execv(sys.executable, [sys.executable, "-B", ' + repr(str(Path(cli.__file__)))
                      + ', "--app-root", sys.argv[5], sys.argv[6]])\n')
        go.chmod(0o755)
        result = subprocess.run(["make", "deploy"], cwd=self.root, capture_output=True, text=True, timeout=15,
                                env={"PATH": str(tools) + os.pathsep + os.environ["PATH"],
                                     "ACTION": "validate", "FORMAT": "json", "PYTHONDONTWRITEBYTECODE": "1"})
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertTrue(json.loads(result.stdout)["ok"])

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
        self.failed("profile", "invalid_profile", environment="acceptance-lab")

    def test_unknown_or_unsafe_profile_selector(self):
        self.failed("profile", "declaration_file", environment="missing")
        self.failed("selection", "invalid_profile", environment="../private")

    def test_profile_filename(self):
        value = self.read("deploy/environments/acceptance.json")
        value["environment"] = "staging"
        self.write("deploy/environments/acceptance.json", value)
        self.failed("profile", "invalid_profile")

    def test_single_environment_reuses_release_sibling_identity_checks(self):
        project = self.read("deploy/project.json")
        value = self.read("deploy/environments/acceptance.json")
        value["instance_id"] = self.read("deploy/environments/acceptance-lab.json")["instance_id"]
        self.write("deploy/environments/acceptance.json", value)
        for environment in ("acceptance", "acceptance-lab"):
            with self.subTest(environment=environment), patch.object(cli, "ROOT", self.root):
                result = self.failed("profile", "invalid_profile", environment=environment)
                profiles = [c for c in result["checks"] if c["kind"] == "profile"]
                self.assertEqual([c["profile"] for c in profiles], [environment])
                with self.assertRaises(cli.LocalError) as raised:
                    cli.profile_config(environment, project)
                self.assertEqual(profiles[0]["errors"][0]["detail"], str(raised.exception))

    def test_single_environment_checks_sibling_filename(self):
        value = self.read("deploy/environments/acceptance.json")
        value["environment"] = "staging"
        self.write("deploy/environments/acceptance.json", value)
        self.failed("profile", "invalid_profile", environment="acceptance-lab")

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

    def test_private_bindings_are_refused_before_open(self):
        from declaration_inputs import declaration_allowed
        for name in (".deploy/settings.json", ".DEPLOY/settings.json", "secrets/bindings.json", "SeCrEtS/bindings.json", "deploy/secrets/bindings.json",
                     ".secrets/bindings.json", "deploy/service-secrets/value", "deploy/copy-secrets/value",
                     "deploy/bootstrap-secrets.json", ".ssh/config", ".aws/credentials", ".netrc", ".npmrc",
                     ".env", "config/app.toml", "config/APP.toml", "config/staging.local.toml", ".claude/settings.local.json",
                     "deploy/value.secret", "deploy/value.key", "storage/bindings.json"):
            with self.subTest(path=name):
                path = self.root / name
                self.write(name, {"private": "must-not-read"})
                self.assertFalse(declaration_allowed(name))
                with self.forbid_open(path):
                    result = self.failed("rehearsal", "unsafe_path", environment="acceptance-lab", flags=("--bindings", name))
                self.assertNotIn("must-not-read", json.dumps(result))

    def test_binding_links_and_special_files_are_refused_before_open(self):
        path = self.root / "deploy/other-bindings.json"
        source = self.root / "deploy/acceptance-lab-bindings.json"
        for kind in ("symlink", "hardlink", "directory", "fifo"):
            with self.subTest(kind=kind):
                if kind == "symlink":
                    path.symlink_to(source)
                elif kind == "hardlink":
                    os.link(source, path)
                elif kind == "directory":
                    path.mkdir()
                else:
                    os.mkfifo(path)
                try:
                    with self.forbid_open(path):
                        self.failed("rehearsal", "unsafe_path", environment="acceptance-lab",
                                    flags=("--bindings", "deploy/other-bindings.json"))
                finally:
                    path.rmdir() if kind == "directory" else path.unlink()

    def test_public_binding_swapped_to_hardlink_before_open_is_not_read(self):
        import declaration_inputs
        path = self.root / "deploy/acceptance-lab-bindings.json"
        original = os.open
        def swap(filename, *args, **kwargs):
            if filename == path.name and kwargs.get("dir_fd") is not None:
                os.link(path, self.root / "secret-alias")
            return original(filename, *args, **kwargs)
        with patch.object(declaration_inputs.os, "open", side_effect=swap):
            self.failed("rehearsal", "unsafe_path", environment="acceptance-lab")

    def test_parent_swapped_to_private_symlink_after_path_check_is_never_read(self):
        import declaration_inputs
        public = self.root / "deploy/public-bindings"
        public.mkdir()
        path = public / "settings.json"
        self.write("deploy/public-bindings/settings.json", self.read("deploy/acceptance-lab-bindings.json"))
        self.write(".deploy/settings.json", {"private": "must-not-read"})
        original = declaration_inputs.check_path
        swapped = []
        def swap(checked, kind=None):
            result = original(checked, kind)
            if checked == path:
                public.rename(public.with_name("saved-public-bindings"))
                public.symlink_to(self.root / ".deploy", target_is_directory=True)
                swapped.append(True)
            return result
        with patch.object(declaration_inputs, "check_path", side_effect=swap), \
                self.forbid_open(self.root / ".deploy/settings.json"):
            result = self.failed("rehearsal", "unsafe_path", environment="acceptance-lab",
                                 flags=("--bindings", "deploy/public-bindings/settings.json"))
        self.assertEqual(swapped, [True])
        self.assertNotIn("must-not-read", json.dumps(result))

    def test_opened_parent_stays_pinned_when_its_path_becomes_a_private_symlink(self):
        import declaration_inputs
        public = self.root / "deploy/public-bindings"
        public.mkdir()
        self.write("deploy/public-bindings/settings.json", self.read("deploy/acceptance-lab-bindings.json"))
        self.write(".deploy/settings.json", {"private": "must-not-read"})
        original = os.open
        swapped = []
        def swap(filename, *args, **kwargs):
            descriptor = original(filename, *args, **kwargs)
            if filename == public.name and kwargs.get("dir_fd") is not None:
                public.rename(public.with_name("saved-public-bindings"))
                public.symlink_to(self.root / ".deploy", target_is_directory=True)
                swapped.append(True)
            return descriptor
        with patch.object(declaration_inputs.os, "open", side_effect=swap), \
                self.forbid_open(self.root / ".deploy/settings.json"):
            status, result = self.invoke(environment="acceptance-lab", flags=("--bindings", "deploy/public-bindings/settings.json"))
        self.assertEqual(swapped, [True])
        self.assertEqual(status, 0, result)
        self.assertNotIn("must-not-read", json.dumps(result))

    def test_single_environment_refuses_hardlinked_sibling_before_open(self):
        path = self.root / "deploy/environments/acceptance.json"
        os.link(path, self.root / "private-alias")
        with self.forbid_open(path):
            self.failed("profile", "unsafe_path", environment="acceptance-lab")

    def test_declared_release_paths_use_preparation_safety_rules(self):
        raw = self.feature()
        for name in ("server.key", "server.pem", ".env/server", "node_modules/server"):
            with self.subTest(path=name):
                raw["application_executables"]["relay-server"] = {"platforms": {
                    platform: "deploy/bin/" + name for platform in ("linux-amd64", "linux-arm64")}}
                self.write("deploy/topology.json", raw)
                result = self.failed("release", "artifact_private", environment="acceptance")
                with self.assertRaises(cli.DeployError) as raised, \
                        patch.object(metadata.subprocess, "run", side_effect=AssertionError("build started")):
                    metadata.prepare({"profile": self.read("deploy/environments/acceptance.json")},
                                     self.root, self.root / "stage")
                self.assertEqual(raised.exception.code, next(c["errors"][0]["code"] for c in result["checks"] if c["kind"] == "release"))

    def test_release_export_collisions_and_private_bundle_exports(self):
        raw = self.feature()
        for paths in (("deploy/bin/relay", "deploy/bin/relay"), ("deploy/bin/relay", "deploy/bin/relay/child")):
            with self.subTest(paths=paths):
                raw["application_executables"] = {name: {"platforms": {p: path for p in ("linux-amd64", "linux-arm64")}}
                                                  for name, path in zip(("relay-server", "other"), paths)}
                self.write("deploy/topology.json", raw)
                self.failed("release", "artifact_inventory")
        raw = self.feature()
        raw["artifacts"] = {"extension": bundle()}
        raw["artifacts"]["extension"]["bundles"]["private"] = ".env"
        self.write("deploy/topology.json", raw)
        self.failed("release", "artifact_private")

    def test_duplicate_release_artifact_sources_use_preparation_checks(self):
        raw = self.feature()
        artifact = bundle()
        artifact["platforms"] = {platform: {"source": {"release": {"path": "deploy/artifacts/shared.tar.gz"}}}
                                 for platform in ("linux-amd64", "linux-arm64")}
        raw["artifacts"] = {"first": artifact, "second": artifact}
        self.write("deploy/topology.json", raw)
        self.failed("release", "build_source")
        with self.assertRaises(cli.DeployError) as raised:
            metadata.prepare({"profile": self.read("deploy/environments/acceptance.json")}, self.root, self.root / "stage")
        self.assertEqual(raised.exception.code, "build_source")

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
