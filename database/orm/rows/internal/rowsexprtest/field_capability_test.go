package rowsexprtest

import "github.com/weiloon1234/Foundry-Go/database/orm/rows"

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
	var selected expr.Expression[aggregateRecord, int64, value.Nullable[int64]] = expr.Value(orm.NullableNumberExpr(f.MaybeExact))
	var parameter expr.RowExpression[aggregateRecord, int64] = expr.Param(orm.NullableNumberExpr(f.MaybeExact), int64(3))
	if _, err := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(rows.From(f.Query()), expr.Row(selected).Value()))); err != nil {
		t.Fatal(err)
	}
	if _, err := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(rows.From(f.Query()), parameter.Value()))); err != nil {
		t.Fatal(err)
	}
	var missing orm.NullableNumber[aggregateRecord, int64]
	checks := []func() error{
		func() error {
			_, e := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(rows.From(f.Query()), expr.Row(expr.Value(orm.NullableNumberExpr(missing))).Value())))
			return e
		},
		func() error {
			_, e := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(rows.From(f.Query()), expr.Param(orm.NullableNumberExpr(missing), int64(3)).Value())))
			return e
		},
		func() error {
			_, e := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(rows.From(f.Query()), expr.CountField(orm.NullableNumberExpr(missing)).Value())))
			return e
		},
		func() error {
			_, e := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(rows.From(f.Query()), expr.CountDistinctField(orm.NullableNumberExpr(missing)).Value())))
			return e
		},
		func() error {
			_, e := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(rows.From(f.Query()), expr.Min(orm.NullableNumberExpr(missing)).Value())))
			return e
		},
		func() error {
			_, e := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(rows.From(f.Query()), expr.Max(orm.NullableNumberExpr(missing)).Value())))
			return e
		},
		func() error {
			_, e := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(rows.From(f.Query()), expr.Sum(orm.NullableNumberExpr(missing)).Value())))
			return e
		},
		func() error {
			_, e := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(rows.From(f.Query()), expr.Avg(orm.NullableNumberExpr(missing)).Value())))
			return e
		},
	}
	for i, check := range checks {
		if err := check(); !errors.Is(err, fault.Invalid) {
			t.Errorf("nil descriptor %d: %v", i, err)
		}
	}
}
