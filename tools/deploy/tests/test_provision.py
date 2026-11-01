"""Foundation bootstrap planning with read-only service adapters and no processes."""
import copy
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch
import uuid

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "lib"))

from contracts import DeployError, instance_paths, validate_registry
from host import Host, Paths
from topology import instance_units
import provision
from test_contracts import fixture

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

    def test_unimplemented_service_refuses_before_any_registry_or_resource_write(self):
        self.inspections[0].stop()
        with patch.object(provision.upgrades, "install_tools") as install:
            with self.assertRaises(NotImplementedError):
                self.plan()
            install.assert_not_called()
        self.assertEqual([], self.host.writes)
        self.assertFalse(self.host.paths.registry.exists())
        self.assertFalse(self.host.paths.opt(self.project["slug"] + "-" + self.profile["environment"]).exists())


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
            certificate = host.paths.p(f"/etc/letsencrypt/live/{entry['profile']['domain']}/fullchain.pem")
            certificate.parent.mkdir(parents=True)
            certificate.write_text("certificate fixture")
            with patch.object(host, "run", return_value=subprocess.CompletedProcess([], 0, "", "")) as command:
                self.assertTrue(certificates.certificate_ready(host, entry))
            self.assertEqual(2, command.call_count)
            self.assertEqual(["-checkend", "0"], command.call_args_list[0].args[0][-2:])
            self.assertEqual(["-checkhost", entry["profile"]["domain"]], command.call_args_list[1].args[0][-2:])


if __name__ == "__main__":
    unittest.main()
