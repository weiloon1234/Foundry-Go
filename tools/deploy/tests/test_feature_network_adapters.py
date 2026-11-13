"""Exercise lane C through the exact checkpoint-2 seam, without host effects."""
from dataclasses import replace
import json
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import Mock, patch

import network_seam_fixture as fixture
from network_seam_fixture import c, BOOT, OPERATION, DIGEST
from host import Host, Paths
import certificates
import edge
import firewall
import listeners


class FeatureAdapterTests(unittest.TestCase):
    def setUp(self):
        self.admission, self.services, self.seam = self.enterContext(fixture.owners())
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.host = Host(Paths(Path(temporary.name)))
        self.host.owned_write(self.host.paths.p("/proc/sys/kernel/random/boot_id"), BOOT)
        self.entry, self.old, self.current, self.transition, self.store = fixture.build(self.host)
        self.instance = self.current.profile.instance_id
        self.registry = {"instances": {self.instance: self.entry}}
        self.host.read_registry = Mock(side_effect=lambda: self.registry)
        self.host.run = Mock(side_effect=self.command)
        self.running = False
        self.manager_override = {}
        self.workers = [(91, 101)]
        self.enterContext(patch.object(edge, "_worker_identities", side_effect=lambda host: tuple(self.workers)))
        self.retain = self.enterContext(patch.object(edge, "retain_global_references", return_value=()))
        self.references = self.enterContext(patch.object(edge, "upstream_references", return_value=()))
        self.enterContext(patch.object(edge, "retained_references", return_value=()))
        self.sockets = self.enterContext(patch.object(listeners, "observe_sockets", return_value=()))
        self.enterContext(patch.object(listeners, "process_members", side_effect=lambda host, unit: frozenset({91}) if unit == "nginx.service" else frozenset({self.pid(unit)})))
        self.fw = firewall.FirewallObservation(self.instance, BOOT, True, DIGEST, (8554,), tuple(cl.socket for cl in listeners.public_claims(self.current.profile)))
        self.enterContext(patch.object(firewall, "withdraw", return_value=self.fw))
        self.enterContext(patch.object(firewall, "observe", side_effect=lambda *a, **kw: replace(self.fw, closed=not kw.get("opened", False))))
        def withdraw(host, instance, roles):
            self.workers = [(self.workers[0][0] + 1, self.workers[0][1] + 1)]
            return edge.EdgeObservation(instance, BOOT, tuple(roles), True, DIGEST, tuple(self.workers))
        self.withdraw = self.enterContext(patch.object(edge, "withdraw", side_effect=withdraw))
        self.enterContext(patch.object(edge, "observe_projection", side_effect=lambda host, instance, roles, **kw:
            edge.EdgeObservation(instance, BOOT, tuple(roles), not kw.get("expected"), DIGEST, tuple(self.workers))))
        self.site = self.host.paths.p("/etc/nginx/sites-available/foundry-" + self.entry["name"] + ".conf")
        self.host.owned_write(self.site, listeners._feature_projection(self.entry, self.old.topology, self.old.profile, self.old.resolution.endpoints)["http"])
        enabled = self.host.paths.p("/etc/nginx/sites-enabled/" + self.site.name)
        enabled.parent.mkdir(parents=True)
        enabled.symlink_to(self.site)

    def pid(self, unit):
        return 100 + list(self.current.units.values()).index(unit)

    def command(self, args, **kwargs):
        self.assertEqual(args[:2], ["systemctl", "show"])
        unit = args[2]
        values = {"Id": unit, "LoadState": "loaded", "ActiveState": "active" if self.running else "inactive",
                  "MainPID": str(self.pid(unit)) if self.running else "0", "ControlPID": "0", "ControlGroup": "", "Job": "",
                  "InvocationID": format(self.pid(unit), "032x")}
        values.update(self.manager_override)
        return subprocess.CompletedProcess(args, 0, "\n".join(k + "=" + v for k, v in values.items()), "")

    def close(self):
        return self.seam.close(self.host, self.entry, self.transition, self.current.resolution, self.current.units)

    def start(self, *, committed=False):
        evidence = self.close()
        self.store.record_boot_closure(evidence)
        def proc(pid, invocation):
            root = self.host.paths.p(f"/proc/{pid}")
            root.mkdir(parents=True, exist_ok=True)
            (root / "stat").write_text(str(pid) + " (fixture) " + " ".join(["S"] + ["0"] * 18 + ["42"]))
            (root / "environ").write_bytes(b"INVOCATION_ID=" + invocation.encode() + b"\0")
            return c.ProcessIdentity(pid, 42, invocation)
        executor = proc(80, "e" * 32)
        permit_id = "00000000-0000-4000-8000-000000000008"
        if committed:
            self.store.record_operation(fixture.attempt("succeeded"))
            self.store.commit()
        else:
            self.store.issue_permit(c.ValidationPermit(permit_id, OPERATION, BOOT, self.current.resolution.id, evidence.id,
                                   executor, tuple(s.identity for s in self.current.specs.values())), evidence)
            self.host.owned_write(self.store.runtime / "active-permit", permit_id, mode=0o600)
        for role, unit in self.current.units.items():
            process = proc(self.pid(unit), format(self.pid(unit), "032x"))
            invocation = c.HelperInvocation(BOOT, self.current.specs[role].identity, process, DIGEST,
                            c.canonical_digest(c.record_document(self.current.specs[role])), self.current.resolution.id, 1, None)
            self.store.admit(invocation, permit_id=None if committed else permit_id)
        self.running = True
        private = tuple(listeners.ObservedSocket(cl.socket, (self.pid(self.current.units[cl.role]),))
                        for cl in listeners.private_claims(self.instance, self.current.resolution))
        public = tuple(listeners.ObservedSocket(cl.socket, (91,)) for cl in listeners.public_claims(self.current.profile))
        # Unrelated host HTTP and other app sockets must not be mistaken for this instance's listeners.
        self.sockets.return_value = (*private, *public, listeners.ObservedSocket(listeners.SocketIdentity("tcp", 4, "0.0.0.0", 80), (91,)))
        self.enterContext(patch.object(self.services, "wait_ready", return_value=True))
        self.enterContext(patch.object(certificates, "ensure_hostnames"))
        self.enterContext(patch.object(edge, "open_runtime", side_effect=lambda *a, **kw: (kw["authorize"](), kw["authorize"]())))
        self.enterContext(patch.object(firewall, "open_runtime", side_effect=lambda *a, **kw: (kw["authorize"](), kw["authorize"]())))
        return evidence

    def test_allocate_is_read_only_and_reserves_every_history_and_live_reference(self):
        self.references.return_value = (listeners.SocketIdentity("tcp", 4, "127.0.0.1", 18004),)
        before = {p: p.read_bytes() for p in self.host.paths.root.rglob("*") if p.is_file()}
        endpoints, projection = self.seam.allocate(self.host, self.entry, self.current.topology, self.current.profile, OPERATION)
        self.assertEqual([e.port for e in endpoints], [18005, 18006])
        self.assertEqual(projection, c.canonical_digest(listeners._feature_projection(self.entry, self.current.topology, self.current.profile, endpoints)))
        self.assertEqual(before, {p: p.read_bytes() for p in self.host.paths.root.rglob("*") if p.is_file()})
        self.retain.assert_not_called()
        self.host.run.assert_not_called()

    def test_lane_a_close_returns_complete_exact_transition_evidence(self):
        evidence = self.close()
        self.assertIs(evidence.stage, c.ClosureStage.APPLICATION_CLOSED)
        self.assertEqual(set(evidence.units), set(self.transition.affected_units))
        self.assertEqual(set(evidence.endpoints), set((*self.old.resolution.endpoints, *self.current.resolution.endpoints)))
        self.assertEqual(evidence.operation_id, OPERATION)
        self.assertEqual(evidence.old_resolution_id, self.old.resolution.id)
        self.assertIsNotNone(evidence.backend_exit_sha256)
        self.assertIsNotNone(evidence.old_reference_isolation_sha256)
        self.assertNotIn("proxy_pass", self.site.read_text())

    def test_wrong_scope_plan_or_unit_map_refuses(self):
        for request in (replace(self.transition, plan_sha256=DIGEST), replace(self.transition, affected_units=self.transition.affected_units[:1])):
            with self.assertRaises(c.DeployError):
                self.seam.close(self.host, self.entry, request, self.current.resolution, self.current.units)
        with self.assertRaises(c.DeployError):
            self.seam.close(self.host, self.entry, self.transition, self.current.resolution, {"http": "foreign.service"})
        self.withdraw.assert_not_called()

    def test_jobs_control_processes_and_cgroup_descendants_refuse_closure(self):
        for override in ({"Job": "17"}, {"ControlPID": "8"}, {"MainPID": "9"}, {"LoadState": "not-found"}, {"ActiveState": "activating"}):
            self.manager_override = override
            with self.subTest(override=override), self.assertRaises(c.DeployError):
                self.close()
        self.manager_override = {"ControlGroup": "/system.slice/fixture.service"}
        path = self.host.paths.p("/sys/fs/cgroup/system.slice/fixture.service/child/cgroup.procs")
        path.parent.mkdir(parents=True)
        path.write_text("123\n")
        with self.assertRaises(c.DeployError):
            self.close()

    def test_old_references_or_listener_reuse_cannot_produce_complete_closure(self):
        endpoint = self.current.resolution.endpoints[-1]
        socket = listeners.SocketIdentity("tcp", 4, endpoint.address, endpoint.port)
        self.references.return_value = (socket,)
        with self.assertRaises(c.DeployError) as raised:
            self.close()
        self.assertEqual(raised.exception.code, "old_backend_reference")
        self.references.return_value = ()
        self.sockets.return_value = (listeners.ObservedSocket(socket, (99,)),)
        with self.assertRaises(c.DeployError):
            self.close()

    def test_undrained_old_workers_refuse_closure(self):
        self.withdraw.side_effect = lambda *args: None
        with patch("time.monotonic", side_effect=(0, 11)), self.assertRaises(c.DeployError) as raised:
            self.close()
        self.assertEqual(raised.exception.code, "network_old_workers")

    def test_same_resolution_restart_requires_fresh_drain_and_exit_proof(self):
        self.transition = replace(self.transition, old_resolution_id=self.current.resolution.id)
        evidence = self.close()
        self.assertEqual(evidence.old_resolution_id, evidence.candidate_resolution_id)
        self.assertIs(evidence.stage, c.ClosureStage.APPLICATION_CLOSED)

    def test_foreign_public_claim_or_unstarted_legacy_http_reservation_is_not_reused(self):
        self.registry["instances"]["00000000-0000-4000-8000-000000000099"] = {
            "name": "legacy-production", "profile": {"schema": 2, "instance_id": "00000000-0000-4000-8000-000000000099"},
            "allocations": {"http_port": 18004}}
        endpoints, _ = self.seam.allocate(self.host, self.entry, self.current.topology, self.current.profile, OPERATION)
        self.assertEqual([e.port for e in endpoints], [18005, 18006])
        self.sockets.return_value = (listeners.ObservedSocket(listeners.public_claims(self.current.profile)[0].socket, (999,)),)
        with self.assertRaises(c.DeployError):
            self.seam.allocate(self.host, self.entry, self.current.topology, self.current.profile, OPERATION)

    def test_pending_live_permit_opens_only_selected_observed_sockets(self):
        evidence = self.start()
        with patch.object(certificates, "verify_network_postcondition") as tls:
            self.assertIs(listeners.feature_open(self.host, self.entry, self.current.resolution, evidence), True)
            self.seam.open_public(self.host, self.entry, self.current.resolution, evidence)
            tls.assert_not_called()

    def test_tls_postcondition_is_required_before_public_firewall_opening(self):
        self.entry, self.old, self.current, self.transition, self.store = fixture.build(
            self.host, mode="edge", instance_id=self.instance, ports=(18010, 18012))
        self.registry["instances"][self.instance] = self.entry
        self.host.owned_write(self.site, listeners._feature_projection(self.entry, self.old.topology, self.old.profile, self.old.resolution.endpoints)["http"])
        evidence = self.start()
        with patch.object(certificates, "verify_network_postcondition") as tls:
            self.seam.open_public(self.host, self.entry, self.current.resolution, evidence)
            tls.assert_called_once_with(self.host, self.entry)
        with patch.object(certificates, "verify_network_postcondition", side_effect=c.DeployError("certificate_effective_renewal", "failed")), \
                patch.object(firewall, "open_runtime") as opening:
            self.withdraw.reset_mock()
            with self.assertRaises(c.DeployError):
                self.seam.open_public(self.host, self.entry, self.current.resolution, evidence)
            opening.assert_not_called()
            self.withdraw.assert_called_once()

    def test_committed_boot_selection_opens_without_validation_permit(self):
        evidence = self.start(committed=True)
        self.assertIs(listeners.feature_open(self.host, self.entry, self.current.resolution, evidence), True)

    def test_dead_operation_owner_refuses_and_withdraws(self):
        evidence = self.start()
        self.host.paths.p("/proc/80/environ").write_bytes(b"INVOCATION_ID=wrong\0")
        self.withdraw.reset_mock()
        with self.assertRaises(c.DeployError):
            self.seam.open_public(self.host, self.entry, self.current.resolution, evidence)
        self.withdraw.assert_called_once()

    def test_live_admitted_helper_blocks_exit_even_if_manager_reports_inactive(self):
        self.start()
        self.running = False
        with self.assertRaises(c.DeployError) as raised:
            self.close()
        self.assertEqual(raised.exception.code, "network_backend_alive")

    def test_failure_during_firewall_open_withdraws_both_owners(self):
        evidence = self.start()
        with patch.object(firewall, "open_runtime", side_effect=c.DeployError("injected", "open failed")), \
                patch.object(firewall, "withdraw") as fw:
            self.withdraw.reset_mock()
            with self.assertRaises(c.DeployError):
                self.seam.open_public(self.host, self.entry, self.current.resolution, evidence)
            fw.assert_called_once()
            self.withdraw.assert_called_once()

    def test_changed_projection_or_foreign_socket_owner_refuses_opening(self):
        evidence = self.start()
        self.sockets.return_value = tuple(replace(s, pids=(999,)) if s.socket.port == 8554 else s for s in self.sockets.return_value)
        with self.assertRaises(c.DeployError):
            self.seam.open_public(self.host, self.entry, self.current.resolution, evidence)
        changed = replace(self.current.resolution, nginx_sha256=DIGEST)
        with self.assertRaises(c.DeployError):
            self.seam.open_public(self.host, self.entry, changed, evidence)


if __name__ == "__main__":
    unittest.main()
