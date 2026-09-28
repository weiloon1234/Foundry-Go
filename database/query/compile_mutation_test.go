package query

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

func mutationQuery() Query[user] {
	return ForModel(Define("users", "id", []Column{{Name: "id", DatabaseDefault: true}, {Name: "name"}, {Name: "enabled", DatabaseDefault: true}, {Name: "note", Nullable: true}}, func(database.Row) (user, error) { return user{}, nil }))
}

func TestMutationCompilerDefaultsNullsAndStableBindings(t *testing.T) {
	q := mutationQuery()
	name := Assign[user]("users", "name", codec.String[string](), "literal '; --")
	insert, err := (mutationPlan[user]{query: q, kind: insertModel, mutation: Change(name)}).compile()
	if err != nil {
		t.Fatal(err)
	}
	want := `INSERT INTO "users" ("name") VALUES ($1) RETURNING "users"."id", "users"."name", "users"."enabled", "users"."note"`
	if insert.SQL() != want || !reflect.DeepEqual(insert.Arguments(), []any{"literal '; --"}) {
		t.Fatal("insert did not preserve omission and binding")
	}
	id := NewScalarField[user, int64]("users", "id", codec.Signed[int64]())
	filtered := q.Where(id.Eq(42), NewTextField[user, string]("users", "name", codec.String[string]()).Eq("allowed"))
	patch := Change(Assign[user]("users", "enabled", codec.Bool[bool](), false), Assign[user]("users", "note", codec.Nullable(codec.String[string]()), value.Null[string]()))
	update, err := (mutationPlan[user]{query: filtered, kind: updateModel, mutation: patch}).compile()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(update.SQL(), `SET "enabled" = $1, "note" = $2 WHERE ("users"."id" = $3) AND ("users"."name" = $4) RETURNING`) || !reflect.DeepEqual(update.Arguments(), []any{false, nil, int64(42), "allowed"}) {
		t.Fatal("patch changed zero/null or scope parameters")
	}
	remove, err := (mutationPlan[user]{query: filtered, kind: deleteModel}).compile()
	if err != nil || !strings.HasPrefix(remove.SQL(), `DELETE FROM "users" WHERE ("users"."id" = $1)`) {
		t.Fatal("delete lost primary-key scope")
	}
	defaults := ForModel(Define("users", "id", []Column{{Name: "id", DatabaseDefault: true}}, func(database.Row) (user, error) { return user{}, nil }))
	statement, err := (mutationPlan[user]{query: defaults, kind: insertModel}).compile()
	if err != nil || !strings.Contains(statement.SQL(), " DEFAULT VALUES RETURNING ") {
		t.Fatal("all-default create is unavailable")
	}
}

func TestMutationCompilerRejectsUnsafeOrIncompleteWrites(t *testing.T) {
	q := mutationQuery()
	id := NewScalarField[user, int64]("users", "id", codec.Signed[int64]())
	name := NewTextField[user, string]("users", "name", codec.String[string]())
	valid := Change(Assign[user]("users", "name", codec.String[string](), "valid"))
	for label, plan := range map[string]mutationPlan[user]{
		"missing required":   {query: q, kind: insertModel},
		"unknown column":     {query: q, kind: insertModel, mutation: Change(Assign[user]("users", "typo", codec.String[string](), "bad"))},
		"wrong table":        {query: q, kind: insertModel, mutation: Change(Assign[user]("orders", "name", codec.String[string](), "bad"))},
		"duplicate field":    {query: q, kind: insertModel, mutation: Change(Assign[user]("users", "name", codec.String[string](), "a"), Assign[user]("users", "name", codec.String[string](), "b"))},
		"null required":      {query: q, kind: insertModel, mutation: Change(Assign[user]("users", "name", codec.Nullable(codec.String[string]()), value.Null[string]()))},
		"create filtered":    {query: q.Where(id.Eq(1)), kind: insertModel, mutation: valid},
		"update unscoped":    {query: q, kind: updateModel, mutation: valid},
		"delete unscoped":    {query: q, kind: deleteModel},
		"key in disjunction": {query: q.Where(Or(id.Eq(1), name.Eq("other"))), kind: deleteModel},
		"negated key":        {query: q.Where(id.Eq(1).Not()), kind: deleteModel},
		"empty update":       {query: q.Where(id.Eq(1)), kind: updateModel},
		"primary mutation":   {query: q.Where(id.Eq(1)), kind: updateModel, mutation: Change(Assign[user]("users", "id", codec.Signed[int64](), int64(2)))},
		"paged delete":       {query: q.Where(id.Eq(1)).Limit(1), kind: deleteModel},
		"ordered update":     {query: q.Where(id.Eq(1)).OrderBy(id.Asc()), kind: updateModel, mutation: valid},
	} {
		t.Run(label, func(t *testing.T) {
			_, err := plan.compile()
			if !errors.Is(err, fault.Invalid) && !errors.Is(err, fault.Missing) {
				t.Fatalf("unsafe write compiled: %v", err)
			}
		})
	}
}
