package validation_test

import (
	"github.com/weiloon1234/Foundry-Go/validation"
	"github.com/weiloon1234/Foundry-Go/value"
	"testing"
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
