"""Systemd enablement authority and the nonstarting preview migration."""
from dataclasses import replace
from pathlib import Path
import subprocess
import sys
import tempfile
import time
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "lib"))
import admission
import contracts as c
import feature_lifecycle as lifecycle
from host import Host, Paths
import release
import services
from test_host_capabilities import selection
from test_services import inventory_fixture


class FeatureEnablementTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.host = Host(Paths(Path(temporary.name).resolve()))
        self.inventory, self.entry, _ = inventory_fixture()
        self.entry["deployment_selection"] = selection()
        self.selected = replace(c.DeploymentSelection.parse(selection()),
                                admission=replace(c.AdmissionRecord.parse(selection()["admission"]),
                                                  disposition=c.AdmissionDisposition.COMMITTED))
        self.store = admission.AdmissionStore(self.host, self.inventory.profile.instance_id)
        self.enabled, self.running, self.commands = set(), {"http", "relay"}, []
        self.host.run = self.command

    def command(self, args, **kwargs):
        self.commands.append(args)
        if args[:2] == ["systemctl", "is-enabled"]:
            enabled = args[2] in self.enabled
            return subprocess.CompletedProcess(args, 0 if enabled else 1, "enabled" if enabled else "disabled", "")
        if args[:2] == ["systemctl", "enable"]:
            self.enabled.update(args[2:])
            return subprocess.CompletedProcess(args, 0, "", "")
        raise AssertionError(args)

    def test_preview_migration_only_enables_running_units_and_never_starts(self):
        with patch.object(lifecycle, "load", return_value=(self.store, self.selected, self.inventory, {})), \
                patch.object(lifecycle, "install_units") as install, \
                patch.object(release, "_matches_executable", return_value=True), \
                patch.object(release, "service", side_effect=lambda host, unit:
                    {"ActiveState": "active" if unit in {self.inventory.units[n] for n in self.running} else "inactive"}):
            result = lifecycle.migrate_enablement(self.host, {"tools": {"generation": 13}}, self.entry)
            self.assertEqual(result["adopt_running"], ["http", "relay"])
            self.assertEqual(result["starts"], [])
            self.assertEqual(self.enabled, {self.inventory.units[n] for n in self.running})
            install.assert_called_once()
            count = len(self.commands)
            self.assertEqual(lifecycle.migrate_enablement(self.host, {"tools": {"generation": 13}}, self.entry), result)
            self.assertEqual(len(self.commands), count)
        self.assertFalse(any(args[:2] in (["systemctl", "start"], ["systemctl", "restart"]) for args in self.commands))

    def test_enablement_adoption_sets_sticky_release_one_floor(self):
        selected = replace(self.selected, minimum_generation=12)
        with patch.object(lifecycle, "load", return_value=(self.store, selected, self.inventory, {})), \
                patch.object(lifecycle, "install_units"), patch.object(self.host, "journal") as journal, \
                patch.object(release, "_matches_executable", return_value=True), \
                patch.object(release, "service", return_value={"ActiveState": "inactive"}):
            lifecycle.migrate_enablement(self.host, {"tools": {"generation": 13}}, self.entry)
        self.assertEqual(self.entry["deployment_selection"]["minimum_generation"], 13)
        self.assertEqual(c.read_json(self.store.durable / "selection.json")["minimum_generation"], 13)
        journal.assert_called_once()

    def test_pending_preview_migration_does_not_enable_or_start_anything(self):
        pending = c.DeploymentSelection.parse(selection())
        with patch.object(lifecycle, "load", return_value=(self.store, pending, self.inventory, {})):
            result = lifecycle.migrate_enablement(self.host, {"tools": {"generation": 13}}, self.entry)
        self.assertEqual(result["state"], "pending")
        self.assertEqual(self.commands, [])

    def test_takeover_start_cannot_override_disabled_unit(self):
        self.enabled = {self.inventory.units["http"]}
        with self.assertRaises(c.DeployError) as caught:
            lifecycle.start_units(self.host, self.inventory, ("relay",), time.monotonic() + 1)
        self.assertEqual(caught.exception.code, "service_enablement")
        self.assertFalse(any(args[1] == "start" for args in self.commands))

    def test_reported_enablement_ignores_stored_audit_projection(self):
        from topology import validate_topology
        self.entry["topology"] = validate_topology(self.inventory.topology.projection())
        self.entry["profile"] = self.inventory.profile.projection()
        self.enabled = {self.inventory.units["http"]}
        self.assertEqual(release.enabled_roles(self.host, self.entry), ["http"])


if __name__ == "__main__":
    unittest.main()
