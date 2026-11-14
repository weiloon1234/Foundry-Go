"""Schema-2 archives, using local payload bytes and fake compiler provenance."""
from dataclasses import asdict
import json
import hashlib
import os
from pathlib import Path
import shutil
import subprocess
import unittest
from unittest.mock import Mock, patch

from test_build_inputs import BuildInputTests
from test_local import metadata
from test_host_capabilities import declaration, profile
from test_artifacts import bundle_fixture, elf, PUBLIC, URL
import artifacts
from contracts import DeployError, ReleaseDeclaration
from topology import ArtifactPlatform, parse_topology
from host import Host, Paths


class FeatureBuildTests(unittest.TestCase):
    setUp = BuildInputTests.setUp
    request_and_stage = BuildInputTests.request_and_stage

    def verify(self, declaration, stage, budget):
        staging = self.root / "prepared"
        self.addCleanup(lambda: artifacts._remove_staging(staging))
        return artifacts.verify_release_tree(declaration, stage, budget, artifact_staging=staging)

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
        self.verify(deployment, stage, artifacts.ExpansionBudget())
        (stage / payload.path).write_bytes(content + b"tampered")
        with self.assertRaises(DeployError):
            self.verify(deployment, stage, artifacts.ExpansionBudget())

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
            self.verify(ReleaseDeclaration.parse(raw), stage, artifacts.ExpansionBudget())

    def test_host_shared_budget_includes_outer_and_all_nested_payloads(self):
        request, stage, content = self.feature()
        archive = self.package(request, stage, content, self.root / "output")
        declaration = ReleaseDeclaration.parse(json.loads((archive.parent / "manifest.json").read_text())["deployment"])
        outer = sum(p.stat().st_size for p in stage.rglob("*") if p.is_file())
        with self.assertRaises(DeployError):
            self.verify(declaration, stage, artifacts.ExpansionBudget(max_bytes=outer, max_disk_bytes=1024**2))

    def test_verification_and_publication_share_one_expansion(self):
        request, stage, content = self.feature()
        archive = self.package(request, stage, content, self.root / "output")
        declaration = ReleaseDeclaration.parse(json.loads((archive.parent / "manifest.json").read_text())["deployment"])
        payload = declaration.artifacts[0]
        limit = sum(p.stat().st_size for p in stage.rglob("*") if p.is_file()) + sum(m.size for m in payload.members)
        budget = artifacts.ExpansionBudget(max_bytes=limit, max_disk_bytes=1024**2)
        prepared, = self.verify(declaration, stage, budget)
        host = Host(Paths(self.root / "host"))
        host.paths.opt("sample-test").mkdir(parents=True)
        before = asdict(budget)
        with patch.object(artifacts, "inspect_payload", side_effect=AssertionError("must not reextract")):
            tree = artifacts.publish(host, "sample-test", prepared, history=(), observed_service_abi="service-1")
        self.addCleanup(lambda: artifacts._remove_staging(tree))
        self.assertEqual(budget.bytes, limit)
        self.assertEqual(asdict(budget), before)

    def test_distinct_payloads_with_same_digest_share_aggregate_budget(self):
        request, stage, content = self.feature()
        request["topology"]["artifacts"]["second-extension"] = request["topology"]["artifacts"]["extension"]
        archive = self.package(request, stage, content, self.root / "output")
        declaration = ReleaseDeclaration.parse(json.loads((archive.parent / "manifest.json").read_text())["deployment"])
        outer = sum(p.stat().st_size for p in stage.rglob("*") if p.is_file())
        inner = sum(m.size for m in declaration.artifacts[0].members)
        budget = artifacts.ExpansionBudget(max_bytes=outer + inner, max_disk_bytes=1024**2)
        with self.assertRaises(DeployError) as error:
            self.verify(declaration, stage, budget)
        self.assertEqual(error.exception.code, "artifact_expansion")
        self.assertFalse((self.root / "prepared").exists())
        budget = artifacts.ExpansionBudget(max_bytes=outer + 2 * inner, max_disk_bytes=1024**2)
        prepared = self.verify(declaration, stage, budget)
        self.assertEqual(len(prepared), 2)
        self.assertEqual(budget.bytes, outer + 2 * inner)

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

    def carried(self, *, build=False):
        request, stage, content = self.feature()
        source = {"path": "deploy/artifacts/linux-amd64/extension.tar.gz"}
        if build:
            source["build"] = ["sh", "deploy/build-extension.sh"]
        request["topology"]["artifacts"]["extension"]["platforms"] = {
            "linux-amd64": {"source": {"release": source}}}
        work = self.root / "source"
        (work / "deploy/files").mkdir(parents=True)
        (work / "deploy/files/relay.json").write_bytes((stage / "deploy/files/relay.json").read_bytes())
        (work / "deploy/topology.json").write_text(json.dumps(request["topology"]))
        path = work / source["path"]
        path.parent.mkdir(parents=True)
        if not build:
            path.write_bytes(content)
        shutil.rmtree(stage / "deploy")
        return request, stage, content, work, path

    def compiler(self, args, **kwargs):
        self.assertEqual(args[0], "go")
        Path(args[args.index("-o") + 1]).write_bytes(elf())
        return subprocess.CompletedProcess(args, 0)

    def test_release_carried_bundle_uses_inventory_and_existing_publication(self):
        request, stage, content, work, path = self.carried()
        with patch.object(metadata.subprocess, "run", side_effect=self.compiler):
            request = metadata.prepare(request, work, stage)
        with patch.object(metadata, "collect_build_inputs", return_value={}), \
                patch.object(artifacts, "download", side_effect=AssertionError("must not download")):
            archive = metadata.package(request, stage, self.root / "output")
        manifest = json.loads((archive.parent / "manifest.json").read_text())
        deployment = ReleaseDeclaration.parse(manifest["deployment"])
        self.assertEqual(deployment.topology_sha256, parse_topology(request["topology"]).sha256)
        self.assertEqual(manifest["artifact_origins"], {"extension": []})
        payload, = deployment.artifacts
        self.assertEqual(payload.sha256, hashlib.sha256(content).hexdigest())
        self.assertFalse((stage / "deploy/artifacts/extension/release-input").exists())
        prepared, = self.verify(deployment, stage, artifacts.ExpansionBudget())
        host = Host(Paths(self.root / "host"))
        host.paths.opt("sample-test").mkdir(parents=True)
        with self.assertRaises(DeployError):
            artifacts.publish(host, "sample-test", prepared, history=(), observed_service_abi="wrong")
        tree = artifacts.publish(host, "sample-test", prepared, history=(), observed_service_abi="service-1")
        reference = artifacts.ArtifactReference(payload.artifact, prepared.artifact.version, payload.platform, payload.sha256)
        view = artifacts.bundle_export(reference, prepared.artifact, tree, "plugin", payload.members,
                                       observed_service_abi="service-1", uid=os.getuid())
        self.assertTrue(view.source.is_dir())
        with self.assertRaises(DeployError):
            artifacts.validate_history((artifacts.ArtifactReference(payload.artifact, prepared.artifact.version,
                                       payload.platform, "a" * 64),), (reference,))
        (stage / payload.path).write_bytes(content + b"tampered")
        with self.assertRaises(DeployError):
            self.verify(deployment, stage, artifacts.ExpansionBudget())

    def test_release_artifact_build_uses_selected_architecture(self):
        request, stage, content, work, path = self.carried(build=True)
        def build(args, cwd, environment):
            self.assertEqual(args, ("sh", "deploy/build-extension.sh"))
            self.assertEqual(cwd, work)
            self.assertEqual(environment["ARCHITECTURE"], "amd64")
            self.assertEqual(environment["GOARCH"], "amd64")
            path.write_bytes(content)
        with patch.object(metadata.subprocess, "run", side_effect=self.compiler), \
                patch.object(metadata, "build_artifact", side_effect=build):
            prepared = metadata.prepare(request, work, stage)
        self.assertEqual(prepared["release_artifacts"]["extension"]["sha256"], hashlib.sha256(content).hexdigest())

    def test_release_artifact_refuses_missing_linked_or_preexisting_build_output(self):
        request, stage, _, work, path = self.carried(build=True)
        for kind in ("missing", "symlink", "hardlink", "preexisting"):
            with self.subTest(kind=kind):
                shutil.rmtree(stage / "deploy", ignore_errors=True)
                path.unlink(missing_ok=True)
                target = work / "deploy/files/relay.json"
                def build(*args):
                    if kind == "symlink":
                        path.symlink_to(target)
                    elif kind == "hardlink":
                        os.link(target, path)
                if kind == "preexisting":
                    path.write_bytes(b"stale")
                with patch.object(metadata.subprocess, "run", side_effect=self.compiler), \
                        patch.object(metadata, "build_artifact", side_effect=build), self.assertRaises(DeployError):
                    metadata.prepare(request, work, stage)

    def test_release_source_is_explicit_and_unambiguous(self):
        good = {"source": {"release": {"path": "deploy/artifacts/linux-arm64/plugin.tar.gz"}}}
        self.assertEqual(ArtifactPlatform.parse(good).source.path, good["source"]["release"]["path"])
        bad = [
            {**good, "url": URL}, {**good, "sha256": "0" * 64},
            {"source": {"release": {"path": "../plugin.tar.gz"}}},
            {"source": {"release": {"path": "deploy/artifacts/plugin", "build": []}}},
            {"source": {"release": {"path": "deploy/artifacts/plugin", "build": "make"}}},
            {"source": {"release": {"path": "deploy/artifacts/plugin"}, "url": URL}},
        ]
        for value in bad:
            with self.subTest(value=value), self.assertRaises(DeployError):
                ArtifactPlatform.parse(value)

    def test_release_carried_inventory_digest_is_required(self):
        request, stage, content, work, path = self.carried()
        with patch.object(metadata.subprocess, "run", side_effect=self.compiler):
            prepared = metadata.prepare(request, work, stage)
        artifact = dict(parse_topology(request["topology"]).artifacts)["extension"]
        with self.assertRaises(DeployError):
            artifacts.inspect_payload(artifact, "linux-amd64", path, artifacts.ExpansionBudget())
        staged = stage / prepared["release_artifacts"]["extension"]["path"]
        staged.write_bytes(content + b"changed")
        with patch.object(metadata, "collect_build_inputs", return_value={}), self.assertRaises(DeployError):
            metadata.package(prepared, stage, self.root / "output")

    def test_release_carried_duplicate_sources_refuse(self):
        request, stage, _, work, _ = self.carried()
        request["topology"]["artifacts"]["second"] = request["topology"]["artifacts"]["extension"]
        (work / "deploy/topology.json").write_text(json.dumps(request["topology"]))
        with patch.object(metadata.subprocess, "run", side_effect=self.compiler), self.assertRaises(DeployError):
            metadata.prepare(request, work, stage)

    def test_release_carried_file_keeps_architecture_validation(self):
        request, stage, _, work, path = self.carried()
        request["topology"]["artifacts"]["extension"] = {
            "version": "1", "format": "file", "executables": {"tool": "tool"},
            "platforms": request["topology"]["artifacts"]["extension"]["platforms"]}
        role = request["topology"]["roles"][1]
        role.pop("artifact_mounts")
        role.pop("bindings")
        path.write_bytes(elf("linux-arm64"))
        (work / "deploy/topology.json").write_text(json.dumps(request["topology"]))
        with patch.object(metadata.subprocess, "run", side_effect=self.compiler):
            prepared = metadata.prepare(request, work, stage)
        with self.assertRaises(DeployError) as error:
            metadata.package(prepared, stage, self.root / "output")
        self.assertEqual(error.exception.code, "artifact_architecture")

    def test_artifact_build_failure_and_timeout_refuse(self):
        process = Mock(pid=1234)
        with patch.object(metadata.subprocess, "Popen", return_value=process) as popen:
            process.wait.return_value = 1
            with self.assertRaises(DeployError):
                metadata.build_artifact(("sh", "build.sh"), self.root, {})
            self.assertTrue(popen.call_args.kwargs["start_new_session"])
            process.wait.side_effect = [subprocess.TimeoutExpired("build", 1200), -9]
            with patch.object(metadata.os, "killpg") as kill, self.assertRaises(subprocess.TimeoutExpired):
                metadata.build_artifact(("sh", "build.sh"), self.root, {})
            kill.assert_called_once_with(1234, metadata.signal.SIGKILL)
            self.assertEqual(process.wait.call_args.args, ())
