"""Host command cancellation owns descendants as well as the immediate child."""
import io
import json
import tempfile
from pathlib import Path
import signal
import socket
import subprocess
import sys
import unittest
from unittest.mock import Mock, patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "lib"))
from contracts import DeployError
from host import Host
import host as host_module


class DNSResolverTests(unittest.TestCase):
    def resolver(self, body, timeout=1):
        run = subprocess.run
        def child(args, **kwargs):
            args = list(args)
            args[3] = 'import socket\ndef lookup(*args, **kwargs):\n' + body + '\nsocket.getaddrinfo = lookup\n' + args[3]
            return run(args, **kwargs)
        with patch.object(subprocess, 'run', side_effect=child):
            return host_module.resolve_addresses('never-query.example.invalid', timeout)

    def test_child_reports_resolution_errors_without_raw_resolver_output(self):
        for error in ('socket.gaierror(-2, "missing")', 'OSError("private resolver diagnostic")'):
            with self.subTest(error=error), self.assertRaises(socket.gaierror) as raised:
                self.resolver('    raise ' + error)
            self.assertEqual(str(raised.exception), 'DNS resolution failed')

    def test_child_returns_bounded_resolved_addresses(self):
        addresses = self.resolver('    return [(2, 1, 6, "", ("203.0.113.10", 0)), (10, 1, 6, "", ("2001:db8::10", 0, 0, 0))]')
        self.assertEqual(addresses, ['203.0.113.10', '2001:db8::10'])

    def test_slow_resolver_is_killed_and_reaped_at_deadline(self):
        processes = []
        spawn = subprocess.Popen
        def child(*args, **kwargs):
            process = spawn(*args, **kwargs)
            processes.append(process)
            return process
        with patch.object(subprocess, 'Popen', side_effect=child), self.assertRaises(subprocess.TimeoutExpired):
            self.resolver('    import time\n    time.sleep(30)', timeout=.2)
        self.assertEqual(len(processes), 1)
        self.assertIsNotNone(processes[0].poll())


class HostCommandTests(unittest.TestCase):
    def process(self):
        return Mock(pid=91234, returncode=0, stdout=io.StringIO(), stderr=io.StringIO(), stdin=None)

    def test_failed_command_journal_records_only_safe_metadata(self):
        with tempfile.TemporaryDirectory() as root:
            host = Host()
            path = Path(root).resolve() / "commands.json"
            cases = [(["/usr/bin/systemctl", "show", "foundry-test.service", "-p", "secret-property",
                       "--property=private-value"], "show", ["foundry-test.service"]),
                     (["/usr/bin/runuser", "secret-argument"], None, [])]
            with host.command_journal(path):
                for args, subcommand, units in cases:
                    process = self.process()
                    process.returncode = 5
                    process.communicate.return_value = ("secret-stdout", "secret-stderr")
                    with patch("host.subprocess.Popen", return_value=process), self.assertRaises(DeployError):
                        host.run(args, input="secret-input", env={"SECRET": "secret-value"})
                    record = json.loads(path.read_text())[-1]
                    self.assertEqual(record, {"command": Path(args[0]).name, "subcommand": subcommand,
                                              "units": units, "exit_status": 5})
                self.assertEqual(path.stat().st_mode & 0o777, 0o600)
                self.assertNotIn("secret", path.read_text())
                before = path.read_bytes()
                host.check_result(subprocess.CompletedProcess(["systemctl", "is-enabled", "foundry-test.service"],
                                  1, "disabled", ""), check=False)
                self.assertEqual(path.read_bytes(), before)
            self.assertIsNone(host._command_journal)

    def test_concurrent_command_failures_preserve_both_journal_records(self):
        import threading
        from concurrent.futures import ThreadPoolExecutor
        with tempfile.TemporaryDirectory() as root:
            host = Host()
            path = Path(root).resolve() / "commands.json"
            path.write_text("[]")
            reading = threading.Barrier(2)
            read = host_module.read_json
            def overlap(path):
                result = read(path)
                try:
                    reading.wait(.2)
                except threading.BrokenBarrierError:
                    pass
                return result
            def fail(unit):
                with self.assertRaises(DeployError):
                    host.check_result(subprocess.CompletedProcess(["systemctl", "show", unit], 5, "", ""))
            with host.command_journal(path), patch.object(host_module, "read_json", side_effect=overlap), ThreadPoolExecutor(2) as pool:
                futures = [pool.submit(fail, unit) for unit in ("first.service", "second.service")]
                for future in futures:
                    future.result()
            records = json.loads(path.read_text())
            self.assertEqual({tuple(record["units"]) for record in records}, {("first.service",), ("second.service",)})
            self.assertEqual(len(records), 2)

    def test_command_journal_serializes_independent_processes(self):
        import time
        script = """
import sys, time
from pathlib import Path
sys.path.insert(0, sys.argv[1])
import host
from contracts import DeployError
from subprocess import CompletedProcess
path, gate, label = Path(sys.argv[2]), Path(sys.argv[3]), sys.argv[4]
read = host.read_json
def slow_read(path):
    records = read(path)
    time.sleep(.01)
    return records
host.read_json = slow_read
machine = host.Host()
with machine.command_journal(path):
    (gate.parent / label).touch()
    while not gate.exists():
        time.sleep(.005)
    for n in range(10):
        try:
            machine.check_result(CompletedProcess(['systemctl', 'show', label + str(n) + '.service'], 5, '', ''))
        except DeployError as error:
            assert error.code == 'command_failed'
"""
        with tempfile.TemporaryDirectory() as root:
            root = Path(root).resolve()
            path, gate = root / "commands.json", root / "start"
            path.write_text("[]")
            processes = [subprocess.Popen([sys.executable, "-B", "-c", script,
                str(Path(host_module.__file__).parent), str(path), str(gate), name], stdout=subprocess.PIPE, stderr=subprocess.PIPE)
                for name in ("first", "second")]
            try:
                deadline = time.monotonic() + 5
                while not all((root / name).exists() for name in ("first", "second")) and time.monotonic() < deadline:
                    time.sleep(.01)
                self.assertTrue(all((root / name).exists() for name in ("first", "second")))
                gate.touch()
                for process in processes:
                    _, error = process.communicate(timeout=10)
                    self.assertEqual(process.returncode, 0, error.decode())
            finally:
                for process in processes:
                    if process.poll() is None:
                        process.kill()
                    process.communicate()
            records = json.loads(path.read_text())
            self.assertEqual(len(records), 20)
            self.assertEqual({tuple(record["units"]) for record in records},
                             {(name + str(n) + ".service",) for name in ("first", "second") for n in range(10)})

    def test_timeout_kills_descendant_process_group_before_returning_error(self):
        process = self.process()
        process.communicate.side_effect = subprocess.TimeoutExpired(["private-argument"], 1)
        with patch("host.subprocess.Popen", return_value=process) as popen, patch("host.os.killpg") as kill:
            with self.assertRaises(DeployError) as error:
                Host().run(["/usr/bin/runuser", "private-argument"], timeout=1)
        self.assertTrue(popen.call_args.kwargs["start_new_session"])
        kill.assert_called_once_with(process.pid, signal.SIGKILL)
        process.wait.assert_called_once_with()
        self.assertTrue(process.stdout.closed)
        self.assertTrue(process.stderr.closed)
        self.assertNotIn("private-argument", str(error.exception))

    def test_outer_cancellation_also_kills_group(self):
        process = self.process()
        process.communicate.side_effect = KeyboardInterrupt()
        with patch("host.subprocess.Popen", return_value=process), patch("host.os.killpg") as kill:
            with self.assertRaises(KeyboardInterrupt):
                Host().run(["/usr/bin/runuser"])
        kill.assert_called_once_with(process.pid, signal.SIGKILL)
        process.wait.assert_called_once_with()

    def test_worker_cancellation_kills_once_then_reaps_and_closes_pipes(self):
        import time
        process = self.process()
        budget = host_module.ObservationBudget(time.monotonic() + 5)
        def communicate(**kwargs):
            budget.cancel()
            raise host_module.OperationTimeout("cancelled while communicating")
        process.communicate.side_effect = communicate
        with patch("host.subprocess.Popen", return_value=process), patch("host.os.killpg") as kill, budget:
            with self.assertRaises(host_module.OperationTimeout):
                Host().run(["/usr/bin/runuser"])
        kill.assert_called_once_with(process.pid, signal.SIGKILL)
        process.wait.assert_called_once_with()
        self.assertTrue(process.stdout.closed)
        self.assertTrue(process.stderr.closed)

    def test_success_retains_result_and_explicit_environment(self):
        process = self.process()
        process.communicate.return_value = ("result\n", "")
        with patch("host.subprocess.Popen", return_value=process) as popen, patch("host.os.killpg") as kill:
            result = Host().run(["/usr/bin/true"], input="private-input", env={"BOUND": "yes"})
        self.assertEqual(result.stdout, "result\n")
        self.assertEqual(result.returncode, 0)
        self.assertEqual(popen.call_args.kwargs["env"]["BOUND"], "yes")
        self.assertNotIn("HOME", popen.call_args.kwargs["env"])
        kill.assert_not_called()

    def test_child_inherits_only_explicit_lock_descriptors(self):
        process = self.process()
        process.communicate.return_value = ("[]\n", "")
        with patch("host.subprocess.Popen", return_value=process) as popen:
            Host().run(["/usr/bin/python3", "-I", "upgrades.py"], pass_fds=(42,))
        self.assertEqual((42,), popen.call_args.kwargs["pass_fds"])
