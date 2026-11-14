"""Public listener declarations, immutable records and runtime descriptor delivery."""
import copy
from dataclasses import FrozenInstanceError
import os
import unittest

from test_host_capabilities import declaration, profile
import contracts as c
import feature_lifecycle as lifecycle
import topology as t
import test_feature_wiring as wiring


class PublicListenerBindingTests(unittest.TestCase):
    def declaration(self):
        raw = declaration()
        raw["roles"][0]["bindings"] = {"public": {"public_listener": {"role": "relay", "listener": "ingress"}}}
        return raw

    def test_public_reference_is_typed_and_requires_a_public_placed_listener(self):
        topology = t.parse_topology(self.declaration())
        binding = dict(topology.roles[0].common.bindings)["public"]
        self.assertIsInstance(binding, t.PublicListenerReference)
        self.assertEqual((binding.role, binding.listener), ("relay", "ingress"))
        t.parse_profile(profile(), topology)
        for role, listener in (("relay", "control"), ("relay", "missing"), ("http", "http"), ("missing", "ingress")):
            raw = self.declaration()
            raw["roles"][0]["bindings"]["public"]["public_listener"].update(role=role, listener=listener)
            with self.subTest(role=role, listener=listener), self.assertRaises(c.DeployError):
                t.parse_topology(raw)
        raw = profile()
        raw["placement"].update(relay=0, worker=0)
        raw["listeners"] = {}
        with self.assertRaisesRegex(c.DeployError, "unplaced listener"):
            t.parse_profile(raw, topology)

    def test_record_has_no_implicit_scalar_conversion(self):
        raw = self.declaration()
        raw["roles"][1]["bindings"] = raw["roles"][0]["bindings"]
        raw["roles"][1]["arguments"].append({"binding": "public"})
        with self.assertRaisesRegex(c.DeployError, "runtime descriptor"):
            t.parse_topology(raw)

    def test_record_roundtrip_and_legacy_wire_shape(self):
        legacy = {"schema": 1, "role": "http", "executables": [], "endpoints": [], "bundles": []}
        self.assertEqual(c.record_document(c.RoleBindings.parse(legacy)), legacy)
        public = {"name": "public", "role": "relay", "listener": "ingress", "hostname": "relay.example.org",
                  "port": 8554, "tls": "none", "transport": "tcp", "public_addresses": ["203.0.113.10", "2001:db8::10"]}
        for mode in ("edge", "none"):
            wire = {**legacy, "public_listeners": [{**public, "tls": mode}]}
            bindings = c.RoleBindings.parse(wire)
            self.assertEqual(c.record_document(bindings), wire)
            with self.assertRaises(FrozenInstanceError):
                bindings.public_listeners[0].port = 1
        for key, value in (("port", 0), ("port", 65536), ("port", True), ("tls", "passthrough"),
                           ("transport", "udp"), ("hostname", "bad/host"), ("role", "../relay"),
                           ("public_addresses", []), ("public_addresses", ["127.0.0.1"]),
                           ("public_addresses", ["203.0.113.10", "203.0.113.10"])):
            with self.subTest(key=key, value=value), self.assertRaises(c.DeployError):
                c.RoleBindings.parse({**legacy, "public_listeners": [{**public, key: value}]})
        with self.assertRaisesRegex(c.DeployError, "unique"):
            c.RoleBindings.parse({**legacy, "public_listeners": [public, public]})
        with self.assertRaisesRegex(c.DeployError, "unique"):
            c.RoleBindings.parse({**legacy, "public_listeners": [public], "executables": [
                {"name": "public", "path": "/usr/bin/jq", "source_sha256": "a" * 64}]})


class PublicListenerDeliveryTests(wiring.FeatureWiringFixture):
    def test_descriptor_uses_profile_values_and_changed_binding_replaces_consumer(self):
        self.build_archive(public=True, public_binding=True, independent=True, maintenance="continue")
        wiring.FeatureWiringTests.configure_firewall(self)
        plan = self.request("bootstrap-plan", topology=self.topology.projection())
        self.request("bootstrap-apply", topology=self.topology.projection(), plan_token=plan["plan_token"], confirm=True)
        self.request("activate", archive=str(self.archive), archive_sha256=c.digest(self.archive))
        _, entry = self.host.require_identity(self.project, self.profile)
        _, _, before, launch = lifecycle.load(self.host, entry)
        public, = before.specs["http"].bindings.public_listeners
        self.assertEqual((public.hostname, public.port, public.tls, public.transport, public.public_addresses),
                         ("relay.example.org", 9443, "none", "tcp", ("203.0.113.10",)))
        descriptor = self.host.paths.opt(entry["name"]) / "runtime" / before.resolution.id / "http.json"
        wire = c.read_json(descriptor)
        self.assertEqual(wire["bindings"], c.record_document(before.specs["http"].bindings))
        self.assertEqual(descriptor.stat().st_mode & 0o777, 0o644)
        self.assertEqual(descriptor.stat().st_uid, os.geteuid())
        relay_descriptor = descriptor.with_name("relay.json")
        self.assertNotIn("public_listeners", c.read_json(relay_descriptor)["bindings"])
        changed = copy.deepcopy(self.profile)
        changed["listeners"]["relay.api"]["ports"] = [8554]
        changed["listeners"]["relay.api"]["dns"].update(hostname="new.example.org", public_addresses=["203.0.113.11"])
        declaration = c.ReleaseDeclaration.parse(launch["declaration"])
        release_path = self.host.paths.p(entry["allocations"]["paths"]["application"]) / "releases" / self.version
        manifest = c.read_json(release_path / "manifest.json")
        # Retained roles still need replacement when their profile-owned public binding changes.
        after, _ = lifecycle.resolve(self.host, entry, manifest, declaration, release_path, launch["artifact_roots"],
            "00000000-0000-4000-8000-000000000099", previous=before, affected=set(), profile=changed)
        updated, = after.specs["http"].bindings.public_listeners
        self.assertEqual((updated.hostname, updated.port, updated.public_addresses),
                         ("new.example.org", 8554, ("203.0.113.11",)))
        self.assertNotEqual(after.specs["http"], before.specs["http"])
        self.assertEqual(c.read_json(descriptor), wire)
        self.assertEqual(public.port, 9443)
