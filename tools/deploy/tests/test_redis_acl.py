"""Exact Redis grants, private journals, and resumable disk-before-live changes."""
import copy
from fnmatch import fnmatchcase
import hashlib
import json
from pathlib import Path
import sys
import tempfile
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
            field = "keys" if command == "GET" else "channels" if command == "PUBLISH" else None
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


if __name__ == "__main__":
    unittest.main()
