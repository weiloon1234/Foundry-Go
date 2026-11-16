"""Operator orchestration through the actual server dispatcher on a fresh host."""
from contextlib import contextmanager
import copy
import json
import os
from pathlib import Path
import shutil
import tarfile
import tempfile
from unittest.mock import patch

from test_local import cli
from test_feature_wiring import FeatureWiringFixture, manager, upgrades, c, lifecycle


class OperatorFeaturePathTests(FeatureWiringFixture):
    def setUp(self):
        super().setUp()
        self.local = Path(self.enterContext(tempfile.TemporaryDirectory())).resolve()
        self.enterContext(patch.object(cli, "ROOT", self.local))
        self.enterContext(patch.object(cli, "load_topology", return_value=self.topology.projection()))
        self.enterContext(patch.object(cli, "confirm"))
        self.enterContext(patch.object(cli, "bootstrap_secrets", return_value={}))
        self.output = self.enterContext(patch.object(cli, "display"))
        self.enterContext(patch.object(cli, "say"))
        self.calls = []
        owner = self

        class SSH:
            def __init__(self, *args, **kwargs):
                pass

            def close(self):
                pass

            @contextmanager
            def staged_tools(self):
                yield "candidate-tools"

            @contextmanager
            def temporary(self):
                with tempfile.TemporaryDirectory(dir=owner.host.paths.root) as path:
                    yield path

            def upload(self, archive, destination):
                shutil.copyfile(archive, destination)

            def request(self, action, request, tools=None):
                owner.calls.append((action, tools))
                return manager.execute(owner.host, action, {**copy.deepcopy(request), **upgrades.protocol_metadata(cli.TOOLS_ROOT)})

        self.enterContext(patch.object(cli, "SSH", SSH))
        self.enterContext(patch.dict(os.environ, {"ROLE": "relay"}))
        self.profile["host"]["host_id"] = None

    def bootstrap(self, action):
        with patch.dict(os.environ, ACTION=action):
            cli.bootstrap(self.project, self.profile, {}, fresh=True)

    def test_inspect_fresh_host_uses_candidate_without_mutation(self):
        self.bootstrap("inspect")
        self.assertEqual(self.calls, [("inspect", "candidate-tools")])
        self.assertIsNone(self.host.read_registry(missing=True))
        self.assertIsNone(self.profile["host"]["host_id"])

    def test_prepare_deploy_status_logs_and_existing_inspect(self):
        self.bootstrap("prepare")
        self.assertEqual(self.calls[:3], [(name, "candidate-tools") for name in ("inspect", "bootstrap-plan", "bootstrap-apply")])
        self.assertEqual(self.host.read_registry()["host_id"], self.profile["host"]["host_id"])
        # The archive is built after bootstrap has persisted the canonical host binding.
        self.build_archive()
        with tarfile.open(self.archive) as archive:
            c.atomic_json(self.archive.parent / "manifest.json", json.load(archive.extractfile("manifest.json")))
        with patch.object(cli, "select_archive", return_value=self.archive), patch.dict(os.environ, ACTION="deploy"):
            cli.deploy(self.project, self.profile, {})
        actions = [action for action, _ in self.calls]
        self.assertEqual(actions[3:], ["sync", "status", "hello", "release-plan", "activate", "status"])
        self.assertEqual(self.host.load_state(self.host.read_registry()["instances"][self.profile["instance_id"]])["status"], "healthy")
        for action in ("status", "logs"):
            with patch.dict(os.environ, {"ACTION": action, **({"LINES": "10"} if action == "logs" else {})}):
                cli.deploy(self.project, self.profile, {})
        self.assertIn("logs", self.output.call_args.args[0])
        self.assertIn("keys", self.request("config-keys"))
        before = self.host.paths.registry.read_bytes()
        self.bootstrap("inspect")
        # Profile-free host inspection must also handle registered feature instances.
        self.assertEqual(manager.execute(self.host, "inspect", self.envelope)["host_id"], self.profile["host"]["host_id"])
        self.assertEqual(self.host.paths.registry.read_bytes(), before)

    def test_hello_bypasses_client_gate_fresh_and_existing(self):
        for prepared in (False, True):
            with self.subTest(prepared=prepared):
                if prepared:
                    self.bootstrap("prepare")
                with patch.object(upgrades, "check_client", side_effect=AssertionError("hello reached client gate")):
                    result = manager.execute(self.host, "hello", {"profile": self.profile, "topology": self.topology.projection()})
                self.assertIn("tools", result)

    def test_every_server_action_has_an_explicit_feature_gate_disposition(self):
        supported = {"hello", "inspect", "bootstrap-plan", "bootstrap-apply", "release-plan", "activate",
                     "rollback", "restart", "stop", "down", "up", "status", "logs", "report", "sync",
                     "config-keys", "config-set", "backup-configure", "backup-run", "restore-drill",
                     "recover-plan", "recover-apply", "tools-plan", "tools-apply", "upgrade-resume"}
        refused = {"capacity-plan", "capacity-apply", "keys-rotate", "backup-keys-rotate",
                   "backup-recipients-rotate", "dns-token-rotate", "reconcile"}
        self.assertEqual(set(manager.ACTIONS), supported | refused)
        for prepared in (False, True):
            if prepared:
                self.bootstrap("prepare")
            for action in supported - {"hello"}:
                with self.subTest(prepared=prepared, action=action):
                    upgrades._check_feature_request(self.host.read_registry(missing=True),
                        {**self.envelope, "profile": self.profile, "topology": self.topology.projection()}, action)

    def test_unsupported_actions_refuse_before_mutation_fresh_and_existing(self):
        actions = ("capacity-plan", "capacity-apply", "keys-rotate", "backup-keys-rotate",
                   "backup-recipients-rotate", "dns-token-rotate", "reconcile")
        for prepared in (False, True):
            if prepared:
                self.bootstrap("prepare")
            for action in actions:
                with self.subTest(prepared=prepared, action=action):
                    before = {str(p.relative_to(self.host.paths.root)): p.read_bytes()
                              for p in self.host.paths.root.rglob("*") if p.is_file() and not p.is_symlink()}
                    self.host.commands.clear()
                    with self.assertRaises(c.DeployError) as caught:
                        self.request(action, confirm=True)
                    self.assertEqual(caught.exception.code, "capability_unavailable")
                    self.assertIn("ACTION=", caught.exception.detail)
                    self.assertNotIn("post-preview", caught.exception.detail)
                    self.assertEqual(self.host.commands, [])
                    self.assertEqual(before, {str(p.relative_to(self.host.paths.root)): p.read_bytes()
                                             for p in self.host.paths.root.rglob("*") if p.is_file() and not p.is_symlink()})
        for action in actions:
            with self.assertRaises(c.DeployError) as caught:
                lifecycle.check_operation(action)
            self.assertEqual(caught.exception.code, "capability_unavailable")
