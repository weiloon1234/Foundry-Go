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
            cli.builder_inputs(self.root)

    def test_unknown_node_version_refuses_without_floating_fallback(self):
        (self.root / "web" / "package.json").write_text('{"engines":{"node":">=26"}}')
        with self.assertRaisesRegex(cli.LocalError, r"node 26 needs a reviewed pin in Foundry-Go: tools/deploy/docker/build-inputs\.json"):
            cli.builder_inputs(self.root)

    def test_each_digest_changes_builder_fingerprint(self):
        versions, images, fingerprint = cli.builder_inputs(self.root)
        self.assertEqual(images, {name: self.pins[name][version] for name, version in versions.items()})
        for name, version in versions.items():
            changed = json.loads(json.dumps(self.pins))
            changed[name][version] = "sha256:" + "0" * 64
            with self.subTest(name=name), patch.object(cli, "read_json", side_effect=lambda path:
                    changed if path.name == "build-inputs.json" else {"engines": {"node": ">=24"}}):
                self.assertNotEqual(cli.builder_inputs(self.root)[2], fingerprint)

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
                "images": cli.builder_inputs(self.root)[1]}, stage, entry

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
