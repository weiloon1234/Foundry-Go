"""Replay unedited Ubuntu captures; commands never reach the host firewall."""
from collections import Counter
import json
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import Mock

import test_network as fixture
from test_network import c, edge, firewall, listeners
from host import Host, Paths
from network_firewall_fixture import ROOT as PACKAGE, nft_inventory

CAPTURES = Path(__file__).parent / "network_fixtures/ufw-real"
VARIANTS = ("24.04", "26.04", "24.04-legacy")


class CapturedUFWTests(unittest.TestCase):
    def load_capture(self, variant):
        directory = CAPTURES / variant
        # The legacy capture contains enabled rules/kernel dumps only. Its Ubuntu
        # settings, status and mode evidence come from the 24.04 capture.
        common = CAPTURES / ("24.04" if variant == "24.04-legacy" else variant)
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        host = Host(Paths(Path(temporary.name)))
        for line in (common / "modes-after-enable.txt").read_text().splitlines():
            mode, name = line.split()
            source = directory / "etc-ufw-enabled" / Path(name).name
            if source.is_file():
                host.owned_write(host.paths.p(name), source.read_text(), mode=int(mode, 8))
        host.owned_write(host.paths.p("/etc/default/ufw"), (common / "etc-default/ufw").read_text())
        host.owned_write(host.paths.p("/proc/sys/kernel/random/boot_id"), fixture.BOOT)
        for name in ("ufw-init", "ufw-init-functions"):
            host.owned_write(host.paths.p("/usr/lib/ufw/" + name), (PACKAGE / name).read_text())
        live = {family: (directory / name).read_text() for family, name in
                ((4, "iptables-save.txt"), (6, "ip6tables-save.txt"))}
        version = (directory / "iptables-version.txt").read_text()
        claims = listeners.public_claims(fixture.network_profile())
        def command(args, **kwargs):
            if args == ["ufw", "status"]:
                output = (common / "ufw-status.txt").read_text()
            elif args in (["iptables", "--version"], ["ip6tables", "--version"]):
                output = version
            elif args[0] in ("iptables-save", "ip6tables-save"):
                output = live[6 if args[0].startswith("ip6") else 4]
            elif args == ["nft", "-j", "list", "ruleset"]:
                # Captures supply text, not JSON. The existing structural adapter
                # covers the JSON boundary; the opt-in test uses the real command.
                output = json.dumps(nft_inventory(live) if "nf_tables" in version else {"nftables": []})
            elif args == ["ip", "-j", "address", "show"]:
                output = json.dumps([{"addr_info": [{"local": claim.socket.address} for claim in claims]}])
            elif args[:2] == ["ip", "-j"]:
                output = "[]"
            else:
                raise AssertionError(args)
            return subprocess.CompletedProcess(args, 0, output, "")
        host.run = command
        return host, claims, live

    def test_captured_enabled_layouts_and_installed_hooks_allow_adoption(self):
        for variant in VARIANTS:
            with self.subTest(variant=variant):
                host, claims, _ = self.load_capture(variant)
                self.assertEqual(host.paths.p("/etc/ufw/before.init").stat().st_mode & 0o777, 0o640)
                self.assertEqual(host.paths.p("/etc/ufw/after.init").stat().st_mode & 0o777, 0o640)
                before = {p: host.paths.p(p).read_bytes() for p in firewall.FILES}
                self.assertTrue(firewall.observe(host, fixture.INSTANCE, claims, integrated=False).closed)
                self.assertEqual(before, {p: host.paths.p(p).read_bytes() for p in firewall.FILES})

    def test_generated_logging_inserts_match_both_saved_family_orders(self):
        for variant in VARIANTS:
            for suffix in ("", "6"):
                with self.subTest(variant=variant, family=suffix):
                    directory = CAPTURES / variant
                    rules, _ = firewall.parse_rules((directory / f"etc-ufw-enabled/user{suffix}.rules").read_text())
                    live, _ = firewall.parse_rules((directory / f"ip{suffix}tables-save.txt").read_text())
                    name = "ufw" + suffix + "-logging-deny"
                    self.assertEqual([firewall._argument(rule, "-j") for rule in rules[name]], ["RETURN", "LOG"])
                    self.assertEqual([firewall._argument(rule, "--ctstate") for rule in rules[name]],
                                     [firewall._argument(rule, "--ctstate") for rule in live[name]])

    def test_projection_preserves_capture_bytes_and_reconstructs_saved_helper_graph(self):
        for variant in VARIANTS:
            host, _, live = self.load_capture(variant)
            for family, suffix in ((4, ""), (6, "6")):
                with self.subTest(variant=variant, family=family):
                    files = {name: host.paths.p(name).read_text() for name in firewall.FILES}
                    settings = firewall._settings(files["/etc/default/ufw"])
                    graph = firewall.reload_graph(files, family, settings)
                    saved, _ = firewall.parse_rules(live[family])
                    targets = lambda chains: {name: [firewall._argument(rule, "-j") for rule in rules]
                                              for name, rules in chains.items()}
                    self.assertEqual(targets(graph), targets(saved))
                    path = f"/etc/ufw/before{suffix}.rules"
                    old = files[path]
                    files[path] = firewall.persistent_projection(old, fixture.INSTANCE, family=family)
                    self.assertEqual(files[path], firewall.persistent_projection(files[path], fixture.INSTANCE, family=family))
                    chain = firewall.chain_name(fixture.INSTANCE)
                    marker = "# Foundry network " + fixture.INSTANCE
                    restored = files[path].replace(marker + " chain\n:" + chain + " - [0:0]\n", "")
                    restored = restored.replace(marker + " jump\n-A ufw" + suffix + "-before-input -j " + chain + "\n", "")
                    self.assertEqual(restored, old)
                    firewall.require_denied(firewall.reload_graph(files, family, settings), ("INPUT", "FORWARD"), (9443,), owned=chain)

    def test_executable_symlink_and_writable_hooks_still_refuse(self):
        host, claims, _ = self.load_capture("24.04")
        for name in ("before.init", "after.init"):
            path = host.paths.p("/etc/ufw/" + name)
            for mode in (0o740, 0o650, 0o641, 0o660):
                with self.subTest(name=name, mode=oct(mode)):
                    path.chmod(mode)
                    with self.assertRaises(c.DeployError):
                        firewall.observe(host, fixture.INSTANCE, claims, integrated=False)
                    path.chmod(0o640)
            original = path.read_bytes()
            path.unlink()
            path.symlink_to(host.paths.p("/etc/ufw/ufw.conf"))
            with self.assertRaises(c.DeployError):
                firewall.observe(host, fixture.INSTANCE, claims, integrated=False)
            path.unlink()
            path.write_bytes(original)
            path.chmod(0o640)

    def test_insert_order_and_foreign_policy_are_not_ignored(self):
        rules = "*filter\n:INPUT DROP [0:0]\n-A INPUT -j DROP\n-I INPUT -j RETURN\n-I INPUT 2 -j LOG\nCOMMIT\n"
        self.assertEqual(firewall.parse_rules(rules)[0]["INPUT"], [("-j", "RETURN"), ("-j", "LOG"), ("-j", "DROP")])
        for position in ("0", "-1", "99"):
            with self.subTest(position=position), self.assertRaises(c.DeployError):
                firewall.parse_rules(rules.replace("INPUT 2", "INPUT " + position))
        for variant in VARIANTS:
            for suffix in ("", "6"):
                host, claims, _ = self.load_capture(variant)
                path = host.paths.p(f"/etc/ufw/user{suffix}.rules")
                path.write_text(path.read_text().replace("COMMIT", f"-I ufw{suffix}-user-input -p tcp --dport 9443 -j ACCEPT\nCOMMIT"))
                with self.subTest(variant=variant, family=suffix), self.assertRaises(c.DeployError) as caught:
                    firewall.observe(host, fixture.INSTANCE, claims, integrated=False)
                self.assertEqual(caught.exception.code, "firewall_foreign_allow")

    def test_captured_nft_text_and_saved_rules_have_same_chain_counts(self):
        for variant in VARIANTS:
            text = (CAPTURES / variant / "nft-ruleset.txt").read_text()
            if variant.endswith("legacy"):
                self.assertEqual(text, "")
                continue
            actual, family, chain = Counter(), None, None
            for line in text.splitlines():
                if line.startswith("table "):
                    family = {"ip": 4, "ip6": 6}[line.split()[1]]
                    self.assertEqual(line.split()[2], "filter")
                elif line.startswith("\tchain "):
                    chain = line.split()[1]
                    actual[family, chain] = 0
                elif line.startswith("\t\t") and not line.strip().startswith("type "):
                    actual[family, chain] += 1
            expected = {}
            for family, suffix in ((4, ""), (6, "6")):
                chains, _ = firewall.parse_rules((CAPTURES / variant / f"ip{suffix}tables-save.txt").read_text())
                expected.update({(family, name): len(rules) for name, rules in chains.items()})
            self.assertEqual(dict(actual), expected)


class NginxEscapeTests(unittest.TestCase):
    def test_regex_locations_preserve_escapes_and_inventory_literal_upstreams(self):
        for quote in ("", "'", '"'):
            pattern = r"\.(gif|jpg|jpeg)$"
            text = "location ~* " + quote + pattern + quote + " { proxy_pass http://127.0.0.1:18001; }"
            with self.subTest(quote=quote):
                self.assertEqual(edge._directives(text)[0][0], ("location", "~*", pattern))
                host = Mock()
                host.run.return_value = subprocess.CompletedProcess([], 0, "# configuration file /etc/nginx/nginx.conf:\nhttp { server { " + text + " } }\n", "")
                self.assertEqual(edge.upstream_references(host), (listeners.SocketIdentity("tcp", 4, "127.0.0.1", 18001),))

    def test_unknown_escapes_are_preserved_and_known_escapes_decoded(self):
        for quote in ("", "'", '"'):
            raw = r"a\.b\;c\{d\}e\ f\#g\\h\n\r\t\'\""
            self.assertEqual(edge._directives("value " + quote + raw + quote + ";")[0][0],
                             ("value", "a\\.b\\;c\\{d\\}e\\ f\\#g\\h\n\r\t'\""))
        with self.assertRaises(c.DeployError):
            edge._directives("value trailing\\")

    def test_nginx_quote_and_comment_boundaries(self):
        self.assertEqual(edge._directives('set $v foo#bar; # comment\nset $w ab"cd;'),
                         [(("set", "$v", "foo#bar"), None), (("set", "$w", 'ab"cd'), None)])
        self.assertEqual(edge._directives('if ($uri ~ "\\.jpg$") { return 404; }')[0][0],
                         ("if", "($uri", "~", r"\.jpg$", ")"))
        for value in ('value "a"suffix;', 'value "a"#comment\n;', 'value "unterminated;'):
            with self.subTest(value=value), self.assertRaises(c.DeployError):
                edge._directives(value)


if __name__ == "__main__":
    unittest.main()
