package rows

import (
	"errors"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
	"testing"
)

func TestFieldCapabilityFunctionsPreservePointersNullsAndInvalidDescriptors(t *testing.T) {
	f := declareAggregateRecords()
	var selected expr.Expression[aggregateRecord, int64, value.Nullable[int64]] = expr.Value(&f.MaybeExact)
	var parameter expr.RowExpression[aggregateRecord, int64] = expr.Param(&f.MaybeExact, int64(3))
	if _, err := CompileRead(ReadScalar(SelectScalar(From(f.Query()), expr.Row(selected).Value()))); err != nil {
		t.Fatal(err)
	}
	if _, err := CompileRead(ReadScalar(SelectScalar(From(f.Query()), parameter.Value()))); err != nil {
		t.Fatal(err)
	}
	var missing *orm.NullableNumber[aggregateRecord, int64]
	checks := []func() error{
		func() error {
			_, e := CompileRead(ReadScalar(SelectScalar(From(f.Query()), expr.Row(expr.Value(missing)).Value())))
			return e
		},
		func() error {
			_, e := CompileRead(ReadScalar(SelectScalar(From(f.Query()), expr.Param(missing, int64(3)).Value())))
			return e
		},
		func() error {
			_, e := CompileRead(ReadScalar(SelectScalar(From(f.Query()), expr.CountField(missing).Value())))
			return e
		},
		func() error {
			_, e := CompileRead(ReadScalar(SelectScalar(From(f.Query()), expr.CountDistinctField(missing).Value())))
			return e
		},
		func() error {
			_, e := CompileRead(ReadScalar(SelectScalar(From(f.Query()), expr.Min(missing).Value())))
			return e
		},
		func() error {
			_, e := CompileRead(ReadScalar(SelectScalar(From(f.Query()), expr.Max(missing).Value())))
			return e
		},
		func() error {
			_, e := CompileRead(ReadScalar(SelectScalar(From(f.Query()), expr.Sum(missing).Value())))
			return e
		},
		func() error {
			_, e := CompileRead(ReadScalar(SelectScalar(From(f.Query()), expr.Avg(missing).Value())))
			return e
		},
	}
	for i, check := range checks {
		if err := check(); !errors.Is(err, fault.Invalid) {
			t.Errorf("nil descriptor %d: %v", i, err)
		}
	}
}
