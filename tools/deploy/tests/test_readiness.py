"""Concurrent readiness observations and cancellation of owned blocking I/O."""
from contextlib import ExitStack
from dataclasses import replace
from pathlib import Path
import sys
import tempfile
import threading
import time
from types import SimpleNamespace
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "lib"))

import contracts as c
from host import Host, Paths
import release
import services
from test_copies import numbered_inventory


class ReadinessTests(unittest.TestCase):
    def fixture(self, count=1):
        stack = self.enterContext(ExitStack())
        root = stack.enter_context(tempfile.TemporaryDirectory())
        host = Host(Paths(root))
        inventory, _, _ = numbered_inventory(count)
        inventory.resolution = replace(inventory.resolution, endpoints=())
        for name, spec in inventory.specs.items():
            path = host.paths.p(spec.executable)
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(b"native")
            path.chmod(0o755)
            inventory.specs[name] = replace(spec, executable_sha256=c.digest(path))
        started = time.monotonic()
        observation = {"ActiveState": "active", "NRestarts": "0", "MainPID": "20",
                       "process": {"start": "42"}, "ExecMainStartTimestampMonotonic": str(int(started * 1_000_000))}
        stack.enter_context(patch.object(host, "read_registry", return_value={"instances": {}}))
        stack.enter_context(patch.object(release, "service", return_value=observation))
        stack.enter_context(patch.object(release, "_matches_executable", return_value=True))
        return host, inventory, started, observation

    def test_twelve_second_probe_does_not_expire_ten_second_peer(self):
        host, inventory, started, _ = self.fixture()
        names = inventory.select(roles=("http", "relay"))
        fast_observations = []
        def probe(host, inventory, identity, deadline):
            if identity.role == "relay":
                time.sleep(12)
            else:
                fast_observations.append(time.monotonic() - started)
            return True
        def contract(role):
            return SimpleNamespace(timeout_seconds=10 if role.common.name == "http" else 30, listener="unused")
        with patch.object(services, "probe", side_effect=probe), \
                patch.object(services, "readiness_probe", side_effect=contract), \
                patch.object(release, "STABILITY_SECONDS", 5):
            self.assertTrue(services.wait_ready_set(host, inventory, names, started + 35, starting=True))
        self.assertTrue(any(5 <= elapsed < 10 for elapsed in fast_observations))

    def test_128_sleeping_probes_are_fair_and_bounded(self):
        host, inventory, started, _ = self.fixture(128)
        counts, active, peak = {}, 0, 0
        lock = threading.Lock()
        def probe(host, inventory, identity, deadline):
            nonlocal active, peak
            with lock:
                active += 1
                peak = max(peak, active)
                counts[identity] = counts.get(identity, 0) + 1
            try:
                time.sleep(.02)
                return True
            finally:
                with lock:
                    active -= 1
        with patch.object(services, "probe", side_effect=probe), \
                patch.object(services, "readiness_probe", return_value=None), \
                patch.object(release, "STABILITY_SECONDS", .2):
            self.assertTrue(services.wait_ready_set(host, inventory, inventory.units, started + 5, starting=True))
        self.assertEqual(set(counts), set(inventory.units))
        self.assertTrue(all(count >= 2 for count in counts.values()))
        self.assertGreater(peak, 1)
        self.assertLessEqual(peak, 16)
        self.assertEqual(active, 0)
        self.assertFalse(any(t.name.startswith("foundry-ready") for t in threading.enumerate()))

    def test_failure_and_deadline_cancel_and_reap_probe_commands(self):
        import os
        for cause in ("peer_failure", "overall_deadline", "unit_deadline"):
            with self.subTest(cause=cause):
                host, inventory, started, observation = self.fixture()
                pid_file = host.paths.root / "probe.pid"
                def inspect(host, unit, **kwargs):
                    if cause == "peer_failure" and unit == inventory.units[inventory.identity("http")] and pid_file.exists():
                        return {**observation, "ActiveState": "failed"}
                    return observation
                def probe(host, inventory, identity, deadline):
                    if identity.role == "relay":
                        host.run([sys.executable, "-c",
                            "import os, pathlib, time; pathlib.Path(" + repr(str(pid_file)) + ").write_text(str(os.getpid())); time.sleep(30)"],
                            timeout=30)
                    return cause != "unit_deadline"
                def contract(role):
                    return SimpleNamespace(timeout_seconds=.3 if cause == "unit_deadline" and role.common.name == "http" else 30,
                                           listener="unused")
                import subprocess
                processes = []
                spawn = subprocess.Popen
                def start(*args, **kwargs):
                    process = spawn(*args, **kwargs)
                    processes.append(process)
                    return process
                with patch.object(subprocess, "Popen", side_effect=start), \
                        patch.object(release, "service", side_effect=inspect), \
                        patch.object(services, "probe", side_effect=probe), \
                        patch.object(services, "readiness_probe", side_effect=contract), \
                        patch.object(release, "STABILITY_SECONDS", .05):
                    self.assertFalse(services.wait_ready_set(host, inventory, inventory.select(roles=("http", "relay")),
                                     started + (.5 if cause == "overall_deadline" else 5), starting=True))
                self.assertEqual(len(processes), 1)
                process = processes[0]
                self.assertIsNotNone(process.returncode)
                self.assertTrue(process.stdout.closed)
                self.assertTrue(process.stderr.closed)
                self.assertTrue(pid_file.exists())
                with self.assertRaises(ProcessLookupError):
                    os.kill(int(pid_file.read_text()), 0)
                self.assertLess(time.monotonic() - started, 2)
                self.assertFalse(any(t.name.startswith("foundry-ready") for t in threading.enumerate()))

    def test_deadline_closes_a_blocked_http_probe(self):
        import socket
        import topology
        for response in (b"", b"HTTP/1.1 200 OK\r\nContent-Length: 1000\r\nConnection: close\r\n\r\nx"):
            with self.subTest(waiting_for="headers" if not response else "body"):
                host, inventory, started, _ = self.fixture()
                listener = socket.socket()
                self.addCleanup(listener.close)
                listener.bind(("127.0.0.1", 0))
                listener.listen()
                accepted, finish, closed = threading.Event(), threading.Event(), threading.Event()
                def serve():
                    listener.settimeout(2)
                    connection, _ = listener.accept()
                    with connection:
                        connection.recv(8192)
                        if response:
                            connection.sendall(response)
                        accepted.set()
                        connection.settimeout(2)
                        # Hold the HTTP response indefinitely; cancellation must close
                        # this connection rather than waiting for the 30-second probe.
                        if connection.recv(1) == b"":
                            closed.set()
                        finish.wait(2)
                server = threading.Thread(target=serve)
                server.start()
                identity = inventory.identity("relay")
                inventory.roles["relay"] = replace(inventory.roles["relay"], readiness=topology.Probe("http", "control", 30, "/", 200, None))
                port = listener.getsockname()[1]
                inventory.resolution = replace(inventory.resolution, endpoints=(c.AllocatedEndpoint("relay", "control", "127.0.0.1", port, 1),))
                from subprocess import CompletedProcess
                try:
                    with patch.object(host, "run", return_value=CompletedProcess([], 0, f'LISTEN 0 5 127.0.0.1:{port} *:* users:(("role",pid=20,fd=5))\n', "")):
                        self.assertFalse(services.wait_ready_set(host, inventory, (identity,), started + .5, starting=True))
                    self.assertTrue(accepted.is_set())
                    self.assertTrue(closed.wait(1))
                    self.assertLess(time.monotonic() - started, 2)
                    self.assertFalse(any(t.name.startswith("foundry-ready") for t in threading.enumerate()))
                finally:
                    finish.set()
                    server.join(3)
                    self.assertFalse(server.is_alive())
