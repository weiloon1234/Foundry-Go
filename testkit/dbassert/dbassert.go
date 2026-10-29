// Package dbassert asserts persisted state through typed query callbacks.
// Assertions only read through the supplied executor, usually the test's own
// transaction; they never reset, truncate or seed data.
package dbassert

import (
	"context"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
)

// AssertExists requires at least one row matched by exists.
// Pass a typed closure calling orm.Exists, or a row result's Exists method.
func AssertExists(t testing.TB, ctx context.Context, executor database.Executor, exists func(context.Context, database.Executor) (bool, error)) {
	t.Helper()
	found, err := exists(ctx, executor)
	if err != nil {
		t.Errorf("database existence query failed: %v", err)
		return
	}
	if !found {
		t.Error("expected a matching database row; none exists")
	}
}

// AssertMissing requires that count matches no rows. Soft-deleted rows are
// invisible to an ordinary ORM query; use AssertSoftDeleted for them.
// Pass a typed closure calling orm.Count, or a row result's Count method.
func AssertMissing(t testing.TB, ctx context.Context, executor database.Executor, count func(context.Context, database.Executor) (int64, error)) {
	t.Helper()
	found, err := count(ctx, executor)
	if err != nil {
		t.Errorf("database count query failed: %v", err)
		return
	}
	if found != 0 {
		t.Errorf("expected no matching database rows; found %d", found)
	}
}

// AssertCount requires exactly want rows matched by count.
// Pass a typed closure calling orm.Count, or a row result's Count method.
func AssertCount(t testing.TB, ctx context.Context, executor database.Executor, count func(context.Context, database.Executor) (int64, error), want int64) {
	t.Helper()
	found, err := count(ctx, executor)
	if err != nil {
		t.Errorf("database count query failed: %v", err)
		return
	}
	if found != want {
		t.Errorf("database row count: got %d, want %d", found, want)
	}
}

// AssertSoftDeleted requires that rows exist only as soft-deleted rows.
// Count must read the ordinary query; existsTrashed must read its OnlyTrashed
// variant. Both callbacks retain the same model filters and supplied executor.
func AssertSoftDeleted(t testing.TB, ctx context.Context, executor database.Executor, count func(context.Context, database.Executor) (int64, error), existsTrashed func(context.Context, database.Executor) (bool, error)) {
	t.Helper()
	visible, err := count(ctx, executor)
	if err != nil {
		t.Errorf("database count query failed: %v", err)
		return
	}
	trashed, err := existsTrashed(ctx, executor)
	if err != nil {
		t.Errorf("database trashed query failed: %v", err)
		return
	}
	if visible != 0 || !trashed {
		t.Errorf("expected only soft-deleted rows; visible %d, soft-deleted present %t", visible, trashed)
	}
}
