"""Local contract checks for the opt-in preview; no sockets or host commands."""
import unittest
from unittest.mock import patch
import uuid

import acceptance_network as preview


class PreviewFixtureTests(unittest.TestCase):
    def test_modes_use_valid_contracts_and_distinct_loopback_backends(self):
        for mode in ("none", "edge"):
            with self.subTest(mode=mode), patch.object(preview.Host, "run", side_effect=AssertionError("host action")):
                topology, profile = preview.preview_documents(mode, str(uuid.uuid4()))
                endpoints = preview.listeners.allocate_private(profile.instance_id, (("http", "http"), ("relay", "ingress")), (), ())
                resolution = preview.preview_resolution(topology, profile, endpoints)
                self.assertEqual(preview.c.RuntimeResolution.parse(preview.c.record_document(resolution)), resolution)
                self.assertEqual({e.address for e in resolution.endpoints}, {"127.0.0.1"})
                self.assertEqual(len({e.port for e in resolution.endpoints}), 2)
                self.assertTrue(all(e.port != 8554 for e in resolution.endpoints))
                rendered = preview.edge.stream_projection(profile, resolution, "preview.acceptance.invalid", topology=topology)
                text = rendered["stream-relay.ingress.conf"]
                self.assertEqual(" ssl" in text, mode == "edge")
                self.assertIn("proxy_pass 127.0.0.1:", text)
                self.assertEqual({claim.socket.family for claim in preview.listeners.public_claims(profile)}, {4, 6})
                self.assertEqual(resolution.nginx_sha256, preview.c.canonical_digest(rendered))

    def test_live_scenarios_are_opt_in_and_lane_a_proofs_are_explicitly_pending(self):
        self.assertTrue(preview.NetworkPreviewAcceptance.__unittest_skip__)
        self.assertFalse(getattr(preview.NetworkPreviewAcceptance.test_application_closure_proven_by_lane_a, "__unittest_skip__", False))
        method = preview.NetworkPreviewAcceptance.test_boot_unit_coordination_by_lane_a
        self.assertTrue(method.__unittest_skip__)
        self.assertIn("Lane A", method.__unittest_skip_why__)


if __name__ == "__main__":
    unittest.main()
