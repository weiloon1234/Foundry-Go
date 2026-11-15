"""Legacy deployment parser compatibility with Go-emitted disabled-feature status."""
import copy
import json
import subprocess
import sys
import unittest
from pathlib import Path
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "lib"))
from contracts import DeployError, parse_json, require
from release import app
import release as current

# Verbatim from v0.36.0 tools/deploy/lib/release.py. Keep this parser frozen.
def migration_status(host, entry, release, env_file=None):
    report = parse_json(app(host, entry, release, "migrate:status", env_file=env_file, extra=("--database", "primary")).stdout)
    require(isinstance(report, dict) and isinstance(report.get("targets"), list) and report["targets"], "migration_status", "Migration status did not return declared database targets.")
    pending = False
    for target in report["targets"]:
        require(isinstance(target, dict) and isinstance(target.get("migrations"), list) and not target.get("problems"), "migration_reconciliation", "Migration history needs reconciliation; inspect artisan migrate:status on the host.")
        for migration in target["migrations"]:
            require(isinstance(migration, dict) and migration.get("state") in ("pending", "applied"), "migration_reconciliation", "Migration history is incomplete or changed; administrator reconciliation required.")
            pending |= migration["state"] == "pending"
    return pending


class MigrationStatusTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        root = Path(__file__).resolve().parents[3]
        result = subprocess.run(["go", "run", "./tools/deploy/tests/fixtures/migration-status"],
                                cwd=root, check=True, text=True, capture_output=True, timeout=120)
        cls.outputs = json.loads(result.stdout)

    def test_v036_parser_accepts_new_go_status_without_reconciliation(self):
        for name, document in self.outputs.items():
            with self.subTest(name=name), patch(__name__ + ".app", return_value=subprocess.CompletedProcess([], 0, json.dumps(document))):
                self.assertEqual(migration_status(None, None, None), name == "enabled-only")

    def test_current_parser_reports_disabled_entries_without_pending_work(self):
        for name, document in self.outputs.items():
            with self.subTest(name=name), patch.object(current, "app", return_value=subprocess.CompletedProcess([], 0, json.dumps(document))):
                report = current.migration_report(None, None, None)
                self.assertEqual(current.migration_status(None, None, None), name == "enabled-only")
                self.assertEqual(report["pending"], name == "enabled-only")
                expected = [{"key": {"origin": "feature", "id": "001"}, "state": "pending_feature_disabled"}] if name == "disabled-pending" else []
                self.assertEqual(report["targets"], [{"schema": "public", "disabled_pending": expected}])

    def test_disabled_details_are_validated_and_public_projection_omits_extra_data(self):
        document = copy.deepcopy(self.outputs["disabled-pending"])
        row = document["targets"][0]["disabled_pending"][0]
        row["private"] = "never-report-this"
        with patch.object(current, "app", return_value=subprocess.CompletedProcess([], 0, json.dumps(document))):
            self.assertNotIn("never-report-this", json.dumps(current.migration_report(None, None, None)))
        for invalid in (None, {}, [{"state": "applied"}], [{"key": {"origin": "feature", "id": "secret\nvalue"}, "state": "pending_feature_disabled"}]):
            document["targets"][0]["disabled_pending"] = invalid
            with self.subTest(invalid=invalid), patch.object(current, "app", return_value=subprocess.CompletedProcess([], 0, json.dumps(document))), self.assertRaises(DeployError):
                current.migration_status(None, None, None)

    def test_real_history_conflicts_still_require_reconciliation(self):
        for state in ("changed", "missing_definition", "incomplete", "pending_feature_disabled"):
            document = copy.deepcopy(self.outputs["enabled-only"])
            document["targets"][0]["migrations"][0]["state"] = state
            with self.subTest(state=state), patch.object(current, "app", return_value=subprocess.CompletedProcess([], 0, json.dumps(document))), self.assertRaises(DeployError):
                current.migration_status(None, None, None)
