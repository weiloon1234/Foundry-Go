package validationrules_test

import (
	"errors"
	"strings"
	"testing"

	"foundry.test/consumer/validationrules"
	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/validation"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestGeneratedRequiredRulesRetainDecodedPresenceStates(t *testing.T) {
	limits := contract.JSONLimits{Bytes: 4096, Depth: 16, Nodes: 500, Steps: 2000, Issues: 20}
	const body = `{"label":"Member","nickname":"Go","count":0,"active":false,"forbidden":null,"tags":["one"]}`
	decode := func(data string) validationrules.PresenceInput {
		t.Helper()
		input, err := validationrules.PresenceInputJSON().Decode(t.Context(), []byte(data), limits)
		if err != nil {
			t.Fatal(err)
		}
		return input
	}
	input := decode(body)
	rules := validationrules.PresenceRules()
	if err := rules.Check(t.Context(), input, validation.DefaultLimits()); err != nil {
		t.Fatal(err)
	}
	count, supplied := input.Count.Get()
	if count != 0 || !supplied {
		t.Fatal("supplied zero was lost")
	}
	active, supplied := input.Active.Get()
	if active || !supplied {
		t.Fatal("supplied false was lost")
	}
	for _, tc := range []struct{ from, to, path string }{
		{`"count":0,`, "", "/count"},
		{`"label":"Member"`, `"label":" "`, "/label"},
		{`"nickname":"Go"`, `"nickname":null`, "/nickname"},
		{`"forbidden":null`, `"forbidden":"value"`, "/forbidden"},
		{`"tags":["one"]`, `"tags":[]`, "/tags"},
	} {
		var failure *validation.Errors
		err := rules.Check(t.Context(), decode(strings.Replace(body, tc.from, tc.to, 1)), validation.DefaultLimits())
		if !errors.As(err, &failure) {
			t.Fatal(err)
		}
		issues := failure.Issues()
		if len(issues) != 1 || issues[0].Path != tc.path {
			t.Fatalf("presence diagnostics: %+v", issues)
		}
	}
	if err := rules.Check(t.Context(), decode(strings.Replace(body, `"forbidden":null`, `"forbidden":" "`, 1)), validation.DefaultLimits()); err != nil {
		t.Fatal(err)
	}
}

func TestRelatedRequiredRulesTreatZeroAndFalseAsTriggers(t *testing.T) {
	rules := validationrules.RequiredWithRules()
	if err := rules.Check(t.Context(), validationrules.PresenceInput{}, validation.DefaultLimits()); err != nil {
		t.Fatal(err)
	}
	for _, input := range []validationrules.PresenceInput{{Count: value.Set(0)}, {Active: value.Set(false)}} {
		var failure *validation.Errors
		err := rules.Check(t.Context(), input, validation.DefaultLimits())
		if !errors.As(err, &failure) {
			t.Fatal(err)
		}
		issues := failure.Issues()
		if len(issues) != 1 || issues[0].Path != "/label" {
			t.Fatal(issues)
		}
		input.Label = value.Set("Member")
		if err := rules.Check(t.Context(), input, validation.DefaultLimits()); err != nil {
			t.Fatal(err)
		}
	}
}
