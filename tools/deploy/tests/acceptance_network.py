#!/usr/bin/env python3
"""Opt-in M6 network scenarios for a prepared, disposable systemd container.

The harness provides /run/foundry-network-acceptance/{owned,case.json}, active
dual-family UFW/iptables-legacy, the stream module, an owned SAN certificate and
the feature HTTP gate. case.json maps test method names to frozen
topology/profile/old/candidate documents, entry, permitted_namespace and
denied_namespace. Each case reserves fresh backend addresses. Both namespaces
have independent IPv4/IPv6 routes to the explicit profile addresses. No provider
access, DNS writes, global firewall setup or package installation occurs here.

FOUNDRY_DISPOSABLE_HOST=1 FOUNDRY_NETWORK_ACCEPTANCE=1 python3 acceptance_network.py
"""
import json
import os
from pathlib import Path
import socket
import subprocess
import sys
import threading
import unittest

sys.dont_write_bytecode = True
sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "lib"))
import certificates
import contracts as c
import edge
import firewall
import listeners
import topology as t
from host import Host

WORK = Path("/run/foundry-network-acceptance")
PROBE = """
import socket, ssl, sys
context = ssl.create_default_context()
with socket.create_connection((sys.argv[1], int(sys.argv[2])), timeout=2) as raw:
    with context.wrap_socket(raw, server_hostname=sys.argv[3]) as stream:
        stream.settimeout(3)
        for line in sys.stdin:
            stream.sendall(line.encode())
            value = stream.recv(256)
            if not value:
                sys.exit(3)
            print(value.decode().strip(), flush=True)
"""


class EchoBackend:
    def __init__(self, endpoint, label):
        self.label, self.connections = label, []
        self.stop_event = threading.Event()
        self.socket = socket.socket(socket.AF_INET6 if ":" in endpoint.address else socket.AF_INET)
        self.socket.bind((endpoint.address, endpoint.port))
        self.socket.listen(16)
        self.socket.settimeout(0.1)
        self.threads = []
        self.thread = threading.Thread(target=self.accept)
        self.thread.start()

    def accept(self):
        while not self.stop_event.is_set():
            try:
                connection, _ = self.socket.accept()
            except socket.timeout:
                continue
            except OSError:
                break
            self.connections.append(connection)
            thread = threading.Thread(target=self.serve, args=(connection,))
            self.threads.append(thread)
            thread.start()

    def serve(self, connection):
        with connection:
            connection.settimeout(0.1)
            while not self.stop_event.is_set():
                try:
                    value = connection.recv(256)
                    if not value:
                        return
                    connection.sendall(self.label.encode() + b"\n")
                except socket.timeout:
                    continue
                except OSError:
                    return

    def close(self):
        if self.stop_event.is_set():
            return
        self.stop_event.set()
        self.socket.close()
        self.thread.join(5)
        for connection in self.connections:
            try:
                connection.shutdown(socket.SHUT_RDWR)
            except OSError:
                pass
        for thread in self.threads:
            thread.join(5)
        if self.thread.is_alive() or any(t.is_alive() for t in self.threads):
            raise RuntimeError("Test-owned backend did not exit")


@unittest.skipUnless(os.environ.get("FOUNDRY_NETWORK_ACCEPTANCE") == "1", "M6 disposable network harness is opt-in")
class NetworkAcceptance(unittest.TestCase):
    def setUp(self):
        self.assertEqual(os.environ.get("FOUNDRY_DISPOSABLE_HOST"), "1")
        self.assertEqual(os.geteuid(), 0)
        self.assertTrue(Path("/.dockerenv").is_file())
        self.assertEqual(c.check_path(WORK / "owned", "file").read_text(), "disposable network acceptance fixture\n")
        self.case = c.read_json(c.check_path(WORK / "case.json", "file"))[self._testMethodName]
        self.profile = t.parse_profile(self.case["profile"], t.parse_topology(self.case["topology"]))
        self.assertTrue(self.case["profile"]["environment"].startswith("acceptance"))
        self.assertEqual(self.case["profile"]["runtime_environment"], "testing")
        self.assertTrue(all(b.hostname.endswith(".acceptance.invalid") for _, b in self.profile.listeners))
        self.old = c.RuntimeResolution.parse(self.case["old"])
        self.candidate = c.RuntimeResolution.parse(self.case["candidate"])
        self.entry = self.case["entry"]
        self.assertEqual(self.entry["profile"]["instance_id"], self.profile.instance_id)
        self.host = Host()
        self.claims = listeners.public_claims(self.profile)
        self.assertEqual({claim.socket.family for claim in self.claims}, {4, 6})
        self.name, self.binding = self.profile.listeners[0]
        self.role, self.listener = self.name.split(".")
        self.lock = self.host.lock("host")
        self.lock.__enter__()
        self.addCleanup(self.lock.__exit__, None, None, None)
        self.instance_lock = self.host.lock(self.profile.instance_id)
        self.instance_lock.__enter__()
        self.addCleanup(self.instance_lock.__exit__, None, None, None)
        self.addCleanup(self.close_ingress)

    def close_ingress(self):
        firewall.withdraw(self.host, self.profile.instance_id, self.claims)
        edge.withdraw(self.host, self.profile.instance_id, (self.role,))

    def backend(self, resolution, label):
        endpoint = next(e for e in resolution.endpoints if (e.role, e.listener) == (self.role, self.listener))
        result = EchoBackend(endpoint, label)
        self.addCleanup(result.close)
        return result

    def expose(self, resolution):
        closed = firewall.observe(self.host, self.profile.instance_id, self.claims)
        edge.open_runtime(self.host, self.profile, resolution, self.entry["certificate"]["lineage"],
                          (self.role,), authorize=lambda: True)
        return firewall.open_runtime(self.host, self.profile.instance_id, self.claims, self.profile.listeners,
                                     closed_sha256=closed.sha256, authorize=lambda: True)

    def probe(self, address, *, permitted=True):
        namespace = self.case["permitted_namespace" if permitted else "denied_namespace"]
        return subprocess.run(["ip", "netns", "exec", namespace, "python3", "-I", "-c", PROBE,
                               address, str(self.binding.ports[0]), self.binding.hostname],
                              input="ping\n", text=True, capture_output=True, timeout=8)

    def test_dual_family_denial_opening_denied_sources_and_reload_closure(self):
        self.close_ingress()
        self.backend(self.candidate, "candidate")
        for address in self.binding.public_addresses:
            self.assertNotEqual(self.probe(address).returncode, 0)
        self.expose(self.candidate)
        for address in self.binding.public_addresses:
            allowed = self.probe(address)
            self.assertEqual(allowed.returncode, 0, "Permitted independent-vantage TLS probe failed")
            self.assertEqual(allowed.stdout.strip(), "candidate")
            self.assertNotEqual(self.probe(address, permitted=False).returncode, 0)
        self.host.run(["ufw", "reload"])
        self.assertTrue(firewall.observe(self.host, self.profile.instance_id, self.claims).closed)
        for address in self.binding.public_addresses:
            self.assertNotEqual(self.probe(address).returncode, 0)

    def test_old_nginx_session_survives_withdrawal_but_cannot_reach_fresh_backend(self):
        self.close_ingress()
        old = self.backend(self.old, "old")
        self.expose(self.old)
        address = self.binding.public_addresses[0]
        process = subprocess.Popen(["ip", "netns", "exec", self.case["permitted_namespace"], "python3", "-I", "-c", PROBE,
                                    address, str(self.binding.ports[0]), self.binding.hostname],
                                   stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, text=True)
        def cleanup():
            process.kill()
            process.wait(timeout=5)
            process.stdin.close()
            process.stdout.close()
        self.addCleanup(cleanup)
        def exchange():
            import select
            process.stdin.write("ping\n")
            process.stdin.flush()
            self.assertTrue(select.select([process.stdout], [], [], 5)[0], "Old-session probe timed out")
            return process.stdout.readline().strip()
        self.assertEqual(exchange(), "old")
        self.close_ingress()
        # Graceful reload did not prove application admission closed.
        self.assertEqual(exchange(), "old")
        references = edge.retained_references(self.host, self.profile.instance_id)
        candidate = [e for e in self.candidate.endpoints if e.role == self.role]
        self.assertFalse({(e.address, e.port) for e in candidate} & {(e.address, e.port) for e in references})
        old.close()
        self.backend(self.candidate, "candidate")
        self.expose(self.candidate)
        self.assertEqual(self.probe(address).stdout.strip(), "candidate")
        # An old peer has ended, never resumed against the candidate allocation.
        try:
            process.stdin.write("ping\n")
            process.stdin.flush()
        except BrokenPipeError:
            pass
        process.wait(timeout=5)
        self.assertNotEqual(process.returncode, 0)

    def test_effective_synthetic_certificate_renewal_and_closed_projection(self):
        self.assertEqual(os.environ.get("FOUNDRY_ACCEPTANCE_SYNTHETIC_CERT"), "1")
        self.close_ingress()
        self.backend(self.candidate, "candidate")
        self.expose(self.candidate)
        certificates._synthetic_issue(self.host, self.entry)
        self.assertEqual(certificates.verify_network_consumers(self.host, self.entry)["state"], "effective")
        self.close_ingress()
        certificates._synthetic_issue(self.host, self.entry)
        for address in self.binding.public_addresses:
            self.assertNotEqual(self.probe(address).returncode, 0)


if __name__ == "__main__":
    unittest.main()
