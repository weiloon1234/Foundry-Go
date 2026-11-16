"""Pinned builder inputs and deterministic packaging without Docker or compilers."""
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

from test_local import DEPLOY, cli, fixture, metadata, release
from host import Host, Paths


class BuildInputTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name).resolve()
        (self.root / "web").mkdir()
        (self.root / "go.mod").write_text("module example.com/app\ngo 1.27.3\n")
        (self.root / "web" / "package.json").write_text('{"engines":{"node":">=24"}}')
        self.pins = json.loads((DEPLOY / "docker" / "build-inputs.json").read_text())

    def test_all_pins_are_sha256_digests(self):
        self.assertEqual(self.pins["schema"], 1)
        for family in ("ubuntu", "go", "node"):
            for version, checksum in self.pins[family].items():
                with self.subTest(family=family, version=version):
                    self.assertRegex(checksum, r"^sha256:[0-9a-f]{64}$")

    def test_unknown_go_version_refuses_without_floating_fallback(self):
        (self.root / "go.mod").write_text("module example.com/app\ngo 1.28.0\n")
        with self.assertRaisesRegex(cli.LocalError, r"go 1\.28\.0 needs a reviewed pin in Foundry-Go: tools/deploy/docker/build-inputs\.json"):
            cli.builder_inputs(self.root, "arm64")

    def test_unknown_node_version_refuses_without_floating_fallback(self):
        (self.root / "web" / "package.json").write_text('{"engines":{"node":">=26"}}')
        with self.assertRaisesRegex(cli.LocalError, r"node 26 needs a reviewed pin in Foundry-Go: tools/deploy/docker/build-inputs\.json"):
            cli.builder_inputs(self.root, "arm64")

    def test_each_digest_changes_builder_fingerprint(self):
        versions, images, fingerprint = cli.builder_inputs(self.root, "arm64")
        self.assertEqual(images, {name: self.pins[name][version] for name, version in versions.items()})
        for name, version in versions.items():
            changed = json.loads(json.dumps(self.pins))
            changed[name][version] = "sha256:" + "0" * 64
            with self.subTest(name=name), patch.object(cli, "read_json", side_effect=lambda path:
                    changed if path.name == "build-inputs.json" else {"engines": {"node": ">=24"}}):
                self.assertNotEqual(cli.builder_inputs(self.root, "arm64")[2], fingerprint)

    def test_builder_architecture_resolution_ignores_docker_default_platform(self):
        _, profile = fixture()
        for server, native in (("x86_64", "amd64"), ("aarch64", "arm64"), ("amd64", "amd64"), ("arm64", "arm64")):
            for selection, expected in ((None, native), ("native", native), ("target", "amd64"),
                                        ("amd64", "amd64"), ("arm64", "arm64")):
                for default in ("linux/amd64", "linux/arm64"):
                    environment = {"DOCKER_DEFAULT_PLATFORM": default}
                    if selection is not None:
                        environment["DEPLOY_BUILDER_ARCHITECTURE"] = selection
                    with self.subTest(server=server, selection=selection, default=default), \
                            patch.dict(os.environ, environment, clear=True), \
                            patch.object(cli, "execute", return_value=server + "\n") as execute:
                        self.assertEqual(cli.builder_architecture(profile), (expected, native))
                        execute.assert_called_once_with(["docker", "version", "--format", "{{.Server.Arch}}"], timeout=30)
        profile["architecture"] = "arm64"
        with patch.dict(os.environ, {"DEPLOY_BUILDER_ARCHITECTURE": "target"}), \
                patch.object(cli, "execute", return_value="amd64"):
            self.assertEqual(cli.builder_architecture(profile), ("arm64", "amd64"))

    def test_invalid_builder_architecture_refuses(self):
        _, profile = fixture()
        for selection in ("", "x86_64", "aarch64", "linux/amd64", "AMD64", "other"):
            with self.subTest(selection=selection), patch.dict(os.environ, {"DEPLOY_BUILDER_ARCHITECTURE": selection}), \
                    patch.object(cli, "execute") as execute, self.assertRaisesRegex(cli.LocalError, "DEPLOY_BUILDER_ARCHITECTURE"):
                cli.builder_architecture(profile)
            execute.assert_not_called()
        with patch.dict(os.environ, {}, clear=True), patch.object(cli, "execute", return_value="riscv64"), \
                self.assertRaisesRegex(cli.LocalError, "Docker server architecture"):
            cli.builder_architecture(profile)

    def test_emulation_preflight_requires_requested_userland(self):
        inputs = {"ubuntu": self.pins["ubuntu"]["24.04"]}
        with patch.object(cli, "execute") as execute:
            cli.preflight_builder("arm64", "arm64", inputs)
            execute.assert_not_called()
        for result in ("amd64\n", "arm64\n", "", cli.LocalError("exec format error")):
            with self.subTest(result=result), patch.object(cli, "execute") as execute:
                if isinstance(result, Exception):
                    execute.side_effect = result
                else:
                    execute.return_value = result
                if result == "amd64\n":
                    cli.preflight_builder("amd64", "arm64", inputs)
                else:
                    with self.assertRaisesRegex(cli.LocalError, "enable Rosetta/binfmt emulation or use a native builder"):
                        cli.preflight_builder("amd64", "arm64", inputs)
                execute.assert_called_once_with(["docker", "run", "--rm", "--platform", "linux/amd64",
                                                "ubuntu:24.04@" + inputs["ubuntu"], "dpkg", "--print-architecture"], timeout=120)

    def mocked_build(self, architecture, *, cached=False, preflight_failure=False):
        project, profile = fixture()
        request, _, _ = self.request_and_stage()
        request["manifest"]["version"] = "20261116T000000Z-aaaaaaaaaaaa"
        calls = []
        image_name = "foundry-builder-" + project["id"]
        image = image_name + ":" + architecture + "-" + cli.builder_inputs(self.root, architecture)[2]
        other = image_name + ":" + ("arm64" if architecture == "amd64" else "amd64") + "-other"
        old = image_name + ":" + architecture + "-old"
        legacy = image_name + ":legacy"
        self.superseded = image_name + ":" + "c" * 12

        def execute(args, **kwargs):
            calls.append(args)
            if args[1] == "version":
                return "arm64\n"
            if args[1:3] == ["image", "ls"]:
                return "\n".join([other, old, legacy, self.superseded] + ([image] if cached else []))
            if args[1:3] == ["image", "inspect"]:
                return "sha256:" + "b" * 64
            if args[1] == "run":
                if args[-2:] == ["dpkg", "--print-architecture"]:
                    return "arm64" if preflight_failure else architecture
                if args[-1] == "/foundry-deploy/local/build.sh":
                    output = self.root / "dist/releases" / project["id"] / profile["instance_id"] / request["manifest"]["version"]
                    (output / "app.tar.gz").write_bytes(b"fixture archive")
                    (output / "manifest.json").write_text("{}")
            return ""

        with patch.object(cli, "ROOT", self.root), patch.dict(os.environ, {
                "DEPLOY_BUILDER_ARCHITECTURE": architecture,
                "DOCKER_DEFAULT_PLATFORM": "linux/" + ("arm64" if architecture == "amd64" else "amd64")}), \
                patch.object(cli, "execute", side_effect=execute), \
                patch.object(cli, "build_request", return_value=request), \
                patch.object(cli, "source_snapshot", return_value=self.root), \
                patch.object(cli, "build_source_epoch", return_value=0), \
                patch.object(cli, "validate_manifest"), patch.object(cli, "prune_releases"), \
                patch.object(cli.tempfile, "gettempdir", return_value=str(self.root)):
            if preflight_failure:
                with self.assertRaisesRegex(cli.LocalError, "Rosetta/binfmt"):
                    cli.build(project, profile)
            else:
                cli.build(project, profile)
        records = list((self.root / ".deploy/builds").glob("*.json"))
        record, = [value for path in records if (value := json.loads(path.read_text()))["project_id"] == project["id"]]
        self.assertNotIn("builder_architecture", request["manifest"])
        return calls, record, image, other, old, legacy

    def test_build_isolates_images_caches_and_all_platform_flags(self):
        fingerprints = {arch: cli.builder_inputs(self.root, arch)[2] for arch in ("amd64", "arm64")}
        self.assertNotEqual(fingerprints["amd64"], fingerprints["arm64"])
        for arch in ("amd64", "arm64"):
            for cached in (False, True):
                with self.subTest(architecture=arch, cached=cached):
                    calls, record, image, other, old, legacy = self.mocked_build(arch, cached=cached)
                    self.assertEqual(record["builder_architecture"], arch)
                    self.assertEqual(record["image"], image)
                    self.assertEqual(record["phase"], "completed")
                    self.assertTrue(record["builder"].endswith("-" + arch))
                    self.assertTrue(all(name.endswith("-" + arch) for name in record["containers"]))
                    builds = [args for args in calls if args[1:3] == ["buildx", "build"]]
                    self.assertEqual(len(builds), 0 if cached else 1)
                    runs = [args for args in calls if args[1] == "run"]
                    self.assertEqual(len(runs), 3 if arch == "amd64" else 2)
                    for args in builds + runs:
                        self.assertEqual(args[args.index("--platform") + 1], "linux/" + arch)
                    for args in runs:
                        if image in args:
                            self.assertIn("type=volume,src=foundry-cache-" + record["project_id"] + "-" + arch + ",dst=/cache", args)
                    self.assertIn(["docker", "image", "rm", old], calls)
                    self.assertNotIn(["docker", "image", "rm", other], calls)
                    self.assertNotIn(["docker", "image", "rm", legacy], calls)
                    self.assertIn(["docker", "image", "rm", self.superseded], calls)
                    self.assertFalse(any(args[1] == "volume" for args in calls))

    def test_build_preflight_refuses_before_image_build_or_cache_mount(self):
        calls, record, *_ = self.mocked_build("amd64", preflight_failure=True)
        self.assertEqual(record["builder_architecture"], "amd64")
        self.assertFalse(any(args[1:3] == ["buildx", "create"] or "--mount" in args for args in calls))

    def test_orphan_checks_cover_legacy_and_both_architectures(self):
        project, _ = fixture()
        for suffix in ("", "-arm64", "-amd64"):
            with self.subTest(suffix=suffix), patch.object(cli, "execute", return_value="foundry-" + project["id"] + suffix + " *\n"), \
                    self.assertRaisesRegex(cli.LocalError, "survives a previous client"):
                cli.refuse_orphan_builder(project)
        with patch.object(cli, "execute", return_value="") as execute:
            cli.recover_build_containers(project)
        names = {call.args[0][call.args[0].index("--filter") + 1] for call in execute.call_args_list}
        self.assertEqual(names, {"name=^/foundry-" + kind + "-" + project["id"] + suffix + "$"
                                for kind in ("build", "cache-clean") for suffix in ("", "-amd64", "-arm64")})

    def test_epoch_uses_snapshot_commit_for_clean_and_dirty_builds(self):
        _, profile = fixture()
        for state in ("clean", "dirty"):
            with self.subTest(state=state), patch.object(cli, "ROOT", self.root), \
                    patch.object(cli, "execute", return_value="1790000000\n") as execute:
                self.assertEqual(cli.build_source_epoch({"commit": "a" * 40, "tree_state": state}, profile), 1790000000)
                execute.assert_called_once_with(["git", "-C", str(self.root), "show", "-s", "--format=%ct", "a" * 40], timeout=30)
        profile["runtime_environment"] = "staging"
        with patch.object(cli, "execute") as execute:
            self.assertEqual(cli.build_source_epoch({"commit": None}, profile), 0)
            execute.assert_not_called()
        profile["runtime_environment"] = "production"
        with self.assertRaises(cli.LocalError):
            cli.build_source_epoch({"commit": None}, profile)

    def request_and_stage(self):
        project, profile = fixture()
        entry = {**cli.identity(project, profile), "profile": profile}
        version = "20261013T120000Z-aaaaaaaaaaaa"
        stage = self.root / "stage"
        for name, content in {"artisan": b"binary", "config/deploy.toml": b"[app]\n",
                              "public/" + "a" * 110 + ".js": b"asset",
                              f"private-source-maps/{version}/app.js.map": b"map"}.items():
            path = stage / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(content)
        manifest = {"schema": 1, "layout_version": 2, "project_id": project["id"],
                    "instance_id": profile["instance_id"], "instance": "abc-prod", "environment": "prod",
                    "architecture": "amd64", "version": version, "commit": "a" * 40, "tree_state": "clean",
                    "built_at": "2026-10-13T12:00:00Z", "native_imaging": False,
                    "source_kind": "git_archive", "source_sha256": "e" * 64}
        return {"manifest": manifest, "source_date_epoch": 1790000000,
                "images": cli.builder_inputs(self.root, "arm64")[1]}, stage, entry

    def command(self, args, **kwargs):
        self.assertEqual(kwargs["timeout"], 30)
        self.assertTrue(kwargs["check"])
        outputs = {"dpkg-query": b"zlib 1.2 arm64\napt 2.0 arm64\n", "go": b"go version go1.27.3 linux/arm64\n",
                   "node": b"v24.0.0\n", "npm": b"11.0.0\n", "aarch64-linux-gnu-gcc": b"gcc 14.0\nCopyright\n"}
        kwargs["stdout"].write(outputs[args[0]])
        return subprocess.CompletedProcess(args, 0)

    def test_same_stage_repackages_identically_across_mtimes_and_clock_change(self):
        request, stage, entry = self.request_and_stage()
        with patch.dict(os.environ, {"CC": "aarch64-linux-gnu-gcc"}), \
                patch.object(metadata.subprocess, "run", side_effect=self.command) as commands:
            with patch("time.time", return_value=1790000100):
                first = metadata.package(request, stage, self.root / "first")
            for path in stage.rglob("*"):
                os.utime(path, (1800000000.125, 1800000000.125))
            with patch("time.time", return_value=1800000000):
                # A stage holds no manifest until packaging writes it.
                (stage / "manifest.json").unlink()
                second = metadata.package(request, stage, self.root / "second")
        self.assertEqual(commands.call_count, 10)
        self.assertEqual(hashlib.sha256(first.read_bytes()).hexdigest(), hashlib.sha256(second.read_bytes()).hexdigest())
        self.assertEqual(first.read_bytes()[4:8], b"\0\0\0\0")
        self.assertEqual(first.read_bytes()[3] & 8, 0)  # No gzip filename.
        with metadata.tarfile.open(first) as archive:
            for member in archive:
                self.assertEqual(member.mtime, request["source_date_epoch"])
                self.assertLessEqual(set(member.pax_headers), {"path"})
                self.assertEqual((member.uid, member.gid, member.uname, member.gname), (0, 0, "root", "root"))
            manifest = json.load(archive.extractfile("manifest.json"))
        expected = {"images": request["images"], "go": "go version go1.27.3 linux/arm64", "node": "v24.0.0",
                    "npm": "11.0.0", "cc": "gcc 14.0",
                    "packages_sha256": hashlib.sha256(b"apt 2.0 arm64\nzlib 1.2 arm64\n").hexdigest()}
        self.assertEqual(manifest["build_inputs"], expected)
        self.assertEqual(json.loads((second.parent / "manifest.json").read_text()), manifest)
        validated = release.unpack_archive(Host(Paths(self.root)), first, self.root / "extracted", entry, cli.digest(first))
        self.assertEqual(validated["build_inputs"], expected)

    def test_each_provenance_command_failure_aborts_packaging(self):
        request, stage, _ = self.request_and_stage()
        for failed in ("dpkg-query", "go", "node", "npm", "aarch64-linux-gnu-gcc"):
            def command(args, **kwargs):
                if args[0] == failed:
                    raise subprocess.CalledProcessError(1, args)
                return self.command(args, **kwargs)
            output = self.root / failed
            with self.subTest(command=failed), patch.dict(os.environ, {"CC": "aarch64-linux-gnu-gcc"}), \
                    patch.object(metadata.subprocess, "run", side_effect=command):
                with self.assertRaises(subprocess.CalledProcessError):
                    metadata.package(request, stage, output)
                self.assertFalse((output / "app.tar.gz").exists())
                self.assertFalse((output / "app.tar.gz.partial").exists())

    def test_provenance_commands_reject_empty_and_oversized_output_and_timeout(self):
        for content in (b"", b"x" * (4 * 1024 * 1024 + 1)):
            def command(args, **kwargs):
                kwargs["stdout"].write(content)
            with self.subTest(size=len(content)), patch.object(metadata.subprocess, "run", side_effect=command):
                with self.assertRaises(ValueError):
                    metadata.public_command(["go", "version"])
        with patch.object(metadata.subprocess, "run", side_effect=subprocess.TimeoutExpired("go", 30)):
            with self.assertRaises(subprocess.TimeoutExpired):
                metadata.public_command(["go", "version"])
