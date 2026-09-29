package dbassert_test

import (
	"context"
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/testkit/dbassert"
)

// rows is a typed stand-in for a generated soft-deleting model query.
type rows struct {
	visible, trashed int64
	onlyTrashed      bool
	err              error
}

func (r rows) Count(context.Context, database.Executor) (int64, error) {
	if r.onlyTrashed {
		return r.trashed, r.err
	}
	return r.visible, r.err
}
func (r rows) Exists(ctx context.Context, executor database.Executor) (bool, error) {
	count, err := r.Count(ctx, executor)
	return count > 0, err
}
func (r rows) OnlyTrashed() rows { r.onlyTrashed = true; return r }

type recorder struct {
	testing.TB
	failures int
}

func (r *recorder) Helper()               {}
func (r *recorder) Error(...any)          { r.failures++ }
func (r *recorder) Errorf(string, ...any) { r.failures++ }

func TestAssertionsReportOnlyMismatches(t *testing.T) {
	ctx := t.Context()
	dbassert.AssertExists(t, ctx, nil, rows{visible: 1})
	dbassert.AssertMissing(t, ctx, nil, rows{trashed: 3})
	dbassert.AssertCount(t, ctx, nil, rows{visible: 2}, 2)
	dbassert.AssertSoftDeleted(t, ctx, nil, rows{trashed: 1})

	failed := &recorder{TB: t}
	dbassert.AssertExists(failed, ctx, nil, rows{})
	dbassert.AssertMissing(failed, ctx, nil, rows{visible: 1})
	dbassert.AssertCount(failed, ctx, nil, rows{visible: 1}, 2)
	dbassert.AssertSoftDeleted(failed, ctx, nil, rows{visible: 1, trashed: 1})
	dbassert.AssertSoftDeleted(failed, ctx, nil, rows{})
	dbassert.AssertCount(failed, ctx, nil, rows{err: errors.New("query failed")}, 0)
	if failed.failures != 6 {
		t.Fatalf("reported %d failure(s), want 6", failed.failures)
	}
}
