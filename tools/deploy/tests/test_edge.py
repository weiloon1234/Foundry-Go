"""Cloudflare nginx policy without running nginx or any services."""
import copy
from datetime import date, timedelta
import json
from pathlib import Path
import sys
import tempfile
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "lib"))
from contracts import DeployError, validate_profile
from edge import cloudflare_ranges, edge_findings
from provision import _template
from test_contracts import fixture

TOOLS = Path(__file__).resolve().parents[1]

class EdgeTests(unittest.TestCase):
    def test_cloudflare_is_mandatory_without_lockdown(self):
        _, profile, entry = fixture()
        for name in ("nginx-http.conf", "nginx-https.conf"):
            text = _template(TOOLS, name, entry)
            self.assertIn("real_ip_header CF-Connecting-IP", text)
            self.assertNotIn("return 444", text)
        for edge in ("none", "cloudflare"):
            profile["edge"] = edge
            with self.assertRaises(DeployError):
                validate_profile(profile)

    def test_real_ip_and_lockdown_share_ranges(self):
        _, profile, entry = fixture()
        profile.update(edge_origin_lockdown=True)
        _, ranges = cloudflare_ranges(TOOLS / "lib")
        for name, servers in (("nginx-http.conf", 1), ("nginx-https.conf", 2)):
            text = _template(TOOLS, name, entry)
            self.assertEqual(text, _template(TOOLS, name, entry))
            self.assertEqual(text.count("return 444"), servers)
            self.assertIn("geo $realip_remote_addr", text)
            for cidr in ranges:
                self.assertEqual(text.count("set_real_ip_from " + cidr + ";"), servers)
                self.assertIn(cidr + " 1;", text)
            self.assertIn("127.0.0.1/32 1;", text)
            self.assertIn("::1/128 1;", text)
            self.assertNotIn("set_real_ip_from 127.0.0.1", text)
            self.assertNotIn("allow ", text)
            self.assertNotIn("deny ", text)
            self.assertNotIn("ssh", text.lower())
            self.assertLess(text.index("return 444"), text.index("location /.well-known/acme-challenge/"))
            acme = text.split("location /.well-known/acme-challenge/ {", 1)[1].split("}", 1)[0]
            self.assertIn("root /var/www/letsencrypt", acme)
            self.assertNotIn("return", acme)
        profile["edge_origin_lockdown"] = False
        text = _template(TOOLS, "nginx-https.conf", entry)
        self.assertIn("real_ip_header CF-Connecting-IP", text)
        self.assertNotIn("geo ", text)
        self.assertNotIn("444", text)

    def test_data_validation_and_warning(self):
        value = json.loads((TOOLS / "lib/cloudflare-ranges.json").read_text())
        with tempfile.TemporaryDirectory() as tmp:
            directory = Path(tmp).resolve()
            path = directory / "cloudflare-ranges.json"
            value["retrieved_at"] = (date.today() - timedelta(days=91)).isoformat()
            path.write_text(json.dumps(value))
            findings = edge_findings({"edge":"cloudflare"}, directory)
            self.assertEqual(findings[-1]["kind"], "warning")
            first = findings[0]["detail"]
            value["ipv4"].append("192.0.2.0/24")
            path.write_text(json.dumps(value))
            self.assertNotEqual(first, edge_findings({"edge":"cloudflare"}, directory)[0]["detail"])
            for bad in ("garbage", "::1/128", "0.0.0.0/0", "192.0.2.1/24"):
                broken = copy.deepcopy(value)
                broken["ipv4"] = [bad]
                path.write_text(json.dumps(broken))
                with self.assertRaises((DeployError, ValueError)):
                    cloudflare_ranges(directory)
