"""Schema-2 archives, using local payload bytes and fake compiler provenance."""
from dataclasses import asdict
import json
import hashlib
from pathlib import Path
import shutil
import subprocess
import unittest
from unittest.mock import patch

from test_build_inputs import BuildInputTests
from test_local import metadata
from test_host_capabilities import declaration, profile
from test_artifacts import bundle_fixture, elf, PUBLIC, URL
import artifacts
from contracts import DeployError, ReleaseDeclaration
from topology import parse_topology


class FeatureBuildTests(unittest.TestCase):
    setUp = BuildInputTests.setUp
    request_and_stage = BuildInputTests.request_and_stage

    def feature(self):
        request, stage, entry = self.request_and_stage()
        artifact, content = bundle_fixture()
        raw = declaration()
        raw["artifacts"] = {"extension": {
            "version": artifact.version, "format": artifact.format,
            "platforms": {name: asdict(pin) for name, pin in artifact.platforms},
            "bundles": dict(artifact.bundles), "modules": list(artifact.modules), "provenance": asdict(artifact.provenance)}}
        raw["roles"][1]["artifact_mounts"] = {"extension": {"artifact": "extension", "bundle": "plugin", "area": "config", "path": "plugins/extension"}}
        raw["roles"][1]["bindings"] = {"plugin": {"bundle": "extension"}}
        request.update(topology=raw, profile={**profile(), "architecture": "amd64"})
        request["manifest"].update(instance_id=request["profile"]["instance_id"], environment=request["profile"]["environment"])
        for name, data in {"artisan": elf(), "deploy/bin/relay-server": elf(),
                           "deploy/files/relay.json": b'{"listen":{"address":"127.0.0.1","port":1}}'}.items():
            target = stage / name
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(data)
        return request, stage, content

    def download(self, content):
        def acquire(pin, destination, *, budget, budget_path):
            self.assertEqual(hashlib.sha256(content).hexdigest(), pin.sha256)
            budget.charge(budget_path, len(content))
            destination.write_bytes(content)
            return (artifacts.DownloadOrigin(URL, PUBLIC),)
        return acquire

    def package(self, request, stage, content, output):
        with patch.object(metadata, "collect_build_inputs", return_value={"fixture": "offline"}), \
                patch.object(metadata.artifacts, "download", side_effect=self.download(content)):
            return metadata.package(request, stage, output)

    def test_archive_carries_exact_payload_inventory_exports_and_provenance(self):
        request, stage, content = self.feature()
        archive = self.package(request, stage, content, self.root / "output")
        manifest = json.loads((archive.parent / "manifest.json").read_text())
        self.assertEqual(manifest["schema"], 2)
        deployment = ReleaseDeclaration.parse(manifest["deployment"])
        self.assertEqual(deployment.topology_sha256, parse_topology(request["topology"]).sha256)
        self.assertEqual(dict(deployment.application_executables), {"relay-server": "deploy/bin/relay-server"})
        self.assertEqual({m.path: m.sha256 for m in deployment.inventory}, manifest["files"])
        payload = deployment.artifacts[0]
        self.assertEqual((stage / payload.path).read_bytes(), content)
        self.assertEqual(manifest["artifact_origins"], {"extension": [{"url": URL, "address": PUBLIC}]})
        self.assertEqual(manifest["deployment"]["topology"]["artifacts"]["extension"]["provenance"]["service_abi"], "service-1")
        self.assertEqual(manifest["deployment"]["topology"]["roles"][1]["artifact_mounts"]["extension"]["area"], "config")
        with metadata.tarfile.open(archive) as tar:
            self.assertEqual(tar.extractfile(payload.path).read(), content)
            self.assertEqual(tar.getmember("deploy/bin/relay-server").mode, 0o755)
            self.assertEqual({m.name for m in tar if m.isfile()}, set(manifest["files"]) | {"manifest.json"})
        artifacts.verify_release_tree(deployment, stage, artifacts.ExpansionBudget())
        (stage / payload.path).write_bytes(content + b"tampered")
        with self.assertRaises(DeployError):
            artifacts.verify_release_tree(deployment, stage, artifacts.ExpansionBudget())

    def test_unrequested_deploy_files_never_enter_archive(self):
        request, stage, content = self.feature()
        (stage / "deploy/unrequested.json").write_text("{}")
        with self.assertRaises(ValueError):
            self.package(request, stage, content, self.root / "output")
        self.assertFalse((self.root / "output/app.tar.gz").exists())

    def test_profile_and_topology_drift_cannot_be_packaged_as_legacy(self):
        request, stage, content = self.feature()
        for changes in ({"topology": {"schema": 99, "roles": []}}, {"topology": {"schema": 1, "roles": [{"name": "http"}]}}):
            with self.subTest(changes=changes), self.assertRaises(DeployError):
                self.package({**request, **changes}, stage, content, self.root / "output")
        request["manifest"]["topology_sha256"] = "0" * 64
        with self.assertRaises(DeployError):
            self.package(request, stage, content, self.root / "output")

    def test_missing_or_wrong_architecture_application_binary_refuses(self):
        request, stage, content = self.feature()
        (stage / "deploy/bin/relay-server").write_bytes(elf("linux-arm64"))
        with self.assertRaises(DeployError):
            self.package(request, stage, content, self.root / "output")
        self.assertFalse((self.root / "output/app.tar.gz").exists())

    def test_host_rejects_modified_inner_inventory_even_with_matching_outer_payload(self):
        request, stage, content = self.feature()
        archive = self.package(request, stage, content, self.root / "output")
        raw = json.loads((archive.parent / "manifest.json").read_text())["deployment"]
        raw["artifacts"][0]["members"][0]["sha256"] = "b" * 64
        with self.assertRaises(DeployError):
            artifacts.verify_release_tree(ReleaseDeclaration.parse(raw), stage, artifacts.ExpansionBudget())

    def test_host_shared_budget_includes_outer_and_all_nested_payloads(self):
        request, stage, content = self.feature()
        archive = self.package(request, stage, content, self.root / "output")
        declaration = ReleaseDeclaration.parse(json.loads((archive.parent / "manifest.json").read_text())["deployment"])
        outer = sum(p.stat().st_size for p in stage.rglob("*") if p.is_file())
        with self.assertRaises(DeployError):
            artifacts.verify_release_tree(declaration, stage, artifacts.ExpansionBudget(max_bytes=outer))

    def test_prepare_builds_declared_go_commands_and_copies_only_named_sources(self):
        request, stage, _ = self.feature()
        work = self.root / "source"
        (work / "deploy/files").mkdir(parents=True)
        (work / "deploy/files/relay.json").write_text('{"listen":{"address":"127.0.0.1","port":1}}')
        (work / "deploy/files/unused.json").write_text("private")
        (work / "deploy/topology.json").write_text(json.dumps(request["topology"]))
        shutil.rmtree(stage / "deploy")
        def build(args, **kwargs):
            self.assertEqual(args[-1], "./cmd/relay-server")
            self.assertEqual(kwargs["env"]["GOARCH"], "amd64")
            self.assertEqual(kwargs["env"]["GOWORK"], "off")
            Path(args[args.index("-o") + 1]).write_bytes(elf())
            return subprocess.CompletedProcess(args, 0)
        with patch.object(metadata.subprocess, "run", side_effect=build) as compiler:
            prepared = metadata.prepare(request, work, stage)
        self.assertEqual(compiler.call_count, 1)
        self.assertEqual(prepared["topology"], json.loads(parse_topology(request["topology"]).canonical_json))
        self.assertFalse((stage / "deploy/files/unused.json").exists())
        self.assertEqual((stage / "deploy/bin/relay-server").read_bytes(), elf())

    def test_prepare_rejects_symlink_source_ancestry_without_following_it(self):
        request, stage, _ = self.feature()
        work = self.root / "source"
        (work / "deploy").mkdir(parents=True)
        (work / "deploy/topology.json").write_text(json.dumps(request["topology"]))
        (work / "deploy/files").symlink_to(stage / "deploy/files")
        shutil.rmtree(stage / "deploy/bin")
        with patch.object(metadata.subprocess, "run"), self.assertRaises(DeployError):
            metadata.prepare(request, work, stage)
