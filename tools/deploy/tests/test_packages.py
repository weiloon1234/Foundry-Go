"""Offline package transaction and interruption regressions; no host mutation."""
from pathlib import Path
import subprocess
import copy
import contextlib
import hashlib
import io
import json
import sys
import tempfile
import tarfile
import os
import unittest
from unittest.mock import Mock, patch

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

    def test_refusal_names_the_installed_packages_apt_would_change(self):
        before = {"perl-base": installed("5.38.2-3.2build2"), "old-tool": installed("1")}
        output = ("Inst perl-base [5.38.2-3.2build2] (5.38.2-3.2ubuntu0.2 Ubuntu:24.04 [amd64])\n"
                  "Inst pgbackrest (2.50-1build2 Ubuntu:24.04 [amd64])\n"
                  "Remv old-tool [1]\n"
                  "Conf perl-base (5.38.2-3.2ubuntu0.2 Ubuntu:24.04 [amd64])\n"
                  "Conf pgbackrest (2.50-1build2 Ubuntu:24.04 [amd64])\n"
                  "1 upgraded, 1 newly installed, 1 to remove and 0 not upgraded.")
        with self.assertRaises(DeployError) as error:
            packages._parse_simulation(output, before)
        self.assertEqual("package_plan_refused", error.exception.code)
        self.assertIn("it would change installed packages: old-tool 1 -> removed, "
                      "perl-base 5.38.2-3.2build2 -> 5.38.2-3.2ubuntu0.2", error.exception.detail)

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
        self.activation = {}
        self.activation_classification = {}
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
            if args[1] in ("disable", "daemon-reload"):
                pass
            elif args[1] in ("list-unit-files", "list-units"):
                output = "\n".join(name + " " + record.get("UnitFileState", "disabled")
                                   for name, record in sorted(self.activation.items()))
            elif args[1] == "show" and "Id" in args:
                records = []
                properties = {args[index + 1] for index, arg in enumerate(args) if arg == "-p"}
                for name in args[2:args.index("--no-pager")]:
                    masked = self.paths.p("/etc/systemd/system/" + name).is_symlink()
                    record = {"LoadState": "masked" if masked else "loaded", "ActiveState": "inactive",
                              "SubState": "dead", "MainPID": "0", "UnitFileState": "masked" if masked else "disabled",
                              "FragmentPath": "", **self.activation.get(name, {})}
                    record.update({"Id": name, "Type": "simple", "TriggeredBy": "", "Transient": "no",
                                   **self.activation_classification.get(name, {})})
                    records.append("\n".join(key + "=" + value for key, value in record.items() if key in properties))
                output = "\n\n".join(records)
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
        for name, kwargs in (
                ("_candidate_evidence", {"side_effect": lambda host, transaction: {
                    name: {"version": version, "sha256": "a" * 64} for name, version in transaction.items()}}),
                ("_prepare_payloads", {"return_value": ({}, {})}),
                ("_activation_snapshot", {"return_value": {}})):
            patcher = patch.object(packages, name, **kwargs)
            patcher.start()
            self.addCleanup(patcher.stop)

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
        with patch.object(packages, "_locks", return_value=["/var/lib/dpkg/lock"]), \
                patch.object(packages, "LOCK_WAIT_SECONDS", 0), self.assertRaises(DeployError):
            packages.install_missing(self.host, ["new"])
        self.assertFalse(packages._journal_path(self.host).exists())

    def test_busy_dpkg_waits_for_a_daily_upgrade_to_finish(self):
        busy = ["/var/lib/dpkg/lock"]
        with patch.object(packages, "_locks", side_effect=[busy, busy, [], []]), \
                patch.object(packages.time, "sleep") as sleep, \
                patch.object(packages, "_transaction_active", side_effect=DeployError("past_the_lock", "reached")), \
                self.assertRaises(DeployError) as error:
            packages.install_missing(self.host, ["new"])
        self.assertEqual("past_the_lock", error.exception.code)
        self.assertEqual(2, sleep.call_count)

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

    def test_new_main_is_accepted_after_a_dependency_created_empty_roots(self):
        # Recovery installs pgBackRest first, whose postgresql-common creates these.
        self.host.paths.p("/etc/postgresql").mkdir(parents=True)
        self.host.paths.p("/var/lib/postgresql").mkdir(parents=True)
        self.host.candidates = {"postgresql-16": "16.4"}
        self.host.new_main = True
        packages.install_missing(self.host, ["postgresql-16"])
        self.assertIn("16/main", packages._load(self.host)["package_created_unused"])

    def test_new_main_is_refused_when_a_postgresql_root_holds_state(self):
        leftover = self.host.paths.p("/var/lib/postgresql/16/main/PG_VERSION")
        leftover.parent.mkdir(parents=True)
        leftover.write_text("16\n")
        self.host.candidates = {"postgresql-16": "16.4"}
        self.host.new_main = True
        with self.assertRaises(DeployError) as error:
            packages.install_missing(self.host, ["postgresql-16"])
        self.assertEqual("package_cluster_conflict", error.exception.code)

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

    def test_reviewed_hash_or_activation_drift_refuses_before_install(self):
        with patch.object(packages, "_required", return_value=["new"]):
            plan = packages.inspect(self.host)
        for key in ("transaction", "evidence", "activation", "installed"):
            changed = copy.deepcopy(plan)
            changed[key] = {"unexpected": {}}
            with self.subTest(key=key), self.assertRaises(DeployError) as error:
                packages.install_missing(self.host, ["new"], reviewed=changed)
            self.assertEqual(error.exception.code, "package_plan_changed")
            self.assertFalse(packages._journal_path(self.host).exists())
        packages.install_missing(self.host, ["new"], reviewed=plan)
        self.assertEqual(packages._load(self.host)["state"], "complete")

    def test_dependency_hash_drift_blocks_resume_before_configuration(self):
        self.host.interrupt = True
        with self.assertRaises(DeployError):
            packages.install_missing(self.host, ["new"])
        self.host.commands.clear()
        with patch.object(packages, "_candidate_evidence", return_value={"new": {"version": "1", "sha256": "b" * 64}}):
            with self.assertRaises(DeployError) as error:
                packages.install_missing(self.host, [])
        self.assertEqual(error.exception.code, "package_plan_changed")
        self.assertFalse(any(args[0] in ("apt-get", "dpkg") for args, _ in self.host.commands))


class RolePackageTests(unittest.TestCase):
    def test_only_placed_roles_request_packages_and_removed_requirements_are_retained(self):
        from test_host_capabilities import profile
        from topology import parse_profile, parse_topology
        topology = parse_topology({"schema": 2, "roles": [
            {"name": "http", "kind": "builtin", "packages": ["shared", "cli"]},
            {"name": "worker", "kind": "builtin", "packages": ["shared", "worker-cli"]},
            {"name": "scheduler", "kind": "builtin", "packages": ["unused"]}]})
        placement = parse_profile({**profile(), "placement": {"http": 1, "worker": 1, "scheduler": 0}, "listeners": {}}, topology)
        self.assertEqual(packages.role_requirements(topology, placement),
                         {"cli": ["http"], "shared": ["http", "worker"], "worker-cli": ["worker"]})
        with tempfile.TemporaryDirectory() as root:
            host = PackageHost(Path(root))
            host.inventory = {name: installed() for name in ("shared", "cli", "worker-cli", "old")}
            with patch.object(packages, "_required", return_value=[]), patch.object(packages, "_activation_snapshot", return_value={}):
                plan = packages.inspect(host, topology, placement, previous_requirements=["old", "shared"])
            self.assertEqual(plan["missing"], [])
            self.assertEqual(plan["retained_requirements"], ["old"])
            self.assertFalse(any(args[0] in ("apt-get", "dpkg") for args, _ in host.commands))

    def evidence(self):
        return {"new": {"version": "1", "architecture": "amd64", "sha256": "a" * 64, "size": 100,
                        "uris": ["https://archive.ubuntu.com/ubuntu/pool/new.deb"],
                        "origins": [{"trusted": True, "origin": "Ubuntu", "label": "Ubuntu",
                                     "codename": "noble", "archive": "noble-updates", "site": "archive.ubuntu.com"}]}}

    def test_all_dependency_origins_must_be_signed_distro_records(self):
        evidence = self.evidence()
        packages._validate_evidence(evidence, {"new": "1"}, "noble")
        for key, value in (("trusted", False), ("origin", "foreign"), ("codename", "other"), ("archive", "noble-proposed")):
            bad = copy.deepcopy(evidence)
            bad["new"]["origins"][0][key] = value
            with self.subTest(key=key), self.assertRaises(DeployError):
                packages._validate_evidence(bad, {"new": "1"}, "noble")
        for uri in ("https://user:password@example.org/p.deb", "file:///tmp/p.deb", "https://archive.ubuntu.com/p?token=x"):
            bad = copy.deepcopy(evidence)
            bad["new"]["uris"] = [uri]
            with self.subTest(uri=uri), self.assertRaises(DeployError):
                packages._validate_evidence(bad, {"new": "1"}, "noble")
        mirrored = copy.deepcopy(evidence)
        mirrored["new"]["uris"] = ["https://mirror.example.org/ubuntu/pool/new.deb"]
        packages._validate_evidence(mirrored, {"new": "1"}, "noble")
        with self.assertRaises(DeployError):
            packages._validate_evidence(evidence, {"new": "1", "dependency": "2"}, "noble")

    def test_package_executable_requires_exact_owner_and_safe_ancestry(self):
        from topology import PackageExecutable
        with tempfile.TemporaryDirectory() as root:
            host = PackageHost(Path(root).resolve())
            path = host.paths.p("/usr/bin/cli")
            path.parent.mkdir(parents=True)
            path.write_bytes(b"executable")
            path.chmod(0o755)
            reference = PackageExecutable("new", "/usr/bin/cli")
            with patch.object(host, "run", return_value=subprocess.CompletedProcess([], 0, "new: /usr/bin/cli\n")):
                result = packages.executable_evidence(host, reference, {"new": installed("2")})
                self.assertEqual(result["version"], "2")
                self.assertEqual(result["path"], "/usr/bin/cli")
            with patch.object(host, "run", return_value=subprocess.CompletedProcess([], 0, "foreign: /usr/bin/cli\n")):
                with self.assertRaises(DeployError):
                    packages.executable_evidence(host, reference, {"new": installed()})
            path.chmod(0o777)
            with self.assertRaises(DeployError):
                packages.executable_evidence(host, reference, {"new": installed()})


class SignedIndexTests(unittest.TestCase):
    def evidence(self, *, altered_index=False, signature_failure=False):
        index = ("Package: new\nVersion: 1\nArchitecture: amd64\nFilename: pool/main/n/new.deb\n"
                 "SHA256: " + "a" * 64 + "\nSize: 100\n\n").encode()
        checksum = hashlib.sha256(index).hexdigest()
        cleartext = ("Origin: Ubuntu\nLabel: Ubuntu\nSuite: noble-updates\nCodename: noble\nSHA256:\n "
                     + checksum + " " + str(len(index)) + " main/binary-amd64/Packages\n").encode()
        target = ("Identifier: Packages\nMetaKey: main/binary-amd64/Packages\nTrusted: yes\n"
                  "Filename: /var/lib/apt/lists/archive.ubuntu.com_ubuntu_dists_noble-updates_main_binary-amd64_Packages.lz4\n"
                  "URI: https://archive.ubuntu.com/ubuntu/dists/noble-updates/main/binary-amd64/Packages\n\n").encode()
        calls = []
        def command(args, **kwargs):
            calls.append(args)
            if args[0] == "dpkg":
                self.assertEqual(args, ["dpkg", "--print-architecture"])
                output = b"amd64\n"
            elif args[0] == "apt-get":
                self.assertEqual(args, ["apt-get", "indextargets"])
                output = target
            else:
                self.assertEqual(args[:5], ["gpgv", "--keyring", "/usr/share/keyrings/ubuntu-archive-keyring.gpg", "--output", "-"])
                if signature_failure:
                    raise subprocess.CalledProcessError(1, args)
                output = cleartext
            kwargs["stdout"].write(output)
            return subprocess.CompletedProcess(args, 0)
        process = Mock()
        process.stdout = io.BytesIO(index.replace(b"Size: 100", b"Size: 200") if altered_index else index)
        process.wait.return_value = process.poll.return_value = 0
        output = io.StringIO()
        with patch.object(subprocess, "run", side_effect=command), patch.object(subprocess, "Popen", return_value=process), \
                patch.object(sys, "argv", ["supply", '{"new":"1"}']), contextlib.redirect_stdout(output):
            exec(packages._APT_EVIDENCE, {"__name__": "__main__"})
        return json.loads(output.getvalue()), calls

    def test_metadata_is_bound_to_index_digest_and_distro_signature_without_python_apt(self):
        records, commands = self.evidence()
        packages._validate_evidence(records, {"new": "1"}, "noble")
        self.assertEqual(records["new"]["sha256"], "a" * 64)
        self.assertEqual(records["new"]["uris"], ["https://archive.ubuntu.com/ubuntu/pool/main/n/new.deb"])
        self.assertEqual(len(commands), 3)

    def test_unsigned_or_changed_index_cannot_supply_reviewed_package_hashes(self):
        with self.assertRaises(ValueError):
            self.evidence(altered_index=True)
        with self.assertRaises(subprocess.CalledProcessError):
            self.evidence(signature_failure=True)


class DefaultActivationTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.host = PackageHost(Path(temporary.name))
        self.saved = patch.object(packages, "_save")
        self.saved.start()
        self.addCleanup(self.saved.stop)

    def record(self, active="inactive", enabled="disabled"):
        return {"LoadState": "loaded", "ActiveState": active, "SubState": "dead",
                "UnitFileState": enabled, "MainPID": "0"}

    def test_masks_only_proven_new_defaults_including_sockets_and_timers(self):
        journal = {"operation_id": "test-operation", "activation_before": {"foreign.service": self.record("active", "enabled")},
                   "activation_units": {name: ["new"] for name in ("new.service", "new.socket", "new.timer", "foreign.service")}}
        packages._mask_defaults(self.host, journal)
        for name in ("new.service", "new.socket", "new.timer"):
            self.assertEqual(self.host.paths.p("/etc/systemd/system/" + name).readlink(), Path("/dev/null"))
        self.assertFalse(self.host.paths.p("/etc/systemd/system/foreign.service").exists())
        packages._mask_defaults(self.host, journal)
        self.assertEqual(len(journal["masked_defaults"]), 3)

    def test_foreign_mask_and_preexisting_template_instances_refuse(self):
        directory = self.host.paths.p("/etc/systemd/system")
        directory.mkdir(parents=True)
        (directory / "new.service").symlink_to("/dev/null")
        for before, units in (({}, {"new.service": ["new"]}),
                              ({"new@foreign.service": self.record()}, {"new@.service": ["new"]})):
            with self.assertRaises(DeployError):
                packages._mask_defaults(self.host, {"operation_id": "test-operation", "activation_before": before, "activation_units": units})

    def test_unexpected_or_active_defaults_remain_unresolved_without_stopping_foreign_units(self):
        journal = {"activation_before": {"foreign.service": self.record("active", "enabled")},
                   "activation_units": {"new.socket": ["new"]}}
        for change in ({"new.socket": self.record("active")}, {"surprise.timer": self.record()},
                       {"foreign.service": self.record("inactive")}):
            with patch.object(packages, "_activation_snapshot", return_value={**journal["activation_before"], **change}):
                with self.assertRaises(DeployError):
                    packages._finish_activation(self.host, journal)
        self.assertFalse(any("stop" in args or "disable" in args for args, _ in self.host.commands))

    def test_inactive_new_default_disabled_while_foreign_activation_is_preserved(self):
        before = {"foreign.service": self.record("active", "enabled")}
        journal = {"activation_before": before, "activation_units": {"new.timer": ["new"]}}
        with patch.object(packages, "_activation_snapshot", return_value={**before, "new.timer": self.record()}):
            packages._finish_activation(self.host, journal)
        self.assertEqual([args for args, _ in self.host.commands if args[1] != "show"],
                         [["systemctl", "disable", "new.timer"]])


CERTBOT_CRON = b"0 */12 * * * root test -x /usr/bin/certbot -a ! -d /run/systemd/system && certbot -q renew\n"


class PayloadPackageHost(PackageHost):
    """Run the package planner/inspector/journal; fake only OS command boundaries."""
    def __init__(self, root, payloads):
        super().__init__(root)
        self.payloads = payloads
        self.conffiles = {path for files in payloads.values() for path in files if path.startswith("/etc/cron.")}
        self.candidates = {name: "1" for name in payloads}
        self.active = set()
        self.paths.p("/etc/os-release").write_text('ID=ubuntu\nVERSION_ID="24.04"\nVERSION_CODENAME=noble\n')
        cache = self.paths.p("/var/cache/apt/archives")
        cache.mkdir(parents=True)
        self.evidence = {}
        for name, files in payloads.items():
            output = io.BytesIO()
            with tarfile.open(fileobj=output, mode="w") as archive:
                for path, content in files.items():
                    item = tarfile.TarInfo("./" + path.lstrip("/"))
                    item.size, item.mode = len(content), 0o644
                    archive.addfile(item, io.BytesIO(content))
            content = output.getvalue()
            (cache / (name + ".deb")).write_bytes(content)
            self.evidence[name] = {"version": "1", "architecture": "amd64", "sha256": hashlib.sha256(content).hexdigest(),
                "size": len(content), "uris": ["https://archive.ubuntu.com/ubuntu/pool/" + name + ".deb"],
                "origins": [{"trusted": True, "origin": "Ubuntu", "label": "Ubuntu", "codename": "noble", "archive": "noble"}]}

    def units(self):
        return {path.name for directory in ("/etc/systemd/system", "/usr/lib/systemd/system")
                for path in self.paths.p(directory).glob("*") if packages.UNIT.fullmatch(path.name)} | self.activation.keys()

    def run(self, args, **kwargs):
        output = ""
        if args[:3] == ["/usr/bin/python3", "-c", packages._APT_EVIDENCE]:
            output = json.dumps({name: self.evidence[name] for name in json.loads(args[3])})
        elif args[:3] == ["/usr/bin/python3", "-c", packages._DEB_UNITS]:
            process = Mock(stdout=io.BytesIO(Path(args[3]).read_bytes()))
            process.wait.return_value = process.poll.return_value = 0
            captured = io.StringIO()
            def control(command, **options):
                assert command == ["dpkg-deb", "--ctrl-tarfile", args[3]]
                with tarfile.open(fileobj=options["stdout"], mode="w") as archive:
                    data = ("\n".join(sorted(self.conffiles)) + "\n").encode()
                    item = tarfile.TarInfo("conffiles")
                    item.size = len(data)
                    archive.addfile(item, io.BytesIO(data))
                return subprocess.CompletedProcess(command, 0)
            with patch.object(subprocess, "Popen", return_value=process), patch.object(sys, "argv", ["inspector", *args[3:]]), \
                    patch.object(subprocess, "run", side_effect=control), contextlib.redirect_stdout(captured):
                exec(packages._DEB_UNITS, {"__name__": "__main__"})
            output = captured.getvalue()
        elif args[0] == "apt-get" and "--download-only" in args:
            pass
        elif args[0] == "apt-get" and "-y" in args:
            assert self.paths.p(packages.POLICY).read_text().endswith("exit 101\n")
            for location in packages._load(self)["cron_defaults"]:
                assert all(line.startswith("#") for line in self.paths.p(location).read_text().splitlines())
            for name, files in self.payloads.items():
                for path, content in files.items():
                    destination = self.paths.p(path)
                    destination.parent.mkdir(parents=True, exist_ok=True)
                    if path.startswith("/etc/cron.") and destination.exists() and "Dpkg::Options::=--force-confold" in args:
                        destination = destination.with_name(destination.name + ".dpkg-dist")
                    destination.write_bytes(content)
                self.inventory[name] = installed()
        elif args[0] == "systemctl" and args[1] in ("list-unit-files", "list-units"):
            output = "\n".join(name + " " + self.activation.get(name, {}).get("UnitFileState", "disabled")
                               for name in sorted(self.units()))
        elif args[:3] == ["systemctl", "enable", "--now"]:
            unit = args[3] if args[3].endswith(".service") else args[3] + ".service"
            if self.paths.p("/etc/systemd/system/" + unit).is_symlink():
                raise DeployError("masked", "Cannot enable a masked service.")
            self.active.add(unit)
        else:
            return super().run(args, **kwargs)
        self.commands.append((list(args), kwargs))
        return subprocess.CompletedProcess(args, 0, output, "")


class ActivationComparisonTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.host = PayloadPackageHost(Path(temporary.name).resolve(), {"new": {}})

    def record(self, name, *, active="active", state="enabled", pid="1171"):
        return {"LoadState": "loaded", "ActiveState": active, "SubState": "running" if active == "active" else "dead",
                "MainPID": pid, "UnitFileState": state, "FragmentPath": "/usr/lib/systemd/system/" + name}

    def cloud_activation(self):
        for name in ("fwupd.service", "packagekit.service"):
            self.host.activation[name] = self.record(name, active="inactive", state="static", pid="0")
            self.host.activation_classification[name] = {"Type": "dbus"}
        self.host.activation["user@0.service"] = self.record("user@0.service", state="static")

    def churn(self):
        for record in self.host.activation.values():
            record.update(ActiveState="active", SubState="running", MainPID="8400")

    def install_with_churn(self):
        run = self.host.run

        def command(args, **kwargs):
            result = run(args, **kwargs)
            if args[:2] == ["apt-get", "-y"] and "--download-only" not in args:
                self.churn()
            return result

        with patch.object(self.host, "run", side_effect=command):
            packages.install_missing(self.host, ["new"])

    def test_stock_ubuntu_dbus_and_root_session_churn_completes_install(self):
        self.cloud_activation()
        before = copy.deepcopy(self.host.activation)
        self.install_with_churn()
        journal = packages._load(self.host)
        self.assertEqual(journal["state"], "complete")
        self.assertEqual(journal["activation_before"], before)
        self.assertEqual(journal["policy_state"], "restored")

    def test_v0384_installed_pending_journal_resumes_all_finish_checks(self):
        self.cloud_activation()

        def old_finish(host, journal):
            packages.require(journal["activation_before"] == packages._activation_snapshot(host),
                             "package_service_changed", "Old comparison refused activation churn.")

        with patch.object(packages, "_finish_activation", side_effect=old_finish), self.assertRaises(DeployError) as error:
            self.install_with_churn()
        self.assertEqual(error.exception.code, "package_service_changed")
        before = packages._load(self.host)
        self.assertEqual(before["state"], "installing")
        self.assertTrue(packages._installed(self.host.inventory, "new"))
        for record in before["activation_before"].values():
            self.assertEqual(set(record), {"LoadState", "ActiveState", "SubState", "MainPID", "UnitFileState", "FragmentPath"})
        self.churn()
        self.host.activation["user@0.service"]["MainPID"] = "9000"
        self.host.commands.clear()
        with patch.object(packages, "_required", return_value=[]):
            plan = packages.inspect(self.host)
        self.assertEqual(plan["state"], "resume")
        self.assertEqual(plan["missing"], [])
        with contextlib.ExitStack() as stack:
            finish = [stack.enter_context(patch.object(packages, name, wraps=getattr(packages, name)))
                      for name in ("_finish_defaults", "_finish_activation", "_finish_cron")]
            packages.install_missing(self.host, plan["missing"], reviewed=plan)
            for check in finish:
                check.assert_called_once()
        after = packages._load(self.host)
        self.assertEqual(after["state"], "complete")
        self.assertEqual(after["policy_state"], "restored")
        self.assertEqual(after["activation_before"], before["activation_before"])
        self.assertEqual(after["supply_sha256"], before["supply_sha256"])
        self.assertFalse(any(args[0] in ("apt-get", "dpkg") for args, _ in self.host.commands))

    def test_reviewed_plan_accepts_root_session_pid_change(self):
        name = "user@0.service"
        self.host.activation[name] = self.record(name)
        with patch.object(packages, "_required", return_value=["new"]):
            plan = packages.inspect(self.host)
        self.host.activation[name]["MainPID"] = "8400"
        packages.install_missing(self.host, ["new"], reviewed=plan)
        self.assertEqual(packages._load(self.host)["state"], "complete")

    def test_payload_acquisition_uses_same_runtime_comparison(self):
        name = "user@0.service"
        self.host.activation[name] = self.record(name)
        prepare = packages._prepare_payloads

        def acquire(*args):
            result = prepare(*args)
            self.host.activation[name]["MainPID"] = "8400"
            return result

        with patch.object(packages, "_prepare_payloads", side_effect=acquire):
            packages.install_missing(self.host, ["new"])
        self.assertEqual(packages._load(self.host)["state"], "complete")

    def test_daemon_started_during_acquisition_is_the_install_baseline(self):
        name = "ssh.service"
        self.host.activation[name] = self.record(name, active="inactive", pid="0")
        self.host.activation_classification[name] = {"TriggeredBy": "ssh.socket", "Type": "notify"}
        prepare, run = packages._prepare_payloads, self.host.run

        def acquire(*args):
            result = prepare(*args)
            self.host.activation[name].update(ActiveState="active", SubState="running", MainPID="900")
            return result

        def command(args, **kwargs):
            result = run(args, **kwargs)
            if args[:2] == ["apt-get", "-y"] and "--download-only" not in args:
                self.host.activation[name].update(ActiveState="inactive", SubState="dead", MainPID="0")
            return result

        with patch.object(packages, "_prepare_payloads", side_effect=acquire), \
                patch.object(self.host, "run", side_effect=command), self.assertRaises(DeployError) as error:
            packages.install_missing(self.host, ["new"])
        self.assertEqual(error.exception.code, "package_service_changed")
        self.assertEqual(packages._load(self.host)["activation_before"][name]["MainPID"], "900")

    def test_independent_service_runtime_changes_still_refuse(self):
        cases = [("cron.service", "enabled", "simple", {"MainPID": "8400"}),
                 ("dbus.service", "static", "notify", {"MainPID": "8400"}),
                 ("cron.service", "enabled", "simple", {"ActiveState": "inactive", "MainPID": "0"}),
                 ("cron.service", "enabled", "simple", {"ActiveState": "failed", "SubState": "failed"}),
                 ("indirect.service", "indirect", "simple", {"MainPID": "8400"}),
                 ("generated.service", "generated", "forking", {"MainPID": "8400"}),
                 ("enabled-dbus.service", "enabled", "dbus", {"MainPID": "8400"})]
        for name, state, kind, change in cases:
            with self.subTest(name=name, change=change):
                before = {name: self.record(name, state=state)}
                self.host.activation = {name: {**before[name], **change}}
                self.host.activation_classification = {name: {"Type": kind}}
                with self.assertRaises(DeployError) as error:
                    packages._finish_activation(self.host, {"activation_before": before, "activation_units": {}})
                self.assertEqual(error.exception.code, "package_service_changed")
        name = "cron.service"
        before = {name: self.record(name, active="inactive", pid="0")}
        self.host.activation = {name: self.record(name)}
        with self.assertRaises(DeployError) as error:
            packages._finish_activation(self.host, {"activation_before": before, "activation_units": {}})
        self.assertEqual(error.exception.code, "package_service_changed")

    def test_session_and_on_demand_configuration_changes_always_refuse(self):
        for name, kind in (("user@0.service", {}), ("user-runtime-dir@42.service", {}),
                           ("packagekit.service", {"Type": "dbus"}),
                           ("refresh.service", {"TriggeredBy": "refresh.timer"})):
            for field, value in (("LoadState", "not-found"), ("UnitFileState", "enabled"),
                                 ("FragmentPath", "/etc/systemd/system/replacement.service")):
                with self.subTest(name=name, field=field):
                    before = {name: self.record(name, state="static")}
                    self.host.activation = {name: {**before[name], field: value}}
                    self.host.activation_classification = {name: kind}
                    with self.assertRaises(DeployError) as error:
                        packages._finish_activation(self.host, {"activation_before": before, "activation_units": {}})
                    self.assertEqual(error.exception.code, "package_service_changed")

    def test_triggered_and_session_runtime_changes_are_ignored(self):
        for name, kind in (("user-runtime-dir@42.service", {}),
                           ("refresh.service", {"TriggeredBy": "refresh.timer"}),
                           ("worker.service", {"TriggeredBy": "worker.socket"}),
                           ("watch.service", {"TriggeredBy": "watch.path"})):
            with self.subTest(name=name):
                before = {name: self.record(name, active="inactive", pid="0")}
                self.host.activation = {name: self.record(name)}
                self.host.activation_classification = {name: kind}
                packages._finish_activation(self.host, {"activation_before": before, "activation_units": {}})

    def test_socket_triggered_daemons_may_start_but_never_stop_or_restart(self):
        for name, trigger in (("ssh.service", "ssh.socket"), ("dbus.service", "dbus.socket"), ("watch.service", "watch.path")):
            kind = {"TriggeredBy": trigger, "Type": "notify", "UnitFileState": "enabled"}
            for change, refused in (({"MainPID": "8400"}, True),
                                    ({"ActiveState": "inactive", "SubState": "dead", "MainPID": "0"}, True),
                                    ({"ActiveState": "failed", "SubState": "failed", "MainPID": "0"}, True),
                                    ({}, False)):
                with self.subTest(name=name, change=change):
                    before = {name: self.record(name)}
                    self.host.activation = {name: {**self.record(name), **change}}
                    self.host.activation_classification = {name: kind}
                    journal = {"activation_before": before, "activation_units": {}}
                    if refused:
                        with self.assertRaises(DeployError) as error:
                            packages._finish_activation(self.host, journal)
                        self.assertEqual(error.exception.code, "package_service_changed")
                    else:
                        packages._finish_activation(self.host, journal)

    def test_static_helpers_started_by_hooks_may_run_but_static_daemons_stay_guarded(self):
        running = {"ActiveState": "activating", "SubState": "start", "MainPID": "7311"}
        finished = {"ActiveState": "inactive", "SubState": "dead", "MainPID": "0"}
        held = {"ActiveState": "active", "SubState": "exited", "MainPID": "0"}
        cases = [  # name, Type, RemainAfterExit, UnitFileState, before change, after change, refused
            ("esm-cache.service", "oneshot", "no", "static", finished, running, False),
            ("apt-news.service", "oneshot", "no", "static", running, finished, False),
            ("helper.service", "simple", "no", "static", finished, {"ActiveState": "active", "SubState": "running", "MainPID": "88"}, False),
            ("helper.service", "simple", "no", "static", finished, {"ActiveState": "failed", "SubState": "failed", "MainPID": "0"}, False),
            ("helper.service", "simple", "no", "static", {}, {"MainPID": "8400"}, True),
            ("helper.service", "simple", "no", "static", {}, finished, True),
            ("job.service", "oneshot", "no", "enabled", finished, running, True),
            # A state-holding static oneshot (e.g. firewall rules removed by ExecStop) must not be stopped.
            ("rules.service", "oneshot", "yes", "static", held, finished, True),
            ("rules.service", "oneshot", None, "static", held, finished, True),
            ("rules.service", "oneshot", "yes", "static", finished, held, False),
        ]
        for name, kind_type, remain, state, old, new, refused in cases:
            with self.subTest(name=name, remain=remain, state=state, new=new):
                before = {name: {**self.record(name, state=state), **old}}
                self.host.activation = {name: {**self.record(name, state=state), **new}}
                kind = {"Type": kind_type, "UnitFileState": state}
                if remain is not None:
                    kind["RemainAfterExit"] = remain
                self.host.activation_classification = {name: kind}
                journal = {"activation_before": before, "activation_units": {}}
                if refused:
                    with self.assertRaises(DeployError) as error:
                        packages._finish_activation(self.host, journal)
                    self.assertEqual(error.exception.code, "package_service_changed")
                else:
                    packages._finish_activation(self.host, journal)

    def test_instances_of_preexisting_foreign_templates_are_not_unowned(self):
        dead = {"ActiveState": "inactive", "SubState": "dead", "MainPID": "0"}
        daemon = {"ActiveState": "active", "SubState": "running", "MainPID": "77"}
        finishing = {"ActiveState": "activating", "SubState": "start", "MainPID": "78"}
        job = {"Type": "oneshot", "RemainAfterExit": "no"}
        cases = [  # template state, instance runtime, instance kind, accepted
            ("static", dead, job, True),
            ("static", finishing, job, True),
            ("enabled", dead, {"Type": "simple"}, True),
            ("static", daemon, {"Type": "simple"}, False),  # a package must not start an unowned daemon
            ("static", {**daemon, "SubState": "exited", "MainPID": "0"}, {"Type": "oneshot", "RemainAfterExit": "yes"}, False),
            ("enabled", daemon, {"Type": "simple"}, False),
        ]
        for template_state, runtime, kind, accepted in cases:
            with self.subTest(template_state=template_state, runtime=runtime, kind=kind):
                name = "modprobe@sd_mod.service"
                self.host.activation = {"modprobe@.service": {"UnitFileState": template_state},
                                        name: {**self.record(name, state="static"), **runtime}}
                self.host.activation_classification = {name: {**kind, "UnitFileState": "static"}}
                self.host.commands.clear()
                journal = {"activation_before": {"modprobe@.service": {"template_state": template_state}}, "activation_units": {}}
                if accepted:
                    packages._finish_activation(self.host, journal)
                    self.assertFalse(any(args[1:3] == ["disable", name] for args, _ in self.host.commands))
                else:
                    with self.assertRaises(DeployError) as error:
                        packages._finish_activation(self.host, journal)
                    self.assertEqual(error.exception.code, "package_activation")
        # An instance of a template that did not exist before remains unowned activation.
        self.host.activation = {"fresh@a.service": self.record("fresh@a.service", state="static", active="inactive", pid="0")}
        self.host.activation_classification = {"fresh@a.service": {"Type": "oneshot"}}
        with self.assertRaises(DeployError):
            packages._finish_activation(self.host, {"activation_before": {}, "activation_units": {}})

    def test_plan_token_projection_ignores_churn_but_binds_independent_services(self):
        self.host.activation_classification = {"packagekit.service": {"Type": "dbus", "UnitFileState": "static"},
                                               "ssh.service": {"TriggeredBy": "ssh.socket"},
                                               "run-u9.service": {"Transient": "yes"}}
        base = {"cron.service": self.record("cron.service"), "user@0.service": self.record("user@0.service", state="static"),
                "packagekit.service": self.record("packagekit.service", state="static", active="inactive", pid="0"),
                "ssh.service": self.record("ssh.service", active="inactive", pid="0")}
        churned = copy.deepcopy(base)
        churned["user@0.service"]["MainPID"] = "8400"
        churned["packagekit.service"].update(ActiveState="active", SubState="running", MainPID="77")
        churned["ssh.service"].update(ActiveState="active", SubState="running", MainPID="78")
        churned["run-u9.service"] = self.record("run-u9.service", state="transient")
        self.host.activation = churned
        self.assertEqual(packages.stable_activation(self.host, base), packages.stable_activation(self.host, churned))
        restarted = copy.deepcopy(base)
        restarted["cron.service"]["MainPID"] = "9999"
        self.assertNotEqual(packages.stable_activation(self.host, base), packages.stable_activation(self.host, restarted))
        reconfigured = copy.deepcopy(base)
        reconfigured["packagekit.service"]["UnitFileState"] = "masked"
        self.assertNotEqual(packages.stable_activation(self.host, base), packages.stable_activation(self.host, reconfigured))

    def test_refusals_name_the_changed_units_and_fields(self):
        before = {"cron.service": self.record("cron.service"), "gone.service": self.record("gone.service")}
        current = {"cron.service": {**self.record("cron.service"), "MainPID": "8400", "UnitFileState": "disabled"}}
        differences = packages._activation_differences(before, current, {})
        self.assertEqual(differences, [("gone.service", "removed"), ("cron.service", "UnitFileState/MainPID")])
        detail = packages._difference_detail([(f"u{i}.service", "MainPID") for i in range(10)])
        self.assertTrue(detail.endswith("u7.service (MainPID) and 2 more"))
        self.host.activation = current
        with self.assertRaises(DeployError) as error:
            packages._finish_activation(self.host, {"activation_before": before, "activation_units": {}})
        self.assertIn("cron.service (UnitFileState/MainPID)", error.exception.detail)

    def test_timer_only_triggered_runs_may_finish_or_restart(self):
        name, kind = "apt-daily.service", {"TriggeredBy": "apt-daily.timer"}
        for change in ({"ActiveState": "inactive", "SubState": "dead", "MainPID": "0"}, {"MainPID": "8400"}):
            with self.subTest(change=change):
                self.host.activation = {name: {**self.record(name, state="static"), **change}}
                self.host.activation_classification = {name: kind}
                packages._finish_activation(self.host, {"activation_before": {name: self.record(name, state="static")},
                                                        "activation_units": {}})

    def test_timer_path_target_ignore_only_substate(self):
        for suffix in ("timer", "path", "target"):
            name = "foreign." + suffix
            before = {name: self.record(name, pid="0")}
            self.host.activation = {name: {**before[name], "SubState": "waiting"}}
            journal = {"activation_before": before, "activation_units": {}}
            with self.subTest(name=name):
                packages._finish_activation(self.host, journal)
                self.host.activation[name]["ActiveState"] = "inactive"
                with self.assertRaises(DeployError) as error:
                    packages._finish_activation(self.host, journal)
                self.assertEqual(error.exception.code, "package_service_changed")

    def test_new_transient_and_session_units_are_ignored_but_unowned_units_refuse(self):
        for name, kind, accepted in (("session-helper.service", {"Transient": "yes"}, True),
                                     ("user@0.service", {}, True), ("user-runtime-dir@42.service", {}, True),
                                     ("surprise.service", {}, False), ("user@foreign.service", {}, False),
                                     ("packagekit.service", {"Type": "dbus"}, False)):
            with self.subTest(name=name):
                self.host.activation = {name: self.record(name, state="static")}
                self.host.activation_classification = {name: kind}
                self.host.commands.clear()
                journal = {"activation_before": {}, "activation_units": {}}
                if accepted:
                    packages._finish_activation(self.host, journal)
                else:
                    with self.assertRaises(DeployError) as error:
                        packages._finish_activation(self.host, journal)
                    self.assertEqual(error.exception.code, "package_activation")
                    self.assertIn("Unowned activation", error.exception.detail)
                self.assertFalse(any(args[1] == "disable" for args, _ in self.host.commands))

    def test_vanished_session_and_transient_units_are_ignored_but_others_refuse(self):
        transient = {**self.record("run-u42.service"), "FragmentPath": "/run/systemd/transient/run-u42.service"}
        for name, record, accepted in (("user@0.service", self.record("user@0.service", state="static"), True),
                                       ("run-u42.service", transient, True),
                                       ("cron.service", self.record("cron.service"), False)):
            with self.subTest(name=name):
                self.host.activation, self.host.activation_classification = {}, {}
                journal = {"activation_before": {name: record}, "activation_units": {}}
                if accepted:
                    packages._finish_activation(self.host, journal)
                    self.assertTrue(packages._activation_matches({name: record}, {}, {}))
                else:
                    with self.assertRaises(DeployError) as error:
                        packages._finish_activation(self.host, journal)
                    self.assertEqual(error.exception.code, "package_service_changed")

    def test_plan_comparison_ignores_new_ephemeral_units_only(self):
        before = {"cron.service": self.record("cron.service")}
        current = {**before, "user@0.service": self.record("user@0.service", state="static"),
                   "run-u7.service": self.record("run-u7.service", state="transient")}
        self.assertTrue(packages._activation_matches(before, current, {"run-u7.service": {"Transient": "yes"}}))
        current["surprise.service"] = self.record("surprise.service")
        self.assertFalse(packages._activation_matches(before, current, {"run-u7.service": {"Transient": "yes"}}))

    def test_classification_is_bounded_and_requires_complete_output(self):
        with self.assertRaises(DeployError):
            packages._activation_classification(self.host, {f"unit-{i}.service": {} for i in range(4097)})
        self.assertEqual(self.host.commands, [])
        name = "packagekit.service"
        for output in ("", f"Id={name}\nType=dbus\nUnitFileState=static\nTransient=no\n"):
            with patch.object(self.host, "run", return_value=subprocess.CompletedProcess([], 0, output)), \
                    self.assertRaises(DeployError):
                packages._activation_classification(self.host, {name: self.record(name)})


class BuiltinPayloadTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name).resolve()

    def host(self, *, cron=True, root=None):
        payloads = {"nginx": {"/usr/lib/systemd/system/nginx.service": b"[Service]\nExecStart=/usr/sbin/nginx\n"}}
        if cron:
            payloads["certbot"] = {"/etc/cron.d/certbot": CERTBOT_CRON,
                "/usr/lib/systemd/system/certbot.timer": b"[Timer]\nOnCalendar=daily\n"}
        return PayloadPackageHost(root or self.root, payloads)

    def test_builtin_certbot_cron_is_inert_before_and_after_installation(self):
        host = self.host()
        packages.install_missing(host, list(host.candidates))
        cron = host.paths.p("/etc/cron.d/certbot")
        self.assertTrue(all(not line.strip() or line.startswith("#") for line in cron.read_text().splitlines()))
        self.assertEqual(cron.with_name("certbot.dpkg-dist").read_bytes(), CERTBOT_CRON)
        self.assertEqual(packages._load(host)["state"], "complete")

    def interrupt_mask_publication(self, host, *, point="published"):
        mask = host.paths.p("/etc/systemd/system/nginx.service")
        sync = packages._sync_directory

        def crash_after_publication(directory):
            sync(directory)
            if directory != mask.parent:
                return
            staged = any(directory.glob(".foundry-mask-*"))
            if ((point == "staged" and staged and not mask.is_symlink())
                    or (point == "published" and mask.is_symlink())):
                raise SystemExit("crash during mask publication: " + point)

        with patch.object(packages, "_sync_directory", side_effect=crash_after_publication), self.assertRaises(SystemExit):
            packages.install_missing(host, ["nginx"])
        return mask

    def test_mask_publication_crash_recovers_journal_owned_inode(self):
        host = self.host(cron=False)
        mask = self.interrupt_mask_publication(host)
        inode = mask.lstat().st_ino
        journal = packages._load(host)
        self.assertEqual(journal["state"], "installing")
        self.assertEqual(journal["default_activation"]["nginx.service"]["state"], "publishing")
        self.assertEqual(journal["default_activation"]["nginx.service"]["identity"]["inode"], inode)
        packages.install_missing(host, ["nginx"])
        self.assertEqual(mask.lstat().st_ino, inode)
        self.assertEqual(packages._load(host)["state"], "complete")
        anchor = next(mask.parent.glob(".foundry-mask-*"))
        self.assertEqual(anchor.lstat().st_ino, inode)
        self.assertEqual(mask.lstat().st_nlink, 2)
        packages.authorize_default_activation(host, "nginx.service",
                                             {"owner": "foundry", "installed_by_foundry": True, "unit": "nginx.service"})
        self.assertFalse(mask.is_symlink())
        self.assertFalse(anchor.is_symlink())
        self.assertFalse(list(mask.parent.glob(".foundry-mask-*")))

    def test_mask_staging_crash_recovers(self):
        host = self.host(cron=False)
        mask = self.interrupt_mask_publication(host, point="staged")
        packages.install_missing(host, ["nginx"])
        self.assertEqual(os.readlink(mask), "/dev/null")
        self.assertEqual(packages._load(host)["state"], "complete")
        self.assertEqual(next(mask.parent.glob(".foundry-mask-*")).lstat().st_ino, mask.lstat().st_ino)

    def test_mask_record_crash_recovers_with_durable_anchor(self):
        host = self.host(cron=False)
        save = packages._save

        def crash_after_masked_record(host, journal):
            save(host, journal)
            if journal.get("default_activation", {}).get("nginx.service", {}).get("state") == "masked":
                raise SystemExit("crash after saving masked identity")

        with patch.object(packages, "_save", side_effect=crash_after_masked_record), self.assertRaises(SystemExit):
            packages.install_missing(host, ["nginx"])
        mask = host.paths.p("/etc/systemd/system/nginx.service")
        anchor = next(mask.parent.glob(".foundry-mask-*"))
        self.assertEqual(anchor.lstat().st_ino, mask.lstat().st_ino)
        identity = packages._mask_identity(mask)
        self.assertEqual(packages._load(host)["default_activation"]["nginx.service"]["identity"], identity)
        packages.install_missing(host, ["nginx"])
        self.assertEqual(packages._mask_identity(mask), identity)
        self.assertEqual(packages._load(host)["state"], "complete")

    def test_mask_publication_recovery_refuses_foreign_replacement(self):
        host = self.host(cron=False)
        mask = self.interrupt_mask_publication(host)
        replacement = mask.with_name("foreign")
        replacement.symlink_to("/dev/null")
        os.replace(replacement, mask)
        identity = packages._mask_identity(mask)
        with self.assertRaises(DeployError):
            packages.install_missing(host, ["nginx"])
        self.assertEqual(packages._mask_identity(mask), identity)
        self.assertNotIn("nginx", host.inventory)
        self.assertEqual(packages._load(host)["state"], "installing")

    def test_mask_cleanup_crash_refuses_replacement_with_reused_inode(self):
        host = self.host(cron=False)
        mask = self.interrupt_mask_publication(host)
        journal = packages._load(host)
        original = journal["default_activation"]["nginx.service"]["identity"]
        # Reproduce the old cleanup crash: the anchor is gone but the durable
        # record is still publishing. Then replace the remaining mask.
        next(mask.parent.glob(".foundry-mask-*")).unlink()
        packages._sync_directory(mask.parent)
        mask.unlink()
        mask.symlink_to("/dev/null")
        replacement = packages._mask_identity(mask)
        self.assertNotEqual(replacement["ctime_ns"], original["ctime_ns"])
        identity = packages._mask_identity

        def reused_inode(path):
            observed = identity(path)
            if path == mask:
                # Filesystem inode allocation is nondeterministic; simulate only
                # reuse of the number, retaining the replacement's real ctime.
                observed["inode"] = original["inode"]
            return observed

        with patch.object(packages, "_mask_identity", side_effect=reused_inode), self.assertRaises(DeployError):
            packages.install_missing(host, ["nginx"])
        self.assertEqual(packages._mask_identity(mask), replacement)
        self.assertNotIn("nginx", host.inventory)
        self.assertEqual(packages._load(host)["state"], "installing")

    def interrupt_cron_publication(self, host):
        cron = host.paths.p("/etc/cron.d/certbot")
        unlink = Path.unlink

        def crash_before_stage_unlink(path, *args, **kwargs):
            if path.name.startswith(".foundry-cron-") and cron.exists():
                raise SystemExit("crash before unlinking cron stage")
            return unlink(path, *args, **kwargs)

        with patch.object(Path, "unlink", new=crash_before_stage_unlink), self.assertRaises(SystemExit):
            packages.install_missing(host, list(host.candidates))
        journal = packages._load(host)
        stage = cron.parent / (".foundry-cron-" + journal["operation_id"])
        self.assertEqual(cron.stat().st_nlink, 2)
        self.assertEqual(cron.stat().st_ino, stage.stat().st_ino)
        return cron, stage, journal

    def test_cron_publication_crash_reconciles_staging_hardlink(self):
        host = self.host()
        cron, stage, journal = self.interrupt_cron_publication(host)
        packages.install_missing(host, list(host.candidates))
        self.assertFalse(stage.exists())
        self.assertEqual(cron.stat().st_nlink, 1)
        self.assertEqual(cron.read_text(), packages._cron_stub(journal))
        self.assertEqual(cron.with_name("certbot.dpkg-dist").read_bytes(), CERTBOT_CRON)
        self.assertEqual(packages._load(host)["state"], "complete")

    def test_cron_recovery_refuses_foreign_staging_inode(self):
        host = self.host()
        cron, stage, journal = self.interrupt_cron_publication(host)
        stage.unlink()
        stage.write_text(packages._cron_stub(journal))
        stage.chmod(0o644)
        os.link(stage, stage.with_name("foreign-stage"))
        os.link(cron, cron.with_name("foreign-cron"))
        # Even matching content, mode and link counts cannot authorize a
        # staging inode unrelated to the published suppression file.
        with self.assertRaises(DeployError):
            packages.install_missing(host, list(host.candidates))
        self.assertTrue(stage.exists())
        self.assertNotEqual(cron.stat().st_ino, stage.stat().st_ino)
        self.assertNotIn("certbot", host.inventory)

    def test_pending_default_activation_is_read_only_and_checks_exact_mask(self):
        host = self.host(cron=False)
        self.assertFalse(packages.default_activation_pending(host, "nginx.service"))
        packages.install_missing(host, ["nginx"])
        mask = host.paths.p("/etc/systemd/system/nginx.service")
        before = mask.lstat()
        self.assertTrue(packages.default_activation_pending(host, "nginx.service"))
        self.assertEqual(mask.lstat().st_ino, before.st_ino)
        self.assertEqual(os.readlink(mask), "/dev/null")
        replacement = mask.with_name("replacement.service")
        replacement.symlink_to("/dev/null")
        os.replace(replacement, mask)
        with self.assertRaises(DeployError):
            packages.default_activation_pending(host, "nginx.service")
        self.assertEqual(os.readlink(mask), "/dev/null")

    def test_nginx_owner_handoff_allows_enable_and_preserves_other_masks(self):
        host = self.host()
        packages.install_missing(host, list(host.candidates))
        mask = host.paths.p("/etc/systemd/system/nginx.service")
        self.assertTrue(mask.is_symlink())
        claim = {"owner": "foundry", "installed_by_foundry": True, "unit": "nginx.service"}
        packages.authorize_default_activation(host, "nginx.service", claim)
        host.run(["systemctl", "enable", "--now", "nginx"])
        self.assertIn("nginx.service", host.active)
        packages.authorize_default_activation(host, "nginx.service", claim)
        self.assertTrue(host.paths.p("/etc/systemd/system/certbot.timer").is_symlink())
        self.assertTrue(host.paths.p("/etc/cron.d/certbot").read_text().startswith("#"))

    def test_preexisting_cron_file_is_not_replaced_or_claimed(self):
        host = self.host()
        cron = host.paths.p("/etc/cron.d/certbot")
        cron.parent.mkdir(parents=True)
        cron.write_text("# foreign cron configuration\n")
        with self.assertRaises(DeployError):
            packages.install_missing(host, list(host.candidates))
        self.assertEqual(cron.read_text(), "# foreign cron configuration\n")
        self.assertFalse(packages._journal_path(host).exists())

    def test_handoff_refuses_external_claim_pending_install_and_replaced_mask(self):
        host = self.host(cron=False)
        packages.install_missing(host, ["nginx"])
        claim = {"owner": "foundry", "installed_by_foundry": True, "unit": "nginx.service"}
        mask = host.paths.p("/etc/systemd/system/nginx.service")
        for change in ({"owner": "external"}, {"installed_by_foundry": False}, {"unit": "foreign.service"}):
            with self.subTest(change=change), self.assertRaises(DeployError):
                packages.authorize_default_activation(host, "nginx.service", {**claim, **change})
            self.assertTrue(mask.is_symlink())
        journal = packages._load(host)
        journal["state"] = "installing"
        packages._save(host, journal)
        with self.assertRaises(DeployError):
            packages.authorize_default_activation(host, "nginx.service", claim)
        journal["state"] = "complete"
        packages._save(host, journal)
        replacement = mask.with_name("replacement.service")
        replacement.symlink_to("/dev/null")
        os.replace(replacement, mask)
        with self.assertRaises(DeployError):
            packages.authorize_default_activation(host, "nginx.service", claim)
        self.assertEqual(os.readlink(mask), "/dev/null")

    def test_handoff_recovers_after_unlink_before_daemon_reload(self):
        host = self.host(cron=False)
        packages.install_missing(host, ["nginx"])
        claim = {"owner": "foundry", "installed_by_foundry": True, "unit": "nginx.service"}
        with patch.object(host, "run", side_effect=DeployError("interrupted", "reload interrupted")), self.assertRaises(DeployError):
            packages.authorize_default_activation(host, "nginx.service", claim)
        self.assertFalse(host.paths.p("/etc/systemd/system/nginx.service").is_symlink())
        self.assertEqual(packages._load(host)["default_activation"]["nginx.service"]["state"], "releasing")
        self.assertTrue(packages.default_activation_pending(host, "nginx.service"))
        packages.authorize_default_activation(host, "nginx.service", claim)
        self.assertEqual(packages._load(host)["default_activation"]["nginx.service"]["state"], "released")
        self.assertFalse(packages.default_activation_pending(host, "nginx.service"))

    def interrupt_handoff_after_release(self, host):
        packages.install_missing(host, ["nginx"])
        claim = {"owner": "foundry", "installed_by_foundry": True, "unit": "nginx.service"}
        directory = host.paths.p("/etc/systemd/system")
        anchor = next(directory.glob(".foundry-mask-*"))
        save = packages._save

        def crash_after_release(host, journal):
            save(host, journal)
            if journal["default_activation"]["nginx.service"]["state"] == "released":
                raise SystemExit("crash after durable release before anchor cleanup")

        with patch.object(packages, "_save", side_effect=crash_after_release), self.assertRaises(SystemExit):
            packages.authorize_default_activation(host, "nginx.service", claim)
        self.assertTrue(anchor.is_symlink())
        self.assertFalse((directory / "nginx.service").is_symlink())
        self.assertEqual(packages._load(host)["default_activation"]["nginx.service"]["state"], "released")
        return claim, directory, anchor

    def test_handoff_retry_cleans_anchor_after_durable_release(self):
        host = self.host(cron=False)
        claim, directory, anchor = self.interrupt_handoff_after_release(host)
        packages.authorize_default_activation(host, "nginx.service", claim)
        self.assertFalse(list(directory.glob(".foundry-mask-*")))
        packages.authorize_default_activation(host, "nginx.service", claim)
        self.assertFalse(list(directory.glob(".foundry-mask-*")))

    def test_handoff_cleanup_refuses_replaced_anchor_even_with_reused_inode(self):
        host = self.host(cron=False)
        claim, directory, anchor = self.interrupt_handoff_after_release(host)
        original = packages._mask_identity(anchor)
        anchor.unlink()
        anchor.symlink_to("/dev/null")
        replacement = packages._mask_identity(anchor)
        self.assertNotEqual(original["ctime_ns"], replacement["ctime_ns"])
        identity = packages._mask_identity

        def reused_inode(path):
            observed = identity(path)
            if path == anchor:
                observed["inode"] = original["inode"]
            return observed

        with patch.object(packages, "_mask_identity", side_effect=reused_inode), self.assertRaises(DeployError):
            packages.authorize_default_activation(host, "nginx.service", claim)
        self.assertEqual(packages._mask_identity(anchor), replacement)

    def test_handoff_cleanup_retry_after_anchor_unlink(self):
        host = self.host(cron=False)
        claim, directory, anchor = self.interrupt_handoff_after_release(host)
        sync = packages._sync_directory

        def crash_after_cleanup(path):
            if path == directory and not anchor.is_symlink():
                raise SystemExit("crash after anchor unlink before directory sync")
            sync(path)

        with patch.object(packages, "_sync_directory", side_effect=crash_after_cleanup), self.assertRaises(SystemExit):
            packages.authorize_default_activation(host, "nginx.service", claim)
        packages.authorize_default_activation(host, "nginx.service", claim)
        self.assertFalse(list(directory.glob(".foundry-mask-*")))

    def test_additional_package_transaction_retains_prior_mask_ownership(self):
        host = self.host()
        host.candidates = {"nginx": "1"}
        # The fake package manager installs only its requested transaction.
        pending_certbot = host.payloads.pop("certbot")
        packages.install_missing(host, ["nginx"])
        host.payloads = {"certbot": pending_certbot}
        host.candidates = {"certbot": "1"}
        packages.install_missing(host, ["certbot"])
        packages.authorize_default_activation(host, "nginx.service",
                                             {"owner": "foundry", "installed_by_foundry": True, "unit": "nginx.service"})
        self.assertFalse(host.paths.p("/etc/systemd/system/nginx.service").is_symlink())

    def test_unknown_application_cron_payload_is_refused_before_install(self):
        host = PayloadPackageHost(self.root, {"application-daemon": {"/etc/cron.d/application-daemon": CERTBOT_CRON}})
        with self.assertRaises((DeployError, ValueError)):
            packages.install_missing(host, ["application-daemon"])
        self.assertFalse(host.paths.p("/etc/cron.d/application-daemon").exists())
        self.assertFalse(packages._journal_path(host).exists())

    def test_builtin_cron_must_be_a_dpkg_conffile_to_preserve_suppression(self):
        host = self.host()
        host.conffiles.clear()
        with self.assertRaises((DeployError, ValueError)):
            packages.install_missing(host, list(host.candidates))
        self.assertFalse(packages._journal_path(host).exists())


if __name__ == "__main__":
    unittest.main()
