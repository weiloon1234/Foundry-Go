"""Owned PostgreSQL planning, interruption and authentication policy sources."""
import copy
import json
import os
import shutil
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "lib"))

from contracts import DeployError
from host import Host, Paths
import postgres
from test_contracts import fixture, registry


class PostgreSQLHost(Host):
    def __init__(self, root):
        super().__init__(Paths(root))
        self.calls = []
        self.clusters = ""
        self.listeners = ""
        self.refresh_failure = False
        self.account = ""
        self.group = ""
        for name, data in (("/etc/os-release", 'ID=ubuntu\nVERSION_ID="24.04"\n'),
                           ("/proc/meminfo", "MemTotal: 4194304 kB\n")):
            path = self.paths.p(name)
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(data)
        for name in ("/usr/bin/pg_lsclusters", "/usr/bin/pg_createcluster", "/usr/bin/openssl",
                     "/usr/sbin/update-ca-certificates", "/usr/lib/postgresql/16/bin/postgres",
                     "/usr/lib/postgresql/16/bin/psql"):
            path = self.paths.p(name)
            path.parent.mkdir(parents=True, exist_ok=True)
            path.touch()

    def run(self, args, **kwargs):
        args = [str(arg) for arg in args]
        self.calls.append((args, kwargs))
        output = ""
        if args == ["pg_lsclusters", "--no-header"]:
            output = self.clusters
        elif args[:2] == ["getent", "passwd"]:
            return subprocess.CompletedProcess(args, 0 if self.account else 2, self.account, "")
        elif args[:2] == ["getent", "group"]:
            return subprocess.CompletedProcess(args, 0 if self.group else 2, self.group, "")
        elif args[0] == "groupadd":
            self.group = "foundry-postgres:x:999:\n"
        elif args[0] == "useradd":
            self.account = "foundry-postgres:x:999:999:" + args[args.index("--comment") + 1] + ":" + postgres.CLUSTER_HOME + ":/usr/sbin/nologin\n"
        elif args[:2] == ["systemctl", "is-active"]:
            return subprocess.CompletedProcess(args, 3, "", "")
        elif args[0] == "pgrep":
            return subprocess.CompletedProcess(args, 1, "", "")
        elif args == ["systemctl", "daemon-reload"]:
            pass
        elif args[0].endswith("/pg_controldata"):
            output = "Database cluster state:               shut down\n"
        elif args[0] == "pg_dropcluster":
            for path in (f"/etc/postgresql/{args[1]}/foundry", f"{postgres.CLUSTER_HOME}/{args[1]}/foundry"):
                directory = self.paths.p(path)
                if directory.exists():
                    shutil.rmtree(directory)
        elif args[0] == "pg_createcluster":
            config = self.paths.p(f"/etc/postgresql/{args[1]}/foundry")
            data = self.paths.p(args[args.index("--datadir") + 1])
            # initdb runs as the cluster account and must traverse the version directory.
            assert data.parent.stat().st_mode & 0o777 == 0o755, "version directory must be prepared"
            config.mkdir(parents=True)
            data.mkdir(parents=True)
            (config / "postgresql.conf").write_text("# initdb configuration\n")
            (data / "PG_VERSION").write_text(args[1] + "\n")
        elif args == ["ss", "-Hltn"]:
            output = self.listeners
        elif args == ["update-ca-certificates"]:
            if self.refresh_failure:
                raise DeployError("interrupted", "fixture trust refresh interrupted")
        elif args[0] == "openssl":
            if "-out" in args:
                destination = Path(args[args.index("-out") + 1])
                destination.write_text("fixture " + destination.name)
                if destination.suffix == ".key":
                    os.chmod(destination, 0o600)
            if "-pubkey" in args or "-pubout" in args:
                output = "fixture public key\n"
        elif args[:2] in (["pg_ctlcluster", "16"], ["systemctl", "enable"], ["systemctl", "start"]):
            pass
        else:
            raise AssertionError("Unexpected host operation: " + args[0])
        return subprocess.CompletedProcess(args, 0, output, "")


class PostgreSQLTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory(dir=Path(tempfile.gettempdir()).resolve())
        self.addCleanup(temporary.cleanup)
        self.host = PostgreSQLHost(Path(temporary.name))
        _, _, self.entry = fixture()
        self.registry = registry(self.entry)

    def claimed(self, state="ready", port=55432, phase="ready"):
        allocation = dict(postgres._allocation(16, port, "a" * 64), state=state)
        self.registry["shared"]["postgresql"] = allocation
        journal = {"host_id": self.registry["host_id"], "allocation": copy.deepcopy(allocation), "phase": phase}
        postgres._save_cluster(self.host, journal)
        return allocation

    def test_mixed_host_inspection_only_reads_foreign_cluster_metadata(self):
        self.host.clusters = "16 main 5432 online postgres /var/lib/postgresql/16/main /var/log/postgresql/main.log\n"
        self.host.listeners = "LISTEN 0 128 [::]:55432 [::]:*\nLISTEN 0 128 127.0.0.1:55433 0.0.0.0:*\n"
        observed = postgres.inspect(self.host, self.registry)
        self.assertEqual(("absent", 55434), (observed["state"], observed["port"]))
        self.assertEqual(["pg_lsclusters", "ss"], [args[0] for args, _ in self.host.calls])
        self.assertFalse(self.host.paths.registry.exists())

    def test_unclaimed_foundry_cluster_is_never_adopted(self):
        self.host.clusters = "16 foundry 55432 down postgres /var/lib/postgresql/16/foundry /var/log/postgresql/foundry.log\n"
        with self.assertRaises(DeployError) as raised:
            postgres.inspect(self.host, self.registry)
        self.assertEqual("postgres_ownership", raised.exception.code)
        self.assertFalse(any("psql" in arg for args, _ in self.host.calls for arg in args))

    def test_reserved_port_survives_retry_even_when_occupied(self):
        self.claimed(state="preparing", port=55437, phase="creating")
        self.host.listeners = "LISTEN 0 128 127.0.0.1:55437 0.0.0.0:*\n"
        observed = postgres.inspect(self.host, self.registry)
        self.assertEqual(("preparing", 55437), (observed["state"], observed["port"]))

    def test_owned_partial_packages_are_inspected_before_missing_cluster_tools(self):
        self.host.paths.p("/usr/bin/pg_lsclusters").unlink()
        self.host.paths.p("/etc/postgresql").mkdir()
        self.claimed(state="preparing", phase="reserved")
        with patch.object(postgres.packages, "inspect", return_value={"state": "resume"}) as packages:
            self.assertEqual("preparing", postgres.inspect(self.host, self.registry)["state"])
        packages.assert_called_once_with(self.host)

    def test_foreign_partial_package_metadata_refuses(self):
        self.host.paths.p("/usr/bin/pg_lsclusters").unlink()
        self.host.paths.p("/etc/postgresql").mkdir()
        with patch.object(postgres.packages, "inspect", return_value={"state": "ready"}):
            with self.assertRaises(DeployError) as raised:
                postgres.inspect(self.host, self.registry)
        self.assertEqual("postgres_uninspectable", raised.exception.code)

    def test_administration_uses_major_binary_and_owned_socket_at_allocated_port(self):
        self.claimed(port=55439)
        with patch.object(self.host, "run", return_value=subprocess.CompletedProcess([], 0, "1\n", "")) as command:
            postgres._psql(self.host, self.registry, "SELECT 1")
        args = command.call_args.args[0]
        self.assertEqual(["runuser", "-u", "foundry-postgres", "--"], args[:4])
        self.assertIn("/usr/lib/postgresql/16/bin/psql", args)
        self.assertEqual(postgres.SOCKET, args[args.index("-h") + 1])
        self.assertEqual("55439", args[args.index("-p") + 1])
        self.assertNotIn("/var/run/postgresql", args)

    def test_hba_contains_only_exact_uuid_pairs_and_rejects_fallbacks(self):
        _, _, other = fixture(environment="staging", project_id=self.entry["project_id"], host_id=self.entry["host_id"], port=8101)
        self.registry["instances"][other["profile"]["instance_id"]] = other
        text = postgres._hba(self.registry)
        for entry in (self.entry, other):
            name = entry["allocations"]["database"]
            self.assertIn(f"hostssl {name} {name} 127.0.0.1/32 scram-sha-256", text)
            self.assertIn(f"hostssl {name} {name} ::1/128 scram-sha-256", text)
        self.assertIn('local all "foundry-postgres" peer', text)
        self.assertNotIn("local all postgres peer", text)
        self.assertIn("local all all reject", text)
        self.assertNotIn("hostssl all", text)
        self.assertTrue(text.endswith("host all all ::/0 reject\n"))

    def test_hba_refuses_database_name_outside_uuid_contract(self):
        self.entry["allocations"]["database"] = "another_database"
        with self.assertRaises(DeployError):
            postgres._hba(self.registry)

    def test_hba_disk_intent_precedes_publish_and_resumes_old_bytes(self):
        allocation = self.claimed()
        path = self.host.paths.p(allocation["config_path"]) / "pg_hba.conf"
        self.host.owned_write(path, "old hba\n")
        journal = postgres._cluster_journal(self.host, self.registry)
        journal["hba"] = {"old": "old hba\n", "new": "old hba\n", "phase": "published"}
        postgres._save_cluster(self.host, journal)
        original = self.host.owned_write

        def interrupted(target, content, **kwargs):
            if target == path:
                value = postgres._cluster_journal(self.host, self.registry)
                self.assertEqual("publishing", value["hba"]["phase"])
                raise DeployError("interrupted", "fixture publication interrupted")
            return original(target, content, **kwargs)

        with patch.object(self.host, "owned_write", side_effect=interrupted), self.assertRaises(DeployError):
            postgres._publish_hba(self.host, self.registry, running=True)
        with patch.object(postgres, "_psql", return_value=subprocess.CompletedProcess([], 0, "0\n", "")):
            postgres._publish_hba(self.host, self.registry, running=True)
        self.assertEqual(postgres._hba(self.registry), path.read_text())
        self.assertEqual("published", postgres._cluster_journal(self.host, self.registry)["hba"]["phase"])
        self.assertIn((["pg_ctlcluster", "16", "foundry", "reload"], {}), self.host.calls)

    def test_hba_syntax_failure_never_reloads(self):
        allocation = self.claimed()
        path = self.host.paths.p(allocation["config_path"]) / "pg_hba.conf"
        content = postgres._hba(self.registry)
        self.host.owned_write(path, content)
        journal = postgres._cluster_journal(self.host, self.registry)
        journal["hba"] = {"old": content, "new": content, "phase": "published"}
        postgres._save_cluster(self.host, journal)
        with patch.object(postgres, "_psql", return_value=subprocess.CompletedProcess([], 0, "1\n", "")):
            with self.assertRaises(DeployError):
                postgres._publish_hba(self.host, self.registry, running=True)
        self.assertFalse(any(args[0] == "pg_ctlcluster" for args, _ in self.host.calls))

    def test_unchanged_published_hba_is_not_reloaded_or_rejournaled(self):
        allocation = self.claimed()
        content = postgres._hba(self.registry)
        path = self.host.paths.p(allocation["config_path"]) / "pg_hba.conf"
        self.host.owned_write(path, content)
        journal = postgres._cluster_journal(self.host, self.registry)
        journal["hba"] = {"old": "older policy", "new": content, "phase": "published"}
        postgres._save_cluster(self.host, journal)
        with patch.object(postgres, "_psql", return_value=subprocess.CompletedProcess([], 0, "0\n", "")), \
                patch.object(self.host, "owned_write") as publish:
            postgres._publish_hba(self.host, self.registry, running=True)
        publish.assert_not_called()
        self.assertFalse(self.host.calls)
        self.host.owned_write(path, "older policy")
        with self.assertRaises(DeployError) as raised:
            postgres._publish_hba(self.host, self.registry, running=True)
        self.assertEqual("postgres_hba_conflict", raised.exception.code)

    def test_a06_equal_source_bytes_do_not_skip_trust_refresh(self):
        self.host.refresh_failure = True
        with self.assertRaises(DeployError):
            postgres.prepare_tls(self.host, self.registry)
        self.assertTrue(self.registry["shared"]["tls"]["source_installed"])
        self.assertFalse(self.registry["shared"]["tls"]["trust_verified"])
        self.host.refresh_failure = False
        tls = postgres.prepare_tls(self.host, self.registry)
        self.assertEqual(2, sum(args == ["update-ca-certificates"] for args, _ in self.host.calls))
        self.assertFalse(self.registry["shared"]["tls"]["trust_verified"])
        postgres.record_trust_probe(self.host, self.registry, "postgresql")
        self.assertFalse(self.registry["shared"]["tls"]["trust_verified"])
        postgres.record_trust_probe(self.host, self.registry, "redis")
        self.assertTrue(self.registry["shared"]["tls"]["trust_verified"])
        self.assertEqual(0o600, (tls["ca"].parent / "ca.key").stat().st_mode & 0o777)
        self.assertEqual(0o700, tls["ca"].parent.stat().st_mode & 0o777)

    def test_failed_refresh_revokes_trust_acknowledgment_until_a_real_probe(self):
        postgres.prepare_tls(self.host, self.registry)
        for service in ("postgresql", "redis"):
            postgres.record_trust_probe(self.host, self.registry, service)
        self.host.refresh_failure = True
        with self.assertRaises(DeployError):
            postgres.prepare_tls(self.host, self.registry)
        self.assertTrue(self.registry["shared"]["tls"]["refresh_pending"])
        self.assertFalse(self.registry["shared"]["tls"]["trust_verified"])
        with self.assertRaises(DeployError):
            postgres.record_trust_probe(self.host, self.registry, "redis")

    def test_interrupted_key_generation_never_publishes_partial_material(self):
        original = self.host.run

        def interrupted(args, **kwargs):
            if args[:2] == ["openssl", "genpkey"]:
                Path(args[args.index("-out") + 1]).write_text("partial private material")
                raise DeployError("interrupted", "fixture key creation interrupted")
            return original(args, **kwargs)

        with patch.object(self.host, "run", side_effect=interrupted), self.assertRaises(DeployError):
            postgres.prepare_tls(self.host, self.registry)
        ca_key = self.host.paths.p("/etc/foundry/pki/ca.key")
        self.assertFalse(ca_key.exists())
        postgres.prepare_tls(self.host, self.registry)
        self.assertNotEqual("partial private material", ca_key.read_text())

    def test_ca_trust_source_conflict_does_not_refresh_or_replace_foreign_source(self):
        path = self.host.paths.p(f"/usr/local/share/ca-certificates/foundry-{self.registry['host_id']}.crt")
        self.host.owned_write(path, "foreign authority")
        with self.assertRaises(DeployError) as raised:
            postgres.prepare_tls(self.host, self.registry)
        self.assertEqual("tls_ownership", raised.exception.code)
        self.assertEqual("foreign authority", path.read_text())
        self.assertFalse(any(args == ["update-ca-certificates"] for args, _ in self.host.calls))

    def test_incomplete_claimed_cluster_never_creates_second_cluster(self):
        allocation = self.claimed(state="preparing", phase="creating")
        config = self.host.paths.p(allocation["config_path"])
        config.mkdir(parents=True)
        (config / "postgresql.conf").write_text("partial")
        self.host.paths.p(allocation["data_path"]).mkdir(parents=True)
        with patch.object(postgres.packages, "install_missing"), patch.object(postgres, "prepare_tls", return_value={}), \
                patch.object(postgres, "_prepare_account"):
            with self.assertRaises(DeployError) as raised:
                postgres.prepare(self.host, self.registry)
        self.assertEqual("postgres_partial_install", raised.exception.code)
        self.assertFalse(any(args[0] == "pg_createcluster" for args, _ in self.host.calls))
        self.assertTrue(config.exists())

    def initial_creation(self):
        allocation = self.claimed(state="preparing", phase="creating")
        journal = postgres._cluster_journal(self.host, self.registry)
        journal.update(creation_intent=True, start_attempted=False, start_succeeded=False)
        postgres._save_cluster(self.host, journal)
        config = self.host.paths.p(allocation["config_path"])
        config.mkdir(parents=True)
        data = self.host.paths.p(allocation["data_path"])
        data.mkdir(parents=True)
        (data / "PG_VERSION").write_text("16\n")
        return journal, config, data

    def test_d08_partial_initialization_drops_only_reserved_cluster_and_recreates(self):
        journal, config, data = self.initial_creation()
        # Reserve the dedicated account before creating the simulated partial tree.
        journal["account_intent"] = True
        postgres._save_cluster(self.host, journal)
        foreign = self.host.paths.p("/var/lib/postgresql/16/main/foreign-data")
        foreign.parent.mkdir(parents=True)
        foreign.write_text("preserved")
        tls = {}
        for name in ("cert", "key"):
            tls[name] = self.host.paths.root / (name + ".fixture")
            tls[name].write_text(name)
        with patch.object(postgres.packages, "install_missing"), \
                patch.object(postgres, "prepare_tls", return_value=tls), \
                patch.object(postgres, "_verify_settings"), patch.object(postgres, "_publish_hba"):
            postgres.prepare(self.host, self.registry)
        commands = [args for args, _ in self.host.calls]
        self.assertEqual([["pg_dropcluster", "16", "foundry"]], [args for args in commands if args[0] == "pg_dropcluster"])
        create = next(args for args in commands if args[0] == "pg_createcluster")
        self.assertEqual("foundry-postgres", create[create.index("--user") + 1])
        self.assertEqual("/" + str(data.relative_to(self.host.paths.root)), create[create.index("--datadir") + 1])
        self.assertEqual("preserved", foreign.read_text())
        final = postgres._cluster_journal(self.host, self.registry)
        self.assertEqual("ready", final["phase"])
        self.assertTrue(final["start_attempted"])
        self.assertTrue(final["start_succeeded"])
        unit = self.host.paths.p("/etc/systemd/system/" + journal["allocation"]["unit"]).read_text()
        self.assertIn("User=foundry-postgres\nGroup=foundry-postgres", unit)
        self.assertIn(postgres.SOCKET + "/postmaster.pid", unit)
        self.assertEqual(0o600, (config / "foundry.key").stat().st_mode & 0o777)
        self.assertFalse(any(args[:3] == ["runuser", "-u", "postgres"] for args in commands))

    def test_d08_start_history_and_user_data_are_never_discarded(self):
        journal, config, data = self.initial_creation()
        for relative in ("postmaster.opts", "postmaster.pid", "base/16384/12345", "base/5/16384",
                         "pg_wal/000000010000000000000002", "pg_tblspc/16384", "backup_label"):
            with self.subTest(artifact=relative):
                path = data / relative
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text("keep")
                with self.assertRaises(DeployError):
                    postgres._discard_initialization(self.host, self.registry, journal)
                self.assertEqual("keep", path.read_text())
                path.unlink()
                while path.parent != data and not any(path.parent.iterdir()):
                    path = path.parent
                    path.rmdir()
        for field in ("start_attempted", "start_succeeded"):
            with self.subTest(field=field), self.assertRaises(DeployError):
                postgres._discard_initialization(self.host, self.registry, dict(journal, **{field: True}))
        self.assertFalse(any(args[0] == "pg_dropcluster" for args, _ in self.host.calls))

    def test_d08_interrupted_drop_retries_same_allocation(self):
        journal, config, data = self.initial_creation()
        original = self.host.run
        def interrupted(args, **kwargs):
            if args[0] == "pg_dropcluster":
                shutil.rmtree(data)
                raise DeployError("interrupted", "drop interrupted")
            return original(args, **kwargs)
        with patch.object(self.host, "run", side_effect=interrupted), self.assertRaises(DeployError):
            postgres._discard_initialization(self.host, self.registry, journal)
        resumed = postgres._cluster_journal(self.host, self.registry)
        self.assertEqual("recreating", resumed["phase"])
        postgres._discard_initialization(self.host, self.registry, resumed)
        self.assertFalse(config.exists())
        self.assertFalse(data.exists())

    def test_d08_symlink_or_running_cluster_refuses_deletion(self):
        journal, config, data = self.initial_creation()
        (data / "pg_wal").symlink_to(self.host.paths.root)
        with self.assertRaises(DeployError):
            postgres._discard_initialization(self.host, self.registry, journal)
        (data / "pg_wal").unlink()
        original = self.host.run
        def running(args, **kwargs):
            if args[:2] == ["systemctl", "is-active"]:
                return subprocess.CompletedProcess(args, 0, "", "")
            return original(args, **kwargs)
        with patch.object(self.host, "run", side_effect=running), self.assertRaises(DeployError):
            postgres._discard_initialization(self.host, self.registry, journal)
        self.assertFalse(any(args[0] == "pg_dropcluster" for args, _ in self.host.calls))

    def test_d08_half_written_configuration_does_not_require_pg_lsclusters(self):
        journal, config, data = self.initial_creation()
        (config / "postgresql.conf").write_text("port = '")
        with patch.object(postgres, "_inventory", side_effect=DeployError("invalid_config", "partial file")) as inventory:
            self.assertEqual("preparing", postgres.inspect(self.host, self.registry)["state"])
            postgres._discard_initialization(self.host, self.registry, journal)
        inventory.assert_not_called()
        self.assertFalse(config.exists())
        self.assertFalse(data.exists())

    def test_d08_surviving_creator_process_refuses_deletion(self):
        journal, config, data = self.initial_creation()
        original = self.host.run
        for flag in ("-u", "-f"):
            def running(args, **kwargs):
                if args[:2] == ["pgrep", flag]:
                    return subprocess.CompletedProcess(args, 0, "12345\n", "")
                return original(args, **kwargs)
            with self.subTest(flag=flag), patch.object(self.host, "run", side_effect=running):
                with self.assertRaises(DeployError):
                    postgres._discard_initialization(self.host, self.registry, journal)
        self.assertFalse(any(args[0] == "pg_dropcluster" for args, _ in self.host.calls))

    def test_d08_unclean_control_state_or_server_log_refuses_deletion(self):
        journal, config, data = self.initial_creation()
        (data / "global").mkdir()
        (data / "global/pg_control").write_bytes(b"fixture")
        original = self.host.run
        def unclean(args, **kwargs):
            if args[0].endswith("/pg_controldata"):
                return subprocess.CompletedProcess(args, 0, "Database cluster state: in production\n", "")
            return original(args, **kwargs)
        with patch.object(self.host, "run", side_effect=unclean), self.assertRaises(DeployError):
            postgres._discard_initialization(self.host, self.registry, journal)
        log = self.host.paths.p("/var/log/postgresql/postgresql-16-foundry.log")
        log.parent.mkdir(parents=True)
        log.write_text("database system is ready to accept connections\n")
        with self.assertRaises(DeployError):
            postgres._discard_initialization(self.host, self.registry, journal)
        self.assertFalse(any(args[0] == "pg_dropcluster" for args, _ in self.host.calls))

    def test_dedicated_account_resumes_group_creation_and_refuses_foreign_account(self):
        self.claimed(state="preparing", phase="reserved")
        journal = postgres._cluster_journal(self.host, self.registry)
        journal["account_intent"] = True
        postgres._save_cluster(self.host, journal)
        self.host.group = "foundry-postgres:x:999:\n"
        postgres._prepare_account(self.host, self.registry, journal)
        command = next(args for args, _ in self.host.calls if args[0] == "useradd")
        self.assertIn("--system", command)
        self.assertIn("/usr/sbin/nologin", command)
        self.assertEqual(postgres.CLUSTER_HOME, command[command.index("--home-dir") + 1])
        self.host.account = self.host.account.replace("/usr/sbin/nologin", "/bin/bash")
        with self.assertRaises(DeployError):
            postgres._prepare_account(self.host, self.registry, journal)

    def test_settings_bytes_survive_sorted_registry_round_trip(self):
        allocation = {"port": 55432, "config_path": "/etc/postgresql/16/foundry", "archive_command": "",
                      "capacity": dict(postgres.CAPACITY)}
        persisted = json.loads(json.dumps(allocation, sort_keys=True))
        self.assertEqual(postgres._configuration(allocation), postgres._configuration(persisted))

    def test_archive_command_needs_explicit_owned_plan_and_reloads_only_foundry(self):
        allocation = self.claimed()
        command = 'pgbackrest --stanza=foundry archive-push "%p"'
        plan = {"owner": "foundry", "cluster": "foundry", "ownership_token": allocation["ownership_token"], "archive_command": command}
        with patch.object(postgres, "inspect", return_value=dict(allocation)), patch.object(postgres, "prepare_tls"), \
                patch.object(postgres, "_publish_hba"), patch.object(postgres, "_psql", return_value=subprocess.CompletedProcess([], 0, command + "\n", "")):
            postgres.prepare(self.host, self.registry, shared_service_plan=plan)
        self.assertEqual(command, postgres.cluster_settings(self.host, self.registry)["archive_command"])
        self.assertEqual([["pg_ctlcluster", "16", "foundry", "reload"]], [args for args, _ in self.host.calls])
        self.assertEqual("complete", postgres._cluster_journal(self.host, self.registry)["archive_transition"]["phase"])
        with self.assertRaises(DeployError):
            postgres._archive_plan(allocation, dict(plan, cluster="main"))

    def test_effective_archive_and_capacity_overrides_are_rejected(self):
        allocation = self.claimed()
        observed = {"port": "55432", "socket": postgres.SOCKET, "ssl": "on", "checksums": "on", "encoding": "UTF8",
                    "password": "scram-sha-256", "fsync": "on", "full_page_writes": "on", "synchronous_commit": "on",
                    "listen": "127.0.0.1,::1", "data": allocation["data_path"], "config": allocation["config_path"] + "/postgresql.conf",
                    "hba": allocation["config_path"] + "/pg_hba.conf", "cert": allocation["config_path"] + "/foundry.crt",
                    "key": allocation["config_path"] + "/foundry.key", "archive_mode": "on", "wal_level": "replica",
                    "archive_command": "", "max_connections": "40", "shared_buffers": "128MB", "work_mem": "4MB",
                    "maintenance_work_mem": "64MB"}
        with patch.object(postgres, "_psql", return_value=subprocess.CompletedProcess([], 0, json.dumps(observed), "")):
            postgres._verify_settings(self.host, self.registry)
        for key, value in (("archive_command", "unauthorized command"), ("max_connections", "1000"), ("work_mem", "1GB")):
            with self.subTest(setting=key), patch.object(postgres, "_psql", return_value=subprocess.CompletedProcess([], 0, json.dumps(dict(observed, **{key: value})), "")):
                with self.assertRaises(DeployError) as raised:
                    postgres._verify_settings(self.host, self.registry)
                self.assertEqual("postgres_configuration_drift", raised.exception.code)

    def test_verify_full_denials_require_authorization_errors_and_positive_controls(self):
        self.claimed(port=55439)
        calls = []

        def login(host, args, **kwargs):
            calls.append((args, kwargs))
            env = kwargs["env"]
            if env["PGPASSWORD"].startswith("invalid-"):
                return subprocess.CompletedProcess(args, 2, "", "password authentication failed")
            if env["PGSSLMODE"] == "disable" or args[args.index("-d") + 1] == "postgres":
                return subprocess.CompletedProcess(args, 2, "", "pg_hba.conf rejects connection")
            return subprocess.CompletedProcess(args, 0, "1\n", "")

        with patch.object(postgres, "_probe", side_effect=login), patch.object(postgres, "record_trust_probe") as trust:
            postgres._verify_pg_password(self.host, self.registry, self.entry, {"database_password": "fixture-private"})
        trust.assert_called_once_with(self.host, self.registry, "postgresql")
        self.assertEqual(15, len(calls))
        self.assertTrue(all(args[args.index("-p") + 1] == "55439" for args, _ in calls))
        self.assertTrue(all("fixture-private" not in args for args, _ in calls))
        self.assertEqual({"localhost", "127.0.0.1", "::1"}, {args[args.index("-h") + 1] for args, _ in calls})

    def test_network_failure_is_not_evidence_of_authorization_denial(self):
        self.claimed()
        results = [subprocess.CompletedProcess([], 0, "1\n", ""), subprocess.CompletedProcess([], 2, "", "connection refused")]
        with patch.object(postgres, "_probe", side_effect=results), patch.object(postgres, "record_trust_probe") as trust:
            with self.assertRaises(DeployError) as raised:
                postgres._verify_pg_password(self.host, self.registry, self.entry, {"database_password": "fixture-private"})
        self.assertEqual("postgres_password_bypass", raised.exception.code)
        trust.assert_not_called()


if __name__ == "__main__":
    unittest.main()
