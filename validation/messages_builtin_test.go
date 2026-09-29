package validation

import (
	"context"
	"testing"
)

func TestEveryBuiltinMessageHasAValidOwnedRecipe(t *testing.T) {
	seen := map[RuleID]bool{}
	for _, m := range builtinMessages {
		if seen[m.id] {
			t.Fatal("duplicate message", m.id)
		}
		seen[m.id] = true
		spec := Spec{ID: m.id, Parameters: []Parameter{parameter("min", 1), parameter("max", 1), parameter("bytes", 1), parameter("value", "2026-01-01"), parameter("divisor", 2), parameter("places", 2), parameter("format", "2006-01-02")}}
		prepared, err := builtinPrepared(spec)
		if err != nil {
			t.Fatal(m.id, err)
		}
		result, err := prepared.Format(context.Background(), nil, "")
		if err != nil || result.Text == "" {
			t.Fatal(m.id, result, err)
		}
		if _, err := prepared.Description().Prepare(); err != nil {
			t.Fatal(m.id, err)
		}
	}
}
