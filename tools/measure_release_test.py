import importlib.util
import json
import os
from pathlib import Path
import platform
import sys
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("measure_release", Path(__file__).with_name("measure-release.py"))
measure = importlib.util.module_from_spec(spec)
spec.loader.exec_module(measure)


class MeasurementTests(unittest.TestCase):
    def test_manifest_rejects_changed_metadata_and_extra_consumer_files(self):
        with tempfile.TemporaryDirectory() as name:
            root = Path(name)
            proxy = root / "proxy"
            proxy.mkdir()
            artifacts = []
            for number in range(3):
                archive = proxy / f"{number}.zip"
                archive.write_bytes(b"archive fixture")
                metadata = proxy / f"{number}.mod"
                metadata.write_text("module fixture\n")
                artifacts.append({"zip": str(archive.relative_to(root)), "sha256": measure.digest(archive),
                                  "proxy_files": [{"path": str(metadata.relative_to(root)), "sha256": measure.digest(metadata)}]})
            consumers = {}
            for label in ("full", "ordinary"):
                directory = root / "consumers" / label
                directory.mkdir(parents=True)
                source = directory / "source.go"
                source.write_text("package fixture\n")
                consumers[label] = [{"path": "source.go", "sha256": measure.digest(source)}]
            manifest = {"format": 1, "artifacts": artifacts, "consumers": consumers}
            measure.verify_inputs(root, manifest)
            metadata.write_text("changed module\n")
            with self.assertRaises(ValueError):
                measure.verify_inputs(root, manifest)
            metadata.write_text("module fixture\n")
            configured = root / "consumers/configured"
            configured.mkdir()
            source = configured / "source.go"
            source.write_text("package configured\n")
            manifest["format"] = 2
            manifest["consumers"]["configured"] = [{"path": "source.go", "sha256": measure.digest(source)}]
            measure.verify_inputs(root, manifest)
            manifest["format"] = 1
            with self.assertRaises(ValueError):
                measure.verify_inputs(root, manifest)
            manifest["format"] = 2
            (root / "consumers/full/extra.go").write_text("package unexpected\n")
            with self.assertRaises(ValueError):
                measure.verify_inputs(root, manifest)

    def test_json_stream_and_editor_byte_position(self):
        self.assertEqual(measure.decode_stream(' {"a": 1}\n{\n"b":2}\n'), [{"a": 1}, {"b": 2}])
        with self.assertRaises(json.JSONDecodeError):
            measure.decode_stream('{} trailing failure')
        source = 'package fixture\n// é\nfunc run() { "é"; Draft{}.SetName("value") }'
        line, column = measure.position(source, "Draft{}.", "SetName", "complete")
        self.assertEqual(line, 3)
        self.assertEqual(column, len('func run() { "é"; Draft{}.'.encode()) + 1)
        self.assertEqual(measure.position(source, "Draft{}.", "SetName", "hover"), (line, column + 1))
        with self.assertRaises(ValueError):
            measure.position(source + source, "Draft{}.", "SetName", "complete")

    def test_consumer_defaults_exclude_private_and_constrained_settings(self):
        with patch.dict(os.environ, {"GOFLAGS": "-gcflags=all=-N", "GOGC": "10", "GOMEMLIMIT": "192MiB", "GOMAXPROCS": "1", "CGO_ENABLED": "0", "FOUNDRY_TEST_POSTGRES_URL": "private", "AWS_SECRET_ACCESS_KEY": "private"}):
            env = measure.clean_environment(Path("/cache"), Path("/modules"), "file:///proxy", ["example.test/Framework"])
        self.assertEqual(env["GOFLAGS"], "-mod=readonly")
        self.assertEqual(env["GOENV"], "off")
        self.assertEqual(env["GONOSUMDB"], "example.test/Framework")
        for key in ("GOGC", "GOMEMLIMIT", "GOMAXPROCS", "CGO_ENABLED", "FOUNDRY_TEST_POSTGRES_URL", "AWS_SECRET_ACCESS_KEY"):
            self.assertNotIn(key, env)

    def test_editor_success_requires_semantic_result_and_timings(self):
        payload = {"result": {"items": [{"label": "SetName(string)"}]}, "timing": {"request_ns": 1, "initialize_ns": 1}}
        measure.validate_editor(payload, "complete", "SetName")
        with self.assertRaises(ValueError):
            measure.validate_editor(payload, "complete", "Missing")
        payload["timing"]["request_ns"] = 0
        with self.assertRaises(ValueError):
            measure.validate_editor(payload, "complete", "SetName")

    def test_module_review_rejects_replacements_and_external_sources(self):
        with tempfile.TemporaryDirectory() as name:
            root = Path(name)
            for module in ({"Path": "example.test/x", "Replace": {"Dir": "/source"}}, {"Path": "example.test/x", "Dir": str(root.parent)}):
                with self.assertRaises(ValueError):
                    measure.module_review([module], root, root)

    @unittest.skipUnless(platform.system() == "Darwin", "native macOS resource runner")
    def test_failed_command_retains_actual_result_without_claiming_success(self):
        with tempfile.TemporaryDirectory() as name:
            root = Path(name)
            runner = measure.Runner(root, 5)
            with self.assertRaises(RuntimeError):
                runner.run("failure", [sys.executable, "-c", "raise SystemExit(7)"], root, os.environ.copy())
            self.assertEqual(json.loads((root / "failure/result.json").read_text())["exit"], 7)
            self.assertEqual(len(json.loads((root / "commands.json").read_text())), 1)


if __name__ == "__main__":
    unittest.main()
