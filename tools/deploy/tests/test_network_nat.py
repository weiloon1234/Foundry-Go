"""NAT isolation against Docker captures; all kernel commands are substituted."""
from dataclasses import replace
import json
from pathlib import Path
import re
import unittest

import test_network_real as captured
from test_network import c, firewall, INSTANCE

PUBLISHED = Path(__file__).parent / "network_fixtures/docker-published"


class NATObservationTests(unittest.TestCase):
    load_capture = captured.CapturedUFWTests.load_capture

    def setUp(self):
        self.host, claims, self.live = self.load_capture("24.04")
        self.claims = tuple(replace(claim, socket=replace(claim.socket, port=8554)) for claim in claims)
        directory = captured.CAPTURES / "docker-acceptance-host"
        self.live.update({family: (directory / name).read_text() for family, name in
                          ((4, "iptables-save.txt"), (6, "ip6tables-save.txt"))})
        self.original = dict(self.live)
        self.command = self.host.run
        self.backend = "nf_tables"
        def command(args, **kwargs):
            response = self.command(args, **kwargs)
            if args[-1] == "--version":
                response.stdout = "iptables v1.8.10 (" + self.backend + ")\n"
            elif args[0] == "nft":
                if self.backend == "legacy":
                    response.stdout = '{"nftables": []}'
                else:
                    value = json.loads(response.stdout)
                    # Actual capture: iptables-save synthesizes these two empty
                    # base chains; neither appears in nft-ruleset.txt.
                    value["nftables"] = [item for item in value["nftables"] if not (
                        "chain" in item and item["chain"]["table"] == "nat"
                        and item["chain"]["name"] in ("PREROUTING", "INPUT")
                        and not firewall.parse_tables(self.live[4 if item["chain"]["family"] == "ip" else 6])["nat"]["chains"][item["chain"]["name"]])]
                    response.stdout = json.dumps(value)
            return response
        self.host.run = command

    def observe(self):
        return firewall.observe(self.host, INSTANCE, self.claims, integrated=False)

    def auxiliary(self, family, text):
        # Keep each captured filter table unchanged while varying auxiliary policy.
        self.live[family] = self.original[family].split("*nat", 1)[0] + text

    def test_unedited_docker_dns_capture_is_closed_and_observation_is_read_only(self):
        before = {p: self.host.paths.p(p).read_bytes() for p in firewall.FILES}
        self.assertTrue(self.observe().closed)
        self.assertEqual(before, {p: self.host.paths.p(p).read_bytes() for p in firewall.FILES})
        text = (captured.CAPTURES / "docker-acceptance-host/nft-ruleset.txt").read_text()
        nat = text[text.index("table ip nat {"):text.index("table ip filter {")]
        self.assertEqual(set(re.findall(r"chain (\w+) \{", nat)), {"OUTPUT", "POSTROUTING", "DOCKER_OUTPUT", "DOCKER_POSTROUTING"})
        self.assertEqual(nat.count("dnat to"), 2)
        self.assertEqual(nat.count("snat to"), 2)
        self.assertIn("type nat hook output priority dstnat; policy accept;", nat)
        self.assertIn("type nat hook postrouting priority srcnat; policy accept;", nat)

    def test_published_port_allowed_or_refused_on_both_families_and_backends(self):
        before = self.observe().sha256
        for backend in ("nf_tables", "legacy"):
            self.backend = backend
            for family in (4, 6):
                for filename, allowed in (("nonclaimed.rules", True), ("claimed.rules", False)):
                    with self.subTest(backend=backend, family=family, fixture=filename):
                        self.live.update(self.original)
                        rules = (PUBLISHED / filename).read_text()
                        if family == 6:
                            rules = rules.replace("127.0.0.0/8", "::1/128").replace("172.17.0.0/16", "fd00:17::/64").replace("172.17.0.2:80", "[fd00:17::2]:80")
                        self.auxiliary(family, rules)
                        if allowed:
                            self.assertTrue(self.observe().closed)
                            self.assertNotEqual(before, self.observe().sha256)
                        else:
                            with self.assertRaises(c.DeployError) as caught:
                                self.observe()
                            self.assertEqual(caught.exception.code, "firewall_delivery_conflict")

    def test_inbound_port_ranges_missing_negated_and_unparseable_matches_refuse(self):
        base = (PUBLISHED / "nonclaimed.rules").read_text()
        for guard in ("", "--dport 8500:8600", "--dport 8554", "! --dport 8080", "--dport https",
                      "--dport 9000:8000", "--dport 99999", "--dports 8080,8554", "--dports 8080,broken",
                      "-m mystery --dport 8080", '--comment "--dport"', "--dport 8080 --dport 8554"):
            with self.subTest(guard=guard):
                self.auxiliary(4, base.replace("--dport 8080", guard))
                with self.assertRaises(c.DeployError):
                    self.observe()

    def test_output_only_translation_becomes_unsafe_when_reachable_from_input(self):
        for hook in ("PREROUTING", "INPUT"):
            for jump in ("-j", "-g"):
                with self.subTest(hook=hook, jump=jump):
                    self.live[4] = self.original[4].replace("--dport 53", "--dport 8554")
                    self.assertTrue(self.observe().closed)
                    position = self.live[4].rfind("COMMIT")
                    self.live[4] = self.live[4][:position] + f"-A {hook} {jump} DOCKER_OUTPUT\n" + self.live[4][position:]
                    with self.assertRaises(c.DeployError):
                        self.observe()

    def test_output_only_matches_need_no_inbound_packet_proof(self):
        # The owner extension is outside the narrow inbound match parser, but
        # this NAT path is exclusively local output and is fully inventoried.
        self.live[4] = self.original[4].replace("-A DOCKER_OUTPUT -d", "-A DOCKER_OUTPUT -m owner --uid-owner 0 -d")
        self.assertTrue(self.observe().closed)
        position = self.live[4].rfind("COMMIT")
        self.live[4] = self.live[4][:position] + "-A PREROUTING -j DOCKER_OUTPUT\n" + self.live[4][position:]
        with self.assertRaises(c.DeployError):
            self.observe()

    def test_transitive_jumps_and_redirects_are_walked_with_port_guards(self):
        base = (PUBLISHED / "nonclaimed.rules").read_text().replace(":DOCKER -", ":SECOND - [0:0]\n:DOCKER -")
        base = base.replace("--dport 8080 -j DNAT --to-destination 172.17.0.2:80", "--dport 8554 -j SECOND")
        for target in ("-j REDIRECT --to-ports 80", "-j DNAT --to-destination 172.17.0.2:80", "-j SECOND", "-j MISSING"):
            with self.subTest(target=target):
                rules = base.replace("COMMIT", "-A SECOND " + target + "\nCOMMIT")
                self.auxiliary(4, rules)
                with self.assertRaises(c.DeployError):
                    self.observe()
        self.auxiliary(4, base.replace("--dport 8554", "--dport 8080").replace("COMMIT", "-A SECOND -j REDIRECT --to-ports 80\nCOMMIT"))
        self.assertTrue(self.observe().closed)

    def test_port_boundaries_and_protocol_matches_are_conservative(self):
        base = (PUBLISHED / "nonclaimed.rules").read_text()
        for guard in ("--dport 8000:8553", "--dport 8555:9000", "-m multiport --dports 8080,8090:8100"):
            with self.subTest(guard=guard):
                self.auxiliary(4, base.replace("--dport 8080", guard))
                self.assertTrue(self.observe().closed)
        udp = base.replace("-p tcp -m tcp --dport 8080", "-p udp")
        self.auxiliary(4, udp)
        self.assertTrue(self.observe().closed)
        for rules in (udp.replace("-p udp", "! -p udp"),
                      base.replace(":DOCKER -", ":DNAT - [0:0]\n:DOCKER -"),
                      base.replace("-p tcp -m tcp --dport 8080", "-p 6 -m tcp --dport 8554")):
            self.auxiliary(4, rules)
            with self.assertRaises(c.DeployError):
                self.observe()

    def test_raw_and_mangle_only_allow_proven_disjoint_delivery_changes(self):
        for family in (4, 6):
            for table in ("raw", "mangle"):
                for action in ("DROP", "NOTRACK", "MARK --set-xmark 0x1/0xffffffff"):
                    with self.subTest(family=family, table=table, action=action):
                        self.live.update(self.original)
                        rules = f"*{table}\n:PREROUTING ACCEPT [0:0]\n-A PREROUTING -p tcp -m tcp --dport 8080 -j {action}\nCOMMIT\n"
                        self.auxiliary(family, rules)
                        self.assertTrue(self.observe().closed)
                        for guard in ("--dport 8554", ""):
                            self.auxiliary(family, rules.replace("--dport 8080", guard))
                            with self.assertRaises(c.DeployError):
                                self.observe()
        self.auxiliary(6, "*raw\n:PREROUTING ACCEPT [0:0]\n:OUTPUT ACCEPT [0:0]\nCOMMIT\n")
        self.assertTrue(self.observe().closed)

    def test_auxiliary_counters_do_not_change_digest_but_translation_does(self):
        before = self.observe().sha256
        original = self.host.run
        def command(args, **kwargs):
            result = original(args, **kwargs)
            if args[0] == "nft":
                value = json.loads(result.stdout)
                for item in value["nftables"]:
                    if "rule" in item:
                        item["rule"]["handle"] += 10000
                        for expression in item["rule"]["expr"]:
                            if "counter" in expression:
                                expression["counter"] = {"packets": 314, "bytes": 15926}
                result.stdout = json.dumps(value)
            return result
        self.host.run = command
        for family in self.live:
            self.live[family] = re.sub(r"\[[0-9]+:[0-9]+\]", "[314:15926]", self.live[family])
        self.assertEqual(before, self.observe().sha256)
        self.live[4] = self.live[4].replace("127.0.0.11:41177", "127.0.0.11:41178")
        self.assertNotEqual(before, self.observe().sha256)

    def test_persistent_nat_is_in_digest_and_cross_file_jumps_are_checked(self):
        before = self.observe().sha256
        path = self.host.paths.p("/etc/ufw/before.rules")
        text = path.read_text()
        path.write_text(text + (PUBLISHED / "nonclaimed.rules").read_text())
        observed = self.observe().sha256
        self.assertNotEqual(before, observed)
        path.write_text(re.sub(r"\[[0-9]+:[0-9]+\]", "[123:456]", path.read_text()))
        self.assertEqual(observed, self.observe().sha256)
        # A later restore can populate a chain declared empty by an earlier file.
        path.write_text(text + "*nat\n:PREROUTING ACCEPT [0:0]\n:CUSTOM - [0:0]\n-A PREROUTING -j CUSTOM\nCOMMIT\n")
        later = self.host.paths.p("/etc/ufw/user.rules")
        later.write_text(later.read_text() + "*nat\n:CUSTOM - [0:0]\n-A CUSTOM -p tcp --dport 8554 -j DNAT --to-destination 172.17.0.2:80\nCOMMIT\n")
        with self.assertRaises(c.DeployError):
            self.observe()

    def test_nat_drift_between_compatibility_reads_refuses(self):
        original = self.host.run
        reads = []
        def command(args, **kwargs):
            result = original(args, **kwargs)
            if args[0] == "iptables-save":
                reads.append(True)
                if len(reads) > 1:
                    result.stdout = result.stdout.replace("127.0.0.11:41177", "127.0.0.11:41178")
            return result
        self.host.run = command
        with self.assertRaises(c.DeployError) as caught:
            self.observe()
        self.assertEqual(caught.exception.code, "firewall_drift")

    def test_nft_nat_alternate_hooks_and_unaccounted_rules_refuse(self):
        original = self.host.run
        for alteration in ("priority", "extra_rule", "native_table", "missing_rule"):
            def command(args, **kwargs):
                result = original(args, **kwargs)
                if args[0] == "nft":
                    value = json.loads(result.stdout)
                    chain = next(item["chain"] for item in value["nftables"] if "chain" in item and item["chain"]["table"] == "nat" and item["chain"]["name"] == "OUTPUT")
                    rule = next(item for item in value["nftables"] if "rule" in item and item["rule"]["table"] == "nat")
                    if alteration == "priority":
                        chain["prio"] = -101
                    elif alteration == "extra_rule":
                        value["nftables"].append(rule)
                    elif alteration == "missing_rule":
                        value["nftables"].remove(rule)
                    else:
                        value["nftables"].append({"table": {"family": "inet", "name": "nat"}})
                    result.stdout = json.dumps(value)
                return result
            self.host.run = command
            with self.subTest(alteration=alteration), self.assertRaises(c.DeployError):
                self.observe()


if __name__ == "__main__":
    unittest.main()
