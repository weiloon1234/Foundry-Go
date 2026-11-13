"""Local contract checks for the opt-in preview; no sockets or host commands."""
import io
import socket
import ssl
import threading
from types import SimpleNamespace
import unittest
from unittest.mock import MagicMock, Mock, patch
import uuid

import acceptance_network as preview


class PreviewFixtureTests(unittest.TestCase):
    def test_peer_probe_checks_terminated_session_after_final_input(self):
        for mode in ("none", "edge"):
            for failure in (None, BrokenPipeError(), ConnectionResetError(), ssl.SSLEOFError()):
                with self.subTest(mode=mode, failure=type(failure).__name__):
                    stream = MagicMock()
                    stream.makefile.return_value.__enter__.return_value.readline.return_value = b""
                    stream.sendall.side_effect = failure
                    context = Mock()
                    context.wrap_socket.return_value = stream
                    with patch.object(socket, "create_connection", return_value=stream), \
                            patch.object(ssl, "create_default_context", return_value=context), \
                            patch.object(preview.sys, "argv", ["probe", "127.0.0.1", "8554", mode, "fixture.invalid", "fixture.pem"]), \
                            patch.object(preview.sys, "stdin", SimpleNamespace(buffer=io.BytesIO(b"after-backend-exit\n"))), \
                            patch.object(preview.sys, "stdout", io.StringIO()), self.assertRaises(SystemExit) as exited:
                        exec(preview.PREVIEW_PROBE, {})
                    self.assertEqual(exited.exception.code, 3)
                    stream.sendall.assert_called_once_with(b"after-backend-exit\n")

    def test_echo_close_shuts_down_accepted_connections_and_joins_workers(self):
        backend = preview.EchoBackend.__new__(preview.EchoBackend)
        backend.stop_event = threading.Event()
        backend.socket, backend.thread = Mock(), Mock()
        backend.thread.is_alive.return_value = False
        backend.connections = [Mock(), Mock()]
        backend.threads = [Mock(), Mock()]
        for worker in backend.threads:
            worker.is_alive.return_value = False
        backend.close()
        backend.close()
        backend.socket.close.assert_called_once()
        for connection, worker in zip(backend.connections, backend.threads):
            connection.shutdown.assert_called_once_with(socket.SHUT_RDWR)
            worker.join.assert_called_once_with(5)

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
