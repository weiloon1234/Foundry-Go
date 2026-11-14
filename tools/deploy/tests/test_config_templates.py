"""Value encoding and invocation-owned secret template preparation."""
from dataclasses import asdict, replace
import hashlib
import json
import os
from pathlib import Path
import sys
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "lib"))
import config_templates
import contracts as c
import runtime_config
import services
import topology as t
import upgrades
from test_host_capabilities import declaration, selection
import test_services


class TemplateEncodingTests(unittest.TestCase):
    def render(self, format, source, value, limit=4096):
        return config_templates.render(source.encode(), format, lambda _: value, limit=limit).decode()

    def test_json_and_yaml_preserve_hostile_values_as_one_string(self):
        for value in ('line\nnew_key: true', '"quoted"', "#comment", ";comment", "]", "!tag &anchor *alias",
                      "---\nadmin: true", "雪☃é😀", "\u2028\u2029\x85", "\x00\r\t", "{{secret.OTHER_FILE}}"):
            with self.subTest(value=repr(value)):
                encoded = self.render("json", '{"password":"{{secret.TOKEN_FILE}}","fixed":true}', value)
                self.assertEqual(json.loads(encoded), {"password": value, "fixed": True})
                encoded = self.render("yaml", "password: {{secret.TOKEN_FILE}}\nfixed: true\n", value)
                lines = encoded.splitlines()
                self.assertEqual(len(lines), 2)
                self.assertEqual(json.loads(lines[0].removeprefix("password: ")), value)
                self.assertEqual(lines[1], "fixed: true")
                self.assertNotIn("\\ud83d", encoded)

    def test_complete_values_are_encoded_once_and_secret_bytes_are_not_rescanned(self):
        source = "callback: {{binding.api.url}}/auth?token={{secret.TOKEN_FILE}}\n"
        values = {"binding.api.url": "http://127.0.0.1:8000", "secret.TOKEN_FILE": 'x"{{secret.OTHER_FILE}}'}
        result = config_templates.render(source.encode(), "yaml", values.__getitem__, limit=4096)
        self.assertEqual(json.loads(result.decode().removeprefix("callback: ")),
                         'http://127.0.0.1:8000/auth?token=x"{{secret.OTHER_FILE}}')

    def test_ini_refuses_ambiguous_values_without_exposing_them(self):
        for value in ('line\nnew=value', "a\rb", "x\x00y", " leading", "trailing ", 'a"b', "a'b", "#x", "a;b",
                      "a\u2028b", "a\u2029b", "a\x85b"):
            with self.subTest(value=repr(value)), self.assertRaises(c.DeployError) as caught:
                self.render("ini", "[OBSWebSocket]\nServerPassword={{secret.TOKEN_FILE}}\n", value)
            self.assertNotIn(value, str(caught.exception))
        for value in ("safe-token", "bracket]value", "雪☃é", "!&*-:?value"):
            self.assertEqual(self.render("ini", "[OBSWebSocket]\nServerPassword={{secret.TOKEN_FILE}}\n", value),
                             "[OBSWebSocket]\nServerPassword=" + value + "\n")

    def test_text_is_one_control_free_value(self):
        self.assertEqual(self.render("text", "{{secret.TOKEN_FILE}}", '"#;]雪'), '"#;]雪')
        for control in ("\n", "\r", "\x00", "\t", "\x7f", "\x85", "\u2028"):
            with self.subTest(control=repr(control)), self.assertRaises(c.DeployError):
                self.render("text", "{{secret.TOKEN_FILE}}", "x" + control + "y")

    def test_invalid_positions_and_unsupported_yaml_context_fail_before_resolution(self):
        cases = {"json": ['{"{{secret.TOKEN_FILE}}":1}', '{"x":{{secret.TOKEN_FILE}}}',
                          '{"x":"{{unknown}}"}', '{"x":"{{secret.TOKEN_FILE}}", "x":2}'],
                 "ini": ['[{{secret.TOKEN_FILE}}]\nx=1', '{{secret.TOKEN_FILE}}=value',
                         '# {{secret.TOKEN_FILE}}'],
                 "yaml": ['{{secret.TOKEN_FILE}}: x', 'key: |\n  value: {{secret.TOKEN_FILE}}',
                          'key: "open\nvalue: {{secret.TOKEN_FILE}}\nend: "',
                          'key: &anchor\nvalue: {{secret.TOKEN_FILE}}',
                          'key: "{{secret.TOKEN_FILE}}"', '# {{secret.TOKEN_FILE}}',
                          'key: [{{secret.TOKEN_FILE}}]', 'key: {{secret.TOKEN_FILE}} # comment'],
                 "text": ['{{secret.UNKNOWN}}', '{{binding.api.unsupported}}', '{{for x}}']}
        for format, sources in cases.items():
            for source in sources:
                with self.subTest(format=format, source=source), self.assertRaises(c.DeployError):
                    config_templates.render(source.encode(), format,
                        lambda _: self.fail("Invalid template reached private resolution"), limit=4096)

    def test_output_and_substitution_amplification_are_bounded(self):
        for source in ('{"x":"{{secret.TOKEN_FILE}}"}',
                       '["{{secret.TOKEN_FILE}}","{{secret.TOKEN_FILE}}"]'):
            with self.assertRaises(c.DeployError):
                self.render("json", source, "x" * 80, limit=100 if source.startswith("[") else 32)


class TemplateDeclarationTests(unittest.TestCase):
    def test_modes_paths_and_formats_are_strict(self):
        base = {"source": "deploy/files/config", "mode": "rendered-template", "format": "ini"}
        for change in ({"mode": "seed-once"}, {"mode": "immutable"}, {"format": None}, {"format": "xml"},
                       {"path": "../escape"}, {"path": "/tmp/escape"}, {"path": "a//b"},
                       {"directory_environment": "PATH"}, {"bindings": {}}):
            with self.subTest(change=change), self.assertRaises(c.DeployError):
                t.ConfigFile.parse({**base, **change})
        config = t.ConfigFile.parse({**base, "mode": "recreate", "path": ".config/obs-studio/global.ini",
                                     "directory_environment": "HOME"})
        self.assertEqual(config.path, ".config/obs-studio/global.ini")
        for value in ({"source": base["source"], "mode": "rendered-template"},
                      {"source": base["source"], "mode": "recreate", "path": "other"}):
            with self.assertRaises(c.DeployError):
                t.ConfigFile.parse(value)

    def test_old_pinned_configuration_digest_is_unchanged(self):
        inventory, _, members = test_services.inventory_fixture()
        role = inventory.roles["relay"]
        sources = {m.path: m for m in members}
        old = c.canonical_digest({"configs": [(name, {"source": cfg.source, "mode": cfg.mode,
              "bindings": asdict(cfg)["bindings"]}, asdict(sources[cfg.source])) for name, cfg in role.configs],
              "bundles": [], "secret_paths": {}})
        self.assertEqual(services.configuration_digest(role, members), old)

    def test_generation_fourteen_is_required_and_sticky_after_rollback(self):
        raw = declaration()
        raw["roles"][1]["configs"] = {"settings": {"source": "deploy/files/config", "mode": "rendered-template", "format": "json"}}
        parsed = t.parse_topology(raw)
        self.assertEqual(upgrades.feature_generation(parsed, {}), 14)
        with self.assertRaisesRegex(c.DeployError, "generation 14"):
            upgrades.check_feature_generation({"tools": {"generation": 13}}, parsed, {})
        host = {"tools": {"generation": 13}, "instances": {}}
        request = {"topology": raw, "client": {"generation": 14}}
        with self.assertRaisesRegex(c.DeployError, "generation 14"):
            upgrades._check_feature_request(host, request, "bootstrap-plan")
        host["tools"]["generation"] = 14
        with self.assertRaisesRegex(c.DeployError, "generation 14"):
            upgrades._check_feature_request(host, {**request, "client": {"generation": 13}}, "bootstrap-plan")
        upgrades._check_feature_request(host, request, "bootstrap-plan")
        entry = {"deployment_selection": {**selection(), "minimum_generation": 14}}
        self.assertEqual(upgrades.feature_generation(t.parse_topology(declaration()), entry), 14)
        with self.assertRaisesRegex(c.DeployError, "generation 14"):
            upgrades.check_instance_capabilities(entry, 13)
        upgrades.check_instance_capabilities(entry, 14)
        self.assertIn("lib/config_templates.py", upgrades.required_files(14))
        self.assertNotIn("lib/config_templates.py", upgrades.required_files(13))


class TemplatePreparationTests(unittest.TestCase):
    setUp = test_services.ServicePreparationTests.setUp
    prepare = test_services.ServicePreparationTests.prepare

    def configure(self, *, source=b'[OBSWebSocket]\nServerPassword={{secret.TOKEN_FILE}}\n',
                  format="ini", mode="recreate", value="private-canary-雪"):
        config = t.ConfigFile.parse({"source": "deploy/files/template", "mode": mode, "format": format,
                                    "path": ".config/obs-studio/global.ini", "directory_environment": "HOME"})
        self.role = replace(self.role, configs=(("settings", config),), secret_files=(("TOKEN_FILE", "token"),))
        (self.root / config.source).write_bytes(source)
        self.members = (c.PayloadMember(config.source, hashlib.sha256(source).hexdigest(), len(source), "config"),)
        self.secret = self.host.paths.instance_config(self.entry["name"]) / "template.secret"
        self.host.owned_write(self.secret, value + "\n", 0o640)
        self.secret_paths = {"TOKEN_FILE": str(self.secret)}
        self.spec = replace(self.spec, arguments=("--config", self.paths.config("settings", mode, config.path)),
                            configuration_sha256=services.configuration_digest(self.role, self.members, secret_paths=self.secret_paths))
        self.invocation = replace(self.invocation, unit_spec_sha256=c.canonical_digest(c.record_document(self.spec)))

    def render(self, invocation=None):
        return self.prepare(invocation, secret_paths=self.secret_paths, entry=self.entry)

    def test_private_baseline_and_writable_home_are_fresh_each_invocation(self):
        self.configure()
        first = self.render()
        relative = "config/settings/.config/obs-studio/global.ini"
        self.assertIn("private-canary-雪", (first / relative).read_text())
        self.assertEqual((first / relative).stat().st_mode & 0o777, 0o600)
        self.assertEqual((first / relative).stat().st_uid, os.geteuid())
        self.assertEqual((first / "baselines/settings").stat().st_mode & 0o777, 0o640)
        self.assertEqual((first / "baselines/settings").stat().st_gid, os.getegid())
        (first / relative).write_text("app-edited")
        next_start = replace(self.invocation, process=replace(self.invocation.process, invocation_id="e" * 32))
        second = self.render(next_start)
        self.assertIn("private-canary-雪", (second / relative).read_text())
        self.assertEqual((first / relative).read_text(), "app-edited")
        self.assertEqual(services.config_environment(self.role, self.paths), {"HOME": self.paths.base + "/current/config/settings"})
        self.assertNotIn("private-canary", (second / "prepared.json").read_text())

    def test_readonly_template_is_group_readable_and_confined(self):
        self.configure(mode="rendered-template")
        view = self.render()
        path = view / "config/settings/.config/obs-studio/global.ini"
        self.assertEqual(path.stat().st_mode & 0o777, 0o640)
        self.assertEqual(path.stat().st_gid, os.getegid())
        self.assertEqual(path.parent.stat().st_mode & 0o777, 0o755)

    def test_unrepresentable_secret_does_not_publish_and_leaves_no_secret_baseline(self):
        self.configure(value="private-canary\nInjected=true")
        with self.assertRaises(c.DeployError) as caught:
            self.render()
        self.assertNotIn("private-canary", str(caught.exception))
        base = self.host.paths.p(self.paths.base)
        self.assertFalse((base / "current").exists())
        self.assertFalse(list(base.glob("invocations/*/baselines/*")))

    def test_failed_next_start_cannot_reuse_previous_output_as_success(self):
        self.configure()
        first = self.render()
        self.secret.write_text("unsafe\nInjected=true\n")
        next_start = replace(self.invocation, process=replace(self.invocation.process, invocation_id="e" * 32))
        with self.assertRaises(c.DeployError):
            self.render(next_start)
        base = self.host.paths.p(self.paths.base)
        self.assertEqual((base / "current").resolve(), first)
        self.assertFalse((base / "invocations" / ("e" * 32) / "prepared.json").exists())

    def test_recreate_does_not_follow_a_previous_working_copy_symlink(self):
        self.configure()
        first = self.render()
        path = first / "config/settings/.config/obs-studio/global.ini"
        target = self.root / "untouched"
        target.write_text("preserved")
        path.unlink()
        path.symlink_to(target)
        next_start = replace(self.invocation, process=replace(self.invocation.process, invocation_id="e" * 32))
        second = self.render(next_start)
        self.assertIn("private-canary", (second / path.relative_to(first)).read_text())
        self.assertEqual(target.read_text(), "preserved")

    def test_undeclared_and_unresolved_references_fail_closed(self):
        for reference in ("secret.MISSING_FILE", "binding.missing.url", "binding.missing.path"):
            with self.subTest(reference=reference):
                self.configure(source=('value: {{' + reference + '}}\n').encode(), format="yaml")
                self.invocation = replace(self.invocation, process=replace(self.invocation.process,
                    invocation_id=hashlib.sha256(reference.encode()).hexdigest()[:32]))
                with self.assertRaises(c.DeployError):
                    self.render()
                self.assertFalse((self.host.paths.p(self.paths.base) / "current").exists())

    def test_private_reader_refuses_symlinks_and_world_readable_secrets(self):
        self.configure()
        self.secret.chmod(0o644)
        with self.assertRaises(c.DeployError):
            self.render()
        self.secret.chmod(0o640)
        target = self.secret.with_name("target")
        self.secret.rename(target)
        self.secret.symlink_to(target)
        next_start = replace(self.invocation, process=replace(self.invocation.process, invocation_id="e" * 32))
        with self.assertRaises(c.DeployError):
            self.render(next_start)

    def test_resolved_endpoint_fields_and_executable_path_use_frozen_bindings(self):
        self.configure(source=b'{"address":"{{binding.api.address}}","port":"{{binding.api.port}}",'
                              b'"url":"{{binding.api.url}}","path":"{{binding.tool.path}}"}', format="json")
        self.role = replace(self.role, common=replace(self.role.common, bindings=(
            ("api", t.EndpointBinding("relay", "control", "http")),
            ("tool", t.ExecutableBinding(t.PackageExecutable("jq", "/usr/bin/jq"))))))
        self.spec = replace(self.spec, bindings=c.RoleBindings("relay",
            (c.ResolvedExecutableBinding("tool", "/usr/bin/jq", "a" * 64),),
            (c.ResolvedEndpointBinding("api", c.AllocatedEndpoint("relay", "control", "::1", 8201), "http"),), ()))
        self.invocation = replace(self.invocation, unit_spec_sha256=c.canonical_digest(c.record_document(self.spec)))
        view = self.render()
        self.assertEqual(json.loads((view / "config/settings/.config/obs-studio/global.ini").read_text()),
                         {"address": "::1", "port": "8201", "url": "http://[::1]:8201", "path": "/usr/bin/jq"})

    def test_directory_environment_conflicts_are_rejected(self):
        self.configure()
        duplicate = replace(self.role, configs=(*self.role.configs, ("other", self.role.configs[0][1])))
        with self.assertRaises(c.DeployError):
            services.config_environment(duplicate, self.paths)
        config = replace(self.role.configs[0][1], directory_environment="XDG_CONFIG_HOME")
        explicit = replace(self.role, configs=(("settings", config),),
                           environment=(("XDG_CONFIG_HOME", t.Argument(t.ArgumentKind.LITERAL, "/tmp")),))
        with self.assertRaises(c.DeployError):
            services.config_environment(explicit, self.paths)


if __name__ == "__main__":
    unittest.main()
