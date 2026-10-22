import unittest
from pathlib import Path
import importlib.util

spec = importlib.util.spec_from_file_location("goway_catalogue", Path(__file__).with_name("goway_catalogue.py"))
catalogue = importlib.util.module_from_spec(spec)
spec.loader.exec_module(catalogue)


class CatalogueTests(unittest.TestCase):
    def test_complete_reviewed_catalogue_and_summary(self):
        rows = catalogue.validate()
        self.assertEqual(len(rows), 1026)
        page = (catalogue.ROOT / "blueprint/goway-data-layer/README.md").read_text()
        self.assertIn(catalogue.summary(rows), page)

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
        for name in names:
            with self.subTest(case=name):
                self.assertEqual((rows[name]["disposition"], rows[name]["stage"]), ("port", "7"))
                old = dict(rows[name], disposition="unaffected", stage="—", area="Authentication and security")
                with self.assertRaisesRegex(ValueError, "generated data-layer"):
                    catalogue.validate_classification(old, sources[name][0], symbols)

    def test_retained_contracts_and_literals_are_not_generated_models(self):
        source = '''import messaging "example/messaging"
        var _ = messaging.MemberChanges
        var _ = RequestValidationFields()
        var _ = SettingsConfigFields()
        // user.FoundryReference() and UserDraft
        var text = "QueryUsers() UserFields()"
        '''
        self.assertEqual(catalogue.generated_references(source, {"example/models": {"MemberChanges"}}), [])

    def test_area_and_stage_have_one_owner(self):
        for area, stages in catalogue.AREA_STAGES.items():
            for stage in stages:
                catalogue.validate_classification(
                    {"case": "valid", "area": area, "stage": stage, "disposition": "port"}, "", {})
        for area, stage in [("Core reads, writes and codecs", "6d"),
                            ("Windows and specialized expressions", "3"),
                            ("Framework integrations", "—"), ("unknown", "3")]:
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
