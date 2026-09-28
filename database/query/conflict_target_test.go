package query

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestConflictIndexExpressionsAndSchemaConstants(t *testing.T) {
	q := conflictCounterQuery()
	name := NewTextField[user, string](q.Table(), "name", codec.String[string]())
	count := NewOrderedField[user, int64](q.Table(), "count", codec.Signed[int64]())
	note := NewNullableTextField[user, string](q.Table(), "note", codec.String[string]())
	policy := OnConflictKeys(Lower(name).Group(), count.Group()).
		TargetWhere(note.IsNull(), name.Ne("O'Reilly\\path")).
		DoUpdate(name.Set("new-bound-value")).Where(count.Gt(1))
	plan := insertPlan[user]{query: q, rows: []Mutation[user]{Change(Assign[user](q.Table(), "name", codec.String[string](), "incoming"))}, conflict: &policy}
	statement, err := plan.compile()
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{
		`ON CONFLICT ((LOWER(CAST("name" AS text))), "count") WHERE ("note" IS NULL)`,
		`("name" <> E'O''Reilly\\path') DO UPDATE SET "name" = $2`,
		`WHERE ("foundry_upsert"."count" > $3)`,
	} {
		if !strings.Contains(statement.SQL(), part) {
			t.Fatal("lost index target or execution scope", part, statement.SQL())
		}
	}
	if strings.Contains(statement.SQL(), "new-bound-value") || !reflect.DeepEqual(statement.Arguments(), []any{"incoming", "new-bound-value", int64(1)}) {
		t.Fatal(statement.SQL(), statement.Arguments())
	}
	derived := policy.TargetWhere(count.Gt(0))
	if len(policy.targetCondition) != 2 || len(derived.targetCondition) != 3 {
		t.Fatal("derivation mutated index predicate")
	}
	again, err := plan.compile()
	if err != nil || again.SQL() != statement.SQL() || !reflect.DeepEqual(again.Arguments(), statement.Arguments()) {
		t.Fatal("repeated compilation changed index target", err)
	}
}

func TestConflictIndexRejectsInvalidTargets(t *testing.T) {
	q := conflictCounterQuery()
	name := NewTextField[user, string](q.Table(), "name", codec.String[string]())
	other := NewTextField[user, string]("another_table", "name", codec.String[string]())
	var absent *TextField[user, string]
	scalar := ScalarRowQuery(q, SelectValue(q, name.Value()).Limit(1))
	hidden := Group[user]{node: caseNode{
		branches:  []caseBranch{{condition: ExistsQuery(q, q).expression, result: name.ref}},
		otherwise: name.ref,
	}}
	policies := []Conflict[user]{
		OnConflict(absent).DoNothing(),
		OnConflict(name).Update(absent),
		OnConflictKeys[user](Group[user]{}).DoNothing(),
		OnConflictKeys(name.Group(), name.Group()).DoNothing(),
		OnConflictKeys(Lower(name).Group(), Lower(name).Group()).DoNothing(),
		OnConflictKeys(other.Group()).DoNothing(),
		OnConflictKeys(scalar.Group()).DoNothing(),
		OnConflictKeys(hidden).DoNothing(),
		OnConflictKeys[user](Group[user]{node: aggregateNode{}}).DoNothing(),
		OnConflictKeys[user](Group[user]{node: windowNode{}}).DoNothing(),
		OnConflictConstraint[user]("counter_name_key").TargetWhere(name.Eq("schema")).DoNothing(),
		OnConflict[user]().TargetWhere(name.Eq("schema")).DoNothing(),
		OnConflict(name).TargetWhere(other.Eq("schema")).DoNothing(),
		OnConflict(name).TargetWhere(ExistsQuery(q, q)).DoNothing(),
		OnConflict(name).TargetWhere(Predicate[user]{}).DoNothing(),
	}
	for _, policy := range policies {
		for _, rows := range [][]Mutation[user]{nil, {Change(Assign[user](q.Table(), "name", codec.String[string](), "incoming"))}} {
			if _, err := (insertPlan[user]{query: q, rows: rows, conflict: &policy}).compile(); !errors.Is(err, fault.Invalid) {
				t.Fatal("invalid index target compiled", err)
			}
		}
	}
	tooMany := OnConflictKeys(make([]Group[user], MaxExpressionNodes+1)...).DoNothing()
	if _, err := (insertPlan[user]{query: q, conflict: &tooMany}).compile(); !errors.Is(err, fault.Invalid) {
		t.Fatal("unbounded index keys compiled", err)
	}
}
