"""Lane C consumer boundaries; expand placements without lifting A1's profile gate."""
import copy
from contextlib import nullcontext
import hashlib
import os
from dataclasses import replace
import json
from pathlib import Path
import sys
import tempfile
from types import SimpleNamespace
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "lib"))
import contracts as c
import edge
import firewall
import listeners
import runtime_config as config
import services
import topology as t
from host import Host, Paths
from test_contracts import fixture
from test_host_capabilities import declaration, profile


def copies_fixture(count=2, *, unix=False, instance_id=None):
    raw = declaration()
    raw["secrets"] = {"password": {"scope": "copy", "prompt": "Worker password"}, "shared": {}}
    role = raw["roles"][1]
    role["secret_files"] = {"PASSWORD_FILE": {"secret": "password", "scope": "copy"}, "SHARED_FILE": "shared"}
    if unix:
        role["listeners"]["socket"] = {"transport": "unix", "socket": "worker"}
    raw["roles"][0]["requires"] = ["relay"] if unix else []
    raw["roles"][0]["bindings"] = {
        "workers": {"copies": {"role": "relay", "listener": "socket" if unix else "control",
                                "scheme": "unix" if unix else "http", "secret": "password"}},
        "workers_public": {"public_listener": {"role": "relay", "listener": "ingress"}}}
    topology = t.parse_topology(raw)
    profile_document = profile()
    if instance_id is not None:
        profile_document["instance_id"] = instance_id
    base = t.parse_profile(profile_document, topology)
    placement = t.parse_placement({"http": 1, "relay": count, "worker": 1})
    public = replace(base.listeners[0][1], ports=tuple(9443 + n for n in range(count)))
    profile_document["placement"] = dict(placement)
    profile_document["listeners"]["relay.ingress"]["ports"] = list(public.ports)
    placed = replace(base, placement=placement, listeners=(("relay.ingress", public),),
                     canonical_json=json.dumps(profile_document, sort_keys=True, separators=(",", ":")))
    t.expand_placement(topology, placed.instance_id, placed.placement)
    endpoints = listeners.allocate_private(placed.instance_id, listeners.private_listeners(topology, placed), (), ())
    resolution = SimpleNamespace(endpoints=endpoints, unix_endpoints=listeners.unix_endpoints(topology, placed))
    return raw, topology, placed, resolution


class CopiesConsumerTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.host = Host(Paths(Path(temporary.name).resolve()))
        self.entry = fixture()[2]
        self.raw, self.topology, self.profile, self.resolution = copies_fixture()
        self.entry["profile"] = self.profile.projection()
        self.entry["configuration_revision"] = 1
        self.entry["topology"] = {**self.raw, "sha256": self.topology.sha256}

    def stage(self, topology=None, placed=None):
        topology, placed = topology or self.topology, placed or self.profile
        changes = {key: "private-canary-" + name for key, name in config.service_secret_keys(topology, placed).items()}
        return config.stage_service_secrets(self.host, self.entry, topology, changes, required=False)

    def test_growth_shrink_and_regrow_keep_old_private_values(self):
        paths = self.stage()
        config.select_service_secrets(self.host, self.entry, paths)
        grown = replace(self.profile, placement=t.parse_placement({"http": 1, "relay": 3, "worker": 1}))
        missing = [r for r in config.service_secret_requirements(self.topology, grown, paths) if not r["present"]]
        self.assertEqual([r["key"] for r in missing], [t.SERVICE_SECRET_PREFIX + "COPY__RELAY__3__PASSWORD"])
        self.assertTrue(all(r["redacted"] for r in missing))
        with self.assertRaises(c.DeployError):
            config.stage_service_secrets(self.host, self.entry, self.topology, {}, profile=grown)
        grown_paths = config.stage_service_secrets(self.host, self.entry, self.topology, {missing[0]["key"]: "third"}, profile=grown)
        self.assertEqual({key: grown_paths[key] for key in paths}, paths)
        config.select_service_secrets(self.host, self.entry, grown_paths)
        shrunk = replace(self.profile, placement=t.parse_placement({"http": 1, "relay": 1, "worker": 1}))
        delivery = config.unit_secret_paths(self.host, self.entry, self.topology, profile=shrunk)
        self.assertEqual(list(delivery), [c.UnitIdentity(shrunk.instance_id, "relay", 1)])
        self.assertEqual(config.service_secret_snapshot(self.host, self.entry), grown_paths)
        self.assertTrue(all(r["present"] for r in config.service_secret_requirements(self.topology, grown, grown_paths)))
        config.unit_secret_paths(self.host, self.entry, self.topology, profile=grown)

    def test_singleton_delivery_contract_refuses_numbered_projection(self):
        paths = self.stage()
        singleton = replace(self.profile, placement=t.parse_placement({"http": 1, "relay": 1, "worker": 1}))
        delivery = config.service_secret_paths(self.host, self.entry, self.topology, paths=paths, profile=singleton)
        self.assertEqual(delivery, {"relay": {"PASSWORD_FILE": paths["relay.1.password"], "SHARED_FILE": paths["shared"]}})
        with self.assertRaises(c.DeployError) as caught:
            config.service_secret_paths(self.host, self.entry, self.topology, paths=paths, profile=self.profile)
        self.assertEqual(caught.exception.code, "unsupported_capability")

    def test_copy_delivery_and_shared_default(self):
        paths = self.stage()
        delivered = config.unit_secret_paths(self.host, self.entry, self.topology, paths=paths, profile=self.profile)
        first, second = (delivered[c.UnitIdentity(self.profile.instance_id, "relay", n)] for n in (1, 2))
        self.assertNotEqual(first["PASSWORD_FILE"], second["PASSWORD_FILE"])
        self.assertEqual(first["SHARED_FILE"], second["SHARED_FILE"])
        for name, path in paths.items():
            self.assertEqual(Path(path).stat().st_mode & 0o777, 0o640)
            self.assertEqual(Path(path).read_text().strip(), "private-canary-" + name)
        symlink = Path(first["PASSWORD_FILE"]).with_name("link.secret")
        symlink.symlink_to(first["PASSWORD_FILE"])
        key = t.SERVICE_SECRET_PREFIX + "COPY__RELAY__1__PASSWORD"
        for changes in ({key: "one", key + "_FILE": first["PASSWORD_FILE"]}, {key + "_FILE": str(symlink)},
                        {t.SERVICE_SECRET_PREFIX + "COPY__RELAY__129__PASSWORD": "bad"}):
            with self.assertRaises(c.DeployError):
                config.stage_service_secrets(self.host, self.entry, self.topology, changes, required=False)

    def test_ordered_tcp_and_unix_lists_and_removed_ordinals(self):
        for unix in (False, True):
            _, topology, placed, resolution = copies_fixture(unix=unix, instance_id=self.profile.instance_id)
            paths = self.stage(topology, placed)
            binding, = config.resolve_copies_bindings(self.host, self.entry, topology, placed, topology.roles[0], resolution, paths=paths)
            self.assertEqual([item.ordinal for item in binding.copies], [1, 2])
            self.assertEqual([item.endpoint.ordinal for item in binding.copies], [1, 2])
            self.assertEqual(len({item.secret_file for item in binding.copies}), 2)
            if unix:
                self.assertEqual([item.endpoint.name for item in binding.copies], ["worker", "worker.2"])
            wire = c.record_document(c.RoleBindings("http", (), (), (), copies=(binding,)))
            self.assertEqual(c.RoleBindings.parse(wire).copies, (binding,))
            self.assertNotIn("private-canary", json.dumps(wire))
            shrunk = replace(placed, placement=t.parse_placement({"http": 1, "relay": 1, "worker": 1}))
            remaining, = config.resolve_copies_bindings(self.host, self.entry, topology, shrunk, topology.roles[0], resolution, paths=paths)
            self.assertEqual([item.ordinal for item in remaining.copies], [1])

    def test_same_secret_name_is_separate_for_each_provider_role(self):
        raw = copy.deepcopy(self.raw)
        provider = copy.deepcopy(raw["roles"][1])
        provider["name"] = "processor"
        provider["secret_files"]["PASSWORD_FILE"] = "password"
        raw["roles"].append(provider)
        keys = config.service_secret_keys(t.parse_topology(raw))
        self.assertEqual(keys[t.SERVICE_SECRET_PREFIX + "COPY__RELAY__1__PASSWORD"], "relay.1.password")
        self.assertEqual(keys[t.SERVICE_SECRET_PREFIX + "COPY__PROCESSOR__1__PASSWORD"], "processor.1.password")
        self.assertEqual(keys[t.SERVICE_SECRET_PREFIX + "SHARED"], "shared")

    def test_recreate_template_receives_each_identitys_own_value(self):
        from test_admission import helper
        source = b"[control]\npassword={{secret.PASSWORD_FILE}}\n"
        template = t.ConfigFile.parse({"source": "deploy/files/worker.ini", "mode": "recreate", "format": "ini"})
        role = replace(self.topology.roles[1], configs=(("settings", template),))
        root = self.host.paths.opt(self.entry["name"]) / "releases" / "fixture"
        (root / "deploy/files").mkdir(parents=True)
        (root / template.source).write_bytes(source)
        members = (c.PayloadMember(template.source, hashlib.sha256(source).hexdigest(), len(source), "config"),)
        private = self.stage()
        delivered = config.unit_secret_paths(self.host, self.entry, self.topology, paths=private, profile=self.profile)
        resolution_id = "00000000-0000-4000-8000-000000000003"
        seed = c.RuntimeResolution(resolution_id, "a" * 64, 1, ("http", "relay", "worker"), self.resolution.endpoints,
                                   (), "a" * 64, "a" * 64, "a" * 64, self.resolution.unix_endpoints)
        store = SimpleNamespace(preparation=lambda *args, **kwargs: nullcontext(seed))
        outputs = []
        for identity, secret_paths in delivered.items():
            paths = services.ServicePaths.for_unit(identity, resolution_id)
            spec = c.UnitSpec(identity, "/opt/foundry/fixture/worker", "a" * 64,
                              ("--config", paths.config("settings", "recreate")), "a" * 64,
                              c.RoleBindings("relay", (), (), ()),
                              services.configuration_digest(role, members, secret_paths=secret_paths))
            invocation = replace(helper("relay"), unit=identity, resolution_id=resolution_id,
                                 unit_spec_sha256=c.canonical_digest(c.record_document(spec)))
            view = config.prepare_service(self.host, store, invocation, role, spec, root, members,
                                          secret_paths=secret_paths, uid=os.geteuid(), gid=os.getegid(), entry=self.entry)
            rendered = (view / "config/settings/value").read_text()
            self.assertIn("password=private-canary-relay." + str(identity.ordinal) + ".password", rendered)
            self.assertEqual((view / "baselines/settings").stat().st_mode & 0o777, 0o640)
            self.assertNotIn("private-canary", (view / "prepared.json").read_text())
            outputs.append(rendered)
        self.assertEqual(len(set(outputs)), 2)

    def test_unknown_provider_secret_scheme_and_cycles_refuse(self):
        for mutate in (
            lambda raw: raw["roles"][0]["bindings"]["workers"]["copies"].update(role="missing"),
            lambda raw: raw["roles"][0]["bindings"]["workers"]["copies"].update(listener="missing"),
            lambda raw: raw["roles"][0]["bindings"]["workers"]["copies"].update(secret="shared"),
            lambda raw: raw["roles"][0]["bindings"]["workers"]["copies"].update(scheme="unix"),
            lambda raw: raw["roles"][1].update(requires=["http"]),
            lambda raw: raw["secrets"]["password"].update(scope="machine"),
            lambda raw: raw["secrets"]["password"].pop("scope"),
        ):
            bad = copy.deepcopy(self.raw)
            mutate(bad)
            with self.assertRaises(c.DeployError):
                t.parse_topology(bad)

    def test_public_ports_route_to_distinct_backends_and_claim_ordinals(self):
        projections = edge.stream_projection(self.profile, self.resolution, "workers.example.org", topology=self.topology)
        self.assertEqual(set(projections), {"stream-relay.ingress.conf", "stream-relay.ingress.2.conf"})
        for ordinal, port in enumerate((9443, 9444), 1):
            endpoint = next(e for e in self.resolution.endpoints if (e.role, e.ordinal, e.listener) == ("relay", ordinal, "ingress"))
            leaf = "stream-relay.ingress" + (f".{ordinal}" if ordinal > 1 else "") + ".conf"
            self.assertIn(f"listen 203.0.113.10:{port} ssl;", projections[leaf])
            self.assertIn(f"proxy_pass {endpoint.address}:{endpoint.port};", projections[leaf])
        claims = listeners.public_claims(self.profile)
        self.assertEqual([(claim.ordinal, claim.socket.port) for claim in claims], [(1, 9443), (2, 9444)])
        _, rules = firewall.exposure_rules(self.profile.instance_id, claims, self.profile.listeners, 4)
        self.assertEqual([rule[rule.index("--dport") + 1] for rule in rules], ["9443", "9444"])
        reference = dict(self.topology.roles[0].common.bindings)["workers_public"]
        public = edge.resolve_public_listener_binding(self.topology, self.profile, "workers_public", reference)
        wire = c.record_document(c.RoleBindings("http", (), (), (), public_listeners=(public,)))
        self.assertEqual(wire["public_listeners"][0]["ports"], [{"ordinal": 1, "port": 9443}, {"ordinal": 2, "port": 9444}])
        bad = replace(self.profile, listeners=(("relay.ingress", replace(self.profile.listeners[0][1], ports=(9443,))),))
        with self.assertRaises(c.DeployError):
            listeners.public_policies(bad, self.topology)

    def test_empty_collections_preserve_wire_and_console_drops_only_secret_paths(self):
        self.assertNotIn("copies", c.record_document(c.RoleBindings("http", (), (), ())))
        paths = self.stage()
        bindings = config.resolve_copies_bindings(self.host, self.entry, self.topology, self.profile,
                                                 self.topology.roles[0], self.resolution, paths=paths)
        identifier = "00000000-0000-4000-8000-000000000002"
        root = self.host.paths.opt(self.entry["name"]) / "runtime" / identifier
        descriptor = {"bindings": c.record_document(c.RoleBindings("http", (), (), (), copies=bindings))}
        config._write_binding_descriptor(self.host, root / "http.json", descriptor)
        console = json.loads(config.console_binding_descriptor(self.host, self.entry, identifier).read_text())
        self.assertTrue(all("secret_file" not in row for row in console["bindings"]["copies"][0]["copies"]))
        self.assertEqual(json.loads((root / "http.json").read_text()), descriptor)
        self.assertEqual([row["ordinal"] for row in console["bindings"]["copies"][0]["copies"]], [1, 2])


if __name__ == "__main__":
    unittest.main()
