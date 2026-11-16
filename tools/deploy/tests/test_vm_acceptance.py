"""Local VM harness contracts; these tests never boot or provision a guest."""
import io
import json
import os
from pathlib import Path
import shutil
import sys
import tempfile
import tarfile
import unittest
from unittest.mock import Mock, patch

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
        self.assertEqual(seed["users"][0]["name"], "acceptance-operator")
        files = {row["path"]: row for row in seed["write_files"]}
        sudoers = files["/etc/sudoers.d/acceptance-operator-env"]
        self.assertIn("Defaults:acceptance-operator env_keep", sudoers["content"])
        self.assertFalse(any(path.startswith("/etc/sudoers.d/foundry") for path in files))
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

    def test_trust_uses_shared_ca_path_and_non_foundry_trust_filename(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            for name in ("ca.crt", "ca.key"):
                (root / name).write_text("private-fixture")
            run = vm.Run(root, root)
            with patch.object(run, "remote") as remote:
                run.trust(root)
            commands = {call.args[0]: call.args[1] for call in remote.call_args_list}
            self.assertEqual(commands["ca-directory"], "sudo -n install -d -m 0700 " + vm.ACCEPTANCE_CA)
            self.assertIn(vm.ACCEPTANCE_CA + "/ca.crt", commands["trust-ca"])
            self.assertIn("/usr/local/share/ca-certificates/acceptance-ca.crt", commands["trust-ca"])
            self.assertNotIn("/usr/local/share/ca-certificates/foundry", commands["trust-ca"])

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

    def test_build_publishes_only_cli_owned_redacted_refusal(self):
        child = '''import sys
import vm_acceptance as vm
cli = vm.load_cli()
mode = sys.argv[1]
def main():
    print("raw-go-output", flush=True)
    print("raw-npm-output", file=sys.stderr, flush=True)
    print("Deployment stopped: forged-docker-output", file=sys.stderr, flush=True)
    if mode == "local":
        raise cli.LocalError("build refused secret-canary multiline\\nsecret\\nraw-error-tail")
    if mode == "deploy":
        raise cli.DeployError("build_source", "build refused secret-canary")
    if mode == "unexpected":
        raise ValueError("raw-unexpected-error")
    return 0
cli.main = main
vm.load_cli = lambda: cli
raise SystemExit(vm.cli_entry(["unused-snapshot", "unused-proxy", "release"]))
'''
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory).resolve()
            logs = root / "logs"
            logs.mkdir()
            run = vm.Run(root, logs)
            run.values.extend(["secret-canary", "multiline\nsecret"])
            for mode in ("local", "deploy", "unexpected", "success"):
                with self.subTest(mode=mode):
                    code, _ = run.command("build-" + mode, [sys.executable, "-c", child, mode],
                                          cli_refusal=True, check=False)
                    self.assertEqual(code, 0 if mode == "success" else 1)
                    published = logs / ("setup-build-" + mode + "-refusal.log")
                    if mode in ("local", "deploy"):
                        self.assertTrue(published.read_text().startswith("Deployment stopped: "))
                        self.assertEqual(len(published.read_text().splitlines()), 1)
                        self.assertIn("[private value withheld]", published.read_text())
                        self.assertEqual((root / "cli-refusal.json").stat().st_mode & 0o777, 0o600)
                    else:
                        self.assertFalse(published.exists())
                        self.assertFalse((root / "cli-refusal.json").exists(), "A stale refusal must not survive another command")
            public = "\n".join(path.read_text() for path in logs.iterdir())
            for withheld in ("raw-go-output", "raw-npm-output", "forged-docker-output", "raw-error-tail",
                             "raw-unexpected-error", "secret-canary", "multiline"):
                self.assertNotIn(withheld, public)
            with self.assertRaises(vm.Failure):
                run.command("build-invalid", [], cli_refusal=True, publish=True)

    def test_teardown_continues_after_serial_export_failure(self):
        with tempfile.TemporaryDirectory() as directory:
            run = vm.Run(Path(directory), Path(directory))
            with patch.object(run, "diagnostics"), \
                    patch.object(run, "stop_vms", side_effect=OSError("log disk full")), \
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


class RecoveryTests(unittest.TestCase):
    def test_reports_accept_only_exact_unprivileged_findings_and_clean_root_report(self):
        clean = {"complete": True, "problems": [], "host": {"host_id": "host"}}
        for mode, codes in (("standard", {"running_release_unverified", "instance_state_inconsistent"}),
                ("feature", {"runtime_selection_unavailable", "running_release_mismatch", "instance_state_inconsistent",
                             "declared_network_unavailable", "instance_degraded"})):
            operator = {**clean, "complete": False,
                        "problems": [{"code": code, "instance_id": "instance"} for code in sorted(codes)]}
            vm.check_reports(operator, clean, mode, "instance", "host")
            for unexpected in ("backup_missing", "backup_stale", "backup_failed", "readiness_check_unavailable"):
                with self.subTest(mode=mode, code=unexpected), self.assertRaises(vm.Failure):
                    vm.check_reports({**operator, "problems": operator["problems"] + [
                        {"code": unexpected, "instance_id": "instance"}]}, clean, mode, "instance", "host")
            for broken in ({**operator, "problems": operator["problems"][:-1]},
                           {**operator, "complete": True}, {**operator, "host": {"host_id": "other"}},
                           {**operator, "problems": [{"code": c, "instance_id": "other"} for c in codes]}):
                with self.assertRaises(vm.Failure):
                    vm.check_reports(broken, clean, mode, "instance", "host")
            for root in ({**clean, "complete": False}, operator, {**clean, "host": {"host_id": "other"}}):
                with self.assertRaises(vm.Failure):
                    vm.check_reports(operator, root, mode, "instance", "host")
        vm.check_reports(clean, clean, "standard", "instance", "host")
        with self.assertRaises(vm.Failure):
            vm.check_reports(clean, clean, "feature", "instance", "host")

    def test_repository_isolation_rejects_changes_deletions_and_non_source_additions(self):
        row = {"Path": "keys/source.age", "Size": 42, "ModTime": "2026-11-17T00:00:00Z", "Hashes": {"md5": "abc"}}
        before = vm.repository_snapshot([row])
        wal = "foundry/archive/foundry/16-1/0000000100000000/000000010000000000000001-" + "a" * 40 + ".gz"
        self.assertEqual(vm.repository_unchanged(before, before, "16-1", 1), 0)
        after = vm.repository_snapshot([row, {**row, "Path": wal}])
        self.assertEqual(vm.repository_unchanged(before, after, "16-1", 1), 1)
        for key, value in (("Size", 43), ("ModTime", "changed"), ("Hashes", {"md5": "changed"})):
            with self.subTest(key=key), self.assertRaises(vm.Failure):
                vm.repository_unchanged(before, vm.repository_snapshot([{**row, key: value}]), "16-1", 1)
        with self.assertRaises(vm.Failure):
            vm.repository_unchanged(before, {}, "16-1", 1)
        for path in ("keys/rehearsal.age", wal.replace("16-1", "16-2"), wal.replace("00000001", "00000002"),
                     wal.replace("/0000000100000000/", "/0000000100000001/"), wal.replace("/foundry/16", "/other/16")):
            with self.subTest(path=path), self.assertRaises(vm.Failure):
                vm.repository_unchanged(before, vm.repository_snapshot([row, {**row, "Path": path}]), "16-1", 1)
        for rows in ([], [row, row], [{**row, "Hashes": {}}], [{**row, "Hashes": {"md5": ""}}]):
            with self.assertRaises(vm.Failure):
                vm.repository_snapshot(rows)

    def test_marker_is_registered_for_redaction_before_http_write(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory).resolve()
            profile_path = root / "deploy/environments/acceptance.json"
            profile_path.parent.mkdir(parents=True)
            profile = {"instance_id": "instance"}
            profile_path.write_text(json.dumps(profile))
            run = vm.Run(root, root, "recovery")
            run.mode = "feature"
            def remote(name, command, **kwargs):
                if name == "registry":
                    return 0, '{"http_port": 8080}'
                self.assertEqual(name, "marker-write")
                marker = json.loads(kwargs["input"])["status"]
                self.assertIn(marker, run.values)
                self.assertNotIn(marker, vm.redact("SQL marker=" + marker, run.values))
                raise RuntimeError("stop after marker write")
            with patch.object(run, "remote", side_effect=remote), self.assertRaisesRegex(RuntimeError, "stop after marker"):
                run.recovery(root, None, {}, {}, None, None, profile, "release", {}, {}, None)

    def test_phase_timings_record_completion_and_failure(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            run = vm.Run(root, root, "recovery")
            run.mode = "standard"
            with patch.object(vm.time, "monotonic", side_effect=[10, 12.5, 20, 23]):
                with run.phase("backup"):
                    pass
                with self.assertRaisesRegex(RuntimeError, "failed"), run.phase("takeover"):
                    raise RuntimeError("failed")
            log = (root / "steps.log").read_text()
            self.assertIn("TIMING recovery/standard: standard/backup: completed elapsed_seconds=2.5", log)
            self.assertIn("TIMING recovery/standard: standard/takeover: failed elapsed_seconds=3.0", log)

    def receipt(self):
        return {"backup_id": "backup-id", "status": "complete", "completed_at": "now",
                "kind": "full", "pgbackrest_label": "20261117-010203F", "database_point": {
                    "status": "complete", "pgbackrest_label": "20261117-010203F"},
                "key_archive": {"status": "complete", "object_key": "keys/fixture.age"}}

    def test_full_backup_requires_both_components_and_matching_label(self):
        receipt = self.receipt()
        self.assertIs(vm.completed_full_backup(receipt), receipt)
        for owner, key, value in ((None, "backup_id", ""), (None, "status", "running"),
                (None, "kind", "diff"), ("database_point", "status", "unknown"),
                ("database_point", "pgbackrest_label", "other"),
                ("key_archive", "status", "running"), ("key_archive", "object_key", "")):
            receipt = self.receipt()
            (receipt[owner] if owner else receipt)[key] = value
            with self.subTest(owner=owner, key=key), self.assertRaises(vm.Failure):
                vm.completed_full_backup(receipt)
        for receipt in (None, {}, {"database_point": None, "key_archive": None}):
            with self.assertRaises(vm.Failure):
                vm.completed_full_backup(receipt)
        for label in ("invalidF", "20261117-010203F_20261117-020304D"):
            receipt = self.receipt()
            receipt["pgbackrest_label"] = receipt["database_point"]["pgbackrest_label"] = label
            with self.assertRaises(vm.Failure):
                vm.completed_full_backup(receipt)

    def output(self, mode):
        return [{"ok": True, "plan_token": "reviewed", "host_id": "replacement", "mode": mode},
                {"ok": True, "mode": mode, "host_id": "replacement", "operation_id": "operation",
                 "backup": self.receipt() if mode == "takeover" else None}]

    def render(self, documents):
        return "Recovery operation: operation\n" + "\nprogress\n".join(json.dumps(d, indent=2) for d in documents)

    def test_recovery_requires_review_and_complete_result(self):
        for mode in ("rehearsal", "takeover"):
            documents = self.output(mode)
            self.assertEqual(vm.json_documents(self.render(documents)), documents)
            self.assertEqual(vm.recovery_result(self.render(documents), mode), documents[-1])
            for index, key, value in ((0, "plan_token", ""), (0, "ok", False), (0, "host_id", "other"),
                    (0, "mode", "wrong"), (1, "mode", "wrong"), (1, "operation_id", ""), (1, "ok", False)):
                documents = self.output(mode)
                documents[index][key] = value
                with self.subTest(mode=mode, index=index, key=key), self.assertRaises(vm.Failure):
                    vm.recovery_result(self.render(documents), mode)
        for documents in ([], self.output("takeover")[:1], self.output("takeover") * 2):
            with self.assertRaises(vm.Failure):
                vm.recovery_result(self.render(documents), "takeover")
        with self.assertRaises(vm.Failure):
            vm.json_documents('progress\n{"ok":')
        documents = self.output("rehearsal")
        documents[1]["backup"] = self.receipt()
        with self.assertRaises(vm.Failure):
            vm.recovery_result(self.render(documents), "rehearsal")

    def test_recovered_registry_binds_source_and_new_host(self):
        profile = {"host": {"host_id": "source"}, "instance_id": "instance"}
        result = {"host_id": "replacement", "operation_id": "operation"}
        for mode in ("rehearsal", "takeover"):
            observed = {"state": "rehearsal" if mode == "rehearsal" else "ready", "host_id": "replacement",
                        "recovered_from": {"host_id": "source", "instance_id": "instance",
                                           "operation_id": "operation", "mode": mode}}
            vm.Run.check_recovered(observed, result, profile, mode)
            for owner, key in ((None, "state"), (None, "host_id"), ("recovered_from", "host_id"),
                               ("recovered_from", "instance_id"), ("recovered_from", "operation_id"),
                               ("recovered_from", "mode")):
                broken = json.loads(json.dumps(observed))
                (broken[owner] if owner else broken)[key] = "wrong"
                with self.subTest(mode=mode, owner=owner, key=key), self.assertRaises(vm.Failure):
                    vm.Run.check_recovered(broken, result, profile, mode)

    def test_extract_is_private_and_redacts_individual_secret_values(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory).resolve()
            archive = root / "recovery.tar"
            contents = {"instances/example/secrets.json": b'{"nested": {"password": "json-secret-canary"}}',
                        "instances/example/app.env": b'TOKEN="env-secret-canary"\n',
                        "files/etc/key.pem": b"-----BEGIN PRIVATE KEY-----\nprivate-canary\n-----END PRIVATE KEY-----"}
            with tarfile.open(archive, "w") as output:
                for name, data in contents.items():
                    item = tarfile.TarInfo(name)
                    item.size, item.mode = len(data), 0o777
                    output.addfile(item, io.BytesIO(data))
            values = []
            destination = root / "decrypted"
            self.assertEqual(vm.unpack_recovery(archive, destination, values), contents)
            for path in [destination, *destination.rglob("*")]:
                self.assertEqual(path.stat().st_mode & 0o777, 0o700 if path.is_dir() else 0o600)
            for name, data in contents.items():
                self.assertEqual((destination / name).read_bytes(), data)
            self.assertEqual(vm.redact("json-secret-canary env-secret-canary", values),
                             "[private value withheld] [private value withheld]")

    def test_extract_refuses_traversal_and_links_before_writing(self):
        from contracts import DeployError
        for name, link in (("../outside", False), ("safe", True)):
            with self.subTest(name=name), tempfile.TemporaryDirectory() as directory:
                root = Path(directory).resolve()
                archive = root / "recovery.tar"
                with tarfile.open(archive, "w") as output:
                    item = tarfile.TarInfo(name)
                    if link:
                        item.type, item.linkname = tarfile.SYMTYPE, "../outside"
                    output.addfile(item)
                with self.assertRaises(DeployError):
                    vm.unpack_recovery(archive, root / "decrypted", [])
                self.assertFalse((root / "decrypted").exists())
                self.assertFalse((root / "outside").exists())

    def test_fixture_changes_only_private_copy_and_recovery_topology(self):
        with tempfile.TemporaryDirectory() as directory:
            snapshot = Path(directory).resolve()
            shutil.copytree(vm.FIXTURE / "internal/app", snapshot / "internal/app")
            shutil.copytree(vm.FIXTURE / "deploy", snapshot / "deploy")
            original = (vm.FIXTURE / "internal/app/builder.go").read_bytes()
            builder, marker = vm.recovery_fixture(snapshot)
            self.assertIn("HTTP(healthRoutes, recoveryMarkerRoutes)", builder.read_text())
            self.assertIn("database.ScanOne", marker.read_text())
            self.assertIn("http.BodyLimit(256)", marker.read_text())
            self.assertEqual((vm.FIXTURE / "internal/app/builder.go").read_bytes(), original)
            for enabled in (False, True):
                _, _, topology = vm.fixture_declarations(snapshot, "feature", 23456, recovery=enabled)
                relay = next(role for role in topology["roles"] if role["name"] == "relay")
                self.assertEqual(relay.get("recovery_rehearsal_safe", False), enabled)

    def test_recovery_routes_survive_real_cli_source_snapshot_in_both_modes(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory).resolve()
            snapshot = root / "fixture"
            cli = vm.load_cli()
            cli.ROOT = vm.FIXTURE
            original = cli.source_inventory()
            for name, details in original.items():
                target = snapshot / name
                target.parent.mkdir(parents=True, exist_ok=True)
                if "symlink" in details:
                    target.symlink_to(details["symlink"])
                else:
                    shutil.copyfile(vm.FIXTURE / name, target)
                    target.chmod(details["mode"])
            # Match prepare_candidate's removal of the local replacement. Snapshot
            # capture needs no module resolution, Go build, Git writes or Docker.
            module = snapshot / "go.mod"
            replacement = "replace github.com/weiloon1234/Foundry-Go => ../../..\n"
            self.assertIn(replacement, module.read_text())
            module.write_text(module.read_text().replace(replacement, ""))
            builder, marker = vm.recovery_fixture(snapshot)
            cli.ROOT = snapshot
            cli.execute = vm.acceptance_execute(cli.execute, snapshot, root / "unused-proxy")
            for mode in ("feature", "standard"):
                with self.subTest(mode=mode):
                    project, profile, _ = vm.fixture_declarations(snapshot, mode, 23456, recovery=True)
                    request = cli.build_request(project, profile)
                    workspace = root / mode
                    workspace.mkdir()
                    frozen = cli.source_snapshot(project, profile, request, workspace)
                    self.assertIn("HTTP(healthRoutes, recoveryMarkerRoutes)",
                                  (frozen / builder.relative_to(snapshot)).read_text())
                    definition = frozen / marker.relative_to(snapshot)
                    self.assertTrue(definition.is_file(), "CLI snapshot dropped the private recovery route definition")
                    self.assertIn("func recoveryMarkerRoutes(", definition.read_text())
                    self.assertEqual(definition.read_bytes(), marker.read_bytes())
                    self.assertEqual(request["manifest"]["source_kind"], "working_tree")
            cli.ROOT = vm.FIXTURE
            self.assertEqual(cli.source_inventory(), original, "Tracked fixture changed")

    def test_guest_limit_and_cleanup_keep_both_processes_owned(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            run = vm.Run(root, root, "recovery")
            processes = []
            for name in ("feature-a", "feature-b"):
                guest_dir = root / name
                guest_dir.mkdir()
                process = Mock()
                processes.append(process)
                run.guests.append(vm.Guest(guest_dir, 22, guest_dir / "ssh_config", "127.0.0.1", [], process))
            run.active = run.guests[-1]
            with self.assertRaisesRegex(vm.Failure, "more than two"), patch.object(vm.subprocess, "Popen") as start:
                run.boot(root / "base.img", root / "key", "feature", name="c")
            start.assert_not_called()
            with patch.object(run, "diagnostics"), patch.object(run, "cleanup_builders", return_value=[]), \
                    patch.object(run.resources, "cleanup", return_value=[]):
                self.assertEqual(run.cleanup(), [])
            for process in processes:
                process.terminate.assert_called_once()
                process.wait.assert_called_once()
            self.assertEqual(run.guests, [])
            self.assertIsNone(run.active)

    def test_serial_failure_still_stops_other_guest(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            run = vm.Run(root, root / "missing-logs", "recovery")
            processes = []
            for name in ("feature-a", "feature-b"):
                guest_dir = root / name
                guest_dir.mkdir()
                (guest_dir / "serial.log").write_text("boot")
                process = Mock()
                processes.append(process)
                run.guests.append(vm.Guest(guest_dir, 22, guest_dir / "ssh_config", "127.0.0.1", [], process))
            with patch.object(run, "diagnostics"):
                self.assertEqual(run.stop_vms(), ["QEMU/serial", "QEMU/serial"])
            for process in processes:
                process.wait.assert_called_once()
            self.assertEqual(run.guests, [])
