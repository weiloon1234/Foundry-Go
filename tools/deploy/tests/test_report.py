"""Public host-report contract and privacy regression fixtures; no services."""

import copy
import json
import signal
import sys
import tempfile
import time
import unittest
from pathlib import Path
from unittest.mock import MagicMock, patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "lib"))

import report
from contracts import atomic_json, instance_paths
from host import Host, Paths

HOST_ID = "10000000-0000-4000-8000-000000000001"
PROJECT_ID = "20000000-0000-4000-8000-000000000002"
INSTANCE_ID = "30000000-0000-4000-8000-000000000003"
SECRET = "REPORT_MUST_NOT_EXPOSE_THIS_SECRET"


def instance(name="abc-production", environment="production", instance_id=INSTANCE_ID, project_id=PROJECT_ID, project="abc"):
    from test_contracts import fixture
    from contracts import identity
    p, profile, entry = fixture(project, environment, project_id, HOST_ID)
    profile["instance_id"] = instance_id
    entry.update(identity(p, profile))
    entry["allocations"].update(http_port=8100, uid=None, gid=None)
    profile["public_env"] = {"VITE_TEST": SECRET}
    return entry


class FixturePaths:
    def __init__(self, root):
        self.root = root.resolve()

    def p(self, path):
        return self.root / path.lstrip("/")

    def opt(self, name):
        return self.p(instance_paths(name)["application"])


class FixtureHost:
    def __init__(self, root):
        self.paths = FixturePaths(root)
        from test_contracts import registry
        self.registry = registry(instance())
        self.states = {INSTANCE_ID: {"schema": 1, "instance_id": INSTANCE_ID, "configuration_revision": 1, "status": "never_deployed", "current": None, "previous": None, "last_attempt": None, "history": [], "secret": SECRET}}
        proc = self.paths.p("/proc")
        proc.mkdir()
        (proc / "meminfo").write_text("MemTotal: 2048 kB\nMemAvailable: 1024 kB\n")
        (proc / "uptime").write_text("123.90 456.0\n")

    def read_registry(self, missing=False):
        return copy.deepcopy(self.registry)

    def load_state(self, entry):
        return copy.deepcopy(self.states[entry["profile"]["instance_id"]])


class ReportTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.host = FixtureHost(Path(self.temporary.name))
        self.service = patch.object(report, "service", return_value={"load": "loaded", "active": "inactive", "substate": "dead"}).start()
        self.ready = patch.object(report, "readiness", return_value={"ready": True, "http_status": 200}).start()
        self.cert = patch.object(report, "certificate", return_value={"status": "unconfigured", "expires_at": None}).start()
        self.addCleanup(patch.stopall)

    def item(self, result):
        return result["projects"][0]["instances"][0]

    def test_before_first_deploy_has_stable_identity_and_no_secrets(self):
        before = copy.deepcopy((self.host.registry, self.host.states))
        files = {str(p): p.read_bytes() for p in self.host.paths.root.rglob("*") if p.is_file()}
        result = report.collect_report(self.host)
        self.assertTrue(result["complete"])
        self.assertEqual(result["schema"], 2)
        self.assertEqual(result["host"]["host_id"], HOST_ID)
        self.assertEqual(result["host"]["resources"]["memory_total_bytes"], 2097152)
        self.assertEqual(result["host"]["resources"]["uptime_seconds"], 123)
        self.assertEqual(self.item(result)["status"], "never_deployed")
        self.assertIsNone(self.item(result)["current_release"])
        self.assertNotIn(SECRET, json.dumps(result))
        self.assertEqual((self.host.registry, self.host.states), before)
        self.assertEqual(files, {str(p): p.read_bytes() for p in self.host.paths.root.rglob("*") if p.is_file()})

    def test_backup_freshness_projects_only_recorded_public_receipts(self):
        completed = report.dt.datetime.now(report.dt.timezone.utc) - report.dt.timedelta(hours=2)
        self.host.registry["instances"][INSTANCE_ID]["backup"] = {
            "destination_ref": "/etc/foundry/private-repository.json", "last_complete_id": "backup-1",
            "last_complete_at": completed.isoformat(), "last_attempt_outcome": "complete",
            "policy": {"interval_hours": 24, "daily": 7, "secret": SECRET},
            "credentials": {"secret": SECRET}, "configuration_sha256": SECRET}
        result = report.collect_report(self.host)
        backup = self.item(result)["backup"]
        self.assertEqual("backup-1", backup["last_complete_id"])
        self.assertTrue(backup["fresh"])
        self.assertGreaterEqual(backup["age_seconds"], 7200)
        self.assertNotIn(SECRET, json.dumps(result))
        self.assertNotIn("destination_ref", backup)

    def test_stale_and_failed_backups_are_reported_without_repository_io(self):
        completed = report.dt.datetime.now(report.dt.timezone.utc) - report.dt.timedelta(days=2)
        self.host.registry["instances"][INSTANCE_ID]["backup"] = {
            "destination_ref": "configured", "last_complete_at": completed.isoformat(),
            "last_attempt_outcome": "failed", "policy": {"interval_hours": 24}}
        result = report.collect_report(self.host)
        self.assertFalse(self.item(result)["backup"]["fresh"])
        self.assertIn({"code": "backup_stale", "instance_id": INSTANCE_ID}, result["problems"])
        self.assertIn({"code": "backup_failed", "instance_id": INSTANCE_ID}, result["problems"])

    def test_invalid_backup_timestamp_cannot_claim_freshness(self):
        for timestamp in ("2026-01-01T00:00:00", "not-a-timestamp", "9999-01-01T00:00:00Z"):
            with self.subTest(timestamp=timestamp):
                problems = []
                result = report.backup_freshness({"backup": {"destination_ref": "configured", "last_complete_at": timestamp}}, problems, INSTANCE_ID)
                self.assertIsNone(result["age_seconds"])
                self.assertFalse(result["fresh"])
                self.assertIn({"code": "backup_state_invalid", "instance_id": INSTANCE_ID}, problems)

    def test_service_exposes_live_process_identity_and_restart_count(self):
        patch.stopall()
        with patch.object(report, "role_service", return_value={"LoadState": "loaded", "ActiveState": "active",
                "SubState": "running", "MainPID": "421", "NRestarts": "3", "exe": "/opt/foundry/apps/abc-production/releases/v1/artisan"}) as observe:
            result = report.service(self.host, "foundry-http@abc-production.service", time.monotonic() + 10)
        self.assertEqual(421, result["main_pid"])
        self.assertEqual(3, result["restarts"])
        self.assertEqual("/opt/foundry/apps/abc-production/releases/v1/artisan", result["exe"])
        observe.assert_called_once()

    def test_undeclared_worker_is_not_probed_or_reported(self):
        from topology import validate_topology
        self.host.registry["instances"][INSTANCE_ID]["topology"] = validate_topology({"schema": 1, "roles": [{"name": "http"}, {"name": "scheduler"}]})
        item = self.item(report.collect_report(self.host))
        self.assertEqual({"http", "scheduler"}, set(item["services"]))
        self.assertFalse(any("worker" in call.args[1] for call in self.service.call_args_list))

    def test_projects_group_multiple_environments(self):
        for name, env, iid, pid, project in [
            ("abc-staging", "staging", "30000000-0000-4000-8000-000000000004", PROJECT_ID, "abc"),
            ("other-prod", "prod", "30000000-0000-4000-8000-000000000005", "20000000-0000-4000-8000-000000000005", "other"),
        ]:
            self.host.registry["instances"][iid] = instance(name, env, iid, pid, project)
            self.host.states[iid] = {**self.host.states[INSTANCE_ID], "instance_id": iid}
        result = report.collect_report(self.host)
        self.assertEqual([p["project"] for p in result["projects"]], ["abc", "other"])
        self.assertEqual([i["environment"] for i in result["projects"][0]["instances"]], ["production", "staging"])

    def test_release_and_failure_projection_excludes_diagnostics(self):
        self.host.states[INSTANCE_ID].update({"status": "failed", "current": {"version": "v1", "commit": "abcdef", "secrets": SECRET}, "last_attempt": {"id": "attempt1", "outcome": "failed", "error": {"code": "migration_failed", "detail": SECRET}, "stdout": SECRET}, "history": [{"secret": SECRET}]})
        result = report.collect_report(self.host)
        self.assertEqual(self.item(result)["last_attempt"]["error_code"], "migration_failed")
        self.assertNotIn(SECRET, json.dumps(result))

    def test_healthy_state_needs_matching_active_release_and_readiness(self):
        self.service.return_value = {"load": "loaded", "active": "active", "substate": "running"}
        release_dir = self.host.paths.opt("abc-production") / "releases" / "v1"
        release_dir.mkdir(parents=True)
        (self.host.paths.opt("abc-production") / "current").symlink_to(release_dir)
        self.host.states[INSTANCE_ID].update({"status": "healthy", "current": {"version": "v1"}})
        self.service.return_value.update(main_pid=42, exe=str(release_dir / "artisan"))
        self.assertTrue(self.item(report.collect_report(self.host))["metadata_consistent"])
        self.ready.return_value = {"ready": False, "http_status": 503}
        result = report.collect_report(self.host)
        self.assertFalse(result["complete"])
        self.assertFalse(self.item(result)["metadata_consistent"])
        self.assertEqual(self.item(result)["status"], "unknown")
        self.assertEqual(self.item(result)["recorded_status"], "healthy")
        self.assertIn({"code": "instance_state_inconsistent", "instance_id": INSTANCE_ID}, result["problems"])

    def test_active_roles_must_match_selected_executable_and_have_a_live_pid(self):
        selected = self.host.paths.opt("abc-production") / "releases" / "v2"
        selected.mkdir(parents=True)
        (self.host.paths.opt("abc-production") / "current").symlink_to(selected)
        self.host.states[INSTANCE_ID].update({"status": "healthy", "current": {"version": "v2"}})
        healthy = {"load": "loaded", "active": "active", "substate": "running",
                   "main_pid": 42, "exe": str(selected / "artisan")}
        for role, field, value in (("scheduler", "exe", str(selected.parent / "v1" / "artisan")),
                                   ("http", "main_pid", None), ("worker", "main_pid", 0),
                                   ("worker", "exe", None)):
            with self.subTest(role=role, field=field):
                self.service.reset_mock()
                def observed(host, unit, deadline):
                    return {**healthy, field: value} if unit.startswith("foundry-" + role + "@") else dict(healthy)
                self.service.side_effect = observed
                result = report.collect_report(self.host)
                item = self.item(result)
                self.assertEqual("unknown", item["status"])
                self.assertFalse(item["metadata_consistent"])
                self.assertIn({"code": "running_release_mismatch", "instance_id": INSTANCE_ID}, result["problems"])
                self.assertEqual(3, self.service.call_count)

    def test_missing_or_linked_release_directory_is_inconsistent_even_with_healthy_process(self):
        self.service.return_value = {"load": "loaded", "active": "active", "substate": "running"}
        application = self.host.paths.opt("abc-production")
        release_dir = application / "releases" / "v1"
        release_dir.parent.mkdir(parents=True)
        (application / "current").symlink_to(release_dir)
        self.host.states[INSTANCE_ID].update({"status": "healthy", "current": {"version": "v1"}})
        self.assertEqual(self.item(report.collect_report(self.host))["status"], "unknown")
        foreign = self.host.paths.root / "foreign-release"
        foreign.mkdir()
        release_dir.symlink_to(foreign)
        result = report.collect_report(self.host)
        self.assertEqual(self.item(result)["status"], "unknown")
        self.assertIn({"code": "instance_state_inconsistent", "instance_id": INSTANCE_ID}, result["problems"])

    def test_running_never_deployed_instance_marks_metadata_inconsistent(self):
        self.service.return_value = {"load": "loaded", "active": "active", "substate": "running"}
        result = report.collect_report(self.host)
        self.assertEqual(self.item(result)["recorded_status"], "never_deployed")
        self.assertFalse(self.item(result)["metadata_consistent"])
        self.assertIn({"code": "instance_state_inconsistent", "instance_id": INSTANCE_ID}, result["problems"])
        self.ready.assert_not_called()

    def test_probe_failure_keeps_partial_report_and_hides_exception(self):
        self.host.states[INSTANCE_ID]["status"] = "failed"
        self.service.side_effect = OSError(SECRET)
        self.ready.side_effect = TimeoutError(SECRET)
        result = report.collect_report(self.host)
        self.assertFalse(result["complete"])
        self.assertIsNone(self.item(result)["service"])
        self.assertIsNone(self.item(result)["readiness"])
        self.assertEqual(result["host"]["host_id"], HOST_ID)
        self.assertNotIn(SECRET, json.dumps(result))

    def test_missing_unit_is_not_reported_as_prepared_and_consistent(self):
        self.service.return_value = {"load": "not-found", "active": "inactive", "substate": "dead"}
        result = report.collect_report(self.host)
        self.assertEqual(self.item(result)["status"], "never_deployed")
        self.assertFalse(self.item(result)["metadata_consistent"])
        self.assertIn({"code": "instance_state_inconsistent", "instance_id": INSTANCE_ID}, result["problems"])

    def test_background_units_are_reported_and_required_for_prepared_state(self):
        def observed(host, unit, deadline):
            return {"load": "not-found" if "foundry-worker@" in unit else "loaded", "active": "inactive", "substate": "dead"}
        self.service.side_effect = observed
        item = self.item(report.collect_report(self.host))
        self.assertEqual(set(item["services"]), {"http", "worker", "scheduler"})
        self.assertEqual(item["service"]["load"], "loaded")
        self.assertEqual(item["services"]["worker"]["state"]["load"], "not-found")
        self.assertFalse(item["metadata_consistent"])

    def test_invalid_registry_never_probes_resources_from_it(self):
        self.host.registry["layout_version"] = 99
        result = report.collect_report(self.host)
        self.assertEqual(result["projects"], [])
        self.assertEqual(result["problems"], [{"code": "registry_unavailable"}])
        self.assertFalse(result["complete"])
        self.service.assert_not_called()

    def test_hostname_failure_keeps_inventory_available(self):
        with patch.object(report.socket, "gethostname", side_effect=OSError(SECRET)):
            result = report.collect_report(self.host)
        self.assertIsNone(result["host"]["hostname"])
        self.assertEqual(result["host"]["host_id"], HOST_ID)
        self.assertEqual(self.item(result)["instance_id"], INSTANCE_ID)
        self.assertIn({"code": "hostname_unavailable"}, result["problems"])
        self.assertNotIn(SECRET, json.dumps(result))

    def test_foreign_state_and_unsafe_instance_name_are_refused(self):
        self.host.states[INSTANCE_ID]["instance_id"] = "30000000-0000-4000-8000-000000000099"
        result = report.collect_report(self.host)
        self.assertEqual(self.item(result)["status"], "unknown")
        self.assertIn({"code": "instance_state_unavailable", "instance_id": INSTANCE_ID}, result["problems"])
        self.host.registry["instances"][INSTANCE_ID]["name"] = "../../etc"
        self.assertEqual(report.collect_report(self.host)["projects"], [])

    def test_probe_budget_and_invalid_port_do_not_open_connections(self):
        patch.stopall()
        with patch.object(report.urllib.request, "build_opener") as opener:
            with self.assertRaises(ValueError):
                report.readiness(80, report.time.monotonic() + 1)
            opener.assert_not_called()
        with patch.object(self.host, "run", create=True) as run:
            with self.assertRaises(TimeoutError):
                report.service(self.host, "nginx.service", report.time.monotonic() - 1)
            run.assert_not_called()

    def test_redirects_are_not_followed(self):
        handler = report.NoRedirect()
        self.assertIsNone(handler.redirect_request(None, None, 302, "", {}, "https://example.com"))

    def test_readiness_requires_bounded_exact_json_not_only_http_200(self):
        patch.stopall()
        response = MagicMock()
        response.status = 200
        response.__enter__.return_value = response
        with patch.object(report.urllib.request, "build_opener") as opener:
            opener.return_value.open.return_value = response
            for payload, expected in [(b'{"ready": true}', True), (b'<html>Login</html>', False), (b'{"ready": false}', False), (b'{"ready": 1}', False), (b'{"ready": false, "ready": true}', False), (b'x' * 4097, False)]:
                response.read.return_value = payload
                self.assertEqual(report.readiness(8100, report.time.monotonic() + 1), {"ready": expected, "http_status": 200})
                response.read.assert_called_with(4097)

    def test_applied_config_failure_preserves_public_outcome(self):
        result = report.attempt({"outcome": "failed", "result": "failed_applied", "secret": SECRET})
        self.assertEqual("failed_applied", result["outcome"])
        self.assertNotIn(SECRET, json.dumps(result))

    def test_attempt_error_accepts_machine_codes_only(self):
        self.assertEqual(report.attempt({"error": "readiness_failed"})["error_code"], "readiness_failed")
        self.assertIsNone(report.attempt({"error": "password=" + SECRET})["error_code"])

    def test_wall_clock_budget_interrupts_a_slow_drip_and_keeps_completed_instances(self):
        patch.stopall()
        self.host.states[INSTANCE_ID]["status"] = "failed"
        second_id = "30000000-0000-4000-8000-000000000004"
        self.host.registry["instances"][second_id] = instance("abc-staging", "staging", second_id)
        self.host.states[second_id] = {**self.host.states[INSTANCE_ID], "instance_id": second_id}
        response = MagicMock()
        response.status = 200
        response.__enter__.return_value = response
        reads = 0

        def drip(size):
            nonlocal reads
            reads += 1
            if reads == 1:
                return b'{"ready": true}'
            # Each byte arrives faster than the socket inactivity timeout, but
            # the full body would take seconds without a wall-clock deadline.
            for _ in range(size):
                time.sleep(0.005)
            return b'{"ready": true}'

        response.read.side_effect = drip
        with patch.object(report, "REPORT_BUDGET", 0.05), \
                patch.object(report, "service", return_value={"load": "loaded", "active": "active", "substate": "running"}), \
                patch.object(report, "certificate", return_value={"status": "unconfigured", "expires_at": None}), \
                patch.object(report.urllib.request, "build_opener") as opener:
            opener.return_value.open.return_value = response
            started = time.monotonic()
            result = report.collect_report(self.host)
            self.assertLess(time.monotonic() - started, 0.5)
        self.assertEqual(result["host"]["host_id"], HOST_ID)
        self.assertEqual([item["instance_id"] for item in result["projects"][0]["instances"]], [INSTANCE_ID])
        self.assertFalse(result["complete"])
        self.assertIn({"code": "report_budget_exhausted"}, result["problems"])
        self.assertNotIn(SECRET, json.dumps(result, allow_nan=False))

    def test_wall_clock_budget_restores_previous_handler_and_timer(self):
        previous_handler = signal.getsignal(signal.SIGALRM)
        previous_timer = signal.getitimer(signal.ITIMER_REAL)
        handler = lambda signum, frame: None
        try:
            signal.signal(signal.SIGALRM, handler)
            signal.setitimer(signal.ITIMER_REAL, 10, 3)
            self.assertTrue(report.collect_report(self.host)["complete"])
            self.assertIs(signal.getsignal(signal.SIGALRM), handler)
            self.assertEqual(signal.getitimer(signal.ITIMER_REAL)[1], 3)
            with self.assertRaises(report.ReportBudgetExceeded):
                with report.wall_clock_budget(0.02):
                    time.sleep(1)
            self.assertIs(signal.getsignal(signal.SIGALRM), handler)
            delay, interval = signal.getitimer(signal.ITIMER_REAL)
            self.assertGreater(delay, 8)
            self.assertLess(delay, 10)
            self.assertEqual(interval, 3)
        finally:
            signal.setitimer(signal.ITIMER_REAL, 0)
            signal.signal(signal.SIGALRM, previous_handler)
            signal.setitimer(signal.ITIMER_REAL, *previous_timer)

    def test_real_host_reads_public_registry_without_touching_private_configuration(self):
        host = Host(Paths(self.host.paths.root))
        atomic_json(host.paths.registry, self.host.registry)
        entry = self.host.registry["instances"][INSTANCE_ID]
        atomic_json(host.paths.instance_state(entry["name"]) / "state.json", self.host.states[INSTANCE_ID])
        private = host.paths.instance_config(entry["name"])
        private.mkdir(parents=True)
        (private / "app.env").write_text("PASSWORD=" + SECRET)
        (private / "app.env").chmod(0o600)
        files = {str(p): p.read_bytes() for p in host.paths.root.rglob("*") if p.is_file()}
        result = report.collect_report(host)
        self.assertTrue(result["complete"])
        self.assertEqual(self.item(result)["instance_id"], INSTANCE_ID)
        self.assertNotIn(SECRET, json.dumps(result))
        self.assertEqual(files, {str(p): p.read_bytes() for p in host.paths.root.rglob("*") if p.is_file()})


if __name__ == "__main__":
    unittest.main()
