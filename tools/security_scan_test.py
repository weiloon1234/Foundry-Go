import json
import os
from pathlib import Path
import sys
import tempfile
import unittest

from security_scan import decode_stream, inspect_vulnerabilities, inspect_npm, run_command

CONFIG = {"config": {"scanner_name": "govulncheck", "scan_level": "symbol", "db_last_modified": "2026-09-16T00:00:00Z"}}


class SecurityScanTests(unittest.TestCase):
    def test_zero_exit_json_is_not_proof_of_safety(self):
        finding = {"finding": {"osv": "GO-2026-0001", "trace": [{"module": "example.test/mod", "function": "Vulnerable"}]}}
        report = inspect_vulnerabilities(json.dumps(CONFIG) + "\n" + json.dumps(finding))
        self.assertEqual(report["affected_function_findings"], [finding["finding"]])

    def test_module_only_finding_is_retained_without_invented_reachability(self):
        finding = {"finding": {"osv": "GO-2026-0001", "trace": [{"module": "example.test/mod"}]}}
        report = inspect_vulnerabilities(json.dumps(CONFIG) + json.dumps(finding))
        self.assertEqual(report["findings"], [finding["finding"]])
        self.assertEqual(report["affected_function_findings"], [])

    def test_missing_malformed_and_trailing_results_fail_closed(self):
        for text in ("", "{}", json.dumps(CONFIG) + "trailing error", json.dumps(CONFIG) + '{"finding":{"osv":"GO-test"}}'):
            with self.assertRaises(ValueError):
                inspect_vulnerabilities(text)
        with self.assertRaises(ValueError):
            inspect_npm('{}')
        report = {"metadata": {"vulnerabilities": dict(info=0, low=0, moderate=0, high=1, critical=0, total=1)}}
        self.assertEqual(inspect_npm(json.dumps(report))["vulnerabilities"]["total"], 1)

    def test_subprocess_failure_and_timeout_are_retained(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            result, text = run_command([sys.executable, "-c", "print('retained'); raise SystemExit(7)"], root, root / "failed", os.environ.copy())
            self.assertEqual(result["exit"], 7)
            self.assertEqual(text.strip(), "retained")
            result, _ = run_command([sys.executable, "-c", "import time;time.sleep(30)"], root, root / "timed", os.environ.copy(), timeout=0.15)
            self.assertIn("deadline", result["execution_error"])
            self.assertNotEqual(result["exit"], 0)

    def test_output_is_bounded(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            result, text = run_command([sys.executable, "-c", "print('x'*2048)"], root, root / "large", os.environ.copy(), output_limit=1024)
            self.assertIn("output bound", result["execution_error"])
            self.assertEqual(text, "")
