"""Numbered-copy identities, materialization, and compatibility boundaries."""
import copy
from dataclasses import FrozenInstanceError, replace
from pathlib import Path
import sys
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "lib"))
from contracts import unit_selection_names, unit_selection_identities

import contracts as c
import listeners
import runtime_config
import services
import topology as t
from test_host_capabilities import CURRENT, INSTANCE, declaration, profile
from test_services import inventory_fixture
from timing_support import ManualClock, Checkpoints, readiness_clock


def copy_fixture(count=2):
    inventory, entry, _ = inventory_fixture()
    placed = replace(inventory.profile, placement=(("http", 1), ("relay", count), ("worker", 1)))
    return inventory, entry, placed


def numbered_inventory(count=3):
    original, entry, members = inventory_fixture()
    raw = original.profile.projection()
    raw["placement"]["relay"] = count
    placed = t.parse_profile(raw, original.topology)
    resolution = replace(original.resolution, endpoints=listeners.allocate_private(INSTANCE,
        listeners.private_listeners(original.topology, placed), (), ()))
    specs = []
    for identity, role in t.expand_placement(original.topology, INSTANCE, placed.placement).items():
        spec = replace(original.specs[original.identity(identity.role)], identity=identity)
        if isinstance(role, t.ExecutableRole):
            paths = services.ServicePaths.for_unit(identity, resolution.id)
            spec = replace(spec, arguments=tuple(services.resolve_argument(argument, role, spec, resolution, paths, (), {})
                                               for argument in role.arguments))
        specs.append(spec)
    deployment = replace(original.deployment, profile_sha256=c.canonical_digest(raw), units=tuple(specs))
    resolution = replace(resolution, deployment_sha256=c.canonical_digest(c.record_document(deployment)))
    return services.UnitInventory(entry["name"], original.topology, placed, deployment, resolution), entry, members


class CopyFoundationTests(unittest.TestCase):
    def setUp(self):
        self.clock = self.enterContext(readiness_clock(ManualClock()))

    def test_inventory_selection_uses_only_copy_identities(self):
        inventory, _, _ = copy_fixture()
        relay = inventory.identity("relay")
        self.assertEqual(inventory.copies("relay"), (relay,))
        self.assertEqual(inventory.select("relay", ordinal=1), (relay,))
        self.assertEqual(inventory.select(roles=["relay"]), (relay,))
        self.assertEqual(set(inventory.specs), set(inventory.units))
        self.assertEqual(unit_selection_identities(INSTANCE, unit_selection_names(inventory.select())), inventory.select())
        for role, ordinal in (("relay", 2), ("absent", 1), ("relay", True)):
            with self.subTest(role=role, ordinal=ordinal), self.assertRaises(c.DeployError):
                inventory.identity(role, ordinal)
        with self.assertRaises(c.DeployError):
            inventory.select(ordinal=1)
        numbered = c.UnitIdentity(INSTANCE, "relay", 2)
        self.assertEqual(unit_selection_names((numbered,)), ("relay@2",))
        self.assertEqual(unit_selection_identities(INSTANCE, ("relay@2",)), (numbered,))
        old_role = "relay"
        with self.assertRaises(KeyError):
            inventory.units[old_role]
        with self.assertRaises(c.DeployError):
            t.Argument.parse({"copy_ordinal": "self"})


    def test_copy_record_keys_are_unambiguous_and_preserve_singletons(self):
        for ordinal in (1, 2, 128):
            identity = c.UnitIdentity(INSTANCE, "relay", ordinal)
            self.assertEqual(c.UnitIdentity.from_record_key(INSTANCE, identity.record_key), identity)
        for value in ("relay@1", "relay@02", "relay@0", "relay@129", "relay@2@3", "relay@", "relay.2", None):
            with self.subTest(value=value), self.assertRaises(c.DeployError):
                c.UnitIdentity.from_record_key(INSTANCE, value)
        units = (c.UnitIdentity(INSTANCE, "relay", 1), c.UnitIdentity(INSTANCE, "relay", 2))
        self.assertEqual(services.role_names(units), ("relay",))
        self.assertEqual(unit_selection_names(units), ("relay", "relay@2"))

    def test_numbered_inventory_expands_dependencies_and_rendered_units(self):
        inventory, entry, _ = numbered_inventory()
        copies = inventory.copies("relay")
        worker = inventory.identity("worker")
        self.assertEqual([unit.ordinal for unit in copies], [1, 2, 3])
        self.assertTrue(set(copies) <= set(inventory.dependencies(worker)))
        content = services.render_unit(inventory, worker, entry, "a" * 64)
        for identity in copies:
            self.assertIn(inventory.units[identity], content)
            rendered = services.render_unit(inventory, identity, entry, "a" * 64)
            self.assertIn(services.ServicePaths.for_unit(identity, inventory.resolution.id).base, rendered)
            self.assertIn(f'"{identity.role}" "{identity.ordinal}"', rendered)
        starts, deadlines = [], []
        def start(identity, deadline):
            starts.extend(identity)
            deadlines.append(deadline)
        def ready(identity, deadline):
            deadlines.append(deadline)
            return True
        deadline = self.clock.monotonic() + 5
        inventory.start_in_order(inventory.units, deadline, start, ready)
        self.assertTrue(all(value == deadline for value in deadlines))
        self.assertGreater(starts.index(worker), max(starts.index(identity) for identity in copies))
        starts.clear()
        with self.assertRaises(c.DeployError):
            inventory.start_in_order(inventory.units, deadline, start, lambda identities, _: copies[1] not in identities)
        self.assertNotIn(worker, starts)

    def test_128_healthy_copies_share_the_readiness_budget(self):
        import feature_lifecycle as lifecycle
        import release
        import tempfile
        from host import Host, Paths
        inventory, entry, _ = numbered_inventory(128)
        began = self.clock.monotonic()
        deadline = began + release.READINESS_SECONDS
        started, probes, batches = set(), set(), []
        def start(argv, **kwargs):
            self.assertEqual(argv[:2], ["systemctl", "start"])
            self.assertEqual(argv[2], "--no-block")
            batches.append(tuple(argv[3:]))
            started.update(argv[3:])
        def observe(host, unit, **kwargs):
            self.assertIn(unit, started)
            return {"ActiveState": "active", "NRestarts": "0", "MainPID": str(list(inventory.units.values()).index(unit) + 1),
                    "process": {"start": "42"}, "ExecMainStartTimestampMonotonic": str(int(began * 1_000_000))}
        def probe(host, current, identity, deadline):
            probes.add(identity)
            return True
        wait = services.wait_ready_set
        def prove_group(host, current, names, until, **kwargs):
            names = tuple(names)
            checkpoints, counts = Checkpoints(), {}
            def observed_probe(host, current, identity, deadline):
                counts[identity] = counts.get(identity, 0) + 1
                if counts[identity] == 2:
                    checkpoints.hold(identity)
                return probe(host, current, identity, deadline)
            def stabilize():
                checkpoints.wait(*names)
                self.clock.advance(5)
            with patch.object(services, 'probe', side_effect=observed_probe), checkpoints.drive(self.clock, stabilize):
                return wait(host, current, names, until, **kwargs)
        with tempfile.TemporaryDirectory() as root:
            host = Host(Paths(Path(root).resolve()))
            for identity, spec in inventory.specs.items():
                executable = host.paths.p(spec.executable)
                executable.parent.mkdir(parents=True, exist_ok=True)
                executable.write_bytes(b"native")
                executable.chmod(0o755)
                inventory.specs[identity] = replace(spec, executable_sha256=c.digest(executable))
            with patch.object(lifecycle, "prepare_shared_directories"), \
                    patch.object(services, "enabled_units", return_value=tuple(inventory.units)), \
                    patch.object(host, "run", side_effect=start), \
                    patch.object(host, "read_registry", return_value={"instances": {}}), \
                    patch.object(release, "service", side_effect=observe), \
                    patch.object(release, "_matches_executable", return_value=True), \
                    patch.object(release, "STABILITY_SECONDS", 5), \
                    patch.object(services, "readiness_probe", return_value=None), \
                    patch.object(services, "wait_ready_set", side_effect=prove_group):
                lifecycle.start_units(host, entry, inventory, inventory.units, deadline)
                self.assertTrue(services.wait_ready_set(host, inventory, inventory.units, deadline))
        self.assertEqual(started, set(inventory.units.values()))
        self.assertEqual(probes, set(inventory.units))
        self.assertEqual([len(batch) for batch in batches], [1, 128, 1])
        self.assertEqual(self.clock.monotonic() - began, 20)
        self.assertLess(self.clock.monotonic(), deadline)

    def test_identity_is_frozen_ordered_and_bounded(self):
        for ordinal in (1, 2, 128):
            identity = c.UnitIdentity(INSTANCE, "relay", ordinal)
            self.assertEqual(c.UnitIdentity.parse({"instance_id": INSTANCE, "role": "relay", "ordinal": ordinal}), identity)
            self.assertEqual({identity: True}[identity], True)
        self.assertLess(c.UnitIdentity(INSTANCE, "relay", 2), c.UnitIdentity(INSTANCE, "relay", 10))
        with self.assertRaises(FrozenInstanceError):
            identity.ordinal = 3
        for ordinal in (0, 129, -1, True, 1.0, "1", None):
            with self.subTest(ordinal=ordinal), self.assertRaises(c.DeployError):
                c.UnitIdentity(INSTANCE, "relay", ordinal)

    def test_placement_limits_builtins_and_dense_expansion(self):
        inventory, _, placed = copy_fixture(128)
        units = t.expand_placement(inventory.topology, INSTANCE, placed.placement)
        self.assertEqual([u.ordinal for u in units if u.role == "relay"], list(range(1, 129)))
        self.assertEqual(len(units), 130)
        for count in (0, 1, 128):
            self.assertEqual(dict(t.parse_placement({"relay": count})), {"relay": count})
        for count in (-1, 129, True, 1.0, "2"):
            with self.subTest(count=count), self.assertRaises(c.DeployError):
                t.parse_placement({"relay": count})
        for counts in ({"http": 0, "relay": 1, "worker": 1},
                       {"http": 2, "relay": 1, "worker": 1},
                       {"http": 1, "relay": 1, "worker": 2},
                       {"http": 1, "relay": 0, "worker": 1},
                       {"http": 1, "relay": 1},
                       {"http": 1, "relay": 1, "worker": 1, "unknown": 0}):
            with self.subTest(counts=counts), self.assertRaises(c.DeployError):
                t.expand_placement(inventory.topology, INSTANCE, tuple(counts.items()))
        unplaced = t.expand_placement(inventory.topology, INSTANCE, (("http", 1), ("relay", 0), ("worker", 0)))
        self.assertEqual(tuple(unplaced), (c.UnitIdentity(INSTANCE, "http", 1),))

    def test_instance_unit_bound_is_256_and_deployments_are_dense(self):
        inventory, _, _ = copy_fixture()
        raw = inventory.topology.projection()
        raw["roles"].append({**copy.deepcopy(raw["roles"][1]), "name": "other"})
        topology = t.parse_topology(raw)
        counts = {"http": 1, "relay": 128, "other": 127, "worker": 0}
        units = t.expand_placement(topology, INSTANCE, t.parse_placement(counts))
        self.assertEqual(len(units), 256)
        specs = tuple(replace(inventory.specs[inventory.identity("http" if key.role == "http" else "relay")], identity=key,
                              bindings=c.RoleBindings(key.role, (), (), ())) for key in units)
        deployment = replace(inventory.deployment, units=specs)
        self.assertEqual(len(services.unit_specs(deployment)), 256)
        with self.assertRaises(c.DeployError):
            t.parse_placement({**counts, "other": 128})
        for changed in (specs + (replace(specs[-1], identity=c.UnitIdentity(INSTANCE, "other", 128)),),
                        tuple(s for s in specs if s.identity != c.UnitIdentity(INSTANCE, "relay", 2)),
                        specs + (specs[1],)):
            with self.subTest(size=len(changed)), self.assertRaises(c.DeployError):
                c.record_document(replace(deployment, units=changed))

    def test_role_selection_and_names_keep_surviving_ordinals(self):
        inventory, entry, placed = copy_fixture(3)
        names = services.expanded_unit_names(entry["name"], inventory.topology, placed)
        selected = services.select_role_units(names, "relay")
        self.assertEqual([key.ordinal for key in selected], [1, 2, 3])
        self.assertEqual(services.select_role_units(names, roles=["relay"]), selected)
        self.assertEqual(services.select_role_units(names), tuple(names))
        for key in selected:
            self.assertEqual(names[key], f"foundry-app-{entry['name']}-relay@{key.ordinal}.service")
        smaller = services.expanded_unit_names(entry["name"], inventory.topology,
                    replace(placed, placement=(("http", 1), ("relay", 2), ("worker", 1))))
        self.assertEqual({key: names[key] for key in smaller}, smaller)
        for selection in (["missing"], ["relay", "relay"]):
            with self.assertRaises(c.DeployError):
                services.select_role_units(names, roles=selection)

    def test_production_profiles_accept_copies_but_inventory_must_match(self):
        raw = profile()
        raw["placement"]["relay"] = 2
        raw["listeners"]["relay.ingress"]["ports"].append(9444)
        for parse in (lambda: c.validate_profile(raw), lambda: t.parse_profile(raw, t.parse_topology(declaration()))):
            parse()
        inventory, entry, _ = copy_fixture()
        extra = replace(inventory.specs[inventory.identity("relay")], identity=c.UnitIdentity(INSTANCE, "relay", 2))
        deployment = replace(inventory.deployment, units=inventory.deployment.units + (extra,))
        resolution = replace(inventory.resolution, deployment_sha256=c.canonical_digest(c.record_document(deployment)))
        with self.assertRaises(c.DeployError):
            services.UnitInventory(entry["name"], inventory.topology, inventory.profile, deployment, resolution)
        resolution = replace(inventory.resolution, endpoints=inventory.resolution.endpoints +
                             (c.AllocatedEndpoint("relay", "control", "127.0.0.1", 8202, 2),))
        with self.assertRaises(c.DeployError):
            services.UnitInventory(entry["name"], inventory.topology, inventory.profile, inventory.deployment, resolution)

    def test_allocations_are_per_copy_and_wildcards_reserve_every_copy(self):
        inventory, _, placed = copy_fixture()
        requested = listeners.private_listeners(inventory.topology, placed)
        endpoints = listeners.allocate_private(INSTANCE, requested, (), (), first=18000, last=18002,
                                                wildcards={("relay", "control")})
        self.assertEqual([(e.role, e.ordinal, e.port) for e in endpoints],
                         [("http", 1, 18000), ("relay", 1, 18001), ("relay", 2, 18002)])
        raw = inventory.topology.projection()
        raw["roles"][1]["network"] = {"confinement": "loopback"}
        raw["roles"][1]["listeners"]["control"]["wildcard"] = True
        claims = listeners.private_claims(INSTANCE, replace(inventory.resolution, endpoints=endpoints), t.parse_topology(raw))
        self.assertEqual([(claim.ordinal, claim.wildcard) for claim in claims if claim.role == "relay"], [(1, True), (2, True)])
        for claim in claims[1:]:
            self.assertEqual(listeners.reservation_socket(claim).address, "::")
        with self.assertRaises(c.DeployError):
            listeners.allocate_private(INSTANCE, requested, claims, (), first=18000, last=18002)

    def test_endpoint_ordinal_defaults_only_at_wire_boundary(self):
        wire = {"role": "relay", "listener": "control", "address": "127.0.0.1", "port": 18000}
        self.assertEqual(c.AllocatedEndpoint.parse(wire).ordinal, 1)
        with self.assertRaises(TypeError):
            c.AllocatedEndpoint("relay", "control", "127.0.0.1", 18000)
        for ordinal in (0, 129, True, None, "2"):
            with self.subTest(ordinal=ordinal), self.assertRaises(c.DeployError):
                c.AllocatedEndpoint.parse({**wire, "ordinal": ordinal})
        inventory, _, _ = copy_fixture()
        second = c.AllocatedEndpoint.parse({**wire, "ordinal": 2})
        resolution = replace(inventory.resolution, endpoints=inventory.resolution.endpoints + (second,))
        encoded = c.record_document(resolution)
        self.assertNotIn("ordinal", encoded["endpoints"][0])
        self.assertEqual(encoded["endpoints"][-1]["ordinal"], 2)
        self.assertEqual(c.RuntimeResolution.parse(encoded), resolution)
        with self.assertRaises(c.DeployError):
            c.record_document(replace(resolution, endpoints=resolution.endpoints + (replace(second, port=18001),)))

    def test_expanded_listener_records_are_not_limited_to_declaration_count(self):
        inventory, _, _ = copy_fixture()
        endpoints = (inventory.resolution.endpoints[0],) + tuple(
            c.AllocatedEndpoint("relay", name, "127.0.0.1", 18000 + 2 * ordinal + offset, ordinal)
            for ordinal in range(1, 129) for offset, name in enumerate(("control", "metrics")))
        resolution = replace(inventory.resolution, endpoints=endpoints)
        self.assertEqual(len(c.RuntimeResolution.parse(c.record_document(resolution)).endpoints), 257)
        from test_host_capabilities import closure
        evidence = c.ClosureEvidence.parse(closure())
        identities = (inventory.identity("http"),) + tuple(c.UnitIdentity(INSTANCE, "relay", n) for n in range(1, 129))
        old = tuple(replace(endpoint, port=endpoint.port + 1000) for endpoint in endpoints)
        evidence = replace(evidence, units=identities, endpoints=endpoints + old)
        self.assertEqual(len(c.ClosureEvidence.parse(c.record_document(evidence)).endpoints), 514)

    def test_unix_names_are_stable_bounded_and_collisions_are_rejected(self):
        inventory, _, placed = copy_fixture()
        raw = inventory.topology.projection()
        raw["roles"][1]["listeners"] = {"control": {"transport": "unix", "socket": "wl0"}}
        raw["roles"][1].update(readiness={"kind": "unix", "listener": "control"}, configs={}, arguments=[])
        topology = t.parse_topology(raw)
        endpoints = listeners.unix_endpoints(topology, placed)
        self.assertEqual([(e.name, e.ordinal) for e in endpoints], [("wl0", 1), ("wl0@2", 2)])
        self.assertEqual(c.UnixEndpoint.parse({key: value for key, value in vars(endpoints[0]).items() if key != "ordinal"}), endpoints[0])
        longest = c.UnixEndpoint.for_listener(INSTANCE, "relay", "control", "x" * 32, ordinal=128)
        self.assertEqual(c.socket_name(longest.name, ordinal=128), longest.name)
        self.assertEqual(len(longest.path.encode()), 103)
        for bad in ("wl0@2", "wl0@128", "wl0@1", "wl0@02", "wl0@2@3"):
            with self.subTest(name=bad), self.assertRaises(c.DeployError):
                c.socket_name(bad)
        for ordinal in range(1, 129):
            name = c.copy_socket_name("x" * 32, ordinal)
            self.assertEqual(c.socket_name(name, ordinal=ordinal), name)
        with self.assertRaises(c.DeployError):
            c.UnixEndpoint.parse({**c.record_document(longest), "path": longest.path + "x" * 6})
        self.assertLessEqual(len(longest.path.encode()), 107)
        other = {**copy.deepcopy(raw["roles"][1]), "name": "other"}
        other["listeners"]["control"]["socket"] = "wl0@2"
        raw["roles"].append(other)
        with self.assertRaisesRegex(c.DeployError, "@ is reserved"):
            t.expand_placement(t.parse_topology(raw), INSTANCE, placed.placement + (("other", 1),))

    def test_copy_references_paths_and_shared_grants(self):
        inventory, _, placed = copy_fixture()
        raw = inventory.topology.projection()
        raw["shared_directories"] = {"cache": {"state": "disposable", "max_bytes": 65536,
                                                "writers": ["relay"], "readers": ["worker"]}}
        raw["roles"][1]["arguments"] = [{"copy_number": "self"}]
        raw["roles"][1]["environment"] = {"COPY": {"copy_number": "self"}}
        topology = t.parse_topology(raw)
        import shared_directories
        for key, role in t.expand_placement(topology, INSTANCE, placed.placement).items():
            if key.role != "relay":
                continue
            spec = replace(inventory.specs[inventory.identity("relay")], identity=key)
            paths = services.ServicePaths.for_unit(key, CURRENT)
            for argument in (*role.arguments, *(value for _, value in role.environment)):
                self.assertEqual(services.resolve_argument(argument, role, spec, inventory.resolution, paths, (), {}), str(key.ordinal))
            rendered = runtime_config._render_service_json(b'{"copy": 0}', (("/copy", role.arguments[0]),),
                                                           lambda argument: str(key.ordinal))
            self.assertEqual(c.parse_json(rendered), {"copy": key.ordinal})
            self.assertTrue(paths.state.endswith("/relay" if key.ordinal == 1 else "/relay@2"))
            grants = shared_directories.bindings(topology, INSTANCE, key.role)
            self.assertEqual([(grant.name, grant.access) for grant in grants], [("cache", "read-write")])
        with self.assertRaises(c.DeployError):
            t.Argument.parse({"copy_number": "2"})

    def test_listener_arguments_resolve_the_selected_copy(self):
        inventory, _, placed = copy_fixture()
        role = inventory.roles["relay"]
        resolution = replace(inventory.resolution, endpoints=listeners.allocate_private(INSTANCE,
            listeners.private_listeners(inventory.topology, placed), (), ()))
        spec = replace(inventory.specs[inventory.identity("relay")], identity=c.UnitIdentity(INSTANCE, "relay", 2))
        paths = services.ServicePaths.for_unit(spec.identity, CURRENT)
        value = services.resolve_argument(t.Argument.parse({"listener_port": "control"}), role, spec, resolution, paths, (), {})
        self.assertEqual(value, str(next(e.port for e in resolution.endpoints if e.role == "relay" and e.ordinal == 2)))
