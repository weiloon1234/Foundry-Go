"""DNS-01 ownership and A03 refusals without ACME or host service mutations."""
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "lib"))
import certificates
from contracts import DeployError
from host import Host, Paths
from test_contracts import fixture, registry


class CertificateTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.host = Host(Paths(Path(self.temporary.name)))
        _, _, self.entry = fixture()
        self.registry = registry(self.entry)
        self.entry["certificate"]["zone_ref"] = "example.invalid"
        self.calls = []
        self.host.run = self.run_command

    def run_command(self, args, **kwargs):
        self.calls.append((list(map(str, args)), kwargs))
        output = "public-key\n" if "-pubkey" in args or "-pubout" in args else ""
        return subprocess.CompletedProcess(args, 0, output, "")

    def credential(self):
        directory = self.host.paths.p("/etc/foundry/cloudflare/zones")
        directory.mkdir(parents=True, mode=0o700)
        path = directory / "example.invalid.ini"
        self.host.owned_write(path, "dns_cloudflare_api_token = " + "private-canary-" * 3 + "\n", mode=0o600)
        return path

    def existing(self):
        certificates._directories(self.host, create=True)
        token = self.credential()
        lineage = self.entry["profile"]["domain"]
        archive = self.host.paths.p("/etc/foundry/letsencrypt/archive/" + lineage)
        live = self.host.paths.p("/etc/foundry/letsencrypt/live/" + lineage)
        archive.mkdir(parents=True)
        live.mkdir(parents=True)
        for name in ("cert", "chain", "privkey", "fullchain"):
            (archive / (name + "1.pem")).write_text("fixture")
            (live / (name + ".pem")).symlink_to(archive / (name + "1.pem"))
        self.host.owned_write(certificates._claim_path(self.host, self.entry),
                              json.dumps(certificates._claim(self.entry)), mode=0o600)
        renewal = certificates._renewal_path(self.host, self.entry)
        self.host.owned_write(renewal, "\n".join(f"{key} = {live / name}" for key, name in (
            ("cert", "cert.pem"), ("chain", "chain.pem"), ("privkey", "privkey.pem"), ("fullchain", "fullchain.pem")))
            + "\n[renewalparams]\nauthenticator = dns-cloudflare\n"
            + f"dns_cloudflare_credentials = {token}\ndns_cloudflare_propagation_seconds = 60\n"
            + f"renew_hook = {certificates.RELOAD_HOOK}\n")
        for name, content in certificates._units(self.entry).items():
            self.host.owned_write(self.host.paths.p("/etc/systemd/system/" + name), content)
        self.host.write_registry(self.registry)
        return renewal

    def test_inspection_of_missing_lineage_is_read_only(self):
        self.assertEqual(certificates.inspect(self.host, self.entry)["state"], "missing")
        self.assertEqual(self.calls, [])

    def test_dangling_lineage_link_cannot_escape_isolated_directories(self):
        live = self.host.paths.p(certificates.CONFIG_DIR + "/live/" + self.entry["profile"]["domain"])
        live.parent.mkdir(parents=True)
        live.symlink_to(self.host.paths.p("/etc/letsencrypt/live/foreign"))
        with self.assertRaises(DeployError):
            certificates.inspect(self.host, self.entry)
        self.assertEqual(self.calls, [])

    def test_existing_lineage_requires_dns_credentials_hook_and_timer(self):
        renewal = self.existing()
        original = renewal.read_text()
        for old, replacement in (("authenticator = dns-cloudflare", "authenticator = nginx"),
                                 ("dns_cloudflare_propagation_seconds = 60", "dns_cloudflare_propagation_seconds = 0"),
                                 (certificates.RELOAD_HOOK, "/bin/true"),
                                 ("zones/example.invalid.ini", "zones/foreign.ini")):
            with self.subTest(field=old):
                renewal.write_text(original.replace(old, replacement))
                with patch.object(certificates.packages, "install_missing") as install:
                    with self.assertRaises(DeployError):
                        certificates.provision(self.host, self.registry, self.entry)
                    install.assert_not_called()
                self.assertFalse(any(command[0] == "certbot" for command, _ in self.calls))
        renewal.write_text(original)
        self.host.paths.p("/etc/systemd/system/" + certificates._timer(self.entry) + ".timer").unlink()
        self.assertEqual(certificates.inspect(self.host, self.entry)["state"], "conflict")

    def test_foreign_lineage_is_not_adopted(self):
        self.existing()
        certificates._claim_path(self.host, self.entry).unlink()
        self.assertEqual(certificates.inspect(self.host, self.entry)["state"], "conflict")
        with self.assertRaises(DeployError):
            certificates.provision(self.host, self.registry, self.entry)

    def test_existing_valid_lineage_still_runs_dry_run(self):
        self.existing()
        with patch.object(certificates.packages, "install_missing"):
            certificates.provision(self.host, self.registry, self.entry)
        commands = [command for command, _ in self.calls]
        dry_runs = [command for command in commands if command[0] == "certbot"]
        self.assertEqual(dry_runs, [["certbot", "renew", "--cert-name", self.entry["profile"]["domain"],
                                    "--dry-run", "--run-deploy-hooks", "--no-random-sleep-on-renew"] + certificates._options()])
        self.assertIsNotNone(self.entry["certificate"]["last_renewal_check_at"])
        self.assertNotIn("private-canary", json.dumps(self.registry))
        self.assertNotIn("private-canary", json.dumps(commands))

    def test_distro_lineage_hooks_and_timer_are_outside_foundry_inspection(self):
        foreign = self.host.paths.p("/etc/letsencrypt/renewal/" + self.entry["profile"]["domain"] + ".conf")
        foreign.parent.mkdir(parents=True)
        foreign.write_text("[renewalparams]\npre_hook = stop-foreign-nginx\n")
        timer = self.host.paths.p("/etc/systemd/system/certbot.timer")
        timer.parent.mkdir(parents=True)
        timer.write_text("foreign timer")
        self.assertEqual("missing", certificates.inspect(self.host, self.entry)["state"])
        self.existing()
        with patch.object(certificates.packages, "install_missing"):
            certificates.provision(self.host, self.registry, self.entry)
        self.assertIn("stop-foreign-nginx", foreign.read_text())
        self.assertEqual("foreign timer", timer.read_text())
        self.assertFalse(any("certbot.timer" in args for args, _ in self.calls))
        self.assertFalse(any("/etc/letsencrypt" in arg for args, _ in self.calls for arg in args))

    def test_issuance_dry_run_and_timer_share_isolated_directories_and_hook_policy(self):
        self.credential()
        original = self.run_command
        def issue(args, **kwargs):
            if args[:2] == ["certbot", "certonly"]:
                # Model the provider's publication; readiness is asserted separately.
                certificates._renewal_path(self.host, self.entry).parent.mkdir(parents=True, exist_ok=True)
            return original(args, **kwargs)
        self.host.run = issue
        with patch.object(certificates.packages, "install_missing"), \
                patch.object(certificates, "_identity_ready"), patch.object(certificates, "_renewal_ready"):
            certificates.provision(self.host, self.registry, self.entry)
        commands = [args for args, _ in self.calls if args[0] == "certbot"]
        self.assertEqual(["certonly", "renew"], [args[1] for args in commands])
        for args in commands:
            self.assertEqual(certificates._options(), args[-len(certificates._options()):])
        service = certificates._units(self.entry)["foundry-certbot-renew.service"]
        self.assertIn(" ".join(certificates._options()), service)
        self.assertIn("--no-directory-hooks", service)
        self.assertEqual("foundry-certbot-renew", certificates._timer(self.entry))
        self.assertIn(["systemctl", "enable", "--now", "foundry-certbot-renew.timer"],
                      [args for args, _ in self.calls])

    def test_a03_refuses_owned_pre_post_hooks_and_global_configuration_edits(self):
        renewal = self.existing()
        original = renewal.read_text()
        for hook in ("pre_hook", "post_hook"):
            renewal.write_text(original + hook + " = stop-nginx\n")
            self.assertEqual("conflict", certificates.inspect(self.host, self.entry)["state"])
        renewal.write_text(original)
        self.host.paths.p(certificates.CONFIG_DIR + "/cli.ini").write_text("pre-hook = stop-nginx\n")
        self.assertEqual("conflict", certificates.inspect(self.host, self.entry)["state"])

    def test_failed_dry_run_has_no_success_receipt(self):
        self.existing()
        def run(args, **kwargs):
            if args[:2] == ["certbot", "renew"]:
                raise DeployError("command_failed", "private provider diagnostics")
            return self.run_command(args, **kwargs)
        self.host.run = run
        with patch.object(certificates.packages, "install_missing"), self.assertRaises(DeployError) as error:
            certificates.provision(self.host, self.registry, self.entry)
        self.assertIsNone(self.entry["certificate"]["last_renewal_check_at"])
        self.assertNotIn("private provider", str(error.exception))

    def test_unsafe_token_permissions_refused(self):
        token = self.credential()
        token.chmod(0o644)
        with self.assertRaises(DeployError):
            certificates.provision(self.host, self.registry, self.entry)
        self.assertEqual(self.calls, [])

    def test_hostname_and_expiry_are_separate_checks(self):
        self.existing()
        self.assertTrue(certificates.certificate_ready(self.host, self.entry))
        commands = [command for command, _ in self.calls]
        self.assertEqual(sum("-checkend" in command for command in commands), 1)
        self.assertEqual(sum("-checkhost" in command for command in commands), 1)
        self.assertFalse(any("-checkend" in command and "-checkhost" in command for command in commands))

    def test_private_stdin_intake_persists_only_zone_reference_publicly(self):
        token = "zone-private-token-" * 3
        self.entry["certificate"]["zone_ref"] = None
        path = self.host.paths.instance_config(self.entry["name"]) / "bootstrap-secrets.json"
        self.host.owned_write(path, json.dumps({"instance_id": self.entry["profile"]["instance_id"],
                                              "cloudflare_dns_token": token}), mode=0o600)
        with patch.object(certificates, "_resolve_zone", return_value="a" * 32) as resolve:
            certificates._stage_credentials(self.host, self.registry, self.entry)
        resolve.assert_called_once_with(token, self.entry["profile"]["domain"])
        stored = self.host.paths.p("/etc/foundry/cloudflare/zones/" + "a" * 32 + ".ini")
        self.assertEqual(stored.read_text(), "dns_cloudflare_api_token = " + token + "\n")
        self.assertEqual(stored.stat().st_mode & 0o777, 0o600)
        self.assertEqual(stored.parent.stat().st_mode & 0o777, 0o700)
        self.assertNotIn(token, json.dumps(self.registry))
        self.assertNotIn(token, json.dumps(self.calls))
        self.entry["provisioning"]["completed_steps"].append("certificate")
        stored.write_text("dns_cloudflare_api_token = existing-working-token-canary\n")
        with self.assertRaises(DeployError):
            certificates._stage_credentials(self.host, self.registry, self.entry)
        self.assertIn("existing-working-token", stored.read_text())

    def test_disabled_renewal_timer_refuses_existing_certificate(self):
        self.existing()
        def command(args, **kwargs):
            if args[:2] == ["systemctl", "is-enabled"]:
                return subprocess.CompletedProcess(args, 1, "disabled", "")
            return self.run_command(args, **kwargs)
        self.host.run = command
        self.assertEqual(certificates.inspect(self.host, self.entry)["state"], "conflict")
        with self.assertRaises(DeployError):
            certificates.provision(self.host, self.registry, self.entry)
        self.assertFalse(any(args[0] == "certbot" for args, _ in self.calls))

    def synthetic_fixture(self):
        self.entry["profile"].update(environment="acceptance", runtime_environment="testing",
                                     domain="app.acceptance.invalid")
        self.entry["certificate"].update(zone_ref=None, lineage="app.acceptance.invalid")
        self.host.paths.p("/.dockerenv").touch()
        directory = self.host.paths.p("/etc/foundry/acceptance-ca")
        directory.mkdir(mode=0o700, parents=True)
        self.host.owned_write(directory / "ca.key", "fixture-private-ca", mode=0o600)
        self.host.owned_write(directory / "ca.crt", "fixture-ca", mode=0o600)
        # Avoid rewriting fixture identity: this test exercises certificate ownership.
        self.host.journal = lambda *args: None

    def test_synthetic_mode_refuses_production_and_incomplete_fixture_guards(self):
        self.synthetic_fixture()
        for field, value in (("runtime_environment", "production"), ("runtime_environment", "staging"),
                             ("environment", "production"), ("domain", "production.example.com")):
            with self.subTest(field=field, value=value):
                original = self.entry["profile"][field]
                self.entry["profile"][field] = value
                with patch.dict(certificates.os.environ, {"FOUNDRY_ACCEPTANCE_SYNTHETIC_CERT": "1"}):
                    with self.assertRaises(DeployError):
                        certificates.provision(self.host, self.registry, self.entry)
                self.entry["profile"][field] = original
        self.host.paths.p("/.dockerenv").unlink()
        with patch.dict(certificates.os.environ, {"FOUNDRY_ACCEPTANCE_SYNTHETIC_CERT": "1"}):
            with self.assertRaises(DeployError):
                certificates.provision(self.host, self.registry, self.entry)
        self.assertEqual(self.calls, [])

    def test_synthetic_certificates_use_owned_lineage_renewal_and_reload_checks(self):
        self.synthetic_fixture()
        def command(args, **kwargs):
            if args[:2] == ["openssl", "req"]:
                Path(args[args.index("-keyout") + 1]).write_text("fixture-private-key")
            if args[:2] == ["openssl", "x509"] and "-req" in args:
                Path(args[args.index("-out") + 1]).write_text("fixture-certificate\n")
            return self.run_command(args, **kwargs)
        self.host.run = command
        with patch.dict(certificates.os.environ, {"FOUNDRY_ACCEPTANCE_SYNTHETIC_CERT": "1"}), \
                patch.object(certificates, "_resolve_zone") as resolve, \
                patch.object(certificates.packages, "install_missing") as install:
            certificates.provision(self.host, self.registry, self.entry)
            self.assertEqual(certificates.inspect(self.host, self.entry)["state"], "ready")
            renewal = certificates._renewal_path(self.host, self.entry)
            original = renewal.read_text()
            # Real Certbot persists --deploy-hook as renew_hook; no other key counts.
            renewal.write_text(original.replace("renew_hook =", "deploy_hook ="))
            self.assertEqual(certificates.inspect(self.host, self.entry)["state"], "conflict")
            renewal.write_text(original.replace(certificates.RELOAD_HOOK, "/bin/true"))
            self.assertEqual(certificates.inspect(self.host, self.entry)["state"], "conflict")
        resolve.assert_not_called()
        install.assert_not_called()
        commands = [args for args, _ in self.calls]
        self.assertFalse(any(args[0] == "certbot" for args in commands))
        self.assertEqual(commands.count(["/usr/bin/systemctl", "reload", "nginx"]), 2)
        self.assertTrue(any(args[:2] == ["openssl", "verify"] for args in commands))
        self.assertEqual(len(list(self.host.paths.p("/etc/foundry/letsencrypt/archive/app.acceptance.invalid").glob("*.pem"))), 8)
        self.assertIsNotNone(self.entry["certificate"]["last_renewal_check_at"])
        with patch.dict(certificates.os.environ, {"FOUNDRY_ACCEPTANCE_SYNTHETIC_CERT": "0"}):
            with self.assertRaises(DeployError):
                certificates.provision(self.host, self.registry, self.entry)

    def test_synthetic_timer_uses_stable_tools_pointer_from_every_caller(self):
        self.synthetic_fixture()
        self.entry["certificate"]["zone_ref"] = "acceptance-synthetic"
        with patch.object(certificates, "__file__", "/verification/deploy/lib/certificates.py"):
            staged = certificates._units(self.entry)
        with patch.object(certificates, "__file__", "/usr/local/lib/foundry/tools/" + "a" * 64 + "/lib/certificates.py"):
            installed = certificates._units(self.entry)
        self.assertEqual(staged, installed)
        service = staged[certificates._timer(self.entry) + ".service"]
        self.assertIn("/usr/local/lib/foundry/current/lib/certificates.py synthetic-renew ", service)
