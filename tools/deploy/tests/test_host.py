"""Host command cancellation owns descendants as well as the immediate child."""
import io
from pathlib import Path
import signal
import subprocess
import sys
import unittest
from unittest.mock import Mock, patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "lib"))
from contracts import DeployError
from host import Host


class HostCommandTests(unittest.TestCase):
    def process(self):
        return Mock(pid=91234, returncode=0, stdout=io.StringIO(), stderr=io.StringIO(), stdin=None)

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
