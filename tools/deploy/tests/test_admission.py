"""Persistent start authority with filesystem/process evidence, no host actions."""
from dataclasses import replace
import os
from pathlib import Path
import sys
import tempfile
from concurrent.futures import ThreadPoolExecutor
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "lib"))
import admission
import contracts as c
from host import Host, Paths
from test_host_capabilities import (INSTANCE, OPERATION, BOOT, CURRENT, CLOSURE, PERMIT,
                                    DIGEST, OTHER, INVOCATION, admission as admission_record,
                                    closure, resolution, selection, unit)


def attempt(outcome="running", phase="health", operation=OPERATION):
    return {"id": "20261113T000000Z-abcdef12", "action": "deploy", "release": "20261113T000000Z-abcdef12",
            "started_at": "2026-11-13T00:00:00Z", "finished_at": None if outcome == "running" else "2026-11-13T00:00:01Z",
            "outcome": outcome, "phase": phase, "error": None, "operation_id": operation}


def helper(role="http", invocation=INVOCATION, spec=DIGEST):
    return c.HelperInvocation(BOOT, c.UnitIdentity(INSTANCE, role, 1), c.ProcessIdentity(22, 100, invocation),
                              OTHER, spec, CURRENT, 1, None)


def fake_executor(host):
    root = host.paths.p("/proc/20")
    root.mkdir(parents=True, exist_ok=True)
    # /proc stat field 22 is starttime (index 19 after comm).
    (root / "stat").write_text("20 (operation owner) " + " ".join(["S"] + ["0"] * 18 + ["42"]))
    (root / "environ").write_bytes(b"INVOCATION_ID=" + b"d" * 32 + b"\0")
    return c.ProcessIdentity(20, 42, "d" * 32)


class AdmissionTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory(dir=Path(tempfile.gettempdir()).resolve())
        self.addCleanup(temporary.cleanup)
        self.host = Host(Paths(temporary.name))
        self.store = admission.AdmissionStore(self.host, INSTANCE)
        boot = self.host.paths.p("/proc/sys/kernel/random/boot_id")
        boot.parent.mkdir(parents=True)
        boot.write_text(BOOT)
        self.resolution = c.RuntimeResolution.parse(resolution())
        self.selection = c.DeploymentSelection.parse(selection())
        self.closure = c.ClosureEvidence.parse(closure())
        self.executor = fake_executor(self.host)
        self.permit = c.ValidationPermit(PERMIT, OPERATION, BOOT, CURRENT, CLOSURE, self.executor,
                                          (c.UnitIdentity.parse(unit()),))
        self.store.record_operation(attempt())
        self.store.begin(self.selection, self.resolution)
        self.store.record_boot_closure(self.closure)

    def test_protected_directories_have_explicit_modes_despite_private_umask(self):
        target = self.host.paths.p("/run/prepared/nested/config")
        private = self.host.paths.p("/run/evidence/private")
        previous = os.umask(0o077)
        try:
            admission.protected_directory(self.host, target)
            admission.protected_directory(self.host, private, 0o700)
        finally:
            os.umask(previous)
        for directory in (target, target.parent, target.parent.parent):
            self.assertEqual(directory.stat().st_mode & 0o777, 0o755)
            self.assertEqual(directory.stat().st_uid, os.geteuid())
            self.assertEqual(directory.stat().st_gid, os.getegid())
        self.assertEqual(private.stat().st_mode & 0o777, 0o700)
        admission.protected_directory(self.host, private)
        self.assertEqual(private.stat().st_mode & 0o777, 0o700)

    def commit(self):
        self.store.record_operation(attempt("succeeded"))
        self.store.commit()

    def assert_denied(self, action, code=None):
        with self.assertRaises((c.DeployError, OSError)) as raised:
            action()
        if code:
            self.assertEqual(code, raised.exception.code)

    def test_pending_blocks_without_allowance_and_survives_new_owner(self):
        self.assert_denied(lambda: self.store.admit(helper()))
        reopened = admission.AdmissionStore(self.host, INSTANCE)
        self.assert_denied(lambda: reopened.admit(helper()))
        self.assertFalse((self.store.runtime / "admitted").exists())

    def test_independent_cleanup_blocks_validation_and_committed_starts_until_completed(self):
        self.store.cleanup_status(OPERATION, "0" * 32, "reserved")
        self.store.cleanup_status(OPERATION, INVOCATION, "armed")
        self.store.issue_permit(self.permit, self.closure)
        self.store.admit(helper(), permit_id=PERMIT)
        for status in ("pending", "closed"):
            self.store.cleanup_status(OPERATION, INVOCATION, status)
            reopened = admission.AdmissionStore(self.host, INSTANCE)
            self.assert_denied(lambda: reopened.admit(helper(), permit_id=PERMIT), "operation_cleanup_pending")
        self.commit()
        self.assert_denied(lambda: self.store.admit(helper()), "operation_cleanup_pending")
        self.store.cleanup_status(OPERATION, INVOCATION, "complete")
        self.assertEqual(self.resolution, self.store.admit(helper()))

    def test_cleanup_reservation_denies_unrelated_operation_and_invocation_reuse(self):
        self.store.cleanup_status(OPERATION, "0" * 32, "reserved")
        self.store.require_cleanup_clear(OPERATION)
        self.assert_denied(lambda: self.store.require_cleanup_clear(CURRENT), "operation_cleanup_pending")
        self.store.cleanup_status(OPERATION, INVOCATION, "armed")
        self.assert_denied(lambda: self.store.cleanup_status(OPERATION, "e" * 32, "complete"), "operation_cleanup")

    def test_role_named_selection_cannot_alias_the_selection_lock(self):
        self.assert_denied(lambda: self.store.admit(helper(role="selection")), "admission_denied")

    def test_claim_is_one_invocation_and_preparation_does_not_reclaim(self):
        self.store.issue_permit(self.permit, self.closure)
        self.store.admit(helper(), permit_id=PERMIT)
        with self.store.preparation(helper(), permit_id=PERMIT) as result:
            self.assertEqual(self.resolution, result)
        self.assertEqual(self.resolution, self.store.admit(helper(), permit_id=PERMIT))
        self.assert_denied(lambda: self.store.admit(helper(invocation="e" * 32), permit_id=PERMIT), "admission_allowance_used")
        self.assert_denied(lambda: self.store.issue_permit(self.permit, self.closure), "admission_permit_exists")

    def test_concurrent_claims_have_exactly_one_winner(self):
        self.store.issue_permit(self.permit, self.closure)
        def start(identity):
            try:
                self.store.admit(helper(invocation=identity), permit_id=PERMIT)
                return True
            except c.DeployError:
                return False
        with ThreadPoolExecutor(max_workers=2) as workers:
            self.assertEqual(1, sum(workers.map(start, ("a" * 32, "b" * 32))))

    def test_preparation_cannot_switch_to_an_unclaimed_permit(self):
        self.store.issue_permit(self.permit, self.closure)
        self.store.admit(helper(), permit_id=PERMIT)
        other = replace(self.permit, id=CLOSURE)
        self.store.issue_permit(other, self.closure)
        with self.assertRaises(c.DeployError):
            with self.store.preparation(helper(), permit_id=CLOSURE):
                self.fail("Preparation used an unclaimed allowance")
        self.store.revoke_permit(PERMIT)
        self.assert_denied(lambda: self.store.admit(helper(), permit_id=PERMIT))
        self.assert_denied(lambda: self.store.issue_permit(self.permit, self.closure), "admission_permit_exists")

    def test_permit_requires_live_executor_and_permitted_phase(self):
        self.store.issue_permit(self.permit, self.closure)
        for change in ("pid_reuse", "invocation", "zombie", "migration"):
            with self.subTest(change=change):
                fake_executor(self.host)
                self.store.record_operation(attempt())
                proc = self.host.paths.p("/proc/20")
                if change == "pid_reuse":
                    (proc / "stat").write_text((proc / "stat").read_text().replace("42", "43"))
                elif change == "invocation":
                    (proc / "environ").write_bytes(b"INVOCATION_ID=" + b"f" * 32 + b"\0")
                elif change == "zombie":
                    (proc / "stat").write_text((proc / "stat").read_text().replace(") S", ") Z"))
                else:
                    self.store.record_operation(attempt(phase="migration"))
                self.assert_denied(lambda: self.store.admit(helper(), permit_id=PERMIT), "admission_permit_invalid")

    def test_executor_cpu_counters_can_change_without_changing_identity(self):
        original = Path.read_text
        def read(path, *args, **kwargs):
            value = original(path, *args, **kwargs)
            if path == self.host.paths.p("/proc/20/stat"):
                path.write_text(value.replace(") S 0", ") R 1"))
            return value
        with patch.object(Path, "read_text", read):
            self.assertTrue(admission.executor_alive(self.host, self.executor))

    def test_complete_closure_is_required_not_ingress_only(self):
        for change in (replace(self.closure, stage=c.ClosureStage.INGRESS_WITHDRAWN),
                       replace(self.closure, candidate_resolution_id=OPERATION),
                       replace(self.closure, endpoints=())):
            with self.subTest(change=change):
                self.assert_denied(lambda: self.store.issue_permit(self.permit, change), "admission_closed_required")

    def test_preparation_without_receipt_or_changed_spec_denies(self):
        self.commit()
        def prepare(invocation):
            with self.store.preparation(invocation):
                self.fail("Unadmitted preparation entered")
        self.assert_denied(lambda: prepare(helper()))
        self.store.admit(helper())
        self.assert_denied(lambda: prepare(helper(spec=OTHER)), "admission_invocation")

    def test_committed_starts_require_terminal_journals_and_enabled_nonheld_role(self):
        self.commit()
        self.assertEqual(self.resolution, self.store.admit(helper()))
        self.store.record_operation(attempt(operation=PERMIT))
        self.assert_denied(lambda: self.store.admit(helper(invocation="f" * 32)), "admission_journal")
        self.store.record_operation(attempt("failed", operation=PERMIT))
        self.assertEqual(self.resolution, self.store.admit(helper(invocation="f" * 32)))
        for update in ({"enabled_roles": []}, {"maintenance_holds": ["http"]}):
            raw = c.record_document(self.store._selection()[0])
            raw.update(update)
            c.atomic_json(self.store.durable / "selection.json", raw, mode=0o600)
            self.assert_denied(lambda: self.store.admit(helper()))

    def test_commit_does_not_infer_completion_from_pointer_or_health(self):
        self.assert_denied(self.store.commit, "admission_journal")
        self.store.record_operation(attempt("needs_reconciliation"))
        self.assert_denied(self.store.commit, "admission_journal")
        self.store.record_operation(attempt("failed"))
        self.assert_denied(self.store.commit, "admission_journal")

    def test_missing_malformed_mismatched_and_previous_boot_evidence_deny(self):
        self.commit()
        for path, content in ((self.store.durable / "selection.json", "{}"),
                              (self.store.durable / "resolution.json", "not json"),
                              (self.store.runtime / "boot.json", "{}")):
            before = path.read_bytes()
            path.write_text(content)
            self.assert_denied(lambda: self.store.admit(helper()))
            path.write_bytes(before)
        path = self.store.durable / "resolution.json"
        c.atomic_json(path, c.record_document(replace(self.resolution, private_revision=8)), mode=0o600)
        self.assert_denied(lambda: self.store.admit(helper()), "admission_selection")
        c.atomic_json(path, c.record_document(self.resolution), mode=0o600)
        self.host.paths.p("/proc/sys/kernel/random/boot_id").write_text(PERMIT)
        self.assert_denied(lambda: self.store.admit(replace(helper(), boot_id=PERMIT)), "admission_boot_required")
        (self.store.runtime / "boot.json").unlink()
        self.assert_denied(lambda: self.store.admit(replace(helper(), boot_id=PERMIT)), "admission_missing")

    def test_control_symlinks_and_writable_evidence_are_refused(self):
        self.commit()
        path = self.store.runtime / "boot.json"
        path.chmod(0o666)
        self.assert_denied(lambda: self.store.admit(helper()), "admission_ownership")
        path.chmod(0o600)
        target = path.with_name("original.json")
        path.rename(target)
        path.symlink_to(target.name)
        self.assert_denied(lambda: self.store.admit(helper()), "unsafe_path")

    def test_pending_intent_blocks_previous_committed_selection(self):
        self.commit()
        self.store.record_operation(attempt(operation=PERMIT))
        self.assert_denied(lambda: self.store.admit(helper()), "admission_journal")


if __name__ == "__main__":
    unittest.main()
