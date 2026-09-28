package validation_test

import (
	"context"
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/validation"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestNullablePresenceMatrixPreservesOmissionNullAndEmpty(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name                                  string
		input                                 value.Optional[value.Nullable[string]]
		present, required, prohibited, absent bool
	}{
		{"omitted", value.Optional[value.Nullable[string]]{}, false, false, true, true},
		{"null", value.Set(value.Null[string]()), true, false, true, false},
		{"empty", value.Set(value.Of("")), true, false, true, false},
		{"blank", value.Set(value.Of(" \t\n")), true, false, true, false},
		{"value", value.Set(value.Of("text")), true, true, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, check := range []struct {
				rule  validation.Rule[value.Optional[value.Nullable[string]]]
				valid bool
			}{
				{validation.Present[value.Nullable[string]](), tc.present},
				{validation.RequiredNullable[string](), tc.required},
				{validation.ProhibitedNullable[string](), tc.prohibited},
				{validation.Absent[value.Nullable[string]](), tc.absent},
			} {
				err := check.rule.Check(t.Context(), tc.input, validation.DefaultLimits())
				if check.valid {
					if err != nil {
						t.Fatal(err)
					}
				} else {
					rejection(t, err)
				}
			}
		})
	}
}

func requirePresenceZero[T any](t *testing.T, input T) {
	t.Helper()
	if err := validation.Required[T]().Check(t.Context(), value.Set(input), validation.DefaultLimits()); err != nil {
		t.Fatal("supplied zero became empty", err)
	}
	if err := validation.RequiredNullable[T]().Check(t.Context(), value.Set(value.Of(input)), validation.DefaultLimits()); err != nil {
		t.Fatal("nullable supplied zero became null", err)
	}
	rejection(t, validation.Prohibited[T]().Check(t.Context(), value.Set(input), validation.DefaultLimits()))
	rejection(t, validation.ProhibitedNullable[T]().Check(t.Context(), value.Set(value.Of(input)), validation.DefaultLimits()))
}

func TestRequiredPreservesZeroFalseAndStructValues(t *testing.T) {
	t.Parallel()
	requirePresenceZero(t, 0)
	requirePresenceZero(t, false)
	requirePresenceZero(t, struct{}{})
	requirePresenceZero(t, Amount(0))
	for _, input := range [][]string{nil, {}} {
		rejection(t, validation.Required[[]string]().Check(t.Context(), value.Set(input), validation.DefaultLimits()))
		if err := validation.Prohibited[[]string]().Check(t.Context(), value.Set(input), validation.DefaultLimits()); err != nil {
			t.Fatal(err)
		}
	}
	if err := validation.Required[map[string]int]().Check(t.Context(), value.Set(map[string]int{"count": 0}), validation.DefaultLimits()); err != nil {
		t.Fatal(err)
	}
	rejection(t, validation.Required[map[string]int]().Check(t.Context(), value.Set(map[string]int{}), validation.DefaultLimits()))
}

func TestRequiredRulesBailBeforeDependentChecks(t *testing.T) {
	t.Parallel()
	calls := 0
	first := validation.Custom(validation.Spec{ID: "app.first_presence", Message: "First check."}, func(context.Context, string) (bool, error) { calls++; return false, nil })
	last := validation.Custom(validation.Spec{ID: "app.last_presence", Message: "Last check."}, func(context.Context, string) (bool, error) { t.Error("ran after rejection"); return true, nil })
	rule := validation.Required(first, last)
	for _, input := range []value.Optional[string]{{}, value.Set(" ")} {
		issues := rejection(t, rule.Check(t.Context(), input, validation.DefaultLimits())).Issues()
		if len(issues) != 1 || issues[0].Code != "foundry.required" {
			t.Fatal(issues)
		}
	}
	if calls != 0 {
		t.Fatal("empty inputs ran dependent checks")
	}
	rejection(t, rule.Check(t.Context(), value.Set("value"), validation.DefaultLimits()))
	if calls != 1 {
		t.Fatal("first check was not called once")
	}
	if err := validation.RequiredNullable(validation.Min(0)).Check(t.Context(), value.Set(value.Of(0)), validation.DefaultLimits()); err != nil {
		t.Fatal(err)
	}
}

func TestEmptyContentRulesKeepBoundsAndRejectUnwrappedStateMistakes(t *testing.T) {
	t.Parallel()
	limits := validation.DefaultLimits()
	limits.ValueBytes = 3
	var bound *validation.LimitError
	if err := validation.Required[string]().Check(t.Context(), value.Set("word"), limits); !errors.As(err, &bound) {
		t.Fatal("required ignored text bound", err)
	}
	rejection(t, validation.Prohibited[string]().Check(t.Context(), value.Set(string([]byte{0xff})), validation.DefaultLimits()))
	for _, err := range []error{
		validation.Required[value.Nullable[string]]().Validate(),
		validation.Prohibited[value.Optional[string]]().Validate(),
		validation.Empty[value.Nullable[string]]().Validate(),
		validation.NonEmpty[*string]().Validate(),
		validation.NonEmpty[any]().Validate(),
	} {
		if !errors.Is(err, fault.Invalid) {
			t.Fatal("ambiguous state declaration accepted", err)
		}
	}
	if err := validation.Empty[string]().Check(t.Context(), " \n", validation.DefaultLimits()); err != nil {
		t.Fatal(err)
	}
	rejection(t, validation.Empty[int]().Check(t.Context(), 0, validation.DefaultLimits()))
	if err := validation.NonEmpty[bool]().Check(t.Context(), false, validation.DefaultLimits()); err != nil {
		t.Fatal(err)
	}
	requireServerOnly(t, validation.Required[WireState](), true)
	requireServerOnly(t, validation.RequiredNullable[WireState](), true)
	requireServerOnly(t, validation.Prohibited[[]byte](), true)
	requireServerOnly(t, validation.Required[[]string](), false)
}
