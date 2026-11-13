"""Lane A checkpoint 3897443a records and unedited owner modules, for tests only."""
from contextlib import contextmanager
from dataclasses import replace
import importlib.util
import json
from pathlib import Path
import sys
from unittest.mock import patch
import uuid

import acceptance_network as preview
from test_contracts import fixture
from test_host_capabilities import BOOT, OPERATION, DIGEST

c, t = preview.c, preview.t
ROOT = Path(__file__).parent / "network_fixtures/lane_a_3897443a"


@contextmanager
def owners():
    modules = {}
    for name in ("admission", "services", "feature_network"):
        spec = importlib.util.spec_from_file_location(name, ROOT / (name + ".py"))
        module = importlib.util.module_from_spec(spec)
        with patch.dict(sys.modules, {**modules, name: module}):
            spec.loader.exec_module(module)
        modules[name] = module
    with patch.dict(sys.modules, modules):
        yield modules["admission"], modules["services"], modules["feature_network"]


def attempt(outcome="running", phase="health"):
    return {"id": "20261113T000000Z-abcdef12", "action": "deploy", "release": "20261113T000000Z-abcdef12",
            "started_at": "2026-11-13T00:00:00Z", "finished_at": None if outcome == "running" else "2026-11-13T00:00:01Z",
            "outcome": outcome, "phase": phase, "error": None, "operation_id": OPERATION}


def build(host, mode="none", *, ports=(18000, 18002), instance_id=None, name="abc-production"):
    """Use the same launch, HostTransition and role->unit map as feature_lifecycle."""
    import admission
    import services
    instance_id = instance_id or str(uuid.uuid4())
    topology, profile = preview.preview_documents(mode, instance_id)
    _, _, entry = fixture()
    entry["name"] = name
    entry["profile"] = profile.projection()
    entry["configuration_revision"] = 1
    entry["topology"] = {**topology.projection(), "sha256": topology.sha256}
    entry["certificate"]["lineage"] = profile.projection()["domain"]
    inventories = []
    for port in ports:
        endpoints = (c.AllocatedEndpoint("http", "http", "127.0.0.1", port),
                     c.AllocatedEndpoint("relay", "ingress", "127.0.0.1", port + 1))
        specs = tuple(c.UnitSpec(c.UnitIdentity(instance_id, raw["name"], 1), "/usr/bin/python3", DIGEST,
                        ("--fixture-port", str(next(e.port for e in endpoints if e.role == raw["name"]))),
                        c.canonical_digest(raw), c.RoleBindings(raw["name"], (), (), ()), DIGEST)
                      for raw in topology.projection()["roles"])
        deployment = c.Deployment(instance_id, "20261113T000000Z-abcdef12", 2, topology.sha256,
                                  c.canonical_digest(profile.projection()), specs)
        resolution = preview.preview_resolution(topology, profile, endpoints)
        resolution = replace(resolution, deployment_sha256=c.canonical_digest(c.record_document(deployment)),
                             bindings_sha256=c.canonical_digest([c.record_document(s.bindings) for s in specs]),
                             readiness_sha256=c.canonical_digest([raw.get("readiness", raw["name"]) for raw in topology.projection()["roles"]]),
                             nginx_sha256=c.canonical_digest(preview.listeners._feature_projection(entry, topology, profile, endpoints)))
        inventory = services.UnitInventory(name, topology, profile, deployment, resolution)
        launch = {"schema": 1, "topology": topology.projection(), "profile": profile.projection(),
                  "deployment": c.record_document(deployment), "resolution": c.record_document(resolution),
                  "declaration": {}, "artifact_roots": {}, "secret_paths": {}, "descriptors": {}}
        host.owned_write(host.paths.opt(name) / "runtime" / resolution.id / "launch.json", json.dumps(launch), mode=0o640)
        inventories.append(inventory)
    old, current = inventories[0], inventories[-1]
    entry["allocations"]["http_port"] = old.resolution.endpoints[0].port
    selected = c.DeploymentSelection(instance_id, 12, tuple(sorted(c.FEATURE_CAPABILITIES)), current.resolution.id,
                    None if old.resolution.id == current.resolution.id else old.resolution.id,
                    c.AdmissionRecord(instance_id, current.resolution.id, current.resolution.deployment_sha256,
                                      1, OPERATION, c.AdmissionDisposition.PENDING), tuple(current.roles), ())
    transition = c.HostTransition(OPERATION, instance_id, old.resolution.id, current.resolution.id, 1,
                                 tuple(s.identity for s in current.specs.values()),
                                 c.canonical_digest(c.record_document(current.deployment)), None)
    store = admission.AdmissionStore(host, instance_id)
    store.record_operation(attempt())
    store.begin(selected, current.resolution)
    store._write(store.durable / "transition.json", transition)
    entry["deployment_selection"] = c.record_document(selected)
    return entry, old, current, transition, store
