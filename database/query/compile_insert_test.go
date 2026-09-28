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

func TestInsertBatchDefaultsAndConflictBindings(t *testing.T) {
	q := mutationQuery()
	name := NewTextField[user, string]("users", "name", codec.String[string]())
	enabled := NewScalarField[user, bool]("users", "enabled", codec.Bool[bool]())
	note := NewNullableTextField[user, string]("users", "note", codec.String[string]())
	rows := []Mutation[user]{
		Change(Assign[user]("users", "name", codec.String[string](), "first"), Assign[user]("users", "enabled", codec.Bool[bool](), false)),
		Change(Assign[user]("users", "name", codec.String[string](), "second"), Assign[user]("users", "note", codec.Nullable(codec.String[string]()), value.Null[string]())),
	}
	policy := OnConflict(name).DoUpdate(enabled.Incoming(), note.Set("literal '; --")).Where(enabled.Eq(false))
	s, err := (insertPlan[user]{query: q, rows: rows, conflict: &policy}).compile()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(s.SQL(), `("name", "enabled", "note") VALUES ($1, $2, DEFAULT), ($3, DEFAULT, $4) ON CONFLICT ("name") DO UPDATE SET "enabled" = "excluded"."enabled", "note" = $5 WHERE ("foundry_upsert"."enabled" = $6) RETURNING "foundry_upsert"."id"`) || !reflect.DeepEqual(s.Arguments(), []any{"first", false, "second", nil, "literal '; --", false}) {
		t.Fatal("upsert lost defaults, bindings or aliases", s)
	}
	if strings.Contains(s.SQL(), "literal") {
		t.Fatal("conflict value interpolated")
	}
	copy := policy.Where(name.Eq("extra"))
	copy = copy.Update(note)
	if len(policy.condition) != 1 || len(policy.updates) != 2 || len(copy.condition) != 2 {
		t.Fatal("conflict derivation changed source")
	}
	for _, p := range []Conflict[user]{OnConflict[user]().DoNothing(), OnConflictConstraint[user]("users_name_key").DoNothing(), OnConflict(name).DoUpdate(note.SetNull())} {
		if _, err := (insertPlan[user]{query: q, rows: rows, conflict: &p}).compile(); err != nil {
			t.Fatal(err)
		}
	}
	defaults := ForModel(Define("defaults", "id", []Column{{Name: "id", DatabaseDefault: true}}, func(database.Row) (user, error) { return user{}, nil }))
	s, err = (insertPlan[user]{query: defaults, rows: []Mutation[user]{{}, {}}}).compile()
	if err != nil || !strings.Contains(s.SQL(), `("id") VALUES (DEFAULT), (DEFAULT) RETURNING`) {
		t.Fatal("all-default batch failed", s, err)
	}
}

func TestInsertRejectsInvalidConflictAndEmptyBatchContracts(t *testing.T) {
	q := mutationQuery()
	name := NewTextField[user, string]("users", "name", codec.String[string]())
	id := NewScalarField[user, int64]("users", "id", codec.Signed[int64]())
	wrong := NewTextField[user, string]("orders", "name", codec.String[string]())
	unknown := NewTextField[user, string]("users", "unknown", codec.String[string]())
	pretendNullable := NewNullableTextField[user, string]("users", "name", codec.String[string]())
	valid := Change(Assign[user]("users", "name", codec.String[string](), "first"))
	for _, p := range []Conflict[user]{
		{}, OnConflict(name), OnConflictConstraint[user]("").DoNothing(), OnConflictConstraint[user]("bad; --").DoNothing(),
		OnConflict(name, name).DoNothing(), OnConflict(wrong).DoNothing(), OnConflict(unknown).DoNothing(), OnConflict[user](nil).DoNothing(),
		OnConflict[user]().Update(name), OnConflict(name).DoUpdate(), OnConflict(name).Update(id), OnConflict(name).Update(name, name),
		OnConflict(name).Update(wrong), OnConflict(name).DoUpdate(ConflictUpdate[user]{}), OnConflict(name).DoUpdate(pretendNullable.SetNull()),
		OnConflict(name).DoNothing().Where(name.Eq("invalid")), OnConflict(name).Update(name).Where(wrong.Eq("invalid")),
	} {
		for _, rows := range [][]Mutation[user]{{valid}, nil} {
			if _, err := (insertPlan[user]{query: q, rows: rows, conflict: &p}).compile(); !errors.Is(err, fault.Invalid) {
				t.Fatal("invalid conflict compiled", err)
			}
		}
	}
	if _, err := (insertPlan[user]{query: q, rows: make([]Mutation[user], MaxInsertRows+1)}).compile(); !errors.Is(err, fault.Invalid) {
		t.Fatal("oversized batch accepted")
	}
	if _, err := (insertPlan[user]{query: q, rows: []Mutation[user]{valid, {}}}).compile(); !errors.Is(err, fault.Missing) {
		t.Fatal("incomplete later row accepted")
	}
	if _, err := (insertPlan[user]{query: q.Where(name.Eq("scope"))}).compile(); !errors.Is(err, fault.Invalid) {
		t.Fatal("empty batch silently dropped filters")
	}
	if s, err := (insertPlan[user]{query: q}).compile(); err != nil || s.SQL() != "" {
		t.Fatal("valid empty batch became executable", err)
	}
}

type conflictAlias struct{}

func TestConflictConditionDiscoversCTEsAndAllocatesTargetAlias(t *testing.T) {
	q := mutationQuery()
	name := NewTextField[user, string]("users", "name", codec.String[string]())
	cte := CTE("foundry_upsert", q.Where(name.Eq("eligible")))
	source := As[conflictAlias](cte, "eligible")
	f := NewTextField[Alias[conflictAlias, user], string]("eligible", "name", codec.String[string]())
	policy := OnConflict(name).Update(name).Where(name.InQuery(SelectValue(source, f.Value())))
	s, err := (insertPlan[user]{query: q, rows: []Mutation[user]{Change(Assign[user]("users", "name", codec.String[string](), "incoming"))}, conflict: &policy}).compile()
	if err != nil || !strings.HasPrefix(s.SQL(), `WITH "foundry_upsert"`) || !strings.Contains(s.SQL(), `INSERT INTO "users" AS "foundry_upsert_2"`) || !reflect.DeepEqual(s.Arguments(), []any{"eligible", "incoming"}) {
		t.Fatal("conflict condition lost CTE scope or parameter order", s, err)
	}
}
