"""Application rehearsal contracts without Docker, host actions or Go builds."""
import copy
import json
import os
from pathlib import Path
import subprocess
import sys
import tomllib
import unittest
from unittest.mock import Mock, patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "local"))
import containers
import rehearsal
import rehearsal_host
import rehearsal_probe
import upgrades
from contracts import DeployError, canonical_digest, digest, read_json
from test_build_inputs import BuildInputTests
from test_build_artifacts import FeatureBuildTests
from test_host_capabilities import declaration, profile as feature_profile
from test_local import cli, fixture, metadata
from topology import parse_topology


class RehearsalTests(unittest.TestCase):
    setUp = BuildInputTests.setUp
    request_and_stage = BuildInputTests.request_and_stage

    def test_deploy_acceptance_fixture_profile_passes_rehearsal_guards(self):
        root = Path(__file__).resolve().parents[3] / "tests/fixtures/deploy-acceptance"
        project, profile, topology, manifest, config = rehearsal.inputs(root, "acceptance-lab", None)
        parsed = rehearsal.acceptance_profile(project, profile, topology.projection())
        self.assertEqual(parsed.architecture, "arm64")
        self.assertIsNotNone(parsed.host_id)
        self.assertEqual(profile["environment"], "acceptance-lab")
        self.assertEqual(profile["runtime_environment"], "testing")
        self.assertEqual(profile["domain"], "acceptance-lab.acceptance.invalid")
        self.assertEqual(dict(parsed.placement), {"http": 1, "scheduler": 1, "worker": 1})
        existing = read_json(root / "deploy/environments/acceptance.json")
        self.assertNotEqual(profile["instance_id"], existing["instance_id"])
        self.assertIsNone(manifest)
        self.assertIsNone(config)
        rehearsal.network_plan(topology, profile)

    def test_deploy_acceptance_fixture_binds_all_external_disks(self):
        root = Path(__file__).resolve().parents[3] / "tests/fixtures/deploy-acceptance"
        path = self.root / "rehearsal-bindings.json"
        path.write_bytes((root / "deploy/acceptance-lab-bindings.json").read_bytes())
        path.chmod(0o600)
        config = tomllib.loads((root / "config/deploy.toml").read_text())
        settings, buckets = rehearsal.test_bindings(path, "acceptance-lab", config, "fixture")
        self.assertEqual(set(settings["app_storage"]["disks"]), {"attachments"})
        binding = settings["app_storage"]["disks"]["attachments"]["cloud"]
        self.assertEqual(binding["endpoint"], "https://s3:9000")
        self.assertEqual([binding["bucket"]], buckets)

    def application(self, feature=False):
        if feature:
            request, stage, _ = FeatureBuildTests.feature(self)
            # The minimal feature archive needs no remote artifact acquisition.
            request["topology"].pop("artifacts")
            request["topology"]["roles"][1].pop("artifact_mounts")
            request["topology"]["roles"][1].pop("bindings")
        else:
            request, stage, _ = self.request_and_stage()
            request["profile"] = fixture()[1]
            request["topology"] = {"schema": 1, "roles": [{"name": "http"}]}
        profile = request["profile"]
        profile.update(environment="acceptance-local", runtime_environment="testing", domain="app.acceptance.invalid",
                       edge_origin_lockdown=False, architecture="amd64")
        for binding in profile.get("listeners", {}).values():
            binding["dns"]["hostname"] = "relay.acceptance.invalid"
            binding["source_cidrs"] = ["203.0.113.20/32"]
        project = {"schema": 1, "id": request["manifest"]["project_id"], "slug": "abc"}
        request["manifest"].update(instance="abc-acceptance-local", instance_id=profile["instance_id"],
            environment=profile["environment"], topology_sha256=parse_topology(request["topology"]).sha256)
        deploy = self.root / "deploy"
        (deploy / "environments").mkdir(parents=True)
        for name, value in (("project.json", project), ("topology.json", request["topology"]),
                            ("environments/acceptance-local.json", profile)):
            (deploy / name).write_text(json.dumps(value))
        with patch.object(metadata, "collect_build_inputs", return_value={}):
            archive = metadata.package(request, stage, self.root / "archive")
        return archive, project, profile, request["topology"]

    def test_exact_legacy_archive_and_profile_without_modification(self):
        archive, project, profile, topology = self.application()
        before = {p: p.read_bytes() for p in (self.root / "deploy").rglob("*.json")}
        original = archive.read_bytes()
        actual = rehearsal.inputs(self.root, "acceptance-local", archive)
        self.assertEqual(actual[:2], (project, profile))
        self.assertEqual(actual[2].projection(), topology)
        self.assertEqual(before, {p: p.read_bytes() for p in before})
        self.assertEqual(original, archive.read_bytes())
        self.assertFalse((self.root / ".deploy/settings.json").exists())

    def test_feature_archive_profile_is_exact_including_host_identity(self):
        archive, _, profile, _ = self.application(feature=True)
        rehearsal.inputs(self.root, "acceptance-local", archive)
        profile["host"]["host_id"] = "10000000-0000-4000-8000-000000000001"
        path = self.root / "deploy/environments/acceptance-local.json"
        path.write_text(json.dumps(profile))
        with self.assertRaises(DeployError):
            rehearsal.inputs(self.root, "acceptance-local", archive)

    def test_production_unbound_and_nonacceptance_profiles_refuse_before_docker(self):
        archive, project, profile, topology = self.application()
        variants = [{**profile, "runtime_environment": "production"}, {**profile, "environment": "staging"},
                    {**profile, "domain": "app.example.com"},
                    {**profile, "host": {**profile["host"], "host_id": None}}]
        with patch.object(rehearsal, "run") as run:
            for value in variants:
                with self.subTest(value=value), self.assertRaises(DeployError):
                    rehearsal.acceptance_profile(project, value, topology)
            run.assert_not_called()
        with self.assertRaises(DeployError):
            rehearsal.inputs(self.root, None, archive)

    def test_missing_bindings_and_unsafe_file_refuse(self):
        config = {"app": {"services": {"storage": {"disks": {"uploads": {"driver": "r2"}}}}}}
        with self.assertRaises(DeployError):
            rehearsal.test_bindings(None, "acceptance-local", config, "a" * 16)
        path = self.root / "private.json"
        value = {"environment": "acceptance-local", "configuration": {}, "storage_disks": ["uploads"]}
        path.write_text(json.dumps(value))
        path.chmod(0o644)
        with self.assertRaises(DeployError):
            rehearsal.test_bindings(path, "acceptance-local", config, "a" * 16)
        path.chmod(0o600)
        settings, buckets = rehearsal.test_bindings(path, "acceptance-local", config, "a" * 16)
        disk = settings["app_storage"]["disks"]["uploads"]
        self.assertEqual(disk["cloud"]["endpoint"], "https://s3:9000")
        self.assertEqual(disk["cloud"]["bucket"], buckets[0])
        self.assertEqual(config["app"]["services"]["storage"]["disks"]["uploads"], {"driver": "r2"})

    def test_network_uses_profile_addresses_and_disjoint_peer_sources(self):
        topology, profile = parse_topology(declaration()), feature_profile()
        profile["listeners"]["relay.ingress"]["source_cidrs"] = ["203.0.113.20/32"]
        original = copy.deepcopy(profile)
        plan = rehearsal.network_plan(topology, profile)
        probe, = plan["probes"]
        self.assertEqual(plan["hosts"][0], "203.0.113.10")
        self.assertEqual(probe["allowed_peer"], "203.0.113.20")
        self.assertNotEqual(probe["denied_peer"], "203.0.113.20")
        self.assertEqual(profile, original)
        profile["listeners"]["relay.ingress"]["dns"]["public_addresses"] = ["8.8.8.8"]
        with self.assertRaises(DeployError):
            rehearsal.network_plan(topology, profile)

    def test_ipv6_peer_selection_and_unavailable_denial(self):
        import ipaddress
        subnet = ipaddress.ip_network("2001:db8::/64")
        self.assertEqual(rehearsal.peer_address(subnet, [ipaddress.ip_network("2001:db8::abcd/128")], set(), True),
                         "2001:db8::abcd")
        self.assertIsNone(rehearsal.peer_address(subnet, [subnet], set(), False))

    def test_rehearsal_orchestration_isolates_before_activate_and_keeps_identity(self):
        archive, project, profile, _ = self.application()
        calls, phases = [], []
        def command(arguments, **kwargs):
            calls.append([str(a) for a in arguments])
            output = "amd64" if arguments[:2] == ["docker", "info"] else "image-digest"
            return subprocess.CompletedProcess(arguments, 0, output, "")
        def phase(host, name, token):
            phases.append(name)
            if name == "activate":
                self.assertTrue(any(c[:3] == ["docker", "network", "disconnect"] for c in calls))
            return {"ok": True}
        output = self.root / "evidence"
        with patch.object(rehearsal, "run", side_effect=command), patch.object(rehearsal, "phase", side_effect=phase), \
                patch.object(rehearsal, "create_ca", side_effect=self.ca), \
                patch.object(rehearsal, "create_host") as create, patch.object(rehearsal, "boot_host"), \
                patch.object(containers.Resources, "cleanup", return_value=[]):
            rehearsal.rehearse(self.root, "acceptance-local", archive, output=output)
        self.assertEqual(phases, ["prepare", "bootstrap-plan", "bootstrap-apply", "activate", "observe", "stop", "stopped",
                                  "restart", "observe", "stop"])
        evidence = read_json(output / "evidence.json")
        self.assertEqual(evidence["status"], "passed")
        self.assertEqual(evidence["archive_sha256"], digest(archive))
        self.assertEqual(evidence["profile_sha256"], canonical_digest(profile))
        self.assertFalse((output / "private").exists())
        self.assertFalse((self.root / ".deploy/settings.json").exists())
        self.assertTrue(any("--internal" in c for c in calls))
        self.assertFalse(any("ssh" in c or "-p" in c or "--publish" in c for c in calls))
        self.assertEqual(create.call_args.kwargs["cpus"], 2)

    def ca(self, path):
        path.mkdir()
        for name in ("ca.crt", "ca.key", "public.crt", "private.key"):
            (path / name).write_text("test")

    def test_failed_run_retention_is_opt_in_and_reports_exact_phase(self):
        archive, _, _, _ = self.application()
        for keep in (False, True):
            output = self.root / ("evidence-" + str(keep))
            with patch.object(rehearsal, "run", return_value=subprocess.CompletedProcess([], 0, "amd64", "")), \
                    patch.object(rehearsal, "create_ca", side_effect=self.ca), patch.object(rehearsal, "create_host"), \
                    patch.object(rehearsal, "boot_host"), patch.object(rehearsal, "phase", return_value={"ok": False, "error": "test_refusal"}), \
                    patch.object(containers.Resources, "cleanup", return_value=[]) as cleanup, self.assertRaises(DeployError):
                rehearsal.rehearse(self.root, "acceptance-local", archive, output=output, keep_failed=keep)
            evidence = read_json(output / "evidence.json")
            self.assertEqual(evidence["status"], "failed")
            self.assertEqual(evidence["phases"][0]["error"], "test_refusal")
            self.assertEqual((output / "private").exists(), keep)
            self.assertEqual(cleanup.call_count, 0 if keep else 1)

    def test_cli_rehearsal_bypasses_settings_and_ssh(self):
        archive, _, _, _ = self.application()
        arguments = ["cli.py", "--app-root", str(self.root), "rehearse", "--environment", "acceptance-local",
                     "--archive", str(archive), "--ubuntu", "26.04"]
        with patch.object(sys, "argv", arguments), patch.dict(os.environ, {}, clear=True), \
                patch.object(cli, "local_settings", side_effect=AssertionError("no settings")), \
                patch.object(cli, "SSH", side_effect=AssertionError("no SSH")), \
                patch.object(rehearsal, "rehearse", return_value=self.root) as execute:
            cli.main()
        self.assertEqual(execute.call_args.args[:3], (self.root, "acceptance-local", archive))
        self.assertEqual(execute.call_args.kwargs["ubuntu"], "26.04")

    def test_bundle_ships_runner_assets_without_test_directory(self):
        source, files, _ = upgrades.tool_bundle(Path(__file__).resolve().parents[1])
        inventory = {p.relative_to(source).as_posix() for p in files}
        self.assertTrue(upgrades.REHEARSAL_FILES <= inventory)
        self.assertFalse(any(p.startswith("tests/") for p in inventory))
        for name in upgrades.REHEARSAL_FILES:
            if name.endswith(".py"):
                self.assertNotIn("from acceptance_", (source / name).read_text())

    def test_public_checks_use_separate_owned_containers_for_allow_and_deny(self):
        topology, profile = parse_topology(declaration()), feature_profile()
        profile["listeners"]["relay.ingress"]["source_cidrs"] = ["203.0.113.20/32"]
        plan = rehearsal.network_plan(topology, profile)
        resources = containers.Resources(self.root)
        with patch.object(rehearsal, "run", return_value=subprocess.CompletedProcess([], 0, '{"ok":true}', "")) as run:
            results = rehearsal.public_checks(resources, "image", "network", "linux/amd64", self.root,
                                               self.root, plan, True)
        self.assertEqual([r["status"] for r in results], ["passed", "passed"])
        self.assertEqual([r["expected_open"] for r in results], [True, False])
        for call in run.call_args_list:
            args = call.args[0]
            self.assertEqual(args[:2], ["docker", "run"])
            self.assertIn(resources.label, args)
            self.assertIn("--cap-drop=ALL", args)
            self.assertNotIn("--privileged", args)
        self.assertIn("203.0.113.20", run.call_args_list[0].args[0])

    def test_bootstrap_action_preserves_profile_and_uses_production_dispatcher(self):
        _, project, profile, topology = self.application()
        context = {"project": project, "profile": profile}
        original = copy.deepcopy(context)
        with patch.object(rehearsal_host, "run", return_value=subprocess.CompletedProcess([], 0, '{"ok":true}', "")) as run:
            rehearsal_host.action("bootstrap-plan", context, bootstrap=True, topology=topology)
        self.assertTrue(run.call_args.args[0][2].endswith("server/manage.py"))
        payload = json.loads(run.call_args.kwargs["input"])
        self.assertEqual(payload["profile"], profile)
        self.assertEqual(context, original)

    def test_native_architecture_refuses_before_resource_creation(self):
        archive, _, _, _ = self.application()
        with patch.object(rehearsal, "run", return_value=subprocess.CompletedProcess([], 0, "aarch64", "")), \
                patch.object(rehearsal, "Resources") as resources, self.assertRaises(DeployError):
            rehearsal.rehearse(self.root, "acceptance-local", archive)
        resources.assert_not_called()

    def test_cleanup_failure_preserves_private_files_and_fails_run(self):
        archive, _, _, _ = self.application()
        output = self.root / "failed-cleanup"
        with patch.object(rehearsal, "run", return_value=subprocess.CompletedProcess([], 0, "amd64", "")), \
                patch.object(rehearsal, "create_ca", side_effect=self.ca), patch.object(rehearsal, "create_host"), \
                patch.object(rehearsal, "boot_host"), patch.object(rehearsal, "phase", return_value={"ok": True}), \
                patch.object(containers.Resources, "cleanup", return_value=["owned-host"]), self.assertRaises(DeployError):
            rehearsal.rehearse(self.root, "acceptance-local", archive, output=output)
        self.assertTrue((output / "private").exists())
        self.assertEqual(read_json(output / "evidence.json")["cleanup_failures"], ["owned-host"])

    def test_shared_acceptance_runner_uses_shipped_container_helpers(self):
        runner = Path(__file__).with_name("run_acceptance.sh").read_text()
        for call in ('python3 "$containers" ca', 'python3 "$containers" boot', 'python3 "$containers" create-host'):
            self.assertIn(call, runner)
        self.assertNotIn("openssl req", runner)
        self.assertNotIn('"$tests/host.Dockerfile"', runner)


class ContainerOwnershipTests(unittest.TestCase):
    setUp = BuildInputTests.setUp

    def test_recovery_targets_start_without_installing_foundry_resources(self):
        runner = Path(__file__).with_name("run_acceptance.sh").read_text()
        setup = runner[runner.index("recovery_target() {"):runner.index('recovery_target "$rehearsal" rehearsal')]
        for mode in ("rehearsal", "takeover"):
            with self.subTest(mode=mode):
                subprocess.run(["bash", "-c", """
set -eu
logs=$1
containers=$2
mode=$3
platform=linux/arm64
network=owned-network
framework=/fixture
base_image=base-image
certs=/synthetic-certs
recovery_hosts=()
python3() {
  if [[ "$2" != create-host ]]; then
    printf '%s\n' "$@" > "$logs/start-args"
  fi
}
docker() { :; }
phase_on() { :; }
""" + setup + '\nrecovery_target recovery-host "$mode"\n', "recovery-setup",
                    str(self.root), str(containers.ASSETS / "containers.py"), mode],
                    capture_output=True, text=True, check=True, timeout=5)
                arguments = (self.root / "start-args").read_text().splitlines()
                with patch.object(sys, "argv", arguments), patch.object(containers, "run",
                        return_value=subprocess.CompletedProcess([], 0, "", "")) as run:
                    containers.main()
                self.assertEqual([call.args[0] for call in run.call_args_list], [
                    ["docker", "start", "recovery-host"],
                    ["docker", "exec", "recovery-host", "test", "-S", "/run/systemd/private"],
                    ["docker", "exec", "recovery-host", "systemctl", "start", "multi-user.target"],
                ])

    def test_application_host_boot_retains_synthetic_ca_setup(self):
        with patch.object(containers, "run", return_value=subprocess.CompletedProcess([], 0, "", "")) as run:
            containers.boot_host("app-host", self.root)
        calls = [call.args[0] for call in run.call_args_list]
        self.assertIn(["docker", "exec", "app-host", "systemctl", "start", "multi-user.target"], calls)
        for name in ("ca.crt", "ca.key"):
            self.assertIn(["docker", "cp", self.root / name, "app-host:/etc/foundry/acceptance-ca/" + name], calls)
        self.assertIn(["docker", "exec", "app-host", "chmod", "600",
                       "/etc/foundry/acceptance-ca/ca.key", "/etc/foundry/acceptance-ca/ca.crt"], calls)
        self.assertEqual(calls[-1], ["docker", "exec", "app-host", "update-ca-certificates"])

    def test_cleanup_refuses_foreign_names_and_removes_only_label_owned_resources(self):
        resources = containers.Resources(self.root)
        foreign = resources.reserve("container", "foreign")
        owned = resources.reserve("network", "owned")
        calls = []
        def run(args, **kwargs):
            calls.append(args)
            if "inspect" in args:
                labels = {containers.LABEL: resources.token if args[-1] == owned else "other"}
                value = {"Config": {"Labels": labels}} if args[1] == "container" else {"Labels": labels}
                return subprocess.CompletedProcess(args, 0, json.dumps([value]), "")
            return subprocess.CompletedProcess(args, 0, "", "")
        with patch.object(containers, "run", side_effect=run):
            self.assertEqual(resources.cleanup(), [foreign])
        self.assertIn(["docker", "network", "rm", owned], calls)
        self.assertFalse(any("rm" in call and foreign in call for call in calls))

    def test_missing_or_daemon_unavailable_cleanup_never_claims_success_blindly(self):
        resources = containers.Resources(self.root)
        name = resources.reserve("container", "not-created")
        with patch.object(containers, "run", side_effect=[subprocess.CompletedProcess([], 1, "", ""),
                subprocess.CompletedProcess([], 0, "", "")]):
            self.assertEqual(resources.cleanup(), [])
        with patch.object(containers, "run", return_value=subprocess.CompletedProcess([], 1, "", "")):
            self.assertEqual(resources.cleanup(), [name])

    def test_disposable_guard_refuses_without_container_marker(self):
        with patch.object(rehearsal_host.os, "geteuid", return_value=0), \
                patch.dict(os.environ, {"FOUNDRY_DISPOSABLE_HOST": "1", "FOUNDRY_ACCEPTANCE_SYNTHETIC_CERT": "1"}), \
                patch.object(Path, "is_file", return_value=False), self.assertRaises(DeployError):
            rehearsal_host.guard()

    def test_command_timeout_kills_owned_process_group(self):
        process = Mock(pid=1234, returncode=-9)
        process.communicate.side_effect = subprocess.TimeoutExpired("docker", 1)
        with patch.object(containers.subprocess, "Popen", return_value=process), \
                patch.object(containers.os, "killpg") as kill, self.assertRaises(containers.ContainerError):
            containers.run(["docker", "info"], timeout=1)
        kill.assert_called_once_with(1234, containers.signal.SIGKILL)
        process.wait.assert_called_once()

    def test_denied_probe_requires_refusal_or_timeout_and_never_accepts_connection(self):
        with patch.object(rehearsal_probe.socket, "create_connection", side_effect=ConnectionRefusedError):
            self.assertTrue(rehearsal_probe.probe("203.0.113.10", 9443, "relay.acceptance.invalid", "none", False)["ok"])
            self.assertFalse(rehearsal_probe.probe("203.0.113.10", 9443, "relay.acceptance.invalid", "none", True)["ok"])
        connection = Mock()
        connection.__enter__ = Mock(return_value=connection)
        connection.__exit__ = Mock(return_value=False)
        with patch.object(rehearsal_probe.socket, "create_connection", return_value=connection):
            self.assertFalse(rehearsal_probe.probe("203.0.113.10", 9443, "relay.acceptance.invalid", "none", False)["ok"])
