package orm

import (
	"database/sql/driver"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestForceDeleteKeyPreservesDefaultAndExplicitVisibility(t *testing.T) {
	r := timedRecords()
	now := time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)
	wantReturning := `DELETE FROM "timed" WHERE ("timed"."id" = $1) AND ("timed"."deleted_at" IS NULL) RETURNING "timed"."id", "timed"."created_at", "timed"."updated_at", "timed"."deleted_at"`
	for _, deleted := range []driver.Value{nil, now} {
		var statements []string
		var bindings [][]any
		db := testDB(t, testConnector{statements: &statements, bindings: &bindings, columns: []string{"id", "created_at", "updated_at", "deleted_at"}, values: [][]driver.Value{{int64(1), now, now, deleted}}})
		got, err := r.ForceDelete(t.Context(), db, SoftDeletes(r.DeletedAt), 1)
		if err != nil || got.ID != 1 || got.DeletedAt.IsNull() != (deleted == nil) {
			t.Fatalf("ForceDelete: %+v, %v", got, err)
		}
		if !reflect.DeepEqual(statements, []string{wantReturning}) || !reflect.DeepEqual(bindings, [][]any{{int64(1)}}) {
			t.Fatalf("SQL: %v; args: %v", statements, bindings)
		}
	}
	for _, tc := range []struct {
		q    Query[timed]
		want string
	}{
		{r.Query(), `DELETE FROM "timed" WHERE ("timed"."id" = $1) AND ("timed"."deleted_at" IS NULL)`},
		{r.Query().OnlyTrashed(), `DELETE FROM "timed" WHERE ("timed"."id" = $1) AND ("timed"."deleted_at" IS NOT NULL)`},
		{r.Query().WithTrashed(), `DELETE FROM "timed" WHERE ("timed"."id" = $1)`},
	} {
		var statements []string
		db := testDB(t, testConnector{statements: &statements})
		n, err := tc.q.Where(r.Key().Eq(1)).ForceDeleteAll(t.Context(), db, SoftDeletes(r.DeletedAt))
		if err != nil || n != 1 || !reflect.DeepEqual(statements, []string{tc.want}) {
			t.Fatalf("ForceDeleteAll: %d, %v, %v", n, err, statements)
		}
	}
}

func TestAdjustmentZeroBoundRejectedBeforeSQL(t *testing.T) {
	r := numericRecords()
	var statements []string
	begins := 0
	db := testDB(t, testConnector{statements: &statements, onBegin: func() { begins++ }})
	for _, a := range []Assignment[numeric]{r.Signed.Increment(1, Bound[int64]{}), r.Signed.Decrement(1, Bound[int64]{}), r.Nullable.Increment(1, Bound[int64]{})} {
		_, err := r.Update(t.Context(), db, 1, a)
		if !errors.Is(err, fault.Invalid) || !strings.Contains(err.Error(), "uninitialized adjustment bound") {
			t.Fatal(err)
		}
		_, err = r.Query().UpdateAll(t.Context(), db, a)
		if !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
	}
	if begins != 0 || len(statements) != 0 {
		t.Fatalf("invalid bounds began %d transactions and sent %v", begins, statements)
	}
}

type observedClock struct{ calls *int }

func (c observedClock) Now() time.Time {
	*c.calls++
	return time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)
}

func TestAdjustmentTargetsValidatedBeforeTransactionAndClock(t *testing.T) {
	type item struct {
		ID        int64
		Amount    int64
		CreatedAt time.Time
		UpdatedAt time.Time
	}
	type table struct {
		Table[item, int64]
		Amount Number[item, int64]
	}
	r := Define("timed_amounts", func(b Builder[item], m *item) table {
		CreatedAtOf(b, &m.CreatedAt)
		UpdatedAtOf(b, &m.UpdatedAt)
		return table{TableOf(b, &m.ID), NumberOf(b, &m.Amount)}
	})
	if err := Check(r); err != nil {
		t.Fatal(err)
	}
	if r.core.model.Timestamps == nil {
		t.Fatal("test requires managed timestamps")
	}
	var statements []string
	begins, calls := 0, 0
	db := testDB(t, testConnector{statements: &statements, onBegin: func() { begins++ }}, database.WithClock(observedClock{&calls}))
	managed := r.Amount.Increment(1)
	managed.adjust.Field.Column = r.core.model.Timestamps.Updated
	for _, as := range [][]Assignment[item]{
		{managed},
		{adjustment[item](r.Key().key, int64(1), false, nil)},
		{r.Amount.Set(10), r.Amount.Increment(1)},
		{r.Amount.Increment(1), r.Amount.Set(10)},
	} {
		if _, err := r.Update(t.Context(), db, 1, as...); !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
		if _, err := r.Query().UpdateAll(t.Context(), db, as...); !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
	}
	if begins != 0 || calls != 0 || len(statements) != 0 {
		t.Fatalf("invalid adjustments began %d transactions, sampled %d times, sent %v", begins, calls, statements)
	}
}

func TestExplicitSoftDeleteConflictsWithNoSoftDeletes(t *testing.T) {
	type row struct {
		ID        int
		RemovedAt value.Nullable[temporal.DateTime]
	}
	tbl := Define("conflict", func(b Builder[row], m *row) Table[row, int] {
		SoftDeleteOf(b, &m.RemovedAt)
		return TableOf(b, &m.ID)
	}, NoSoftDeletes())
	err := Check(tbl)
	if err == nil || !strings.Contains(err.Error(), "NoSoftDeletes conflicts with explicit SoftDeleteOf") {
		t.Fatal(err)
	}
	var statements []string
	db := testDB(t, testConnector{statements: &statements})
	if _, err := tbl.Query().All(t.Context(), db); err == nil || !strings.Contains(err.Error(), "NoSoftDeletes conflicts with explicit SoftDeleteOf") {
		t.Fatal(err)
	}
	if len(statements) != 0 {
		t.Fatal(statements)
	}
	if err := Check(unmanagedTimed(NoSoftDeletes())); err != nil {
		t.Fatalf("disabling convention: %v", err)
	}
}

func TestStartupErrorsClassifyAsInvalid(t *testing.T) {
	type row struct {
		ID      int
		Missing string
	}
	tbl := Define("invalid_declaration", func(b Builder[row], r *row) Table[row, int] { return TableOf(b, &r.ID) })
	if err := Check(tbl); !errors.Is(err, fault.Invalid) || !strings.Contains(err.Error(), "Missing") {
		t.Fatal(err)
	}
	var statements []string
	db := testDB(t, testConnector{statements: &statements})
	if _, err := tbl.Query().All(t.Context(), db); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if len(statements) != 0 {
		t.Fatal(statements)
	}
}
