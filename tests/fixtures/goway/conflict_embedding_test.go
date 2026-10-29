package goway_test

import (
	"context"
	"database/sql/driver"
	"testing"
	"time"

	"foundry.test/goway/coremodels"
	n "foundry.test/goway/models/hooks"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/value"
)

type conflictInputWrapper struct {
	orm.NullableInputTextAdapter[n.Member, coremodels.LabelInput, coremodels.StoredLabel]
	calls *int
}

func (w conflictInputWrapper) SetNull() orm.Assignment[n.Member] {
	(*w.calls)++
	return orm.InputTextSet(n.Members.Name, "redirected")
}

type conflictNullableWrapper struct {
	orm.ExpressionColumn[n.Member, time.Time, value.Nullable[time.Time]]
	calls *int
}

func (w conflictNullableWrapper) SetNull() orm.Assignment[n.Member] {
	(*w.calls)++
	return orm.InputTextSet(n.Members.Name, "redirected")
}

// Nullable wrappers inherit sealed metadata but override the public setter.
// It must never run or redirect SQL. Literal wrappers are compiler regressions.
func TestConflictEmbeddingCannotRedirectAssignments(t *testing.T) {
	f := n.Members
	calls := 0
	input := conflictInputWrapper{orm.NullableInputTextExpr(f.Note), &calls}
	nullable := conflictNullableWrapper{orm.NullableTimeExpr(f.DeletedAt), &calls}
	for _, tc := range []struct {
		name            string
		direct, wrapped orm.ConflictUpdate[n.Member]
	}{
		{"distinct_null", orm.ConflictNull(orm.NullableInputTextExpr(f.Note)), orm.ConflictNull(input)},
		{"null", orm.ConflictNull(orm.NullableTimeExpr(f.DeletedAt)), orm.ConflictNull(nullable)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var transcripts []string
			for _, update := range []orm.ConflictUpdate[n.Member]{tc.direct, tc.wrapped} {
				now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
				capture := &goldenCapture{scriptedColumns: []string{"id", "name", "note", "body", "created_at", "updated_at", "deleted_at"}, scriptedBatches: [][][]driver.Value{{{int64(1), "stored", "label", []byte{1}, now, now, nil}}}}
				db, err := database.Prepare(database.Adapter{Connector: connector{capture}}, database.DefaultPoolConfig(), database.WithClock(fixedClock{}))
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { check(db.Close(context.Background())) })
				if err := db.Start(t.Context()); err != nil {
					t.Fatal(err)
				}
				policy := orm.OnConflict(orm.ConflictKey(orm.KeyExpr(f.Key()))).DoUpdate(update)
				got, err := orm.Upsert(t.Context(), db, f.Query(), policy, orm.KeySet(f.Key(), 1), orm.InputTextSet(f.Name, " proposed "), orm.ColumnSet(f.Body, []byte{1}))
				if err != nil {
					t.Fatal(err)
				}
				if !got.IsSet() {
					t.Fatal("missing stored row")
				}
				transcripts = append(transcripts, capture.output.String())
			}
			if transcripts[0] != transcripts[1] {
				t.Fatalf("wrapper redirected conflict SQL or bindings\n%s", goldenDiff(transcripts[0], transcripts[1]))
			}
		})
	}
	if calls != 0 {
		t.Fatalf("application setters invoked %d times", calls)
	}
}
