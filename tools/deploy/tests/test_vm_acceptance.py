"""Local VM harness contracts; these tests never boot or provision a guest."""
import json
from pathlib import Path
import shutil
import sys
import tempfile
import unittest
from unittest.mock import patch

import vm_acceptance as vm
from contracts import validate_profile
from topology import instance_units


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
