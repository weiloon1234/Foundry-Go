"""Role selection and atomic publication contracts, without host processes."""
import copy
import os
from pathlib import Path
import stat
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "lib"))
from contracts import DeployError, atomic_json, atomic_write
from topology import load_topology, validate_topology, instance_units
from test_contracts import fixture


class TopologyTests(unittest.TestCase):
    def test_http_only_and_optional_roles(self):
        _, _, entry = fixture()
        for names in (("http",), ("http", "scheduler"), ("http", "worker", "scheduler")):
            entry["topology"] = validate_topology({"schema": 1, "roles": [{"name": n} for n in names]})
            self.assertEqual(instance_units(entry), {n: f"foundry-{n}@abc-production.service" for n in names})

    def test_invalid_and_duplicate_roles_are_rejected(self):
        for roles in ([], [{"name": "worker"}], [{"name": "http"}, {"name": "http"}], [{"name": "http", "command": "sh"}], [{"name": "other"}]):
            with self.subTest(roles=roles), self.assertRaises(DeployError):
                validate_topology({"schema": 1, "roles": roles})

    def test_snapshot_is_owned_and_digest_detects_mutation(self):
        raw = {"schema": 1, "roles": [{"name": "http"}]}
        result = validate_topology(raw)
        raw["roles"].append({"name": "worker"})
        self.assertEqual(len(result["roles"]), 1)
        result["roles"].append({"name": "scheduler"})
        with self.assertRaises(DeployError):
            validate_topology(result, registered=True)

    def test_load_requires_explicit_app_declaration(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory).resolve()
            atomic_json(root / "deploy/topology.json", {"schema": 1, "roles": [{"name": "http"}]})
            self.assertEqual(load_topology(root)["roles"], [{"name": "http"}])

    def test_atomic_publication_has_final_metadata_before_replace(self):
        with tempfile.TemporaryDirectory() as directory:
            target = Path(directory).resolve() / "private.env"
            real_replace = os.replace
            observed = []
            def publish(source, destination):
                info = os.stat(source)
                observed.append((info.st_uid, info.st_gid, stat.S_IMODE(info.st_mode)))
                real_replace(source, destination)
            with patch("contracts.os.replace", side_effect=publish):
                atomic_write(target, "private", 0o640, owner=os.geteuid(), group=os.getegid())
            self.assertEqual(observed, [(os.geteuid(), os.getegid(), 0o640)])
            self.assertFalse(atomic_write(target, "private", 0o640))
            target.chmod(0o644)
            self.assertTrue(atomic_write(target, "private", 0o640))

    def test_same_bytes_wrong_owner_is_not_a_noop(self):
        with tempfile.TemporaryDirectory() as directory:
            target = Path(directory).resolve() / "private.env"
            atomic_write(target, "private", 0o640)
            # Simulate an owner repair without requiring privileged chown.
            with patch("contracts.os.fchown") as chown:
                self.assertTrue(atomic_write(target, "private", 0o640, owner=os.geteuid() + 1))
                self.assertEqual(chown.call_args.args[1], os.geteuid() + 1)
