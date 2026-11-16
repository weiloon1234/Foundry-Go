"""Private copy allocation and retained endpoint stability without host effects."""
from dataclasses import replace
import os
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import Mock, patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "lib"))
import contracts as c
import edge
import listeners
import report
import topology as t
from host import Host, Paths
from test_host_capabilities import INSTANCE, OPERATION
from test_services import inventory_fixture


class NetworkClientTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.host = Host(Paths(Path(temporary.name).resolve()))
        self.installed = {}
        original_stat = Path.stat

        def root_owned(path, *args, **kwargs):
            info = original_stat(path, *args, **kwargs)
            if path in self.installed:
                fields = list(info)
                fields[4] = self.installed[path]
                return os.stat_result(fields)
            return info
        self.enterContext(patch.object(Path, "stat", root_owned))

    def process(self, executable, group, *, pid=42, installed=True):
        path = self.host.paths.p(executable)
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text("fixture executable")
        path.chmod(0o755)
        if installed:
            self.installed[path] = 0
        proc = self.host.paths.p(f"/proc/{pid}")
        proc.mkdir(parents=True, exist_ok=True)
        (proc / "exe").symlink_to(path)
        (proc / "stat").write_text(f"{pid} (systemd-networkd) " + " ".join(["S"] + ["0"] * 18 + ["42"]))
        (proc / "cgroup").write_text("0::" + group + "\n")
        return path

    def findings(self, *, transport="udp", family=4, port=68, pids=(42,)):
        address = "10.0.2.15" if family == 4 else "fe80::1234"
        observed = listeners.ObservedSocket(listeners.SocketIdentity(transport, family, address, port), pids)
        return listeners.ownership_findings((), (observed,), {}, host=self.host)

    def test_dhcp_clients_require_installed_executable_and_service_ownership(self):
        for executable, groups in listeners.NETWORK_CLIENTS.items():
            for group in groups:
                with self.subTest(executable=executable, group=group):
                    link = self.host.paths.p("/proc/42/exe")
                    link.unlink(missing_ok=True)
                    self.process(executable, group)
                    self.assertEqual(self.findings(), [])
                    self.assertEqual(self.findings(family=6, port=546), [])
                    for kwargs in ({"transport": "tcp"}, {"port": 67}, {"family": 6, "port": 68},
                                   {"port": 546}, {"family": 6, "port": 547}, {"pids": ()}, {"pids": (42, 99)}):
                        self.assertEqual(self.findings(**kwargs)[0]["code"], "unexpected_public_listener")
                    self.host.paths.p("/proc/42/cgroup").write_text("0::/user.slice/session.scope\n")
                    self.assertTrue(self.findings(), "A manually launched namesake must not count as an OS client")

    def test_unknown_or_unprotected_dhcp_owner_is_reported_with_safe_details(self):
        executable = self.process("/tmp/private/systemd-networkd", "/system.slice/systemd-networkd.service", installed=False)
        finding = self.findings()[0]
        self.assertEqual(finding, {"code": "unexpected_public_listener", "transport": "udp", "family": 4,
                                  "address": "10.0.2.15", "port": 68, "processes": ["systemd-networkd"]})
        self.assertNotIn(str(executable.parent), str(finding))
        self.host.paths.p("/proc/42/exe").unlink()
        executable = self.process("/usr/lib/systemd/systemd-networkd", "/system.slice/systemd-networkd.service")
        executable.chmod(0o777)
        self.assertTrue(self.findings())
        executable.chmod(0o755)
        self.host.paths.p("/proc/42/stat").unlink()
        self.assertEqual(self.findings()[0]["processes"], ["unknown"])

    def test_dhcp_rejects_nonroot_or_mismatched_executable_inode(self):
        import release
        executable = self.process("/usr/lib/systemd/systemd-networkd", "/system.slice/systemd-networkd.service")
        self.installed[executable] = 1000
        self.assertTrue(self.findings())
        self.installed[executable] = 0
        process = release._process(self.host, "42")
        with patch.object(release, "_process", return_value={**process, "inode": process["inode"] + 1}):
            self.assertTrue(self.findings())


class CopyListenerTests(unittest.TestCase):
    def test_grow_shrink_and_regrow_preserve_survivors_and_history_reservations(self):
        inventory, entry, _ = inventory_fixture()
        placed = replace(inventory.profile, placement=(("http", 1), ("relay", 3), ("worker", 1)))
        registry = {"instances": {INSTANCE: entry}, "shared": {}}
        with tempfile.TemporaryDirectory() as root:
            host = Host(Paths(Path(root)))
            with patch.object(listeners, "_feature_history", return_value=(registry, ((entry, inventory),))) as history, \
                    patch.object(report, "claim_inventory", return_value=()), \
                    patch.object(listeners, "observe_sockets", return_value=()), \
                    patch.object(edge, "upstream_references", return_value=()), \
                    patch.object(edge, "retained_references", return_value=()):
                grown, _ = listeners.feature_allocate(host, entry, inventory.topology, placed, OPERATION,
                                                       retained=inventory.resolution.endpoints)
                self.assertEqual(grown[:2], inventory.resolution.endpoints)
                self.assertEqual([(e.ordinal, e.port) for e in grown[2:]], [(2, 18000), (3, 18001)])
                inventory.resolution = replace(inventory.resolution, endpoints=grown)
                history.return_value = (registry, ((entry, inventory),))
                smaller = replace(placed, placement=(("http", 1), ("relay", 2), ("worker", 1)))
                retained = tuple(e for e in grown if e.ordinal <= 2)
                shrunk, _ = listeners.feature_allocate(host, entry, inventory.topology, smaller, OPERATION, retained=retained)
                self.assertEqual(shrunk, retained)
                regrown, _ = listeners.feature_allocate(host, entry, inventory.topology, placed, OPERATION, retained=shrunk)
                self.assertEqual(regrown[:-1], shrunk)
                self.assertEqual((regrown[-1].ordinal, regrown[-1].port), (3, 18002))
                with self.assertRaises(c.DeployError):
                    listeners.feature_allocate(host, entry, inventory.topology, smaller, OPERATION, retained=grown)

    def test_every_copy_reserves_wildcards_across_address_families(self):
        inventory, _, _ = inventory_fixture()
        raw = inventory.topology.projection()
        raw["roles"][1]["network"] = {"confinement": "loopback"}
        raw["roles"][1]["listeners"]["control"]["wildcard"] = True
        topology = t.parse_topology(raw)
        identities = tuple(c.UnitIdentity(INSTANCE, "relay", n) for n in (1, 2, 3))
        endpoints = listeners.allocate_private(INSTANCE, tuple((i, "control") for i in identities), (), (),
            first=18000, last=18002, wildcards=listeners.wildcard_listeners(topology))
        claims = listeners.private_claims(INSTANCE, replace(inventory.resolution, endpoints=endpoints), topology)
        self.assertEqual(tuple(claim.identity for claim in claims), identities)
        for claim in claims:
            self.assertTrue(claim.wildcard)
            for family, address in ((4, "203.0.113.10"), (6, "2001:db8::10")):
                other = replace(claim, wildcard=False, socket=listeners.SocketIdentity("tcp", family, address, claim.socket.port))
                with self.assertRaises(c.DeployError):
                    listeners.require_available((other,), claims, ())

    def test_unix_socket_names_stay_stable_when_count_changes(self):
        inventory, _, _ = inventory_fixture()
        raw = inventory.topology.projection()
        raw["roles"][1].update(listeners={"control": {"transport": "unix", "socket": "relay.sock"}},
                                readiness={"kind": "unix", "listener": "control"}, configs={}, arguments=[])
        topology = t.parse_topology(raw)
        def sockets(count):
            placed = replace(inventory.profile, placement=(("http", 1), ("relay", count), ("worker", 1)))
            return listeners.unix_endpoints(topology, placed)
        self.assertEqual(sockets(3)[:2], sockets(2))
        self.assertEqual(sockets(2)[:1], sockets(1))
        self.assertEqual([e.name for e in sockets(3)], ["relay.sock", "relay.sock@2", "relay.sock@3"])
