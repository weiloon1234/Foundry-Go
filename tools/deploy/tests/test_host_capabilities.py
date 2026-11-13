"""B24 boundary fixtures: no systemd, network, host installation or subprocesses."""
import copy
from dataclasses import FrozenInstanceError
import json
from pathlib import Path
import sys
import unittest
import uuid
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "lib"))
import contracts as c
import topology as t
import upgrades
from test_contracts import fixture, registry

DIGEST = "a" * 64
OTHER = "b" * 64
INSTANCE, OPERATION, BOOT, OLD, CURRENT, CLOSURE, PERMIT = tuple(str(uuid.UUID(int=i, version=4)) for i in range(1, 8))
INVOCATION = "c" * 32


def declaration():
    return {"schema": 2, "roles": [
        {"name": "http", "kind": "builtin"},
        {"name": "relay", "kind": "executable", "executable": {"application": "relay-server"},
         "arguments": [{"literal": "--config"}, {"config": "settings"}],
         "configs": {"settings": {"source": "deploy/files/relay.json", "mode": "rendered-json",
                                  "bindings": {"/listen/address": {"listener_address": "control"},
                                               "/listen/port": {"listener_port": "control"}}}},
         "listeners": {"control": {"transport": "tcp", "bind": "loopback"},
                       "ingress": {"transport": "tcp", "bind": "public", "tls": "edge", "authentication": True}},
         "readiness": {"kind": "http", "listener": "control", "path": "/ready", "status": 200},
         "restart": {"policy": "on-failure", "delay_seconds": 5, "burst": 5},
         "limits": {"memory_bytes": 268435456, "cpu_percent": 100, "tasks": 64},
         "maintenance": "stop", "state": {"kind": "disposable"}},
        {"name": "worker", "kind": "builtin", "packages": ["coreutils"], "requires": ["http", "relay"],
         "bindings": {"checksum": {"executable": {"package": "coreutils", "path": "/usr/bin/sha256sum"}},
                      "relay_api": {"endpoint": {"role": "relay", "listener": "control", "scheme": "http"}}}}],
        "application_executables": {"relay-server": {"go_package": "./cmd/relay-server"}}}


def profile():
    base = fixture()[1]
    return {**base, "schema": 3, "placement": {"http": 1, "relay": 1, "worker": 1},
            "listeners": {"relay.ingress": {"ports": [9443], "source_cidrs": ["203.0.113.0/24"],
                "dns": {"mode": "external", "hostname": "relay.example.org", "public_addresses": ["203.0.113.10"], "proxied": False}}}}


def bundle():
    return {"version": "1.2.3", "format": "tar.gz",
            "platforms": {"linux-amd64": {"url": "https://downloads.example.org/bundle.tar.gz", "sha256": DIGEST},
                          "linux-arm64": {"url": "https://downloads.example.org/bundle-arm.tar.gz", "sha256": OTHER}},
            "bundles": {"plugin": "plugin"}, "modules": ["plugin/extension.so"],
            "provenance": {"source_revision": "release-1", "build_inputs": "toolchain-1", "license": "MIT", "service_abi": "relay-1"}}


def unit(role="http"):
    return {"instance_id": INSTANCE, "role": role, "ordinal": 1}


def resolution():
    return {"schema": 1, "id": CURRENT, "deployment_sha256": DIGEST, "private_revision": 7,
            "roles": ["http"], "endpoints": [{"role": "http", "listener": "http", "address": "127.0.0.1", "port": 8200}],
            "environments": [{"role": "http", "path": "runtime/" + CURRENT + "/http/app.env", "sha256": OTHER}],
            "bindings_sha256": DIGEST, "readiness_sha256": DIGEST, "nginx_sha256": DIGEST}


def admission():
    return {"schema": 1, "instance_id": INSTANCE, "resolution_id": CURRENT, "deployment_sha256": DIGEST,
            "private_revision": 7, "operation_id": OPERATION, "disposition": "pending"}


def selection(instance=INSTANCE):
    a = admission()
    a["instance_id"] = instance
    return {"schema": 1, "instance_id": instance, "minimum_generation": upgrades.protocol()["generation"],
            "required_capabilities": sorted(c.FEATURE_CAPABILITIES), "selected_resolution_id": CURRENT,
            "previous_resolution_id": OLD, "admission": a, "enabled_roles": ["http"], "maintenance_holds": []}


def closure():
    return {"schema": 1, "id": CLOSURE, "operation_id": OPERATION, "boot_id": BOOT,
            "old_resolution_id": OLD, "candidate_resolution_id": CURRENT, "units": [unit()],
            "endpoints": resolution()["endpoints"], "retained_units_sha256": DIGEST,
            "ingress_sha256": DIGEST, "backend_exit_sha256": DIGEST, "old_reference_isolation_sha256": DIGEST,
            "observed_at": "2026-11-13T00:00:00Z", "stage": "application_admission_closed"}


class CapabilityTopologyTests(unittest.TestCase):
    def test_secret_file_arguments_use_declared_environment_keys(self):
        raw = declaration()
        raw["secrets"] = {"token": {"prompt": "Service token"}}
        raw["roles"][1]["secret_files"] = {"TOKEN_FILE": "token"}
        raw["roles"][1]["arguments"] = [{"secret_file": "TOKEN_FILE"}]
        role = t.parse_topology(raw).roles[1]
        self.assertEqual(role.arguments, (t.Argument(t.ArgumentKind.SECRET_FILE, "TOKEN_FILE"),))
        self.assertEqual(dict(role.secret_files)[role.arguments[0].value], "token")
        for reference in ("token", "token_file", "TOKEN", "MISSING_FILE", "../TOKEN_FILE", 1):
            bad = copy.deepcopy(raw)
            bad["roles"][1]["arguments"] = [{"secret_file": reference}]
            with self.subTest(reference=reference), self.assertRaises(c.DeployError):
                t.parse_topology(bad)
        for key in ("token_file", "TOKEN"):
            bad = copy.deepcopy(raw)
            bad["roles"][1]["arguments"] = []
            bad["roles"][1]["secret_files"] = {key: "token"}
            with self.subTest(key=key), self.assertRaises(c.DeployError):
                t.parse_topology(bad)

    def test_non_http_builtin_can_be_unplaced_without_weakening_dependencies(self):
        raw = {"schema": 2, "roles": [{"name": "http", "kind": "builtin"},
                                      {"name": "worker", "kind": "builtin"}]}
        placed = {**profile(), "placement": {"http": 1, "worker": 0}, "listeners": {}}
        self.assertEqual(dict(t.parse_profile(placed, t.parse_topology(raw)).placement),
                         {"http": 1, "worker": 0})
        with self.assertRaises(c.DeployError):
            t.parse_profile({**placed, "placement": {"http": 0, "worker": 0}}, t.parse_topology(raw))
        raw["roles"].append({"name": "scheduler", "kind": "builtin", "requires": ["worker"]})
        placed["placement"]["scheduler"] = 1
        with self.assertRaises(c.DeployError):
            t.parse_profile(placed, t.parse_topology(raw))

    def test_legacy_reader_and_unit_map_are_unchanged(self):
        _, p, entry = fixture()
        raw = {"schema": 1, "roles": [{"name": "http"}, {"name": "worker"}]}
        before = copy.deepcopy(raw)
        result = t.parse_topology(raw)
        self.assertEqual(result.projection(), before)
        self.assertEqual(result.sha256, t.validate_topology(raw)["sha256"])
        self.assertEqual(dict(t.parse_profile(p, result).placement), {"http": 1, "worker": 1})
        self.assertEqual(t.instance_units(entry)["worker"], "foundry-worker@abc-production.service")
        raw["roles"].clear()
        self.assertEqual(len(result.roles), 2)

    def test_complete_worker_example_and_snapshots(self):
        raw = declaration()
        parsed = t.parse_topology(raw)
        worker = parsed.roles[2]
        self.assertIsInstance(worker, t.BuiltinRole)
        bindings = dict(worker.common.bindings)
        self.assertEqual(bindings["checksum"].executable.path, "/usr/bin/sha256sum")
        self.assertEqual(bindings["relay_api"], t.EndpointBinding("relay", "control", "http"))
        self.assertEqual(t.parse_profile(profile(), parsed).schema, 3)
        raw["roles"][2]["bindings"].clear()
        self.assertEqual(len(worker.common.bindings), 2)
        snapshot = parsed.projection()
        snapshot["roles"].clear()
        self.assertEqual(len(parsed.roles), 3)
        with self.assertRaises(FrozenInstanceError):
            parsed.schema = 9
        self.assertEqual(t.parse_topology({**parsed.projection(), "sha256": parsed.sha256}, registered=True), parsed)

    def test_feature_contracts_do_not_enable_incomplete_legacy_runtime(self):
        with self.assertRaises(c.DeployError) as caught:
            t.validate_topology(declaration())
        self.assertEqual(caught.exception.code, "capability_unavailable")
        with self.assertRaises(c.DeployError) as caught:
            c.validate_profile(profile())
        self.assertEqual(caught.exception.code, "capability_unavailable")
        self.assertEqual(upgrades.contract_versions()["archive"], (1, 2))

    def test_unknown_tags_dependencies_and_unsafe_references(self):
        variants = [
            lambda v: v.update(schema=True), lambda v: v.update(extra=True),
            lambda v: v["roles"].append(copy.deepcopy(v["roles"][0])),
            lambda v: v["roles"][1].update(name="http"),
            lambda v: v["roles"][1].update(requires=["worker"]),
            lambda v: v["roles"][1].update(requires=["missing"]),
            lambda v: v["roles"][1].update(executable={"application": "missing"}),
            lambda v: v["roles"][1].update(executable={"application": "relay-server", "package": "sh"}),
            lambda v: v["roles"][2].update(packages=[]),
            lambda v: v["roles"][2]["bindings"]["relay_api"]["endpoint"].update(listener="missing"),
            lambda v: v["roles"][1].update(arguments=[{"command": "sh -c true"}]),
            lambda v: v["roles"][1].update(arguments=[{"config": "missing"}]),
            lambda v: v["roles"][1]["configs"]["settings"].update(source="deploy/files/../private.env"),
            lambda v: v["roles"][1]["configs"]["settings"]["bindings"].update({"/listen": {"listener_port": "control"}}),
            lambda v: v["roles"][1].update(secret_files={"TOKEN": "token"}),
            lambda v: v["roles"][1].update(state={"kind": "local-unprotected"}),
            lambda v: v["roles"][1].update(readiness={"kind": "http", "listener": "control", "path": "https://remote/", "status": 200}),
            lambda v: v["roles"][1]["limits"].update(tasks=True),
        ]
        for mutate in variants:
            value = declaration()
            mutate(value)
            with self.subTest(mutation=variants.index(mutate)), self.assertRaises(c.DeployError):
                t.parse_topology(value)

    def test_duplicate_json_and_collection_limits(self):
        with self.assertRaises(c.DeployError):
            t.parse_topology('{"schema":2,"schema":1,"roles":[]}')
        value = declaration()
        value["roles"][1]["arguments"] = [{"literal": "x"}] * 129
        with self.assertRaises(c.DeployError):
            t.parse_topology(value)
        value = declaration()
        value["roles"][1]["arguments"] = [{"literal": "x" * 4096}] * 9
        with self.assertRaises(c.DeployError):
            t.parse_topology(value)

    def test_deferred_capabilities_are_explicit_rejections(self):
        for mutate in (lambda v: v["roles"][1].update(wrapper={}),
                       lambda v: v["roles"][1]["listeners"]["ingress"].update(transport="udp"),
                       lambda v: v["roles"][1]["listeners"]["ingress"].update(tls="native")):
            value = declaration()
            mutate(value)
            with self.assertRaises(c.DeployError) as caught:
                t.parse_topology(value)
            self.assertEqual(caught.exception.code, "unsupported_capability")
        for mutate in (lambda v: v["placement"].update(relay=2),
                       lambda v: v["listeners"]["relay.ingress"]["dns"].update(mode="managed")):
            value = profile()
            mutate(value)
            with self.assertRaises(c.DeployError) as caught:
                t.parse_profile(value, t.parse_topology(declaration()))
            self.assertEqual(caught.exception.code, "unsupported_capability")

    def test_profile_placement_and_binding_ownership(self):
        for mutate in (lambda v: v["placement"].update(http=0),
                       lambda v: v["placement"].update(relay=0),
                       lambda v: v["listeners"].clear(),
                       lambda v: v.update(executable="/bin/sh"),
                       lambda v: v["listeners"]["relay.ingress"].update(ports=[443]),
                       lambda v: v["listeners"]["relay.ingress"].update(source_cidrs=["203.0.113.5/24"]),
                       lambda v: v["listeners"]["relay.ingress"]["dns"].update(public_addresses=["127.0.0.1"])):
            value = profile()
            mutate(value)
            with self.assertRaises(c.DeployError):
                t.parse_profile(value, t.parse_topology(declaration()))

    def test_bundle_exports_and_service_local_mounts(self):
        value = declaration()
        value["artifacts"] = {"extension": bundle()}
        value["roles"][1]["artifact_mounts"] = {"extension": {"artifact": "extension", "bundle": "plugin", "area": "config", "path": "plugins/extension"}}
        parsed = t.parse_topology(value)
        self.assertEqual(dict(parsed.artifacts)["extension"].modules, ("plugin/extension.so",))
        for path in ("/usr/lib/plugins", "../plugins", "plugins/../../system", "settings"):
            bad = copy.deepcopy(value)
            bad["roles"][1]["artifact_mounts"]["extension"]["path"] = path
            with self.subTest(path=path), self.assertRaises(c.DeployError):
                t.parse_topology(bad)
        bad = copy.deepcopy(value)
        del bad["artifacts"]["extension"]["provenance"]
        with self.assertRaises(c.DeployError):
            t.parse_topology(bad)

    def test_builtin_bundle_binding_and_custom_environment(self):
        raw = declaration()
        raw["artifacts"] = {"extension": bundle()}
        mount = {"artifact": "extension", "bundle": "plugin", "area": "config", "path": "plugins/extension"}
        raw["roles"][2]["artifact_mounts"] = {"extension": mount}
        raw["roles"][2]["bindings"]["extension"] = {"bundle": "extension"}
        raw["roles"][1]["environment"] = {"XDG_CONFIG_HOME": {"config_root": "self"}}
        result = t.parse_topology(raw)
        self.assertEqual(dict(result.roles[2].common.bindings)["extension"], t.BundleDirectoryBinding("extension"))
        self.assertEqual(result.roles[1].environment[0][1].kind, t.ArgumentKind.CONFIG_ROOT)
        for key in ("PATH", "LD_PRELOAD", "TOKEN_FILE", "APP__HTTP__SERVER__ADDRESS"):
            bad = copy.deepcopy(raw)
            bad["roles"][1]["environment"][key] = {"literal": "unsafe"}
            with self.subTest(key=key), self.assertRaises(c.DeployError):
                t.parse_topology(bad)

    def test_artifact_origin_and_architecture_boundary(self):
        for url in ("http://downloads.example.org/a", "https://user:pass@example.org/a",
                    "https://example.org/a?token=x", "https://example.org/a#x", "https://127.0.0.1/a",
                    "https://[::ffff:127.0.0.1]/a", "https://169.254.169.254/a", "https://example.org:bad/a"):
            with self.subTest(url=url), self.assertRaises(c.DeployError):
                t.ArtifactPlatform.parse({"url": url, "sha256": DIGEST})
        value = declaration()
        value["artifacts"] = {"extension": bundle()}
        del value["artifacts"]["extension"]["platforms"]["linux-" + profile()["architecture"]]
        with self.assertRaises(c.DeployError):
            t.parse_profile(profile(), t.parse_topology(value))


class CapabilityRecordTests(unittest.TestCase):
    def test_selection_floor_cannot_predate_its_capabilities(self):
        for floor in range(12):
            with self.subTest(floor=floor), self.assertRaises(c.DeployError):
                c.DeploymentSelection.parse({**selection(), "minimum_generation": floor})
        for floor in (12, 13):
            self.assertEqual(c.DeploymentSelection.parse({**selection(), "minimum_generation": floor}).minimum_generation,
                             floor)

    def test_prebuilt_executable_export_matches_selected_architecture(self):
        raw = declaration()
        platforms = {"linux-amd64": "deploy/bin/relay-amd64", "linux-arm64": "deploy/bin/relay-arm64"}
        raw["application_executables"]["relay-server"] = {"platforms": platforms}
        parsed = t.parse_topology(raw)
        value = {"schema": 2, "topology": raw, "profile": profile(), "topology_sha256": parsed.sha256,
                 "inventory": [{"path": "deploy/files/relay.json", "sha256": DIGEST, "size": 42, "kind": "config"},
                               *[{"path": path, "sha256": OTHER, "size": 1024, "kind": "application"}
                                 for path in platforms.values()]],
                 "artifacts": [], "application_executables": {}}
        for architecture, other in (("amd64", "arm64"), ("arm64", "amd64")):
            value["profile"]["architecture"] = architecture
            expected = platforms["linux-" + architecture]
            value["application_executables"]["relay-server"] = expected
            self.assertEqual(dict(c.ReleaseDeclaration.parse(value).application_executables)["relay-server"], expected)
            value["application_executables"]["relay-server"] = platforms["linux-" + other]
            with self.subTest(architecture=architecture), self.assertRaises(c.DeployError):
                c.ReleaseDeclaration.parse(value)

    def test_resolution_preserves_private_revision_and_owns_environment(self):
        raw = resolution()
        target = c.RuntimeResolution.parse(raw)
        c.require_current_private_configuration(target, 7)
        with self.assertRaises(c.DeployError) as caught:
            c.require_current_private_configuration(target, 8)
        self.assertEqual(caught.exception.code, "private_revision_conflict")
        raw["endpoints"][0]["port"] = 9999
        self.assertEqual(target.endpoints[0].port, 8200)
        for mutate in (lambda v: v["environments"][0].update(path="runtime/" + OLD + "/app.env"),
                       lambda v: v["endpoints"].append(v["endpoints"][0]),
                       lambda v: v.update(private_revision=True),
                       lambda v: v.update(credentials={"password": "not-allowed"})):
            v = resolution()
            mutate(v)
            with self.assertRaises(c.DeployError):
                c.RuntimeResolution.parse(v)

    def test_enablement_is_independent_of_admission_and_legacy_rollback_is_sticky(self):
        value = selection()
        value["enabled_roles"] = []
        parsed = c.DeploymentSelection.parse(value)
        self.assertEqual(parsed.admission.disposition, c.AdmissionDisposition.PENDING)
        value["admission"]["disposition"] = "committed"
        self.assertEqual(c.DeploymentSelection.parse(value).enabled_roles, ())
        value["admission"]["resolution_id"] = OLD
        with self.assertRaises(c.DeployError):
            c.DeploymentSelection.parse(value)

    def test_ingress_withdrawal_is_not_a_validation_permit(self):
        permit = c.ValidationPermit.parse({"schema": 1, "id": PERMIT, "operation_id": OPERATION, "boot_id": BOOT,
            "resolution_id": CURRENT, "closure_id": CLOSURE, "executor": {"pid": 42, "start_ticks": 100, "invocation_id": INVOCATION},
            "units": [unit()]})
        permit.require_closure(c.ClosureEvidence.parse(closure()), c.RuntimeResolution.parse(resolution()))
        for mutate in (lambda v: v.update(stage="ingress_withdrawn", backend_exit_sha256=None, old_reference_isolation_sha256=None),
                       lambda v: v.update(boot_id=OLD), lambda v: v.update(candidate_resolution_id=OLD),
                       lambda v: v.update(endpoints=[]),
                       lambda v: v.update(units=[unit("worker")], endpoints=[])):
            value = closure()
            mutate(value)
            with self.assertRaises(c.DeployError):
                permit.require_closure(c.ClosureEvidence.parse(value), c.RuntimeResolution.parse(resolution()))
        value = closure()
        value["backend_exit_sha256"] = None
        with self.assertRaises(c.DeployError):
            c.ClosureEvidence.parse(value)

    def test_helper_pin_and_reference_retention(self):
        raw = {"schema": 1, "boot_id": BOOT, "unit": unit(),
               "process": {"pid": 42, "start_ticks": 100, "invocation_id": INVOCATION},
               "bundle_sha256": DIGEST, "unit_spec_sha256": OTHER, "resolution_id": CURRENT,
               "helper_format": 1, "cleanup_id": OPERATION}
        pin = c.HelperInvocation.parse(raw)
        raw["bundle_sha256"] = OTHER
        self.assertEqual(pin.bundle_sha256, DIGEST)
        refs = {"schema": 1, "bundle_sha256": DIGEST, "selected": False, "unit_generations": [],
                "invocations": [], "operations": [], "cleanup": [], "observations_complete": True}
        self.assertTrue(c.BundleReferences.parse(refs).deletable)
        for field, value in (("selected", True), ("unit_generations", [OTHER]), ("invocations", [INVOCATION]),
                             ("operations", [OPERATION]), ("cleanup", [OPERATION]), ("observations_complete", False)):
            self.assertFalse(c.BundleReferences.parse({**refs, field: value}).deletable)
        with self.assertRaises(c.DeployError):
            c.HelperInvocation.parse({**raw, "helper_format": 2})

    def test_replacement_address_binding_is_explicit_and_external_only(self):
        value = {"schema": 1, "operation_id": OPERATION, "source_instance_id": INSTANCE,
                 "source_host_id": OLD, "target_host_id": CURRENT, "deployment_sha256": DIGEST,
                 "dns_cutover": "external_pending", "observations": [], "listeners": [{"name": "relay.ingress", "hostname": "relay.example.org",
                    "transport": "tcp", "ports": [9443], "dns_mode": "external", "old_addresses": ["203.0.113.10"],
                    "replacement_addresses": ["203.0.113.20"], "source_binding_sha256": DIGEST}]}
        parsed = c.RecoveryPublicBinding.parse(value)
        self.assertEqual(parsed.listeners[0].replacement_addresses, ("203.0.113.20",))
        for mutate in (lambda v: v.update(dns_cutover="managed_complete"),
                       lambda v: v.update(dns_cutover="external_observed"),
                       lambda v: v["listeners"][0].update(replacement_addresses=["2001:db8::1"]),
                       lambda v: v["listeners"][0].update(executable="/bin/sh")):
            raw = copy.deepcopy(value)
            mutate(raw)
            with self.assertRaises(c.DeployError):
                c.RecoveryPublicBinding.parse(raw)

    def test_archive_declaration_inventories_and_schema_boundary(self):
        raw = declaration()
        parsed = t.parse_topology(raw)
        value = {"schema": 2, "topology": raw, "profile": profile(), "topology_sha256": parsed.sha256,
                 "inventory": [{"path": "deploy/files/relay.json", "sha256": DIGEST, "size": 42, "kind": "config"},
                               {"path": "deploy/bin/relay-server", "sha256": OTHER, "size": 1024, "kind": "application"}],
                 "artifacts": [], "application_executables": {"relay-server": "deploy/bin/relay-server"}}
        self.assertEqual(c.ReleaseDeclaration.parse(value).topology_sha256, parsed.sha256)
        self.assertEqual(c.ReleaseDeclaration.parse(value).projection(), value)
        for mutate in (lambda v: v.update(schema=1), lambda v: v.update(topology_sha256=OTHER),
                       lambda v: v.update(inventory=[]), lambda v: v["inventory"].append(v["inventory"][0])):
            bad = copy.deepcopy(value)
            mutate(bad)
            with self.assertRaises(c.DeployError):
                c.ReleaseDeclaration.parse(bad)

    def test_record_serialization_owns_nested_versions(self):
        for parser, raw in ((c.AdmissionRecord.parse, admission()), (c.RuntimeResolution.parse, resolution()),
                            (c.ClosureEvidence.parse, closure()), (c.DeploymentSelection.parse, selection())):
            record = parser(raw)
            wire = c.record_document(record)
            self.assertEqual(parser(wire), record)
            self.assertEqual(wire, raw)
        bindings = {"schema": 1, "role": "http", "executables": [], "endpoints": [], "bundles": []}
        spec = {"schema": 1, "identity": unit(), "executable": "/opt/foundry/example/releases/current/artisan",
                "executable_sha256": DIGEST, "arguments": ["", "serve"], "declaration_sha256": DIGEST,
                "bindings": bindings, "configuration_sha256": OTHER}
        deployment = {"schema": 1, "instance_id": INSTANCE, "release": "20261113T000000Z-abcdef12",
                      "archive_schema": 1, "topology_sha256": DIGEST, "profile_sha256": OTHER, "units": [spec]}
        self.assertEqual(c.record_document(c.Deployment.parse(deployment)), deployment)
        transition = {"schema": 1, "operation_id": OPERATION, "instance_id": INSTANCE, "old_resolution_id": OLD,
                      "candidate_resolution_id": CURRENT, "current_private_revision": 7,
                      "affected_units": [unit()], "plan_sha256": DIGEST, "closure_id": None}
        self.assertEqual(c.record_document(c.HostTransition.parse(transition)), transition)
        generation = {"schema": 1, "id": DIGEST, "unit": unit(), "bundle_sha256": OTHER,
                      "unit_spec_sha256": DIGEST, "resolution_id": CURRENT, "state": "retiring",
                      "queued_jobs": [4], "control_processes": [], "observations_complete": False}
        self.assertEqual(c.record_document(c.UnitGenerationReference.parse(generation)), generation)
        cleanup = {"schema": 1, "id": CLOSURE, "operation_id": OPERATION, "instance_id": INSTANCE,
                   "resolution_id": CURRENT, "bundle_sha256": DIGEST, "helper_format": 1, "state": "pending"}
        self.assertEqual(c.record_document(c.CleanupObligation.parse(cleanup)), cleanup)

    def test_records_reject_unknown_fields_and_bool_versions(self):
        for parser, raw in ((c.AdmissionRecord.parse, admission()), (c.RuntimeResolution.parse, resolution()),
                            (c.ClosureEvidence.parse, closure()), (c.DeploymentSelection.parse, selection())):
            for change in ({"extra": True}, {"schema": True}, {"schema": 2}):
                with self.subTest(parser=parser, change=change), self.assertRaises(c.DeployError):
                    parser({**raw, **change})


class CapabilityCompatibilityTests(unittest.TestCase):
    def test_feature_instance_rejection_does_not_block_legacy_neighbor(self):
        _, _, legacy = fixture()
        _, _, feature = fixture(project_slug="feature", host_id=legacy["host_id"], port=8101)
        feature["deployment_selection"] = selection(feature["profile"]["instance_id"])
        mixed = registry(legacy, feature)
        mixed["tools"]["generation"] = upgrades.protocol()["generation"]
        c.validate_registry(mixed)
        for generation in (1, 9, 10, 11):
            upgrades.check_instance_capabilities(legacy, generation)
            with self.assertRaises(c.DeployError) as caught:
                upgrades.check_instance_capabilities(feature, generation)
            self.assertEqual(caught.exception.code, "instance_capability_required")
        for action in ("status", "restart", "config-set", "rollback"):
            with patch("upgrades.protocol", return_value={"protocol": {"major": 2, "minor": 0}, "generation": 99,
                                                       "minimum_client_generation": 1}):
                request = {"profile": legacy["profile"], "client": {"generation": 9}, "protocol": {"major": 2, "minor": 0}}
                upgrades._check_feature_request(mixed, request, action)
                request["profile"] = feature["profile"]
                with self.assertRaises(c.DeployError):
                    upgrades._check_feature_request(mixed, request, action)
        # A capable client also cannot activate partial M1 runtime support.
        with self.assertRaises(c.DeployError) as caught:
            upgrades.check_instance_capabilities(feature, upgrades.protocol()["generation"])
        self.assertEqual(caught.exception.code, "capability_unavailable")

    def test_source_protocol_fixtures_preserve_upgrade_floors(self):
        # Captured 2.0 envelope shape of all supported source generations. These
        # are contract fixtures; installed source-bundle acceptance belongs to M6.
        for source_generation in (9, 10, 11):
            source = {"protocol": {"major": 2, "minor": 0}, "generation": source_generation,
                      "minimum_host_generation": 9, "minimum_client_generation": 1, "framework_version": "development"}
            upgrades.check_upgrade_generation(source)
            candidate = upgrades.protocol()
            self.assertEqual(set(source), set(candidate))
            self.assertGreater(candidate["generation"], source["generation"])
            self.assertEqual(candidate["minimum_client_generation"], 1)
        with self.assertRaises(c.DeployError):
            upgrades.check_upgrade_generation({"generation": 8})
