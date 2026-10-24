package orm

import (
	"context"
	"database/sql/driver"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestSourceInsertNormalizesLiteralsOnceAndRetainsPlan(t *testing.T) {
	calls, begun := 0, 0
	transform := func(s string) (string, error) {
		if begun == 0 {
			t.Fatal("mutator ran outside transaction")
		}
		calls++
		return strings.TrimSpace(s), nil
	}
	f := transformFields(transform, transform, transform)
	plan := FoundryInsertFrom[transformRow](f.Query(), FoundrySource(f.Query(), sqlowner.Metadata()), sqlowner.Queries()).Values(f.Key().Set(1), f.First.Set(" first "), f.Last.Set(" last "), f.Maybe.SetNull())
	var bindings [][]any
	db := testDB(t, testConnector{onBegin: func() { begun++ }, bindings: &bindings, columns: []string{"id", "first", "maybe", "last"}, values: [][]driver.Value{{int64(1), "first", nil, "last"}, {int64(2), "second", nil, "last"}}})
	for range 2 {
		got, err := plan.Returning(t.Context(), db, 2)
		if err != nil || len(got) != 2 {
			t.Fatal(got, err)
		}
	}
	if calls != 4 || !reflect.DeepEqual(bindings[0], bindings[1]) {
		t.Fatal(calls, bindings)
	}
}
func TestSourceInsertDoesNotSynthesizeSharedUUID(t *testing.T) {
	type row struct {
		ID   model.ID[row]
		Name string
	}
	type fields struct {
		Table[row, model.ID[row]]
		Name Text[row, string]
	}
	f := Define("uuid_source", func(b Builder[row], m *row) fields { return fields{TableOf(b, &m.ID), TextOf(b, &m.Name)} })
	begun := 0
	db := testDB(t, testConnector{onBegin: func() { begun++ }})
	_, err := FoundryInsertFrom[row](f.Query(), FoundrySource(f.Query(), sqlowner.Metadata()), sqlowner.Queries()).Values(f.Name.Set("value")).Exec(t.Context(), db)
	if err == nil || begun != 0 {
		t.Fatal(err, begun)
	}
}
func TestSourceInsertOutcomeAndClosure(t *testing.T) {
	identity := func(s string) (string, error) { return s, nil }
	f := transformFields(identity, identity, identity)
	plan := FoundryInsertFrom[transformRow](f.Query(), FoundrySource(f.Query(), sqlowner.Metadata()), sqlowner.Queries()).Values(f.Key().Set(1), f.First.Set("first"), f.Last.Set("last"))
	for _, closeFailure := range []bool{false, true} {
		closed := 0
		cfg := testConnector{onClose: func() { closed++ }, columns: []string{"id", "first", "maybe", "last"}, values: [][]driver.Value{{int64(1), "first", nil, "last"}}}
		if closeFailure {
			cfg.closeErr = errors.New("close failed")
		} else {
			cfg.commitErr = errors.New("commit uncertain")
		}
		got, err := plan.Returning(t.Context(), testDB(t, cfg), 2)
		var reconciliation *WriteError[[]transformRow]
		hasCandidate := errors.As(err, &reconciliation)
		if got != nil || err == nil || closed != 1 || hasCandidate == closeFailure {
			t.Fatal(got, err, closed, hasCandidate)
		}
		if hasCandidate && len(reconciliation.Candidate()) != 1 {
			t.Fatal(reconciliation.Candidate())
		}
	}
}

type noSourceTransaction struct{}

func (noSourceTransaction) Transaction(context.Context, func(*database.Tx) error, ...database.TxOptions) error {
	return nil
}
func TestSourceWriteRejectsTransactorWithoutCallback(t *testing.T) {
	identity := func(s string) (string, error) { return s, nil }
	f := transformFields(identity, identity, identity)
	_, err := FoundryInsertFrom[transformRow](f.Query(), FoundrySource(f.Query(), sqlowner.Metadata()), sqlowner.Queries()).Values(f.Key().Set(1), f.First.Set("first"), f.Last.Set("last")).Exec(t.Context(), noSourceTransaction{})
	if err == nil {
		t.Fatal("false success")
	}
}

// Exercise malformed framework metadata directly: public phase markers prevent
// consumers from converting a nullable match key into an assignment.
func TestSourceWriteRejectsNullableKeyAssignmentsBeforeSQL(t *testing.T) {
	type row struct {
		ID   int64 `foundry:"default=database"`
		Name string
		Note value.Nullable[string]
	}
	type fields struct {
		Table[row, int64]
		Name Text[row, string]
		Note NullableText[row, string]
	}
	f := Define("nullable_mapping", func(b Builder[row], m *row) fields {
		return fields{TableOf(b, &m.ID), TextOf(b, &m.Name), NullableTextOf(b, &m.Note)}
	})
	_, name := FoundryColumn(f.Name, sqlowner.Metadata())
	_, note := FoundryColumn(f.Note, sqlowner.Metadata())
	_, id := FoundryColumn(f.Key(), sqlowner.Metadata())
	malformed := ormcore.ModelMapping{
		Destination: name.Reference, Field: name.Field, Stored: reflect.TypeFor[string](),
		Value:       ormcore.Expression{Node: note.Field, Refs: []ormcore.Reference{note.Reference}},
		NullableKey: true,
	}
	key := FoundrySourceKey[row, row](ormcore.ModelMapping{
		Destination: id.Reference, Field: id.Field, Stored: reflect.TypeFor[int64](),
		Value: ormcore.Expression{Node: id.Field, Refs: []ormcore.Reference{id.Reference}},
	}, sqlowner.Expressions())
	source := FoundrySource(f.Query(), sqlowner.Metadata())
	insert := FoundryInsertFrom[row](f.Query(), source, sqlowner.Queries()).Select(FoundryInsertMapping[row, row](malformed, sqlowner.Expressions()))
	update := FoundryUpdateFrom[row](f.Query(), source, sqlowner.Queries()).Match(key).Select(FoundryUpdateMapping[row, row](malformed, sqlowner.Expressions()))
	for _, tc := range []struct {
		name string
		run  func(context.Context, database.Transactor) error
	}{
		{"insert_exec", func(ctx context.Context, db database.Transactor) error { _, err := insert.Exec(ctx, db); return err }},
		{"insert_returning", func(ctx context.Context, db database.Transactor) error {
			_, err := insert.Returning(ctx, db, 1)
			return err
		}},
		{"update_exec", func(ctx context.Context, db database.Transactor) error { _, err := update.Exec(ctx, db); return err }},
		{"update_returning", func(ctx context.Context, db database.Transactor) error {
			_, err := update.Returning(ctx, db, 1)
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var statements []string
			begun := 0
			db := testDB(t, testConnector{statements: &statements, onBegin: func() { begun++ }})
			err := tc.run(t.Context(), db)
			if !errors.Is(err, fault.Invalid) || !strings.Contains(err.Error(), "nullable source keys cannot be used as assignments") {
				t.Fatalf("expected nullable assignment rejection, got %v", err)
			}
			if begun != 0 || len(statements) != 0 {
				t.Fatalf("invalid assignment reached database: transactions=%d statements=%v", begun, statements)
			}
		})
	}
}
