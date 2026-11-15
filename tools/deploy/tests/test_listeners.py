"""Private copy allocation and retained endpoint stability without host effects."""
from dataclasses import replace
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
