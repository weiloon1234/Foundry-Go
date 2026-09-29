// Package dbassert asserts persisted state through typed generated queries.
// Assertions only read through the supplied executor, usually the test's own
// transaction; they never reset, truncate or seed data.
package dbassert

import (
	"context"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
)

// Query is satisfied by generated model queries, such as QueryUsers().Where(...),
// and by query.Query[M]. Its model scopes, including soft-delete visibility,
// apply exactly as they do in application code.
type Query interface {
	Count(context.Context, database.Executor) (int64, error)
	Exists(context.Context, database.Executor) (bool, error)
}

// Trashable is a generated soft-deleting model query deriving its own type.
type Trashable[Q any] interface {
	Query
	OnlyTrashed() Q
}

// AssertExists requires at least one row matched by query.
func AssertExists(t testing.TB, ctx context.Context, executor database.Executor, query Query) {
	t.Helper()
	exists, err := query.Exists(ctx, executor)
	if err != nil {
		t.Errorf("database existence query failed: %v", err)
		return
	}
	if !exists {
		t.Error("expected a matching database row; none exists")
	}
}

// AssertMissing requires that query matches no rows. Soft-deleted rows are
// invisible to an ordinary generated query; use AssertSoftDeleted for them.
func AssertMissing(t testing.TB, ctx context.Context, executor database.Executor, query Query) {
	t.Helper()
	count, err := query.Count(ctx, executor)
	if err != nil {
		t.Errorf("database count query failed: %v", err)
		return
	}
	if count != 0 {
		t.Errorf("expected no matching database rows; found %d", count)
	}
}

// AssertCount requires exactly want rows matched by query.
func AssertCount(t testing.TB, ctx context.Context, executor database.Executor, query Query, want int64) {
	t.Helper()
	count, err := query.Count(ctx, executor)
	if err != nil {
		t.Errorf("database count query failed: %v", err)
		return
	}
	if count != want {
		t.Errorf("database row count: got %d, want %d", count, want)
	}
}

// AssertSoftDeleted requires that query's rows exist only as soft-deleted
// rows: the ordinary query matches none and its OnlyTrashed variant matches at
// least one. Pass an ordinary query, not one derived with WithTrashed.
func AssertSoftDeleted[Q Trashable[Q]](t testing.TB, ctx context.Context, executor database.Executor, query Q) {
	t.Helper()
	visible, err := query.Count(ctx, executor)
	if err != nil {
		t.Errorf("database count query failed: %v", err)
		return
	}
	trashed, err := query.OnlyTrashed().Exists(ctx, executor)
	if err != nil {
		t.Errorf("database trashed query failed: %v", err)
		return
	}
	if visible != 0 || !trashed {
		t.Errorf("expected only soft-deleted rows; visible %d, soft-deleted present %t", visible, trashed)
	}
}
