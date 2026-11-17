"""Configuration trees reuse file validation and preparation."""
from contextlib import nullcontext
from dataclasses import replace
import json
import os
from pathlib import Path
from types import SimpleNamespace
import sys
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "lib"))

import artifacts
import contracts as c
import runtime_config
import services
import topology as t
import test_config_templates
from test_host_capabilities import declaration


GLOBAL = ".config/obs-studio/global.ini"
BASIC = ".config/obs-studio/basic/profiles/stream/basic.ini"
ENCODER = ".config/obs-studio/basic/profiles/stream/streamEncoder.json"


def tree_declaration():
    return {"directory_environment": "HOME", "files": {
        GLOBAL: {"source": "deploy/files/template", "mode": "recreate", "format": "ini"},
        BASIC: {"source": "deploy/files/basic.ini", "mode": "recreate", "format": "ini"},
        ENCODER: {"source": "deploy/files/encoder.json", "mode": "recreate", "format": "json"}}}


class ConfigTreeDeclarationTests(unittest.TestCase):
    def test_paths_bounds_and_exclusive_shapes(self):
        leaf = tree_declaration()["files"][GLOBAL]
        for path in ("", "/absolute", "a/../b", "a/./b", ".", "..", "a//b", "a/", "a\\b", "/".join(["a"] * 9)):
            with self.subTest(path=path), self.assertRaises(c.DeployError):
                t.parse_config({"files": {path: leaf}})
        for files in ({}, [], None, {"a": leaf, "a/b": leaf}, {str(n): leaf for n in range(33)}):
            with self.subTest(files=files), self.assertRaises(c.DeployError):
                t.parse_config({"files": files})
        for key, value in {"source": leaf["source"], "mode": "recreate", "format": "ini", "path": "x", "bindings": {}}.items():
            with self.subTest(key=key), self.assertRaises(c.DeployError):
                t.parse_config({**tree_declaration(), key: value})
        for key, value in (("path", "x"), ("directory_environment", "HOME"), ("extra", True)):
            with self.subTest(key=key), self.assertRaises(c.DeployError):
                t.parse_config({"files": {"x": {**leaf, key: value}}})
        for environment in (None, "PATH", "home"):
            with self.subTest(environment=environment), self.assertRaises(c.DeployError):
                t.parse_config({**tree_declaration(), "directory_environment": environment})
        plain = {"source": leaf["source"], "mode": "immutable"}
        with self.assertRaises(c.DeployError):
            t.parse_config({"files": {"x": plain}, "directory_environment": "HOME"})
        self.assertEqual(len(t.parse_config({"files": {str(n): plain for n in range(32)}}).files), 32)
        self.assertEqual(t.parse_config({"files": {"/".join(["a"] * 8): plain}}).files[0].mode, "immutable")
        self.assertEqual(t.parse_config({**tree_declaration(), "directory_environment": "XDG_CONFIG_HOME"}).directory_environment,
                         "XDG_CONFIG_HOME")
        with self.assertRaises(c.DeployError):
            c.parse_json('{"files":{"x":{},"x":{}}}')

    def test_members_use_single_file_validation(self):
        leaf = tree_declaration()["files"][GLOBAL]
        invalid = [{**leaf, **change} for change in (
            {"source": "private/file"}, {"source": "deploy/files/../escape"}, {"mode": "unknown"},
            {"mode": "seed-once"}, {"mode": "immutable"}, {"format": None}, {"format": "xml"}, {"bindings": {}})]
        invalid += [{"source": leaf["source"], "mode": "rendered-template"},
                    {"source": leaf["source"], "mode": "immutable", "bindings": {"/x": {"copy_number": "self"}}},
                    {"source": leaf["source"], "mode": "rendered-json", "bindings": {"bad": {"copy_number": "self"}}}]
        for value in invalid:
            with self.subTest(value=value):
                with self.assertRaises(c.DeployError):
                    t.ConfigFile.parse(value)
                with self.assertRaises(c.DeployError):
                    t.parse_config({"files": {"nested/file": value}})
        for value in (leaf, {"source": leaf["source"], "mode": "seed-once"},
                      {"source": leaf["source"], "mode": "rendered-json", "bindings": {"/copy": {"copy_number": "self"}}}):
            self.assertEqual(t.parse_config({"files": {"nested/file": value}}).files,
                             (replace(t.ConfigFile.parse(value), path="nested/file"),))
        raw = declaration()
        raw["roles"][1]["configs"] = {"tree": {"files": {"file": {"source": leaf["source"], "mode": "rendered-json",
            "bindings": {"/port": {"listener_port": "unknown"}}}}}}
        with self.assertRaises(c.DeployError):
            t.parse_topology(raw)


class ConfigTreePreparationTests(unittest.TestCase):
    setUp = test_config_templates.TemplatePreparationTests.setUp
    prepare = test_config_templates.TemplatePreparationTests.prepare
    render = test_config_templates.TemplatePreparationTests.render

    def configure(self, raw=None):
        test_config_templates.TemplatePreparationTests.configure(self)
        raw = raw or tree_declaration()
        for name, data in (("basic.ini", b"[Output]\nMode=Advanced\n"), ("encoder.json", b'{"keyint_sec":1}\n')):
            (self.root / "deploy/files" / name).write_bytes(data)
        tree = t.parse_config(raw)
        self.role = replace(self.role, configs=(("settings", tree),))
        self.members = tuple(c.PayloadMember(file.source, c.digest(self.root / file.source),
                            (self.root / file.source).stat().st_size, "config") for file in tree.files)
        self.spec = replace(self.spec, arguments=("--config", self.paths.base + "/current/config/settings"),
                            configuration_sha256=services.configuration_digest(self.role, self.members, secret_paths=self.secret_paths))
        self.invocation = replace(self.invocation, unit_spec_sha256=c.canonical_digest(c.record_document(self.spec)))

    def test_nested_home_tree_permissions_restart_and_unit_environment(self):
        self.configure()
        view = self.render()
        home = view / "config/settings"
        self.assertIn("ServerPassword=private-canary-雪", (home / GLOBAL).read_text())
        self.assertEqual((home / BASIC).read_text(), "[Output]\nMode=Advanced\n")
        self.assertEqual(json.loads((home / ENCODER).read_text()), {"keyint_sec": 1})
        for path in [home, *home.rglob("*")]:
            self.assertFalse(path.is_symlink())
            self.assertEqual(path.stat().st_uid, os.geteuid())
            self.assertEqual(path.stat().st_gid, os.getegid())
            self.assertEqual(path.stat().st_mode & 0o777, 0o700 if path.is_dir() else 0o600)
        for file in self.role.configs[0][1].files:
            baseline = view / "baselines/settings" / file.path
            self.assertEqual(baseline.read_bytes(), (home / file.path).read_bytes())
            self.assertEqual(baseline.stat().st_mode & 0o777, 0o640)
        (home / BASIC).write_text("app-edited")
        (home / "unexpected").write_text("runtime-only")
        second = self.render(replace(self.invocation, process=replace(self.invocation.process, invocation_id="e" * 32)))
        self.assertEqual((second / "config/settings" / BASIC).read_text(), "[Output]\nMode=Advanced\n")
        self.assertFalse((second / "config/settings/unexpected").exists())
        expected = self.paths.base + "/current/config/settings"
        self.assertEqual(services.config_environment(self.role, self.paths), {"HOME": expected})
        self.inventory.roles["relay"] = self.role
        self.inventory.specs[self.spec.identity] = self.spec
        unit = services.render_unit(self.inventory, self.spec.identity, self.entry, "a" * 64, secret_paths=self.secret_paths)
        self.assertIn("HOME=" + expected, unit)
        self.assertNotIn("private-canary", unit)
        duplicate = replace(self.role, configs=(*self.role.configs, ("other", self.role.configs[0][1])))
        with self.assertRaises(c.DeployError):
            services.config_environment(duplicate, self.paths)
        with self.assertRaises(c.DeployError):
            services.config_environment(replace(self.role, environment=(("HOME", t.Argument(t.ArgumentKind.LITERAL, "/tmp")),)), self.paths)

    def test_every_file_affects_digest_and_inventory(self):
        self.configure()
        original = services.configuration_digest(self.role, self.members)
        tree = self.role.configs[0][1]
        for index, file in enumerate(tree.files):
            with self.subTest(path=file.path):
                members = list(self.members)
                members[index] = replace(members[index], sha256="a" * 64)
                self.assertNotEqual(original, services.configuration_digest(self.role, members))
                changed = replace(tree, files=tuple(replace(item, mode="rendered-template") if item == file else item for item in tree.files))
                self.assertNotEqual(original, services.configuration_digest(replace(self.role, configs=(("settings", changed),)), self.members))
                smaller = replace(tree, files=tuple(item for item in tree.files if item != file))
                reduced = services.configuration_digest(replace(self.role, configs=(("settings", smaller),)), self.members)
                self.assertNotEqual(original, reduced)
                renamed = replace(tree, files=tuple(replace(item, path="renamed/file") if item == file else item for item in tree.files))
                self.assertNotEqual(original, services.configuration_digest(replace(self.role, configs=(("settings", renamed),)), self.members))
                with self.assertRaises(c.DeployError):
                    services.configuration_digest(self.role, self.members[:index] + self.members[index + 1:])
        raw = declaration()
        raw["roles"][1]["configs"] = {"settings": tree_declaration()}
        kinds = artifacts.declared_release_files(t.parse_topology(raw), (), ())
        self.assertTrue(all(kinds[file.source] == "config" for file in tree.files))
        reordered = tree_declaration()
        reordered["files"] = dict(reversed(list(reordered["files"].items())))
        self.assertEqual(tree, t.parse_config(reordered))

    def test_mixed_modes_keep_seed_and_recreate_other_files(self):
        raw = tree_declaration()
        raw["files"][BASIC] = {"source": "deploy/files/basic.ini", "mode": "seed-once"}
        raw["files"][ENCODER] = {"source": "deploy/files/encoder.json", "mode": "rendered-json",
                                 "bindings": {"/keyint_sec": {"copy_number": "self"}}}
        raw["files"]["static/file"] = {"source": "deploy/files/fixed.txt", "mode": "immutable"}
        self.configure(raw)
        first = self.render()
        home = first / "config/settings"
        self.assertTrue((home / BASIC).is_symlink())
        self.assertEqual((home / BASIC).stat().st_mode & 0o777, 0o600)
        self.assertEqual((home / ENCODER).stat().st_mode & 0o777, 0o444)
        self.assertEqual(json.loads((home / ENCODER).read_text()), {"keyint_sec": 1})
        self.assertEqual((home / "static/file").read_bytes(), b"fixed")
        (home / BASIC).write_text("persistent edit")
        second = self.render(replace(self.invocation, process=replace(self.invocation.process, invocation_id="e" * 32)))
        self.assertEqual((second / "config/settings" / BASIC).read_text(), "persistent edit")
        self.assertIn("private-canary", (second / "config/settings" / GLOBAL).read_text())
        seed = (home / BASIC).resolve()
        seed.unlink()
        seed.symlink_to(self.root / "deploy/files/basic.ini")
        with self.assertRaises(c.DeployError):
            self.render(replace(self.invocation, process=replace(self.invocation.process, invocation_id="f" * 32)))

    def test_each_copy_prepares_an_independent_tree(self):
        self.configure()
        store = SimpleNamespace(preparation=lambda *args, **kwargs: nullcontext(self.inventory.resolution))
        homes = []
        for ordinal in (1, 2):
            identity = replace(self.spec.identity, ordinal=ordinal)
            paths = services.ServicePaths.for_unit(identity, self.invocation.resolution_id)
            spec = replace(self.spec, identity=identity, arguments=("--config", paths.base + "/current/config/settings"))
            invocation = replace(self.invocation, unit=identity, unit_spec_sha256=c.canonical_digest(c.record_document(spec)))
            view = runtime_config.prepare_service(self.host, store, invocation, self.role, spec, self.root, self.members,
                       secret_paths=self.secret_paths, uid=os.geteuid(), gid=os.getegid(), entry=self.entry)
            homes.append(view / "config/settings")
            self.assertEqual(services.config_environment(self.role, paths)["HOME"], paths.base + "/current/config/settings")
        self.assertNotEqual(homes[0], homes[1])
        (homes[0] / GLOBAL).write_text("copy-one-edit")
        self.assertIn("private-canary", (homes[1] / GLOBAL).read_text())

    def test_unsafe_sources_and_nested_symlinks_never_publish(self):
        self.configure()
        source = self.root / self.members[0].path
        source.unlink()
        source.symlink_to(self.root / "deploy/files/template")
        with self.assertRaises(c.DeployError):
            self.render()
        self.assertFalse((self.host.paths.p(self.paths.base) / "current").exists())

    def test_nested_destination_symlink_is_refused(self):
        self.configure()
        original = runtime_config._service_directory
        def inject(host, path):
            if path.name == ".config" and path.parent.name == "settings" and "config" in path.parts:
                path.symlink_to(self.root, target_is_directory=True)
            return original(host, path)
        with patch.object(runtime_config, "_service_directory", side_effect=inject), self.assertRaises(c.DeployError):
            self.render()
        self.assertFalse((self.host.paths.p(self.paths.base) / "current").exists())

    def test_each_file_retains_size_limit(self):
        self.configure()
        with patch.object(runtime_config, "MAX_VALUE_BYTES", 8), self.assertRaises(c.DeployError):
            self.render()
        self.assertFalse((self.host.paths.p(self.paths.base) / "current").exists())


if __name__ == "__main__":
    unittest.main()
