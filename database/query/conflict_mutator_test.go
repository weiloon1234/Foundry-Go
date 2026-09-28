package query

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Reuse the shared compiler model so this behavior does not duplicate the
// advanced expression test matrix for an otherwise identical model scope.
func conflictMutatorQuery(name, note func(string) (string, error)) Query[user] {
	q := mutationQuery()
	q.definition.modelFields = []ModelField[user]{
		NewMutatedModelField("name", codec.String[string](), func(user) string { return "" }, name),
		NewNullableMutatedModelField("note", codec.String[string](), func(user) value.Nullable[string] { return value.Null[string]() }, note),
	}
	return q
}

func TestConflictMutatorsNormalizeLiteralAssignmentsOnce(t *testing.T) {
	var calls []string
	q := conflictMutatorQuery(func(s string) (string, error) {
		calls = append(calls, "name:"+s)
		return strings.ToLower(strings.TrimSpace(s)), nil
	}, func(s string) (string, error) {
		calls = append(calls, "note:"+s)
		return s + "!", nil
	})
	name := NewTextField[user, string]("users", "name", codec.String[string]())
	note := NewNullableTextField[user, string]("users", "note", codec.String[string]())
	policy := OnConflict(name).DoUpdate(note.Set("constant"), name.Set(" AFTER "))
	plan := insertPlan[user]{query: q, rows: []Mutation[user]{
		Change(Assign[user]("users", "name", codec.String[string](), " FIRST ")),
		Change(Assign[user]("users", "name", codec.String[string](), " SECOND ")),
	}, conflict: &policy}
	for range 2 {
		calls = nil
		statement, err := plan.prepare(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(statement.Arguments(), []any{"first", "second", "constant!", "after"}) {
			t.Fatalf("conflict literal bypassed transformation: %v", statement.Arguments())
		}
		if !reflect.DeepEqual(calls, []string{"name: FIRST ", "name: SECOND ", "name: AFTER ", "note:constant"}) {
			t.Fatalf("mutator calls lost declaration order or repeated policy transformation per input row: %v", calls)
		}
	}
	for _, update := range []ConflictUpdate[user]{note.SetNull(), note.Incoming()} {
		calls = nil
		p := policy.DoUpdate(update)
		plan.conflict = &p
		if _, err := plan.prepare(t.Context()); err != nil || len(calls) != 2 {
			t.Fatalf("NULL/incoming conflict assignment invoked its mutator: %v %v", calls, err)
		}
	}
}

func TestConflictMutatorsRejectComputedBypassBeforeUserCode(t *testing.T) {
	calls := 0
	transform := func(s string) (string, error) { calls++; return s, nil }
	q := conflictMutatorQuery(transform, transform)
	name := NewTextField[user, string]("users", "name", codec.String[string]())
	note := NewNullableTextField[user, string]("users", "note", codec.String[string]())
	stored := NewTextField[ConflictRow[user], string](conflictStoredTable, "name", codec.String[string]())
	proposed := NewTextField[ConflictRow[user], string](conflictProposedTable, "name", codec.String[string]())
	proposedNote := NewNullableTextField[ConflictRow[user], string](conflictProposedTable, "note", codec.String[string]())
	for _, update := range []ConflictUpdate[user]{
		SetConflictValue(name, stored),
		SetConflictValue(name, Lower(proposed)),
		SetConflictValue(note, NullableRow(proposed)),
		SetConflictValue(name, proposed.Param("literal expression")),
	} {
		policy := OnConflict(name).DoUpdate(update)
		plan := insertPlan[user]{query: q, rows: []Mutation[user]{Change(Assign[user]("users", "name", codec.String[string](), "input"))}, conflict: &policy}
		if _, err := plan.prepare(t.Context()); !errors.Is(err, fault.Invalid) || calls != 0 {
			t.Fatalf("computed conflict bypassed mutator protection: %v calls=%d", err, calls)
		}
	}
	// The destination's own proposed value is already transformed by its draft.
	for _, update := range []ConflictUpdate[user]{SetConflictValue(name, proposed), SetConflictValue(note, proposedNote)} {
		policy := OnConflict(name).DoUpdate(update)
		plan := insertPlan[user]{query: q, rows: []Mutation[user]{Change(Assign[user]("users", "name", codec.String[string](), "input"))}, conflict: &policy}
		if _, err := plan.prepare(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
}

func TestConflictMutatorsPreserveErrorsAndPrivateInputs(t *testing.T) {
	sentinel := errors.New("reject conflict value")
	q := conflictMutatorQuery(func(s string) (string, error) {
		if s == "private-input" {
			return "", sentinel
		}
		return s, nil
	}, func(s string) (string, error) { return s, nil })
	name := NewTextField[user, string]("users", "name", codec.String[string]())
	update := name.Set("private-input")
	policy := OnConflict(name).DoUpdate(update)
	plan := insertPlan[user]{query: q, rows: []Mutation[user]{Change(Assign[user]("users", "name", codec.String[string](), "input"))}, conflict: &policy}
	if _, err := plan.prepare(t.Context()); !errors.Is(err, sentinel) {
		t.Fatalf("conflict mutator error was lost: %v", err)
	}
	for _, formatted := range []string{fmt.Sprint(update), fmt.Sprintf("%#v", update), fmt.Sprint(policy), fmt.Sprintf("%#v", policy)} {
		if strings.Contains(formatted, "private-input") {
			t.Fatal("conflict diagnostic exposed its pending input")
		}
	}
}
