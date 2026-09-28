package validationrules_test

import (
	"errors"
	"testing"

	"foundry.test/consumer/validationrules"
	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/validation"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestConditionalValidationAfterGeneratedJSONDecoding(t *testing.T) {
	limits := contract.JSONLimits{Bytes: 4096, Depth: 16, Nodes: 500, Steps: 2000, Issues: 20}
	rules := validationrules.Rules()
	for _, tc := range []struct {
		body  string
		issue string
	}{
		{`{"kind":"personal","email":"a@example.test","tags":["one"]}`, ""},
		{`{"kind":"business","email":"a@example.test","tags":["one"]}`, "/company"},
		{`{"kind":"business","company":"Example","email":"a@example.test","tags":["one"]}`, ""},
		{`{"kind":"personal","company":"","email":"a@example.test","tags":["one"]}`, "/company"},
		{`{"kind":"personal","email":"a@example.test","tags":["one","one"]}`, "/tags"},
	} {
		input, err := validationrules.RegistrationJSON().Decode(t.Context(), []byte(tc.body), limits)
		if err != nil {
			t.Fatal(err)
		}
		err = rules.Check(t.Context(), input, validation.DefaultLimits())
		if tc.issue == "" {
			if err != nil {
				t.Fatal(err)
			}
			continue
		}
		var failure *validation.Errors
		if !errors.As(err, &failure) {
			t.Fatal(err)
		}
		issues := failure.Issues()
		if len(issues) != 1 || issues[0].Path != tc.issue {
			t.Fatalf("decoded conditional diagnostics: %+v", issues)
		}
	}
}

func TestGeneratedConditionalConsumerRules(t *testing.T) {
	rules := validationrules.Rules()
	base := validationrules.Registration{Kind: validationrules.Business, Email: "Member@Example.test", Company: value.Set("Example"), Tags: []string{"one"}, Website: value.Set("https://example.test")}
	if err := rules.Check(t.Context(), base, validation.DefaultLimits()); err != nil {
		t.Fatal(err)
	}
	if base.Email != "Member@Example.test" {
		t.Fatal("validation normalized an input value")
	}
	for _, tc := range []struct {
		name, path string
		change     func(*validationrules.Registration)
	}{
		{"company", "/company", func(input *validationrules.Registration) { input.Company = value.Optional[string]{} }},
		{"personal-company", "/company", func(input *validationrules.Registration) { input.Kind = validationrules.Personal }},
		{"email", "/email", func(input *validationrules.Registration) { input.Email = "invalid" }},
		{"tags", "/tags", func(input *validationrules.Registration) { input.Tags = []string{"one", "one"} }},
		{"website", "/website", func(input *validationrules.Registration) { input.Website = value.Set("/relative") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := base
			tc.change(&input)
			var failure *validation.Errors
			if err := rules.Check(t.Context(), input, validation.DefaultLimits()); !errors.As(err, &failure) {
				t.Fatal(err)
			}
			issues := failure.Issues()
			if len(issues) != 1 || issues[0].Path != tc.path {
				t.Fatalf("consumer issues: %+v", issues)
			}
		})
	}
	base.Kind = validationrules.Personal
	base.Company = value.Optional[string]{}
	if err := rules.Check(t.Context(), base, validation.DefaultLimits()); err != nil {
		t.Fatal(err)
	}
	info, err := rules.Description()
	if err != nil || !info.ServerOnly {
		t.Fatal("consumer metadata lost server checks")
	}
}
