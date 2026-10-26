import unittest
import tempfile
from pathlib import Path
import importlib.util

spec = importlib.util.spec_from_file_location("goway_catalogue", Path(__file__).with_name("goway_catalogue.py"))
catalogue = importlib.util.module_from_spec(spec)
spec.loader.exec_module(catalogue)


class CatalogueTests(unittest.TestCase):
    def test_archived_inputs_have_one_unambiguous_owner(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            consumer = root / "tests/fixtures/consumer"
            case = consumer / "testdata/compilefail/example"
            case.mkdir(parents=True)
            (consumer / "types_test.go").write_text('{"example", "cannot convert"}')
            (case / "invalid.go.txt").write_text("package example")
            self.assertEqual(catalogue.cases(root)["example"], ("package example", "cannot convert"))
            (case / "invalid.go").write_text("package example")
            with self.assertRaisesRegex(ValueError, "both active and archived"):
                catalogue.cases(root)
            (case / "invalid.go").unlink()
            (case / "invalid.go.backup").write_text("package example")
            with self.assertRaisesRegex(ValueError, "unsupported compiler evidence suffix"):
                catalogue.cases(root)

    def test_complete_reviewed_catalogue_and_summary(self):
        rows = catalogue.validate()
        self.assertEqual(len(rows), 1032)
        page = (catalogue.ROOT / "blueprint/goway-data-layer/README.md").read_text()
        self.assertIn(catalogue.summary(rows), page)

    def test_retired_hook_exports_still_classify_without_generated_files(self):
        symbols = catalogue.generated_symbols()
        row = {"case": "retired_hook", "area": "Other framework services and contracts",
               "stage": "—", "disposition": "unaffected"}
        source = 'import m "foundry.test/consumer/pivothooks"\nvar _ m.AssignmentHooks'
        with self.assertRaisesRegex(ValueError, "generated data-layer"):
            catalogue.validate_classification(row, source, symbols)

    def test_standalone_import_recognizes_historical_exports(self):
        source = 'import "foundry.test/consumer/models"\nvar _ = (models.UserChanges{}).Fields().Scratch'
        self.assertIn("models.UserChanges", catalogue.generated_references(
            source, catalogue.generated_symbols()))

    def test_unaffected_rejects_generated_data_layer_shapes(self):
        row = {"case": "example", "area": "Authentication and security",
               "stage": "—", "disposition": "unaffected"}
        sources = [
            "var _ = user.FoundryReference()",
            "var _ models.UserDraft",
            "var _ = models.QueryUsers()",
            "var _ = models.UserFields()",
            "var _ = models.UserRelations()",
            'import places "github.com/weiloon1234/Foundry-Go/countries"\nvar _ = places.Find',
            'import m "example/models"\nvar _ = m.CompareUsers',
        ]
        for source in sources:
            with self.subTest(source=source):
                with self.assertRaisesRegex(ValueError, "unaffected references generated data-layer"):
                    catalogue.validate_classification(row, source, {"example/models": {"CompareUsers"}})

    def test_previously_missed_reference_and_store_sources(self):
        names = """auth_assurance auth_provider_key auth_scope_proof_owner country_key_contract
        mfa_retirement_owner notification_payload_owner notification_recipient_owner
        recovery_reference_model revocation_reference_model session_metadata_model
        token_id_model websocket_disconnect_model""".split()
        sources = catalogue.cases()
        symbols = catalogue.generated_symbols()
        rows = {r["case"]: r for r in catalogue.read_mapping()}
        # Stage 9 migrates these programs to explicit ORM identities. Keep their
        # original generated-reference expressions as regression inputs, so the
        # guard remains tested after the consumer no longer uses that API.
        legacy_references = {
            "auth_assurance": '(models.User{}).FoundryReference()',
            "auth_provider_key": '(models.User{}).FoundryReference()',
            "auth_scope_proof_owner": 'user.FoundryReference()',
            "mfa_retirement_owner": 'u.FoundryReference()',
            "recovery_reference_model": 'account.FoundryReference()',
            "revocation_reference_model": 'account.FoundryReference()',
            "notification_payload_owner": 'user.FoundryReference()',
            "notification_recipient_owner": 'other.FoundryReference()',
            "session_metadata_model": '(models.User{}).FoundryReference()',
            "token_id_model": 'user.FoundryReference()',
            "websocket_disconnect_model": 'o.FoundryReference()',
        }
        for name in names:
            with self.subTest(case=name):
                self.assertIn((rows[name]["disposition"], rows[name]["stage"]),
                              {("port", "7"), ("ported", "7")})
                old = dict(rows[name], disposition="unaffected", stage="—", area="Authentication and security")
                with self.assertRaisesRegex(ValueError, "generated data-layer"):
                    catalogue.validate_classification(
                        old, legacy_references.get(name, sources[name][0]), symbols)

    def test_retained_contracts_and_literals_are_not_generated_models(self):
        source = '''import messaging "example/messaging"
        var _ = messaging.MemberChanges
        var _ = RequestValidationFields()
        var _ = SettingsConfigFields()
        // user.FoundryReference() and UserDraft
        var text = "QueryUsers() UserFields()"
        '''
        self.assertEqual(catalogue.generated_references(source, {"example/models": {"MemberChanges"}}), [])

    def test_unaffected_framework_integration_keeps_dto_owner_rule(self):
        row = {"case": "importer_column_row", "area": "Framework integrations",
               "stage": "—", "disposition": "unaffected"}
        source, diagnostic = catalogue.cases()[row["case"]]
        symbols = catalogue.generated_symbols()
        catalogue.validate_classification(row, source, symbols)
        self.assertIn("importer.Column[reporting.MemberImport]", source)
        self.assertIn("func(row *reporting.OrderRow, v string)", source)
        self.assertEqual(diagnostic, "cannot use")
        # Allowing this area at stage — must not exempt legacy dependencies.
        for legacy in ("var _ = user.FoundryReference()",
                       "var _ = models.QueryUsers()",
                       'import m "example/models"\nvar _ m.MemberRow'):
            with self.subTest(legacy=legacy):
                with self.assertRaisesRegex(ValueError, "generated data-layer"):
                    catalogue.validate_classification(
                        row, source + "\n" + legacy,
                        {**symbols, "example/models": {"MemberRow"}})

    def test_area_and_stage_have_one_owner(self):
        for area, stages in catalogue.AREA_STAGES.items():
            for stage in stages:
                catalogue.validate_classification(
                    {"case": "valid", "area": area, "stage": stage, "disposition": "port"}, "", {})
        for area, stage in [("Core reads, writes and codecs", "6d"),
                            ("Windows and specialized expressions", "3"),
                            ("Framework integrations", "6a"), ("unknown", "3")]:
            with self.subTest(area=area, stage=stage):
                with self.assertRaisesRegex(ValueError, "inconsistent area/stage"):
                    catalogue.validate_classification(
                        {"case": "invalid", "area": area, "stage": stage, "disposition": "port"}, "", {})

    def test_blueprint_links_do_not_require_sibling_checkout(self):
        page = catalogue.ROOT / "blueprint/goway-data-layer/README.md"
        for link in catalogue.re.findall(r"\]\(([^)]+)\)", page.read_text()):
            if "://" in link or link.startswith("#"):
                continue
            target = (page.parent / link.split("#", 1)[0]).resolve()
            self.assertTrue(target.is_relative_to(catalogue.ROOT), link)
            self.assertTrue(target.exists(), link)

    def test_rule_retains_supporting_types_and_diagnostic(self):
        source = 'package bad\nimport "context"\ntype A int\nvar _ A = "x"\n'
        self.assertEqual(catalogue.rule(source, "cannot use"),
                         'Reject `type A int var _ A = "x"`; expected: cannot use')


if __name__ == "__main__":
    unittest.main()
