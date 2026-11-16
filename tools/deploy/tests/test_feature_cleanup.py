"""Incident hook interleavings and closed-operation same-host recovery."""
from contextlib import redirect_stderr
import io
import os
import uuid
from unittest.mock import Mock, patch

from test_feature_wiring import (FeatureWiringFixture, admission, c, feature_runtime,
                                 lifecycle, operations, services)


class FeatureCleanupTests(FeatureWiringFixture):
    def prepared(self):
        self.build_archive(independent=True, public=True)
        self.configure_firewall()
        plan = self.request("bootstrap-plan", topology=self.topology.projection())
        self.request("bootstrap-apply", topology=self.topology.projection(),
                     plan_token=plan["plan_token"], confirm=True)
        self.request("activate", archive=str(self.archive), archive_sha256=c.digest(self.archive))
        self.previous_version = self.version
        self.build_archive(version="20261116T095532Z-abcdef12", independent=True,
                           public=True, application_content=b"frontend change")

    def incident(self, *, stopped_role=None):
        self.prepared()
        if stopped_role:
            self.request("stop", role=stopped_role)
        operation = str(uuid.uuid4())
        with patch.object(admission.AdmissionStore, "bind_workload",
                          side_effect=c.DeployError("admission_invocation", "private-path secret-canary")), \
                redirect_stderr(io.StringIO()), self.assertRaises(c.DeployError):
            self.request("activate", archive=str(self.archive), archive_sha256=c.digest(self.archive),
                         operation_id=operation)
        _, self.entry = self.host.require_identity(self.project, self.profile)
        self.store, _, self.inventory, _ = lifecycle.load(self.host, self.entry)
        self.root = operations.directory(self.host, operation)
        self.cleanup_path = self.store.durable / "cleanup" / (operation + ".json")
        self.assertEqual(c.read_json(self.cleanup_path)["status"], "closed")
        attempt = self.host.load_state(self.entry)["last_attempt"]
        self.assertEqual((attempt["outcome"], attempt["error"], attempt["phase"]),
                         ("needs_reconciliation", "operation_cleanup_pending", "health"))
        for name in ("transition.json", "cleanup-closure.json", "validated.json", "result.json"):
            self.assertTrue((self.root / name).exists(), name)
        self.assertTrue((self.store.runtime / "active-permit").exists())
        self.assertFalse(self.host.running)
        return operation

    def test_pending_armed_hooks_interleave_without_losing_the_permit(self):
        self.prepared()
        run = feature_runtime.run
        deferred, phases = [], []

        def hooks(action, pin, launch):
            phases.append((pin["unit"]["role"], action))
            store = admission.AdmissionStore(self.host, self.profile["instance_id"])
            selection, resolution = store._selection()
            self.assertEqual(selection.admission.disposition, c.AdmissionDisposition.PENDING)
            self.assertIsNone(store.retained_invocation(c.HelperInvocation.parse(pin), resolution))
            cleanup = c.read_json(store.durable / "cleanup" / (selection.admission.operation_id + ".json"))
            self.assertEqual(cleanup["status"], "armed")
            permit = (store.runtime / "active-permit").read_text()
            if action == "running" and not deferred:
                deferred.append((pin, launch))
                return 0
            if action == "running":
                # Both workloads exist. Complete B's hook before A's delayed hook.
                self.assertEqual(run(action, pin, launch), 0)
                first, first_launch = deferred[0]
                for invocation in (first, pin):
                    identity = c.UnitIdentity.parse(invocation["unit"])
                    claim = store.runtime / "permits" / permit / (identity.record_key + ".claim")
                    self.assertEqual(claim.read_text(), invocation["process"]["invocation_id"])
                unit = next(u for u, (_, _, identity) in self.host.running.items()
                            if identity.role == first["unit"]["role"])
                group = self.host.paths.p("/proc/self/cgroup")
                original = group.read_text()
                self.host.owned_write(group, "0::/system.slice/" + unit)
                try:
                    self.assertEqual(run(action, first, first_launch), 0)
                finally:
                    self.host.owned_write(group, original)
                return 0
            return run(action, pin, launch)

        with patch.object(feature_runtime, "run", side_effect=hooks):
            result = self.request("activate", archive=str(self.archive), archive_sha256=c.digest(self.archive))
        self.assertEqual(result["status"], "healthy")
        self.assertEqual([action for _, action in phases], ["admit", "prepare", "running"] * 2)

    def test_refusal_prints_action_and_code_without_private_detail(self):
        output = io.StringIO()
        with patch.object(feature_runtime, "selected", side_effect=c.DeployError(
                "operation_cleanup_pending", "/private/path secret-canary")), redirect_stderr(output):
            self.assertEqual(feature_runtime.run("running", {}, {}), 1)
        self.assertEqual(output.getvalue(), "feature_runtime_refused: running: operation_cleanup_pending\n")

    def test_refusal_does_not_print_a_malformed_error_code_or_action(self):
        output = io.StringIO()
        with patch.object(feature_runtime, "selected", side_effect=c.DeployError(
                "/private/path\nsecret-canary", "secret-canary")), redirect_stderr(output):
            self.assertEqual(feature_runtime.run("/private/action", {}, {}), 1)
        self.assertEqual(output.getvalue(), "feature_runtime_refused: unknown: invalid_error_code\n")

    def test_closed_cleanup_reconcile_leaves_stopped_then_reviewed_rollback_starts(self):
        self.incident()
        result = self.request("reconcile", confirm=True)
        self.assertEqual(result["status"], "stopped")
        self.assertTrue(result["cleanup_reconciled"])
        self.assertFalse(self.host.running)
        self.assertEqual(c.read_json(self.cleanup_path)["status"], "complete")
        self.assertEqual(self.request("status")["state"]["status"], "stopped")
        self.store.require_cleanup_clear()
        self.assertEqual(self.store._selection()[0].admission.disposition, c.AdmissionDisposition.PENDING)
        self.assertFalse((self.store.runtime / "active-permit").exists())
        self.assertTrue(self.request("reconcile", confirm=True)["unchanged"])
        operation = str(uuid.uuid4())
        review = self.request("release-plan", target=self.previous_version, operation_id=operation,
                              expected_current=self.version)
        self.assertEqual(set(review["running_roles"]), {"http", "relay"})
        result = self.request("rollback", target=self.previous_version, operation_id=operation,
                              expected_current=self.version, plan_token=review["plan_token"], confirm=True)
        self.assertEqual(result["status"], "healthy")
        self.assertEqual(len(self.host.running), 2)

    def test_closed_cleanup_reconcile_then_deploy_starts(self):
        self.incident()
        self.request("reconcile", confirm=True)
        result = self.request("activate", archive=str(self.archive), archive_sha256=c.digest(self.archive))
        self.assertEqual(result["status"], "healthy")
        self.assertEqual(len(self.host.running), 2)

    def test_reconcile_preserves_deliberately_disabled_roles(self):
        self.incident(stopped_role="relay")
        self.request("reconcile", confirm=True)
        self.request("activate", archive=str(self.archive), archive_sha256=c.digest(self.archive))
        self.assertEqual({identity.role for _, _, identity in self.host.running.values()}, {"http"})
        self.assertNotIn(self.inventory.units[self.inventory.identity("relay")], self.host.enabled)

    def test_reconcile_resumes_after_terminal_state_before_cleanup_completion(self):
        self.incident()
        with patch.object(admission.AdmissionStore, "cleanup_status", side_effect=OSError("interrupted")), \
                self.assertRaises(OSError):
            self.request("reconcile", confirm=True)
        self.assertEqual(c.read_json(self.cleanup_path)["status"], "closed")
        self.assertEqual(self.host.load_state(self.entry)["last_attempt"]["error"], "operation_cleanup_reconciled")
        self.request("reconcile", confirm=True)
        self.store.require_cleanup_clear()
        self.assertFalse(self.host.running)

    def test_existing_local_cli_reconcile_reaches_the_narrow_handler(self):
        from test_local import cli
        from test_feature_wiring import manager
        self.incident()
        calls = []
        def request(action, request):
            calls.append(action)
            return manager.execute(self.host, action, {**request, **self.envelope})
        ssh = Mock()
        ssh.request.side_effect = request
        with patch.object(cli, "SSH", return_value=ssh), patch.object(cli, "ROOT", self.host.paths.root), \
                patch.object(cli, "confirm"), patch.object(cli, "say"), patch.object(cli, "display") as display, \
                patch.dict(os.environ, ACTION="reconcile"):
            cli.deploy(self.project, self.profile, {})
        self.assertEqual(calls, ["status", "reconcile", "status"])
        self.assertTrue(display.call_args.args[0]["cleanup_reconciled"])
        self.assertFalse(self.host.running)

    def test_reconcile_refuses_unproven_or_unrelated_cleanup(self):
        operation = self.incident()
        closure = c.read_json(self.root / "cleanup-closure.json")
        cleanup = c.read_json(self.cleanup_path)
        state = self.host.load_state(self.entry)
        for change in ({"outcome": "failed"}, {"error": "feature_transition_pending"}, {"phase": "migration"},
                       {"recovery_mode": "takeover"}, {"maintenance_action": "up"}):
            with self.subTest(attempt_change=change):
                self.host.save_state(self.entry, {**state, "last_attempt": {**state["last_attempt"], **change}})
                try:
                    with self.assertRaises(c.DeployError) as raised:
                        self.request("reconcile", confirm=True)
                    self.assertEqual(raised.exception.code, "operation_reconcile_unavailable")
                    self.assertEqual(c.read_json(self.cleanup_path), cleanup)
                finally:
                    self.host.save_state(self.entry, state)
        transition = c.read_json(self.root / "transition.json")
        cases = (
            (self.cleanup_path, {**cleanup, "status": "armed"}, "operation_cleanup_pending"),
            (self.cleanup_path, {**cleanup, "status": "pending"}, "operation_cleanup_pending"),
            (self.root / "cleanup-closure.json", {**closure, "operation_id": str(uuid.uuid4())}, "operation_cleanup_evidence"),
            (self.root / "cleanup-closure.json", {**closure, "stage": c.ClosureStage.INGRESS_WITHDRAWN.value}, "operation_cleanup_evidence"),
            (self.root / "cleanup-closure.json", {**closure, "backend_exit_sha256": None}, "invalid_contract"),
            (self.root / "cleanup-closure.json", {**closure, "old_reference_isolation_sha256": None}, "invalid_contract"),
            (self.root / "cleanup-closure.json", {**closure, "units": closure["units"][:1], "endpoints": []}, "operation_cleanup_evidence"),
            (self.root / "transition.json", {**transition, "transition": {**transition["transition"],
                "affected_units": transition["transition"]["affected_units"][:1]}}, "operation_cleanup_evidence"),
        )
        for path, value, code in cases:
            with self.subTest(code=code, value=value):
                before = path.read_bytes()
                c.atomic_json(path, value, mode=0o600)
                try:
                    with self.assertRaises(c.DeployError) as raised:
                        self.request("reconcile", confirm=True)
                    self.assertEqual(raised.exception.code, code)
                    self.assertEqual(self.host.load_state(self.entry), state)
                finally:
                    path.write_bytes(before)
        missing = self.root / "cleanup-closure.json"
        missing.unlink()
        with self.assertRaises(c.DeployError) as raised:
            self.request("reconcile", confirm=True)
        self.assertEqual(raised.exception.code, "admission_missing")
        c.atomic_json(missing, closure, mode=0o600)
        # A live workload, pending start/stop job or control process is not exit.
        stopped = services.unit_state
        for change in ({"MainPID": "123", "ActiveState": "active"}, {"ControlPID": "123"}, {"Job": "42"}):
            with self.subTest(change=change), patch.object(services, "unit_state", side_effect=lambda h, u: {**stopped(h, u), **change}):
                with self.assertRaises(c.DeployError) as raised:
                    self.request("reconcile", confirm=True)
                self.assertEqual(raised.exception.code, "feature_stop_unverified")
        with patch("feature_network.close", side_effect=c.DeployError("network_backend_alive", "Still listening")), \
                self.assertRaises(c.DeployError) as raised:
            self.request("reconcile", confirm=True)
        self.assertEqual(raised.exception.code, "network_backend_alive")
        with patch("dns.load", return_value={"operations": {operation: {"results": {"record": "pending"}}}}), \
                self.assertRaises(c.DeployError) as raised:
            self.request("reconcile", confirm=True)
        self.assertEqual(raised.exception.code, "dns_reconciliation")
        self.store.cleanup_status(str(uuid.uuid4()), "f" * 32, "closed")
        with self.assertRaises(c.DeployError) as raised:
            self.request("reconcile", confirm=True)
        self.assertEqual(raised.exception.code, "operation_cleanup_pending")
        self.assertEqual(c.read_json(self.cleanup_path), cleanup)

    def test_reconcile_requires_the_incident_attempt(self):
        self.prepared()
        with self.assertRaises(c.DeployError) as raised:
            self.request("reconcile", confirm=True)
        self.assertEqual(raised.exception.code, "operation_reconcile_unavailable")
