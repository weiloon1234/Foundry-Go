"""Lane C unit regressions; all host commands are fake and all files task-owned."""
from dataclasses import replace
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import Mock, patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "lib"))
import certificates
import contracts as c
import edge
import firewall
import listeners
import report
import topology as t
from host import Host, Paths
from test_contracts import fixture
from test_host_capabilities import BOOT, CURRENT, DIGEST, INSTANCE, OLD, OPERATION, OTHER, declaration, profile, resolution


def network_profile():
    value = profile()
    value["instance_id"] = INSTANCE
    value["listeners"]["relay.ingress"]["source_cidrs"].append("2001:db8:1::/64")
    value["listeners"]["relay.ingress"]["dns"]["public_addresses"].append("2001:db8::10")
    return t.parse_profile(value, t.parse_topology(declaration()))


def runtime(identity=CURRENT, port=18000):
    value = resolution()
    value["id"] = identity
    value["roles"] = ["http", "relay", "worker"]
    value["environments"] = [{"role": role, "path": f"runtime/{identity}/{role}/app.env", "sha256": OTHER}
                             for role in value["roles"]]
    value["endpoints"] = [{"role": "http", "listener": "http", "address": "127.0.0.1", "port": port},
                          {"role": "relay", "listener": "ingress", "address": "127.0.0.1", "port": port + 1}]
    return c.RuntimeResolution.parse(value)


def rule_file(stem, chain=None):
    names = [f"ufw-{stem}-input", f"ufw-{stem}-forward", f"ufw-{stem}-output"]
    text = "*filter\n" + "".join(":" + name + " - [0:0]\n" for name in names)
    if stem == "before":
        text += "-A ufw-before-input -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT\n"
        text += "-A ufw-before-input -i lo -j ACCEPT\n"
    if stem == "user":
        text += "-A ufw-user-input -p tcp -m tcp --dport 22 -j ACCEPT\n"
    text += "COMMIT\n"
    return firewall.persistent_projection(text, INSTANCE) if chain and stem == "before" else text


def live_rules(open_rules=()):
    chain = firewall.chain_name(INSTANCE)
    parts = [firewall.parse_rules(rule_file(stem, chain))[0] for stem in ("before", "user", "after")]
    chains = {name: rules for part in parts for name, rules in part.items()}
    chains[chain] = list(open_rules)
    chains["INPUT"] = [("-j", "ufw-" + stem + "-input") for stem in ("before", "user", "after")]
    chains["FORWARD"] = [("-j", "ufw-" + stem + "-forward") for stem in ("before", "user", "after")]
    chains["OUTPUT"] = []
    chains["ufw-skip-to-policy-input"] = [("-j", "DROP")]
    chains["ufw-skip-to-policy-forward"] = [("-j", "DROP")]
    text = "*filter\n"
    for name in chains:
        policy = "DROP" if name in ("INPUT", "FORWARD") else "ACCEPT" if name == "OUTPUT" else "-"
        text += f":{name} {policy} [0:0]\n"
    for name, rules in chains.items():
        for rule in rules:
            text += "-A " + name + " " + " ".join(rule) + "\n"
    return text + "COMMIT\n"


class SocketTests(unittest.TestCase):
    def test_protocol_family_address_and_wildcards_are_not_port_only(self):
        tcp = listeners.SocketIdentity("tcp", 4, "203.0.113.10", 9443)
        self.assertFalse(listeners.conflicts(tcp, replace(tcp, transport="udp")))
        self.assertFalse(listeners.conflicts(tcp, replace(tcp, address="203.0.113.11")))
        self.assertTrue(listeners.conflicts(tcp, listeners.SocketIdentity("tcp", 6, "::", 9443)))
        self.assertTrue(listeners.conflicts(tcp, replace(tcp, address="0.0.0.0")))

    def test_reservation_prevents_a_b_a_reuse_and_live_socket_collision(self):
        a = runtime(OLD)
        reserved = listeners.private_claims(INSTANCE, a)
        observed = (listeners.ObservedSocket(listeners.SocketIdentity("tcp", 4, "0.0.0.0", 18002), (23,)),)
        allocated = listeners.allocate_private(INSTANCE, (("http", "http"), ("relay", "ingress")), reserved, observed,
                                                first=18000, last=18005)
        self.assertEqual([e.port for e in allocated], [18003, 18004])
        with self.assertRaises(c.DeployError):
            listeners.require_available(reserved, reserved, ())

    def test_actual_udp_and_tcp_owners_are_checked(self):
        claim = listeners.public_claims(network_profile())[0]
        line = 'LISTEN 0 511 203.0.113.10:9443 0.0.0.0:* users:(("nginx",pid=9,fd=6))\n'
        observed = listeners.parse_sockets(line, "tcp", 4)
        self.assertEqual(listeners.ownership_findings((claim,), observed, {"nginx": {9}}, edge_verified=True), [])
        self.assertEqual(listeners.ownership_findings((claim,), observed, {"nginx": {8}}, edge_verified=True)[0]["code"],
                         "listener_owner_unverified")
        udp = listeners.ObservedSocket(replace(claim.socket, transport="udp"), (9,))
        self.assertIn("unexpected_public_listener", [v["code"] for v in listeners.ownership_findings((claim,), (udp,), {})])

    def test_external_dns_is_instructions_and_observation_not_provider_proof(self):
        binding = network_profile().listeners[0][1]
        observation = c.DNSObservation(binding.hostname, binding.public_addresses, c.now(), "recursive")
        result = listeners.external_dns(binding, observation)
        self.assertTrue(result["addresses_match"])
        self.assertFalse(result["provider_write"])
        self.assertIsNone(result["provider_dns_only"])
        self.assertEqual(result["cutover"], "pending_external_dns")
        self.assertEqual([v["type"] for v in result["instructions"]], ["A", "AAAA"])

    def test_ingress_does_not_authorize_start_until_exit_and_isolation(self):
        old, candidate = runtime(OLD), runtime(CURRENT, 19000)
        claims = listeners.public_claims(network_profile())
        fw = firewall.FirewallObservation(INSTANCE, BOOT, True, DIGEST, (9443,), tuple(c.socket for c in claims))
        ng = edge.EdgeObservation(INSTANCE, BOOT, ("http", "relay"), True, OTHER, ((19, 7),))
        units = tuple(c.UnitIdentity(INSTANCE, role, 1) for role in ("http", "relay"))
        evidence = listeners.ingress_evidence(OPERATION, BOOT, old, candidate, units, DIGEST, fw, ng, claims=claims)
        self.assertIs(evidence.stage, c.ClosureStage.INGRESS_WITHDRAWN)
        self.assertIsNone(evidence.backend_exit_sha256)
        with self.assertRaises(c.DeployError):
            listeners.complete_closure(evidence, candidate, backend_exit_sha256=None,
                                       reserved=candidate.endpoints, old_references=old.endpoints)
        with self.assertRaises(c.DeployError):
            listeners.complete_closure(evidence, candidate, backend_exit_sha256=DIGEST,
                                       reserved=candidate.endpoints, old_references=candidate.endpoints)
        closed = listeners.complete_closure(evidence, candidate, backend_exit_sha256=DIGEST,
                                           reserved=candidate.endpoints, old_references=old.endpoints)
        self.assertIs(closed.stage, c.ClosureStage.APPLICATION_CLOSED)
        self.assertEqual(c.ClosureEvidence.parse(c.record_document(closed)), closed)
        with self.assertRaises(c.DeployError):
            listeners.ingress_evidence(OPERATION, BOOT, old, candidate, units, DIGEST, replace(fw, sockets=()), ng, claims=claims)


class NetworkHostTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.host = Host(Paths(Path(temporary.name)))
        self.profile = network_profile()
        self.claims = listeners.public_claims(self.profile)
        self.calls = []
        self.live = {4: live_rules(), 6: live_rules()}
        self.host.run = self.command
        self.host.owned_write(self.host.paths.p("/proc/sys/kernel/random/boot_id"), BOOT)
        self.host.owned_write(self.host.paths.p("/etc/default/ufw"), 'IPV6=yes\nDEFAULT_INPUT_POLICY="DROP"\nDEFAULT_FORWARD_POLICY="DROP"\n')
        self.host.owned_write(self.host.paths.p("/etc/ufw/ufw.conf"), "ENABLED=yes\n")
        for suffix in ("", "6"):
            for stem in ("before", "user", "after"):
                self.host.owned_write(self.host.paths.p(f"/etc/ufw/{stem}{suffix}.rules"), rule_file(stem, True))

    def command(self, args, **kwargs):
        self.calls.append((args, kwargs))
        if args == ["ufw", "status"]:
            output = "Status: active\n"
        elif args[-1] == "--version":
            output = "iptables v1.8.10 (legacy)\n"
        elif args[0] in ("iptables-save", "ip6tables-save"):
            output = self.live[6 if args[0].startswith("ip6") else 4]
        elif args[0] == "nft":
            output = '{"nftables": []}'
        elif args[:4] == ["ip", "-j", "address", "show"]:
            output = json.dumps([{"addr_info": [{"local": a} for a in self.profile.listeners[0][1].public_addresses]}])
        elif args[0] == "ip":
            output = "[]"
        elif args[0].endswith("-restore"):
            body = kwargs["input"]
            rules = [tuple(line.split()[2:]) for line in body.splitlines() if line.startswith("-A ")]
            self.live[6 if args[0].startswith("ip6") else 4] = live_rules(rules)
            output = ""
        else:
            raise AssertionError(args)
        return subprocess.CompletedProcess(args, 0, output, "")

    def test_full_observation_and_runtime_exposure_never_write_persistent_allows(self):
        before = firewall.observe(self.host, INSTANCE, self.claims)
        snapshot = {p: self.host.paths.p(p).read_bytes() for p in firewall.FILES}
        opened = firewall.open_runtime(self.host, INSTANCE, self.claims, self.profile.listeners,
                                       closed_sha256=before.sha256, authorize=lambda: True)
        self.assertFalse(opened.closed)
        self.assertEqual(snapshot, {p: self.host.paths.p(p).read_bytes() for p in firewall.FILES})
        self.assertFalse(any(args[:2] == ["ufw", "allow"] for args, _ in self.calls))
        closed = firewall.withdraw(self.host, INSTANCE, self.claims)
        self.assertTrue(closed.closed)
        self.assertEqual(before.sha256, closed.sha256)

    def test_foreign_allow_routed_allow_ipv6_allow_and_default_allow_refuse(self):
        original = self.live.copy()
        variants = ((4, "-A INPUT -j ACCEPT\n"), (4, "-A FORWARD -i eth1 -j ACCEPT\n"),
                    (6, "-A INPUT -p tcp -m multiport --dports 9000:9999 -j ACCEPT\n"))
        for family, rule in variants:
            with self.subTest(family=family, rule=rule):
                self.live = original.copy()
                self.live[family] = self.live[family].replace("COMMIT", rule + "COMMIT")
                with self.assertRaises(c.DeployError):
                    firewall.observe(self.host, INSTANCE, self.claims)
        self.live = original.copy()
        self.live[4] = self.live[4].replace(":INPUT DROP", ":INPUT ACCEPT")
        with self.assertRaises(c.DeployError):
            firewall.observe(self.host, INSTANCE, self.claims)
        self.assertFalse(any(args[0].endswith("-restore") for args, _ in self.calls))

    def test_persistent_foreign_allow_and_missing_ipv6_fail_before_open(self):
        path = self.host.paths.p("/etc/ufw/user6.rules")
        path.write_text(path.read_text().replace("COMMIT", "-A ufw-user-input -j ACCEPT\nCOMMIT"))
        with self.assertRaises(c.DeployError):
            firewall.observe(self.host, INSTANCE, self.claims)
        path.unlink()
        with self.assertRaises(c.DeployError):
            firewall.observe(self.host, INSTANCE, self.claims)

    def test_failed_second_family_open_closes_first_and_never_returns_success(self):
        before = firewall.observe(self.host, INSTANCE, self.claims)
        normal = self.command
        failed = False
        def fail_once(args, **kwargs):
            nonlocal failed
            if args[0] == "ip6tables-restore" and not failed:
                failed = True
                raise c.DeployError("injected", "second family failed")
            return normal(args, **kwargs)
        self.host.run = fail_once
        with self.assertRaises(c.DeployError):
            firewall.open_runtime(self.host, INSTANCE, self.claims, self.profile.listeners,
                                  closed_sha256=before.sha256, authorize=lambda: True)
        self.assertTrue(firewall.observe(self.host, INSTANCE, self.claims).closed)

    def test_persistent_projection_preserves_foreign_bytes_and_is_idempotent(self):
        self.assertNotEqual(firewall.chain_name(INSTANCE), firewall.chain_name(OLD))
        old = rule_file("before")
        changed = firewall.persistent_projection(old, INSTANCE)
        self.assertEqual(changed, firewall.persistent_projection(changed, INSTANCE))
        added = [line for line in changed.splitlines(True) if line not in old.splitlines(True)]
        reconstructed = changed
        for line in added:
            reconstructed = reconstructed.replace(line, "", 1)
        self.assertEqual(reconstructed, old)
        self.assertNotIn("ACCEPT", "".join(added))

    def test_scoped_withdrawal_preserves_unaffected_instance_rules(self):
        retained = tuple(replace(claim, role="retained", socket=replace(claim.socket, port=9444)) for claim in self.claims)
        binding = replace(self.profile.listeners[0][1], ports=(9444,))
        bindings = (*self.profile.listeners, ("retained.ingress", binding))
        for family in (4, 6):
            _, rules = firewall.exposure_rules(INSTANCE, (*self.claims, *retained), bindings, family)
            self.live[family] = live_rules(rules)
        closed = firewall.withdraw(self.host, INSTANCE, self.claims, retained_claims=retained, bindings=bindings)
        self.assertTrue(closed.closed)
        for family in (4, 6):
            self.assertIn("--dport 9444", self.live[family])
            self.assertNotIn("--dport 9443", self.live[family])

    def test_failed_top_level_stream_integration_restores_existing_layout(self):
        import hashlib
        main = "include /etc/nginx/modules-enabled/*.conf;\nhttp {}\n"
        self.host.owned_write(self.host.paths.p("/etc/nginx/nginx.conf"), main)
        with patch.object(self.host, "run", side_effect=c.DeployError("nginx_invalid", "injected")):
            with self.assertRaises(c.DeployError):
                edge.install_stream_include(self.host, reviewed_nginx_sha256=hashlib.sha256(main.encode()).hexdigest())
        self.assertFalse(self.host.paths.p(edge.STREAM_INCLUDE).exists())
        self.assertEqual(self.host.paths.p("/etc/nginx/nginx.conf").read_text(), main)

    def test_boot_and_reload_keep_reference_quarantine_until_new_boot(self):
        edge.prepare_boot(self.host, INSTANCE)
        first = runtime(OLD)
        edge.retain_references(self.host, INSTANCE, first)
        directory = edge._runtime(self.host, INSTANCE)
        (directory / "http-open.conf").write_text('"" 1;\n')
        (directory / "stream-relay.ingress.conf").write_text("open")
        edge.prepare_boot(self.host, INSTANCE)
        self.assertEqual(edge.retained_references(self.host, INSTANCE), first.endpoints)
        self.assertEqual(list(directory.glob("*.conf")), [])
        self.host.paths.p("/proc/sys/kernel/random/boot_id").write_text(OPERATION)
        edge.prepare_boot(self.host, INSTANCE)
        self.assertEqual(edge.retained_references(self.host, INSTANCE), ())


class ProjectionTests(unittest.TestCase):
    def test_stream_is_top_level_tls_only_dual_family_and_uses_resolved_backends(self):
        text = edge.stream_projection(network_profile(), runtime(), "web.example.org")["stream-relay.ingress.conf"]
        self.assertIn("listen [2001:db8::10]:9443 ssl ipv6only=on;", text)
        self.assertIn("proxy_pass 127.0.0.1:18001;", text)
        self.assertIn("ssl_protocols TLSv1.2 TLSv1.3;", text)
        self.assertIn("deny all;", text)
        self.assertNotIn("proxy_protocol", text)
        self.assertTrue(edge._top_level_include("include /etc/nginx/modules-enabled/*.conf;\nhttp {}", "/etc/nginx/modules-enabled/*.conf"))
        self.assertFalse(edge._top_level_include("http { include /etc/nginx/modules-enabled/*.conf; }", "/etc/nginx/modules-enabled/*.conf"))

    def test_http_gate_preserves_upload_configuration_and_is_instance_local(self):
        from provision import _template
        tools = Path(__file__).resolve().parents[1]
        _, _, entry = fixture()
        original = _template(tools, "nginx-https.conf", entry)
        gated = edge.gate_http(INSTANCE, original)
        self.assertEqual(gated.count("return 503;"), 2)
        self.assertIn("include /run/foundry/edge/" + INSTANCE, gated)
        self.assertIn("default 0;", gated)
        restored = gated.split("}\n", 1)[1].replace("    if ($fdy_" + INSTANCE.replace("-", "") + "_open = 0) { return 503; }\n", "")
        self.assertEqual(restored, original)
        self.assertNotEqual(gated, edge.gate_http(OLD, original))

    def test_report_distinguishes_recorded_ingress_from_complete_closure(self):
        from test_host_capabilities import closure
        document = closure()
        document.update(stage="ingress_withdrawn", backend_exit_sha256=None, old_reference_isolation_sha256=None)
        evidence = c.ClosureEvidence.parse(document)
        with patch.object(listeners, "observe_sockets", return_value=()), \
                patch.object(listeners, "process_members", return_value=frozenset()), \
                patch.object(listeners, "observe_dns", side_effect=OSError), \
                patch.object(firewall, "observe"):
            result = report.declared_network(Mock(), network_profile(), closure=evidence)
        self.assertEqual(result["admission"]["recorded_stage"], "ingress_withdrawn")
        self.assertFalse(result["admission"]["current_proof"])
        self.assertIn({"code": "application_admission_unverified"}, result["findings"])
        self.assertNotIn(DIGEST, json.dumps(result))
        self.assertNotIn(OTHER, json.dumps(result))


class NetworkCertificateTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.host = Host(Paths(Path(temporary.name)))
        _, _, self.entry = fixture()
        self.entry["profile"]["instance_id"] = INSTANCE
        self.entry["certificate"]["zone_ref"] = "example.org"
        self.host.journal = Mock()
        self.calls = []
        def command(args, **kwargs):
            self.calls.append(args)
            output = "DNS:retained.example.org, DNS:" + self.entry["profile"]["domain"] if "-ext" in args else ""
            return subprocess.CompletedProcess(args, 0, output, "")
        self.host.run = command

    def test_san_extension_retains_existing_names_and_changes_only_owned_hook(self):
        credential = self.host.paths.p("/token")
        self.host.owned_write(credential, "dns_cloudflare_api_token = " + "x" * 32 + "\n", mode=0o600)
        original_hook = certificates.RELOAD_HOOK
        with patch.object(certificates, "_identity_ready"), patch.object(certificates, "_renewal_ready"), \
                patch.object(certificates, "_synthetic", return_value=False), \
                patch.object(certificates, "_credentials", return_value=credential), \
                patch.object(certificates, "_resolve_zone", return_value="example.org"), \
                patch.object(certificates, "_renewal_dry_run"):
            certificates.ensure_hostnames(self.host, {}, self.entry, (network_profile(),))
            claim = certificates._network_claim(self.host, self.entry)
            self.assertEqual(set(claim["hostnames"]), {self.entry["profile"]["domain"], "retained.example.org", "relay.example.org"})
            issued = [args for args in self.calls if args[0] == "certbot"]
            self.assertEqual(len(issued), 1)
            self.assertIn(certificates.NETWORK_RELOAD_HOOK, issued[0])
            self.assertEqual(certificates._reload_hook(self.host, self.entry), certificates.NETWORK_RELOAD_HOOK)
            certificates.ensure_hostnames(self.host, {}, self.entry, (network_profile(),))
            self.assertEqual(len([args for args in self.calls if args[0] == "certbot"]), 1)
        self.assertEqual(certificates.RELOAD_HOOK, original_hook)

    def test_foreign_zone_refuses_before_claim_or_issuance(self):
        credential = self.host.paths.p("/token")
        self.host.owned_write(credential, "dns_cloudflare_api_token = " + "x" * 32 + "\n", mode=0o600)
        with patch.object(certificates, "_identity_ready"), patch.object(certificates, "_renewal_ready"), \
                patch.object(certificates, "_synthetic", return_value=False), \
                patch.object(certificates, "_credentials", return_value=credential), \
                patch.object(certificates, "_resolve_zone", return_value="other-zone"):
            with self.assertRaises(c.DeployError):
                certificates.ensure_hostnames(self.host, {}, self.entry, (network_profile(),))
        self.assertFalse(certificates._network_claim_path(self.host, self.entry).exists())
        self.assertFalse(any(args[0] == "certbot" for args in self.calls))


if __name__ == "__main__":
    unittest.main()
