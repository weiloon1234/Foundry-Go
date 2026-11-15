"""Managed DNS behavior against a bounded loopback provider, never Cloudflare."""
from dataclasses import asdict, replace
import copy
import io
import json
from pathlib import Path
import signal
import subprocess
import sys
import tempfile
import time
import unittest
from unittest.mock import patch, Mock
import uuid
from urllib.error import HTTPError

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

    def test_ownership_comment_fits_free_plan_and_binds_full_identity(self):
        operation, plan = self.new_plan()
        change = dns.Change.parse(plan[0])
        self.assertRegex(change.comment, r"^foundry:v1:[a-f0-9]{64}$")
        self.assertLessEqual(len(change.comment), 100)
        self.assertEqual(change.comment, dns.Change.parse(asdict(change)).comment)
        for field in ("instance_id", "ownership_id", "create_operation_id"):
            self.assertNotEqual(change.comment, replace(change, **{field: str(uuid.uuid4())}).comment)
        with patch.object(dns.Change, "comment", new=property(lambda self: "x" * 101)):
            with self.assertRaises(c.DeployError):
                self.new_plan()

    def test_fake_rejects_live_failure_payload_and_invalid_names(self):
        _, plan = self.new_plan()
        body = {**plan[0]["desired"], "name": "a" * 32 + ".foundry-dns-test." + "b" * 12 + ".org",
                "comment": f"foundry:{INSTANCE}:{OWNER}:{OPERATION}"}
        self.assertEqual((len(body["name"]), len(body["comment"])), (66, 118))
        client = dns.Cloudflare(TOKEN)
        for invalid in (body, {**body, "comment": "valid", "name": "a" * 64 + ".example.org"},
                        {**body, "comment": "valid", "name": "bad..example.org"},
                        {**body, "comment": "valid", "name": "-bad.example.org"},
                        {**body, "comment": "valid", "name": ".".join(["a" * 63] * 4)}):
            with self.subTest(name=invalid["name"]):
                with self.assertRaises(c.DeployError) as failure:
                    client.request("POST", ZONE, body=invalid)
                self.assertEqual(failure.exception.code, "dns_provider_rejected" if invalid is body else "dns_provider_error")
                self.assertFalse(self.provider.records)

    def test_definite_create_rejection_preserves_diagnostics_without_reconciliation(self):
        operation, plan = self.new_plan()
        self.provider.fault = (400, {"success": False, "errors": [
            {"code": 9313, "message": "DNS record comment exceeds the maximum length of 100 characters."}]})
        with self.assertRaises(c.DeployError) as failure:
            dns.apply(self.host, self.entry, plan, operation, track_public_admission=True)
        self.assertEqual(failure.exception.code, "dns_provider_rejected")
        self.assertIn("9313", str(failure.exception))
        self.assertIn("comment exceeds", str(failure.exception))
        self.assertFalse(self.provider.records)
        calls = list(self.provider.calls)
        self.assertEqual(calls[-1][0], "POST")
        saved = dns.load(self.host, self.entry)["operations"][operation]
        self.assertEqual(saved["results"][OWNER], "rejected")
        self.assertEqual(saved["public_admission"], "failed")
        with self.assertRaises(c.DeployError) as replay:
            dns.apply(self.host, self.entry, plan, operation, track_public_admission=True)
        self.assertEqual(replay.exception.code, "dns_provider_rejected")
        self.assertIn("9313", str(replay.exception))
        self.assertEqual(self.provider.calls, calls)
        self.assertEqual(dns.report(self.host, self.entry, self.profile)[-1]["state"], "provider_rejected")
        next_operation, reviewed = self.new_plan()
        dns.apply(self.host, self.entry, reviewed, next_operation)
        self.assertEqual(len(self.provider.records), 1)

    def test_owned_record_rename_refuses_deletion(self):
        record_id = self.create()
        self.provider.put({**self.provider.records[record_id], "name": "renamed.example.org"}, record_id)
        with self.assertRaises(c.DeployError):
            self.new_plan(replace(self.profile, listeners=()))

    def test_rejected_update_and_delete_preserve_owned_record_and_do_not_read_back(self):
        record_id = self.create()
        original = copy.deepcopy(self.provider.records[record_id])
        for profile in (managed_profile("203.0.113.20"), replace(self.profile, listeners=())):
            with self.subTest(removing=not profile.listeners):
                operation, plan = self.new_plan(profile)
                self.provider.fault = "reject"
                with self.assertRaises(c.DeployError) as failure:
                    dns.apply(self.host, self.entry, plan, operation)
                self.assertEqual(failure.exception.code, "dns_provider_rejected")
                self.assertIn("9005", failure.exception.detail)
                self.assertEqual(self.provider.records[record_id], original)
                self.assertIn(self.provider.calls[-1][0], ("PATCH", "DELETE"))
                self.assertEqual(dns.load(self.host, self.entry)["owned"][OWNER]["prior"]["record_id"], record_id)

    def test_uncertain_http_errors_reconcile_once_without_retry_and_keep_codes(self):
        self.create()
        for status, envelope in [(status, {"success": False, "errors": [{"code": 1001, "message": "Request failed"}]})
                                 for status in (408, 409, 429, 500, 503)] + [
                (400, {"success": False}), (400, {"success": True, "errors": [{"code": 1001}]}),
                (200, {"success": False, "errors": [{"code": 1001, "message": "Request failed"}]})]:
            with self.subTest(status=status, envelope=envelope):
                operation, plan = self.new_plan(managed_profile("203.0.113.20"))
                self.provider.fault = status, envelope
                writes = sum(method == "PATCH" for method, _ in self.provider.calls)
                with self.assertRaises(c.DeployError) as failure:
                    dns.apply(self.host, self.entry, plan, operation)
                self.assertEqual(failure.exception.code, "dns_reconciliation")
                if "errors" in envelope:
                    self.assertIn("1001", failure.exception.detail)
                self.assertEqual(self.provider.calls[-1][0], "GET")
                self.assertEqual(dns.load(self.host, self.entry)["operations"][operation]["results"][OWNER], "prior")
                with self.assertRaises(c.DeployError):
                    dns.apply(self.host, self.entry, plan, operation)
                self.assertEqual(sum(method == "PATCH" for method, _ in self.provider.calls), writes + 1)

    def test_diagnostics_contain_only_codes_and_framework_descriptions(self):
        for status in (400, 429):
            with self.subTest(status=status):
                # Updates permit a fresh reviewed operation after the uncertain 429.
                if not self.provider.records:
                    self.create()
                operation, plan = self.new_plan(managed_profile("203.0.113.20"))
                message = f"private.example.org private-account Invalid\nfield\x1b[31m {TOKEN} {ZONE.upper()} " + "x" * 300
                self.provider.fault = status, {"success": False, "errors": [
                    {"code": code, "message": message} for code in (9313, 10000, 1001, 9999)],
                    "private": "DO-NOT-PRINT-RESPONSE"}
                with self.assertRaises(c.DeployError) as failure:
                    dns.apply(self.host, self.entry, plan, operation)
                detail = failure.exception.detail
                for code in (9313, 10000, 1001):
                    self.assertIn(str(code), detail)
                for hidden in (TOKEN, ZONE, ZONE.upper(), "9999", "DO-NOT-PRINT", "private.example.org", "private-account", "x" * 161):
                    self.assertNotIn(hidden, detail)
                self.assertTrue(all(32 <= ord(char) <= 126 for char in detail))
                self.assertLess(len(detail), 1024)

    def test_only_complete_allowlisted_validation_errors_are_definite(self):
        self.create()
        cases = [(400, [{"code": code}], True) for code in (9005, 9313)]
        cases += [(400, [{"code": code}], False) for code in (10000, 9000, 9006, 9999)]
        cases += [(status, [{"code": 9313}], False) for status in (408, 409, 429, 500, 503)]
        cases += [(400, errors, False) for errors in (
            [{"code": 9313}] * 3 + [{"code": 10000}],
            [{"code": 9313}, {"message": "missing code"}],
            [{"code": 9313, "error_chain": [{"code": 10000}]}],
            [{"code": True}], [{"code": "9313"}])]
        for status, errors, definite in cases:
            with self.subTest(status=status, errors=errors):
                operation, plan = self.new_plan(managed_profile("203.0.113.20"))
                self.provider.fault = status, {"success": False, "errors": errors}
                with self.assertRaises(c.DeployError) as failure:
                    dns.apply(self.host, self.entry, plan, operation)
                self.assertEqual(failure.exception.code, "dns_provider_rejected" if definite else "dns_reconciliation")
                saved = dns.load(self.host, self.entry)["operations"][operation]
                self.assertEqual(saved["results"][OWNER], "rejected" if definite else "prior")

    def test_rejected_batch_marks_remaining_records_not_attempted(self):
        binding = self.profile.listeners[0][1]
        extra = replace(binding.records[0], ownership_id="ffffffff-ffff-4fff-8fff-ffffffffffff", address="203.0.113.11")
        profile = replace(self.profile, listeners=(("relay.ingress", replace(binding,
            public_addresses=(*binding.public_addresses, extra.address), records=(*binding.records, extra))),))
        operation, plan = self.new_plan(profile)
        self.provider.fault = "reject"
        with self.assertRaises(c.DeployError):
            dns.apply(self.host, self.entry, plan, operation, track_public_admission=True)
        state = dns.load(self.host, self.entry)
        item = state["operations"][operation]
        self.assertEqual(item["results"], {OWNER: "rejected", extra.ownership_id: "not_attempted"})
        self.assertEqual(dns.report(self.host, self.entry, profile)[-1]["observed_results"][extra.ownership_id], "not_attempted")
        item["results"][extra.ownership_id] = "pending"
        with self.assertRaises(c.DeployError):
            dns.parse_state(state, profile.instance_id)

    def partial_rejection(self):
        binding = self.profile.listeners[0][1]
        extra = replace(binding.records[0], ownership_id=str(uuid.uuid4()), address="203.0.113.11")
        profile = replace(self.profile, listeners=(("relay.ingress", replace(binding,
            public_addresses=(*binding.public_addresses, extra.address), records=(*binding.records, extra))),))
        operation, plan = self.new_plan(profile)
        write = dns.Cloudflare.write
        calls = []

        def reject_second(client, change):
            calls.append(change.ownership_id)
            if len(calls) == 2:
                self.provider.fault = (400, {"success": False, "errors": [{"code": 9313, "message": "private.example.org"}]})
            return write(client, change)

        with patch.object(dns.Cloudflare, "write", reject_second):
            with self.assertRaises(c.DeployError) as failure:
                dns.apply(self.host, self.entry, plan, operation, track_public_admission=True)
        self.assertEqual(failure.exception.code, "dns_provider_rejected")
        saved = dns.load(self.host, self.entry)["operations"][operation]
        self.assertEqual(saved["results"], {calls[0]: "applied", calls[1]: "rejected"})
        self.assertEqual(saved["public_admission"], "failed")
        self.assertEqual(len(self.provider.records), 1)
        with self.assertRaises(c.DeployError):
            dns.commit_public_admission(self.host, self.entry, operation)
        return profile, operation, calls

    def test_partial_rejection_report_is_resolved_and_contains_no_provider_text(self):
        profile, operation, calls = self.partial_rejection()
        report = dns.report(self.host, self.entry, profile)[-1]
        self.assertEqual(report["state"], "provider_rejected")
        self.assertEqual(report["rejections"][calls[1]], [9313])
        self.assertNotIn("private.example.org", dns.state_path(self.host, self.entry).read_text())

    def test_partial_rejection_corrective_plan_succeeds_from_actual_state(self):
        profile, _, calls = self.partial_rejection()
        record_id = next(iter(self.provider.records))
        operation, plan = self.new_plan(profile)
        by_owner = {change["ownership_id"]: change for change in plan}
        self.assertEqual(by_owner[calls[0]]["prior"]["record_id"], record_id)
        self.assertIsNone(by_owner[calls[1]]["prior"])
        dns.apply(self.host, self.entry, plan, operation, track_public_admission=True)
        dns.commit_public_admission(self.host, self.entry, operation)
        self.assertEqual(len(self.provider.records), 2)
        self.assertIn(record_id, self.provider.records)

    def test_partial_rejection_rollback_deletes_only_applied_owned_record(self):
        self.partial_rejection()
        operation, plan = self.new_plan(replace(self.profile, listeners=()))
        self.assertEqual(len(plan), 1)
        dns.apply(self.host, self.entry, plan, operation, track_public_admission=True)
        dns.commit_public_admission(self.host, self.entry, operation)
        self.assertFalse(self.provider.records)

    def test_corrective_plan_after_partial_rejection_still_refuses_drift(self):
        profile, _, _ = self.partial_rejection()
        operation, plan = self.new_plan(profile)
        record_id = next(iter(self.provider.records))
        self.provider.put({**self.provider.records[record_id], "ttl": 600}, record_id)
        with self.assertRaises(c.DeployError) as failure:
            dns.apply(self.host, self.entry, plan, operation, track_public_admission=True)
        self.assertEqual(failure.exception.code, "dns_drift")

    def test_correction_with_no_managed_records_can_finish_rejected_admission(self):
        operation, plan = self.new_plan()
        self.provider.fault = "reject"
        with self.assertRaises(c.DeployError):
            dns.apply(self.host, self.entry, plan, operation, track_public_admission=True)
        operation, plan = self.new_plan(replace(self.profile, listeners=()))
        self.assertFalse(plan)
        dns.apply(self.host, self.entry, plan, operation, track_public_admission=True)
        dns.commit_public_admission(self.host, self.entry, operation)
        self.assertEqual(dns.load(self.host, self.entry)["operations"][operation]["public_admission"], "committed")

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
            self.assertEqual(error.exception.code, "dns_provider_rejected" if fault == "reject" else "dns_reconciliation")
            self.assertIn("9005" if fault == "reject" else "1015", error.exception.detail)
            self.assertEqual(dns.load(self.host, self.entry)["operations"][operation]["results"][OWNER],
                             "rejected" if fault == "reject" else "prior")
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

    def test_total_dns_budget_preserves_unknown_write_and_pending_admission(self):
        operation, planned = self.new_plan()
        write = dns.Cloudflare.write

        def delayed_ack(client, change):
            write(client, change)
            time.sleep(2)

        with patch.object(dns, "APPLY_SECONDS", 0.5), patch.object(dns.Cloudflare, "write", delayed_ack):
            with self.assertRaises(c.DeployError) as failure:
                dns.apply(self.host, self.entry, planned, operation, track_public_admission=True)
        self.assertEqual(failure.exception.code, "dns_budget_exhausted")
        saved = dns.load(self.host, self.entry)["operations"][operation]
        self.assertEqual(saved["results"][OWNER], "pending")
        self.assertEqual(saved["public_admission"], "pending")
        with self.assertRaises(c.DeployError):
            dns.commit_public_admission(self.host, self.entry, operation)
        invalid = dns.load(self.host, self.entry)
        invalid["operations"][operation]["public_admission"] = "committed"
        with self.assertRaises(c.DeployError):
            dns.parse_state(invalid, self.profile.instance_id)
        self.assertEqual(signal.getitimer(signal.ITIMER_REAL)[0], 0)
        observed = dns.report(self.host, self.entry, self.profile)[-1]
        self.assertEqual(observed["state"], "public_admission_pending")
        self.assertEqual(observed["observed_results"][OWNER], "applied")
        dns.apply(self.host, self.entry, planned, operation, track_public_admission=True)
        self.assertEqual(dns.load(self.host, self.entry)["operations"][operation]["public_admission"], "pending")
        self.assertEqual([method for method, _ in self.provider.calls].count("POST"), 1)

    def test_retained_records_report_drift_absence_and_failed_observation(self):
        record_id = self.create()
        removed = replace(self.profile, listeners=())
        self.provider.put({**self.provider.records[record_id], "ttl": 600}, record_id)
        self.assertEqual(dns.report(self.host, self.entry, removed)[0]["provider_state"], "drift")
        with patch.object(dns, "provider", side_effect=c.DeployError("dns_provider_unavailable", "Unavailable")):
            value = dns.report(self.host, self.entry, removed)[0]
            self.assertEqual(value["state"], "retained_record")
            self.assertEqual(value["provider_state"], "unavailable")
        del self.provider.records[record_id]
        self.assertEqual(dns.report(self.host, self.entry, removed)[0]["provider_state"], "absent")

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
        self.assertEqual(dns.load(self.host, self.entry)["operations"][operation]["public_admission"], "pending")
        self.assertEqual(set(self.provider.records), {record_id})
        self.assertEqual(self.provider.records[record_id]["content"], "203.0.113.20")
        self.assertEqual([m for m, _ in self.provider.calls].count("PATCH"), 1)
        with self.assertRaises(c.DeployError):
            recover.replacement_binding(raw, metadata, target, operation, "rehearsal")


class DNSContractTests(unittest.TestCase):
    def test_profile_and_sticky_generation(self):
        parsed = managed_profile()
        topology = t.parse_topology(declaration())
        self.assertEqual(upgrades.feature_generation(topology, {}, profile=parsed.projection()), 18)
        with self.assertRaises(c.DeployError):
            upgrades.check_feature_generation({"tools": {"generation": 17}}, topology, {}, profile=parsed.projection())
        with self.assertRaises(c.DeployError):
            upgrades.check_feature_generation({"tools": {"generation": 18}}, topology, {}, profile=parsed.projection(), client_generation=17)
        self.assertEqual(upgrades.feature_generation(topology, {"deployment_selection": {"minimum_generation": 18}}), 18)
        for change in ({"proxied": True}, {"records": []}, {"zone_id": "../invalid"},
                       {"public_addresses": ["203.0.113.90"]}, {"mode": "external"}):
            value = parsed.projection()
            value["listeners"]["relay.ingress"]["dns"].update(change)
            with self.assertRaises(c.DeployError):
                t.parse_profile(value, topology)
        self.assertIn("lib/dns.py", upgrades.required_files(18))
        self.assertNotIn("lib/dns.py", upgrades.required_files(17))
        with self.assertRaises(c.DeployError):
            upgrades.check_instance_capabilities({"profile": parsed.projection()}, 17)
        with self.assertRaises(c.DeployError):
            upgrades._check_feature_request(None, {"profile": parsed.projection(), "client": {"generation": 17}}, "bootstrap-plan")

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

    def test_http_error_bodies_are_bounded_and_malformed_errors_stay_uncertain(self):
        client = dns.Cloudflare(TOKEN)
        for raw, code in ((b"x" * (dns.MAX_RESPONSE + 1), "dns_response_bound"),
                          (b"not-json", "dns_provider_unavailable")):
            body = io.BytesIO(raw)
            response = HTTPError("https://provider.invalid", 400, "Bad request", {}, body)
            with patch.object(client._opener, "open", side_effect=response):
                with self.assertRaises(c.DeployError) as failure:
                    client.request("POST", ZONE, body={})
            self.assertEqual(failure.exception.code, code)
            self.assertTrue(body.closed)

    def test_read_rejection_does_not_claim_a_write_was_rejected(self):
        client = dns.Cloudflare(TOKEN)
        response = HTTPError("https://provider.invalid", 403, "Forbidden", {},
                             io.BytesIO(b'{"success":false,"errors":[{"code":10000,"message":"Authentication error"}]}'))
        with patch.object(client._opener, "open", side_effect=response):
            with self.assertRaises(c.DeployError) as failure:
                client.records(ZONE, "relay.example.org")
        self.assertEqual(failure.exception.code, "dns_provider_error")
        self.assertIn("10000", failure.exception.detail)

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

    def test_numbered_proxy_count_changes_and_rollback_never_write_dns(self):
        import feature_lifecycle
        import edge
        raw = managed_profile().projection()["listeners"]["relay.ingress"]["dns"]
        options = dict(public=True, proxy_protocol=True, managed_dns=raw,
                       independent=True, maintenance="continue")
        self.build_archive(copies=2, **options)
        self.bootstrap()
        self.request("activate", archive=str(self.archive), archive_sha256=c.digest(self.archive))
        original = self.version
        owned = copy.deepcopy(self.provider.records)
        for count, version in ((3, "20261116T000000Z-abcdef12"), (1, "20261117T000000Z-abcdef12")):
            current = copy.deepcopy(self.profile)
            self.build_archive(copies=count, version=version, **options)
            self.host.public_profile = t.parse_profile(self.profile, self.topology)
            self.profile = current
            with patch.object(dns.Cloudflare, "write", side_effect=AssertionError("Count changes must not write DNS")):
                result = self.request("activate", archive=str(self.archive), archive_sha256=c.digest(self.archive))
            self.profile = result["profile_update"]
            self.assertEqual(self.provider.records, owned)
            _, entry = self.host.require_identity(self.project, self.profile)
            inventory = feature_lifecycle.load(self.host, entry)[2]
            self.assertEqual(len(inventory.copies("relay")), count)
            self.assertEqual(inventory.roles["relay"].readiness.listener, "health")
            for identity in inventory.copies("relay"):
                self.assertIn(inventory.units[identity], self.host.running)
                endpoint = next(e for e in inventory.resolution.endpoints
                                if e.role == "relay" and e.ordinal == identity.ordinal and e.listener == "api")
                leaf = "stream-relay.api" + ("@" + str(identity.ordinal) if identity.ordinal > 1 else "") + ".conf"
                content = (self.host.paths.p(edge.RUNTIME) / self.profile["instance_id"] / leaf).read_text()
                self.assertEqual(content.count("proxy_protocol on;"), 1)
                self.assertIn(f"proxy_pass {endpoint.address}:{endpoint.port};", content)
            full = self.host.nginx_config()
            self.assertNotRegex(full, r"@[A-Z_]+@")
            self.assertTrue(edge._expanded_configuration(full))
            self.assertIn(["nginx", "-t", "-q"], self.host.commands)
        target_profile = copy.deepcopy(self.profile)
        target_profile["placement"]["relay"] = 2
        target_profile["listeners"]["relay.api"]["ports"] = [9443, 9444]
        self.host.public_profile = t.parse_profile(target_profile, self.topology)
        with patch.object(dns.Cloudflare, "write", side_effect=AssertionError("Copy rollback must not write DNS")):
            from test_feature_wiring import FeatureWiringTests
            FeatureWiringTests.rollback_target(self, original)
        self.assertEqual(self.provider.records, owned)

    def failed_dns_batch(self, *, cleanup_closes=True):
        import feature_lifecycle
        import feature_network
        self.build_managed()
        self.bootstrap()
        self.request("activate", archive=str(self.archive), archive_sha256=c.digest(self.archive))
        first = self.version
        raw = managed_profile().projection()["listeners"]["relay.ingress"]["dns"]
        raw["records"][0]["ttl"] = 600
        raw["records"].append({"ownership_id": "ffffffff-ffff-4fff-8fff-ffffffffffff", "address": "203.0.113.11", "ttl": 300})
        raw["public_addresses"].append("203.0.113.11")
        original = copy.deepcopy(self.profile)
        self.build_archive(public=True, managed_dns=raw, version="20261115T000000Z-abcdef12")
        candidate = t.parse_profile(self.profile, self.topology)
        run = self.host.run

        def with_host_addresses(args, **kwargs):
            if list(args) == ["ip", "-j", "address", "show"]:
                return subprocess.CompletedProcess(args, 0, json.dumps([{"addr_info": [
                    {"local": address} for address in raw["public_addresses"]]}]), "")
            return run(args, **kwargs)

        self.enterContext(patch.object(self.host, "run", side_effect=with_host_addresses))
        self.profile = original
        write = dns.Cloudflare.write

        def reject_create(client, change):
            if change.prior is None:
                self.provider.fault = (400, {"success": False, "errors": [{"code": 9313, "message": "private-account"}]})
                if not cleanup_closes:
                    self.enterContext(patch.object(feature_network, "close",
                        side_effect=c.DeployError("closure_failed", "Closure is unproven.")))
            return write(client, change)

        with patch.object(dns.Cloudflare, "write", reject_create), patch.object(feature_network, "open_public") as opened:
            with self.assertRaises(c.DeployError) as failure:
                self.request("activate", archive=str(self.archive), archive_sha256=c.digest(self.archive))
        self.assertEqual(failure.exception.code, "dns_provider_rejected", failure.exception.detail)
        opened.assert_not_called()
        self.assertFalse(self.host.running)
        self.host.public_profile = candidate
        entry = self.host.read_registry()["instances"][self.profile["instance_id"]]
        self.profile = entry["profile"]
        if not cleanup_closes:
            return first, raw
        report = feature_lifecycle.network_report(self.host, entry, ())
        self.assertIn("managed_dns_provider_rejected", {f["code"] for f in report["findings"]})
        self.assertNotIn("private-account", str(report))
        self.assertEqual(len(self.provider.records), 1)
        self.assertEqual(next(iter(self.provider.records.values()))["ttl"], 600)
        return first, raw

    def test_partial_rejection_then_corrective_release(self):
        import feature_network
        _, raw = self.failed_dns_batch()
        with patch.object(feature_network, "open_public") as opened:
            with self.assertRaises(c.DeployError):
                self.request("restart")
        opened.assert_not_called()
        original = copy.deepcopy(self.profile)
        self.build_archive(public=True, managed_dns=raw, version="20261115T010000Z-abcdef12")
        self.profile = original
        self.request("activate", archive=str(self.archive), archive_sha256=c.digest(self.archive))
        self.assertEqual(len(self.provider.records), 2)
        self.assertTrue(self.host.running)

    def test_partial_rejection_then_reviewed_rollback(self):
        first, _ = self.failed_dns_batch()
        binding = self.host.public_profile.listeners[0][1]
        self.host.public_profile = replace(self.host.public_profile, listeners=(("relay.api",
            replace(binding, public_addresses=("203.0.113.10",), records=binding.records[:1])),))
        operation = str(uuid.uuid4())
        review = self.request("release-plan", target=first, expected_current=self.version, operation_id=operation)
        self.request("rollback", target=first, expected_current=self.version, operation_id=operation, plan_token=review["plan_token"])
        self.assertEqual(len(self.provider.records), 1)
        self.assertEqual(next(iter(self.provider.records.values()))["ttl"], 300)
        self.assertTrue(self.host.running)

    def test_partial_rejection_requires_proven_cleanup_before_new_review(self):
        self.failed_dns_batch(cleanup_closes=False)
        with self.assertRaises(c.DeployError) as failure:
            self.request("release-plan", archive=str(self.archive), archive_sha256=c.digest(self.archive), operation_id=str(uuid.uuid4()))
        self.assertIn(failure.exception.code, ("reconciliation_required", "operation_cleanup_pending"))

    def test_opening_failure_after_dns_is_reported_as_pending_admission(self):
        import feature_lifecycle
        import feature_network
        self.build_managed()
        self.bootstrap()
        operation = str(uuid.uuid4())
        with patch.object(feature_network, "open_public", side_effect=c.DeployError("readiness_failed", "Opening failed")):
            with self.assertRaises(c.DeployError):
                self.request("activate", archive=str(self.archive), archive_sha256=c.digest(self.archive), operation_id=operation)
        self.assertTrue(self.provider.records)
        self.assertFalse(self.host.running)
        _, entry = self.host.require_identity(self.project, self.profile)
        state = dns.load(self.host, entry)
        self.assertEqual(state["operations"][operation]["public_admission"], "pending")
        self.assertEqual(set(state["operations"][operation]["results"].values()), {"applied"})
        report = feature_lifecycle.network_report(self.host, entry, ())
        self.assertIn("managed_dns_public_admission_pending", {f["code"] for f in report["findings"]})
        pending = next(v for v in report["dns"] if v["state"] == "public_admission_pending")
        self.assertEqual(pending["operation_id"], operation)

    def test_failed_listener_removal_reports_retained_owned_records(self):
        import feature_lifecycle
        import services
        self.build_managed()
        self.bootstrap()
        self.request("activate", archive=str(self.archive), archive_sha256=c.digest(self.archive))
        record_id = next(iter(self.provider.records))
        previous = copy.deepcopy(self.profile)
        self.build_archive(version="20261115T010000Z-abcdef12")
        self.profile = previous
        with patch.object(services, "wait_ready_set", return_value=False):
            with self.assertRaises(c.DeployError):
                self.request("activate", archive=str(self.archive), archive_sha256=c.digest(self.archive))
        entry = self.host.read_registry()["instances"][self.profile["instance_id"]]
        self.assertFalse(entry["profile"]["listeners"])
        self.assertIn(record_id, self.provider.records)
        before = dns.state_path(self.host, entry).read_bytes()
        report = feature_lifecycle.network_report(self.host, entry, ())
        retained = next(v for v in report["dns"] if v["state"] == "retained_record")
        self.assertEqual(retained["record_id"], record_id)
        self.assertEqual(retained["provider_state"], "published")
        self.assertIn("managed_dns_retained_record", {f["code"] for f in report["findings"]})
        self.assertEqual(dns.state_path(self.host, entry).read_bytes(), before)

    def test_real_release_plan_activate_rollback_and_listener_removal(self):
        import feature_network
        import feature_lifecycle
        monotonic, apply = time.monotonic, dns.apply
        elapsed = [0]

        def slow_apply(*args, **kwargs):
            result = apply(*args, **kwargs)
            elapsed[0] += release.READINESS_SECONDS + 1
            return result

        import release
        self.enterContext(patch.object(time, "monotonic", side_effect=lambda: monotonic() + elapsed[0]))
        self.enterContext(patch.object(dns, "apply", side_effect=slow_apply))
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
        self.assertEqual(feature_lifecycle.load(self.host, entry)[1].minimum_generation, 18)
        self.assertEqual({item["public_admission"] for item in dns.load(self.host, entry)["operations"].values()}, {"committed"})

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
