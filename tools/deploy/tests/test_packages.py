"""Offline package transaction and interruption regressions; no host mutation."""
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "lib"))

from contracts import DeployError
from host import Host, Paths
import packages


def installed(version="1"):
    return {"status": "install ok installed", "version": version}


def simulation(names):
    return "\n".join([f"Inst {name} ({version} Ubuntu:24.04 [arm64])" for name, version in names.items()]
                     + [f"Conf {name} ({version} Ubuntu:24.04 [arm64])" for name, version in names.items()])


class PackageSimulationTests(unittest.TestCase):
    def test_pure_new_dependencies_record_exact_versions(self):
        plan = {"redis-server": "5:7.0.15-1", "redis-tools": "5:7.0.15-1"}
        self.assertEqual(plan, packages._parse_simulation(simulation(plan), {"base": installed()}))

    def test_installed_upgrade_reinstall_reconfigure_remove_all_refuse(self):
        before = {"foreign": installed()}
        for output in ("Inst foreign [1] (2 Ubuntu)\nConf foreign (2 Ubuntu)",
                       simulation({"foreign": "1"}), "Conf foreign (1 Ubuntu)",
                       "Remv foreign [1]", "Purg foreign [1]"):
            with self.subTest(output=output), self.assertRaises(DeployError) as error:
                packages._parse_simulation(output, before)
            self.assertIn("foreign", error.exception.detail)

    def test_unowned_pending_configuration_refuses(self):
        with self.assertRaises(DeployError):
            packages._parse_simulation("Conf broken (1 Ubuntu)",
                                       {"broken": {"status": "install ok unpacked", "version": "1"}})

    def test_owned_pending_configuration_requires_exact_approved_version(self):
        before = {"new": {"status": "install ok unpacked", "version": "1"}}
        self.assertEqual({"new": "1"}, packages._parse_simulation("Conf new (1 Ubuntu)", before, approved={"new": "1"}))
        with self.assertRaises(DeployError):
            packages._parse_simulation("Conf new (2 Ubuntu)", before, approved={"new": "1"})

    def test_resume_cannot_add_dependency_outside_journal(self):
        with self.assertRaises(DeployError):
            packages._parse_simulation(simulation({"surprise": "1"}), {}, approved={"new": "1"})

    def test_half_installed_upgrade_style_action_refuses_conservatively(self):
        with self.assertRaises(DeployError):
            packages._parse_simulation("Inst new [1] (1 Ubuntu)\nConf new (1 Ubuntu)",
                                       {"new": {"status": "install reinstreq half-installed", "version": "1"}},
                                       approved={"new": "1"})

    def test_resume_accepts_native_multiarch_status_names(self):
        before = {"foreign:arm64": installed()}
        current = {**before, "libpq5:arm64": installed("18.1")}
        packages._check_resume(current, {"before": before, "transaction": {"libpq5": "18.1"}})
        self.assertTrue(packages._installed(current, "libpq5"))
        current["libpq5:amd64"] = installed("18.1")
        with self.assertRaises(DeployError):
            packages._check_resume(current, {"before": before, "transaction": {"libpq5": "18.1"}})


class PackageHost(Host):
    def __init__(self, root):
        super().__init__(Paths(root))
        self.inventory = {"foreign": installed()}
        self.candidates = {"new": "1"}
        self.commands = []
        self.interrupt = False
        self.new_main = False
        self.main_running = False
        self.paths.p("/usr/sbin").mkdir(parents=True)
        os_release = self.paths.p("/etc/os-release")
        os_release.parent.mkdir(parents=True)
        os_release.write_text('ID=ubuntu\nVERSION_ID="24.04"\n')

    def run(self, args, **kwargs):
        self.commands.append((list(args), kwargs))
        output = ""
        if args[0] == "dpkg-query":
            output = "\n".join(f"{name}\t{value['status']}\t{value['version']}" for name, value in self.inventory.items())
        elif args[0] == "systemctl":
            if args[1] == "disable":
                pass
            else:
                exists = self.paths.p("/etc/postgresql/16/main").exists() and args[2] == "postgresql@16-main.service"
                output = ("LoadState=loaded\nActiveState=" + ("active\nMainPID=42" if self.main_running else "inactive\nMainPID=0")) if exists else "LoadState=not-found\nActiveState=inactive\nMainPID=0"
        elif args[:2] == ["apt-get", "-s"]:
            targets = {name: version for name, version in self.candidates.items() if not packages._installed(self.inventory, name)}
            output = simulation(targets)
        elif args[0] in {"apt-get", "dpkg"}:
            assert self.paths.p(packages.POLICY).read_text().endswith("exit 101\n")
            assert kwargs["env"]["NEEDRESTART_SUSPEND"] == "1"
            assert packages._load(self)["state"] == "installing"
            if self.interrupt:
                self.interrupt = False
                self.inventory["new"] = {"status": "install ok unpacked", "version": "1"}
                raise DeployError("injected", "Interrupted after unpacking.")
            self.inventory.update({name: installed(version) for name, version in self.candidates.items()})
            if self.new_main:
                config = self.paths.p("/etc/postgresql/16/main/start.conf")
                config.parent.mkdir(parents=True, exist_ok=True)
                config.write_text("auto\n")
        else:
            raise AssertionError(args)
        return subprocess.CompletedProcess(args, 0, output, "")


class PackageTransactionTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.host = PackageHost(Path(temporary.name))

    def test_missing_install_is_pinned_guarded_and_restores_policy(self):
        packages.install_missing(self.host, ["new", "foreign"])
        command, kwargs = next((args, opts) for args, opts in self.host.commands if args[:2] == ["apt-get", "-y"])
        self.assertIn("new=1", command)
        self.assertNotIn("foreign", command)
        self.assertEqual("1", kwargs["env"]["NEEDRESTART_SUSPEND"])
        self.assertFalse(self.host.paths.p(packages.POLICY).exists())
        state = packages._load(self.host)
        self.assertEqual("complete", state["state"])
        self.assertEqual("restored", state["policy_state"])
        self.assertEqual(installed(), self.host.inventory["foreign"])

    def test_existing_policy_refuses_and_keeps_bytes(self):
        policy = self.host.paths.p(packages.POLICY)
        policy.write_text("#!/bin/sh\nexit 0\n")
        before = policy.read_bytes()
        with self.assertRaises(DeployError):
            packages.install_missing(self.host, ["new"])
        self.assertEqual(before, policy.read_bytes())
        self.assertFalse(packages._journal_path(self.host).exists())

    def test_existing_policy_symlink_is_never_followed_or_replaced(self):
        self.host.paths.p(packages.POLICY).symlink_to("missing-foreign-policy")
        with self.assertRaises(DeployError):
            packages.install_missing(self.host, ["new"])
        self.assertTrue(self.host.paths.p(packages.POLICY).is_symlink())

    def test_busy_dpkg_refuses_without_intent(self):
        with patch.object(packages, "_locks", return_value=["/var/lib/dpkg/lock"]), self.assertRaises(DeployError):
            packages.install_missing(self.host, ["new"])
        self.assertFalse(packages._journal_path(self.host).exists())

    def test_owned_partial_install_resumes_even_with_empty_missing_list(self):
        self.host.interrupt = True
        with self.assertRaises(DeployError):
            packages.install_missing(self.host, ["new"])
        self.assertFalse(self.host.paths.p(packages.POLICY).exists())
        self.assertEqual("installing", packages._load(self.host)["state"])
        packages.install_missing(self.host, [])
        self.assertIn(["dpkg", "--configure", "new"], [args for args, _ in self.host.commands])
        self.assertEqual("complete", packages._load(self.host)["state"])

    def test_unrelated_broken_package_blocks_owned_recovery(self):
        self.host.interrupt = True
        with self.assertRaises(DeployError):
            packages.install_missing(self.host, ["new"])
        self.host.inventory["foreign"]["status"] = "install ok unpacked"
        self.host.commands.clear()
        with self.assertRaises(DeployError):
            packages.install_missing(self.host, [])
        self.assertFalse(any(args[0] == "dpkg" for args, _ in self.host.commands))

    def test_recovery_refuses_modified_policy_without_removing_it(self):
        self.host.interrupt = True
        with self.assertRaises(DeployError):
            packages.install_missing(self.host, ["new"])
        policy = self.host.paths.p(packages.POLICY)
        policy.write_text("foreign replacement\n")
        with self.assertRaises(DeployError):
            packages.install_missing(self.host, [])
        self.assertEqual("foreign replacement\n", policy.read_text())

    def test_policy_published_before_crash_is_recovered_from_intent(self):
        with patch.object(packages, "_restore_policy", side_effect=SystemExit), self.assertRaises(SystemExit):
            packages.install_missing(self.host, ["new"])
        self.assertTrue(self.host.paths.p(packages.POLICY).exists())
        self.assertEqual("complete", packages._load(self.host)["state"])
        packages.install_missing(self.host, [])
        self.assertFalse(self.host.paths.p(packages.POLICY).exists())

    def test_surviving_package_process_keeps_policy_and_blocks_recovery(self):
        with patch.object(packages, "_restore_policy", side_effect=SystemExit), self.assertRaises(SystemExit):
            packages.install_missing(self.host, ["new"])
        journal = packages._load(self.host)
        journal["state"] = "installing"
        journal["process_group"] = 1234
        packages._save(self.host, journal)
        process = self.host.paths.p("/proc/1235/stat")
        process.parent.mkdir(parents=True)
        process.write_text("1235 (maintainer) S 1 1234 1234 0\n")
        with self.assertRaises(DeployError) as error:
            packages.install_missing(self.host, [])
        self.assertEqual("package_locked", error.exception.code)
        self.assertTrue(self.host.paths.p(packages.POLICY).exists())
        process.unlink()
        packages.install_missing(self.host, [])
        self.assertFalse(self.host.paths.p(packages.POLICY).exists())

    def test_new_main_is_preserved_and_manual_only_with_fresh_journal(self):
        self.host.candidates = {"postgresql-16": "16.4"}
        self.host.new_main = True
        packages.install_missing(self.host, ["postgresql-16"])
        config = self.host.paths.p("/etc/postgresql/16/main/start.conf")
        self.assertTrue(config.exists())
        self.assertIn("manual", config.read_text())
        self.assertIn("16/main", packages._load(self.host)["package_created_unused"])
        self.assertFalse(any("pg_dropcluster" in args or "restart" in args or "stop" in args for args, _ in self.host.commands))

    def test_preexisting_main_bytes_and_service_are_untouched(self):
        config = self.host.paths.p("/etc/postgresql/16/main/start.conf")
        config.parent.mkdir(parents=True)
        config.write_bytes(b"# foreign\r\nauto\r\n")
        before = config.read_bytes()
        packages.install_missing(self.host, ["new"])
        self.assertEqual(before, config.read_bytes())
        self.assertFalse(any(args[:2] == ["systemctl", "disable"] for args, _ in self.host.commands))

    def test_unexpected_package_autostart_refuses_without_stopping_service(self):
        self.host.candidates = {"postgresql-16": "16.4"}
        self.host.new_main = self.host.main_running = True
        with self.assertRaises(DeployError) as error:
            packages.install_missing(self.host, ["postgresql-16"])
        self.assertEqual("package_autostart", error.exception.code)
        self.assertEqual("auto\n", self.host.paths.p("/etc/postgresql/16/main/start.conf").read_text())
        self.assertEqual("installing", packages._load(self.host)["state"])
        self.assertFalse(any("stop" in args for args, _ in self.host.commands))

    def test_inspection_is_read_only_and_pending_precedes_broken_classification(self):
        self.host.interrupt = True
        with self.assertRaises(DeployError):
            packages.install_missing(self.host, ["new"])
        path = packages._journal_path(self.host)
        before = path.read_bytes()
        with patch.object(packages, "_required", return_value=["new"]):
            result = packages.inspect(self.host)
        self.assertEqual("resume", result["state"])
        self.assertEqual(before, path.read_bytes())
        self.assertNotIn("policy", result)

    def test_ubuntu_distro_metadata_symlink_supports_both_majors(self):
        path = self.host.paths.p("/etc/os-release")
        path.unlink()
        target = self.host.paths.p("/usr/lib/os-release")
        target.parent.mkdir(parents=True)
        path.symlink_to("../usr/lib/os-release")
        target.write_text('ID=ubuntu\nVERSION_ID="26.04"\n')
        self.assertEqual(18, packages.ubuntu_major(self.host))


if __name__ == "__main__":
    unittest.main()
