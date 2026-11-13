"""Independent cleanup uses durable intent, never a surviving executor stack."""
from contextlib import ExitStack
from dataclasses import replace
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import Mock, patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "lib"))
import admission
import contracts as c
import feature_lifecycle as lifecycle
import feature_network as network
from host import Host, Paths
import operations
import release
import services
from test_admission import attempt
from test_host_capabilities import INSTANCE, OPERATION, INVOCATION, BOOT, DIGEST, OTHER, closure, selection
from test_services import inventory_fixture


class OperationTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.host = Host(Paths(Path(temporary.name).resolve()))
        self.inventory, self.entry, _ = inventory_fixture()
        self.entry["configuration_revision"] = 7
        self.request = {"schema": 1, "operation_id": OPERATION, "action": "restart", "project": {},
                        "profile": self.entry["profile"]}
        self.pin = {"schema": 1, "operation_id": OPERATION, "instance_id": INSTANCE, "boot_id": BOOT,
                    "invocation_id": INVOCATION, "bundle_sha256": DIGEST,
                    "request_sha256": c.canonical_digest(self.request)}
        self.store = admission.AdmissionStore(self.host, INSTANCE)
        self.registry = {"instances": {INSTANCE: self.entry}}
        self.state = {"status": "deploying", "history": [], "last_attempt": attempt(), "current": None, "previous": None}

    def context(self):
        transition = lifecycle.transition_request(self.entry, self.inventory, OPERATION)
        operations.bind_transition(self.host, self.entry, self.inventory, transition)
        stage = operations.staging(self.host, self.entry, OPERATION)
        admission.protected_directory(self.host, stage / "artifacts", 0o700)
        (stage / "artifacts/unpublished").write_text("retained")
        return stage

    def cleanup_dependencies(self, stack):
        stack.enter_context(patch.object(self.host, "require_identity", return_value=(self.registry, self.entry)))
        stack.enter_context(patch.object(self.host, "load_state", return_value=self.state))
        stack.enter_context(patch.object(self.host, "save_state"))
        stack.enter_context(patch.object(release, "_save_operation"))

    def test_executor_death_after_open_withdraws_stops_and_blocks_every_later_start(self):
        stage = self.context()
        # Simulate death after opening (or after commit but before completion receipt).
        self.state["last_attempt"] = attempt("succeeded")
        self.store.cleanup_status(OPERATION, INVOCATION, "armed")
        events = []
        def withdraw(*args):
            with self.assertRaises(c.DeployError) as raised:
                admission.AdmissionStore(self.host, INSTANCE).require_cleanup_clear(OPERATION)
            self.assertEqual(raised.exception.code, "operation_cleanup_pending")
            events.append("withdraw")
        with ExitStack() as stack:
            self.cleanup_dependencies(stack)
            stack.enter_context(patch.object(network, "withdraw", side_effect=withdraw))
            stack.enter_context(patch.object(lifecycle, "stop_units", side_effect=lambda h, inv, names: events.extend(names)))
            stack.enter_context(patch.object(network, "close", side_effect=lambda *a: (events.append("prove-closed"), c.ClosureEvidence.parse(closure()))[1]))
            self.assertEqual(operations.cleanup(self.host, self.request, self.pin), 0)
        self.assertEqual(events, ["withdraw", "worker", "relay", "http", "prove-closed"])
        self.assertEqual(self.state["last_attempt"]["outcome"], "needs_reconciliation")
        self.assertTrue(stage.exists())
        value = c.read_json(self.store.durable / "cleanup" / (OPERATION + ".json"))
        self.assertEqual(value["status"], "closed")
        with self.assertRaises(c.DeployError):
            admission.AdmissionStore(self.host, INSTANCE).require_cleanup_clear(OPERATION)

    def test_cleanup_attempts_every_unit_even_when_withdrawal_or_exit_proof_fails(self):
        stage = self.context()
        stopped = []
        def stop(host, inventory, names):
            stopped.extend(names)
            if names == ("worker",):
                raise c.DeployError("feature_stop_unverified", "synthetic")
        with ExitStack() as stack:
            self.cleanup_dependencies(stack)
            stack.enter_context(patch.object(network, "withdraw", side_effect=c.DeployError("closure", "synthetic")))
            stack.enter_context(patch.object(lifecycle, "stop_units", side_effect=stop))
            proof = stack.enter_context(patch.object(network, "close", return_value=c.ClosureEvidence.parse(closure())))
            self.assertEqual(operations.cleanup(self.host, self.request, self.pin), 1)
        self.assertEqual(stopped, ["worker", "relay", "http"])
        proof.assert_called_once()
        self.assertEqual(c.read_json(self.store.durable / "cleanup" / (OPERATION + ".json"))["status"], "pending")
        self.assertTrue(stage.exists())

    def test_cleanup_requires_empty_cgroup_not_just_a_stopped_main_pid(self):
        group = self.host.paths.p("/sys/fs/cgroup/unit/child")
        group.mkdir(parents=True)
        (group / "cgroup.procs").write_text("23\n")
        observed = "MainPID=0\nControlPID=0\nControlGroup=/unit\nActiveState=inactive\nJob=0\n"
        with patch.object(self.host, "run", return_value=subprocess.CompletedProcess([], 0, observed, "")):
            with self.assertRaises(c.DeployError) as raised:
                lifecycle.stop_units(self.host, self.inventory, ("relay",))
            self.assertEqual(raised.exception.code, "feature_stop_unverified")
            (group / "cgroup.procs").write_text("")
            lifecycle.stop_units(self.host, self.inventory, ("relay",))

    def test_success_requires_all_durable_completion_records_and_removes_only_its_staging(self):
        stage = self.context()
        other = operations.staging(self.host, self.entry, self.inventory.resolution.id)
        admission.protected_directory(self.host, other, 0o700)
        result = {"ok": True}
        root = operations.directory(self.host, OPERATION)
        c.atomic_json(root / "result.json", result)
        self.state["last_attempt"] = attempt("succeeded")
        self.store.record_operation(self.state["last_attempt"])
        selected = c.DeploymentSelection.parse(selection())
        selected = replace(selected, admission=replace(selected.admission, disposition=c.AdmissionDisposition.COMMITTED))
        self.entry["deployment_selection"] = c.record_document(selected)
        with ExitStack() as stack:
            self.cleanup_dependencies(stack)
            stack.enter_context(patch.object(lifecycle, "load", return_value=(self.store, selected, self.inventory, {})))
            stops = stack.enter_context(patch.object(lifecycle, "stop_units"))
            network_close = stack.enter_context(patch.object(network, "close"))
            self.assertFalse(operations.completion(self.host, self.entry, self.pin))
            c.atomic_json(root / "complete.json", {"schema": 1, "operation_id": OPERATION,
                          "invocation_id": INVOCATION, "result_sha256": c.canonical_digest(result)})
            self.assertTrue(operations.completion(self.host, self.entry, self.pin))
            self.assertEqual(operations.cleanup(self.host, self.request, self.pin), 0)
            stops.assert_not_called()
            network_close.assert_not_called()
            self.store.require_cleanup_clear()
            c.atomic_json(root / "result.json", {"ok": False})
            self.assertFalse(operations.completion(self.host, self.entry, self.pin))
        self.assertFalse(stage.exists())
        self.assertTrue(other.exists())
        self.assertNotIn("releases", stage.relative_to(self.host.paths.opt(self.entry["name"])).parts)

    def test_activation_refuses_without_loaded_pinned_stop_post_owner(self):
        properties = {"Type": "exec", "KillMode": "control-group", "Restart": "no", "Transient": "yes", "DropInPaths": ""}
        for directive, action in (("ExecStart", "operation"), ("ExecStopPost", "operation-cleanup")):
            properties[directive] = "{ path=/usr/bin/python3 ; argv[]=" + " ".join(operations.command(action, OPERATION, DIGEST)) + " ; ignore_errors=no ; }"
        def show(*args, **kwargs):
            return subprocess.CompletedProcess([], 0, "\n".join(k + "=" + v for k, v in properties.items()), "")
        with patch.dict(os.environ, INVOCATION_ID=INVOCATION), patch.object(admission, "boot_id", return_value=BOOT), \
                patch.object(self.host, "run", side_effect=show):
            operations.verify_executor(self.host, self.request, self.pin)
            properties["ExecStopPost"] = properties["ExecStopPost"].replace(DIGEST, OTHER)
            with self.assertRaises(c.DeployError) as raised:
                operations.verify_executor(self.host, self.request, self.pin)
            self.assertEqual(raised.exception.code, "capability_unavailable")
            with self.assertRaises(c.DeployError):
                operations.verify_executor(self.host, self.request, None)

    def test_operation_request_installs_cleanup_in_the_same_unit_and_bundle(self):
        root = operations.directory(self.host, OPERATION)
        def prepare(*args):
            admission.protected_directory(self.host, root, 0o700)
            return self.pin
        def run(argv, **kwargs):
            self.assertEqual(argv[0], "systemd-run")
            expected = "--property=ExecStopPost=" + services.command_line(operations.command("operation-cleanup", OPERATION, DIGEST))
            self.assertIn(expected, argv)
            self.assertEqual(argv[-6:], operations.command("operation", OPERATION, DIGEST))
            c.atomic_json(root / "result.json", {"ok": True})
            return subprocess.CompletedProcess(argv, 0, "", "")
        with patch.object(self.host, "require_identity", return_value=(self.registry, self.entry)), \
                patch.object(network, "capability"), patch.object(operations, "prepare", side_effect=prepare), \
                patch.object(self.host, "run", side_effect=run):
            self.assertEqual(lifecycle.request_operation(self.host, "restart", {}, self.entry["profile"], operation_id=OPERATION), {"ok": True})

    def test_fixed_runtime_cleanup_keeps_original_invocation_pin_when_tools_change(self):
        source = Path(__file__).resolve().parents[1] / "server/runtime_v1.py"
        spec = importlib.util.spec_from_file_location("operation_runtime_test", source)
        runtime = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(runtime)
        root = operations.directory(self.host, OPERATION)
        admission.protected_directory(self.host, root, 0o700)
        owner = {k: v for k, v in self.pin.items() if k not in ("boot_id", "invocation_id")}
        c.atomic_json(root / "owner.json", owner)
        c.atomic_json(root / "request.json", self.request)
        def proc(path, *args, **kwargs):
            return BOOT if path.name == "boot_id" else "0::/system.slice/foundry-operation-" + OPERATION + ".service\n"
        with patch.object(runtime, "STATE", self.store.durable.parent), \
                patch.object(runtime, "PINS", self.host.paths.p("/run/pins")), \
                patch.object(runtime, "owned", side_effect=lambda p: p), patch.object(runtime.os, "fchown"), \
                patch.object(runtime.os, "geteuid", return_value=0), patch.dict(os.environ, INVOCATION_ID=INVOCATION), \
                patch.object(Path, "read_text", proc):
            first = runtime.operation_pin("operation", OPERATION, DIGEST)
            # The helper passed on a later phase may have changed, but its import
            # must still come from the first invocation's immutable bundle.
            second = runtime.operation_pin("operation-cleanup", OPERATION, OTHER)
        self.assertEqual(first, self.pin)
        self.assertEqual(second, first)

    def test_activate_previous_selection_refuses_before_owner_staging_or_host_effects(self):
        previous = self.inventory.deployment.release
        current = "20261114T000000Z-abcdef12"
        self.state.update(current={"version": current}, previous={"version": previous}, last_attempt=None)
        archive = self.host.paths.root / "archive"
        archive.write_text("header fixture")
        def inspect(host, path, destination, entry, checksum, *, manifest_check):
            self.assertIsNone(destination)
            manifest_check({"version": previous})
        with patch.object(self.host, "require_identity", return_value=(self.registry, self.entry)), \
                patch.object(self.host, "load_state", return_value=self.state), patch.object(network, "capability"), \
                patch.object(release, "unpack_archive", side_effect=inspect), \
                patch.object(operations, "prepare") as prepare, patch.object(self.host, "run") as run:
            with self.assertRaises(c.DeployError) as raised:
                lifecycle.request_operation(self.host, "activate", {}, self.entry["profile"], operation_id=OPERATION,
                                            archive=str(archive), archive_sha256=DIGEST)
            self.assertEqual(raised.exception.code, "capability_unavailable")
            self.assertEqual(raised.exception.detail, "Feature rollback requires " + lifecycle.LATER + ".")
            prepare.assert_not_called()
            run.assert_not_called()
        self.assertFalse(operations.directory(self.host, OPERATION).exists())
        self.assertFalse(operations.staging(self.host, self.entry, OPERATION).exists())
        lifecycle.record_selection(self.host, self.entry, self.state, previous)
        self.state["previous"] = None
        self.state["history"] = []
        with self.assertRaises(c.DeployError):
            lifecycle.check_activation(self.host, self.entry, self.state, {"version": previous})
        lifecycle.check_activation(self.host, self.entry, self.state, {"version": current})

    def test_boot_refuses_opening_when_a_prerequisite_becomes_unhealthy(self):
        selected = c.DeploymentSelection.parse(selection())
        selected = replace(selected, enabled_roles=tuple(self.inventory.roles),
                           admission=replace(selected.admission, disposition=c.AdmissionDisposition.COMMITTED))
        self.store._write(self.store.runtime / "boot.json", c.ClosureEvidence.parse(closure()))
        deadlines = []
        def ready(host, entry, inventory, name, deadline):
            deadlines.append(deadline)
            return True
        with patch.object(admission.AdmissionStore, "_selection", return_value=(selected, self.inventory.resolution)), \
                patch.object(admission.AdmissionStore, "_completed"), patch.object(self.host, "run"), \
                patch.object(lifecycle, "ready", side_effect=ready), \
                patch.object(services, "wait_ready_set", return_value=False) as whole, \
                patch.object(network, "open_public") as opening:
            with self.assertRaises(c.DeployError) as raised:
                lifecycle.open_boot(self.host, self.entry, self.inventory)
            self.assertEqual(raised.exception.code, "readiness_failed")
            opening.assert_not_called()
            self.assertEqual(whole.call_args.args[2], selected.enabled_roles)
            self.assertEqual(deadlines, [whole.call_args.args[3]] * 3)
