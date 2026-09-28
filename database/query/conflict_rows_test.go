package query

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func conflictCounterQuery() Query[user] {
	return ForModel(Define("counters", "id", []Column{
		{Name: "id", DatabaseDefault: true}, {Name: "name"}, {Name: "count", DatabaseDefault: true},
		{Name: "note", Nullable: true},
	}, func(database.Row) (user, error) { return user{}, nil }))
}

func TestConflictRowCalculationsAndConditions(t *testing.T) {
	q := conflictCounterQuery()
	name := NewTextField[user, string](q.Table(), "name", codec.String[string]())
	count := NewOrderedField[user, int64](q.Table(), "count", codec.Signed[int64]())
	note := NewNullableTextField[user, string](q.Table(), "note", codec.String[string]())
	rows := q.ConflictRows()
	stored := NewOrderedField[ConflictRow[user], int64](rows.Stored().Table(), "count", codec.Signed[int64]())
	proposed := NewOrderedField[ConflictRow[user], int64](rows.Proposed().Table(), "count", codec.Signed[int64]())
	proposedNote := NewNullableTextField[ConflictRow[user], string](rows.Proposed().Table(), "note", codec.String[string]())
	policy := OnConflict(name).DoUpdate(
		SetConflictValue(count, Add(stored, Add(proposed, proposed.Param(2)))),
		SetConflictValue(note, proposedNote),
	).Where(name.Ne("blocked")).WhereRows(Greater(proposed, stored))
	plan := insertPlan[user]{query: q, rows: []Mutation[user]{Change(Assign[user](q.Table(), "name", codec.String[string](), "counter"))}, conflict: &policy}
	s, err := plan.compile()
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{
		`"count" = (CAST("foundry_upsert"."count" AS bigint) +`,
		`CAST("excluded"."count" AS bigint) +`,
		`"note" = "excluded"."note"`,
		`WHERE ("foundry_upsert"."name" <> $3) AND ("excluded"."count" > "foundry_upsert"."count")`,
	} {
		if !strings.Contains(s.SQL(), part) {
			t.Fatalf("missing %s in %s", part, s.SQL())
		}
	}
	if !reflect.DeepEqual(s.Arguments(), []any{"counter", int64(2), "blocked"}) {
		t.Fatal(s.Arguments())
	}
	derived := policy.WhereRows(proposed.Gt(10))
	if len(policy.rowCondition) != 1 || len(derived.rowCondition) != 2 {
		t.Fatal("derivation mutated policy")
	}
	again, err := plan.compile()
	if err != nil || s.SQL() != again.SQL() || !reflect.DeepEqual(s.Arguments(), again.Arguments()) {
		t.Fatal("repeated compilation changed policy", err)
	}
}

func TestConflictRowsRejectInvalidExpressions(t *testing.T) {
	q := conflictCounterQuery()
	name := NewTextField[user, string](q.Table(), "name", codec.String[string]())
	count := NewOrderedField[user, int64](q.Table(), "count", codec.Signed[int64]())
	wrong := NewOrderedField[ConflictRow[user], int64]("other", "count", codec.Signed[int64]())
	undeclared := NewOrderedField[ConflictRow[user], int64](conflictProposedTable, "typo", codec.Signed[int64]())
	invalidScope := Query[user]{}.ConflictRows()
	zeroField := NewOrderedField[ConflictRow[user], int64](invalidScope.Stored().Table(), "count", codec.Signed[int64]())
	for _, policy := range []Conflict[user]{
		OnConflict(name).DoUpdate(SetConflictValue(count, RowExpression[ConflictRow[user], int64]{})),
		OnConflict(name).DoUpdate(SetConflictValue(count, wrong)),
		OnConflict(name).DoUpdate(SetConflictValue(count, undeclared)),
		OnConflict(name).DoUpdate(SetConflictValue(count, zeroField)),
		OnConflict(name).DoUpdate(SetConflictValue[user, int64](nil, wrong)),
		OnConflict(name).DoNothing().WhereRows(wrong.Gt(0)),
		OnConflict(name).Update(count).WhereRows(Predicate[ConflictRow[user]]{}),
		OnConflict(name).DoUpdate(ConflictUpdate[user]{field: count.ref, value: aggregateNode{}, incoming: true}),
		OnConflict(name).DoUpdate(ConflictUpdate[user]{field: count.ref, value: aggregateNode{}}),
	} {
		for _, inputs := range [][]Mutation[user]{nil, {Change(Assign[user](q.Table(), "name", codec.String[string](), "valid"))}} {
			if _, err := (insertPlan[user]{query: q, rows: inputs, conflict: &policy}).compile(); !errors.Is(err, fault.Invalid) {
				t.Fatal("invalid conflict compiled", err)
			}
		}
	}
	proposed := NewOrderedField[ConflictRow[user], int64](q.ConflictRows().Proposed().Table(), "count", codec.Signed[int64]())
	expression := rowInput[ConflictRow[user], int64](proposed)
	for range MaxExpressionDepth + 1 {
		expression = rowInput[ConflictRow[user], int64](Add(expression, proposed))
	}
	policy := OnConflict(name).DoUpdate(SetConflictValue(count, expression))
	if _, err := (insertPlan[user]{query: q, conflict: &policy}).compile(); !errors.Is(err, fault.Invalid) {
		t.Fatal("unbounded expression compiled", err)
	}
}

func TestConflictAssignmentDiscoversCTEAndCorrelatedScope(t *testing.T) {
	q := conflictCounterQuery()
	name := NewTextField[user, string](q.Table(), "name", codec.String[string]())
	count := NewOrderedField[user, int64](q.Table(), "count", codec.Signed[int64]())
	rows := q.ConflictRows()
	cte := CTE("foundry_upsert", q.Where(name.Eq("lookup")))
	lookup := As[conflictAlias](cte, "lookup")
	correlated := Correlate(rows, lookup)
	outer := OuterScope(correlated, rows.Stored())
	inner := InnerScope(correlated, lookup.Scope())
	stored := NewOrderedField[Correlation[ConflictRow[user], Alias[conflictAlias, user]], int64](outer.Table(), "count", codec.Signed[int64]())
	found := NewOrderedField[Correlation[ConflictRow[user], Alias[conflictAlias, user]], int64](inner.Table(), "count", codec.Signed[int64]())
	selected := SelectCorrelatedValue(correlated, found.Value()).Where(Greater(found, stored)).Limit(1)
	proposed := NewOrderedField[ConflictRow[user], int64](rows.Proposed().Table(), "count", codec.Signed[int64]())
	policy := OnConflict(name).DoUpdate(SetConflictValue(count, Coalesce(CorrelatedScalarRowQuery(selected), proposed)))
	s, err := (insertPlan[user]{query: q, rows: []Mutation[user]{Change(Assign[user](q.Table(), "name", codec.String[string](), "counter"))}, conflict: &policy}).compile()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(s.SQL(), `WITH "foundry_upsert"`) || !strings.Contains(s.SQL(), `INSERT INTO "counters" AS "foundry_upsert_2"`) ||
		!strings.Contains(s.SQL(), `"lookup"."count" > "foundry_upsert_2"."count"`) ||
		!reflect.DeepEqual(s.Arguments(), []any{"lookup", "counter", int64(1)}) {
		t.Fatal(s.SQL(), s.Arguments())
	}
}
