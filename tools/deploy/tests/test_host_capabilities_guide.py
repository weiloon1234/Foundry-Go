"""Keep the consumer's complete declarations accepted by the shipped parsers."""
import json
from pathlib import Path
import re
import sys
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "lib"))
from topology import parse_profile, parse_topology


class HostCapabilitiesGuideTests(unittest.TestCase):
    def test_worked_example_has_placeable_roles_and_private_callback_bindings(self):
        guide = Path(__file__).resolve().parents[3] / "docs/guides/host-capabilities.md"
        documents = [json.loads(value) for value in re.findall(r"```json\n(.*?)\n```", guide.read_text(), re.S)]
        topology_document, profile_document = documents
        topology = parse_topology(topology_document)
        profile = parse_profile(profile_document, topology)
        self.assertEqual(profile.projection()["placement"], {"http": 1, "relay": 1})
        roles = {role["name"]: role for role in topology.projection()["roles"]}
        self.assertEqual(roles["http"]["requires"], ["relay"])
        self.assertEqual(roles["http"]["packages"], ["jq"])
        self.assertEqual(roles["http"]["bindings"]["json"],
                         {"executable": {"package": "jq", "path": "/usr/bin/jq"}})
        self.assertEqual(roles["relay"]["requires"], [])
        self.assertEqual(roles["relay"]["bindings"]["authorization"]["endpoint"],
                         {"role": "http", "listener": "http", "scheme": "http"})
        self.assertEqual(roles["relay"]["listeners"]["control"], {"transport": "tcp", "bind": "loopback"})
        self.assertEqual(profile.projection()["listeners"]["relay.ingress"]["ports"], [8554])


if __name__ == "__main__":
    unittest.main()
