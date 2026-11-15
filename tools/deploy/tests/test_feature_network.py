"""Copy-scoped closure keeps surviving units outside the stop and allocation scope."""
from dataclasses import replace
import json
from pathlib import Path
import subprocess
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import Mock, patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "lib"))
import admission
import contracts as c
import edge
import firewall
import listeners
from host import Host, Paths
from test_host_capabilities import BOOT, DIGEST, INSTANCE, OLD, OPERATION
from test_services import inventory_fixture


class CopyClosureTests(unittest.TestCase):
    def setUp(self):
        base, self.entry, _ = inventory_fixture()
        self.copy = c.UnitIdentity(INSTANCE, "relay", 2)
        self.old = SimpleNamespace(profile=base.profile, units=dict(base.units), specs=dict(base.specs),
            resolution=replace(base.resolution, id=OLD, environments=tuple(
                replace(env, path=env.path.replace(base.resolution.id, OLD)) for env in base.resolution.environments)))
        self.current = SimpleNamespace(profile=base.profile, units={**base.units, self.copy: "foundry-app-fixture-relay@2.service"},
            specs={**base.specs, self.copy: replace(base.specs[base.identity("relay")], identity=self.copy)},
            resolution=replace(base.resolution, endpoints=base.resolution.endpoints +
                               (c.AllocatedEndpoint("relay", "control", "127.0.0.1", 18002, 2),)))
        self.entry["configuration_revision"] = base.resolution.private_revision

    def scope(self, old, current, affected=None):
        affected = (self.copy,) if affected is None else affected
        transition = c.HostTransition(OPERATION, INSTANCE, old.resolution.id, current.resolution.id,
            old.resolution.private_revision, affected, current.resolution.deployment_sha256, None)
        store = Mock()
        store._selection.return_value = (SimpleNamespace(admission=SimpleNamespace(operation_id=OPERATION)), current.resolution)
        with patch.object(listeners, "_feature_inventory", side_effect=(current, old)), \
                patch.object(admission, "AdmissionStore", return_value=store):
            return listeners._transition_scope(None, self.entry, transition, current.resolution,
                                               {**old.units, **current.units})

    def test_grow_and_shrink_scope_only_the_changed_copy(self):
        for old, current in ((self.old, self.current), (self.current, self.old)):
            with self.subTest(growing=len(old.units) < len(current.units)):
                _, _, _, scoped, retained = self.scope(old, current)
                self.assertEqual(set(scoped), {self.copy})
                self.assertEqual(retained, set(self.old.units))

    def test_missing_affected_copy_and_changed_survivor_refuse_closure(self):
        with self.assertRaises(c.DeployError):
            self.scope(self.old, self.current, (next(iter(self.old.units)),))
        first = c.UnitIdentity(INSTANCE, "relay", 1)
        self.current.specs[first] = replace(self.current.specs[first], arguments=("changed",))
        with self.assertRaises(c.DeployError) as caught:
            self.scope(self.old, self.current)
        self.assertEqual(caught.exception.code, "network_retained_changed")

    def test_partial_public_copy_scope_preserves_sibling_binding(self):
        from topology import PublicListenerBinding
        from test_host_capabilities import profile
        binding = PublicListenerBinding.parse(profile()["listeners"]["relay.ingress"])
        self.old.profile = replace(self.old.profile, listeners=(("relay.ingress", binding),))
        self.current.profile = replace(self.current.profile,
            listeners=(("relay.ingress", replace(binding, ports=(binding.ports[0], 9555))),))
        _, _, _, scoped, retained = self.scope(self.old, self.current)
        self.assertEqual(set(scoped), {self.copy})
        self.assertIn(c.UnitIdentity(INSTANCE, "relay", 1), retained)
        self.current.profile = replace(self.current.profile,
            listeners=(("relay.ingress", replace(binding, ports=(9556, 9555))),))
        with self.assertRaises(c.DeployError) as caught:
            self.scope(self.old, self.current)
        self.assertEqual(caught.exception.code, "network_retained_changed")

    def test_network_proof_and_closure_keep_exact_copies(self):
        identities = (c.UnitIdentity(INSTANCE, "relay", 1), self.copy)
        fw = firewall.FirewallObservation(INSTANCE, BOOT, True, DIGEST, (), ())
        ng = edge.EdgeObservation(INSTANCE, BOOT, ("relay", "relay@2"), True, DIGEST, ())
        evidence = listeners.ingress_evidence(OPERATION, BOOT, self.old.resolution, self.current.resolution,
            identities, DIGEST, fw, ng, claims=())
        self.assertEqual(evidence.units, identities)
        self.assertEqual({e.ordinal for e in evidence.endpoints}, {1, 2})
        removed = listeners.ingress_evidence(OPERATION, BOOT, self.current.resolution, self.old.resolution,
            (self.copy,), DIGEST, fw, replace(ng, roles=("relay@2",)), claims=())
        self.assertEqual([e.ordinal for e in removed.endpoints], [2])
        closed = listeners.complete_closure(removed, self.old.resolution, backend_exit_sha256=DIGEST,
                                            reserved=(), old_references=())
        self.assertEqual(closed.units, (self.copy,))
        self.assertIs(closed.stage, c.ClosureStage.APPLICATION_CLOSED)

    def test_removed_copy_exit_requires_empty_jobs_main_control_and_child_processes(self):
        with tempfile.TemporaryDirectory() as root:
            host = Host(Paths(Path(root)))
            unit = self.current.units[self.copy]
            group = "/system.slice/" + unit
            procs = host.paths.p("/sys/fs/cgroup" + group + "/child/cgroup.procs")
            procs.parent.mkdir(parents=True)
            procs.write_text("")
            values = {"Id": unit, "LoadState": "loaded", "ActiveState": "inactive", "MainPID": "0",
                      "ControlPID": "0", "ControlGroup": group, "Job": ""}
            def command(args, **kwargs):
                self.assertEqual(args[2], unit)
                return subprocess.CompletedProcess(args, 0, "\n".join(k + "=" + v for k, v in values.items()), "")
            host.run = Mock(side_effect=command)
            scoped = {self.copy: unit}
            proof = listeners._feature_exit(host, self.current, scoped)
            self.assertEqual(set(proof), {"relay@2"})
            for field, bad in (("MainPID", "42"), ("ControlPID", "42"), ("Job", "12"), ("LoadState", "loading")):
                previous = values[field]
                values[field] = bad
                with self.subTest(field=field), self.assertRaises(c.DeployError):
                    listeners._feature_exit(host, self.current, scoped)
                values[field] = previous
            procs.write_text("42\n")
            with self.assertRaises(c.DeployError) as caught:
                listeners._feature_exit(host, self.current, scoped)
            self.assertEqual(caught.exception.code, "network_backend_alive")
            procs.write_text("")
            host.owned_write(host.paths.p("/proc/sys/kernel/random/boot_id"), BOOT)
            receipt = c.HelperInvocation(BOOT, self.copy, c.ProcessIdentity(42, 123, "a" * 32),
                DIGEST, DIGEST, self.current.resolution.id, 1, None)
            store = admission.AdmissionStore(host, INSTANCE)
            path = store.runtime / "admitted" / self.copy.record_key / (receipt.process.invocation_id + ".json")
            host.owned_write(path, json.dumps(c.record_document(receipt)), mode=0o600)
            with patch.object(admission, "executor_alive", return_value=True):
                with self.assertRaises(c.DeployError) as caught:
                    listeners._feature_exit(host, self.current, scoped)
                self.assertEqual(caught.exception.code, "network_backend_alive")
                survivor = replace(receipt, unit=c.UnitIdentity(INSTANCE, "relay", 1))
                path.unlink()
                survivor_path = store.runtime / "admitted" / survivor.unit.record_key / path.name
                host.owned_write(survivor_path, json.dumps(c.record_document(survivor)), mode=0o600)
                self.assertEqual(set(listeners._feature_exit(host, self.current, scoped)), {"relay@2"})
