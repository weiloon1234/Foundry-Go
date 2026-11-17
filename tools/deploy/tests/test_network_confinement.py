"""Loopback confinement declarations, sticky floors and host-wide reservations."""
from dataclasses import replace
from pathlib import Path
import subprocess
import sys
import tempfile
import time
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "lib"))
import contracts as c
import listeners
import network_confinement
import report
import services
import topology as t
from host import Paths
from test_host_capabilities import declaration, INSTANCE
from test_network import runtime
from test_services import inventory_fixture


def confined_topology():
    raw = declaration()
    raw["roles"][1]["listeners"].pop("ingress")
    raw["roles"][1]["network"] = {"confinement": "loopback"}
    raw["roles"][1]["listeners"]["control"]["wildcard"] = True
    return raw


class ConfinementTests(unittest.TestCase):
    def test_declaration_requires_private_tcp_and_explicit_confinement(self):
        raw = confined_topology()
        parsed = t.parse_topology(raw)
        self.assertEqual(parsed.roles[1].common.confinement, "loopback")
        self.assertTrue(dict(parsed.roles[1].listeners)["control"].wildcard)
        for change in (
                lambda r: r.pop("network"),
                lambda r: r.update(network={"confinement": "none"}),
                lambda r: r.update(network={"confinement": "loopback", "extra": True}),
                lambda r: r["listeners"]["control"].update(wildcard="true"),
                lambda r: r["listeners"]["control"].update(bind="public", tls="none", authentication=True),
                lambda r: r["listeners"]["control"].update(transport="unix", socket="control.sock")):
            raw = confined_topology()
            change(raw["roles"][1])
            with self.assertRaises(c.DeployError):
                t.parse_topology(raw)
        raw = declaration()
        raw["roles"][0]["network"] = {"confinement": "loopback"}
        with self.assertRaises(c.DeployError):
            t.parse_topology(raw)
        raw = confined_topology()
        raw["roles"][1]["listeners"]["control"].pop("wildcard")
        t.parse_topology(raw)


    def test_wildcard_reserves_every_address_and_both_families(self):
        topology = t.parse_topology(confined_topology())
        resolution = replace(runtime(), endpoints=(c.AllocatedEndpoint("relay", "control", "127.0.0.1", 18000, 1),))
        claim, = listeners.private_claims(INSTANCE, resolution, topology)
        for family, address in ((4, "127.0.0.2"), (4, "203.0.113.1"), (6, "::1"), (6, "2001:db8::1")):
            other = replace(claim, instance_id="another", wildcard=False,
                            socket=listeners.SocketIdentity("tcp", family, address, 18000))
            for requested, reserved in ((claim, other), (other, claim)):
                with self.assertRaises(c.DeployError):
                    listeners.require_available((requested,), (reserved,), ())
            allocated = listeners.allocate_private(INSTANCE, ((c.UnitIdentity(INSTANCE, "relay", 1), "control"),), (other,), (),
                first=18000, last=18001, wildcards=listeners.wildcard_listeners(topology))
            self.assertEqual(allocated[0].port, 18001)
        self.assertEqual(listeners.allocate_private(INSTANCE, ((c.UnitIdentity(INSTANCE, "http", 1), "http"),), (claim,), (),
                         first=18000, last=18001)[0].port, 18001)

    def test_wildcard_ownership_does_not_accept_foreign_pids_ports_or_addresses(self):
        topology = t.parse_topology(confined_topology())
        resolution = replace(runtime(), endpoints=(c.AllocatedEndpoint("relay", "control", "127.0.0.1", 18000, 1),))
        claim, = listeners.private_claims(INSTANCE, resolution, topology)
        owners = {c.UnitIdentity(INSTANCE, "relay", 1): {10}}
        for family, address in ((4, "0.0.0.0"), (6, "::"), (4, "127.0.0.1")):
            observed = listeners.ObservedSocket(listeners.SocketIdentity("tcp", family, address, 18000), (10,))
            self.assertEqual(listeners.ownership_findings((claim,), (observed,), owners), [])
            for bad in (replace(observed, pids=()), replace(observed, pids=(10, 11)),
                        replace(observed, socket=replace(observed.socket, port=18001))):
                self.assertTrue(listeners.ownership_findings((claim,), (bad,), owners))
        self.assertFalse(listeners.matches(claim, listeners.SocketIdentity("tcp", 4, "203.0.113.1", 18000)))

    def test_builtin_and_custom_unit_filters_reuse_shared_policy(self):
        inventory, entry, _ = inventory_fixture()
        for name in ("relay", "worker"):
            role = inventory.roles[name]
            inventory.roles[name] = replace(role, common=replace(role.common, confinement="loopback"))
            text = services.render_unit(inventory, inventory.identity(name), entry, "a" * 64)
            self.assertEqual(text.count(network_confinement.FILTER), 1)
            self.assertIn("AF_UNIX", text)

    def test_private_socket_owner_is_the_exact_copy(self):
        first = c.UnitIdentity(INSTANCE, "relay", 1)
        second = c.UnitIdentity(INSTANCE, "relay", 2)
        claim = listeners.ListenerClaim(INSTANCE, "relay", "control", 2,
            listeners.SocketIdentity("tcp", 4, "127.0.0.1", 18001), "application")
        owners = {first: {10}, second: {11}}
        self.assertEqual(listeners.ownership_findings((claim,),
            (listeners.ObservedSocket(claim.socket, (11,)),), owners), [])
        findings = listeners.ownership_findings((claim,),
            (listeners.ObservedSocket(claim.socket, (10,)),), owners)
        self.assertEqual([finding["code"] for finding in findings], ["listener_owner_unverified"])

    def test_bpf_proof_is_required_for_each_numbered_copy_cgroup(self):
        from types import SimpleNamespace
        from unittest.mock import Mock
        identities = tuple(c.UnitIdentity(INSTANCE, "relay", n) for n in (1, 2, 3))
        inventory = SimpleNamespace(units={i: f"foundry-app-fixture-relay@{i.ordinal}.service" for i in identities})
        host = Mock()
        for identity in identities:
            unit = inventory.units[identity]
            group = "/system.slice/" + unit
            host.run.return_value = subprocess.CompletedProcess([], 0,
                "IPAddressDeny=0.0.0.0/0 ::/0\nIPAddressAllow=127.0.0.0/8 ::1/128\n"
                "DropInPaths=\nControlGroup=" + group, "")
            with patch.object(network_confinement, "attached_programs", return_value=[[1], [2]]) as attached:
                self.assertEqual(network_confinement.isolation(host, inventory, identity)["programs"], [[1], [2]])
                attached.assert_called_once_with(host, group)
                self.assertEqual(host.run.call_args.args[0][2], unit)
            if identity.ordinal > 1:
                host.run.return_value.stdout = host.run.return_value.stdout.replace(group,
                    "/system.slice/" + inventory.units[identities[0]])
                with patch.object(network_confinement, "attached_programs") as attached, self.assertRaises(c.DeployError):
                    network_confinement.isolation(host, inventory, identity)
                attached.assert_not_called()

    def test_readiness_accepts_only_declared_wildcards_with_fresh_filter_proof(self):
        import tempfile
        import release
        from host import Host, Paths
        with tempfile.TemporaryDirectory(dir=Path(tempfile.gettempdir()).resolve()) as root:
            host = Host(Paths(root))
            inventory, _, _ = inventory_fixture()
            spec = inventory.specs[inventory.identity("relay")]
            executable = host.paths.p(spec.executable)
            executable.parent.mkdir(parents=True)
            executable.write_bytes(b"native")
            executable.chmod(0o755)
            inventory.specs[inventory.identity("relay")] = replace(spec, executable_sha256=c.digest(executable))
            original = inventory.roles["relay"]
            confined = replace(original, common=replace(original.common, confinement="loopback"),
                               listeners=tuple((name, replace(listener, wildcard=True)) for name, listener in original.listeners))
            observed = {"ActiveState": "active", "NRestarts": "0", "MainPID": "20", "process": {"start": "42"}}
            with patch.object(release, "service", return_value=observed), \
                    patch.object(release, "_matches_executable", return_value=True), \
                    patch.object(release, "STABILITY_SECONDS", 0), \
                    patch.object(host, "run") as sockets, patch.object(services, "probe", return_value=True), \
                    patch.object(network_confinement, "isolation", return_value={"programs": [[1], [2]]}) as proof:
                for kind in ("tcp", "http"):
                    inventory.roles["relay"] = replace(confined, readiness=replace(confined.readiness, kind=kind))
                    for address, port, pid, expected in (("0.0.0.0", 8201, 20, True), ("[::]", 8201, 20, True),
                            ("*", 8201, 20, True), ("127.0.0.1", 8201, 20, True),
                            ("0.0.0.0", 8202, 20, False), ("0.0.0.0", 8201, 21, False),
                            ("203.0.113.1", 8201, 20, False)):
                        sockets.return_value = subprocess.CompletedProcess([], 0,
                            f'LISTEN 0 128 {address}:{port} 0.0.0.0:* users:(("app",pid={pid},fd=3))', '')
                        self.assertEqual(services.wait_ready(host, inventory, inventory.identity("relay"), time.monotonic() + .03), expected)
                sockets.return_value = subprocess.CompletedProcess([], 0,
                    'LISTEN 0 128 *:8201 0.0.0.0:* users:(("app",pid=20,fd=3))', '')
                proof.side_effect = c.DeployError("network_confinement", "No BPF")
                with self.assertRaises(c.DeployError):
                    services.wait_ready(host, inventory, inventory.identity("relay"), time.monotonic() + .1)
                inventory.roles["relay"] = original
                self.assertFalse(services.wait_ready(host, inventory, inventory.identity("relay"), time.monotonic() + .03))

    def test_reports_require_live_confinement_and_owned_pid_before_hiding_wildcard(self):
        from types import SimpleNamespace
        from unittest.mock import Mock
        import release
        topology = t.parse_topology(confined_topology())
        resolution = replace(runtime(), endpoints=(c.AllocatedEndpoint("relay", "control", "127.0.0.1", 18000, 1),))
        inventory = SimpleNamespace(topology=topology, resolution=resolution, units={c.UnitIdentity(INSTANCE, "relay", 1): "relay.service"},
            copies=lambda role: (c.UnitIdentity(INSTANCE, role, 1),),
            identity=lambda role, ordinal=1: c.UnitIdentity(INSTANCE, role, ordinal))
        entry = {"topology": t.validate_topology(topology.projection()),
                 "deployment_selection": {"selected_resolution_id": resolution.id}}
        registry = {"instances": {INSTANCE: entry}}
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        host = Mock(paths=Paths(Path(temporary.name)))
        observed = listeners.ObservedSocket(listeners.SocketIdentity("tcp", 4, "0.0.0.0", 18000), (10,))
        def command(args, **kwargs):
            output = ('LISTEN 0 128 0.0.0.0:18000 0.0.0.0:* users:(("app",pid=10,fd=3))'
                      if args == ["ss", "-H", "-ltnp", "-4"] else "")
            return subprocess.CompletedProcess(args, 0, output, "")
        host.run.side_effect = command
        with patch.object(listeners, "_feature_inventory", return_value=inventory), \
                patch.object(listeners, "observe_sockets", return_value=(observed,)), \
                patch.object(listeners, "process_members", return_value={10}), \
                patch.object(release, "service", return_value={"ActiveState": "active"}), \
                patch.object(network_confinement, "isolation", return_value={"programs": [[1], [2]]}) as proof:
            verified = report.confined_network(host, registry)
            self.assertEqual(verified["roles"][0]["state"], "verified")
            self.assertEqual(verified["sockets"], (observed,))
            self.assertEqual(report.network(host, [], time.monotonic() + 1, confined=verified["sockets"])["public_listeners"], [])
            with patch.object(listeners, "process_members", return_value={11}):
                self.assertEqual(report.confined_network(host, registry)["sockets"], ())
            proof.side_effect = c.DeployError("network_confinement", "No BPF")
            failed = report.confined_network(host, registry)
            self.assertEqual(failed["roles"][0]["state"], "unverified")
            self.assertEqual(failed["sockets"], ())
            problems = []
            self.assertEqual(report.network(host, problems, time.monotonic() + 1, confined=failed["sockets"])["public_listeners"], [18000])
            self.assertIn("unexpected_public_listener", [p["code"] for p in problems])
            self.assertNotIn("network_unavailable", [p["code"] for p in problems])

    def test_stopped_historical_wildcards_remain_reserved_for_other_allocation_owners(self):
        from types import SimpleNamespace
        topology = t.parse_topology(confined_topology())
        resolution = replace(runtime(), endpoints=(c.AllocatedEndpoint("relay", "control", "127.0.0.1", c.POSTGRES_PORT, 1),))
        inventory = SimpleNamespace(topology=topology, resolution=resolution, profile=SimpleNamespace(instance_id=INSTANCE))
        with patch.object(listeners, "_feature_history", return_value=({}, (({}, inventory),))):
            self.assertEqual(listeners.reserved_wildcard_ports(None, {}), {c.POSTGRES_PORT})
        observed = listeners.ObservedSocket(listeners.SocketIdentity("tcp", 6, "2001:db8::1", 18000), (10,))
        allocated = listeners.allocate_private(INSTANCE, ((c.UnitIdentity(INSTANCE, "relay", 1), "control"),), (), (observed,),
                    first=18000, last=18001, wildcards=listeners.wildcard_listeners(topology))
        self.assertEqual(allocated[0].port, 18001)
    def test_public_plan_refuses_a_stopped_wildcard_from_another_instance(self):
        from types import SimpleNamespace
        from unittest.mock import Mock
        from test_network import network_profile, network_topology
        from test_host_capabilities import OPERATION, OLD
        profile = network_profile()
        topology = t.parse_topology(confined_topology())
        historical = SimpleNamespace(topology=topology, profile=SimpleNamespace(instance_id=OLD, listeners=()),
            resolution=replace(runtime(), endpoints=(c.AllocatedEndpoint("relay", "control", "127.0.0.1", 9443, 1),)))
        entry = {"profile": {"instance_id": INSTANCE}, "allocations": {"http_port": 8100}}
        registry = {"instances": {INSTANCE: entry}, "shared": {}}
        with patch.object(listeners, "_feature_history", return_value=(registry, (({}, historical),))), \
                patch.object(report, "claim_inventory", return_value=()), \
                patch.object(listeners, "observe_sockets", return_value=()):
            with self.assertRaises(c.DeployError) as error:
                listeners.feature_allocate(Mock(), entry, network_topology(), profile, OPERATION)
        self.assertEqual(error.exception.code, "listener_conflict")

    def test_closure_rejects_old_upstream_on_another_address_at_wildcard_port(self):
        from test_host_capabilities import closure
        topology = t.parse_topology(confined_topology())
        candidate = replace(runtime(), endpoints=(c.AllocatedEndpoint("relay", "control", "127.0.0.1", 18000, 1),))
        evidence = replace(c.ClosureEvidence.parse(closure()), stage=c.ClosureStage.INGRESS_WITHDRAWN,
                           units=(c.UnitIdentity(INSTANCE, "relay", 1),),
                           endpoints=candidate.endpoints)
        with self.assertRaises(c.DeployError) as error:
            listeners.complete_closure(evidence, candidate, backend_exit_sha256="a" * 64,
                reserved=candidate.endpoints, wildcards=listeners.wildcard_listeners(topology),
                old_references=(listeners.SocketIdentity("tcp", 6, "::1", 18000),))
        self.assertEqual(error.exception.code, "old_backend_reference")
    def test_rehearsal_rejects_any_upstream_that_can_reach_the_wildcard(self):
        import tempfile
        from types import SimpleNamespace
        from host import Host, Paths
        import edge
        import recovery_rehearsal
        import release
        topology = t.parse_topology(confined_topology())
        endpoint = c.AllocatedEndpoint("relay", "control", "127.0.0.1", 18000, 1)
        inventory = SimpleNamespace(topology=topology, resolution=replace(runtime(), endpoints=(endpoint,)),
            profile=SimpleNamespace(instance_id=INSTANCE), roles={"relay": topology.roles[1]}, units={c.UnitIdentity(INSTANCE, "relay", 1): "relay.service"},
            copies=lambda role: (c.UnitIdentity(INSTANCE, role, 1),),
            identity=lambda role, ordinal=1: c.UnitIdentity(INSTANCE, role, ordinal))
        observed = listeners.ObservedSocket(listeners.SocketIdentity("tcp", 4, "0.0.0.0", 18000), (10,))
        entry = {"name": "fixture", "profile": {"instance_id": INSTANCE}}
        with tempfile.TemporaryDirectory() as root, patch.object(edge, "observe_projection"), \
                patch.object(edge, "upstream_references", return_value=()) as references, \
                patch.object(listeners, "observe_sockets", return_value=(observed,)), \
                patch.object(listeners, "process_members", return_value={10}), \
                patch.object(release, "service", return_value={"ActiveState": "active"}), \
                patch.object(network_confinement, "isolation", return_value={"programs": [[1], [2]]}) as proof:
            host = Host(Paths(Path(root).resolve()))
            self.assertTrue(recovery_rehearsal.observe(host, entry, inventory))
            proof.assert_called_once()
            for address in ("127.0.0.2", "::1"):
                references.return_value = (listeners.SocketIdentity("tcp", 6 if ":" in address else 4, address, 18000),)
                with self.assertRaises(c.DeployError) as error:
                    recovery_rehearsal.observe(host, entry, inventory)
                self.assertEqual(error.exception.code, "rehearsal_exposure")
