package pgsmoke_test

import (
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"github.com/weiloon1234/Foundry-Go/value"
)

type conditionalRecordResult struct {
	Required   int64
	Choice     int64
	NullChoice value.Nullable[int64]
	Equal      value.Nullable[int64]
	Missing    value.Nullable[int64]
}
type conditionalRecordFields struct {
	rows.Row[conditionalRecordResult]
	Required, Choice           rows.Field[conditionalRecordResult, int64]
	NullChoice, Equal, Missing rows.Field[conditionalRecordResult, value.Nullable[int64]]
}

func TestStage6a2Conditionals(t *testing.T) {
	db := postgres.Isolate(t).Open(t)
	ctx := t.Context()
	_, err := db.Exec(ctx, `CREATE TABLE conditional_records (id bigint PRIMARY KEY,amount bigint NOT NULL,missing bigint,deleted_at timestamptz)`)
	must(t, err)
	_, err = db.Exec(ctx, `INSERT INTO conditional_records VALUES (1,7,NULL,NULL),(2,0,0,NULL),(3,8,4,now())`)
	must(t, err)
	table := orm.Define("conditional_records", func(b orm.Builder[arithmeticRecord], r *arithmeticRecord) arithmeticTable {
		return arithmeticTable{orm.TableOf(b, &r.ID), orm.NumberOf(b, &r.Amount), orm.NullableNumberOf(b, &r.Missing), orm.SoftDeleteOf(b, &r.DeletedAt)}
	})
	source := rows.As[arithmeticSide](table.Query(), "r")
	amount := expr.OrderRow(expr.At(rows.Scope(source), table.Amount))
	missing := expr.At(rows.Scope(source), table.Missing)
	zero := expr.OrderRow(amount).Param(0)
	fields := rows.Define(func(b rows.Builder[conditionalRecordResult], r *conditionalRecordResult) conditionalRecordFields {
		return conditionalRecordFields{b.Row(), rows.NumberOf(b, &r.Required), rows.NumberOf(b, &r.Choice), rows.NullableNumberOf(b, &r.NullChoice), rows.NullableNumberOf(b, &r.Equal), rows.NullableNumberOf(b, &r.Missing)}
	})
	choice := expr.When(amount.Gt(5), amount).When(amount.Eq(0), zero).Else(expr.OrderRow(amount).Param(10))
	q := rows.Select(source, fields,
		rows.MapSelected(fields.Required, expr.Coalesce(missing, amount).Value()),
		rows.MapSelected(fields.Choice, choice.Value()),
		rows.MapSelected(fields.NullChoice, expr.When(amount.Gt(5), amount).ElseNull().Value()),
		rows.MapSelected(fields.Equal, expr.NullIf(choice, zero).Value()),
		rows.MapSelected(fields.Missing, expr.NullIfNullable(expr.CoalesceNullable(missing, expr.NullFor(amount)), expr.NullableRow(zero)).Value()),
	).OrderBy(expr.At(rows.Scope(source), table.Key()).Asc())
	got, err := q.All(ctx, db)
	must(t, err)
	if len(got) != 2 || got[0].Required != 7 || got[0].Choice != 7 || !got[0].Missing.IsNull() || got[1].Required != 0 || got[1].Choice != 0 || !got[1].NullChoice.IsNull() || !got[1].Equal.IsNull() || !got[1].Missing.IsNull() {
		t.Fatal("conditional values/scopes", got)
	}
	if x, ok := got[0].NullChoice.Get(); !ok || x != 7 {
		t.Fatal(got)
	}
	if x, ok := got[0].Equal.Get(); !ok || x != 7 {
		t.Fatal(got)
	}
	type summary struct {
		Count   int64
		Missing value.Nullable[int64]
	}
	type summaryFields struct {
		rows.Row[summary]
		Count   rows.Field[summary, int64]
		Missing rows.Field[summary, value.Nullable[int64]]
	}
	summaryRows := rows.Define(func(b rows.Builder[summary], r *summary) summaryFields {
		return summaryFields{b.Row(), rows.NumberOf(b, &r.Count), rows.NullableNumberOf(b, &r.Missing)}
	})
	read := func(ex database.Executor, empty bool) summary {
		t.Helper()
		input := table.Query()
		if empty {
			input = input.Where(table.Amount.Lt(0))
		}
		a := rows.As[arithmeticSide](input, "r")
		count := expr.Count[rows.Alias[arithmeticSide, arithmeticRecord]]()
		selected := expr.WhenValue(count.Gt(0), count.Value()).Else(count.Param(0).Value())
		result := expr.CoalesceValue(expr.NullIfValue(selected, count.Param(0).Value()), count.Param(0).Value())
		nullable := expr.CoalesceNullableValue(expr.WhenValue(count.Gt(0), count.Value()).ElseNull())
		out, e := rows.Select(a, summaryRows, rows.MapSelected(summaryRows.Count, result), rows.MapSelected(summaryRows.Missing, expr.NullIfNullableValue(nullable, nullable))).All(ctx, ex)
		must(t, e)
		if len(out) != 1 || !out[0].Missing.IsNull() {
			t.Fatal(out)
		}
		return out[0]
	}
	if r := read(db, false); r.Count != 2 {
		t.Fatal("soft-delete scope lost", r)
	}
	if r := read(db, true); r.Count != 0 {
		t.Fatal("empty selected CASE", r)
	}
	rollback := errors.New("rollback conditional transaction")
	err = db.Transaction(ctx, func(tx *database.Tx) error {
		_, e := table.Insert(ctx, tx, table.Key().Set(4), table.Amount.Set(9))
		if e != nil {
			return e
		}
		if r := read(tx, false); r.Count != 3 {
			t.Fatal("conditional did not use transaction", r)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	if r := read(db, false); r.Count != 2 {
		t.Fatal("conditional rollback not persisted", r)
	}
}
