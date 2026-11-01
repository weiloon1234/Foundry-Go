"""Exact Redis grants, private journals, and resumable disk-before-live changes."""
import copy
from fnmatch import fnmatchcase
import hashlib
import json
from pathlib import Path
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "lib"))

from contracts import DeployError, REDIS_ACLFILE, REDIS_PORT, atomic_write, instance_paths
from host import Host, Paths
import redis_acl


def entry_fixture():
    return {
        "name": "sample-production",
        "profile": {"instance_id": "30000000-0000-4000-8000-000000000003"},
        "allocations": {"redis_user": "foundry-30000000-0000-4000-8000-000000000003",
                        "namespace": {"application": "sample", "environment": "production"}},
        "redis_grants": {"keys": [], "channels": [], "pending_operation": None},
        "provisioning": {"completed_steps": ["redis"]},
    }


def inventory(keys=(), channels=()):
    return [{"connection": "main", "keys": [{"prefix": item} for item in keys],
             "channels": [{"prefix": item} for item in channels]}]


class ExactRedisGrantTests(unittest.TestCase):
    def test_new_instance_has_no_legacy_key_or_channel_grants(self):
        rules = redis_acl.redis_rules(entry_fixture(), "test-password")
        self.assertFalse(any(rule.startswith(("~", "&")) for rule in rules))
        for forbidden in ("+@all", "+keys", "+scan", "+flushdb", "+config", "+acl", "+script"):
            self.assertNotIn(forbidden, rules)
        for required in ("+eval", "+evalsha", "+hget", "+multi", "+exec", "resetchannels", "-@all"):
            self.assertIn(required, rules)
        self.assertNotIn("test-password", " ".join(rules))

    def test_exact_child_namespaces_and_channels(self):
        entry = entry_fixture()
        declared = inventory(["foundry:lease:v1:sample.coordination:production:"],
                             ["foundry:events:v1:sample:production:"])
        patterns = redis_acl.redis_grant_patterns(entry, declared)
        channels = redis_acl.redis_channel_patterns(entry, declared)
        self.assertEqual(["foundry:lease:v1:sample.coordination:production:*"], patterns)
        self.assertEqual(["foundry:events:v1:sample:production:*"], channels)
        rules = redis_acl.redis_rules(entry, "password", patterns, channels=channels)
        self.assertIn("&" + channels[0], rules)
        for key in ("foundry:lease:v1:sample-other:production:key",
                    "foundry:lease:v1:sample.coordination:staging:production:key",
                    "foundry:lease:v1:sample.coordination2:production:key"):
            self.assertFalse(any(fnmatchcase(key, pattern) for pattern in patterns))

    def test_rejects_foreign_glob_crafted_and_duplicate_inventory(self):
        entry = entry_fixture()
        for prefix in ("foundry:jobs:v1:sample-other:production:", "foundry:jobs:v1:sample:staging:",
                       "foundry:*:v1:sample:production:", "foundry:jobs:v1:sample.*:production:",
                       "foundry:jobs:v1:sample:production:extra:", "foundry:jobs:v1:sample[x]:production:"):
            with self.subTest(prefix=prefix), self.assertRaises(DeployError):
                redis_acl.redis_grant_patterns(entry, inventory([prefix]))
        with self.assertRaises(DeployError):
            redis_acl.redis_grant_patterns(entry, inventory() * 2)
        with self.assertRaises(DeployError):
            redis_acl.redis_grant_patterns(entry, inventory(channels=["*"]))


class MemoryRedisHost(Host):
    def __init__(self, root):
        super().__init__(Paths(root))
        self.saved_registry = None

    def owned_write(self, path, content, mode=0o644, owner="root", group="root"):
        atomic_write(path, content, mode=mode)

    def journal(self, registry, entry=None):
        self.saved_registry = copy.deepcopy(registry)


class RedisGrantTransitionTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.host = MemoryRedisHost(Path(self.temp.name))
        self.entry = entry_fixture()
        self.registry = {"shared": {"redis": {"owner": "foundry", "port": REDIS_PORT, "aclfile": REDIS_ACLFILE}},
                         "instances": {self.entry["profile"]["instance_id"]: self.entry}}
        self.password = "test-password"
        line = redis_acl._acl_line(self.entry, self.password)
        self.path = self.host.paths.p(REDIS_ACLFILE)
        self.path.parent.mkdir(parents=True)
        self.foreign = "# unchanged declaration\r\nuser other off\r\n"
        self.path.write_bytes((self.foreign + line + "\n").encode())
        self.path.chmod(0o640)
        redis_acl._save_state(self.host, self.entry, {
            "instance_id": self.entry["profile"]["instance_id"],
            "committed": {"patterns": [], "channels": [], "sha256": hashlib.sha256(line.encode()).hexdigest()},
        })
        self.live = self.live_policy([])
        self.patterns = ["foundry:lease:v1:sample.coordination:production:*"]
        self.addCleanup(patch.stopall)
        patch.object(redis_acl, "read_credentials", return_value={"redis_password": self.password}).start()
        patch.object(redis_acl, "_redis", side_effect=self.redis_command).start()
        self.fail_set = False

    def live_policy(self, patterns, channels=()):
        rules = redis_acl.redis_rules(self.entry, self.password, patterns, channels=channels)
        return {"flags": ["on"], "passwords": [hashlib.sha256(self.password.encode()).hexdigest()],
                "keys": " ".join(rule for rule in rules if rule.startswith("~")),
                "channels": " ".join(rule for rule in rules if rule.startswith("&")),
                "commands": "-@all " + " ".join("+" + command for command in sorted(redis_acl.REDIS_COMMANDS)),
                "selectors": []}

    def redis_command(self, host, *args, **kwargs):
        if args[:2] == ("ACL", "GETUSER"):
            return self.live
        if args[:2] == ("ACL", "SETUSER"):
            if self.fail_set:
                raise DeployError("injected", "Injected live update interruption.")
            self.live = self.live_policy([rule[1:] for rule in args[3:] if rule.startswith("~")],
                                         [rule[1:] for rule in args[3:] if rule.startswith("&")])
            return "OK"
        if args[:2] == ("ACL", "DRYRUN"):
            command, key = args[3:5] if len(args) > 4 else (args[3], "")
            field = "keys" if command in ("GET", "SET") else "channels" if command in ("PUBLISH", "SUBSCRIBE") else None
            if field and any(fnmatchcase(key, pattern[1:]) for pattern in self.live[field].split()):
                return "OK"
            return {"error": "NOPERM"}
        raise AssertionError(args)

    def test_widen_narrow_preserves_other_bytes_and_public_registry_has_no_hash(self):
        self.assertTrue(redis_acl.reconcile_redis_grants(self.host, self.registry, self.entry, self.patterns))
        self.assertTrue(self.path.read_bytes().startswith(self.foreign.encode()))
        self.assertEqual([self.patterns[0][:-1]], self.entry["redis_grants"]["keys"])
        public = json.dumps(self.host.saved_registry)
        self.assertNotIn(hashlib.sha256(self.password.encode()).hexdigest(), public)
        self.assertNotIn("sha256", public)
        self.assertFalse(redis_acl.reconcile_redis_grants(self.host, self.registry, self.entry, self.patterns))
        self.assertTrue(redis_acl.reconcile_redis_grants(self.host, self.registry, self.entry, []))
        self.assertEqual("", self.live["keys"])

    def test_interrupted_disk_publication_resumes_original_target(self):
        self.fail_set = True
        with self.assertRaises(DeployError):
            redis_acl.reconcile_redis_grants(self.host, self.registry, self.entry, self.patterns)
        self.assertEqual([], self.entry["redis_grants"]["keys"])
        self.assertIn("~" + self.patterns[0], self.path.read_text())
        self.assertIn("pending", redis_acl._load_state(self.host, self.entry))
        self.fail_set = False
        self.assertTrue(redis_acl.reconcile_redis_grants(self.host, self.registry, self.entry, self.patterns))
        self.assertNotIn("pending", redis_acl._load_state(self.host, self.entry))

    def test_unknown_live_permissions_refuse_before_mutating_disk(self):
        self.live["keys"] = "~*"
        before = self.path.read_bytes()
        with self.assertRaises(DeployError):
            redis_acl.reconcile_redis_grants(self.host, self.registry, self.entry, self.patterns)
        self.assertEqual(before, self.path.read_bytes())

    def test_channel_widen_then_narrow_probes_the_union(self):
        old_keys = ["foundry:cache:v1:sample.portal-runtime:production:*"]
        old_channels = ["foundry:events:v1:sample.portal-runtime:production:*"]
        new_channels = ["foundry:events:v1:sample.coordination:production:*"]
        redis_acl.reconcile_redis_grants(self.host, self.registry, self.entry, old_keys, channels=old_channels)
        redis_acl.reconcile_redis_grants(self.host, self.registry, self.entry, old_keys + self.patterns,
                                         channels=old_channels + new_channels)
        self.assertEqual(set(old_channels + new_channels), {item[1:] for item in self.live["channels"].split()})
        redis_acl.reconcile_redis_grants(self.host, self.registry, self.entry, self.patterns, channels=new_channels)
        self.assertEqual("&" + new_channels[0], self.live["channels"])
        self.assertEqual("~" + self.patterns[0], self.live["keys"])
        self.assertTrue(self.path.read_bytes().startswith(self.foreign.encode()))

    def test_narrow_failure_keeps_durable_cleanup_and_resumes_after_process_loss(self):
        redis_acl.reconcile_redis_grants(self.host, self.registry, self.entry, self.patterns)
        self.fail_set = True
        with self.assertRaises(DeployError):
            redis_acl.reconcile_redis_grants(self.host, self.registry, self.entry, [])
        state = redis_acl._load_state(self.host, self.entry)
        self.assertEqual("disk_published", state["transition"]["phase"])
        operation = state["transition"]["operation_id"]
        self.assertEqual(operation, self.entry["cleanup_pending"]["redis_acl"]["operation_id"])
        self.assertEqual(operation, self.host.saved_registry["instances"][self.entry["profile"]["instance_id"]]["redis_grants"]["pending_operation"])
        self.assertEqual(self.patterns[0][:-1], state["transition"]["old_inventory"]["keys"][0])
        self.assertEqual([], state["transition"]["final_target"]["keys"])
        self.fail_set = False
        redis_acl.reconcile_redis_grants(self.host, self.registry, self.entry, [])
        self.assertNotIn("redis_acl", self.entry["cleanup_pending"])
        self.assertIsNone(self.entry["redis_grants"]["pending_operation"])

    def test_pending_target_cannot_be_replaced(self):
        self.fail_set = True
        with self.assertRaises(DeployError):
            redis_acl.reconcile_redis_grants(self.host, self.registry, self.entry, self.patterns)
        before = self.path.read_bytes()
        with self.assertRaises(DeployError) as raised:
            redis_acl.reconcile_redis_grants(self.host, self.registry, self.entry, [])
        self.assertEqual("redis_acl_pending", raised.exception.code)
        self.assertEqual(before, self.path.read_bytes())

    def test_unknown_disk_line_and_missing_live_user_are_conflicts(self):
        before = self.path.read_bytes()
        self.path.write_bytes(before + b"# stable\n")
        self.live = None
        with self.assertRaises(DeployError):
            redis_acl.reconcile_redis_grants(self.host, self.registry, self.entry, self.patterns)
        self.live = self.live_policy([])
        self.path.write_bytes(before.replace(b" -@all ", b" +@all "))
        with self.assertRaises(DeployError):
            redis_acl.reconcile_redis_grants(self.host, self.registry, self.entry, self.patterns)

    def test_probe_connection_error_is_not_negative_permission_evidence(self):
        def broken(host, *args, **kwargs):
            if args[:2] == ("ACL", "DRYRUN"):
                return {"error": "NOAUTH Authentication required"}
            return self.redis_command(host, *args, **kwargs)
        with patch.object(redis_acl, "_redis", side_effect=broken), self.assertRaises(DeployError):
            redis_acl.reconcile_redis_grants(self.host, self.registry, self.entry, [])

    def test_real_dryrun_bulk_string_permission_denials_are_accepted(self):
        def denied(host, *args, **kwargs):
            value = self.redis_command(host, *args, **kwargs)
            return "This user has no permissions to run this command" if isinstance(value, dict) and "error" in value else value
        with patch.object(redis_acl, "_redis", side_effect=denied):
            self.assertFalse(redis_acl.reconcile_redis_grants(self.host, self.registry, self.entry, []))

    def test_redis_8_denials_must_name_the_probed_user(self):
        user = self.entry["allocations"]["redis_user"]
        for reply, accepted in ((f"User {user} has no permissions to run the 'flushdb' command", True),
                                ("User foundry-other has no permissions to run the 'flushdb' command", False)):
            def denied(host, *args, **kwargs):
                value = self.redis_command(host, *args, **kwargs)
                return reply if isinstance(value, dict) and "error" in value else value
            with self.subTest(accepted=accepted), patch.object(redis_acl, "_redis", side_effect=denied):
                if accepted:
                    self.assertFalse(redis_acl.reconcile_redis_grants(self.host, self.registry, self.entry, []))
                else:
                    with self.assertRaises(DeployError):
                        redis_acl.reconcile_redis_grants(self.host, self.registry, self.entry, [])

    def test_public_grant_drift_cannot_skip_reconciliation(self):
        self.entry["redis_grants"]["keys"] = [self.patterns[0][:-1]]
        with self.assertRaises(DeployError) as raised:
            redis_acl.reconcile_redis_grants(self.host, self.registry, self.entry, self.patterns)
        self.assertEqual("redis_ownership", raised.exception.code)
        self.assertEqual("", self.live["keys"])

    def test_metadata_is_enforced_before_grant_mutation(self):
        self.path.chmod(0o644)
        with self.assertRaises(DeployError):
            redis_acl.reconcile_redis_grants(self.host, self.registry, self.entry, self.patterns)

    def test_initial_provision_recovers_disk_before_live_without_changing_other_lines(self):
        self.registry["shared"]["redis"]["state"] = "ready"
        self.entry["provisioning"]["completed_steps"] = []
        redis_acl._state_path(self.host, self.entry).unlink()
        self.path.write_bytes(self.foreign.encode())
        self.live = None
        self.fail_set = True
        with self.assertRaises(DeployError):
            redis_acl.provision(self.host, self.registry, self.entry, {"redis_password": self.password})
        self.assertTrue(self.path.read_bytes().startswith(self.foreign.encode()))
        self.assertEqual("provision_disk", redis_acl._load_state(self.host, self.entry)["phase"])
        self.assertEqual([], self.entry["provisioning"]["completed_steps"])
        self.fail_set = False
        redis_acl.provision(self.host, self.registry, self.entry, {"redis_password": self.password})
        self.assertEqual(["redis"], self.entry["provisioning"]["completed_steps"])
        self.assertEqual("", self.live["keys"])
        self.assertEqual("", self.live["channels"])
        self.assertTrue(self.path.read_bytes().startswith(self.foreign.encode()))

    def test_initial_provision_rejects_existing_live_user_without_claim(self):
        self.registry["shared"]["redis"]["state"] = "ready"
        self.entry["provisioning"]["completed_steps"] = []
        redis_acl._state_path(self.host, self.entry).unlink()
        self.path.write_bytes(self.foreign.encode())
        with self.assertRaises(DeployError):
            redis_acl.provision(self.host, self.registry, self.entry, {"redis_password": self.password})
        self.assertEqual(self.foreign.encode(), self.path.read_bytes())


class RedisServiceHost(MemoryRedisHost):
    def __init__(self, root):
        super().__init__(root)
        self.calls = []
        self.account = None
        self.active = False
        self.occupied = False
        self.fail_start = False
        self.paths.etc.mkdir(parents=True)
        meminfo = self.paths.p("/proc/meminfo")
        meminfo.parent.mkdir(parents=True)
        meminfo.write_text("MemTotal:       8388608 kB\n")
        for binary in ("redis-server", "redis-cli"):
            path = self.paths.p("/usr/bin/" + binary)
            path.parent.mkdir(parents=True, exist_ok=True)
            path.touch()

    def run(self, args, **kwargs):
        args = [str(arg) for arg in args]
        self.calls.append(args)
        output, code = "", 0
        if args[:2] == ["systemctl", "show"]:
            exists = self.paths.p(redis_acl.REDIS_UNIT_PATH).exists()
            output = ("LoadState=" + ("loaded" if exists else "not-found") + "\nActiveState="
                      + ("active" if self.active else "inactive") + "\nFragmentPath="
                      + (redis_acl.REDIS_UNIT_PATH if exists else "") + "\n")
        elif args[:2] == ["getent", "passwd"]:
            output, code = (self.account, 0) if self.account else ("", 2)
        elif args[:2] == ["getent", "group"]:
            output, code = ("foundry-redis:x:998:", 0) if self.account else ("", 2)
        elif args[0] == "useradd":
            marker = args[args.index("--comment") + 1]
            self.account = f"foundry-redis:x:998:998:{marker}:{redis_acl.REDIS_DATA}:/usr/sbin/nologin"
        elif args[0] == "install":
            Path(args[-1]).mkdir(parents=True, exist_ok=True, mode=0o750)
        elif args[0] == "ss":
            output = "LISTEN 0 511 [::1]:56379 [::]:*" if self.occupied or self.active else ""
        elif args == ["/usr/bin/redis-server", "--version"]:
            output = "Redis server v=7.0.15 sha=00000000:0 malloc=jemalloc-5.3.0 bits=64"
        elif args == ["redis-cli", "--help"]:
            output = "--tls --sni --json"
        elif args == ["systemctl", "enable", "--now", redis_acl.REDIS_UNIT]:
            if self.fail_start:
                raise DeployError("injected", "Injected start interruption")
            self.active = True
        elif args != ["systemctl", "daemon-reload"]:
            raise AssertionError(args)
        return SimpleNamespace(returncode=code, stdout=output, stderr="")


class RedisServicePreparationTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.host = RedisServiceHost(Path(self.temp.name))
        self.registry = {"host_id": "10000000-0000-4000-8000-000000000001", "shared": {}, "instances": {}}
        self.cert = Path(self.temp.name) / "certificate.pem"
        self.key = Path(self.temp.name) / "key.pem"
        self.cert.write_text("test certificate\n")
        self.key.write_text("test key\n")
        self.addCleanup(patch.stopall)
        self.packages = patch("packages.install_missing").start()
        self.tls = patch("postgres.prepare_tls", return_value={"ca": self.cert, "cert": self.cert, "key": self.key}).start()
        self.trust = patch("postgres.record_trust_probe").start()
        self.probe = patch.object(redis_acl, "_verify_runtime").start()

    def test_owned_service_uses_installed_binary_and_never_default_unit(self):
        redis_acl.prepare(self.host, self.registry)
        self.packages.assert_not_called()
        shared = self.registry["shared"]["redis"]
        self.assertEqual("ready", shared["state"])
        self.assertEqual(56379, shared["port"])
        self.assertEqual("redis-server@foundry.service", shared["unit"])
        self.assertFalse(any("redis-server.service" in call for call in self.host.calls))
        config = self.host.paths.p(redis_acl.REDIS_CONFIG).read_text()
        for expected in ("port 0", "tls-port 56379", "appendonly yes", "appendfsync everysec",
                         "maxmemory-policy noeviction", "save 60 10000", "tls-auth-clients no"):
            self.assertIn(expected + "\n", config)
        path = self.host.paths.p(REDIS_ACLFILE)
        self.assertEqual(0o640, path.stat().st_mode & 0o777)
        self.assertTrue(path.read_text().startswith("user default reset off\nuser foundry-admin reset on #"))
        self.assertNotIn("redis_password", json.dumps(shared))
        self.trust.assert_called_once_with(self.host, self.registry, "redis")
        self.assertEqual("ready", redis_acl.inspect(self.host, self.registry)["state"])

    def test_ready_retry_reverifies_trust_without_service_restart(self):
        redis_acl.prepare(self.host, self.registry)
        before = self.host.paths.p(REDIS_ACLFILE).read_bytes()
        self.host.calls = []
        redis_acl.prepare(self.host, self.registry)
        self.assertEqual(2, self.tls.call_count)
        self.assertEqual(2, self.trust.call_count)
        self.assertEqual(2, self.probe.call_count)
        self.assertFalse(any(call[:2] in (["systemctl", "enable"], ["systemctl", "restart"]) for call in self.host.calls))
        self.assertEqual(before, self.host.paths.p(REDIS_ACLFILE).read_bytes())

    def test_service_start_interruption_reuses_admin_and_reservation(self):
        self.host.fail_start = True
        with self.assertRaises(DeployError):
            redis_acl.prepare(self.host, self.registry)
        self.trust.assert_not_called()
        before = (self.host.paths.etc / "host-secrets.json").read_bytes()
        allocation = self.registry["shared"]["redis"]["ownership_token"]
        self.host.fail_start = False
        redis_acl.prepare(self.host, self.registry)
        self.assertEqual(allocation, self.registry["shared"]["redis"]["ownership_token"])
        self.assertEqual(before, (self.host.paths.etc / "host-secrets.json").read_bytes())
        self.assertEqual("ready", self.registry["shared"]["redis"]["state"])

    def test_conflicting_paths_or_listener_block_before_mutation(self):
        self.host.occupied = True
        with self.assertRaises(DeployError):
            redis_acl.prepare(self.host, self.registry)
        self.assertEqual({}, self.registry["shared"])
        self.packages.assert_not_called()
        self.host.occupied = False
        self.host.paths.p("/etc/foundry/redis").mkdir()
        with self.assertRaises(DeployError):
            redis_acl.prepare(self.host, self.registry)
        self.assertEqual({}, self.registry["shared"])

    def test_admin_creation_preserves_other_host_secret_fields(self):
        path = self.host.paths.etc / "host-secrets.json"
        atomic_write(path, json.dumps({"unrelated": "retained"}), mode=0o600)
        redis_acl.prepare(self.host, self.registry)
        value = json.loads(path.read_text())
        self.assertEqual("retained", value["unrelated"])
        self.assertEqual("foundry-admin", value["redis_user"])

    def test_unknown_ready_configuration_is_not_overwritten(self):
        redis_acl.prepare(self.host, self.registry)
        path = self.host.paths.p(redis_acl.REDIS_CONFIG)
        path.write_text(path.read_text() + "maxmemory-policy allkeys-lru\n")
        before = path.read_bytes()
        with self.assertRaises(DeployError):
            redis_acl.prepare(self.host, self.registry)
        self.assertEqual(before, path.read_bytes())

    def test_private_reservation_survives_missing_public_journal(self):
        self.host.fail_start = True
        with self.assertRaises(DeployError):
            redis_acl.prepare(self.host, self.registry)
        token = self.registry["shared"]["redis"]["ownership_token"]
        self.registry["shared"].pop("redis")
        self.host.fail_start = False
        redis_acl.prepare(self.host, self.registry)
        self.assertEqual(token, self.registry["shared"]["redis"]["ownership_token"])


if __name__ == "__main__":
    unittest.main()
