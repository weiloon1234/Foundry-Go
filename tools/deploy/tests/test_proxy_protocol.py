"""Outbound stream PROXY policy, generation admission and unchanged HTTP delivery."""
import copy
from dataclasses import FrozenInstanceError, replace
import json
from types import SimpleNamespace
import unittest
import uuid
from unittest.mock import Mock, patch

from test_host_capabilities import declaration, profile
from test_network import network_profile, runtime
import contracts as c
import edge
import firewall
import listeners
import recover
import report
import topology as t
import upgrades
import test_feature_wiring as wiring


def proxy_topology(mode="none"):
    raw = declaration()
    raw["roles"][1]["listeners"]["ingress"].update(tls=mode, proxy_protocol="v1")
    return raw


class ProxyProtocolTests(unittest.TestCase):
    def test_typed_opt_in_roundtrips_both_tls_modes(self):
        for mode in ("none", "edge"):
            raw = proxy_topology(mode)
            parsed = t.parse_topology(raw)
            listener = dict(parsed.roles[1].listeners)["ingress"]
            self.assertIs(listener.proxy_protocol, t.ProxyProtocol.V1)
            self.assertEqual(parsed.projection(), raw)
            with self.assertRaises(FrozenInstanceError):
                listener.proxy_protocol = None
            raw["roles"][1]["listeners"]["ingress"]["proxy_protocol"] = "v2"
            self.assertEqual(parsed.projection()["roles"][1]["listeners"]["ingress"]["proxy_protocol"], "v1")
        self.assertIsNone(dict(t.parse_topology(declaration()).roles[1].listeners)["ingress"].proxy_protocol)

    def test_rejects_unknown_versions_and_nonpublic_transports(self):
        for value in ("v2", "on", "off", "", None, True, 1, [], {}):
            raw = proxy_topology()
            raw["roles"][1]["listeners"]["ingress"]["proxy_protocol"] = value
            with self.subTest(value=value), self.assertRaisesRegex(c.DeployError, "must be v1"):
                t.parse_topology(raw)
        for listener in ({"transport": "tcp", "bind": "loopback"},
                         {"transport": "unix", "socket": "control"},
                         {"transport": "udp", "bind": "public"}):
            with self.subTest(listener=listener), self.assertRaisesRegex(c.DeployError, "only supported on public TCP"):
                t.Listener.parse({**listener, "proxy_protocol": "v1"})

    def test_rejects_role_http_and_profile_policy(self):
        for index in (0, 1):
            raw = declaration()
            raw["roles"][index]["proxy_protocol"] = "v1"
            with self.assertRaisesRegex(c.DeployError, "not on a role"):
                t.parse_topology(raw)
        placed = profile()
        placed["listeners"]["relay.ingress"]["proxy_protocol"] = "v1"
        with self.assertRaisesRegex(c.DeployError, "not in profile"):
            t.parse_profile(placed, t.parse_topology(declaration()))

    def test_tcp_and_http_readiness_require_separate_listener(self):
        for kind in ("tcp", "http"):
            raw = proxy_topology()
            raw["roles"][1]["readiness"] = {"kind": kind, "listener": "ingress",
                **({"path": "/ready", "status": 200} if kind == "http" else {})}
            with self.assertRaisesRegex(c.DeployError, "separate non-PROXY listener"):
                t.parse_topology(raw)
            raw["roles"][1]["readiness"]["listener"] = "control"
            t.parse_topology(raw)

    def test_stream_version_is_outbound_and_per_listener_only(self):
        for mode in ("none", "edge"):
            raw = proxy_topology(mode)
            raw["roles"][1]["listeners"]["plain"] = {"transport": "tcp", "bind": "public", "tls": mode, "authentication": True}
            parsed = t.parse_topology(raw)
            placed = network_profile().projection()
            placed["listeners"]["relay.plain"] = {**placed["listeners"]["relay.ingress"], "ports": [9444]}
            selected = runtime()
            selected = replace(selected, endpoints=(*selected.endpoints, c.AllocatedEndpoint("relay", "plain", "127.0.0.1", 18002)))
            rendered = edge.stream_projection(t.parse_profile(placed, parsed), selected, "web.example.org", topology=parsed)
            enabled = rendered["stream-relay.ingress.conf"]
            self.assertIn("proxy_protocol on; # v1\n", enabled)
            self.assertEqual(enabled.count("proxy_protocol"), 1)
            self.assertNotIn("proxy_protocol", rendered["stream-relay.plain.conf"])
            self.assertTrue(all("proxy_protocol" not in line for line in enabled.splitlines() if line.strip().startswith("listen ")))
            self.assertEqual("ssl_certificate " in enabled, mode == "edge")
            self.assertNotIn("@", enabled)

    def test_report_exposes_version_or_null(self):
        for raw, expected in ((proxy_topology(), "v1"), (declaration(), None)):
            with patch.object(listeners, "observe_dns", return_value=None), \
                    patch.object(listeners, "observe_sockets", return_value=()), \
                    patch.object(listeners, "process_members", return_value=frozenset()), patch.object(firewall, "observe"):
                result = report.declared_network(Mock(), network_profile(), topology=t.parse_topology(raw))
            records = json.loads(json.dumps(result["listeners"]))
            self.assertTrue(records)
            self.assertTrue(all(record["proxy_protocol"] == expected for record in records))

    def test_generation_18_client_host_and_sticky_floor(self):
        parsed = t.parse_topology(proxy_topology())
        self.assertEqual(c.FEATURE_PROXY_PROTOCOL_GENERATION, 18)
        self.assertGreaterEqual(upgrades.protocol()["generation"], c.FEATURE_PROXY_PROTOCOL_GENERATION)
        for topology, entry in ((parsed, {}), (t.parse_topology(declaration()), {"deployment_selection": {"minimum_generation": 18}})):
            self.assertEqual(upgrades.feature_generation(topology, entry), 18)
            for client, host, code in ((17, 18, "instance_capability_required"), (18, 17, "host_upgrade_required")):
                with self.assertRaisesRegex(c.DeployError, "generation 18") as caught:
                    upgrades.check_feature_generation({"tools": {"generation": host}}, topology, entry, client_generation=client)
                self.assertEqual(caught.exception.code, code)
            upgrades.check_feature_generation({"tools": {"generation": 18}}, topology, entry, client_generation=18)
        self.assertEqual(upgrades.feature_generation(t.parse_topology(declaration()), {}), 13)
        with self.assertRaisesRegex(c.DeployError, "generation 18"):
            upgrades._check_feature_request({"tools": {"generation": 18}, "instances": {}},
                {"topology": proxy_topology(), "client": {"generation": 17}}, "bootstrap-plan")

    def test_recovery_preflight_checks_archived_declaration_and_sticky_floor(self):
        host = Mock()
        host.read_registry.return_value = None
        request = {"client": {"generation": 17}, "project": {}, "profile": {}, "recovery_archive": "/private/input"}
        for raw, floor in ((proxy_topology(), 13), (declaration(), 18)):
            recovered = {"feature": SimpleNamespace(topology=t.parse_topology(raw)),
                         "metadata": {"deployment_selection": {"minimum_generation": floor}}}
            with patch.object(recover, "read_archive", return_value={}), \
                    patch.object(recover, "archive_configuration", return_value=recovered), \
                    self.assertRaisesRegex(c.DeployError, "generation 18"):
                upgrades.check_target_capabilities(host, request, "recover-plan")


class ProxyProtocolDeliveryTests(wiring.FeatureWiringFixture):
    def test_activation_target_gate_and_rollback_retain_floor(self):
        self.build_archive(public=True, independent=True, maintenance="continue")
        wiring.FeatureWiringTests.configure_firewall(self)
        plan = self.request("bootstrap-plan", topology=self.topology.projection())
        self.request("bootstrap-apply", topology=self.topology.projection(), plan_token=plan["plan_token"], confirm=True)
        self.request("activate", archive=str(self.archive), archive_sha256=c.digest(self.archive))
        old_version = self.version
        self.build_archive(public=True, proxy_protocol=True, independent=True, maintenance="continue",
                           version="20261115T020000Z-abcdef19")
        before = self.host.read_registry()
        with patch.dict(self.envelope, client={**self.envelope["client"], "generation": 17}), \
                self.assertRaisesRegex(c.DeployError, "generation 18"):
            self.request("release-plan", archive=str(self.archive), archive_sha256=c.digest(self.archive),
                         operation_id=str(uuid.uuid4()))
        self.assertEqual(self.host.read_registry(), before)
        self.request("activate", archive=str(self.archive), archive_sha256=c.digest(self.archive))
        registry, entry = self.host.require_identity(self.project, self.profile)
        self.assertEqual(entry["deployment_selection"]["minimum_generation"], 18)
        older_entry = copy.deepcopy(entry)
        older_entry["deployment_selection"]["minimum_generation"] = 13
        with patch.object(self.host, "require_identity", return_value=(registry, older_entry)), \
                self.assertRaisesRegex(c.DeployError, "generation 18"):
            upgrades.check_target_capabilities(self.host, {"project": self.project, "profile": self.profile,
                "client": {"generation": 17}, "target": self.version}, "rollback")
        wiring.FeatureWiringTests.rollback_target(self, old_version)
        _, entry = self.host.require_identity(self.project, self.profile)
        self.assertEqual(entry["deployment_selection"]["minimum_generation"], 18)
        with self.assertRaisesRegex(c.DeployError, "generation 18"):
            upgrades.check_instance_capabilities(entry, 17)


if __name__ == "__main__":
    unittest.main()
