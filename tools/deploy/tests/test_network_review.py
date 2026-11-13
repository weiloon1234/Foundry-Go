"""Regressions for the six findings against 34cd3be3; no host/provider effects."""
import copy
from dataclasses import replace
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import Mock, patch

import test_network as fixture
from network_firewall_fixture import ROOT, live_rules, nft_inventory, rule_file
from host import Host, Paths
import certificates
import contracts as c
import edge
import firewall
import listeners
import report


class FirewallReviewTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.host = Host(Paths(Path(temporary.name)))
        self.profile = fixture.network_profile()
        self.claims = listeners.public_claims(self.profile)
        self.backend = "legacy"
        self.live = {family: live_rules(fixture.INSTANCE, family) for family in (4, 6)}
        self.host.run = self.command
        self.host.owned_write(self.host.paths.p("/proc/sys/kernel/random/boot_id"), fixture.BOOT)
        self.host.owned_write(self.host.paths.p("/etc/default/ufw"),
                              'IPV6=yes\nDEFAULT_INPUT_POLICY="DROP"\nDEFAULT_FORWARD_POLICY="DROP"\nDEFAULT_OUTPUT_POLICY="ACCEPT"\nMANAGE_BUILTINS=no\nIPT_MODULES=""\nIPT_SYSCTL=/etc/ufw/sysctl.conf\n')
        self.host.owned_write(self.host.paths.p("/etc/ufw/ufw.conf"), "ENABLED=yes\nLOGLEVEL=low\n")
        for family, suffix in ((4, ""), (6, "6")):
            for stem in ("before", "after", "user"):
                self.host.owned_write(self.host.paths.p(f"/etc/ufw/{stem}{suffix}.rules"), rule_file(stem, fixture.INSTANCE, family))
        for name in ("ufw-init", "ufw-init-functions"):
            self.host.owned_write(self.host.paths.p("/usr/lib/ufw/" + name), (ROOT / name).read_text())

    def command(self, args, **kwargs):
        if args == ["ufw", "status"]:
            output = "Status: active\n"
        elif args[-1] == "--version":
            output = "iptables v1.8.10 (" + self.backend + ")\n"
        elif args[0] in ("iptables-save", "ip6tables-save"):
            output = self.live[6 if args[0].startswith("ip6") else 4]
        elif args[0] == "nft":
            output = json.dumps(nft_inventory(self.live) if self.backend == "nf_tables" else {"nftables": []})
        elif args[:4] == ["ip", "-j", "address", "show"]:
            output = json.dumps([{"addr_info": [{"local": a} for a in self.profile.listeners[0][1].public_addresses]}])
        elif args[0] == "ip":
            output = "[]"
        else:
            raise AssertionError(args)
        return subprocess.CompletedProcess(args, 0, output, "")

    def test_default_ubuntu_nft_layout_is_observed_closed(self):
        self.backend = "nf_tables"
        self.assertTrue(firewall.observe(self.host, fixture.INSTANCE, self.claims, integrated=False).closed)

    def test_standard_ipv6_projection_targets_ufw6(self):
        text = rule_file("before", family=6)
        # The family is explicit because UFW owns two different chain namespaces.
        projected = firewall.persistent_projection(text, fixture.INSTANCE, family=6)
        self.assertIn("-A ufw6-before-input -j " + firewall.chain_name(fixture.INSTANCE), projected)
        self.assertNotIn("-A ufw-before-input", projected)

    def test_standard_reload_helper_graph_is_observed_closed(self):
        self.assertIn("-j ufw-logging-deny", rule_file("before"))
        self.assertTrue(firewall.observe(self.host, fixture.INSTANCE, self.claims, integrated=False).closed)

    def test_both_backends_preserve_foreign_policy_and_refuse_unsafe_helpers(self):
        original = self.live.copy()
        snapshot = {p: self.host.paths.p(p).read_bytes() for p in firewall.FILES}
        for backend in ("legacy", "nf_tables"):
            self.backend = backend
            self.live = original.copy()
            self.assertTrue(firewall.observe(self.host, fixture.INSTANCE, self.claims).closed)
            for family in (4, 6):
                with self.subTest(backend=backend, family=family):
                    prefix = "ufw6" if family == 6 else "ufw"
                    self.live = original.copy()
                    self.live[family] = self.live[family].replace("COMMIT", f"-A {prefix}-before-logging-input -j ACCEPT\nCOMMIT")
                    with self.assertRaises(c.DeployError):
                        firewall.observe(self.host, fixture.INSTANCE, self.claims)
        self.assertEqual(snapshot, {p: self.host.paths.p(p).read_bytes() for p in firewall.FILES})

    def test_native_tables_hooks_invisible_rules_and_decoder_warnings_refuse(self):
        self.backend = "nf_tables"
        original = self.command
        base = nft_inventory(self.live)
        variants = []
        native = copy.deepcopy(base)
        native["nftables"].append({"table": {"family": "inet", "name": "foreign", "handle": 900}})
        variants.append(native)
        hook = copy.deepcopy(base)
        next(v["chain"] for v in hook["nftables"] if "chain" in v and v["chain"]["name"] == "INPUT")["prio"] = -10
        variants.append(hook)
        missing = copy.deepcopy(base)
        missing["nftables"].remove(next(v for v in missing["nftables"] if "rule" in v))
        variants.append(missing)
        expression = copy.deepcopy(base)
        next(v["rule"] for v in expression["nftables"] if "rule" in v)["expr"].append({"dup": {"addr": "127.0.0.1"}})
        variants.append(expression)
        for document in variants:
            def command(args, **kwargs):
                return subprocess.CompletedProcess(args, 0, json.dumps(document), "") if args[0] == "nft" else original(args, **kwargs)
            self.host.run = command
            with self.assertRaises(c.DeployError):
                firewall.observe(self.host, fixture.INSTANCE, self.claims)
        def warning(args, **kwargs):
            response = original(args, **kwargs)
            if args[0] == "iptables-save":
                response.stderr = "Warning: incomplete native rule translation"
            return response
        self.host.run = warning
        with self.assertRaises(c.DeployError):
            firewall.observe(self.host, fixture.INSTANCE, self.claims)

    def test_modified_reload_code_and_shell_configuration_are_not_proof(self):
        path = self.host.paths.p("/usr/lib/ufw/ufw-init-functions")
        path.write_text(path.read_text() + "\n# drift\n")
        with self.assertRaises(c.DeployError):
            firewall.observe(self.host, fixture.INSTANCE, self.claims)
        path.write_text((ROOT / "ufw-init-functions").read_text())
        config = self.host.paths.p("/etc/default/ufw")
        config.write_text(config.read_text() + "RULES_PATH=/foreign\n")
        with self.assertRaises(c.DeployError):
            firewall.observe(self.host, fixture.INSTANCE, self.claims)


class NginxReviewTests(unittest.TestCase):
    def test_multiline_quoted_and_included_upstreams_are_reserved(self):
        dump = '''# configuration file /etc/nginx/nginx.conf:
http { upstream backend { include upstreams/*.conf; }
server { location / { proxy_pass
    "http://127.0.0.1:19001/path";
} location /other { proxy_pass http://backend; } } }
# configuration file /etc/nginx/upstreams/backend.conf:
server
    "[::1]:19002";
'''
        host = Mock()
        host.run.return_value = subprocess.CompletedProcess([], 0, dump, "")
        references = edge.upstream_references(host)
        self.assertEqual({(s.address, s.port) for s in references}, {("127.0.0.1", 19001), ("::1", 19002)})
        with self.assertRaises(c.DeployError):
            listeners.allocate_private(fixture.INSTANCE, (("http", "http"),), (),
                                       tuple(listeners.ObservedSocket(s, ()) for s in references), first=19001, last=19001)

    def test_unresolved_multiline_reference_refuses_instead_of_partial_inventory(self):
        host = Mock()
        host.run.return_value.stdout = '# configuration file /etc/nginx/nginx.conf:\nhttp { server { location / { proxy_pass\n "$backend"; } } }'
        with self.assertRaises(c.DeployError):
            edge.upstream_references(host)

    def test_included_fragment_reused_in_http_and_stream_keeps_both_contexts(self):
        host = Mock()
        host.run.return_value.stdout = '''# configuration file /etc/nginx/nginx.conf:
http { upstream peer { include peers.conf; } server { location / { proxy_pass http://peer; } } }
stream { upstream peer { include peers.conf; } server { proxy_pass peer; } }
# configuration file /etc/nginx/peers.conf:
# comment containing a fake server 127.0.0.1:9999;
server '127.0.0.1:19100'; server "[::1]:19101";
'''
        self.assertEqual({(s.address, s.port) for s in edge.upstream_references(host)}, {("127.0.0.1", 19100), ("::1", 19101)})

    def test_missing_includes_truncated_quotes_dns_and_orphan_files_refuse(self):
        bodies = ('http { include absent.conf; }', 'http { server { proxy_pass "http://127.0.0.1:19100; } }',
                  'http { upstream peer { server relay.example.org:19100; } }',
                  'http {}\n# configuration file /etc/nginx/orphan.conf:\nserver 127.0.0.1:19100;')
        for body in bodies:
            with self.subTest(body=body):
                host = Mock()
                host.run.return_value.stdout = '# configuration file /etc/nginx/nginx.conf:\n' + body
                with self.assertRaises(c.DeployError):
                    edge.upstream_references(host)


class RenewalReviewTests(unittest.TestCase):
    def test_certbot_zero_after_failed_hook_does_not_verify_issuance(self):
        case = fixture.NetworkCertificateTests()
        case.setUp()
        self.addCleanup(case.doCleanups)
        credential = case.host.paths.p("/token")
        case.host.owned_write(credential, "dns_cloudflare_api_token = " + "x" * 32 + "\n", mode=0o600)
        # Model Certbot's documented behavior: a failed deploy hook does not make
        # a successful issuance exit nonzero. The postcondition must run separately.
        original = case.host.run
        def certbot(args, **kwargs):
            if args[0] == "certbot":
                try:
                    certificates.verify_network_consumers(case.host, case.entry)
                except c.DeployError:
                    return subprocess.CompletedProcess(args, 0, "", "deploy hook failed")
            return original(args, **kwargs)
        case.host.run = certbot
        with patch.object(certificates, "_identity_ready"), patch.object(certificates, "_renewal_ready"), \
                patch.object(certificates, "_synthetic", return_value=False), \
                patch.object(certificates, "_credentials", return_value=credential), \
                patch.object(certificates, "_resolve_zone", return_value="example.org"), \
                patch.object(certificates, "_renewal_dry_run"), \
                patch.object(certificates, "verify_network_consumers", side_effect=c.DeployError("certificate_effective_renewal", "wrong leaf")), \
                patch.object(certificates, "_renewal_verified") as verified:
            with self.assertRaises(c.DeployError):
                certificates.ensure_hostnames(case.host, {}, case.entry, (fixture.network_profile(),))
            verified.assert_not_called()
        self.assertEqual(certificates.network_renewal_status(case.host, case.entry)["state"], "failed")

    def test_renewal_service_enforces_postcondition_after_certbot_zero(self):
        case = fixture.NetworkCertificateTests()
        case.setUp()
        self.addCleanup(case.doCleanups)
        case.host.owned_write(certificates._network_claim_path(case.host, case.entry), json.dumps({
            "instance_id": fixture.INSTANCE, "hostnames": [case.entry["profile"]["domain"]], "listeners": {}}), mode=0o600)
        case.host.read_registry = Mock(return_value={"instances": {fixture.INSTANCE: case.entry}})
        certificates._install_network_postcondition(case.host, case.entry)
        unit = case.host.paths.p("/etc/systemd/system/foundry-certbot-renew.service").read_text()
        self.assertIn("ExecStart=/usr/bin/certbot renew", unit)
        self.assertIn("ExecStartPost=/usr/bin/python3 -I ", unit)
        self.assertIn("certificates.py verify-network-renewal", unit)
        with patch.object(certificates, "Host", return_value=case.host), patch.object(certificates.os, "geteuid", return_value=0), \
                patch.object(case.host, "owned_write", side_effect=lambda path, content, mode=0o644:
                             c.atomic_write(path, content, mode, owner=os.getuid(), group=os.getgid())), \
                patch.object(certificates, "verify_network_consumers", side_effect=c.DeployError("certificate_effective_renewal", "handshake failed")):
            result = case.host.run(["certbot", "renew"])
            self.assertEqual(result.returncode, 0)
            with self.assertRaises(c.DeployError):
                certificates._main(["verify-network-renewal"])
        self.assertEqual(certificates.network_renewal_status(case.host, case.entry)["state"], "failed")
        with patch.object(certificates, "_identity_ready"), patch.object(certificates, "_renewal_ready"):
            observation = certificates.inspect(case.host, case.entry)
        # inspect also needs an existing lineage path; the dedicated status always
        # records this failure even if the underlying lineage is subsequently lost.
        self.assertNotEqual(observation["state"], "ready")

    def test_actual_postcondition_rejects_old_leaf_and_failed_handshake(self):
        case = fixture.NetworkCertificateTests()
        case.setUp()
        self.addCleanup(case.doCleanups)
        case.host.owned_write(certificates._network_claim_path(case.host, case.entry), json.dumps({
            "instance_id": fixture.INSTANCE, "hostnames": [case.entry["profile"]["domain"]], "listeners": {}}), mode=0o600)
        case.host.owned_write(case.host.paths.p(edge.RUNTIME + "/" + fixture.INSTANCE + "/http-open.conf"), '"" 1;\n')
        leaf = case.host.paths.p(certificates.CONFIG_DIR + "/live/" + case.entry["certificate"]["lineage"] + "/cert.pem")
        case.host.owned_write(leaf, "fixture certificate", mode=0o600)
        for failure in (False, True):
            connection, context = Mock(), Mock()
            connection.__enter__ = Mock(return_value=connection)
            connection.__exit__ = Mock(return_value=False)
            secured = Mock()
            secured.__enter__ = Mock(return_value=secured)
            secured.__exit__ = Mock(return_value=False)
            secured.getpeercert.return_value = b"old-leaf"
            context.wrap_socket.side_effect = OSError("handshake") if failure else None
            context.wrap_socket.return_value = secured
            with patch.object(certificates, "_identity_ready"), patch.object(certificates, "_renewal_ready"), \
                    patch.object(certificates.ssl, "PEM_cert_to_DER_cert", return_value=b"new-leaf"), \
                    patch.object(certificates.ssl, "create_default_context", return_value=context), \
                    patch.object(certificates.socket, "create_connection", return_value=connection), \
                    patch.object(certificates.time, "monotonic", side_effect=[0, 0, 0, 20]), \
                    patch.object(certificates.time, "sleep"):
                with self.assertRaises(c.DeployError):
                    certificates.verify_network_postcondition(case.host, case.entry)
            self.assertEqual(certificates.network_renewal_status(case.host, case.entry)["state"], "failed")


class ReportReviewTests(unittest.TestCase):
    def test_effective_renewal_failure_reaches_report_after_legacy_rollback(self):
        import test_report as report_fixture
        case = report_fixture.ReportTests()
        case.setUp()
        self.addCleanup(case.doCleanups)
        entry = case.host.registry["instances"][report_fixture.INSTANCE_ID]
        native = Host(Paths(case.host.paths.root))
        native.owned_write(certificates._network_claim_path(native, entry), json.dumps({
            "instance_id": entry["profile"]["instance_id"], "hostnames": [entry["profile"]["domain"]], "listeners": {}}), mode=0o600)
        with patch.object(certificates, "verify_network_consumers", side_effect=c.DeployError("certificate_effective_renewal", "private canary")):
            with self.assertRaises(c.DeployError):
                certificates.verify_network_postcondition(native, entry)
        result = report.collect_report(case.host)
        self.assertIn({"code": "certificate_effective_renewal_unverified", "instance_id": report_fixture.INSTANCE_ID}, result["problems"])
        self.assertEqual(case.item(result)["certificate"]["effective_renewal"]["state"], "failed")
        self.assertNotIn("private canary", json.dumps(result))

    def test_other_instances_claim_is_not_unexpected_and_host_scanner_uses_inventory(self):
        first = fixture.network_profile()
        second = replace(first, instance_id=fixture.OLD,
                         listeners=tuple((n, replace(b, ports=(9444,))) for n, b in first.listeners))
        claims = (*listeners.public_claims(first), *listeners.public_claims(second))
        observed = tuple(listeners.ObservedSocket(claim.socket, (9,)) for claim in claims)
        host = Mock()
        topology = fixture.t.parse_topology(fixture.declaration())
        raw_second = copy.deepcopy(first.projection())
        raw_second["instance_id"] = second.instance_id
        raw_second["listeners"]["relay.ingress"]["ports"] = [9444]
        host.read_registry.return_value = {"instances": {p["instance_id"]: {"profile": p, "topology": {**topology.projection(), "sha256": topology.sha256}}
                                                       for p in (first.projection(), raw_second)}}
        host.run.return_value = subprocess.CompletedProcess([], 0, "LISTEN 0 511 203.0.113.10:9443 0.0.0.0:*\nLISTEN 0 511 203.0.113.10:9444 0.0.0.0:*\n", "")
        with patch.object(listeners, "observe_sockets", return_value=observed), \
                patch.object(listeners, "process_members", return_value=frozenset({9})), \
                patch.object(listeners, "observe_dns", side_effect=OSError), patch.object(firewall, "observe"):
            value = report.declared_network(host, first)
        self.assertFalse(any(f["code"] == "unexpected_public_listener" for f in value["findings"]))
        self.assertEqual(value["unexpected_exposure"], [])
        self.assertTrue(all(f.get("instance_id", first.instance_id) == first.instance_id for f in value["findings"]))
        import time
        problems = []
        report.network(host, problems, time.monotonic() + 10, claims=claims)
        self.assertNotIn({"code": "unexpected_public_listener"}, problems)

    def test_declared_port_does_not_hide_wrong_address_or_udp(self):
        first = fixture.network_profile()
        claims = listeners.public_claims(first)
        unknown = listeners.ObservedSocket(replace(claims[0].socket, address="0.0.0.0"), (3,))
        udp = listeners.ObservedSocket(replace(claims[0].socket, transport="udp"), (4,))
        with patch.object(listeners, "observe_sockets", return_value=(unknown, udp)), \
                patch.object(listeners, "process_members", return_value=frozenset({9})), \
                patch.object(listeners, "observe_dns", side_effect=OSError), patch.object(firewall, "observe"):
            value = report.declared_network(Mock(), first, host_claims=claims)
        self.assertEqual({v["transport"] for v in value["unexpected_exposure"]}, {"tcp", "udp"})
