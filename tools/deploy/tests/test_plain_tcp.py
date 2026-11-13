"""Plain public TCP shares network custody but never becomes a TLS consumer."""
import copy
from dataclasses import replace
import json
from pathlib import Path
import unittest
from unittest.mock import Mock, patch

import test_network as fixture
from test_network import certificates, c, edge, firewall, listeners, report, t


def mixed_deployment():
    topology = fixture.network_topology("none").projection()
    topology["roles"][1]["listeners"]["secure"] = {
        "transport": "tcp", "bind": "public", "tls": "edge", "authentication": True}
    topology = t.parse_topology(topology)
    profile = fixture.network_profile().projection()
    profile["listeners"]["relay.secure"] = copy.deepcopy(profile["listeners"]["relay.ingress"])
    profile["listeners"]["relay.secure"]["ports"] = [9444]
    profile["listeners"]["relay.secure"]["dns"]["hostname"] = "secure.example.org"
    profile = t.parse_profile(profile, topology)
    resolution = fixture.runtime()
    resolution = replace(resolution, endpoints=(*resolution.endpoints,
                         c.AllocatedEndpoint("relay", "secure", "127.0.0.1", 18002)))
    return profile, topology, resolution


class PlainTCPTests(unittest.TestCase):
    def test_plain_dual_family_uses_same_proxy_acl_and_backend_without_certificate(self):
        profile, resolution = fixture.network_profile(), fixture.runtime()
        plain = edge.stream_projection(profile, resolution, None, topology=fixture.network_topology("none"))
        tls = edge.stream_projection(profile, resolution, "web.example.org", topology=fixture.network_topology())
        text = plain["stream-relay.ingress.conf"]
        self.assertIn("listen 203.0.113.10:9443;", text)
        self.assertIn("listen [2001:db8::10]:9443 ipv6only=on;", text)
        self.assertIn("proxy_pass 127.0.0.1:18001;", text)
        self.assertNotIn("ssl", text)
        self.assertNotIn("certificate", text)
        self.assertNotIn("@", text)
        stripped = "\n".join(line.replace(" ssl", "") for line in tls["stream-relay.ingress.conf"].splitlines()
                             if not line.strip().startswith("ssl_")) + "\n"
        self.assertEqual(text, stripped)
        profile, topology, resolution = mixed_deployment()
        mixed = edge.stream_projection(profile, resolution, "web.example.org", topology=topology)
        self.assertNotIn("ssl", mixed["stream-relay.ingress.conf"])
        self.assertIn("listen 203.0.113.10:9444 ssl;", mixed["stream-relay.secure.conf"])
        self.assertIn("proxy_pass 127.0.0.1:18002;", mixed["stream-relay.secure.conf"])

    def test_plan_and_report_explain_plain_authentication_without_tls_inspection(self):
        profile, topology = fixture.network_profile(), fixture.network_topology("none")
        findings = edge.edge_findings(profile.projection(), Path(edge.__file__).parent, topology=topology)
        self.assertEqual(findings[0]["code"], "listener_unencrypted")
        self.assertIn("unencrypted public TCP", findings[0]["detail"])
        self.assertIn("service must authenticate every client", findings[0]["detail"])
        with self.assertRaises(c.DeployError):
            edge.edge_findings(profile.projection(), Path("unused"))
        with patch.object(listeners, "observe_sockets", return_value=()), \
                patch.object(listeners, "process_members", return_value=frozenset()), \
                patch.object(listeners, "observe_dns", side_effect=OSError), \
                patch.object(firewall, "observe"), patch.object(certificates, "inspect") as inspect, \
                patch.object(certificates, "verify_network_consumers") as verify:
            result = report.declared_network(Mock(), profile, topology=topology, entry={}, host_claims=())
        self.assertIn(findings[0], result["findings"])
        self.assertTrue(all(item["tls"] == "none" and item["authentication"] is True for item in result["listeners"]))
        self.assertIsNone(result["tls"])
        inspect.assert_not_called()
        verify.assert_not_called()

    def test_plain_only_certificate_preparation_has_no_effects(self):
        host = Mock()
        entry = {"profile": {"instance_id": fixture.INSTANCE}}
        certificates.ensure_hostnames(host, {}, entry, (fixture.network_profile(),),
                                     topologies=(fixture.network_topology("none"),))
        self.assertEqual(host.mock_calls, [])
        with self.assertRaises(c.DeployError):
            certificates.ensure_hostnames(host, {}, entry, (fixture.network_profile(),), topologies=())
        self.assertEqual(host.mock_calls, [])

    def test_mixed_certificate_inventory_and_issuance_exclude_plain_names(self):
        case = fixture.NetworkCertificateTests()
        case.setUp()
        self.addCleanup(case.doCleanups)
        profile, topology, _ = mixed_deployment()
        credential = case.host.paths.p("/token")
        case.host.owned_write(credential, "dns_cloudflare_api_token = " + "x" * 32 + "\n", mode=0o600)
        with patch.object(certificates, "_identity_ready"), patch.object(certificates, "_renewal_ready"), \
                patch.object(certificates, "_synthetic", return_value=False), \
                patch.object(certificates, "_credentials", return_value=credential), \
                patch.object(certificates, "_resolve_zone", return_value="example.org") as zone, \
                patch.object(certificates, "_renewal_dry_run"), \
                patch.object(certificates, "verify_network_consumers", return_value={"state": "effective", "consumers": 1}):
            certificates.ensure_hostnames(case.host, {}, case.entry, (profile,), topologies=(topology,))
        claim = certificates._network_claim(case.host, case.entry)
        self.assertNotIn("relay.example.org", claim["hostnames"])
        self.assertIn("secure.example.org", claim["hostnames"])
        self.assertEqual({key.split(":")[0] for key in claim["listeners"]}, {"relay.secure"})
        issued = next(args for args in case.calls if args[0] == "certbot")
        self.assertNotIn("relay.example.org", issued)
        self.assertIn("secure.example.org", issued)
        self.assertNotIn("relay.example.org", [call.args[1] for call in zone.call_args_list])

    def test_retained_tls_binding_is_not_probed_when_selected_projection_is_plain(self):
        case = fixture.NetworkCertificateTests()
        case.setUp()
        self.addCleanup(case.doCleanups)
        profile = fixture.network_profile()
        lineage = case.entry["certificate"]["lineage"]
        case.host.owned_write(case.host.paths.p(certificates.CONFIG_DIR + "/live/" + lineage + "/cert.pem"), "fixture")
        claim = {"instance_id": fixture.INSTANCE, "hostnames": ["relay.example.org"],
                 "listeners": {"relay.ingress:" + fixture.DIGEST: profile.projection()["listeners"]["relay.ingress"]}}
        case.host.owned_write(certificates._network_claim_path(case.host, case.entry), json.dumps(claim), mode=0o600)
        projection = edge.stream_projection(profile, fixture.runtime(), None, topology=fixture.network_topology("none"))
        for name, content in projection.items():
            case.host.owned_write(case.host.paths.p(edge.RUNTIME + "/" + fixture.INSTANCE + "/" + name), content)
        with patch.object(certificates, "_identity_ready"), patch.object(certificates, "_renewal_ready"), \
                patch.object(certificates.ssl, "PEM_cert_to_DER_cert", return_value=b"leaf"), \
                patch.object(certificates.ssl, "create_default_context"), \
                patch.object(certificates.socket, "create_connection") as connect:
            result = certificates.verify_network_consumers(case.host, case.entry)
        self.assertEqual(result["consumers"], 0)
        connect.assert_not_called()

    def test_plain_open_withdraw_and_boot_use_existing_runtime_and_firewall_owners(self):
        case = fixture.NetworkHostTests()
        case.setUp()
        self.addCleanup(case.doCleanups)
        profile, topology, resolution = case.profile, fixture.network_topology("none"), fixture.runtime()
        directory = edge._runtime(case.host, fixture.INSTANCE)
        with patch.object(edge, "retain_global_references"), patch.object(edge, "_reload"), \
                patch.object(edge, "observe_projection"):
            before = firewall.observe(case.host, fixture.INSTANCE, case.claims)
            self.assertTrue(before.closed)
            authority = Mock(return_value=True)
            edge.open_runtime(case.host, profile, resolution, None, ("relay",), topology=topology, authorize=authority)
            self.assertEqual(authority.call_count, 2)
            self.assertIn("proxy_pass 127.0.0.1:18001;", (directory / "stream-relay.ingress.conf").read_text())
            opened = firewall.open_runtime(case.host, fixture.INSTANCE, case.claims, profile.listeners,
                                           closed_sha256=before.sha256, authorize=lambda: True)
            self.assertFalse(opened.closed)
            self.assertTrue(firewall.withdraw(case.host, fixture.INSTANCE, case.claims).closed)
            edge.withdraw(case.host, fixture.INSTANCE, ("relay",))
            self.assertFalse((directory / "stream-relay.ingress.conf").exists())
            self.assertEqual(edge.retained_references(case.host, fixture.INSTANCE), resolution.endpoints)
            edge.open_runtime(case.host, profile, resolution, None, ("relay",), topology=topology, authorize=lambda: True)
            edge.prepare_boot(case.host, fixture.INSTANCE)
            self.assertEqual(list(directory.glob("*.conf")), [])
            self.assertEqual(edge.retained_references(case.host, fixture.INSTANCE), resolution.endpoints)
            self.assertTrue(firewall.observe(case.host, fixture.INSTANCE, case.claims).closed)


if __name__ == "__main__":
    unittest.main()
