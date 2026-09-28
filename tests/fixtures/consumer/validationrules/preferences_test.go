package validationrules_test

import (
	"errors"
	"testing"

	"foundry.test/consumer/validationrules"
	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/validation"
)

func TestGeneratedTextRulesKeepReferenceCodesAndExplicitZones(t *testing.T) {
	limits := contract.JSONLimits{Bytes: 4096, Depth: 16, Nodes: 500, Steps: 2000, Issues: 20}
	input, err := validationrules.ProfilePreferencesJSON().Decode(t.Context(), []byte(`{"alias":"成员123","timezone":"+08:00","reference":"000123"}`), limits)
	if err != nil {
		t.Fatal(err)
	}
	rules := validationrules.PreferencesRules()
	if err := rules.Check(t.Context(), input, validation.DefaultLimits()); err != nil {
		t.Fatal(err)
	}
	if input.Reference != validationrules.ReferenceCode("000123") {
		t.Fatal("reference code lost its leading zeros")
	}
	input.Timezone = "Local"
	var failure *validation.Errors
	if err := rules.Check(t.Context(), input, validation.DefaultLimits()); !errors.As(err, &failure) {
		t.Fatal(err)
	}
	issues := failure.Issues()
	if len(issues) != 1 || issues[0].Path != "/timezone" {
		t.Fatalf("timezone diagnostics: %+v", issues)
	}
}
