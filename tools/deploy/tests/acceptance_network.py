#!/usr/bin/env python3
"""Opt-in network scenarios for disposable Ubuntu containers.

NetworkPreviewAcceptance prepares UFW and peer networking in a fresh systemd
container and exercises real nginx stream. UFWObservationAcceptance is read-only.
NETWORK_HANDOFF.md contains the image/run commands and lane A proof boundaries.

For the older NetworkAcceptance M6 scenarios, the harness provides
/run/foundry-network-acceptance/{owned,case.json}, active
dual-family UFW (iptables-nft or iptables-legacy), the stream module, an owned SAN certificate and
the feature HTTP gate. case.json maps test method names to frozen
topology/profile/old/candidate documents, entry, permitted_namespace and
denied_namespace. Each case reserves fresh backend addresses. Both namespaces
have independent IPv4/IPv6 routes to the explicit profile addresses. No provider
access or DNS writes occur in any scenario. Package installation belongs to the
container image. Only the preview scenario performs container firewall setup.

FOUNDRY_DISPOSABLE_HOST=1 FOUNDRY_NETWORK_ACCEPTANCE=1 python3 acceptance_network.py
"""
import hashlib
import json
import select
import time
import os
from pathlib import Path
import socket
import subprocess
import sys
import threading
import unittest
import uuid

sys.dont_write_bytecode = True
sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "lib"))
import certificates
import contracts as c
import edge
import firewall
import listeners
import report
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
                    connection.sendall(value if self.label is None else self.label.encode() + b"\n")
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


@unittest.skipUnless(os.environ.get("FOUNDRY_UFW_OBSERVATION_ACCEPTANCE") == "1", "Live UFW observation is opt-in")
class UFWObservationAcceptance(unittest.TestCase):
    """Read-only observation of the enabled UFW layout in a disposable container.

    The command in NETWORK_HANDOFF.md prepares packages, UFW and fixture addresses.
    This scenario requires neither systemd nor the M6 case.json/HTTP/TLS setup.
    """
    def test_enabled_ubuntu_ufw_is_closed_before_adoption(self):
        self.assertEqual(os.environ.get("FOUNDRY_DISPOSABLE_HOST"), "1")
        self.assertEqual(os.geteuid(), 0)
        self.assertTrue(Path("/.dockerenv").is_file())
        host = Host()
        status = host.run(["ufw", "status", "verbose"], timeout=5).stdout
        self.assertIn("Logging: on (low)", status)
        for name in ("before.init", "after.init"):
            self.assertEqual(Path("/etc/ufw", name).stat().st_mode & 0o777, 0o640)
        for family, binary in ((4, "iptables-save"), (6, "ip6tables-save")):
            rules, _ = firewall.parse_rules(host.run([binary], timeout=5).stdout)
            namespace = "ufw6" if family == 6 else "ufw"
            allows = {firewall._argument(rule, "--dport") for rule in rules[namespace + "-user-input"]
                      if firewall._argument(rule, "-j") == "ACCEPT"}
            self.assertEqual(allows, {"22", "80", "443"})
        instance_id = str(uuid.uuid4())
        claims = tuple(listeners.ListenerClaim(instance_id, "relay", "ingress", 1,
                       listeners.SocketIdentity("tcp", family, address, 8554), "nginx")
                       for family, address in ((4, "203.0.113.10"), (6, "2001:db8::10")))
        paths = (*firewall.FILES, *firewall.RELOAD_FILES, "/etc/ufw/before.init", "/etc/ufw/after.init")
        before = {name: (Path(name).read_bytes(), Path(name).stat().st_mode) for name in paths}
        observation = firewall.observe(host, instance_id, claims, integrated=False)
        self.assertTrue(observation.closed)
        self.assertEqual(set(observation.sockets), {claim.socket for claim in claims})
        self.assertEqual(before, {name: (Path(name).read_bytes(), Path(name).stat().st_mode) for name in paths})


@unittest.skipUnless(os.environ.get("FOUNDRY_NETWORK_ACCEPTANCE") == "1", "M6 disposable network harness is opt-in")
class NetworkAcceptance(unittest.TestCase):
    def setUp(self):
        self.assertEqual(os.environ.get("FOUNDRY_DISPOSABLE_HOST"), "1")
        self.assertEqual(os.geteuid(), 0)
        self.assertTrue(Path("/.dockerenv").is_file())
        self.assertEqual(c.check_path(WORK / "owned", "file").read_text(), "disposable network acceptance fixture\n")
        self.case = c.read_json(c.check_path(WORK / "case.json", "file"))[self._testMethodName]
        self.topology = t.parse_topology(self.case["topology"])
        self.profile = t.parse_profile(self.case["profile"], self.topology)
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
                          (self.role,), topology=self.topology, authorize=lambda: True)
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

PREVIEW = WORK / "preview"
PEER = "fdy-preview-peer"
ADDRESSES = ("203.0.113.10", "2001:db8:42::10")
PEER_ADDRESSES = ("203.0.113.20", "2001:db8:42::20")
PREVIEW_PROBE = r'''
import socket, ssl, sys
address, port, mode, hostname, certificate = sys.argv[1:]
raw = socket.create_connection((address, int(port)), timeout=2)
stream = ssl.create_default_context(cafile=certificate).wrap_socket(raw, server_hostname=hostname) if mode == "edge" else raw
with stream:
    stream.settimeout(3)
    print("READY", flush=True)
    with stream.makefile("rb") as reader:
        for line in sys.stdin.buffer:
            stream.sendall(line)
            result = reader.readline(4096)
            if not result:
                sys.exit(3)
            sys.stdout.buffer.write(result)
            sys.stdout.buffer.flush()
'''


def preview_documents(mode, instance_id):
    """Fixture declarations only: no release, admission or lane A implementation."""
    topology = t.parse_topology({"schema": 2, "roles": [
        {"name": "http", "kind": "builtin"},
        {"name": "relay", "kind": "executable", "executable": {"application": "echo"},
         "listeners": {"ingress": {"transport": "tcp", "bind": "public", "tls": mode, "authentication": True}},
         "readiness": {"kind": "tcp", "listener": "ingress"},
         "restart": {"policy": "on-failure", "delay_seconds": 1, "burst": 3},
         "limits": {"memory_bytes": 67108864, "cpu_percent": 100, "tasks": 64},
         "state": {"kind": "disposable"}}],
        "application_executables": {"echo": {"go_package": "./cmd/echo"}}})
    profile = t.parse_profile({"schema": 3, "environment": "acceptance", "runtime_environment": "testing",
        "instance_id": instance_id, "host": {"address": "preview.acceptance.invalid", "port": 22, "host_id": instance_id},
        "domain": "preview.acceptance.invalid", "certbot_email": "ops@acceptance.invalid", "architecture": "amd64",
        "public_env": {}, "retention": {"server_releases": 1, "local_releases": 1, "pre_migration_dumps": 1},
        "edge_origin_lockdown": False, "placement": {"http": 1, "relay": 1},
        "listeners": {"relay.ingress": {"ports": [8554], "source_cidrs": [PEER_ADDRESSES[0] + "/32", PEER_ADDRESSES[1] + "/128"],
            "dns": {"mode": "external", "hostname": "preview.acceptance.invalid", "public_addresses": list(ADDRESSES), "proxied": False}}}}, topology)
    return topology, profile


def preview_resolution(topology, profile, endpoints):
    identity = str(uuid.uuid4())
    digest = c.canonical_digest({"fixture": "network preview", "topology": topology.sha256})
    value = {"schema": 1, "id": identity, "deployment_sha256": digest, "private_revision": 1,
        "roles": ["http", "relay"], "endpoints": [dict(role=e.role, listener=e.listener, address=e.address, port=e.port) for e in endpoints],
        "environments": [{"role": role, "path": f"runtime/{identity}/{role}/app.env", "sha256": digest} for role in ("http", "relay")],
        "bindings_sha256": digest, "readiness_sha256": digest, "nginx_sha256": digest}
    resolution = c.RuntimeResolution.parse(value)
    value["nginx_sha256"] = c.canonical_digest(edge.stream_projection(profile, resolution, "preview.acceptance.invalid", topology=topology))
    return c.RuntimeResolution.parse(value)


@unittest.skipUnless(os.environ.get("FOUNDRY_NETWORK_PREVIEW") == "1", "Live network preview is opt-in")
class NetworkPreviewAcceptance(unittest.TestCase):
    """Real kernel/nginx tests. Fixture authority is not lane A admission proof."""
    @classmethod
    def setUpClass(cls):
        c.require(os.environ.get("FOUNDRY_DISPOSABLE_HOST") == "1" and os.geteuid() == 0
                  and Path("/.dockerenv").is_file() and Path("/run/systemd/private").exists(),
                  "acceptance_forbidden", "Use a fresh privileged systemd test container.")
        c.require(not PREVIEW.exists(), "acceptance_forbidden", "Use a fresh container for each preview run.")
        PREVIEW.mkdir(parents=True, mode=0o700)
        cls.host = Host()
        cls.host.run(["systemctl", "start", "nginx"], timeout=30)
        cls.host.run(["ip", "netns", "add", PEER])
        cls.addClassCleanup(cls.host.run, ["ip", "netns", "delete", PEER])
        cls.host.run(["ip", "link", "add", "fdy-host", "type", "veth", "peer", "name", "fdy-peer"])
        cls.host.run(["ip", "link", "set", "fdy-peer", "netns", PEER])
        cls.host.run(["ip", "link", "set", "fdy-host", "up"])
        cls.host.run(["ip", "-n", PEER, "link", "set", "lo", "up"])
        cls.host.run(["ip", "-n", PEER, "link", "set", "fdy-peer", "up"])
        for index, family in enumerate((4, 6)):
            prefix = "/24" if family == 4 else "/64"
            options = [] if family == 4 else ["nodad"]
            cls.host.run(["ip", "-" + str(family), "addr", "add", ADDRESSES[index] + prefix, "dev", "fdy-host", *options])
            cls.host.run(["ip", "-n", PEER, "-" + str(family), "addr", "add", PEER_ADDRESSES[index] + prefix, "dev", "fdy-peer", *options])
        cls.host.run(["ufw", "default", "deny", "incoming"])
        for port in (22, 80, 443):
            cls.host.run(["ufw", "allow", str(port) + "/tcp"])
        cls.host.run(["ufw", "logging", "low"])
        cls.host.run(["ufw", "--force", "enable"], timeout=30)
        edge.install_stream_include(cls.host, reviewed_nginx_sha256=hashlib.sha256(Path("/etc/nginx/nginx.conf").read_bytes()).hexdigest())
        cls.lineage = "preview.acceptance.invalid"
        cls.certificate = Path(certificates.CONFIG_DIR) / "live" / cls.lineage / "fullchain.pem"
        cls.certificate.parent.mkdir(parents=True, mode=0o700)
        cls.host.run(["openssl", "req", "-x509", "-newkey", "rsa:2048", "-nodes", "-days", "1",
                      "-subj", "/CN=" + cls.lineage, "-addext", "subjectAltName=DNS:" + cls.lineage,
                      "-keyout", str(cls.certificate.parent / "privkey.pem"), "-out", str(cls.certificate)], timeout=30)
        (cls.certificate.parent / "privkey.pem").chmod(0o600)

    def setUp(self):
        self.instance_id = str(uuid.uuid4())
        self.mode = "edge" if self._testMethodName.endswith("tls") else "none"
        self.topology, self.profile = preview_documents(self.mode, self.instance_id)
        self.claims = listeners.public_claims(self.profile)
        self.backends, self.probes = [], []
        self.selected = None
        self.addCleanup(self.cleanup_instance)
        self.http_site = Path("/etc/nginx/conf.d/preview-" + self.instance_id + ".conf")
        self.host.owned_write(self.http_site, edge.gate_http(self.instance_id,
                              "server {\n    listen 127.0.0.1:8080;\n    location / { return 503; }\n}\n"))
        before = firewall.observe(self.host, self.instance_id, self.claims, integrated=False)
        firewall.install_closed(self.host, self.instance_id, self.claims, reviewed_sha256=before.sha256)
        edge.withdraw(self.host, self.instance_id, ("relay",))
        self.old = self.allocate()
        self.candidate = self.allocate(reserved=self.old.endpoints)

    def cleanup_instance(self):
        for process in self.probes:
            if process.poll() is None:
                process.kill()
            process.wait(timeout=5)
            process.stdin.close()
            process.stdout.close()
        for backend in self.backends:
            backend.close()
        firewall.withdraw(self.host, self.instance_id, self.claims)
        self.host.run(["systemctl", "start", "nginx"], timeout=30)
        edge.withdraw(self.host, self.instance_id, ("relay",))
        if hasattr(self, "http_site"):
            self.http_site.unlink(missing_ok=True)
            self.host.run(["nginx", "-t", "-q"])
            self.host.run(["systemctl", "reload", "nginx"])

    def allocate(self, reserved=()):
        references = edge.retain_global_references(self.host)
        reserved_claims = tuple(listeners.ListenerClaim(self.instance_id, e.role, e.listener, 1,
                               listeners.SocketIdentity("tcp", 4, e.address, e.port), "application") for e in reserved)
        endpoints = listeners.allocate_private(self.instance_id, (("http", "http"), ("relay", "ingress")),
                     reserved_claims, (*listeners.observe_sockets(self.host), *(listeners.ObservedSocket(s, ()) for s in references)))
        return preview_resolution(self.topology, self.profile, endpoints)

    def backend(self, resolution):
        endpoint = next(e for e in resolution.endpoints if e.role == "relay")
        backend = EchoBackend(endpoint, None)
        self.backends.append(backend)
        return backend

    def authority(self, resolution):
        # This proves only fixture health, never admission or a start permit.
        endpoint = next(e for e in resolution.endpoints if e.role == "relay")
        with socket.create_connection((endpoint.address, endpoint.port), timeout=2) as stream:
            stream.settimeout(2)
            stream.sendall(b"fixture-health\n")
            with stream.makefile("rb") as reader:
                return reader.readline(256) == b"fixture-health\n"

    def expose(self, resolution):
        closed = firewall.observe(self.host, self.instance_id, self.claims)
        edge.open_runtime(self.host, self.profile, resolution, self.lineage, ("relay",), topology=self.topology,
                          authorize=lambda: self.authority(resolution))
        self.assertTrue({claim.socket for claim in self.claims} <= {s.socket for s in listeners.observe_sockets(self.host)})
        self.reachable(False)  # nginx is now listening; UFW must still deny new TCP.
        opened = firewall.open_runtime(self.host, self.instance_id, self.claims, self.profile.listeners,
                                       closed_sha256=closed.sha256, authorize=lambda: self.authority(resolution))
        self.selected = resolution
        return opened

    def probe_command(self, address, *, tcp_only=False):
        return ["ip", "netns", "exec", PEER, "python3", "-I", "-c", PREVIEW_PROBE,
                address, "8554", "none" if tcp_only else self.mode, self.lineage, str(self.certificate)]

    def reachable(self, expected):
        for address in ADDRESSES:
            response = self.host.run(self.probe_command(address, tcp_only=not expected), input="preview-" + self.instance_id + "\n", timeout=8, check=False)
            if expected:
                self.assertEqual(response.returncode, 0, response.stderr)
                self.assertEqual(response.stdout, "READY\npreview-" + self.instance_id + "\n")
            else:
                # Require connection failure, not a successful TCP session with wrong bytes.
                self.assertNotEqual(response.returncode, 0)
                self.assertNotIn("READY", response.stdout)

    def closed_evidence(self):
        retained_http = self.http_site.read_bytes()
        fw = firewall.withdraw(self.host, self.instance_id, self.claims)
        ng = edge.withdraw(self.host, self.instance_id, ("relay",))
        self.assertEqual(self.http_site.read_bytes(), retained_http)
        old = self.selected or self.old
        candidate = self.candidate if old.id == self.old.id else self.allocate(reserved=old.endpoints)
        evidence = listeners.ingress_evidence(str(uuid.uuid4()), firewall.boot_id(self.host), old, candidate,
                    (c.UnitIdentity(self.instance_id, "relay", 1),),
                    c.canonical_digest({"retained_fixture_http_gate_sha256": hashlib.sha256(retained_http).hexdigest()}),
                    fw, ng, claims=self.claims)
        self.assertEqual(evidence.stage, c.ClosureStage.INGRESS_WITHDRAWN)
        self.assertIsNone(evidence.backend_exit_sha256)
        self.assertIsNone(evidence.old_reference_isolation_sha256)
        self.host.owned_write(PREVIEW / (self._testMethodName + "-ingress.json"), json.dumps(c.record_document(evidence), sort_keys=True) + "\n")
        return evidence

    def opening_case(self):
        self.reachable(False)
        # Closed policy is proved before the echo backend starts.
        self.assertTrue(firewall.observe(self.host, self.instance_id, self.claims).closed)
        self.backend(self.candidate)
        self.assertFalse(self.expose(self.candidate).closed)
        self.reachable(True)
        self.closed_evidence()
        self.reachable(False)

    def test_default_deny_open_and_withdraw_plain(self):
        self.opening_case()

    def test_default_deny_open_and_withdraw_tls(self):
        self.opening_case()

    def test_stale_session_is_ingress_withdrawn_not_application_closed(self):
        old = self.backend(self.old)
        self.expose(self.old)
        process = subprocess.Popen(self.probe_command(ADDRESSES[0]), stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                                   stderr=subprocess.DEVNULL, text=True, bufsize=1)
        self.probes.append(process)
        def read_line():
            self.assertTrue(select.select([process.stdout], [], [], 5)[0], "Established peer stalled")
            return process.stdout.readline()
        self.assertEqual(read_line(), "READY\n")
        process.stdin.write("old-session\n")
        process.stdin.flush()
        self.assertEqual(read_line(), "old-session\n")
        evidence = self.closed_evidence()
        self.reachable(False)
        process.stdin.write("still-old\n")
        process.stdin.flush()
        self.assertEqual(read_line(), "still-old\n")
        references = (*edge.retain_global_references(self.host), *edge.retained_references(self.host, self.instance_id))
        self.assertFalse({(e.address, e.port) for e in self.candidate.endpoints} & {(e.address, e.port) for e in references})
        with self.assertRaises(c.DeployError):
            listeners.complete_closure(evidence, self.candidate, backend_exit_sha256=None,
                                       reserved=self.candidate.endpoints, old_references=references)
        old.close()
        process.wait(timeout=5)
        self.assertNotEqual(process.returncode, 0)
        # Network-only fixture restart; no fabricated application-admission record.
        self.backend(self.candidate)
        self.expose(self.candidate)
        self.reachable(True)

    def test_stale_session_tls(self):
        self.test_stale_session_is_ingress_withdrawn_not_application_closed()

    def test_boot_cleanup_primitives_close_then_reopen(self):
        old = self.backend(self.old)
        self.expose(self.old)
        self.reachable(True)
        boot = firewall.boot_id(self.host)
        self.host.run(["systemctl", "stop", "nginx"], timeout=30)
        old.close()
        self.host.run(["ufw", "reload"], timeout=30)  # Flush runtime allows via normal persistent restoration.
        edge.prepare_boot(self.host, self.instance_id)
        self.assertEqual(list((Path(edge.RUNTIME) / self.instance_id).glob("*.conf")), [])
        self.assertEqual(firewall.boot_id(self.host), boot)  # No fake reboot identity or retired reservations.
        self.assertTrue(edge.retained_references(self.host, self.instance_id))
        self.host.run(["systemctl", "start", "nginx"], timeout=30)
        self.assertTrue(firewall.observe(self.host, self.instance_id, self.claims).closed)
        self.assertTrue(edge.observe_projection(self.host, self.instance_id, ("relay",)).closed)
        self.reachable(False)
        self.backend(self.candidate)
        self.expose(self.candidate)
        self.reachable(True)

    def test_boot_cleanup_tls(self):
        self.test_boot_cleanup_primitives_close_then_reopen()

    def test_report_scopes_owner_and_flags_undeclared_listener(self):
        self.backend(self.candidate)
        self.expose(self.candidate)
        foreign = EchoBackend(c.AllocatedEndpoint("fixture", "unexpected", ADDRESSES[0], 18555), None)
        self.backends.append(foreign)
        registry = {"instances": {self.instance_id: {"profile": self.profile.projection(),
                    "topology": {**self.topology.projection(), "sha256": self.topology.sha256}}}}
        inventory = report.claim_inventory(registry)
        self.assertEqual({claim.instance_id for claim in inventory}, {self.instance_id})
        dns = c.DNSObservation(self.lineage, ADDRESSES, c.now(), "fixture-addresses-not-dns-proof")
        def observe(closure=None):
            return report.declared_network(self.host, self.profile, topology=self.topology, entry={}, resolution=self.candidate,
                                           closure=closure, dns_observations=(dns,), host_claims=inventory)
        value = observe()
        self.assertEqual({v["port"] for v in value["listeners"]}, {8554})
        self.assertTrue(all(v["expected_owner"] == "nginx" for v in value["listeners"]))
        self.assertFalse(any(v["code"] in ("listener_owner_unverified", "nginx_projection_unverified", "declared_listener_absent") for v in value["findings"]))
        self.assertIn(18555, {v["port"] for v in value["unexpected_exposure"]})
        self.assertNotIn(8554, {v["port"] for v in value["unexpected_exposure"]})
        evidence = self.closed_evidence()
        # Wait for nginx's old generation to relinquish the closed listening sockets.
        deadline = time.monotonic() + 5
        while any(s.socket.port == 8554 for s in listeners.observe_sockets(self.host)) and time.monotonic() < deadline:
            time.sleep(0.05)
        closed = observe(evidence)
        missing = [v for v in closed["findings"] if v["code"] == "declared_listener_absent"]
        self.assertEqual({v["instance_id"] for v in missing}, {self.instance_id})
        self.assertEqual(closed["admission"]["recorded_stage"], "ingress_withdrawn")
        self.assertFalse(closed["admission"]["current_proof"])
        self.assertIn({"code": "application_admission_unverified"}, closed["findings"])
        self.host.owned_write(PREVIEW / "report.json", json.dumps(closed, sort_keys=True) + "\n")

    def test_application_closure_proven_by_lane_a(self):
        import network_seam_fixture as seam_fixture
        with seam_fixture.owners() as (admission, _, seam):
            # Only registry setup is a fixture boundary. Every process, cgroup,
            # firewall and nginx observation below runs against the container.
            class FixtureHost(Host):
                def read_registry(self):
                    return admission.read_owned(self, PREVIEW / "registry.json", lambda value: value)
            host = FixtureHost()
            entry, old, candidate, transition, _ = seam_fixture.build(host, instance_id=self.instance_id,
                ports=(self.old.endpoints[0].port, self.candidate.endpoints[0].port))
            host.owned_write(PREVIEW / "registry.json", json.dumps({"instances": {self.instance_id: entry}}), mode=0o600)
            self.http_site.unlink()
            self.http_site = Path("/etc/nginx/sites-available/foundry-" + entry["name"] + ".conf")
            self.assertFalse(self.http_site.exists())
            host.owned_write(self.http_site, listeners._feature_projection(entry, old.topology, old.profile, old.resolution.endpoints)["http"])
            enabled = Path("/etc/nginx/sites-enabled") / self.http_site.name
            self.assertFalse(enabled.exists())
            enabled.symlink_to(self.http_site)
            self.addCleanup(enabled.unlink, missing_ok=True)
            paths = []
            def remove_units():
                for path in paths:
                    host.run(["systemctl", "stop", path.name], timeout=15)
                    path.unlink()
                host.run(["systemctl", "daemon-reload"])
            self.addCleanup(remove_units)
            for role, unit in old.units.items():
                path = Path("/etc/systemd/system") / unit
                self.assertFalse(path.exists())
                endpoint = next(e for e in old.resolution.endpoints if e.role == role)
                host.owned_write(path, "[Service]\nType=simple\nRestart=no\nKillMode=control-group\n"
                    "Environment=FOUNDRY_DISPOSABLE_HOST=1\n"
                    f"ExecStart=/usr/bin/python3 -I {Path(__file__).resolve()} --echo {endpoint.address} {endpoint.port}\n")
                paths.append(path)
            host.run(["systemctl", "daemon-reload"])
            host.run(["systemctl", "start", *old.units.values()], timeout=20)
            for endpoint in old.resolution.endpoints:
                deadline = time.monotonic() + 5
                while True:
                    try:
                        with socket.create_connection((endpoint.address, endpoint.port), timeout=1) as connection:
                            connection.settimeout(1)
                            connection.sendall(b"systemd-echo")
                            self.assertEqual(connection.recv(256), b"systemd-echo")
                        break
                    except OSError:
                        if time.monotonic() >= deadline:
                            raise
                        time.sleep(0.05)
            with self.assertRaises(c.DeployError) as alive:
                seam.close(host, entry, transition, candidate.resolution, candidate.units)
            self.assertEqual(alive.exception.code, "network_backend_alive")
            host.run(["systemctl", "stop", *old.units.values()], timeout=20)
            evidence = seam.close(host, entry, transition, candidate.resolution, candidate.units)
            self.assertEqual(evidence.stage, c.ClosureStage.APPLICATION_CLOSED)
            self.assertEqual(set(evidence.units), set(transition.affected_units))
            self.assertTrue(set(candidate.resolution.endpoints) <= set(evidence.endpoints))
            self.assertIsNotNone(evidence.backend_exit_sha256)
            self.assertIsNotNone(evidence.old_reference_isolation_sha256)
            self.reachable(False)
            host.owned_write(PREVIEW / "adapter-closure.json", json.dumps(c.record_document(evidence), sort_keys=True) + "\n")

    @unittest.skip("Lane A owns the boot coordinator/unit ordering and fresh permits; only lane C cleanup is tested here")
    def test_boot_unit_coordination_by_lane_a(self):
        pass



if __name__ == "__main__":
    if len(sys.argv) == 4 and sys.argv[1] == "--echo":
        import signal
        c.require(os.environ.get("FOUNDRY_DISPOSABLE_HOST") == "1" and os.geteuid() == 0
                  and Path("/.dockerenv").exists(), "acceptance_forbidden", "Echo units require a disposable container.")
        backend = EchoBackend(c.AllocatedEndpoint("fixture", "echo", sys.argv[2], int(sys.argv[3])), None)
        def stop_echo(signum, frame):
            raise SystemExit(0)
        signal.signal(signal.SIGTERM, stop_echo)
        try:
            while True:
                signal.pause()
        finally:
            backend.close()
    else:
        unittest.main()
