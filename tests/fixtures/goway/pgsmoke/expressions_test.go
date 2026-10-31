package pgsmoke_test

import (
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"github.com/weiloon1234/Foundry-Go/value"
)

type arithmeticSide struct{}
type arithmeticRecord struct {
	ID        int64
	Amount    int64
	Missing   value.Nullable[int64]
	DeletedAt value.Nullable[temporal.DateTime]
}
type arithmeticTable struct {
	orm.Table[arithmeticRecord, int64]
	Amount    orm.Number[arithmeticRecord, int64]
	Missing   orm.NullableNumber[arithmeticRecord, int64]
	DeletedAt orm.SoftDelete[arithmeticRecord, temporal.DateTime]
}
type arithmeticResult struct {
	Add, Subtract, Multiply, Divide, Remainder, Negate, Abs int64
	Exact                                                   decimal.Decimal
	Approx                                                  float64
	Missing                                                 value.Nullable[int64]
}
type arithmeticFields struct {
	rows.Row[arithmeticResult]
	Add, Subtract, Multiply, Divide, Remainder, Negate, Abs rows.Field[arithmeticResult, int64]
	Exact                                                   rows.Field[arithmeticResult, decimal.Decimal]
	Approx                                                  rows.Field[arithmeticResult, float64]
	Missing                                                 rows.Field[arithmeticResult, value.Nullable[int64]]
}

func TestStage6a1Expressions(t *testing.T) {
	db := postgres.Isolate(t).Open(t)
	ctx := t.Context()
	_, err := db.Exec(ctx, `CREATE TABLE expression_records (id bigint PRIMARY KEY,amount bigint NOT NULL,missing bigint,deleted_at timestamptz)`)
	must(t, err)
	_, err = db.Exec(ctx, `INSERT INTO expression_records VALUES (1,7,NULL,NULL),(2,99,1,now())`)
	must(t, err)
	table := orm.Define("expression_records", func(b orm.Builder[arithmeticRecord], r *arithmeticRecord) arithmeticTable {
		return arithmeticTable{orm.TableOf(b, &r.ID), orm.NumberOf(b, &r.Amount), orm.NullableNumberOf(b, &r.Missing), orm.SoftDeleteOf(b, &r.DeletedAt)}
	})
	source := rows.As[arithmeticSide](table.Query(), "r")
	v := expr.At(rows.Scope(source), orm.NumberExpr(table.Amount))
	n := expr.At(rows.Scope(source), orm.NullableNumberExpr(table.Missing))
	two := expr.ParamOf(expr.OrderRow(v), 2)
	fields := rows.Define(func(b rows.Builder[arithmeticResult], r *arithmeticResult) arithmeticFields {
		return arithmeticFields{b.Row(), rows.NumberOf(b, &r.Add), rows.NumberOf(b, &r.Subtract), rows.NumberOf(b, &r.Multiply), rows.NumberOf(b, &r.Divide), rows.NumberOf(b, &r.Remainder), rows.NumberOf(b, &r.Negate), rows.NumberOf(b, &r.Abs), rows.NumberOf(b, &r.Exact), rows.NumberOf(b, &r.Approx), rows.NullableNumberOf(b, &r.Missing)}
	})
	q := rows.OrderBy(rows.Select(source, fields,
		rows.MapSelected(fields.Add, expr.AsSelected(expr.Add(v, two))), rows.MapSelected(fields.Subtract, expr.AsSelected(expr.Subtract(v, two))), rows.MapSelected(fields.Multiply, expr.AsSelected(expr.Multiply(v, two))), rows.MapSelected(fields.Divide, expr.AsSelected(expr.Divide(v, two))), rows.MapSelected(fields.Remainder, expr.AsSelected(expr.Remainder(v, two))), rows.MapSelected(fields.Negate, expr.AsSelected(expr.Negate(v))), rows.MapSelected(fields.Abs, expr.AsSelected(expr.Abs(expr.Negate(v)))), rows.MapSelected(fields.Exact, expr.AsSelected(expr.DecimalOf(v))), rows.MapSelected(fields.Approx, expr.AsSelected(expr.FloatOf(v))), rows.MapSelected(fields.Missing, expr.AsSelected(expr.AddNullable(n, expr.NullableRow(two)))),
	).Where(expr.Greater(v, two), expr.LessOrEqual(two, v), expr.DistinctFrom(n, expr.NullableRow(v))), expr.Asc(expr.Negate(v)))
	got, err := rows.Read(q).All(ctx, db)
	must(t, err)
	if len(got) != 1 {
		t.Fatal(got)
	}
	r := got[0]
	if r.Add != 9 || r.Subtract != 5 || r.Multiply != 14 || r.Divide != 3 || r.Remainder != 1 || r.Negate != -7 || r.Abs != 7 || r.Exact.String() != "7" || r.Approx != 7 || !r.Missing.IsNull() {
		t.Fatal(r)
	}
	modelAmount := expr.Value(orm.NumberExpr(table.Amount))
	selected, err := orm.All(ctx, db, expr.OrderBy(expr.Where(table.Query(), expr.Eq(expr.Add(modelAmount, expr.Param(orm.NumberExpr(table.Amount), 2)), 9)), expr.Asc(expr.Negate(modelAmount))))
	must(t, err)
	if len(selected) != 1 || selected[0].ID != 1 {
		t.Fatal(selected)
	}
	type summary struct {
		Count  int64
		Exists bool
	}
	type summaryFields struct {
		rows.Row[summary]
		Count  rows.Field[summary, int64]
		Exists rows.Field[summary, bool]
	}
	summaryRows := rows.Define(func(b rows.Builder[summary], r *summary) summaryFields {
		return summaryFields{b.Row(), rows.NumberOf(b, &r.Count), rows.FieldOf(b, &r.Exists, orm.Codec(codec.Bool[bool]()))}
	})
	read := func(ex database.Executor, empty bool) summary {
		t.Helper()
		input := table.Query()
		if empty {
			input = input.Where(orm.NumberLt(table.Amount, 0))
		}
		a := rows.As[arithmeticSide](input, "r")
		out, e := rows.Read(rows.Select(a, summaryRows, rows.MapSelected(summaryRows.Count, expr.AsSelected(expr.Count[rows.Alias[arithmeticSide, arithmeticRecord]]())), rows.MapSelected(summaryRows.Exists, expr.AsSelected(expr.Exists[rows.Alias[arithmeticSide, arithmeticRecord]]())))).All(ctx, ex)
		must(t, e)
		if len(out) != 1 {
			t.Fatal(out)
		}
		return out[0]
	}
	if x := read(db, true); x.Count != 0 || x.Exists {
		t.Fatal(x)
	}
	if x := read(db, false); x.Count != 1 || !x.Exists {
		t.Fatal("soft-delete scope lost", x)
	}
	rollback := errors.New("rollback expression transaction")
	err = db.Transaction(ctx, func(tx *database.Tx) error {
		_, e := orm.Insert(ctx, tx, table.Table, orm.KeySet(table.Key(), 3), orm.NumberSet(table.Amount, 8))
		if e != nil {
			return e
		}
		if x := read(tx, false); x.Count != 2 {
			t.Fatal("transaction not used", x)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	if x := read(db, false); x.Count != 1 {
		t.Fatal("rollback not persisted", x)
	}
}
