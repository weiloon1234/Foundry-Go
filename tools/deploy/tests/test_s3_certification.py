"""Capability mapping and certification evidence without Docker or provider I/O."""
import copy
import io
import json
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

import certify_s3
import containers
import rehearsal
from contracts import DeployError


class S3CertificationTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name).resolve()

    def bindings(self, disks, capped=None):
        path = self.root / "bindings.json"
        value = {"environment": "acceptance-test", "configuration": {}, "storage_disks": list(disks)}
        if capped is not None:
            value["allow_single_request_conditions"] = capped
        path.write_text(json.dumps(value))
        path.chmod(0o600)
        return path, {"app": {"services": {"storage": {"disks": disks}}}}

    def bind(self, disks, capped=None, status="passed"):
        path, config = self.bindings(disks, capped)
        original = copy.deepcopy(config)
        with patch.object(containers, "S3_CONDITIONAL_MULTIPART",
                          {"image": containers.S3_IMAGE, "status": status, "evidence": "test"}):
            result = rehearsal.test_bindings(path, "acceptance-test", config, "test")
        self.assertEqual(config, original)
        return result

    def test_r2_preserves_large_object_policy_and_uncapped_conditions(self):
        disk = {"driver": "r2", "cloud": {"part_bytes": 8388608, "max_object_bytes": 1073741824},
                "prefix": "attachments/"}
        settings, _, fidelity = self.bind({"uploads": disk})
        mapped = settings["app_storage"]["disks"]["uploads"]
        self.assertEqual(mapped["prefix"], disk["prefix"])
        self.assertEqual(mapped["cloud"]["max_object_bytes"], 1073741824)
        self.assertEqual(mapped["cloud"]["part_bytes"], 8388608)
        self.assertEqual(mapped["cloud"]["compatible"], {"conditional_create": True, "conditional_replace": True,
                                                       "requires_nfc_keys": True, "single_request_conditions": False})
        self.assertEqual(fidelity["status"], "preserved")

    def test_compatible_declarations_are_not_widened(self):
        for declared in ({}, {"conditional_create": False}, {"conditional_create": True},
                         {"conditional_replace": True, "single_request_conditions": True, "server_copy": False},
                         {"versions": True, "conditional_delete": True, "listing_path_encoding": True}):
            with self.subTest(declared=declared):
                settings, _, _ = self.bind({"uploads": {"driver": "s3_compatible", "cloud": {"compatible": declared}}})
                self.assertEqual(settings["app_storage"]["disks"]["uploads"]["cloud"]["compatible"], declared)

    def test_uncertified_multipart_refuses_or_caps_only_named_disks(self):
        for status in ("pending", "failed"):
            for driver in ("r2", "s3_compatible"):
                disks = {"uploads": {"driver": driver, "cloud": {"compatible": {"conditional_replace": True}}},
                         "small": {"driver": "s3_compatible", "cloud": {"compatible": {
                             "conditional_create": True, "single_request_conditions": True}}}}
                with self.subTest(status=status, driver=driver):
                    with self.assertRaisesRegex(DeployError, "uploads.*CompleteMultipartUpload"):
                        self.bind(disks, status=status)
                    settings, _, fidelity = self.bind(disks, ["uploads"], status)
                    mapped = settings["app_storage"]["disks"]
                    self.assertTrue(mapped["uploads"]["cloud"]["compatible"]["single_request_conditions"])
                    self.assertEqual(mapped["small"]["cloud"]["compatible"], disks["small"]["cloud"]["compatible"])
                    self.assertEqual(fidelity["status"], "reduced")
                    self.assertEqual(fidelity["conditional_writes_capped_at_part_bytes"], ["uploads"])
                    if driver == "s3_compatible":
                        self.assertNotIn("conditional_create", mapped["uploads"]["cloud"]["compatible"])

    def test_single_request_and_unconditional_disks_need_no_multipart_opt_in(self):
        for declared in ({}, {"conditional_create": True, "single_request_conditions": True}):
            _, _, fidelity = self.bind({"uploads": {"driver": "s3_compatible", "cloud": {"compatible": declared}}}, status="pending")
            self.assertEqual(fidelity["status"], "preserved")

    def test_opt_in_is_bounded_to_unique_bound_names_and_never_forces_a_cap(self):
        disks = {"uploads": {"driver": "r2"}}
        for invalid in (True, "uploads", ["missing"], ["uploads", "uploads"], [False], [{}]):
            with self.subTest(invalid=invalid), self.assertRaises(DeployError):
                self.bind(disks, invalid)
        settings, _, fidelity = self.bind(disks, ["uploads"])
        self.assertFalse(settings["app_storage"]["disks"]["uploads"]["cloud"]["compatible"]["single_request_conditions"])
        self.assertEqual(fidelity["status"], "preserved")

    def test_pin_change_requires_new_record_and_recorded_result(self):
        record = containers.s3_certification()
        self.assertEqual(record["image"], containers.S3_IMAGE)
        if record["status"] == "pending":
            self.assertIsNone(record["evidence"])
        else:
            evidence = json.loads((Path(__file__).resolve().parents[3] / record["evidence"]).read_text())
            self.assertEqual(evidence["image"], record["image"])
            self.assertEqual(evidence["status"], record["status"])
            self.assertEqual(evidence["matrix"], containers.S3_CONDITIONAL_TEST)
            if record["status"] == "passed":
                self.assertTrue(certify_s3.matrix_passed(evidence["exit_code"], evidence["cases"]))
        with patch.object(containers, "S3_IMAGE", "changed@sha256:" + "0" * 64):
            with self.assertRaisesRegex(containers.ContainerError, "re-certify"):
                containers.s3_certification()
            path, config = self.bindings({"uploads": {"driver": "r2"}})
            with self.assertRaisesRegex(DeployError, "re-certify"):
                rehearsal.test_bindings(path, "acceptance-test", config, "test")
        with patch.dict(containers.S3_CONDITIONAL_MULTIPART, status="passed", evidence=None):
            with self.assertRaises(containers.ContainerError):
                containers.s3_certification()

    def test_matrix_result_requires_both_sizes_no_skips_and_success(self):
        name = containers.S3_CONDITIONAL_TEST
        good = "\n".join("--- PASS: " + case + " (0.1s)" for case in (name, name + "/5242880", name + "/5242881"))
        for output, code, status in ((good, 0, "passed"), (good, 1, "failed"), ("", 0, "failed"),
                                     (good.replace("PASS", "SKIP", 1), 0, "failed"),
                                     (good.replace("5242881", "other"), 0, "failed")):
            with self.subTest(output=output, code=code):
                result = certify_s3.matrix_result(subprocess.CompletedProcess([], code, output + "\nSECRET diagnostic"))
                self.assertEqual(result["status"], status)
                self.assertNotIn("SECRET", json.dumps(result))

    def test_acceptance_runs_certification_before_application_phases(self):
        source = Path(__file__).with_name("run_acceptance.sh").read_text()
        self.assertLess(source.index('"$tests/certify_s3.py"'), source.index('phase foreign-baseline '))
        self.assertIn('--s3-certification-only)', source)
        self.assertIn('--output "$logs/s3-certification.json"', source)

    def test_certification_records_failure_and_refuses_a_different_image_before_build(self):
        output = self.root / "evidence.json"
        with patch.object(certify_s3, "run", return_value=subprocess.CompletedProcess([], 0, "other-image")) as run:
            with self.assertRaisesRegex(containers.ContainerError, "pinned"):
                certify_s3.certify("host", "store", "linux/arm64", "private", output)
        run.assert_called_once()
        self.assertEqual(json.loads(output.read_text())["status"], "failed")
        with self.assertRaises(FileExistsError):
            certify_s3.certify("host", "store", "linux/arm64", "private", output)

    def test_certification_builds_native_binary_and_records_observed_result(self):
        for status, code in (("passed", 0), ("failed", 1)):
            calls = []
            def command(args, **kwargs):
                calls.append((args, kwargs))
                output = ""
                if args[:2] == ["docker", "inspect"]:
                    output = containers.S3_IMAGE if args[3] == "{{.Config.Image}}" else "sha256:resolved"
                elif "-c" in args:
                    Path(args[args.index("-o") + 1]).write_bytes(b"test binary")
                elif args[:2] == ["docker", "exec"]:
                    return subprocess.CompletedProcess(args, code, json.dumps({"status": status, "cases": [], "exit_code": code}))
                return subprocess.CompletedProcess(args, 0, output)
            destination = self.root / (status + ".json")
            with patch.object(certify_s3, "run", side_effect=command):
                if code:
                    with self.assertRaises(containers.ContainerError):
                        certify_s3.certify("host", "store", "linux/arm64", "private", destination)
                else:
                    certify_s3.certify("host", "store", "linux/arm64", "private", destination)
            evidence = json.loads(destination.read_text())
            self.assertEqual(evidence["status"], status)
            self.assertEqual(evidence["image"], containers.S3_IMAGE)
            self.assertEqual(evidence["image_id"], "sha256:resolved")
            build, = [kwargs for args, kwargs in calls if "-c" in args]
            self.assertEqual(build["env"]["GOOS"], "linux")
            self.assertEqual(build["env"]["GOARCH"], "arm64")
            self.assertEqual(build["env"]["CGO_ENABLED"], "0")

    def test_host_requires_matrix_configuration_and_cleans_only_successful_bucket(self):
        name = containers.S3_CONDITIONAL_TEST
        output = "\n".join("--- PASS: " + case + " (0.1s)" for case in (name, name + "/5242880", name + "/5242881"))
        settings = {"endpoint": "https://s3:9000", "access_key": "private-access", "secret_key": "private-secret"}
        for code in (0, 1):
            with patch("acceptance_support.guard"), patch("acceptance_backup.s3_settings", return_value=settings), \
                    patch("acceptance_backup.rclone") as rclone, patch.object(certify_s3, "run",
                        return_value=subprocess.CompletedProcess([], code, output)) as run, \
                    patch("sys.stdout", new_callable=io.StringIO) as stdout:
                self.assertEqual(certify_s3.in_host("/test-binary"), code)
            self.assertEqual(rclone.call_count, 1 if code else 2)
            bucket = rclone.call_args_list[0].args[2]
            self.assertTrue(bucket.startswith("acceptance:certification-"))
            if not code:
                self.assertEqual(rclone.call_args_list[1].args, (settings, "rmdir", bucket))
            self.assertEqual(run.call_args.kwargs["env"]["FOUNDRY_TEST_COMPATIBLE_REQUIRED"], "1")
            self.assertIn("-test.run=^" + name + "$", run.call_args.args[0])
            self.assertNotIn("private-secret", stdout.getvalue())


if __name__ == "__main__":
    unittest.main()
