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
from host import Host, Paths, current_observation
import host as host_module
from timing_support import Checkpoints, ManualClock, readiness_clock
import release
import services
from test_copies import numbered_inventory


class ReadinessTests(unittest.TestCase):
    def fixture(self, count=1, clock=time):
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
        started = clock.monotonic()
        observation = {"ActiveState": "active", "NRestarts": "0", "MainPID": "20",
                       "process": {"start": "42"}, "ExecMainStartTimestampMonotonic": str(int(started * 1_000_000))}
        stack.enter_context(patch.object(host, "read_registry", return_value={"instances": {}}))
        stack.enter_context(patch.object(release, "service", return_value=observation))
        stack.enter_context(patch.object(release, "_matches_executable", return_value=True))
        return host, inventory, started, observation

    def slow_peer_scenario(self, count):
        clock, checkpoints = ManualClock(), Checkpoints()
        host, inventory, started, observation = self.fixture(count, clock)
        relay = inventory.roles["relay"]
        inventory.roles["slow"] = replace(relay, common=replace(relay.common, name="slow"))
        slow_count = 16 if count == 128 else 1
        def rename(identity):
            if identity.role != "relay":
                return identity
            return c.UnitIdentity(identity.instance_id, "slow" if identity.ordinal <= slow_count else "relay",
                                  identity.ordinal if identity.ordinal <= slow_count else identity.ordinal - slow_count)
        inventory.units = {rename(name): unit for name, unit in inventory.units.items()}
        inventory.specs = {rename(name): replace(spec, identity=rename(name)) for name, spec in inventory.specs.items()}
        names = inventory.select(roles=("slow", "relay") if count == 128 else ("http", "slow"))
        slow = tuple(name for name in names if name.role == "slow")
        fast = tuple(name for name in names if name.role != "slow")
        calls, first, proved = {}, {}, {}
        active_commands, peak_commands = 0, 0
        lock = threading.Lock()
        inspection_wave = threading.Barrier(min(len(names), services.READINESS_INSPECTIONS))
        admitted = 0
        def spawn(*args, **kwargs):
            from unittest.mock import Mock
            nonlocal active_commands, peak_commands, admitted
            with lock:
                admitted += 1
                synchronize = admitted <= inspection_wave.parties
                active_commands += 1
                peak_commands = max(peak_commands, active_commands)
            def communicate(**kwargs):
                nonlocal active_commands
                try:
                    if synchronize:
                        inspection_wave.wait(10)
                    return "", ""
                finally:
                    with lock:
                        active_commands -= 1
            return Mock(returncode=0, communicate=communicate)
        def inspect(host, unit, **kwargs):
            host.run(["systemctl", "show", unit], timeout=5)
            return observation
        def probe(host, inventory, identity, deadline):
            calls[identity] = attempt = calls.get(identity, 0) + 1
            first.setdefault(identity, clock.monotonic() - started)
            if attempt == 2:
                self.assertEqual(current_observation().deadline, started + (30 if identity in slow else 10),
                                 "The first observation must not complete the stability proof")
            if identity in slow and attempt <= 2:
                checkpoints.hold((identity, attempt))
            elif identity in fast and attempt in (2, 3):
                if attempt == 3:
                    # A third observation must retain the peer beyond its original
                    # ten-second deadline only after its five-second proof.
                    proved[identity] = (clock.monotonic() - started, current_observation().deadline)
                checkpoints.hold((identity, attempt))
            return True
        def scenario():
            checkpoints.wait(*((name, 1) for name in slow), *((name, 2) for name in fast))
            self.assertEqual(set(first), set(names))
            self.assertEqual(set(first.values()), {0})
            clock.advance(6)
            checkpoints.release(*((name, 2) for name in fast))
            checkpoints.wait(*((name, 3) for name in fast))
            self.assertEqual(proved, {name: (6, started + 35) for name in fast})
            clock.advance(6)
            checkpoints.release(*((name, 1) for name in slow))
            checkpoints.wait(*((name, 2) for name in slow))
            clock.advance(6)
            # Subsequent health checks return immediately. Repeating the artificial
            # twelve-second stall can start a third probe before the group drains.
        def contract(role):
            return SimpleNamespace(timeout_seconds=30 if role.common.name == "slow" else 10, listener="unused")
        with patch.object(services, "time", clock), patch.object(host_module, "time", clock), \
                patch("host.subprocess.Popen", side_effect=spawn), patch("host.os.killpg"), \
                patch.object(release, "service", side_effect=inspect), \
                patch.object(services, "probe", side_effect=probe), \
                patch.object(services, "readiness_probe", side_effect=contract), \
                patch.object(release, "STABILITY_SECONDS", 5), checkpoints.drive(clock, scenario):
            self.assertTrue(services.wait_ready_set(host, inventory, names, started + 35, starting=True))
        self.assertEqual(len(proved), len(fast))
        self.assertEqual(peak_commands, min(len(names), services.READINESS_INSPECTIONS))
        self.assertEqual(active_commands, 0)

    def test_twelve_second_probe_does_not_expire_ten_second_peer(self):
        self.slow_peer_scenario(1)

    def test_sixteen_slow_probes_cannot_queue_112_short_deadlines(self):
        self.slow_peer_scenario(128)

    def test_specific_errors_survive_false_peer_and_follow_inventory_order(self):
        from host import current_observation
        clock = self.enterContext(readiness_clock(ManualClock()))
        host, inventory, started, observation = self.fixture(clock=clock)
        entered = threading.Barrier(3)
        completed = []
        def inspect(host, unit, **kwargs):
            entered.wait(10)
            if unit == inventory.units[inventory.identity("http")]:
                return {**observation, "ActiveState": "failed"}
            # Errors that finish during cancellation must still be collected.
            self.assertTrue(current_observation().cancelled.wait(10))
            identity = next(name for name, value in inventory.units.items() if value == unit)
            completed.append(identity.role)
            raise c.DeployError("confinement" if identity.role == "relay" else "inspection", identity.role + " proof failed")
        with patch.object(release, "service", side_effect=inspect), self.assertRaises(c.DeployError) as caught:
            services.wait_ready_set(host, inventory, reversed(inventory.units), started + 3, starting=True)
        self.assertEqual(set(completed), {"relay", "worker"})
        self.assertEqual(caught.exception.code, "confinement")
        self.assertIn("relay proof failed", caught.exception.detail)
        self.assertIn("worker proof failed", caught.exception.detail)
        self.assertLess(caught.exception.detail.index("relay"), caught.exception.detail.index("worker"))
        self.assertEqual([error.code for error in caught.exception.__cause__.exceptions], ["confinement", "inspection"])

    def test_128_sleeping_probes_are_fair_and_bounded(self):
        clock, checkpoints = ManualClock(), Checkpoints()
        host, inventory, started, _ = self.fixture(128, clock)
        import socket
        counts, active, peak, threads = {}, 0, 0, 0
        sockets = []
        lock = threading.Lock()
        def probe(host, inventory, identity, deadline):
            nonlocal active, peak, threads
            sock = socket.socket()
            with lock:
                sockets.append(sock)
                threads = max(threads, sum(t.name.startswith("foundry-ready") for t in threading.enumerate()))
                active += 1
                peak = max(peak, active)
                counts[identity] = attempt = counts.get(identity, 0) + 1
            try:
                if attempt <= 2:
                    checkpoints.hold((identity, attempt))
                return True
            finally:
                sock.close()
                with lock:
                    active -= 1
        def scenario():
            checkpoints.wait(*((name, 1) for name in inventory.units))
            checkpoints.release(*((name, 1) for name in inventory.units))
            checkpoints.wait(*((name, 2) for name in inventory.units))
            clock.advance(5)
        with patch.object(services, "time", clock), patch.object(host_module, "time", clock), \
                patch.object(services, "probe", side_effect=probe), \
                patch.object(services, "readiness_probe", return_value=None), \
                patch.object(release, "STABILITY_SECONDS", 5), checkpoints.drive(clock, scenario):
            self.assertTrue(services.wait_ready_set(host, inventory, inventory.units, started + 30, starting=True))
        self.assertEqual(set(counts), set(inventory.units))
        self.assertTrue(all(count >= 2 for count in counts.values()))
        self.assertEqual(peak, len(inventory.units))
        self.assertLessEqual(threads, len(inventory.units))
        self.assertLessEqual(threads, c.MAX_UNITS)
        self.assertTrue(all(sock.fileno() == -1 for sock in sockets))
        self.assertEqual(active, 0)
        self.assertFalse(any(t.name.startswith("foundry-ready") for t in threading.enumerate()))

    def test_selection_rejects_duplicates_and_more_than_the_unit_cap_before_spawning(self):
        host, inventory, started, _ = self.fixture()
        identity = inventory.identity("relay")
        for names in ((identity, identity), (identity,) * (c.MAX_UNITS + 1)):
            with self.subTest(count=len(names)), patch.object(services, "ThreadPoolExecutor") as pool:
                with self.assertRaises(c.DeployError):
                    services.wait_ready_set(host, inventory, names, started + 5)
                pool.assert_not_called()

    def test_failure_and_deadline_cancel_and_reap_probe_commands(self):
        import os
        for cause in ("peer_failure", "overall_deadline", "unit_deadline"):
            with self.subTest(cause=cause):
                clock = self.enterContext(readiness_clock(ManualClock()))
                checkpoints, fail_peer = Checkpoints(), threading.Event()
                returned = threading.Event()
                host, inventory, started, observation = self.fixture(clock=clock)
                pid_file = host.paths.root / "probe.pid"
                def inspect(host, unit, **kwargs):
                    if cause == "peer_failure" and unit == inventory.units[inventory.identity("http")] and fail_peer.is_set():
                        return {**observation, "ActiveState": "failed"}
                    return observation
                def probe(host, inventory, identity, deadline):
                    if identity.role == "relay":
                        host.run([sys.executable, "-c",
                            "import os, pathlib, signal; pathlib.Path(" + repr(str(pid_file)) + ").write_text(str(os.getpid())); print('ready', flush=True); signal.pause()"],
                            timeout=300)
                    return cause != "unit_deadline"
                def contract(role):
                    return SimpleNamespace(timeout_seconds=3 if cause == "unit_deadline" and role.common.name == "http" else 300,
                                           listener="unused")
                import subprocess
                processes = []
                spawn = subprocess.Popen
                def start(*args, **kwargs):
                    import select
                    process = spawn(*args, **kwargs)
                    processes.append(process)
                    self.assertTrue(select.select([process.stdout], [], [], 10)[0], 'Probe child did not start')
                    self.assertEqual(process.stdout.readline(), 'ready\n')
                    communicate = process.communicate
                    def blocked(**kwargs):
                        checkpoints.hold('communicating')
                        try:
                            return communicate(**kwargs)
                        finally:
                            returned.set()
                    process.communicate = blocked
                    return process
                def cancel():
                    checkpoints.wait('communicating')
                    self.assertTrue(pid_file.exists())
                    checkpoints.release('communicating')
                    if cause == 'peer_failure':
                        fail_peer.set()
                    else:
                        clock.advance(31 if cause == 'overall_deadline' else 4)
                    self.assertTrue(returned.wait(10), 'Cancellation waited for the native process timeout')
                with patch.object(subprocess, "Popen", side_effect=start), \
                        patch.object(release, "service", side_effect=inspect), \
                        patch.object(services, "probe", side_effect=probe), \
                        patch.object(services, "readiness_probe", side_effect=contract), \
                        patch.object(release, "STABILITY_SECONDS", 0), checkpoints.drive(clock, cancel):
                    self.assertFalse(services.wait_ready_set(host, inventory, inventory.select(roles=("http", "relay")),
                                     started + (30 if cause == "overall_deadline" else 300), starting=True))
                self.assertEqual(len(processes), 1)
                process = processes[0]
                self.assertIsNotNone(process.returncode)
                self.assertTrue(process.stdout.closed)
                self.assertTrue(process.stderr.closed)
                self.assertTrue(pid_file.exists())
                with self.assertRaises(ProcessLookupError):
                    os.kill(int(pid_file.read_text()), 0)
                self.assertFalse(any(t.name.startswith("foundry-ready") for t in threading.enumerate()))

    def test_deadline_closes_a_blocked_http_probe(self):
        import socket
        import topology
        for response in (b"", b"HTTP/1.1 200 OK\r\nContent-Length: 1000\r\nConnection: close\r\n\r\nx"):
            with self.subTest(waiting_for="headers" if not response else "body"):
                clock = self.enterContext(readiness_clock(ManualClock()))
                host, inventory, started, _ = self.fixture(clock=clock)
                listener = socket.socket()
                self.addCleanup(listener.close)
                listener.bind(("127.0.0.1", 0))
                listener.listen()
                accepted, finish, closed = threading.Event(), threading.Event(), threading.Event()
                def serve():
                    listener.settimeout(10)
                    connection, _ = listener.accept()
                    with connection:
                        connection.recv(8192)
                        if response:
                            connection.sendall(response)
                        accepted.set()
                        connection.settimeout(10)
                        # Hold the HTTP response indefinitely; cancellation must close
                        # this connection rather than waiting for the 30-second probe.
                        if connection.recv(1) == b"":
                            closed.set()
                        finish.wait(10)
                server = threading.Thread(target=serve)
                server.start()
                identity = inventory.identity("relay")
                inventory.roles["relay"] = replace(inventory.roles["relay"], readiness=topology.Probe("http", "control", 30, "/", 200, None))
                port = listener.getsockname()[1]
                inventory.resolution = replace(inventory.resolution, endpoints=(c.AllocatedEndpoint("relay", "control", "127.0.0.1", port, 1),))
                from subprocess import CompletedProcess
                def expire():
                    self.assertTrue(accepted.wait(10), 'HTTP probe did not connect')
                    clock.advance(31)
                try:
                    with patch.object(host, "run", return_value=CompletedProcess([], 0, f'LISTEN 0 5 127.0.0.1:{port} *:* users:(("role",pid=20,fd=5))\n', "")), \
                            Checkpoints().drive(clock, expire):
                        self.assertFalse(services.wait_ready_set(host, inventory, (identity,), started + 30, starting=True))
                    self.assertTrue(accepted.is_set())
                    self.assertTrue(closed.wait(10))
                    self.assertFalse(any(t.name.startswith("foundry-ready") for t in threading.enumerate()))
                finally:
                    finish.set()
                    server.join(10)
                    self.assertFalse(server.is_alive())
