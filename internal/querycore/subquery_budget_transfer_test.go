package querycore

import (
	"database/sql/driver"
	"errors"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"testing"
)

func TestSubqueriesShareTheParameterBudget(t *testing.T) {
	inner := structuralSelect("records")
	outer := inner
	values := make([]any, MaxParameters/2+1)
	for i := range values {
		values[i] = float64(i)
	}
	wide := Comparison{Operand: FieldRef{Table: "records", Column: "id"}, Operator: In, Values: RetainComparisonValues(values), Kind: codec.TypeFloat, Bind: func(v any) (driver.Value, error) { return v, nil }}
	inner.Predicates = []Expression{wide}
	if _, e := CompileRead(inner, t.Context(), false, nil, true); e != nil {
		t.Fatal("half budget must compile", e)
	}
	outer.Predicates = []Expression{wide, SubqueryPredicate{Operand: FieldRef{Table: "records", Column: "id"}, Query: Subquery{Node: inner}}}
	s, e := CompileRead(outer, t.Context(), false, nil, true)
	if !errors.Is(e, fault.Invalid) || s.SQLText != "" || len(s.Bindings) != 0 {
		t.Fatal("shared parameter limit", e)
	}
}
