"""Public host-report contract and privacy regression fixtures; no services."""

import copy
import json
import signal
import subprocess
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
        self.commands = []
        self.outputs = {}
        self.failures = {}

    def run(self, args, **kwargs):
        command = [str(item) for item in args]
        self.commands.append(command)
        if command[0] in self.failures:
            raise self.failures[command[0]]
        defaults = {"systemctl": "", "ss": "", "ufw": "Status: inactive\n",
                    "sshd": "passwordauthentication no\npermitrootlogin prohibit-password\n"}
        output = self.outputs.get(command[0], defaults.get(command[0], ""))
        return subprocess.CompletedProcess(command, 0, output, "")

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
            "policy": {"interval_hours": 24, "full_retention_days": 35, "pitr_days": 28, "secret": SECRET},
            "database": {"status": "complete", "configuration_revision": self.host.registry["instances"][INSTANCE_ID].get("configuration_revision"),
                "database_point": {"status": "complete", "completed_at": completed.isoformat()},
                "key_archive": {"status": "complete"}},
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

    def test_freshness_uses_database_interval_and_bundle_revision(self):
        clock = report.dt.datetime.now(report.dt.timezone.utc)
        entry = {"configuration_revision": 2, "backup": {"destination_ref": "configured", "last_complete_id": "bundle",
            "last_complete_at": clock.isoformat(),
            "database": {"status": "complete", "configuration_revision": 2, "completed_at": clock.isoformat(),
                "database_point": {"status": "complete", "completed_at": clock.isoformat()},
                "key_archive": {"status": "complete", "completed_at": clock.isoformat()}}}}
        maximum_age = report.BACKUP_FRESH_SECONDS
        for age, fresh in ((2 * 3600, True), (maximum_age - 60, True), (maximum_age + 60, False), (36 * 3600, False)):
            with self.subTest(age=age):
                point_time = (clock - report.dt.timedelta(seconds=age)).isoformat()
                entry["backup"]["database"]["database_point"]["completed_at"] = point_time
                result = report.backup_freshness(entry, [], INSTANCE_ID)
                self.assertEqual(result["fresh"], fresh)
                self.assertEqual(result["last_complete_at"], point_time)
        entry["backup"]["database"]["database_point"]["completed_at"] = clock.isoformat()
        entry["backup"]["database"]["configuration_revision"] = 1
        self.assertFalse(report.backup_freshness(entry, [], INSTANCE_ID)["fresh"])
        entry["backup"]["database"]["configuration_revision"] = 2
        for component in ("database_point", "key_archive"):
            with self.subTest(component=component):
                entry["backup"]["database"][component]["status"] = "running"
                self.assertFalse(report.backup_freshness(entry, [], INSTANCE_ID)["fresh"])
                entry["backup"]["database"][component]["status"] = "complete"
        for timestamp in ("not-a-timestamp", "2026-01-01T00:00:00", "9999-01-01T00:00:00Z", None):
            with self.subTest(timestamp=timestamp):
                entry["backup"]["database"]["database_point"]["completed_at"] = timestamp
                problems = []
                self.assertFalse(report.backup_freshness(entry, problems, INSTANCE_ID)["fresh"])
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
                                   ("http", "main_pid", None), ("worker", "main_pid", 0)):
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

    def test_unreadable_process_executable_is_unverified_without_claiming_a_mismatch(self):
        selected = self.host.paths.opt("abc-production") / "releases" / "v2"
        selected.mkdir(parents=True)
        (self.host.paths.opt("abc-production") / "current").symlink_to(selected)
        self.host.states[INSTANCE_ID].update({"status": "healthy", "current": {"version": "v2"}})
        self.service.return_value = {"load": "loaded", "active": "active", "substate": "running",
                                     "main_pid": 42, "exe": None}
        result = report.collect_report(self.host)
        item = self.item(result)
        self.assertFalse(item["metadata_consistent"])
        self.assertEqual("unknown", item["status"])
        self.assertEqual("healthy", item["recorded_status"])
        self.assertIn({"code": "running_release_unverified", "instance_id": INSTANCE_ID}, result["problems"])
        self.assertNotIn({"code": "running_release_mismatch", "instance_id": INSTANCE_ID}, result["problems"])
        self.service.return_value["exe"] = str(selected / "artisan")
        verified = self.item(report.collect_report(self.host))
        self.assertTrue(verified["metadata_consistent"])
        self.assertEqual("healthy", verified["status"])

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

    def test_failed_units_are_reported_and_bounded(self):
        units = [f"broken-{number}.service" for number in range(40)]
        self.host.outputs["systemctl"] = "".join(f"{unit} loaded failed failed\n" for unit in units)
        result = report.collect_report(self.host)
        self.assertEqual(units[:report.MAX_FAILED_UNITS], result["host"]["failed_units"])
        self.assertIn({"code": "units_failed"}, result["problems"])

    def test_failed_units_probe_failure_is_reported_without_exception(self):
        self.host.failures["systemctl"] = OSError(SECRET)
        result = report.collect_report(self.host)
        self.assertIsNone(result["host"]["failed_units"])
        self.assertIn({"code": "failed_units_unavailable"}, result["problems"])
        self.assertNotIn(SECRET, json.dumps(result))

    def test_public_listeners_exclude_loopback_and_flag_other_ports(self):
        self.host.outputs["ss"] = "".join((
            "LISTEN 0 4096 0.0.0.0:22 0.0.0.0:*\n",
            "LISTEN 0 4096 [::]:443 [::]:*\n",
            "LISTEN 0 4096 127.0.0.1:8100 0.0.0.0:*\n",
            "LISTEN 0 4096 [::1]:9000 [::]:*\n",
            "LISTEN 0 4096 0.0.0.0:8100 0.0.0.0:*\n",
        ))
        result = report.collect_report(self.host)
        self.assertEqual([22, 443, 8100], result["host"]["network"]["public_listeners"])
        self.assertIn({"code": "unexpected_public_listener"}, result["problems"])

    def test_scoped_loopback_listener_is_ignored(self):
        self.host.outputs["ss"] = "LISTEN 0 4096 127.0.0.53%lo:53 0.0.0.0:*\nLISTEN 0 4096 0.0.0.0:22 0.0.0.0:*\n"
        result = report.collect_report(self.host)
        self.assertEqual([22], result["host"]["network"]["public_listeners"])
        self.assertNotIn({"code": "unexpected_public_listener"}, result["problems"])

    def test_loopback_listeners_are_ignored(self):
        self.host.outputs["ss"] = "".join((
            "LISTEN 0 4096 127.0.0.1:8100 0.0.0.0:*\n",
            "LISTEN 0 4096 127.1.2.3:9000 0.0.0.0:*\n",
            "LISTEN 0 4096 [::1]:9001 [::]:*\n",
        ))
        result = report.collect_report(self.host)
        self.assertEqual([], result["host"]["network"]["public_listeners"])
        self.assertNotIn({"code": "unexpected_public_listener"}, result["problems"])

    def test_network_probe_failure_is_reported_without_exception(self):
        self.host.failures["ss"] = OSError(SECRET)
        result = report.collect_report(self.host)
        self.assertIsNone(result["host"]["network"]["public_listeners"])
        self.assertIn({"code": "network_unavailable"}, result["problems"])
        self.assertNotIn(SECRET, json.dumps(result))

    def test_root_firewall_status_is_informational(self):
        with patch.object(report.os, "geteuid", return_value=0):
            for output, expected in (("Status: active\n", "active"), ("Status: inactive\n", "inactive")):
                with self.subTest(status=expected):
                    self.host.outputs["ufw"] = output
                    result = report.collect_report(self.host)
                    self.assertEqual(expected, result["host"]["network"]["firewall"])
                    self.assertNotIn({"code": "network_unavailable"}, result["problems"])

    def test_missing_firewall_and_sshd_are_unknown_without_problem(self):
        self.host.failures.update({"ufw": OSError("missing"), "sshd": OSError("missing")})
        with patch.object(report.os, "geteuid", return_value=0):
            result = report.collect_report(self.host)
        self.assertIsNone(result["host"]["network"]["firewall"])
        self.assertEqual({"password_authentication": None, "root_login": None}, result["host"]["ssh"])
        self.assertNotIn({"code": "network_unavailable"}, result["problems"])
        self.assertNotIn({"code": "ssh_password_login"}, result["problems"])

    def test_nonroot_does_not_probe_firewall_or_ssh(self):
        with patch.object(report.os, "geteuid", return_value=1000):
            result = report.collect_report(self.host)
        self.assertIsNone(result["host"]["network"]["firewall"])
        self.assertEqual({"password_authentication": None, "root_login": None}, result["host"]["ssh"])
        self.assertFalse(any(command[0] in {"ufw", "sshd"} for command in self.host.commands))

    def test_ssh_password_authentication_is_reported(self):
        self.host.outputs["sshd"] = "passwordauthentication yes\npermitrootlogin yes\n"
        with patch.object(report.os, "geteuid", return_value=0):
            result = report.collect_report(self.host)
        self.assertEqual({"password_authentication": True, "root_login": "yes"}, result["host"]["ssh"])
        self.assertIn({"code": "ssh_password_login"}, result["problems"])

    def test_disabled_ssh_password_authentication_is_not_a_problem(self):
        self.host.outputs["sshd"] = "passwordauthentication no\npermitrootlogin prohibit-password\n"
        with patch.object(report.os, "geteuid", return_value=0):
            result = report.collect_report(self.host)
        self.assertEqual({"password_authentication": False, "root_login": "prohibit-password"}, result["host"]["ssh"])
        self.assertNotIn({"code": "ssh_password_login"}, result["problems"])

    def test_low_memory_adds_problem(self):
        self.host.paths.p("/proc/meminfo").write_text("MemTotal: 1000 kB\nMemAvailable: 99 kB\n")
        problems = []
        report.resources(self.host, problems, {})
        self.assertIn({"code": "memory_low"}, problems)

    def test_disk_low_uses_percentage_and_absolute_floor(self):
        for total, free in ((100, 5), (10, 1.5)):
            with self.subTest(total=total, free=free):
                usage = type("Usage", (), {"total": int(total * 1024 ** 3), "used": 0,
                                           "free": int(free * 1024 ** 3)})()
                problems = []
                with patch.object(report.shutil, "disk_usage", return_value=usage):
                    report.resources(self.host, problems, {})
                self.assertIn({"code": "disk_low"}, problems)

    def test_healthy_resources_do_not_report_low(self):
        usage = type("Usage", (), {"total": 100 * 1024 ** 3, "used": 50 * 1024 ** 3,
                                   "free": 50 * 1024 ** 3})()
        problems = []
        with patch.object(report.shutil, "disk_usage", return_value=usage):
            report.resources(self.host, problems, {})
        self.assertNotIn({"code": "disk_low"}, problems)
        self.assertNotIn({"code": "memory_low"}, problems)

    def test_certificate_status_and_expiry_problems(self):
        clock = report.dt.datetime.now(report.dt.timezone.utc)
        cases = (
            ({"status": "invalid", "expires_at": None}, "certificate_invalid"),
            ({"status": "valid", "expires_at": (clock + report.dt.timedelta(days=13)).isoformat()}, "certificate_expiring"),
            ({"status": "valid", "expires_at": (clock + report.dt.timedelta(days=15)).isoformat()}, None),
            ({"status": "unconfigured", "expires_at": None}, None),
        )
        for certificate, code in cases:
            with self.subTest(code=code):
                self.cert.return_value = certificate
                result = report.collect_report(self.host)
                for candidate in ("certificate_invalid", "certificate_expiring"):
                    item = {"code": candidate, "instance_id": INSTANCE_ID}
                    if candidate == code:
                        self.assertIn(item, result["problems"])
                    else:
                        self.assertNotIn(item, result["problems"])

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
            self.assertLess(time.monotonic() - started, 5)
        self.assertEqual(result["host"]["host_id"], HOST_ID)
        self.assertEqual([item["instance_id"] for item in result["projects"][0]["instances"]], [INSTANCE_ID])
        self.assertFalse(result["complete"])
        self.assertIn({"code": "report_budget_exhausted"}, result["problems"])
        self.assertNotIn(SECRET, json.dumps(result, allow_nan=False))

    def test_wall_clock_budget_survives_an_alarm_at_its_own_edges(self):
        # In a child: the fault this guards against kills the whole process.
        script = """
import random, signal, sys, time
sys.path.insert(0, sys.argv[1])
from host import OperationTimeout, wall_clock_budget
def work(seconds):
    start = time.perf_counter()
    while time.perf_counter() - start < seconds:
        pass
signal.signal(signal.SIGALRM, signal.SIG_DFL)
end = time.monotonic() + 2
while time.monotonic() < end:
    budget = random.uniform(0.00002, 0.0003)
    try:
        with wall_clock_budget(budget):
            try:
                with wall_clock_budget(budget * random.uniform(0.9, 1.0)):
                    work(budget * random.uniform(0.8, 1.2))
            except OperationTimeout:
                pass
    except OperationTimeout:
        pass
signal.raise_signal(signal.SIGALRM)
"""
        result = subprocess.run([sys.executable, "-I", "-B", "-c", script, str(Path(report.__file__).parent)],
                                capture_output=True, text=True, timeout=60)
        self.assertEqual(0, result.returncode, result.stderr[-400:])

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
        host.run = self.host.run
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
