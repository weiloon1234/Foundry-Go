"""Local deployment contracts. No network, Docker daemon or server required."""
import contextlib
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import pty
import select
import signal
import subprocess
import sys
import tempfile
import tarfile
import time
import unittest
from unittest.mock import MagicMock, patch
import uuid

DEPLOY = Path(__file__).resolve().parents[1]


def module(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    loaded = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(loaded)
    return loaded


cli = module("deployment_local_cli", DEPLOY / "local" / "cli.py")
metadata = module("deployment_build_metadata", DEPLOY / "local" / "build_metadata.py")
release = module("deployment_release", DEPLOY / "lib" / "release.py")
from host import Host, Paths


def fixture():
    project = {"schema": 1, "id": str(uuid.uuid4()), "slug": "abc"}
    profile = {"schema": 2, "environment": "prod", "instance_id": str(uuid.uuid4()),
               "host": {"address": "example.com", "port": 22, "host_id": str(uuid.uuid4())},
               "runtime_environment": "production", "domain": "abc.example.com",
               "certbot_email": "ops@example.com", "architecture": "amd64", "public_env": {},
               "retention": {"server_releases": 5, "local_releases": 3, "pre_migration_dumps": 7},
               "edge_origin_lockdown": True}
    return project, profile


class BootstrapRemote:
    def close(self):
        pass

    def __init__(self, host_id, registered=None, architecture="amd64", fail_apply=False):
        self.host_id, self.registered = host_id, registered
        self.architecture, self.fail_apply = architecture, fail_apply
        self.calls = []

    @contextlib.contextmanager
    def staged_tools(self):
        yield "/tmp/fixture"

    def request(self, action, request, tools=None):
        self.calls.append((action, json.loads(json.dumps(request))))
        if action == "inspect":
            return {"host_id": self.registered, "architecture": self.architecture}
        if action == "bootstrap-plan":
            return {"ok": True, "host_id": self.host_id, "findings": [], "plan_token": "reviewed-plan"}
        if action == "bootstrap-apply":
            if self.fail_apply:
                raise cli.LocalError("injected interruption")
            return {"host_id": self.host_id}
        raise AssertionError(f"unexpected action {action}")


class UploadRemote:
    def close(self):
        pass

    """Real upload/staging boundaries with only SSH execution substituted."""
    upload = cli.SSH.upload
    temporary = cli.SSH.temporary

    def __init__(self, profile):
        self.base = ["ssh", "fixture-host"]
        self.profile, self.calls, self.commands = profile, [], []
        self.directory = "/tmp/foundry.ABCDE12345"

    def command(self, command):
        self.commands.append(command)
        if command.startswith("umask 077; mktemp -d "):
            return self.directory + "\n"
        if command == "rm -rf -- " + self.directory:
            return ""
        raise AssertionError(f"unexpected remote shell command {command}")

    def request(self, action, request):
        self.calls.append(action)
        if action != "status":
            raise AssertionError("failed upload must never reach activation")
        return {"state": {"instance_id": self.profile["instance_id"], "current": None, "previous": None}}


class LocalDeploymentTests(unittest.TestCase):


    def test_tool_actions_refuse_old_installed_generation_before_staging(self):
        project, profile = fixture()
        for action in ('tools-plan', 'tools-apply', 'upgrade-resume'):
            remote = MagicMock()
            remote.request.return_value = {'tools': {'generation': 5}, 'client_generation': {'maximum': 6}}
            with patch.dict(os.environ, {'ACTION': action}, clear=True), \
                    patch.object(cli, 'SSH', return_value=remote):
                with self.assertRaisesRegex(cli.LocalError, 'tools_upgrade_floor:.*v0.16.0 or v0.16.1 tools first'):
                    cli.deploy(project, profile, {})
            remote.request.assert_called_once_with('hello', {'project': project, 'profile': profile})
            remote.staged_tools.assert_not_called()

    def test_tool_plan_stages_after_installed_generation_six_is_observed(self):
        project, profile = fixture()
        remote = MagicMock()
        remote.staged_tools.return_value.__enter__.return_value = '/candidate'
        def request(action, payload, tools=None):
            if action == 'hello':
                remote.staged_tools.assert_not_called()
                return {'tools': {'generation': 6}}
            self.assertEqual('tools-plan', action)
            self.assertEqual('/candidate', tools)
            return {'plan_token': 'reviewed'}
        remote.request.side_effect = request
        with patch.dict(os.environ, {'ACTION': 'tools-plan'}, clear=True), \
                patch.object(cli, 'SSH', return_value=remote), patch.object(cli, 'display'):
            cli.deploy(project, profile, {})
        self.assertEqual(['hello', 'tools-plan'], [call.args[0] for call in remote.request.call_args_list])

    def test_restart_and_stop_forward_only_explicit_declared_role(self):
        project, profile = fixture()
        for action in ("restart", "stop"):
            for role in ("http", "worker", "scheduler", None, "missing", ""):
                remote = MagicMock()
                remote.request.return_value = {"state": {}}
                environment = {"ACTION": action}
                if role is not None:
                    environment["ROLE"] = role
                with patch.dict(os.environ, environment, clear=True), \
                        patch.object(cli, "SSH", return_value=remote), \
                        patch.object(cli, "load_topology", return_value={"roles": [{"name": r} for r in ("http", "worker", "scheduler")]}), \
                        patch.object(cli, "confirm") as confirm, patch.object(cli, "display"), \
                        patch.object(cli, "say"), patch.object(cli, "remember_releases"):
                    if role in ("missing", ""):
                        with self.assertRaises(cli.LocalError):
                            cli.deploy(project, profile, {})
                        self.assertFalse(any(call.args[0] == action for call in remote.request.call_args_list))
                        continue
                    cli.deploy(project, profile, {})
                    self.assertEqual(f"{action.capitalize()} {role or 'all roles'} for {cli.instance_name(project, profile)}",
                                     confirm.call_args.args[0])
                sent = next(call.args[1] for call in remote.request.call_args_list if call.args[0] == action)
                self.assertEqual(role, sent.get("role"))
                self.assertEqual(role is not None, "role" in sent)


    def test_maintenance_parameters_and_confirmation(self):
        project, profile = fixture()
        for action, parameters, expected in (
            ("down", {"MESSAGE": "Back soon ✓", "RETRY": "0", "ALLOW": "203.0.113.1, 2001:db8::/32",
                      "EXCEPT": "GET /health, POST /hooks/*", "BYPASS": "yes"},
             {"message": "Back soon ✓", "retry": 0, "allow": ["203.0.113.1", "2001:db8::/32"],
              "except": ["GET /health", "POST /hooks/*"], "bypass": True}),
            ("down", {}, {}), ("up", {}, {}),
        ):
            remote = MagicMock()
            remote.request.return_value = {"state": {}}
            with patch.dict(os.environ, {"ACTION": action, **parameters}, clear=True), \
                    patch.object(cli, "SSH", return_value=remote), patch.object(cli, "confirm") as confirm, \
                    patch.object(cli, "display"), patch.object(cli, "say"), patch.object(cli, "remember_releases"):
                cli.deploy(project, profile, {})
            label = (f"Put {cli.instance_name(project, profile)} into maintenance mode" if action == "down"
                     else f"Bring {cli.instance_name(project, profile)} out of maintenance mode")
            confirm.assert_called_once_with(label)
            self.assertEqual(["status", action], [call.args[0] for call in remote.request.call_args_list])
            self.assertEqual({"project": project, "profile": profile, "confirm": True, **expected},
                             remote.request.call_args.args[1])

    def test_maintenance_rejects_invalid_parameters_before_ssh(self):
        project, profile = fixture()
        invalid = [("MESSAGE", "é" * 257), ("MESSAGE", "bad\nmessage"), ("MESSAGE", "bad\x7f"),
                   ("RETRY", "86401"), ("RETRY", "-1"), ("RETRY", "1.5"), ("RETRY", "true"),
                   ("ALLOW", "invalid"), ("ALLOW", "127.0.0.1,"), ("ALLOW", ",".join(["::1"] * 65)),
                   ("EXCEPT", "get /health"), ("EXCEPT", "/a/../b"), ("EXCEPT", "GET relative"),
                   ("EXCEPT", ",".join(["/health"] * 65)), ("BYPASS", "no")]
        cases = [("down", name, value) for name, value in invalid]
        cases += [("up", name, value) for name in ("MESSAGE", "RETRY", "ALLOW", "EXCEPT", "BYPASS")
                  for value in ("", "yes")]
        for action, name, value in cases:
            with self.subTest(action=action, name=name, value=value), \
                    patch.dict(os.environ, {"ACTION": action, name: value}, clear=True), patch.object(cli, "SSH") as ssh:
                with self.assertRaises(cli.LocalError):
                    cli.deploy(project, profile, {})
                ssh.assert_not_called()
        with patch.dict(os.environ, {"RETRY": "86400", "MESSAGE": "é" * 256}, clear=True):
            self.assertEqual({"retry": 86400, "message": "é" * 256}, cli.maintenance_parameters("down"))

    def test_maintenance_bypass_is_displayed_once_and_never_saved_locally(self):
        project, profile = fixture()
        secret = "/GENERATED_SECRET_ONCE_1234"
        remote = MagicMock()
        remote.request.side_effect = [{"state": {"instance_id": profile["instance_id"], "current": None, "previous": None}},
                                      {"maintenance": {"down": True}, "bypass_path": secret}]
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary).resolve()
            output, diagnostic = io.StringIO(), io.StringIO()
            with patch.dict(os.environ, {"ACTION": "down", "BYPASS": "yes", "CONFIRM": "yes"}, clear=True), \
                    patch.object(cli, "ROOT", root), patch.object(cli, "SSH", return_value=remote), \
                    contextlib.redirect_stdout(output), contextlib.redirect_stderr(diagnostic):
                cli.deploy(project, profile, {})
            self.assertEqual(1, output.getvalue().count(secret))
            self.assertNotIn(secret, diagnostic.getvalue())
            for path in root.rglob("*"):
                if path.is_file():
                    self.assertNotIn(secret.encode(), path.read_bytes())

    def test_logs_rejects_explicit_empty_role(self):
        project, profile = fixture()
        for options in ({}, {"FOLLOW": "yes"}):
            with self.subTest(options=options), \
                    patch.dict(os.environ, {"ACTION": "logs", "ROLE": "", **options}, clear=True), \
                    patch.object(cli, "SSH") as ssh, \
                    patch.object(cli, "load_topology", return_value={"roles": [{"name": "http"}]}):
                with self.assertRaises(cli.LocalError):
                    cli.deploy(project, profile, {})
                ssh.assert_not_called()

    def test_logs_lines_default_and_bounds_are_sent_as_integers(self):
        project, profile = fixture()
        for supplied, expected in ((None, 100), ("1", 1), ("2000", 2000), ("00042", 42)):
            remote = MagicMock()
            remote.request.side_effect = [{"state": {}}, {"logs": "fixture journal\n"}]
            environment = {"ACTION": "logs"}
            if supplied is not None:
                environment["LINES"] = supplied
            with self.subTest(lines=supplied), patch.dict(os.environ, environment, clear=True), \
                    patch.object(cli, "SSH", return_value=remote), \
                    patch.object(cli, "load_topology", return_value={"roles": [{"name": "http"}]}), \
                    patch.object(cli, "remember_releases"), patch.object(cli, "display"), \
                    patch.object(cli.sys.stdin, "isatty", return_value=False):
                cli.deploy(project, profile, {})
            self.assertEqual(["status", "logs"], [call.args[0] for call in remote.request.call_args_list])
            self.assertEqual(("logs", {"project": project, "profile": profile, "role": "http", "lines": expected}),
                             remote.request.call_args.args)

    def test_logs_rejects_invalid_lines_and_follow_before_ssh(self):
        project, profile = fixture()
        invalid = [("LINES", value) for value in
                   ("", "0", "-1", "2001", "1.0", "+1", " 1", "1\n", "true", "١", "9" * 5000)]
        invalid += [("FOLLOW", value) for value in ("", "no", "true", "YES", "1")]
        for name, value in invalid:
            with self.subTest(name=name, value=value[:20]), \
                    patch.dict(os.environ, {"ACTION": "logs", name: value}, clear=True), patch.object(cli, "SSH") as ssh:
                with self.assertRaisesRegex(cli.LocalError, name):
                    cli.deploy(project, profile, {})
                ssh.assert_not_called()

    def test_log_variables_are_rejected_for_every_other_deployment_action(self):
        project, profile = fixture()
        for action in cli.ACTIONS:
            if action == "logs":
                continue
            for name in ("LINES", "FOLLOW"):
                for value in ("", "yes"):
                    with self.subTest(action=action, name=name, value=value), \
                            patch.dict(os.environ, {"ACTION": action, name: value}, clear=True), \
                            patch.object(cli, "SSH") as ssh:
                        with self.assertRaisesRegex(cli.LocalError, "only supported for logs"):
                            cli.deploy(project, profile, {})
                        ssh.assert_not_called()

    def test_log_variables_are_rejected_for_bootstrap_and_release(self):
        for mode in ("bootstrap", "release"):
            for name in ("LINES", "FOLLOW"):
                with self.subTest(mode=mode, name=name), patch.dict(os.environ, {name: ""}, clear=True), \
                        patch.object(cli.sys, "argv", ["cli.py", "--app-root", "/fixture", mode]), \
                        patch.object(cli, "ROOT"), \
                        patch.object(cli, "resolve_app_root", return_value=Path("/fixture")), \
                        patch.object(cli, "lock") as lock:
                    with self.assertRaisesRegex(cli.LocalError, "only supported for logs"):
                        cli.main()
                    lock.assert_not_called()

    def test_follow_logs_uses_saved_interactive_ssh_without_server_requests(self):
        from topology import validate_topology
        project, profile = fixture()
        profile["host"]["port"] = 2222
        topology = validate_topology({"schema": 1, "roles": [{"name": "http"}, {"name": "scheduler"}]})
        settings = {"hosts": {"example.com:2222": {"target": "work-host", "key_path": "~/.ssh/fixture"}}}
        for stdin, stdout in ((True, True), (False, True), (True, False), (False, False)):
            for supplied, expected in ((None, 100), ("2000", 2000)):
                environment = {"ACTION": "logs", "ROLE": "scheduler", "FOLLOW": "yes"}
                if supplied is not None:
                    environment["LINES"] = supplied
                with self.subTest(stdin=stdin, stdout=stdout, lines=supplied), \
                        patch.dict(os.environ, environment, clear=True), \
                        patch.object(cli.sys.stdin, "isatty", return_value=stdin), \
                        patch.object(cli.sys.stdout, "isatty", return_value=stdout), \
                        patch.object(cli, "load_topology", return_value=topology), \
                        patch.object(cli, "execute", return_value="hostname example.com\nport 2222\n") as execute, \
                        patch.object(cli.subprocess, "run", return_value=subprocess.CompletedProcess([], 0)) as run, \
                        patch.object(cli.SSH, "request") as request, patch.object(cli, "protocol_metadata") as protocol, \
                        patch.object(cli, "confirm") as confirm:
                    cli.deploy(project, profile, settings)
                request.assert_not_called()
                protocol.assert_not_called()
                confirm.assert_not_called()
                argv = run.call_args.args[0]
                # Following never allocates a remote terminal, so Ctrl-C ends the local session.
                self.assertNotIn("-t", argv)
                self.assertEqual(argv[-2], "work-host")
                for option in ("StrictHostKeyChecking=yes", "UseKeychain=yes", "IdentitiesOnly=yes", "ControlMaster=auto"):
                    self.assertIn(option, argv)
                self.assertFalse(any(option.startswith("BatchMode=") for option in argv))
                self.assertEqual(argv[argv.index("-p") + 1], "2222")
                self.assertEqual(argv[argv.index("-i") + 1], str(Path("~/.ssh/fixture").expanduser()))
                self.assertEqual({"check": False}, run.call_args.kwargs)
                self.assertEqual(["-O", "exit", "work-host"], execute.call_args.args[0][-3:])
                journal_argv = ["journalctl", "--unit", "foundry-scheduler@abc-prod.service",
                                f"--lines={expected}", "--follow", "--no-pager", "--output=short-iso"]
                # Execute the actual remote conditional with shell functions standing in for host commands.
                for uid in (0, 1000):
                    functions = (f'id() {{ echo {uid}; }}; '
                                 'journalctl() { printf "%s\\n" journalctl "$@"; }; '
                                 'sudo() { printf "%s\\n" sudo "$@"; }; ')
                    result = subprocess.run(["/bin/sh", "-c", functions + argv[-1]],
                                            text=True, capture_output=True, check=True)
                    self.assertEqual(([] if uid == 0 else ["sudo", "-n"]) + journal_argv,
                                     result.stdout.splitlines())

    def test_follow_logs_interrupt_and_remote_exit_are_quiet_but_ssh_failure_is_not(self):
        from topology import validate_topology
        project, profile = fixture()
        topology = validate_topology({"schema": 1, "roles": [{"name": "http"}]})
        settings = {"hosts": {"example.com:22": {"target": "work-host", "key_path": ""}}}
        for outcome in (KeyboardInterrupt(), 0, 1, 130, -signal.SIGINT, 255):
            with self.subTest(outcome=outcome), \
                    patch.dict(os.environ, {"ACTION": "logs", "ROLE": "http", "FOLLOW": "yes"}, clear=True), \
                    patch.object(cli, "load_topology", return_value=topology), \
                    patch.object(cli, "execute", return_value="hostname example.com\nport 22\n") as execute, \
                    patch.object(cli.subprocess, "run") as run, patch.object(cli, "say") as say:
                if isinstance(outcome, BaseException):
                    run.side_effect = outcome
                else:
                    run.return_value = subprocess.CompletedProcess([], outcome)
                if outcome == 255:
                    with self.assertRaisesRegex(cli.LocalError, "SSH connection failed"):
                        cli.deploy(project, profile, settings)
                else:
                    cli.deploy(project, profile, settings)
                say.assert_not_called()
                self.assertEqual(["-O", "exit", "work-host"], execute.call_args.args[0][-3:])

    def test_rotation_reuses_operation_without_recording_private_material_locally(self):
        project, profile = fixture()
        operation = str(uuid.uuid4())
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary).resolve()
            calls = []
            class Remote:
                def close(self):
                    pass

                def request(self, action, request):
                    calls.append((action, request))
                    return {"ok": True}
            with patch.object(cli, "ROOT", root), patch.object(cli, "SSH", return_value=Remote()), \
                    patch.object(cli, "remember_releases"), patch.object(cli, "confirm"), \
                    patch.object(cli, "secret_input", return_value=None), patch.object(cli.sys.stdin, "isatty", return_value=False), \
                    patch.dict(os.environ, {"ACTION": "keys rotate", "OPERATION": operation}, clear=True), \
                    contextlib.redirect_stdout(io.StringIO()):
                cli.deploy(project, profile, {})
                cli.deploy(project, profile, {})
            rotations = [request for action, request in calls if action == "keys-rotate"]
            self.assertEqual([operation, operation], [request["operation_id"] for request in rotations])
            receipt = json.loads((root / ".deploy" / "operations" / (operation + ".json")).read_text())
            self.assertEqual({"action", "instance_id", "host_id"}, set(receipt))
    def setUp(self):
        # Resolve macOS's /var and /tmp aliases: deployment metadata refuses
        # symlink ancestors, including in test fixtures.
        self.temporary = tempfile.TemporaryDirectory(dir=Path(tempfile.gettempdir()).resolve())
        self.root = Path(self.temporary.name)
        self.addCleanup(self.temporary.cleanup)

    def test_help_uses_command_metadata_without_settings_locks_or_external_commands(self):
        for option in ("help",):
            output = io.StringIO()
            with self.subTest(option=option), patch.object(cli, "ROOT", self.root), \
                    patch.object(cli.sys, "argv", ["cli.py", option]), \
                    patch.object(cli, "lock") as lock, patch.object(cli, "local_settings") as settings, \
                    patch.object(cli, "execute") as execute, patch.object(cli, "SSH") as ssh, \
                    contextlib.redirect_stdout(output):
                cli.main()
            for command, description in cli.COMMANDS.items():
                self.assertIn(f"make {command:<10} {description}", output.getvalue())
            lock.assert_not_called()
            settings.assert_not_called()
            execute.assert_not_called()
            ssh.assert_not_called()
            self.assertEqual(list(self.root.iterdir()), [])

    def test_repeated_interrupt_at_a_prompt_cleans_up_once(self):
        (self.root / "go.mod").write_text("module example.com/app\ngo 1.26\n")
        controller, terminal = pty.openpty()
        self.addCleanup(os.close, controller)
        self.addCleanup(os.close, terminal)
        process = subprocess.Popen([sys.executable, "-B", str(DEPLOY / "local" / "cli.py"), "--app-root", str(self.root), "bootstrap"],
                                   stdin=terminal, stderr=subprocess.PIPE, env={"PATH": os.environ["PATH"]})
        self.addCleanup(process.stderr.close)
        self.addCleanup(process.kill)
        output, deadline = b"", time.monotonic() + 30
        while b"Project slug" not in output and time.monotonic() < deadline:
            if select.select([process.stderr], [], [], 1)[0]:
                output += os.read(process.stderr.fileno(), 4096)
        self.assertIn(b"Project slug", output)
        process.send_signal(signal.SIGINT)
        time.sleep(0.005)
        process.send_signal(signal.SIGINT)
        _, remainder = process.communicate(timeout=30)
        self.assertEqual(process.returncode, 130)
        self.assertIn(b"Interrupted.", remainder)
        self.assertNotIn(b"Traceback", remainder)

    def test_interrupt_handler_ignores_later_interrupts(self):
        with patch.object(cli.signal, "signal") as set_handler, self.assertRaises(KeyboardInterrupt):
            cli.interrupted(None, None)
        set_handler.assert_called_once_with(cli.signal.SIGINT, cli.signal.SIG_IGN)

    def test_app_root_must_be_explicit_and_contain_go_module(self):
        (self.root / "go.mod").write_text("module example.com/app\ngo 1.26\n")
        with patch.dict(os.environ, {}, clear=True):
            with self.assertRaisesRegex(cli.LocalError, "--app-root"):
                cli.resolve_app_root(None)
            self.assertEqual(cli.resolve_app_root(str(self.root)), self.root)

    def test_standard_help_never_resolves_an_app_root(self):
        for option in ("--help", "-h"):
            with self.subTest(option=option), patch.object(cli.sys, "argv", ["cli.py", option]), \
                    patch.object(cli, "resolve_app_root") as resolve, \
                    contextlib.redirect_stdout(io.StringIO()), self.assertRaises(SystemExit) as result:
                cli.main()
            self.assertEqual(result.exception.code, 0)
            resolve.assert_not_called()

    def test_copied_profile_identity_is_refused_before_connecting(self):
        project, profile = fixture()
        directory = self.root / "deploy" / "environments"
        directory.mkdir(parents=True)
        (directory / "prod.json").write_text(json.dumps(profile))
        copied = json.loads(json.dumps(profile))
        copied["environment"] = "staging"
        copied["domain"] = "staging.example.com"
        copied["host"]["address"] = "another.example.com"
        (directory / "staging.json").write_text(json.dumps(copied))
        with patch.object(cli, "ROOT", self.root):
            with self.assertRaisesRegex(cli.LocalError, "copied instance UUID"):
                cli.profile_config("prod", project)

    def test_new_product_identity_cannot_adopt_copied_profiles(self):
        project, profile = fixture()
        directory = self.root / "deploy" / "environments"
        directory.mkdir(parents=True)
        (directory / "prod.json").write_text(json.dumps(profile))
        with patch.object(cli, "ROOT", self.root), patch.object(cli, "ask") as ask:
            with self.assertRaisesRegex(cli.LocalError, "existing environment profiles"):
                cli.project_config("bootstrap")
            ask.assert_not_called()

    def test_ssh_alias_must_resolve_to_committed_host_with_strict_keys(self):
        _, profile = fixture()
        settings = {"hosts": {"example.com:22": {"target": "work-host", "key_path": ""}}}
        with patch.object(cli, "execute", return_value="hostname example.com\nport 22\n") as run:
            ssh = cli.SSH(profile, settings)
        self.assertIn("StrictHostKeyChecking=yes", run.call_args.args[0])
        self.assertIn("BatchMode=yes", ssh.base)
        self.assertEqual(ssh.base[-1], "work-host")
        with patch.object(cli, "execute", return_value="hostname another.example.com\nport 22\n"):
            with self.assertRaisesRegex(cli.LocalError, "different hostname"):
                cli.SSH(profile, settings)

    def test_shell_reuses_personal_options_without_protocol_or_confirmation(self):
        project, profile = fixture()
        profile["host"]["port"] = 2222
        settings = {"hosts": {"example.com:2222": {"target": "work-host", "key_path": "~/.ssh/fixture"}}}
        for returncode in (0, 1, 127, 255):
            with self.subTest(returncode=returncode), patch.dict(os.environ, {"ACTION": "shell"}, clear=True), \
                    patch.object(cli.sys.stdin, "isatty", return_value=True), \
                    patch.object(cli.sys.stdout, "isatty", return_value=True), \
                    patch.object(cli, "execute", return_value="hostname example.com\nport 2222\n") as execute, \
                    patch.object(cli.subprocess, "run", return_value=subprocess.CompletedProcess([], returncode)) as run, \
                    patch.object(cli.SSH, "request") as request, patch.object(cli, "protocol_metadata") as protocol, \
                    patch.object(cli, "confirm") as confirm:
                if returncode == 255:
                    with self.assertRaisesRegex(cli.LocalError, "SSH connection failed"):
                        cli.deploy(project, profile, settings)
                else:
                    cli.deploy(project, profile, settings)
            request.assert_not_called()
            protocol.assert_not_called()
            confirm.assert_not_called()
            argv = run.call_args.args[0]
            self.assertEqual(argv[-3:], ["-t", "work-host", 'cd /opt/foundry/abc-prod/current && exec "${SHELL:-/bin/bash}" -l'])
            for option in ("StrictHostKeyChecking=yes", "UseKeychain=yes", "IdentitiesOnly=yes", "ControlMaster=auto"):
                self.assertIn(option, argv)
            self.assertFalse(any(option.startswith("BatchMode=") for option in argv))
            self.assertEqual(argv[argv.index("-p") + 1], "2222")
            self.assertEqual(argv[argv.index("-i") + 1], str(Path("~/.ssh/fixture").expanduser()))
            self.assertEqual(run.call_args.kwargs, {"check": False})
            closed = execute.call_args.args[0]
            self.assertEqual(closed[-3:], ["-O", "exit", "work-host"])
            self.assertEqual(next(x for x in argv if x.startswith("ControlPath=")),
                             next(x for x in closed if x.startswith("ControlPath=")))

    def test_shell_requires_both_terminal_streams_before_connecting(self):
        project, profile = fixture()
        for stdin, stdout in ((False, True), (True, False), (False, False)):
            with self.subTest(stdin=stdin, stdout=stdout), patch.dict(os.environ, {"ACTION": "shell"}, clear=True), \
                    patch.object(cli.sys.stdin, "isatty", return_value=stdin), \
                    patch.object(cli.sys.stdout, "isatty", return_value=stdout), patch.object(cli, "execute") as execute:
                with self.assertRaisesRegex(cli.LocalError, "ACTION=shell requires a terminal on stdin and stdout"):
                    cli.deploy(project, profile, {})
                execute.assert_not_called()

    def test_ssh_saved_key_needs_no_operator_ssh_config(self):
        _, profile = fixture()
        settings = {"hosts": {"example.com:22": {"target": "root@example.com", "key_path": "~/.ssh/fixture"}}}
        with patch.object(cli, "execute", return_value="hostname example.com\nport 22\n"):
            ssh = cli.SSH(profile, settings)
        self.assertLess(ssh.base.index("IgnoreUnknown=UseKeychain"), ssh.base.index("UseKeychain=yes"))
        self.assertIn("IdentitiesOnly=yes", ssh.base)
        self.assertEqual(ssh.base[ssh.base.index("-i") + 1], str(Path("~/.ssh/fixture").expanduser()))
        settings["hosts"]["example.com:22"]["key_path"] = ""
        with patch.object(cli, "execute", return_value="hostname example.com\nport 22\n"):
            ssh = cli.SSH(profile, settings)
        for option in ("IdentitiesOnly=yes", "UseKeychain=yes", "-i"):
            self.assertNotIn(option, ssh.base)

    def test_ssh_first_contact_accepts_only_an_unknown_host_key(self):
        _, profile = fixture()
        settings = {"hosts": {"example.com:22": {"target": "root@example.com", "key_path": ""}}}
        with patch.object(cli, "execute", return_value="hostname example.com\nport 22\n"):
            ssh = cli.SSH(profile, settings, first_contact=True)
        self.assertIn("StrictHostKeyChecking=accept-new", ssh.base)
        self.assertNotIn("StrictHostKeyChecking=yes", ssh.base)

    def test_refused_ssh_login_is_named_without_echoing_stderr(self):
        with self.assertRaisesRegex(cli.LocalError, "login was refused") as caught:
            cli.check_ssh_exit(255, b"secret-canary\nroot@example.com: Permission denied (publickey).\n")
        self.assertNotIn("secret-canary", str(caught.exception))
        for stderr in (None, b"", b"bash: /tmp/x: Permission denied\n"):
            with self.assertRaisesRegex(cli.LocalError, "connection failed or dropped"):
                cli.check_ssh_exit(255, stderr)
        cli.check_ssh_exit(0, b"root@example.com: Permission denied (publickey).\n")

    def test_refused_host_key_is_named_without_echoing_stderr(self):
        with self.assertRaisesRegex(cli.LocalError, "host key is unknown or differs") as caught:
            cli.check_ssh_exit(255, b"secret-canary\nHost key verification failed.\n")
        self.assertNotIn("secret-canary", str(caught.exception))
        with self.assertRaisesRegex(cli.LocalError, "connection failed or dropped"):
            cli.check_ssh_exit(255, b"remote said Host key verification failed.\n")

    def test_ssh_rejects_options_and_shell_metacharacters(self):
        _, profile = fixture()
        for target in ("-oProxyCommand=id", "host;echo-canary", "user@host$(id)", "host\nother"):
            settings = {"hosts": {"example.com:22": {"target": target, "key_path": ""}}}
            with self.subTest(target=target), self.assertRaises(cli.LocalError):
                cli.SSH(profile, settings)

    def test_ssh_controls_are_private_short_and_session_owned(self):
        _, profile = fixture()
        settings = {"hosts": {"example.com:22": {"target": "work-host", "key_path": ""}}}
        with patch.object(cli, "execute", return_value="hostname example.com\nport 22\n"):
            first, second = cli.SSH(profile, settings), cli.SSH(profile, settings)
        for value in ("ControlMaster=auto", "ControlPersist=120", "ConnectionAttempts=3",
                      "ConnectTimeout=30", "ServerAliveCountMax=8"):
            self.assertIn(value, first.base)
        controls = [next(option.split("=", 1)[1] for option in ssh.base if option.startswith("ControlPath="))
                    for ssh in (first, second)]
        self.assertNotEqual(*controls)
        self.assertLess(len(controls[0].encode()) + 18, 104)
        directory = Path(controls[0]).parent
        self.assertEqual(cli.os.getuid(), directory.stat().st_uid)
        self.assertEqual(0o700, directory.stat().st_mode & 0o777)
        with patch.object(cli, "execute", side_effect=cli.LocalError("already gone")) as close:
            first.close()
        self.assertEqual(["-O", "exit", "work-host"], close.call_args.args[0][-3:])

    def test_ssh_refuses_unsafe_control_directory(self):
        info = self.root.stat()
        for mode, uid in ((0o40755, info.st_uid), (0o40700, info.st_uid + 1), (0o120700, info.st_uid)):
            fake = type("Info", (), {"st_mode": mode, "st_uid": uid})()
            with patch.object(cli.Path, "mkdir"), patch.object(cli.Path, "lstat", return_value=fake), \
                    self.assertRaisesRegex(cli.LocalError, "0700"):
                cli.control_directory()

    def test_dropped_ssh_precedes_json_and_never_retries_mutations(self):
        ssh = object.__new__(cli.SSH)
        ssh.base = ["ssh", "fixture"]
        archive = self.root / "release.tar"
        archive.write_bytes(b"fixture")
        result = type("Result", (), {"stdout": b'{"ok":true}', "stderr": b"secret-canary", "returncode": 255})()
        operations = ((lambda: ssh.request("config-set", {}), True),
                      (lambda: ssh.request("bootstrap-apply", {}, "/tmp/staged"), True),
                      (lambda: ssh.report(), False), (lambda: ssh.upload(archive, "/tmp/release"), True),
                      (lambda: ssh.command("tar -xf - -C /tmp/staged", input=b"private"), True))
        for operation, mutates in operations:
            with patch.object(cli.subprocess, "run", return_value=result) as run, self.assertRaises(cli.LocalError) as caught:
                operation()
            self.assertEqual(1, run.call_count)
            self.assertIn("SSH connection failed or dropped", str(caught.exception))
            self.assertIn("ACTION=status", str(caught.exception))
            self.assertEqual(mutates, "unknown" in str(caught.exception))
            self.assertNotIn("secret-canary", str(caught.exception))
        result.returncode, result.stdout = 1, b"no JSON"
        with patch.object(cli.subprocess, "run", return_value=result), self.assertRaisesRegex(cli.LocalError, "server returned no valid JSON"):
            ssh.request("config-set", {})

    def test_credential_actions_use_private_input_confirmation_and_close(self):
        project, profile = fixture()
        from unittest.mock import Mock
        for action, name in (("backup-keys-rotate", "backup_s3"),
                             ("backup-recipients-rotate", "age_recipients"), ("dns-token-rotate", "cloudflare_dns_token")):
            remote = Mock()
            remote.request.return_value = {"ok": True}
            output = io.StringIO()
            with patch.object(cli, "SSH", return_value=remote), patch.object(cli, "secret_input", return_value="private-canary") as secret, \
                    patch.object(cli, "confirm") as confirm, patch.dict(os.environ, {"ACTION": action}, clear=True), \
                    contextlib.redirect_stdout(output), contextlib.redirect_stderr(output):
                cli.deploy(project, profile, {})
            self.assertEqual(name, secret.call_args.args[0])
            confirm.assert_called_once()
            self.assertEqual((action, {"project": project, "profile": profile, "confirm": True,
                                      "secrets": {name: "private-canary"}}), remote.request.call_args.args)
            remote.close.assert_called_once()
            self.assertNotIn("private-canary", output.getvalue())
            remote.reset_mock()
            remote.request.side_effect = cli.LocalError("dropped")
            with patch.object(cli, "SSH", return_value=remote), patch.dict(os.environ, {"ACTION": "status"}), \
                    self.assertRaises(cli.LocalError):
                cli.deploy(project, profile, {})
            remote.close.assert_called_once()

    def test_config_secret_travels_only_on_stdin(self):
        _, profile = fixture()
        settings = {"hosts": {"example.com:22": {"target": "example.com", "key_path": ""}}}
        with patch.object(cli, "execute", return_value="hostname example.com\nport 22\n"):
            ssh = cli.SSH(profile, settings)
        result = type("Result", (), {"stderr": b"", "stdout": b'{"ok":true}', "returncode": 0})()
        with patch.object(cli.subprocess, "run", return_value=result) as run:
            ssh.request("config-set", {"secrets": {"configuration": {"APP__KEY": "runtime-secret-canary"}}})
        self.assertNotIn("runtime-secret-canary", " ".join(run.call_args.args[0]))
        self.assertIn(b"runtime-secret-canary", run.call_args.kwargs["input"])
        self.assertIn("sudo -n", run.call_args.args[0][-1])
        self.assertIn("/usr/local/sbin/foundry-host config-set", run.call_args.args[0][-1])
        envelope = json.loads(run.call_args.kwargs["input"])
        self.assertIn("protocol", envelope)
        self.assertEqual(envelope["client"]["generation"], cli.protocol_metadata()["client"]["generation"])

    def test_ssh_prints_only_the_known_deferred_renewal_warning(self):
        _, profile = fixture()
        settings = {"hosts": {"example.com:22": {"target": "example.com", "key_path": ""}}}
        with patch.object(cli, "execute", return_value="hostname example.com\nport 22\n"):
            ssh = cli.SSH(profile, settings)
        result = type("Result", (), {"stderr": b"private diagnostic\n" + cli.RENEWAL_WARNING.encode() + b"\n",
                                      "stdout": b'{"ok":true}', "returncode": 0})()
        with patch.object(cli.subprocess, "run", return_value=result), patch.object(cli, "say") as say:
            ssh.request("bootstrap-apply", {})
        say.assert_called_once_with(cli.RENEWAL_WARNING)

    def test_config_action_sends_related_keys_as_one_change(self):
        project, profile = fixture()
        requests = []

        class FakeSSH:
            def close(self):
                pass

            def __init__(self, *_):
                pass

            def request(self, action, payload):
                requests.append((action, payload))
                return {"ok": True}

        def run(answers):
            replies = iter(answers)
            with patch.object(cli, "SSH", FakeSSH), patch.object(cli, "remember_releases"), \
                    patch.object(cli, "ask", side_effect=lambda *args, **kwargs: next(replies)), \
                    patch.dict(os.environ, {"ACTION": "config", "CONFIRM": "yes"}), contextlib.redirect_stdout(io.StringIO()):
                cli.deploy(project, profile, {})

        run(["APP__ENCRYPTION__KEY_ID", "fixture_2026", "APP__ENCRYPTION__KEY", "key-material-canary", ""])
        action, payload = requests[-1]
        self.assertEqual("config-set", action)
        self.assertEqual({"APP__ENCRYPTION__KEY_ID": "fixture_2026", "APP__ENCRYPTION__KEY": "key-material-canary"}, payload["secrets"]["configuration"])
        requests.clear()
        with self.assertRaises(cli.LocalError):
            run(["PROJECT__NAME", "one", "PROJECT__NAME"])
        self.assertNotIn("config-set", [action for action, _ in requests])

    def test_sync_displays_registered_difference_without_writes(self):
        project, profile = fixture()
        topology = {"schema": 1, "roles": [{"name": "http"}], "sha256": "a" * 64}
        remote_profile = {**profile, "domain": "registered.example.com"}
        requests = []

        class FakeSSH:
            def close(self):
                pass

            def __init__(self, *_):
                pass

            def request(self, action, payload):
                requests.append(action)
                return {"profile": remote_profile, "topology": topology}

        output = io.StringIO()
        with patch.object(cli, "ROOT", self.root), patch.object(cli, "SSH", FakeSSH), \
                patch.object(cli, "load_topology", return_value=topology), \
                patch.object(cli, "atomic_json") as write, \
                patch.dict(os.environ, {"ACTION": "sync"}), contextlib.redirect_stdout(output):
            cli.deploy(project, profile, {})
        self.assertEqual(requests, ["sync"])
        self.assertIn("registered.example.com", output.getvalue())
        self.assertIn("abc.example.com", output.getvalue())
        write.assert_not_called()

    def test_failed_or_interrupted_upload_cleans_only_its_staging_without_activation(self):
        project, profile = fixture()
        archive = self.root / "app.tar.gz"
        archive.write_bytes(b"completed-release-fixture")
        (self.root / "manifest.json").write_text(json.dumps(self.release_manifest(
            project, profile, "20261013T120000Z-aaaaaaaaaaaa")))
        failed = type("Result", (), {"stdout": b"", "stderr": b"private-server-canary", "returncode": 255})()
        scenarios = (("exit", failed, cli.LocalError),
                     ("timeout", cli.subprocess.TimeoutExpired(["ssh"], 3600), cli.LocalError),
                     ("interrupt", KeyboardInterrupt(), KeyboardInterrupt))
        for label, outcome, expected_error in scenarios:
            remote = UploadRemote(profile)
            output = io.StringIO()
            run_options = {"side_effect": outcome} if isinstance(outcome, BaseException) else {"return_value": outcome}
            with self.subTest(outcome=label), patch.object(cli, "ROOT", self.root), \
                    patch.object(cli, "SSH", return_value=remote), patch.object(cli, "select_archive", return_value=archive), \
                    patch.object(cli, "confirm"), patch.dict(os.environ, {"ACTION": "deploy"}), \
                    patch.object(cli.subprocess, "run", **run_options) as run, \
                    contextlib.redirect_stdout(output), contextlib.redirect_stderr(output):
                with self.assertRaises(expected_error) as error:
                    cli.deploy(project, profile, {})
            self.assertEqual(remote.calls, ["status"])
            self.assertEqual(remote.commands[-1], "rm -rf -- " + remote.directory)
            self.assertEqual(len(remote.commands), 2)
            self.assertEqual(run.call_args.args[0][-1], "umask 077; cat > " + remote.directory + "/app.tar.gz")
            self.assertEqual(run.call_args.kwargs["timeout"], 3600)
            self.assertTrue(archive.is_file())
            self.assertNotIn("private-server-canary", output.getvalue() + str(error.exception))

    def test_public_build_input_rejects_interpolation_and_private_keys(self):
        for public in ({"PRIVATE": "canary"}, {"VITE_LOCALE": "$PASSWORD"}, {"VITE_LOCALE": "first\nsecret"}):
            with self.subTest(public=public), self.assertRaises(ValueError):
                metadata.public_env({"profile": {"public_env": public}}, self.root / "env")
        metadata.public_env({"profile": {"public_env": {"VITE_LOCALE": "A public name"}}}, self.root / "env")
        self.assertEqual((self.root / "env").read_text(), "VITE_LOCALE='A public name'\n")

    def test_public_build_values_preserve_literal_backslashes(self):
        value = r"Public\nname\rpath"
        metadata.public_env({"profile": {"public_env": {"VITE_LOCALE": value}}}, self.root / "env")
        self.assertEqual((self.root / "env").read_text(), f"VITE_LOCALE='{value}'\n")
        value = "John's " + value
        metadata.public_env({"profile": {"public_env": {"VITE_LOCALE": value}}}, self.root / "env")
        self.assertEqual((self.root / "env").read_text(), f"VITE_LOCALE=`{value}`\n")
        with self.assertRaisesRegex(ValueError, "represented literally"):
            metadata.public_env({"profile": {"public_env": {"VITE_LOCALE": value + "`"}}}, self.root / "env")

    def stage(self):
        stage = self.root / "stage"
        stage.mkdir()
        (stage / "artisan").write_bytes(b"compiled-artisan-fixture")
        (stage / "config").mkdir()
        (stage / "config" / "deploy.toml").write_text("[app]\n")
        (stage / "public").mkdir()
        (stage / "public" / "index.html").write_text("public site")
        return stage

    def test_archive_contains_checksums_and_no_sources_or_runtime_configuration(self):
        stage = self.stage()
        with patch.object(metadata, "collect_build_inputs", return_value={}):
            archive = metadata.package({"manifest": {"schema": 1, "version": "fixture", "instance": "abc-prod"}, "source_date_epoch": 0},
                                       stage, self.root / "release")
        with metadata.tarfile.open(archive) as tar:
            names = set(tar.getnames())
            manifest = json.load(tar.extractfile("manifest.json"))
            self.assertEqual(set(manifest["files"]), {"artisan", "config/deploy.toml", "public/index.html"})
            for name, checksum in manifest["files"].items():
                self.assertEqual(hashlib.sha256(tar.extractfile(name).read()).hexdigest(), checksum)
            self.assertFalse(names & {".env", "app.env", "deploy", "internal", "go.mod"})
            self.assertEqual(tar.getmember("artisan").mode, 0o755)
            self.assertNotIn("config/deploy-runtime.json", names)
        self.assertFalse((stage / "config/deploy-runtime.json").exists())

    def test_local_archive_roundtrips_through_server_validator(self):
        project, profile = fixture()
        entry = cli.identity(project, profile)
        entry["profile"] = profile
        version = "20261013T120000Z-aaaaaaaaaaaa"
        stage = self.stage()
        maps = stage / "private-source-maps" / version / "admin"
        maps.mkdir(parents=True)
        (maps / "manifest.json").write_text('{}')
        manifest = {"schema": 1, "layout_version": 2, "project_id": project["id"], "instance_id": profile["instance_id"],
                    "instance": "abc-prod", "environment": "prod", "architecture": "amd64", "version": version,
                    "commit": "a" * 40, "tree_state": "clean", "built_at": "2026-10-13T12:00:00Z", "native_imaging": False,
                    "source_kind": "git_archive", "source_sha256": "e" * 64}
        with patch.object(metadata, "collect_build_inputs", return_value={}):
            archive = metadata.package({"manifest": manifest, "source_date_epoch": 0}, stage, self.root / "release")
        extracted = self.root / "extracted"
        result = release.unpack_archive(Host(Paths(self.root)), archive, extracted, entry, cli.digest(archive))
        self.assertEqual(result["version"], version)
        self.assertEqual((extracted / "artisan").read_bytes(), b"compiled-artisan-fixture")
        self.assertTrue((extracted / "private-source-maps" / version / "admin" / "manifest.json").is_file())

    def test_package_rejects_runtime_descriptor(self):
        stage = self.stage()
        (stage / "config/deploy-runtime.json").write_text('{"schema":1}')
        with self.assertRaisesRegex(ValueError, "unexpected release path"):
            metadata.package({"manifest": {"instance": "abc-prod"}}, stage, self.root / "release")
        self.assertFalse((self.root / "release" / "app.tar.gz").exists())

    def test_archive_rejects_symlinks_and_environment_files(self):
        stage = self.stage()
        secret = self.root / "secret"
        secret.write_text("secret-canary")
        bad = stage / "public" / "link"
        bad.symlink_to(secret)
        with self.assertRaisesRegex(ValueError, "non-regular"):
            metadata.package({"manifest": {"instance": "abc-prod"}}, stage, self.root / "release")
        bad.unlink()
        (stage / "public" / ".env.production").write_text("secret-canary")
        with self.assertRaisesRegex(ValueError, "forbidden"):
            metadata.package({"manifest": {"instance": "abc-prod"}}, stage, self.root / "release")
        self.assertFalse((self.root / "release" / "app.tar.gz").exists())

    def test_retention_only_removes_complete_scoped_releases(self):
        project, profile = fixture()
        entry = {**cli.identity(project, profile), "profile": profile}
        current = "20261013T120000Z-aaaaaaaaaaaa"
        old = "20261012T120000Z-bbbbbbbbbbbb"
        incomplete = "20261011T120000Z-cccccccccccc"
        for name in (current, old, incomplete, "operator-data"):
            directory = self.root / name
            directory.mkdir()
            if name in (old, current):
                (directory / "app.tar.gz").touch()
                (directory / "manifest.json").write_text(json.dumps(self.release_manifest(project, profile, name)))
        link = self.root / "20261010T120000Z-dddddddddddd"
        link.symlink_to(self.root / "operator-data")
        cli.prune_releases(self.root, 1, current, entry)
        self.assertTrue((self.root / current).exists())
        self.assertFalse((self.root / old).exists())
        self.assertTrue((self.root / incomplete).exists())
        self.assertTrue((self.root / "operator-data").exists())
        self.assertTrue(link.is_symlink())

    def test_retention_preserves_known_active_and_previous_versions(self):
        project, profile = fixture()
        entry = {**cli.identity(project, profile), "profile": profile}
        versions = [f"202610{day:02d}T120000Z-aaaaaaaaaaaa" for day in range(10, 14)]
        for name in versions:
            directory = self.root / name
            directory.mkdir()
            (directory / "app.tar.gz").touch()
            (directory / "manifest.json").write_text(json.dumps(self.release_manifest(project, profile, name)))
        cli.prune_releases(self.root, 1, versions[-1], entry, versions[:2])
        self.assertTrue((self.root / versions[0]).exists())
        self.assertTrue((self.root / versions[1]).exists())
        self.assertFalse((self.root / versions[2]).exists())
        self.assertTrue((self.root / versions[3]).exists())

    def release_manifest(self, project, profile, version):
        return {"schema": 1, "layout_version": 2, "project_id": project["id"], "instance_id": profile["instance_id"],
                "instance": cli.instance_name(project, profile), "environment": profile["environment"],
                "architecture": profile["architecture"], "native_imaging": False, "version": version,
                "commit": "a" * 40, "tree_state": "clean", "built_at": "2026-10-13T12:00:00Z",
                "source_kind": "git_archive", "source_sha256": "e" * 64, "files": {
                    "artisan": "a" * 64, "config/deploy.toml": "b" * 64, "public/index.html": "c" * 64,
                    f"private-source-maps/{version}/admin/manifest.json": "d" * 64}}

    def test_retention_preserves_foreign_malformed_misnamed_and_symlink_releases(self):
        project, profile = fixture()
        entry = {**cli.identity(project, profile), "profile": profile}
        versions = [f"202610{day:02d}T120000Z-aaaaaaaaaaaa" for day in range(7, 14)]
        for version in versions:
            directory = self.root / version
            directory.mkdir()
            (directory / "app.tar.gz").touch()
            (directory / "manifest.json").write_text(json.dumps(self.release_manifest(project, profile, version)))
        foreign = self.release_manifest(*fixture(), versions[0])
        (self.root / versions[0] / "manifest.json").write_text(json.dumps(foreign))
        (self.root / versions[1] / "manifest.json").write_text('{"invalid": true}')
        (self.root / versions[2] / "manifest.json").write_text(json.dumps(self.release_manifest(project, profile, versions[3])))
        manifest = self.root / versions[3] / "manifest.json"
        manifest.unlink()
        manifest.symlink_to(self.root / versions[4] / "manifest.json")
        archive = self.root / versions[4] / "app.tar.gz"
        archive.unlink()
        archive.symlink_to(self.root / versions[5] / "app.tar.gz")
        cli.prune_releases(self.root, 1, versions[-1], entry)
        for version in versions[:5] + versions[-1:]:
            self.assertTrue((self.root / version).is_dir(), version)
        self.assertFalse((self.root / versions[5]).exists())

    def test_status_cache_keeps_only_public_release_versions(self):
        _, profile = fixture()
        version = "20261013T120000Z-aaaaaaaaaaaa"
        response = {"state": {"instance_id": profile["instance_id"], "current": {"version": version},
                              "previous": None, "credentials": "secret-canary"}}
        with patch.object(cli, "ROOT", self.root):
            cli.remember_releases(profile, response)
            self.assertEqual(cli.known_releases(profile), (version,))
            self.assertNotIn("secret-canary", cli.state_cache(profile).read_text())
            profile["host"]["host_id"] = str(uuid.uuid4())
            with self.assertRaisesRegex(cli.LocalError, "another instance"):
                cli.known_releases(profile)

    def test_lock_refuses_concurrent_operation_and_symlink(self):
        path = self.root / "operation.lock"
        with cli.lock(path):
            with self.assertRaisesRegex(cli.LocalError, "another local"):
                with cli.lock(path):
                    self.fail("concurrent lock acquired")
        alias = self.root / "alias.lock"
        alias.symlink_to(path)
        with self.assertRaisesRegex(cli.LocalError, "symlink"):
            with cli.lock(alias):
                self.fail("symlink lock acquired")
        parent = self.root / "alias-directory"
        parent.symlink_to(self.root, target_is_directory=True)
        with self.assertRaisesRegex(cli.DeployError, "symlink"):
            with cli.lock(parent / "other.lock"):
                self.fail("symlink ancestor lock acquired")
        self.assertFalse((self.root / "other.lock").exists())

    def bootstrap_context(self, remote):
        stack = contextlib.ExitStack()
        stack.enter_context(patch.object(cli, "ROOT", self.root))
        stack.enter_context(patch.object(cli, "SSH", return_value=remote))
        stack.enter_context(patch.object(cli, "load_topology", return_value={
            "schema": 1, "roles": [{"name": "http"}], "sha256": "a" * 64}))
        stack.enter_context(patch.object(cli, "confirm"))
        stack.enter_context(patch.object(cli, "bootstrap_secrets", return_value={"cloudflare_dns_token": "private-token-canary"}))
        stack.enter_context(patch.dict(os.environ, {"ACTION": "prepare"}))
        stack.enter_context(contextlib.redirect_stdout(io.StringIO()))
        return stack

    def test_bootstrap_allows_first_contact_only_before_binding(self):
        project, profile = fixture()
        host_id = str(uuid.uuid4())
        profile["host"]["host_id"] = None
        with self.bootstrap_context(BootstrapRemote(host_id)):
            cli.bootstrap(project, profile, {}, True)
            self.assertEqual(cli.SSH.call_args.kwargs, {"first_contact": True})
        self.assertEqual(profile["host"]["host_id"], host_id)
        with self.bootstrap_context(BootstrapRemote(host_id, registered=host_id)):
            cli.bootstrap(project, profile, {}, False)
            self.assertEqual(cli.SSH.call_args.kwargs, {"first_contact": False})

    def test_deploy_never_allows_first_contact(self):
        project, profile = fixture()
        remote = MagicMock()
        remote.report.return_value = {}
        remote.request.return_value = {"ok": True}
        with patch.object(cli, "SSH", return_value=remote) as ssh, patch.object(cli, "remember_releases"), \
                patch.dict(os.environ, {"ACTION": "status"}), contextlib.redirect_stdout(io.StringIO()):
            cli.deploy(project, profile, {})
        self.assertNotIn("first_contact", ssh.call_args.kwargs)

    def test_bootstrap_binds_reviewed_host_and_passes_plan_token(self):
        project, profile = fixture()
        profile["host"]["host_id"] = None
        host_id = str(uuid.uuid4())
        remote = BootstrapRemote(host_id, architecture="arm64")
        with self.bootstrap_context(remote):
            cli.bootstrap(project, profile, {}, True)
            marker, pending = cli.pending_bootstrap(project, profile)
        self.assertEqual(profile["host"]["host_id"], host_id)
        self.assertEqual(profile["architecture"], "arm64")
        self.assertEqual(remote.calls[-1][1]["plan_token"], "reviewed-plan")
        self.assertEqual(remote.calls[-1][1]["topology"]["roles"], [{"name": "http"}])
        self.assertEqual(remote.calls[-1][1]["profile"]["host"]["host_id"], host_id)
        self.assertEqual(remote.calls[-1][1]["secrets"], {"cloudflare_dns_token": "private-token-canary"})
        self.assertNotIn("private-token-canary", json.dumps(remote.calls[:-1]))
        self.assertFalse(marker.exists())
        self.assertIsNone(pending)
        self.assertEqual(json.loads((self.root / "deploy/environments/prod.json").read_text()), profile)

    def test_bootstrap_inspection_includes_selected_profile_without_binding_host(self):
        project, profile = fixture()
        profile["host"]["host_id"] = None
        host_id = str(uuid.uuid4())
        remote = BootstrapRemote(host_id, registered=host_id)
        output = io.StringIO()
        with self.bootstrap_context(remote), patch.dict(os.environ, {"ACTION": "inspect"}), contextlib.redirect_stdout(output):
            cli.bootstrap(project, profile, {}, True)
        value = json.loads(output.getvalue())
        self.assertEqual(value["project"], project)
        self.assertEqual(value["profile"], profile)
        self.assertEqual(value["host"]["host_id"], host_id)
        self.assertIsNone(profile["host"]["host_id"])
        self.assertEqual([action for action, _ in remote.calls], ["inspect"])

    def test_bootstrap_can_retry_saved_binding_before_first_registry_write(self):
        project, profile = fixture()
        host_id = profile["host"]["host_id"]
        profile["host"]["host_id"] = None
        remote = BootstrapRemote(host_id, fail_apply=True)
        with self.bootstrap_context(remote):
            with self.assertRaisesRegex(cli.LocalError, "injected interruption"):
                cli.bootstrap(project, profile, {}, True)
            marker, pending = cli.pending_bootstrap(project, profile)
            self.assertEqual(pending, host_id)
            self.assertEqual(marker.stat().st_mode & 0o777, 0o600)
            saved = json.loads((self.root / "deploy/environments/prod.json").read_text())
            self.assertEqual(saved["host"]["host_id"], host_id)
            remote.fail_apply = False
            cli.bootstrap(project, saved, {}, False)
            self.assertFalse(marker.exists())
        self.assertEqual([action for action, _ in remote.calls].count("bootstrap-apply"), 2)

    def test_bootstrap_recovers_interruption_between_pending_and_binding_writes(self):
        project, profile = fixture()
        host_id = profile["host"]["host_id"]
        with self.bootstrap_context(BootstrapRemote(host_id)):
            path, _ = cli.pending_bootstrap(project, profile)
            cli.atomic_json(path, cli.bootstrap_marker(project, profile, host_id), mode=0o600)
            profile["host"]["host_id"] = None
            cli.bootstrap(project, profile, {}, True)
        self.assertEqual(profile["host"]["host_id"], host_id)
        self.assertFalse(path.exists())

    def test_bound_profile_cannot_initialize_another_pristine_server(self):
        project, profile = fixture()
        remote = BootstrapRemote(profile["host"]["host_id"])
        with self.bootstrap_context(remote):
            with self.assertRaisesRegex(cli.LocalError, "refuse to duplicate"):
                cli.bootstrap(project, profile, {}, False)
        self.assertEqual([action for action, _ in remote.calls], ["inspect"])
        self.assertFalse((self.root / "deploy").exists())

    def test_pending_bootstrap_cannot_follow_changed_host_binding(self):
        project, profile = fixture()
        remote = BootstrapRemote(profile["host"]["host_id"])
        with self.bootstrap_context(remote):
            path, _ = cli.pending_bootstrap(project, profile)
            cli.atomic_json(path, cli.bootstrap_marker(project, profile, profile["host"]["host_id"]), mode=0o600)
            profile["host"]["address"] = "other.example.com"
            with self.assertRaisesRegex(cli.LocalError, "different project, instance or host"):
                cli.bootstrap(project, profile, {}, False)
        self.assertEqual([action for action, _ in remote.calls], ["inspect"])

    def test_existing_matching_registry_clears_pending_marker_after_success(self):
        project, profile = fixture()
        host_id = profile["host"]["host_id"]
        remote = BootstrapRemote(host_id, registered=host_id)
        with self.bootstrap_context(remote):
            path, _ = cli.pending_bootstrap(project, profile)
            cli.atomic_json(path, cli.bootstrap_marker(project, profile, host_id), mode=0o600)
            cli.bootstrap(project, profile, {}, False)
        self.assertFalse(path.exists())

    def test_wrong_host_identity_stops_before_plan_even_with_pending_marker(self):
        project, profile = fixture()
        remote = BootstrapRemote(profile["host"]["host_id"], registered=str(uuid.uuid4()))
        with self.bootstrap_context(remote):
            path, _ = cli.pending_bootstrap(project, profile)
            cli.atomic_json(path, cli.bootstrap_marker(project, profile, profile["host"]["host_id"]), mode=0o600)
            with self.assertRaisesRegex(cli.LocalError, "identity differs"):
                cli.bootstrap(project, profile, {}, False)
        self.assertEqual([action for action, _ in remote.calls], ["inspect"])

    def test_secret_files_are_read_privately_and_parse_errors_are_redacted(self):
        secret = self.root / "backup.json"
        value = {"bucket": "private-bucket", "secret_access_key": "credential-canary"}
        secret.write_text(json.dumps(value))
        with patch.dict(os.environ, {"FOUNDRY_SECRET_BACKUP_S3_FILE": str(secret)}), patch.object(cli, "ask") as ask:
            self.assertEqual(cli.secret_input("backup_s3", "Backup", structured=True), value)
            ask.assert_not_called()
            secret.write_text("credential-canary invalid JSON")
            with self.assertRaises(cli.LocalError) as error:
                cli.secret_input("backup_s3", "Backup", structured=True)
            self.assertNotIn("credential-canary", str(error.exception))

    def test_bootstrap_private_envelope_requires_all_recovery_inputs(self):
        values = {"cloudflare_dns_token": "token-canary",
                  "app_storage": {"credentials": {}, "disks": {}},
                  "backup_s3": {key: "fixture" for key in ("endpoint", "region", "bucket", "access_key_id", "secret_access_key")},
                  "age_recipients": ["age1fixture"]}
        with patch.dict(os.environ, {}, clear=True), patch.object(cli, "ask", side_effect=[
                value if isinstance(value, str) else json.dumps(value) for value in values.values()]) as ask:
            self.assertEqual(cli.bootstrap_secrets(True), values)
            self.assertTrue(all(call.kwargs["secret"] for call in ask.call_args_list))

    def test_backup_private_envelope_reads_only_repository_and_recipients(self):
        values = {"backup_s3": {key: "fixture" for key in ("endpoint", "region", "bucket", "access_key_id", "secret_access_key")},
                  "age_recipients": ["age1fixture"]}
        with patch.object(cli, "secret_input", side_effect=lambda name, *args, **kwargs: values[name]) as read:
            self.assertEqual(cli.bootstrap_secrets(False, backup_only=True), values)
        self.assertEqual([call.args[0] for call in read.call_args_list], ["backup_s3", "age_recipients"])

    def test_build_provenance_waits_for_both_locks_and_orphan_checks(self):
        project, profile = fixture()
        events = []

        @contextlib.contextmanager
        def locked(path):
            events.append("lock")
            yield

        def provenance(*_):
            self.assertEqual(events, ["lock", "lock", "containers", "builder"])
            events.append("provenance")
            return {"manifest": {"version": "20261101T000000Z-abcdef123456"}}

        with patch.object(cli, "ROOT", self.root), patch.object(cli, "lock", locked), \
                patch.object(cli, "execute", return_value=""), \
                patch.object(cli, "recover_build_containers", side_effect=lambda _: events.append("containers")), \
                patch.object(cli, "refuse_orphan_builder", side_effect=lambda _: events.append("builder")), \
                patch.object(cli, "build_request", side_effect=provenance), \
                patch.object(cli, "source_snapshot", side_effect=cli.LocalError("snapshot boundary")):
            with self.assertRaisesRegex(cli.LocalError, "snapshot boundary"):
                cli.build(project, profile)
        self.assertEqual(events[-1], "provenance")

    def test_dirty_production_refuses_before_archive_or_docker(self):
        project, profile = fixture()
        with patch.object(cli, "execute") as execute:
            with self.assertRaisesRegex(cli.LocalError, "clean committed HEAD"):
                cli.source_snapshot(project, profile, {"manifest": {"tree_state": "dirty"}}, self.root)
        execute.assert_not_called()

    def snapshot_files(self, project, profile, root=None):
        root = self.root if root is None else root
        files = {"go.mod": "module example.com/app\ngo 1.26\n",
                 "deploy/project.json": json.dumps(project),
                 "deploy/environments/" + profile["environment"] + ".json": json.dumps(profile),
                 "deploy/topology.json": '{"schema":1,"roles":[{"name":"http"}]}',
                 "main.go": "package main\n", ".env": "private-canary"}
        for name, value in files.items():
            path = root / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(value)
        return files

    def snapshot_with_links(self, production, links=(), special=None):
        root = self.root / uuid.uuid4().hex
        root.mkdir()
        project, profile = fixture()
        if not production:
            profile["runtime_environment"] = "staging"
        files = self.snapshot_files(project, profile, root)
        name = "system/.claude/skills/rule.md"
        files[name] = "skill content\n"
        (root / name).parent.mkdir(parents=True)
        (root / name).write_text(files[name])
        members = []
        for name, target in links:
            info = tarfile.TarInfo(name)
            info.type, info.linkname = tarfile.SYMTYPE, target
            members.append(info)
            if not production:
                (root / name).parent.mkdir(parents=True, exist_ok=True)
                (root / name).symlink_to(target)
        if special is not None:
            info = tarfile.TarInfo("special")
            info.type, info.linkname = special, "main.go" if special == tarfile.LNKTYPE else ""
            members.append(info)
            if not production:
                if special == tarfile.LNKTYPE:
                    os.link(root / "main.go", root / "special")
                elif special == tarfile.FIFOTYPE:
                    os.mkfifo(root / "special")
        work = root / "snapshot"
        work.mkdir()
        request = {"manifest": {"tree_state": "clean" if production else "dirty", "commit": "a" * 40}}

        def execute(args, **_):
            if "archive" in args:
                target = next(value.removeprefix("--output=") for value in args if value.startswith("--output="))
                with tarfile.open(target, "w") as archive:
                    # Put links first to exercise forward references and avoid
                    # depending on git archive's member order.
                    for info in members:
                        archive.addfile(info)
                    for name, content in files.items():
                        info = tarfile.TarInfo(name)
                        data = content.encode()
                        info.size = len(data)
                        archive.addfile(info, io.BytesIO(data))
            if "-z" in args:
                return "\0".join([*files, *(info.name for info in members)]) + "\0"
            return ""

        with patch.object(cli, "ROOT", root), patch.object(cli, "execute", side_effect=execute), \
                patch.object(cli, "build_request", return_value=request):
            snapshot = cli.source_snapshot(project, profile, request, work)
        return snapshot, work / "source.tar"

    def test_source_snapshot_preserves_relative_in_tree_symlinks(self):
        links = [("system/.agents/skills", "../.claude/skills"),
                 ("skill-alias", "system/.agents/skills"),
                 ("dangling", "missing/target")]
        for production in (True, False):
            with self.subTest(production=production):
                snapshot, archive = self.snapshot_with_links(production, links)
                for name, target in links:
                    self.assertTrue((snapshot / name).is_symlink())
                    self.assertEqual(os.readlink(snapshot / name), target)
                self.assertEqual((snapshot / "skill-alias/rule.md").read_text(), "skill content\n")
                with tarfile.open(archive) as bundle:
                    for name, target in links:
                        self.assertTrue(bundle.getmember(name).issym())
                        self.assertEqual(bundle.getmember(name).linkname, target)

    def test_source_snapshot_refuses_absolute_symlink_targets(self):
        for production in (True, False):
            with self.subTest(production=production), self.assertRaisesRegex(cli.LocalError, "symlink"):
                self.snapshot_with_links(production, [("link", str(self.root))])

    def test_source_snapshot_refuses_parent_escape_symlinks(self):
        for production in (True, False):
            with self.subTest(production=production), self.assertRaisesRegex(cli.LocalError, "symlink"):
                self.snapshot_with_links(production, [("nested/link", "../../outside")])

    def test_source_snapshot_refuses_escaping_symlink_chains(self):
        # Each target is lexically inside the root in isolation. Expanding the
        # intermediate directory link makes the final '..' escape.
        links = [("alias", "."), ("nested/link", "../alias/../outside")]
        for production in (True, False):
            with self.subTest(production=production), self.assertRaisesRegex(cli.LocalError, "symlink"):
                self.snapshot_with_links(production, links)

    def test_source_snapshot_refuses_symlink_cycles(self):
        for production in (True, False):
            with self.subTest(production=production), self.assertRaisesRegex(cli.LocalError, "symlink"):
                self.snapshot_with_links(production, [("first", "second"), ("second", "first")])

    def test_source_snapshot_refuses_hardlinks_and_special_files(self):
        for production in (True, False):
            kinds = (tarfile.LNKTYPE, tarfile.FIFOTYPE, tarfile.CHRTYPE, tarfile.BLKTYPE) if production else (
                tarfile.LNKTYPE, tarfile.FIFOTYPE)
            for kind in kinds:
                with self.subTest(production=production, kind=kind), self.assertRaisesRegex(cli.LocalError, "unsupported"):
                    self.snapshot_with_links(production, special=kind)

    def test_source_archive_refuses_entries_beneath_symlinked_directories(self):
        with self.assertRaisesRegex(cli.LocalError, "symlink ancestor"):
            self.snapshot_with_links(True, [("system/.claude/skills", "..")])

    def test_source_inventory_does_not_follow_symlinked_ancestors(self):
        outside = self.root / "outside"
        outside.mkdir()
        (outside / "private").write_text("private-canary")
        root = self.root / "app"
        root.mkdir()
        (root / "alias").symlink_to(outside)
        with patch.object(cli, "ROOT", root), patch.object(cli, "execute", return_value="alias/private\0"), \
                patch.object(cli, "digest") as digest:
            with self.assertRaisesRegex(cli.DeployError, "symlink"):
                cli.source_inventory()
        digest.assert_not_called()

    def test_production_build_uses_exact_commit_archive_and_not_checkout(self):
        project, profile = fixture()
        files = self.snapshot_files(project, profile)
        work = self.root / "snapshot"
        work.mkdir()
        request = {"manifest": {"tree_state": "clean", "commit": "a" * 40}}
        commands = []

        def execute(args, **_):
            commands.append(args)
            if "archive" in args:
                self.assertEqual(args[-1], "a" * 40)
                target = next(value.removeprefix("--output=") for value in args if value.startswith("--output="))
                with tarfile.open(target, "w") as archive:
                    for name in files:
                        archive.add(self.root / name, arcname=name)
            return ""

        with patch.object(cli, "ROOT", self.root), patch.object(cli, "execute", side_effect=execute), \
                patch.object(cli, "build_request", return_value=request):
            snapshot = cli.source_snapshot(project, profile, request, work)
        (self.root / "main.go").write_text("package changed\n")
        self.assertEqual((snapshot / "main.go").read_text(), "package main\n")
        self.assertFalse((snapshot / ".env").exists())
        self.assertEqual(request["manifest"]["source_kind"], "git_archive")
        self.assertEqual(request["manifest"]["source_sha256"], cli.digest(work / "source.tar"))
        self.assertFalse((snapshot / "main.go").stat().st_mode & 0o222)

    def test_staging_snapshot_records_dirty_content_and_refuses_unstable_capture(self):
        project, profile = fixture()
        profile["runtime_environment"] = "staging"
        self.snapshot_files(project, profile)
        request = {"manifest": {"tree_state": "dirty", "commit": "a" * 40}}
        inventory = {name: {"sha256": cli.digest(self.root / name), "mode": 0o644}
                     for name in ("go.mod", "main.go", "deploy/topology.json")}
        for unstable in (False, True):
            work = self.root / str(unstable)
            work.mkdir()
            after = {} if unstable else inventory
            with self.subTest(unstable=unstable), patch.object(cli, "ROOT", self.root), \
                    patch.object(cli, "execute", return_value=""), \
                    patch.object(cli, "source_inventory", side_effect=[inventory, after]), \
                    patch.object(cli, "build_request", return_value=request):
                if unstable:
                    with self.assertRaisesRegex(cli.LocalError, "source changed"):
                        cli.source_snapshot(project, profile, request, work)
                else:
                    snapshot = cli.source_snapshot(project, profile, request, work)
                    self.assertEqual((snapshot / "main.go").read_text(), "package main\n")
                    self.assertEqual(request["manifest"]["tree_state"], "dirty")
                    self.assertEqual(request["manifest"]["source_kind"], "working_tree")

    def test_orphan_writer_requires_matching_record_and_exit_before_removal(self):
        project, _ = fixture()
        operation = str(uuid.uuid4())
        name = "foundry-build-" + project["id"]
        record = {"project_id": project["id"], "operation_id": operation, "containers": [name]}
        path = self.root / ".deploy" / "builds" / (operation + ".json")
        cli.atomic_json(path, record, mode=0o600)
        observed = {"Name": "/" + name, "Config": {"Labels": {
            "foundry.project": project["id"], "foundry.operation": operation}},
                    "State": {"Running": True, "Status": "running"}}
        for state, matching in (("running", True), ("exited", False), ("exited", True)):
            calls = []
            observed["State"] = {"Running": state == "running", "Status": state}
            observed["Config"]["Labels"]["foundry.project"] = project["id"] if matching else str(uuid.uuid4())

            def execute(args, **_):
                calls.append(args)
                if args[1] == "inspect":
                    return json.dumps([observed])
                if args[1] == "ps" and "name=^/" + name + "$" in args:
                    return "container-id\n"
                return ""

            with self.subTest(state=state, matching=matching), patch.object(cli, "ROOT", self.root), \
                    patch.object(cli, "execute", side_effect=execute):
                if state == "running" or not matching:
                    with self.assertRaises(cli.LocalError):
                        cli.recover_build_containers(project)
                    self.assertFalse(any(args[1] == "rm" for args in calls))
                else:
                    cli.recover_build_containers(project)
                    self.assertIn(["docker", "rm", "container-id"], calls)

    def test_rollback_resume_keeps_original_target_and_expectation(self):
        project, profile = fixture()
        target, current = "20261101T000000Z-abcdef123456", "20261101T010000Z-abcdef123456"
        operation = str(uuid.uuid4())
        requests = []

        class Remote:
            def close(self):
                pass

            def request(self, action, payload):
                requests.append((action, payload))
                return {"state": {"current": {"version": current}, "previous": {"version": target}}}

        with patch.object(cli, "ROOT", self.root), patch.object(cli, "SSH", return_value=Remote()), \
                patch.object(cli, "remember_releases"), patch.object(cli, "confirm"), \
                patch.dict(os.environ, {"ACTION": "rollback", "TARGET": target, "OPERATION": operation}, clear=True), \
                contextlib.redirect_stdout(io.StringIO()), contextlib.redirect_stderr(io.StringIO()):
            cli.deploy(project, profile, {})
            os.environ.pop("TARGET")
            current = target
            cli.deploy(project, profile, {})
        rollbacks = [request for action, request in requests if action == "rollback"]
        self.assertEqual(rollbacks[0], rollbacks[1])
        self.assertEqual(rollbacks[0]["target"], target)
        self.assertNotEqual(rollbacks[0]["expected_current"], target)
        self.assertEqual(rollbacks[0]["operation_id"], operation)


if __name__ == "__main__":
    unittest.main()
