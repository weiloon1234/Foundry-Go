package querycore

import (
	"context"
	"database/sql/driver"
	"math"
	"reflect"
	"testing"
)

func TestLookupDefaultsShareEqualityAndConflictRules(t *testing.T) {
	eq := func(name string, v any) Expression {
		return Comparison{Operand: FieldRef{Table: "records", Column: name}, Operator: Equal, Values: []any{v}, Bind: func(v any) (driver.Value, error) { return v, nil }}
	}
	q := QueryCore{Table: "records", Predicates: []Expression{eq("same", float64(0)), eq("same", math.Copysign(0, -1)), eq("conflict", int64(1)), eq("conflict", int64(2)), eq("mutated", "stored"), Junction{Any: true, Children: []Expression{eq("optional", "skip")}}, eq("null", nil)}}
	got, err := LookupDefaults(context.Background(), q, func(name string) bool { return name != "mutated" })
	if err != nil || !reflect.DeepEqual(got, []LookupDefault{{Column: "same", Value: float64(0)}}) {
		t.Fatal(got, err)
	}
}
