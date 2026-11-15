"""Opt-in Cloudflare test. Owns random names beneath foundry-dns-test only."""
from dataclasses import replace
import os
from pathlib import Path
import sys
import tempfile
import unittest
import uuid

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "lib"))
import certificates
import contracts as c
import dns
from host import Host, Paths
import topology
from test_dns import managed_profile


@unittest.skipUnless(os.environ.get("FOUNDRY_TEST_CLOUDFLARE_REQUIRED") == "1", "Cloudflare live test is opt-in")
class CloudflareLiveTests(unittest.TestCase):
    def test_random_owned_record_lifecycle(self):
        zone = dns.provider_id(os.environ.get("FOUNDRY_TEST_CLOUDFLARE_ZONE_ID"))
        token = os.environ.get("FOUNDRY_TEST_CLOUDFLARE_API_TOKEN")
        certificates._validate_token(token)
        domain = c.dns_hostname(os.environ.get("FOUNDRY_TEST_CLOUDFLARE_DOMAIN"))
        name = uuid.uuid4().hex + ".foundry-dns-test." + domain
        raw = managed_profile().projection()
        raw["domain"] = domain
        value = raw["listeners"]["relay.ingress"]["dns"]
        value.update(hostname=name, zone_id=zone)
        value["records"][0]["ownership_id"] = str(uuid.uuid4())
        from test_host_capabilities import declaration
        profile = topology.parse_profile(raw, topology.parse_topology(declaration()))
        with tempfile.TemporaryDirectory(prefix="foundry-dns-live-") as temporary:
            host = Host(Paths(Path(temporary)))
            entry = {"name": "dns-live", "profile": raw, "certificate": {"zone_ref": zone}}
            host.paths.instance_config(entry["name"]).mkdir(parents=True, mode=0o700)
            directory = host.paths.p("/etc/foundry/cloudflare/zones")
            directory.mkdir(parents=True, mode=0o700)
            host.owned_write(directory / (zone + ".ini"), "dns_cloudflare_api_token = " + token + "\n", mode=0o600)
            try:
                operation = str(uuid.uuid4())
                planned = dns.plan(host, entry, profile, operation)
                dns.apply(host, entry, planned, operation)
                binding = profile.listeners[0][1]
                updated = replace(binding, public_addresses=("203.0.113.20",),
                                  records=(replace(binding.records[0], address="203.0.113.20"),))
                operation = str(uuid.uuid4())
                planned = dns.plan(host, entry, replace(profile, listeners=(("relay.ingress", updated),)), operation)
                dns.apply(host, entry, planned, operation)
                self.assertEqual(dns.report(host, entry, replace(profile, listeners=(("relay.ingress", updated),)))[0]["state"], "verified")
            finally:
                # Read unknown writes back first; never delete a record without its
                # durable creation receipt and exact random-name ownership comment.
                state = dns.load(host, entry)
                for operation in state["operations"]:
                    dns.reconcile(host, entry, state, operation)
                client = dns.Cloudflare(token)
                for owned in list(state["owned"].values()):
                    change = dns.Change.parse(owned)
                    current = client.get(zone, change.prior.record_id)
                    if current is None:
                        continue
                    self.assertEqual(current.value.name, name)
                    self.assertEqual(current.value.comment, change.comment)
                    client.write(replace(change, prior=current, desired=None))
                    self.assertIsNone(client.get(zone, current.record_id))
