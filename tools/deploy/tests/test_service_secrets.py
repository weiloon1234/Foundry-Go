"""Private service configuration, immutable review inputs and file delivery."""
import json
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "lib"))
import contracts as c
from host import Host, Paths
import runtime_config as config
import topology as t
from test_contracts import fixture
from test_host_capabilities import declaration, profile


class ServiceSecretTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.host = Host(Paths(Path(temporary.name).resolve()))
        self.entry = fixture()[2]
        self.raw = declaration()
        self.raw["secrets"] = {"relay-token": {"prompt": "Relay token"}, "retained": {}}
        self.raw["roles"][1]["secret_files"] = {"TOKEN_FILE": "relay-token"}
        self.topology = t.parse_topology(self.raw)
        self.profile = t.parse_profile(profile(), self.topology)
        self.entry["profile"] = self.profile.projection()
        self.entry["topology"] = {**self.raw, "sha256": self.topology.sha256}
        self.entry["configuration_revision"] = 7
        self.key = t.SERVICE_SECRET_PREFIX + "RELAY_TOKEN"
        self.value = "service-secret-canary"

    def stage(self, changes=None):
        return config.stage_service_secrets(self.host, self.entry, self.topology,
                                           changes if changes is not None else {self.key: self.value})

    def test_private_snapshot_and_declared_file_delivery(self):
        paths = self.stage()
        self.assertEqual(config.service_secret_snapshot(self.host, self.entry), {})
        path = Path(paths["relay-token"])
        self.assertEqual(path.stat().st_mode & 0o777, 0o640)
        self.assertEqual(path.read_text(), self.value + "\n")
        config.select_service_secrets(self.host, self.entry, paths)
        delivery = config.service_secret_paths(self.host, self.entry, self.topology)
        self.assertEqual(delivery, {"relay": {"TOKEN_FILE": str(path)}})
        self.assertNotIn(self.value, json.dumps(delivery))
        backup = config.service_secret_snapshot_paths(self.host, self.entry)
        self.assertEqual(set(backup), {path, path.parent / "service-secrets.json"})
        self.assertEqual(backup[0].stat().st_mode & 0o777, 0o600)

    def test_unknown_names_alias_collisions_and_namespace_collisions_refuse(self):
        for changes in ({self.key + "_OTHER": self.value}, {self.key: self.value, self.key + "_FILE": "bad"}):
            with self.subTest(changes=list(changes)), self.assertRaises(c.DeployError):
                self.stage(changes)
        self.raw["secrets"]["relay-token-file"] = {}
        with self.assertRaises(c.DeployError):
            t.parse_topology(self.raw)

    def test_missing_required_secret_refuses_before_any_command(self):
        with patch.object(self.host, "run") as command, self.assertRaises(c.DeployError):
            self.stage({})
        command.assert_not_called()

    def test_service_file_interface_cannot_override_framework_or_value_environment(self):
        for variable in ("FOUNDRY_DEPLOY_RUNTIME_FILE", "LD_PRELOAD_FILE", "APP__ENCRYPTION__KEY_FILE"):
            with self.subTest(variable=variable), self.assertRaises(c.DeployError):
                self.raw["roles"][1]["secret_files"] = {variable: "relay-token"}
                t.parse_topology(self.raw)
        self.raw["roles"][1]["secret_files"] = {"TOKEN_FILE": "relay-token"}
        self.raw["roles"][1]["environment"] = {"TOKEN": {"literal": "collision"}}
        with self.assertRaises(c.DeployError):
            t.parse_topology(self.raw)

    def test_file_input_is_confined_snapshotted_and_not_followed(self):
        paths = self.stage()
        original = Path(paths["relay-token"])
        copied = self.stage({self.key + "_FILE": str(original)})
        self.assertNotEqual(copied["relay-token"], str(original))
        self.assertEqual(Path(copied["relay-token"]).read_text(), original.read_text())
        link = original.with_name("link.secret")
        link.symlink_to(original)
        for bad in (str(link), "/tmp/unowned.secret"):
            with self.subTest(bad=bad), self.assertRaises(c.DeployError):
                self.stage({self.key + "_FILE": bad})
        original.chmod(0o644)
        with self.assertRaises(c.DeployError):
            self.stage({self.key + "_FILE": str(original)})

    def test_intent_is_opaque_immutable_and_does_not_select_revision(self):
        identifier = config.stage_service_secret_intent(self.host, self.entry, self.topology, self.profile,
                                                       {self.key: self.value})
        c.contract_uuid(identifier)
        paths = config.read_service_secret_intent(self.host, self.entry, identifier, self.topology, self.profile)
        self.assertEqual(self.entry["configuration_revision"], 7)
        self.assertEqual(config.service_secret_snapshot(self.host, self.entry), {})
        Path(paths["relay-token"]).write_text("changed\n")
        with self.assertRaises(c.DeployError) as error:
            config.read_service_secret_intent(self.host, self.entry, identifier, self.topology, self.profile)
        self.assertEqual(error.exception.code, "stale_plan")

    def test_rotation_retains_other_secrets_and_unchanged_values_keep_paths(self):
        paths = self.stage({self.key: self.value, t.SERVICE_SECRET_PREFIX + "RETAINED": "old-required-value"})
        config.select_service_secrets(self.host, self.entry, paths)
        self.assertEqual(self.stage(), paths)
        rotated = self.stage({self.key: "rotated"})
        self.assertEqual(rotated["retained"], paths["retained"])
        self.assertNotEqual(rotated["relay-token"], paths["relay-token"])
        self.assertEqual(Path(paths["relay-token"]).read_text(), self.value + "\n")

    def test_application_projection_never_contains_reserved_keys(self):
        credentials = {"database_password": "d" * 43, "redis_password": "r" * 43,
                       "configuration": {self.key: self.value, "APP_SETTING": "ordinary"}}
        values = config.runtime_environment(self.entry, credentials)
        self.assertNotIn(self.key, values)
        self.assertNotIn(self.value, c.dump_env(values))
        self.assertEqual(values["APP_SETTING"], "ordinary")

    def test_rendered_json_accepts_only_declared_secret_paths(self):
        self.raw["roles"][1]["configs"]["settings"]["bindings"]["/token_file"] = {"secret_file": "TOKEN_FILE"}
        parsed = t.parse_topology(self.raw)
        role = parsed.roles[1]
        bindings = dict(role.configs)["settings"].bindings
        output = config._render_service_json(b'{"listen":{"address":"", "port":0},"token_file":""}', bindings,
                    lambda arg: "/private/token.secret" if arg.kind is t.ArgumentKind.SECRET_FILE else "8200")
        self.assertIn(b"/private/token.secret", output)
        self.raw["roles"][1]["secret_files"] = {}
        with self.assertRaises(c.DeployError):
            t.parse_topology(self.raw)


if __name__ == "__main__":
    unittest.main()
