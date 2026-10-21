package validation_test

import (
	"context"
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/validation"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestOriginalInputProhibitionsPreserveConditions(t *testing.T) {
	type input struct {
		Admin  bool
		Hidden value.Optional[string]
	}
	hidden := validation.DefineField("hidden", func(v input) value.Optional[string] { return v.Hidden })
	admin := validation.DefineField("admin", func(v input) bool { return v.Admin })
	condition := admin.Rules(validation.OneOf(true))
	rule := validation.All(validation.When(condition, hidden.Rules(validation.Absent[string]())), hidden.Rules(validation.Required[string]()))
	for _, tc := range []struct {
		in     input
		reject bool
	}{
		{input{false, value.Optional[string]{}}, false},
		{input{false, value.Set("x")}, false},
		{input{true, value.Set("")}, true},
		{input{true, value.Optional[string]{}}, false},
	} {
		err := rule.CheckProhibitions(t.Context(), tc.in, validation.DefaultLimits())
		if (err != nil) != tc.reject {
			t.Fatalf("%+v: %v", tc.in, err)
		}
	}
	// Prohibitions inside a predicate select a branch; they do not reject directly.
	predicate := validation.When(hidden.Rules(validation.Absent[string]()), admin.Rules(validation.OneOf(true)))
	if err := predicate.CheckProhibitions(t.Context(), input{}, validation.DefaultLimits()); err != nil {
		t.Fatal(err)
	}
}

func TestProhibitionSkipStillChecksLimitsAndContext(t *testing.T) {
	calls := 0
	rule := validation.Custom(validation.Spec{ID: "app.unused", Message: "Unused."}, func(context.Context, string) (bool, error) {
		calls++
		return true, nil
	})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for _, tc := range []struct {
		name   string
		ctx    context.Context
		limits validation.Limits
		want   error
	}{
		{"valid", t.Context(), validation.DefaultLimits(), nil},
		{"canceled", ctx, validation.DefaultLimits(), context.Canceled},
		{"nil-context", nil, validation.DefaultLimits(), fault.Invalid},
		{"limits-before-cancellation", ctx, validation.Limits{}, fault.Invalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := rule.CheckProhibitions(tc.ctx, "input", tc.limits)
			if !errors.Is(err, tc.want) {
				t.Fatal("prohibition skip changed validation preconditions", err)
			}
		})
	}
	if calls != 0 {
		t.Fatal("prohibition-only validation ran an unrelated callback", calls)
	}
}
