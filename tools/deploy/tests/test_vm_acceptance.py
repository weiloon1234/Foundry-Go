"""Local VM harness contracts; these tests never boot or provision a guest."""
import io
import json
import os
from pathlib import Path
import shutil
import sys
import tempfile
import unittest
from unittest.mock import patch

import vm_acceptance as vm
from contracts import validate_profile
from topology import instance_units


class InteractiveTests(unittest.TestCase):
    def test_review_and_prompt_are_both_required(self):
        feature = json.dumps({"ok": True, "plan_token": "reviewed"})
        standard = json.dumps({key: "fixture" for key in ("version", "commit", "tree_state", "architecture", "instance")})
        for review, mode in ((feature, True), (standard, False)):
            with self.subTest(feature=mode):
                self.assertFalse(vm.reviewed_prompt(review, mode))
                self.assertFalse(vm.reviewed_prompt("Activate (type yes): ", mode))
                self.assertFalse(vm.reviewed_prompt(review + "\nActivate (type ye", mode))
                self.assertTrue(vm.reviewed_prompt("diagnostic\r\n" + review + "\r\nActivate (type yes): ", mode))
        self.assertFalse(vm.reviewed_prompt('{"ok": false, "plan_token": "no"}\nActivate (type yes): ', True))
        self.assertFalse(vm.reviewed_prompt(standard + "\nActivate (type yes): ", True))

    def test_fake_child_receives_yes_only_after_reviewed_prompt(self):
        child = '''import json, os, sys, time
assert sys.stdin.isatty() and sys.stdout.isatty() and sys.stderr.isatty()
assert "CONFIRM" not in os.environ
print(json.dumps({"ok": True, "plan_token": "reviewed"}), flush=True)
sys.stderr.write("Activate (type ye")
sys.stderr.flush()
time.sleep(0.03)
sys.stderr.write("s): ")
sys.stderr.flush()
assert input() == "yes"
print("activated", flush=True)
'''
        output, calls = io.StringIO(), []
        def confirm(alive):
            self.assertTrue(alive())
            self.assertTrue(vm.reviewed_prompt(output.getvalue(), True))
            calls.append(True)
        result = vm.interactive_command([sys.executable, "-c", child],
            env={k: v for k, v in os.environ.items() if k != "CONFIRM"}, stream=output,
            feature=True, before_confirm=confirm, timeout=5)
        self.assertEqual(result, 0)
        self.assertEqual(calls, [True])
        self.assertIn("activated", output.getvalue())

    def test_pty_failure_and_deadline_publish_only_redacted_output(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            run = vm.Run(root, root)
            run.values.append("secret-canary")
            for name, child, message in (
                ("missing-review", 'print("secret-canary"); print("Activate (type yes): ")', "without a complete review"),
                ("deadline", 'import time; print("secret-canary", flush=True); time.sleep(30)', "deadline"),
            ):
                with self.subTest(name=name), self.assertRaisesRegex(vm.Failure, message):
                    run.command(name, [sys.executable, "-c", child], timeout=0.5,
                                delayed=lambda _: self.fail("Unreviewed prompt was accepted"), publish=True)
                published = (root / ("setup-" + name + ".log")).read_text()
                self.assertNotIn("secret-canary", published)
                self.assertIn("[private value withheld]", published)

    def test_socket_wait_requires_natural_exit_and_margin_without_ssh(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "control"
            # The waiter observes only the socket pathname; no listener is needed.
            path.touch()
            now = [0.0]
            def advance(seconds):
                now[0] += seconds
                if now[0] >= 120 and path.exists():
                    path.unlink()
            with patch.object(vm.time, "monotonic", side_effect=lambda: now[0]), \
                    patch.object(vm.time, "sleep", side_effect=advance), \
                    patch.object(vm.subprocess, "Popen") as child:
                vm.wait_control_socket(path, alive=lambda: True)
            self.assertGreaterEqual(now[0], 150)
            self.assertLess(now[0], 151)
            child.assert_not_called()

    def test_socket_reappearance_restarts_margin_and_persistent_socket_times_out(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "control"
            for persistent in (False, True):
                now = [0.0]
                path.touch()
                def advance(seconds):
                    now[0] += seconds
                    exists = persistent or now[0] < 1 or 2 <= now[0] < 4
                    if exists:
                        path.touch()
                    else:
                        path.unlink(missing_ok=True)
                with self.subTest(persistent=persistent), \
                        patch.object(vm.time, "monotonic", side_effect=lambda: now[0]), \
                        patch.object(vm.time, "sleep", side_effect=advance):
                    if persistent:
                        with self.assertRaisesRegex(vm.Failure, "did not disappear"):
                            vm.wait_control_socket(path, alive=lambda: True, timeout=8, minimum=0, margin=3)
                    else:
                        vm.wait_control_socket(path, alive=lambda: True, timeout=8, minimum=0, margin=3)
                        self.assertGreaterEqual(now[0], 7)
            with self.assertRaisesRegex(vm.Failure, "exited during"):
                vm.wait_control_socket(path, alive=lambda: False)


class FailureStateTests(unittest.TestCase):
    def status(self, mode):
        feature = mode == "feature"
        return {"state": {"status": "unknown" if feature else "rolled_back",
            "current": {"version": "bad" if feature else "good"},
            "last_attempt": {"action": "deploy", "phase": "health", "release": "bad",
                **({"operation_id": "operation"} if feature else {}),
                "outcome": "needs_reconciliation" if feature else "failed",
                "error": "operation_cleanup_pending" if feature else "readiness_failed"}}}

    def test_closed_feature_failure_and_standard_auto_restoration(self):
        for mode, expected in (("feature", "bad"), ("standard", "good")):
            self.assertEqual(vm.failed_deploy_state(json.dumps(self.status(mode)), mode,
                                                   "bad", "good", "operation"), expected)

    def test_rejects_wrong_release_phase_outcome_error_and_operation(self):
        for mode in ("feature", "standard"):
            for owner, key, value in (("state", "status", "healthy"), ("current", "version", "other"),
                    ("last_attempt", "phase", "seed"), ("last_attempt", "action", "restart"),
                    ("last_attempt", "release", "other"), ("last_attempt", "outcome", "succeeded"),
                    ("last_attempt", "error", "code_recovery_failed")):
                data = self.status(mode)
                state = data["state"]
                (state if owner == "state" else state[owner])[key] = value
                with self.subTest(mode=mode, key=key), self.assertRaises(vm.Failure):
                    vm.failed_deploy_state(json.dumps(data), mode, "bad", "good", "operation")
        with self.assertRaises(vm.Failure):
            vm.failed_deploy_state(json.dumps(self.status("feature")), "feature", "bad", "good", "other")
        for output in ("not JSON", "{}", '{"state": null}', '{"state": {"current": null}}'):
            with self.subTest(output=output), self.assertRaises(vm.Failure):
                vm.failed_deploy_state(output, "feature", "bad", "good", "operation")


class VMAcceptanceTests(unittest.TestCase):
    def test_pin_is_serial_specific_and_unsupported_release_refuses(self):
        pin = vm.image_pin("24.04")
        self.assertIn("/release-" + pin["serial"] + "/", pin["url"])
        self.assertEqual(len(pin["sha256"]), 64)
        with self.assertRaises(vm.Failure):
            vm.image_pin("26.04")

    def test_seed_has_sudo_admin_marker_and_no_disabled_services(self):
        seed = vm.cloud_config("ssh-ed25519 public-fixture-key")
        self.assertFalse(seed["ssh_pwauth"])
        self.assertTrue(seed["disable_root"])
        self.assertEqual(seed["users"][0]["sudo"], "ALL=(ALL) NOPASSWD:ALL")
        files = {row["path"]: row for row in seed["write_files"]}
        marker = files[vm.VM_MARKER]
        self.assertEqual((marker["owner"], marker["permissions"]), ("root:root", "0600"))
        self.assertNotIn("packages", seed)
        commands = json.dumps(seed["runcmd"])
        for forbidden in ("disable", "mask", "remove", "purge", "policy-rc.d"):
            self.assertNotIn(forbidden, commands)
        self.assertIn("fwupd packagekit", commands)
        self.assertIn("dpkg-query", commands)
        self.assertIn("apt-get install", commands)

    def test_both_fixture_profiles_expand_all_placed_units(self):
        for mode, count in (("feature", 3), ("standard", 3)):
            with self.subTest(mode=mode), tempfile.TemporaryDirectory() as directory:
                snapshot = Path(directory).resolve()
                shutil.copytree(vm.FIXTURE / "deploy", snapshot / "deploy")
                project, profile, topology = vm.fixture_declarations(snapshot, mode, 23456)
                validate_profile(profile, project)
                self.assertIsNone(profile["host"]["host_id"])
                self.assertEqual(profile["host"]["port"], 23456)
                self.assertEqual(profile["architecture"], "amd64")
                units = instance_units({"name": "deployacceptance-acceptance", "profile": profile, "topology": topology})
                self.assertEqual(len(units), count)
                if mode == "feature":
                    self.assertEqual(topology["schema"], 2)
                    self.assertTrue(any("relay@2.service" in unit for unit in units.values()))
                    self.assertNotIn("uploads", profile)
                else:
                    self.assertEqual(topology["schema"], 1)
                    self.assertTrue(any("worker" in unit for unit in units.values()))

    def test_private_material_is_removed_from_public_diagnostics(self):
        original = ("boot ready\nssh-ed25519 public-key\n"
                    "-----BEGIN OPENSSH PRIVATE KEY-----\nprivate\n-----END OPENSSH PRIVATE KEY-----\n"
                    "token=secret-canary\nAGE-SECRET-KEY-ABC\nfailed service\n")
        result = vm.redact(original, ["secret-canary"])
        for private in ("public-key", "\nprivate\n", "secret-canary", "AGE-SECRET-KEY-ABC"):
            self.assertNotIn(private, result)
        self.assertIn("boot ready", result)
        self.assertIn("failed service", result)

    def test_command_separates_json_and_diagnostics_and_keeps_failure_code(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            logs = root / "logs"
            logs.mkdir()
            run = vm.Run(root, logs)
            run.values.append("secret-canary")
            code, output = run.command("json", [sys.executable, "-c",
                'import sys; print(\'{"ok": true}\'); print("warning", file=sys.stderr)'], publish=True)
            self.assertEqual(code, 0)
            self.assertEqual(json.loads(output), {"ok": True})
            with self.assertRaisesRegex(vm.Failure, "exit=7"):
                run.command("failure", [sys.executable, "-c", 'print("secret-canary"); raise SystemExit(7)'], publish=True)
            self.assertNotIn("secret-canary", (logs / "setup-failure.log").read_text())
            with self.assertRaises(vm.Failure):
                run.command("private", [sys.executable, "-c", 'print("private-key"); raise SystemExit(1)'])
            self.assertFalse((logs / "setup-private.log").exists())

    def test_teardown_continues_after_serial_export_failure(self):
        with tempfile.TemporaryDirectory() as directory:
            run = vm.Run(Path(directory), Path(directory))
            with patch.object(run, "diagnostics"), \
                    patch.object(run, "stop_vm", side_effect=OSError("log disk full")), \
                    patch.object(run, "cleanup_builders", return_value=[]) as builders, \
                    patch.object(run.resources, "cleanup", return_value=[]) as storage:
                self.assertEqual(run.cleanup(), ["QEMU/serial"])
                builders.assert_called_once()
                storage.assert_called_once()

    def test_no_kvm_fails_before_downloading_or_booting(self):
        with patch.object(sys, "argv", ["vm_acceptance.py", "--ubuntu", "24.04"]), \
                patch.object(vm.os, "access", return_value=False), \
                patch.object(vm.subprocess, "Popen") as start:
            with self.assertRaisesRegex(vm.Failure, "/dev/kvm"):
                vm.main()
            start.assert_not_called()

    def test_ca_adds_valid_ip_names_without_changing_container_default(self):
        for addresses, expected in (((), "subjectAltName=DNS:s3\n"),
                                    (("127.0.0.1", "10.0.2.2"), "subjectAltName=DNS:s3,IP:127.0.0.1,IP:10.0.2.2\n")):
            with tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                def openssl(args):
                    for option in ("-keyout", "-out"):
                        if option in args:
                            Path(args[args.index(option) + 1]).touch()
                with patch.object(vm.containers, "run", side_effect=openssl):
                    vm.containers.create_ca(root, addresses=addresses)
                self.assertIn(expected, (root / "s3.ext").read_text())
