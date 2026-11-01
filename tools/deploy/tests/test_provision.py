"""Foundation bootstrap planning with read-only service adapters and no processes."""
import copy
from contextlib import ExitStack
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch
import uuid

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "lib"))

from contracts import (CLEANUP_GRACE_KEY, DEFAULT_CLEANUP_GRACE, DeployError, instance_paths, parse_env,
                       validate_registry, bind_storage)
from host import Host, Paths
from topology import instance_units
import provision
from test_contracts import fixture, storage_fixture

TOOLS = Path(__file__).resolve().parents[1]
TOPOLOGY = {"schema": 1, "roles": [{"name": "http"}, {"name": "scheduler"}]}


class PlanningHost(Host):
    def __init__(self, root):
        super().__init__(Paths(root))
        self.registry_value = None
        self.commands = []
        self.ports = []
        self.writes = []
        os_release = self.paths.p("/etc/os-release")
        os_release.parent.mkdir(parents=True)
        os_release.write_text('ID=ubuntu\nVERSION_ID="24.04"\n')

    def read_registry(self, missing=False):
        return copy.deepcopy(self.registry_value)

    def write_registry(self, value):
        self.writes.append(copy.deepcopy(value))
        self.registry_value = copy.deepcopy(value)

    def run(self, args, **kwargs):
        self.commands.append(list(args))
        if args == ["dpkg", "--print-architecture"]:
            return subprocess.CompletedProcess(args, 0, "arm64\n", "")
        if args == ["ss", "-Hltn"]:
            rows = [f"LISTEN 0 128 127.0.0.1:{port} 0.0.0.0:*" for port in self.ports]
            return subprocess.CompletedProcess(args, 0, "\n".join(rows), "")
        if args[:2] == ["getent", "passwd"]:
            return subprocess.CompletedProcess(args, 2, "", "")
        if args[:2] == ["dpkg-query", "-W"]:
            return subprocess.CompletedProcess(args, 1, "", "")
        raise AssertionError(f"Unexpected host command: {args[0]}")


class FoundationProvisionTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.host = PlanningHost(Path(temporary.name))
        self.project, self.profile, _ = fixture()
        self.inspections = []
        for module, result in ((provision.packages, {"missing": []}),
                               (provision.postgres, {"state": "planned"}),
                               (provision.redis_acl, {"state": "planned"}),
                               (provision.certificates, {"state": "planned"})):
            patcher = patch.object(module, "inspect", return_value=result)
            self.inspections.append(patcher)
            patcher.start()
            self.addCleanup(patcher.stop)
        patcher = patch.object(provision.upgrades, "tools_upgrade", return_value=None)
        patcher.start()
        self.addCleanup(patcher.stop)

    def plan(self, **kwargs):
        return provision.bootstrap_plan(self.host, self.project, self.profile, TOOLS,
                                        topology=kwargs.pop("topology", TOPOLOGY), **kwargs)

    def register(self, plan):
        registry = provision.new_registry(plan["host_id"], TOOLS)
        registry["projects"][self.project["id"]] = {"slug": self.project["slug"], "created_at": registry["created_at"]}
        registry["instances"][self.profile["instance_id"]] = plan["entry"]
        self.host.registry_value = validate_registry(registry)

    def test_plan_allocates_v2_identity_and_declared_roles_without_mutations(self):
        self.host.ports = [8100, 8101, 6379, 5432]
        plan = self.plan()
        entry = plan["entry"]
        self.assertTrue(plan["ok"])
        self.assertEqual(8102, entry["allocations"]["http_port"])
        self.assertEqual("f_" + uuid.UUID(self.profile["instance_id"]).hex, entry["allocations"]["database"])
        self.assertEqual("foundry-" + self.profile["instance_id"], entry["allocations"]["redis_user"])
        self.assertEqual(instance_paths(entry["name"]), entry["allocations"]["paths"])
        self.assertEqual({"http", "scheduler"}, set(instance_units(entry)))
        self.assertEqual([], self.host.writes)
        self.assertEqual([], entry["redis_grants"]["keys"])
        self.register(plan)
        self.assertEqual(2, self.host.registry_value["schema"])
        self.assertEqual(1, self.host.registry_value["tools"]["generation"])

    def test_plan_digest_changes_when_observed_ports_change(self):
        before = self.plan()
        self.host.ports = [8100]
        after = self.plan()
        self.assertNotEqual(before["plan_token"], after["plan_token"])
        self.assertNotEqual(before["entry"]["allocations"]["http_port"], after["entry"]["allocations"]["http_port"])

    def test_existing_instance_keeps_allocation_and_refuses_topology_drift(self):
        first = self.plan()
        self.register(first)
        self.host.ports = [first["entry"]["allocations"]["http_port"]]
        again = self.plan()
        self.assertEqual(first["entry"]["allocations"], again["entry"]["allocations"])
        with self.assertRaises(DeployError) as error:
            self.plan(topology={"schema": 1, "roles": [{"name": "http"}]})
        self.assertEqual("topology_change_required", error.exception.code)

    def test_host_identity_mismatch_refuses_before_service_inspection(self):
        self.register(self.plan())
        self.profile["host"]["host_id"] = str(uuid.uuid4())
        provision.packages.inspect.reset_mock()
        with self.assertRaises(DeployError) as error:
            self.plan()
        self.assertEqual("host_mismatch", error.exception.code)
        provision.packages.inspect.assert_not_called()

    def test_tool_upgrade_blocks_bootstrap(self):
        with patch.object(provision.upgrades, "tools_upgrade", return_value={"generation": 2, "from_digest": "a" * 64}):
            plan = self.plan()
        self.assertFalse(plan["ok"])
        self.assertIn("host_upgrade_required", {finding["code"] for finding in plan["findings"]})

    def test_installs_only_declared_hardened_units_without_starting(self):
        entry = self.plan()["entry"]
        registry = {"shared": {"postgresql": {"unit": "postgresql@16-foundry.service"}}}
        written = {}
        def write(host, path, content, *args):
            written[path.name] = content
            return True
        with patch.object(provision, "_owned_write", side_effect=write), patch.object(self.host, "run") as run:
            provision._install_unit(self.host, registry, entry, TOOLS)
        self.assertEqual(set(instance_units(entry).values()), set(written))
        self.assertFalse(any("worker" in name for name in written))
        for content in written.values():
            self.assertIn("ProtectSystem=strict", content)
            self.assertIn("NoNewPrivileges=yes", content)
            self.assertIn("KillMode=control-group", content)
            self.assertIn("postgresql@16-foundry.service", content)
            self.assertNotIn("postgresql.service", content)
        commands = [call.args[0] for call in run.call_args_list]
        self.assertEqual(["systemctl", "daemon-reload"], commands[0])
        self.assertEqual(["systemctl", "enable", *instance_units(entry).values()], commands[1])

    def test_declared_worker_has_no_product_specific_connection(self):
        topology = {"schema": 1, "roles": [{"name": "http"}, {"name": "worker"}]}
        entry = self.plan(topology=topology)["entry"]
        content = provision._template(TOOLS, "foundry-worker@.service", entry, postgres_unit="postgresql@16-foundry.service")
        self.assertIn("artisan queue:work --connection ${FOUNDRY_WORKER_CONNECTION} --config", content)
        self.assertIn("EnvironmentFile=" + entry["allocations"]["paths"]["configuration"] + "/worker.env", content)
        self.assertNotIn("@WORKER_CONNECTION@", content)

    def test_foreign_unit_template_and_dropin_refuse_before_install(self):
        entry = self.plan()["entry"]
        unit = instance_units(entry)["http"]
        paths = [f"/etc/systemd/system/{unit}", "/etc/systemd/system/foundry-http@.service",
                 f"/run/systemd/system/{unit}", f"/etc/systemd/system/{unit}.d"]
        for relative in paths:
            with self.subTest(path=relative):
                path = self.host.paths.p(relative)
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text("foreign unit")
                with patch.object(provision, "_owned_write") as write, self.assertRaises(DeployError):
                    provision._install_unit(self.host, {"shared": {"postgresql": {"unit": "postgresql@16-foundry.service"}}}, entry, TOOLS)
                write.assert_not_called()
                path.unlink()

    def test_nginx_exact_and_wildcard_domains_compare_case_insensitively(self):
        entry = self.plan()["entry"]
        domain = entry["profile"]["domain"]
        for declared in (domain.upper(), "*." + domain.split(".", 1)[1].upper(), "." + domain.upper()):
            with self.subTest(declared=declared):
                config = "# configuration file /etc/nginx/conf.d/foreign.conf:\nserver { server_name " + declared + "; }\n"
                with patch.object(provision, "_installed", return_value=True), \
                     patch.object(self.host, "run", return_value=subprocess.CompletedProcess([], 0, config, "")), \
                     self.assertRaises(DeployError):
                    provision._site_checks(self.host, entry)

    def test_d07_source_installed_nginx_is_inspected_and_conflicts_refused(self):
        entry = self.plan()["entry"]
        binary = self.host.paths.p("/usr/sbin/nginx")
        binary.parent.mkdir(parents=True)
        binary.touch()
        config = "# configuration file /etc/nginx/custom.conf:\nserver { server_name " + entry["profile"]["domain"] + "; }\n"
        with patch.object(provision, "_installed", return_value=False), \
                patch.object(self.host, "run", return_value=subprocess.CompletedProcess([], 0, config, "")) as run:
            with self.assertRaises(DeployError):
                provision._site_checks(self.host, entry)
        run.assert_called_once_with(["nginx", "-T"], check=False)

    def test_d07_uninspectable_binary_configuration_refuses(self):
        entry = self.plan()["entry"]
        binary = self.host.paths.p("/usr/sbin/nginx")
        binary.parent.mkdir(parents=True)
        binary.touch()
        for result in (subprocess.CompletedProcess([], 1, "", "invalid config"),
                       subprocess.CompletedProcess([], 0, "", ""),
                       subprocess.CompletedProcess([], 0, "# configuration file /etc/nginx/nginx.conf:\n", "")):
            with self.subTest(result=result), patch.object(self.host, "run", return_value=result):
                with self.assertRaises(DeployError):
                    provision._site_checks(self.host, entry)
        with patch.object(self.host, "run", side_effect=OSError("unavailable")), self.assertRaises(DeployError):
            provision._site_checks(self.host, entry)

    def test_only_fresh_foundry_nginx_is_started_after_validation(self):
        active = subprocess.CompletedProcess([], 0, "", "")
        inactive = subprocess.CompletedProcess([], 3, "", "")
        with patch.object(self.host, "run", side_effect=[inactive, active, active, active]) as run:
            provision._ensure_nginx(self.host, {"shared": {"nginx": {"owner": "foundry", "installed_by_foundry": True}}})
        self.assertEqual([["systemctl", "is-active", "--quiet", "nginx"], ["nginx", "-t", "-q"],
                          ["systemctl", "enable", "--now", "nginx"], ["systemctl", "is-active", "--quiet", "nginx"]],
                         [call.args[0] for call in run.call_args_list])
        with patch.object(self.host, "run", return_value=inactive) as run, self.assertRaises(DeployError):
            provision._ensure_nginx(self.host, {"shared": {}})
        self.assertEqual(1, run.call_count)
        registry = {"shared": {}}
        with patch.object(self.host, "run", return_value=active) as run:
            provision._ensure_nginx(self.host, registry)
        self.assertEqual(1, run.call_count)
        self.assertEqual("nginx.service", registry["shared"]["nginx"]["unit"])
        self.assertFalse(registry["shared"]["nginx"]["installed_by_foundry"])
        with patch.object(self.host, "run", return_value=inactive) as run, self.assertRaises(DeployError):
            provision._ensure_nginx(self.host, registry)
        self.assertEqual(1, run.call_count)

    def test_ready_instance_repairs_site_without_reprovisioning_shared_services(self):
        plan = self.plan()
        plan["entry"]["provisioning"] = {"state": "preparing", "completed_steps": [
            "secrets", "bootstrap_secrets", "database", "redis", "configuration", "backup", "unit", "certificate"]}
        for key in ("postgresql", "redis", "certificate"):
            plan["observations"][key] = {"state": "ready"}
        self.register(plan)
        self.profile["host"]["host_id"] = plan["host_id"]
        with patch.object(provision, "bootstrap_plan", return_value=plan), \
             patch.object(provision, "validate_runtime_environment"), \
             patch.object(provision, "initialize_bootstrap_secrets"), \
             patch.object(provision, "_ready_configuration", return_value=False), \
             patch.object(provision, "_install_unit"), patch.object(provision, "_install_site") as site, \
             patch.object(provision.postgres, "prepare") as postgres, \
             patch.object(provision.backup, "configure") as backup:
            result = provision.bootstrap_apply(self.host, self.project, self.profile, TOOLS,
                                              plan["plan_token"], topology=TOPOLOGY)
        self.assertFalse(result["unchanged"])
        self.assertTrue(site.call_args.kwargs["certificate_ready"])
        postgres.assert_not_called()
        backup.assert_not_called()
        recorded = self.host.registry_value["instances"][self.profile["instance_id"]]
        self.assertEqual("ready", recorded["provisioning"]["state"])
        self.assertIn("site", recorded["provisioning"]["completed_steps"])

    def test_prepared_site_reload_failure_records_failed_for_recovery(self):
        plan = self.plan()
        plan["entry"]["provisioning"] = {"state": "preparing", "completed_steps": [
            "secrets", "bootstrap_secrets", "database", "redis", "configuration", "backup", "unit", "certificate"]}
        for key in ("postgresql", "redis", "certificate"):
            plan["observations"][key] = {"state": "ready"}
        self.register(plan)
        self.profile["host"]["host_id"] = plan["host_id"]
        with patch.object(provision, "bootstrap_plan", return_value=plan), \
             patch.object(provision, "validate_runtime_environment"), \
             patch.object(provision, "initialize_bootstrap_secrets"), \
             patch.object(provision, "_ready_configuration", return_value=False), \
             patch.object(provision, "_install_unit"), \
             patch.object(provision, "_install_site", side_effect=DeployError("nginx_reload", "Reload failed")), \
             self.assertRaises(DeployError):
            provision.bootstrap_apply(self.host, self.project, self.profile, TOOLS,
                                      plan["plan_token"], topology=TOPOLOGY)
        recorded = self.host.registry_value["instances"][self.profile["instance_id"]]
        self.assertEqual("failed", recorded["provisioning"]["state"])
        self.assertNotIn("site", recorded["provisioning"]["completed_steps"])

    def test_bootstrap_configures_backup_after_database_and_private_environment(self):
        plan = self.plan()
        self.profile["host"]["host_id"] = plan["host_id"]
        calls = []
        private = {"age_recipients": ["age1fixture"]}
        credentials = {"database_password": "fixture", "redis_password": "fixture"}
        with ExitStack() as stack:
            stack.enter_context(patch.object(provision, "bootstrap_plan", return_value=plan))
            stack.enter_context(patch.object(provision.upgrades, "install_tools"))
            stack.enter_context(patch.object(provision, "_journal"))
            stack.enter_context(patch.object(provision, "_prepare_instance"))
            initialize = stack.enter_context(patch.object(provision, "initialize_bootstrap_secrets", return_value=credentials))
            for module, name, label in ((provision.packages, "install_missing", "packages"),
                    (provision.postgres, "prepare", "pg_prepare"), (provision.redis_acl, "prepare", "redis_prepare"),
                    (provision.postgres, "provision", "postgres"), (provision.redis_acl, "provision", "redis"),
                    (provision, "_provision_environment", "environment"), (provision.backup, "configure", "backup"),
                    (provision, "_install_unit", "units"), (provision, "_install_site", "site")):
                stack.enter_context(patch.object(module, name, side_effect=lambda *args, label=label: calls.append(label)))
            result = provision.bootstrap_apply(self.host, self.project, self.profile, TOOLS, plan["plan_token"],
                                               topology=TOPOLOGY, private_secrets=private)
        self.assertTrue(result["ok"])
        initialize.assert_called_once_with(self.host, plan["entry"], private)
        self.assertLess(calls.index("postgres"), calls.index("backup"))
        self.assertLess(calls.index("environment"), calls.index("backup"))
        self.assertLess(calls.index("backup"), calls.index("units"))
        self.assertIn("bootstrap_secrets", plan["entry"]["provisioning"]["completed_steps"])
        self.assertIn("backup", plan["entry"]["provisioning"]["completed_steps"])

    def bootstrap_storage(self):
        plan = self.plan()
        self.register(plan)
        registry = self.host.read_registry()
        registry["shared"]["postgresql"] = {"port": 55432}
        self.host.write_registry(registry)
        entry = registry["instances"][self.profile["instance_id"]]
        credentials = {"instance_id": self.profile["instance_id"], "database_password": "d" * 43,
                       "redis_password": "r" * 43, "app_storage": storage_fixture("initial/")}
        path = self.host.paths.instance_config(entry["name"]) / "app.env"
        self.host.owned_write(path.with_name("secrets.json"), json.dumps(credentials), 0o600)
        return registry, entry, credentials, path

    def test_bootstrap_reserves_before_first_configuration_and_registers_backup_source(self):
        registry, entry, credentials, path = self.bootstrap_storage()
        original = provision._owned_write
        observed = []
        def publish(host, destination, *args, **kwargs):
            if destination == path:
                selected = host.read_registry()["instances"][self.profile["instance_id"]]
                self.assertFalse(path.exists())
                self.assertNotIn("storage_bindings", selected)
                self.assertEqual("initial/", selected["pending_storage_bindings"]["uploads"]["namespace"])
                observed.append(True)
            return original(host, destination, *args, **kwargs)
        with patch.object(provision, "_owned_write", side_effect=publish):
            provision._provision_environment(self.host, registry, entry, credentials)
        self.assertEqual([True], observed)
        selected = self.host.read_registry()["instances"][self.profile["instance_id"]]
        self.assertNotIn("pending_storage_bindings", selected)
        self.assertIn("configuration", selected["provisioning"]["completed_steps"])
        self.assertEqual(storage_fixture("initial/"), provision.backup._runtime_storage(self.host, selected)[0])
        self.assertEqual(DEFAULT_CLEANUP_GRACE, parse_env(path.read_text())[CLEANUP_GRACE_KEY])
        self.assertNotIn("canary-secret", json.dumps(self.host.registry_value))

    def test_bootstrap_retry_selects_published_storage_instead_of_new_intake(self):
        registry, entry, credentials, path = self.bootstrap_storage()
        original = provision._owned_write
        def interrupt(host, destination, *args, **kwargs):
            result = original(host, destination, *args, **kwargs)
            if destination == path:
                raise KeyboardInterrupt()
            return result
        with patch.object(provision, "_owned_write", side_effect=interrupt), self.assertRaises(KeyboardInterrupt):
            provision._provision_environment(self.host, registry, entry, credentials)
        self.assertTrue(path.exists())
        registry = self.host.read_registry()
        entry = registry["instances"][self.profile["instance_id"]]
        self.assertIn("pending_storage_bindings", entry)
        credentials["app_storage"] = storage_fixture("unselected/")
        provision._provision_environment(self.host, registry, entry, credentials)
        selected = self.host.read_registry()["instances"][self.profile["instance_id"]]
        self.assertNotIn("pending_storage_bindings", selected)
        self.assertEqual(storage_fixture("initial/"), provision.backup._runtime_storage(self.host, selected)[0])

    def test_bootstrap_storage_collision_refuses_first_configuration(self):
        registry, entry, credentials, path = self.bootstrap_storage()
        _, _, other = fixture(project_slug="second", host_id=entry["host_id"], port=8101)
        registry["projects"][other["project_id"]] = {"slug": "second", "created_at": other["created_at"]}
        registry["instances"][other["profile"]["instance_id"]] = other
        bind_storage(registry, other, storage_fixture("initial/"))
        self.host.write_registry(registry)
        with self.assertRaises(DeployError) as caught:
            provision._provision_environment(self.host, registry, entry, credentials)
        self.assertEqual("storage_binding_conflict", caught.exception.code)
        self.assertFalse(path.exists())
        self.assertNotIn("pending_storage_bindings", self.host.read_registry()["instances"][self.profile["instance_id"]])


class MovedServiceBoundaryTests(unittest.TestCase):
    def test_postgres_refuses_foreign_or_unprepared_clusters_before_any_command(self):
        import postgres
        host = unittest.mock.Mock()
        for cluster in ({}, {"owner": "foreign", "cluster": "main", "port": 5432},
                        {"owner": "foundry", "cluster": "foundry", "port": 55432, "state": "installing"}):
            with self.subTest(cluster=cluster), self.assertRaises(DeployError):
                postgres._psql(host, {"shared": {"postgresql": cluster}}, "SELECT 1")
        host.run.assert_not_called()

    def test_postgres_role_ownership_refuses_foreign_marker(self):
        import postgres
        _, _, entry = fixture()
        role = {"marker": "foreign", "login": True, "memberships": 0, "members": 0}
        with self.assertRaises(DeployError):
            postgres._validate_pg_resources(entry, {"role": role, "database": None})

    def test_certificate_expiry_and_hostname_use_separate_checks(self):
        import certificates
        _, _, entry = fixture()
        with tempfile.TemporaryDirectory() as temporary:
            host = Host(Paths(Path(temporary)))
            certificate = host.paths.p(f"/etc/foundry/letsencrypt/live/{entry['profile']['domain']}/fullchain.pem")
            certificate.parent.mkdir(parents=True)
            certificate.write_text("certificate fixture")
            with patch.object(host, "run", return_value=subprocess.CompletedProcess([], 0, "", "")) as command:
                self.assertTrue(certificates.certificate_ready(host, entry))
            self.assertEqual(2, command.call_count)
            self.assertEqual(["-checkend", "0"], command.call_args_list[0].args[0][-2:])
            self.assertEqual(["-checkhost", entry["profile"]["domain"]], command.call_args_list[1].args[0][-2:])


if __name__ == "__main__":
    unittest.main()
