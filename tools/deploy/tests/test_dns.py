"""Managed DNS behavior against a bounded loopback provider, never Cloudflare."""
from dataclasses import asdict, replace
import copy
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch, Mock
import uuid

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "lib"))
import certificates
import contracts as c
import dns
from host import Host, Paths
import recover
import topology as t
import upgrades
from dns_provider_fixture import controlled_provider
from test_host_capabilities import declaration, profile, INSTANCE, OPERATION, CURRENT

ZONE = "a" * 32
OWNER = "d4707582-c805-49ba-a218-6eb84868150f"
TOKEN = "local-controlled-provider-token-only"


def managed_profile(address="203.0.113.10"):
    value = profile()
    value["instance_id"] = INSTANCE
    value["listeners"]["relay.ingress"]["dns"].update(mode="managed", zone_id=ZONE,
        public_addresses=[address], records=[{"ownership_id": OWNER, "address": address, "ttl": 300}])
    return t.parse_profile(value, t.parse_topology(declaration()))


class ManagedDNSTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.host = Host(Paths(Path(temporary.name)))
        self.profile = managed_profile()
        self.entry = {"name": "dns-test", "profile": self.profile.projection(), "certificate": {"zone_ref": ZONE}}
        self.host.paths.instance_config(self.entry["name"]).mkdir(parents=True, mode=0o700)
        directory = self.host.paths.p("/etc/foundry/cloudflare/zones")
        directory.mkdir(parents=True, mode=0o700)
        (directory / (ZONE + ".ini")).write_text("dns_cloudflare_api_token = " + TOKEN + "\n")
        (directory / (ZONE + ".ini")).chmod(0o600)
        self.provider = self.enterContext(controlled_provider())

    def new_plan(self, profile=None):
        operation = str(uuid.uuid4())
        return operation, dns.plan(self.host, self.entry, profile or self.profile, operation)

    def create(self):
        operation, plan = self.new_plan()
        dns.apply(self.host, self.entry, plan, operation)
        return next(iter(self.provider.records))

    def test_create_update_delete_and_rollback_keep_record_identity(self):
        record_id = self.create()
        for profile in (managed_profile("203.0.113.20"), self.profile):
            operation, plan = self.new_plan(profile)
            dns.apply(self.host, self.entry, plan, operation)
            self.assertEqual(set(self.provider.records), {record_id})
            self.assertEqual(self.provider.records[record_id]["content"], profile.listeners[0][1].public_addresses[0])
            self.assertFalse(self.provider.records[record_id]["proxied"])
        operation, plan = self.new_plan(replace(self.profile, listeners=()))
        dns.apply(self.host, self.entry, plan, operation)
        self.assertFalse(self.provider.records)
        self.assertFalse(dns.load(self.host, self.entry)["owned"])
        self.assertEqual([m for m, _ in self.provider.calls if m != "GET"], ["POST", "PATCH", "PATCH", "DELETE"])

    def test_current_provider_state_is_bound_and_rechecked(self):
        record_id = self.create()
        operation, plan = self.new_plan(managed_profile("203.0.113.20"))
        self.provider.put({**self.provider.records[record_id], "ttl": 600}, record_id)
        with self.assertRaisesRegex(c.DeployError, "Provider state changed"):
            dns.apply(self.host, self.entry, plan, operation)
        self.assertEqual(self.provider.records[record_id]["content"], "203.0.113.10")
        self.assertEqual(dns.report(self.host, self.entry, self.profile)[0]["state"], "drift")

    def test_foreign_record_is_never_adopted_even_with_matching_address(self):
        operation, plan = self.new_plan()
        foreign = dict(plan[0]["desired"], comment="foreign owner")
        record_id = self.provider.put(foreign)
        with self.assertRaises(c.DeployError):
            self.new_plan()
        with self.assertRaises(c.DeployError):
            dns.apply(self.host, self.entry, plan, operation)
        self.assertIn(record_id, self.provider.records)
        self.assertFalse(dns.load(self.host, self.entry)["owned"])
        self.assertFalse([m for m, _ in self.provider.calls if m != "GET"])

    def test_lost_create_ack_readback_and_replay_send_one_post(self):
        operation, plan = self.new_plan()
        self.provider.fault = "lost_after"
        dns.apply(self.host, self.entry, plan, operation)
        dns.apply(self.host, self.entry, plan, operation)
        self.assertEqual([m for m, _ in self.provider.calls].count("POST"), 1)
        self.assertEqual(len(dns.load(self.host, self.entry)["owned"]), 1)

    def test_unknown_create_absent_never_retries_or_adopts_later_foreign_record(self):
        operation, plan = self.new_plan()
        self.provider.fault = "lost_before"
        with self.assertRaises(c.DeployError):
            dns.apply(self.host, self.entry, plan, operation)
        with self.assertRaises(c.DeployError):
            dns.apply(self.host, self.entry, plan, operation)
        self.provider.put(dict(plan[0]["desired"], comment="foreign"))
        with self.assertRaises(c.DeployError):
            self.new_plan()
        self.assertEqual([m for m, _ in self.provider.calls].count("POST"), 1)
        self.assertEqual(dns.report(self.host, self.entry, self.profile)[-1]["state"], "reconciliation_required")

    def test_unknown_update_new_prior_and_third_state(self):
        self.create()
        operation, plan = self.new_plan(managed_profile("203.0.113.20"))
        self.provider.fault = "lost_after"
        dns.apply(self.host, self.entry, plan, operation)
        dns.apply(self.host, self.entry, plan, operation)
        operation, plan = self.new_plan(self.profile)
        self.provider.fault = "lost_before"
        with self.assertRaises(c.DeployError):
            dns.apply(self.host, self.entry, plan, operation)
        with self.assertRaises(c.DeployError):
            dns.apply(self.host, self.entry, plan, operation)
        state = dns.load(self.host, self.entry)
        self.assertEqual(state["operations"][operation]["results"][OWNER], "prior")
        new_operation, reviewed = self.new_plan(self.profile)
        self.assertNotEqual(operation, new_operation)
        self.provider.fault = "third_state"
        with self.assertRaises(c.DeployError):
            dns.apply(self.host, self.entry, reviewed, new_operation)
        with self.assertRaises(c.DeployError):
            self.new_plan()
        self.assertEqual([m for m, _ in self.provider.calls if m != "GET"], ["POST", "PATCH", "PATCH", "PATCH"])

    def test_lost_delete_ack_reconciles_absence(self):
        self.create()
        operation, plan = self.new_plan(replace(self.profile, listeners=()))
        self.provider.fault = "lost_after"
        dns.apply(self.host, self.entry, plan, operation)
        dns.apply(self.host, self.entry, plan, operation)
        self.assertFalse(self.provider.records)
        self.assertEqual([m for m, _ in self.provider.calls].count("DELETE"), 1)

    def test_rate_limit_and_errors_are_safe_and_not_retried(self):
        self.create()
        for fault in ("rate_limit", "reject"):
            operation, plan = self.new_plan(managed_profile("203.0.113.20"))
            self.provider.fault = fault
            with self.assertRaises(c.DeployError) as error:
                dns.apply(self.host, self.entry, plan, operation)
            self.assertNotIn(TOKEN, str(error.exception))
            self.assertNotIn("DO-NOT-PRINT", str(error.exception))
            self.assertEqual(dns.load(self.host, self.entry)["operations"][operation]["results"][OWNER], "prior")
        self.assertEqual([m for m, _ in self.provider.calls].count("PATCH"), 2)

    def test_ownership_mismatch_refuses_delete(self):
        record_id = self.create()
        self.provider.put(dict(self.provider.records[record_id], comment="foreign"), record_id)
        with self.assertRaises(c.DeployError):
            self.new_plan(replace(self.profile, listeners=()))
        self.assertIn(record_id, self.provider.records)

    def test_late_update_readback_is_visible_without_report_mutations(self):
        record_id = self.create()
        operation, plan = self.new_plan(managed_profile("203.0.113.20"))
        self.provider.fault = "lost_before"
        with self.assertRaises(c.DeployError):
            dns.apply(self.host, self.entry, plan, operation)
        path = dns.state_path(self.host, self.entry)
        before = path.read_bytes()
        self.provider.put({**self.provider.records[record_id], **plan[0]["desired"]}, record_id)
        report = dns.report(self.host, self.entry, self.profile)
        self.assertEqual(report[-1]["observed_results"][OWNER], "applied")
        self.assertEqual(path.read_bytes(), before)
        writes = [m for m, _ in self.provider.calls if m != "GET"]
        dns.apply(self.host, self.entry, plan, operation)
        self.assertEqual(writes, [m for m, _ in self.provider.calls if m != "GET"])

    def test_missing_owned_update_is_a_conflicting_third_state(self):
        record_id = self.create()
        operation, plan = self.new_plan(managed_profile("203.0.113.20"))
        self.provider.fault = "lost_before"
        with self.assertRaises(c.DeployError):
            dns.apply(self.host, self.entry, plan, operation)
        del self.provider.records[record_id]
        with self.assertRaises(c.DeployError):
            dns.apply(self.host, self.entry, plan, operation)
        self.assertEqual(dns.load(self.host, self.entry)["operations"][operation]["results"][OWNER], "conflict")

    def test_custody_and_zone_match(self):
        self.assertEqual(certificates.dns_token(self.host, self.entry, ZONE), TOKEN)
        with self.assertRaises(c.DeployError):
            certificates.dns_token(self.host, self.entry, "b" * 32)
        path = dns.state_path(self.host, self.entry)
        self.create()
        self.assertEqual(path.stat().st_mode & 0o777, 0o600)
        self.assertNotIn(TOKEN, path.read_text())

    def test_recovery_preserves_owned_id_and_requires_takeover_health(self):
        record_id = self.create()
        operation, changes = self.new_plan(managed_profile("203.0.113.20"))
        source = self.profile.projection()
        source["host"]["host_id"] = CURRENT
        target = copy.deepcopy(source)
        target["host"]["host_id"] = OPERATION
        metadata = {"profile": source, "host_id": CURRENT, "certificate": self.entry["certificate"],
                    "feature_recovery": {"deployment": {"source": "immutable"}}}
        listener = source["listeners"]["relay.ingress"]
        raw = {"schema": 1, "operation_id": operation, "source_instance_id": source["instance_id"],
               "source_host_id": CURRENT, "target_host_id": OPERATION,
               "deployment_sha256": c.canonical_digest(metadata["feature_recovery"]["deployment"]),
               "dns_cutover": "managed_pending", "observations": [], "listeners": [{"name": "relay.ingress",
                "hostname": listener["dns"]["hostname"], "transport": "tcp", "ports": listener["ports"],
                "dns_mode": "managed", "old_addresses": ["203.0.113.10"], "replacement_addresses": ["203.0.113.20"],
                "source_binding_sha256": c.canonical_digest(listener), "managed_records": changes}]}
        binding = recover.replacement_binding(raw, metadata, target, operation, "takeover")
        self.assertEqual(c.record_document(binding), raw)
        state = dns.load(self.host, self.entry)
        dns.recovery_review(binding, metadata, state, TOKEN)
        bad_state = copy.deepcopy(state)
        bad_state["owned"][OWNER]["prior"]["record_id"] = "f" * 32
        with self.assertRaises(c.DeployError):
            dns.recovery_review(binding, metadata, bad_state, TOKEN)
        effective = recover.effective_profile(target, binding)
        t.parse_profile(effective, t.parse_topology(declaration()))
        self.entry["profile"] = effective
        intent = {"operation_id": operation, "mode": "takeover", "recovery_public_binding": raw}
        for stopped, healthy in ((False, True), (True, False)):
            with self.assertRaises(c.DeployError):
                dns.recovery_apply(self.host, self.entry, intent, old_server_stopped=stopped, private_healthy=healthy)
        self.provider.fault = "lost_after"
        dns.recovery_apply(self.host, self.entry, intent, old_server_stopped=True, private_healthy=True)
        dns.recovery_apply(self.host, self.entry, intent, old_server_stopped=True, private_healthy=True)
        self.assertEqual(set(self.provider.records), {record_id})
        self.assertEqual(self.provider.records[record_id]["content"], "203.0.113.20")
        self.assertEqual([m for m, _ in self.provider.calls].count("PATCH"), 1)
        with self.assertRaises(c.DeployError):
            recover.replacement_binding(raw, metadata, target, operation, "rehearsal")


class DNSContractTests(unittest.TestCase):
    def test_profile_and_sticky_generation(self):
        parsed = managed_profile()
        topology = t.parse_topology(declaration())
        self.assertEqual(upgrades.feature_generation(topology, {}, profile=parsed.projection()), 20)
        with self.assertRaises(c.DeployError):
            upgrades.check_feature_generation({"tools": {"generation": 19}}, topology, {}, profile=parsed.projection())
        with self.assertRaises(c.DeployError):
            upgrades.check_feature_generation({"tools": {"generation": 20}}, topology, {}, profile=parsed.projection(), client_generation=19)
        self.assertEqual(upgrades.feature_generation(topology, {"deployment_selection": {"minimum_generation": 20}}), 20)
        for change in ({"proxied": True}, {"records": []}, {"zone_id": "../invalid"},
                       {"public_addresses": ["203.0.113.90"]}, {"mode": "external"}):
            value = parsed.projection()
            value["listeners"]["relay.ingress"]["dns"].update(change)
            with self.assertRaises(c.DeployError):
                t.parse_profile(value, topology)
        self.assertIn("lib/dns.py", upgrades.required_files(20))
        self.assertNotIn("lib/dns.py", upgrades.required_files(19))
        with self.assertRaises(c.DeployError):
            upgrades.check_instance_capabilities({"profile": parsed.projection()}, 19)
        with self.assertRaises(c.DeployError):
            upgrades._check_feature_request(None, {"profile": parsed.projection(), "client": {"generation": 19}}, "bootstrap-plan")

    def test_transport_response_bound_redirects_and_redaction(self):
        client = dns.Cloudflare(TOKEN)
        response = Mock()
        response.__enter__ = Mock(return_value=response)
        response.__exit__ = Mock(return_value=False)
        response.read.return_value = b"x" * (dns.MAX_RESPONSE + 1)
        with patch.object(client._opener, "open", return_value=response):
            with self.assertRaises(c.DeployError):
                client.records(ZONE, "relay.example.org")
        self.assertEqual(response.read.call_args.args, (dns.MAX_RESPONSE + 1,))
        self.assertIsNone(dns.NoRedirect().redirect_request(None, None, 302, "", {}, "https://foreign.invalid"))

    def test_missing_result_is_not_evidence_of_record_absence(self):
        client = dns.Cloudflare(TOKEN)
        for result in ({"success": True}, {"success": True, "result": None}):
            with patch.object(client, "request", return_value=(result, None)):
                with self.assertRaises(c.DeployError):
                    client.get(ZONE, "b" * 32)
        with patch.object(client, "request", return_value=(None, None)):
            self.assertIsNone(client.get(ZONE, "b" * 32))


from test_feature_wiring import FeatureWiringFixture


class DNSLifecycleTests(FeatureWiringFixture):
    def setUp(self):
        super().setUp()
        self.provider = self.enterContext(controlled_provider())
        self.enterContext(patch.object(certificates, "dns_token", return_value=TOKEN))

    def build_managed(self, address="203.0.113.10", version="20261114T000000Z-abcdef12", ttl=300):
        raw = managed_profile(address).projection()["listeners"]["relay.ingress"]["dns"]
        raw["records"][0]["ttl"] = ttl
        self.build_archive(public=True, managed_dns=raw, version=version)

    def bootstrap(self):
        from test_feature_wiring import FeatureWiringTests
        FeatureWiringTests.configure_firewall(self)
        plan = self.request("bootstrap-plan", topology=self.topology.projection())
        self.request("bootstrap-apply", topology=self.topology.projection(), plan_token=plan["plan_token"], confirm=True)

    def test_real_release_plan_activate_rollback_and_listener_removal(self):
        import feature_network
        import feature_lifecycle
        self.build_managed()
        self.bootstrap()
        opened = feature_network.open_public
        def open_after_dns(*args, **kwargs):
            self.assertTrue(self.provider.records)
            return opened(*args, **kwargs)
        with patch.object(feature_network, "open_public", side_effect=open_after_dns):
            self.request("activate", archive=str(self.archive), archive_sha256=c.digest(self.archive))
        record_id = next(iter(self.provider.records))
        first = self.version
        original = copy.deepcopy(self.profile)
        self.build_managed(version="20261115T000000Z-abcdef12", ttl=600)
        self.profile = original
        result = self.request("activate", archive=str(self.archive), archive_sha256=c.digest(self.archive))
        self.profile = result["profile_update"]
        self.assertEqual(self.provider.records[record_id]["ttl"], 600)
        operation = str(uuid.uuid4())
        review = self.request("release-plan", target=first, expected_current=self.version, operation_id=operation)
        result = self.request("rollback", target=first, expected_current=self.version, operation_id=operation, plan_token=review["plan_token"])
        self.profile = result["profile_update"]
        self.assertEqual(self.provider.records[record_id]["ttl"], 300)
        original = copy.deepcopy(self.profile)
        self.build_archive(version="20261115T010000Z-abcdef12")
        self.profile = original
        result = self.request("activate", archive=str(self.archive), archive_sha256=c.digest(self.archive))
        self.profile = result["profile_update"]
        self.assertFalse(self.provider.records)
        _, entry = self.host.require_identity(self.project, self.profile)
        self.assertEqual(feature_lifecycle.load(self.host, entry)[1].minimum_generation, 20)

    def test_provider_drift_refuses_before_closure(self):
        self.build_managed()
        self.bootstrap()
        operation = str(uuid.uuid4())
        request = {"archive": str(self.archive), "archive_sha256": c.digest(self.archive), "operation_id": operation}
        review = self.request("release-plan", **request)
        self.provider.put(dict(review["managed_dns"][0]["desired"], comment="foreign"))
        import feature_network
        with patch.object(feature_network, "withdraw") as withdraw:
            with self.assertRaises(c.DeployError):
                self.request("activate", **request, plan_token=review["plan_token"])
        withdraw.assert_not_called()

    def test_unknown_provider_outcome_never_opens_public_admission(self):
        self.build_managed()
        self.bootstrap()
        self.provider.fault = "lost_before"
        import feature_network
        with patch.object(feature_network, "open_public") as opened:
            with self.assertRaises(c.DeployError):
                self.request("activate", archive=str(self.archive), archive_sha256=c.digest(self.archive))
        opened.assert_not_called()
        self.assertFalse(self.host.running)


if __name__ == "__main__":
    unittest.main()
