"""The optional copy operations scenario must require the disposable-host guard."""
import unittest
from unittest.mock import Mock, patch

import acceptance_copies_ops as scenario


class CopyAcceptanceTests(unittest.TestCase):
    def test_guard_runs_before_host_access(self):
        host = Mock()
        with patch.object(scenario, "guard", side_effect=RuntimeError("not disposable")), self.assertRaises(RuntimeError):
            scenario.assert_copies_operations(host, {}, {})
        self.assertEqual(host.mock_calls, [])


if __name__ == "__main__":
    unittest.main()
