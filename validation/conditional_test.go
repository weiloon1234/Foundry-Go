package validation_test

import (
	"context"
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/validation"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestConditionalRulesRetainFieldTypesAndHideConditionIssues(t *testing.T) {
	t.Parallel()
	type request struct {
		Kind    string
		Company value.Optional[string]
	}
	kind := validation.DefineField("kind", func(r request) string { return r.Kind })
	company := validation.DefineField("company", func(r request) value.Optional[string] { return r.Company })
	business := kind.Rules(validation.OneOf("business"))
	rule := validation.All(
		kind.Rules(validation.OneOf("personal", "business")),
		validation.When(business, company.Rules(validation.Bail(validation.Present[string](), validation.Optional(validation.NonBlank[string]())))),
		validation.Unless(business, company.Rules(validation.Absent[string]())),
	)
	for _, input := range []request{{Kind: "personal"}, {Kind: "business", Company: value.Set("Example")}} {
		if err := rule.Check(t.Context(), input, validation.DefaultLimits()); err != nil {
			t.Fatal(err)
		}
	}
	for _, input := range []request{{Kind: "business"}, {Kind: "business", Company: value.Set(" ")}, {Kind: "personal", Company: value.Set("")}} {
		issues := rejection(t, rule.Check(t.Context(), input, validation.DefaultLimits())).Issues()
		if len(issues) != 1 || issues[0].Path != "/company" {
			t.Fatalf("conditional diagnostics: %+v", issues)
		}
	}
	info, err := rule.Description()
	if err != nil {
		t.Fatal(err)
	}
	if info.Children[1].Kind != validation.WhenKind || info.Children[2].Kind != validation.UnlessKind {
		t.Fatal("conditional metadata lost control flow")
	}
}

func TestConditionsShareBoundsAndPropagateExecutionFailure(t *testing.T) {
	t.Parallel()
	condition := validation.Custom(validation.Spec{ID: "app.condition", Message: "Private condition rejection."}, func(context.Context, string) (bool, error) { return false, errors.New("private infrastructure detail") })
	branch := validation.Custom(validation.Spec{ID: "app.branch", Message: "Branch."}, func(context.Context, string) (bool, error) {
		t.Error("branch ran after failed condition")
		return true, nil
	})
	err := validation.Unless(condition, branch).Check(t.Context(), "value", validation.DefaultLimits())
	if !errors.Is(err, fault.Internal) {
		t.Fatal("failed condition became false", err)
	}
	condition = validation.OneOf("expected")
	limits := validation.DefaultLimits()
	limits.Checks = 2
	var bound *validation.LimitError
	err = validation.Unless(condition, validation.NonBlank[string]()).Check(t.Context(), "different", limits)
	if !errors.As(err, &bound) {
		t.Fatal("branch received a fresh work budget", err)
	}
	limits = validation.DefaultLimits()
	limits.Issues = 1
	err = validation.Unless(condition, validation.NonBlank[string]()).Check(t.Context(), "", limits)
	issues := rejection(t, err).Issues()
	if len(issues) != 1 || issues[0].Code != "foundry.non_blank" {
		t.Fatal("condition consumed public issue capacity")
	}
}

func TestPointerAndAbsentRulesPreservePresentZero(t *testing.T) {
	t.Parallel()
	zero := 0
	if err := validation.NotNil[int]().Check(t.Context(), &zero, validation.DefaultLimits()); err != nil {
		t.Fatal(err)
	}
	rejection(t, validation.NotNil[int]().Check(t.Context(), nil, validation.DefaultLimits()))
	rule := validation.Pointer(validation.Min(1))
	if err := rule.Check(t.Context(), nil, validation.DefaultLimits()); err != nil {
		t.Fatal(err)
	}
	rejection(t, rule.Check(t.Context(), &zero, validation.DefaultLimits()))
	rejection(t, validation.Absent[int]().Check(t.Context(), value.Set(0), validation.DefaultLimits()))
}
