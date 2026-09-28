package query

import (
	"context"
	"slices"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

// LockedResult retains input scope S and complete result R while requiring a
// transaction for every read. It cannot be used as an unrestricted query source.
type LockedResult[S, R any] struct {
	query ProjectionQuery[S, R]
	locks []lockSpec
}

// ForUpdate locks contributing table rows. For outer joins use Of to select
// preserved, non-nullable sources. Aggregate/distinct/window results are invalid.
func (q ProjectionQuery[S, R]) ForUpdate() LockedResult[S, R] {
	return LockedResult[S, R]{query: q, locks: []lockSpec{{strength: lockUpdate}}}
}

// ForNoKeyUpdate requests a row lock compatible with FOR KEY SHARE.
func (q ProjectionQuery[S, R]) ForNoKeyUpdate() LockedResult[S, R] {
	return LockedResult[S, R]{query: q, locks: []lockSpec{{strength: lockNoKeyUpdate}}}
}

// ForShare requests shared row locks on contributing table rows.
func (q ProjectionQuery[S, R]) ForShare() LockedResult[S, R] {
	return LockedResult[S, R]{query: q, locks: []lockSpec{{strength: lockShare}}}
}

// ForKeyShare prevents deletion and key-changing updates to contributing rows.
func (q ProjectionQuery[S, R]) ForKeyShare() LockedResult[S, R] {
	return LockedResult[S, R]{query: q, locks: []lockSpec{{strength: lockKeyShare}}}
}

// ForUpdate locks the table rows contributing to each selected value.
func (q ValueQuery[S, V]) ForUpdate() LockedResult[S, V] { return q.query.ForUpdate() }

// ForNoKeyUpdate requests a row lock compatible with FOR KEY SHARE.
func (q ValueQuery[S, V]) ForNoKeyUpdate() LockedResult[S, V] { return q.query.ForNoKeyUpdate() }

// ForShare requests shared locks on contributing table rows.
func (q ValueQuery[S, V]) ForShare() LockedResult[S, V] { return q.query.ForShare() }

// ForKeyShare prevents deletion and key-changing updates to contributing rows.
func (q ValueQuery[S, V]) ForKeyShare() LockedResult[S, V] { return q.query.ForKeyShare() }

// Of replaces the lock target list with source-owned, non-nullable scopes.
// Empty/repeated/unknown scopes are rejected before execution. For multiple
// clauses, declare each target list in its RowLock descriptor instead.
func (q LockedResult[S, R]) Of(targets ...LockTarget[S]) LockedResult[S, R] {
	if len(q.locks) != 1 {
		q.query.source.err = fault.New(fault.Invalid, "Of requires a single lock clause; declare per-input targets in LockRows")
		return q
	}
	q.locks = slices.Clone(q.locks)
	q.locks[0] = lockOf(q.locks[0], targets)
	return q
}

// NoWait sets every clause to report conflicting row locks immediately.
func (q LockedResult[S, R]) NoWait() LockedResult[S, R] {
	q.locks = withLockBehavior(q.locks, lockNoWait)
	return q
}

// SkipLocked sets every clause to skip conflicting row locks, replacing NoWait.
func (q LockedResult[S, R]) SkipLocked() LockedResult[S, R] {
	q.locks = withLockBehavior(q.locks, lockSkip)
	return q
}

// Wait restores ordinary context-bounded waiting for every clause.
func (q LockedResult[S, R]) Wait() LockedResult[S, R] {
	q.locks = withLockBehavior(q.locks, lockWait)
	return q
}

func (q LockedResult[S, R]) Where(predicates ...Predicate[S]) LockedResult[S, R] {
	q.query = q.query.Where(predicates...)
	return q
}
func (q LockedResult[S, R]) OrderBy(orders ...ProjectionOrder[S]) LockedResult[S, R] {
	q.query = q.query.OrderBy(orders...)
	return q
}
func (q LockedResult[S, R]) Limit(count int) LockedResult[S, R] {
	q.query = q.query.Limit(count)
	return q
}
func (q LockedResult[S, R]) Offset(count int) LockedResult[S, R] {
	q.query = q.query.Offset(count)
	return q
}

func (q LockedResult[S, R]) reader() readResult[R] {
	r := q.query.reader()
	if len(q.locks) == 0 && r.err == nil {
		r.err = fault.New(fault.Invalid, "locked result requires at least one lock clause")
	}
	r.node.locks = q.locks
	r.transaction = true
	return r
}

// Compile validates the complete SELECT and its lock clause without execution.
func (q LockedResult[S, R]) Compile() (Statement, error) { return q.reader().Compile() }

// All collects complete typed results, discarding partial results on failure.
func (q LockedResult[S, R]) All(ctx context.Context, tx *database.Tx) ([]R, error) {
	if err := lockContext(ctx, tx); err != nil {
		return nil, err
	}
	return q.reader().All(ctx, tx)
}

// First returns an omitted Optional when no row can be selected. Order explicitly.
func (q LockedResult[S, R]) First(ctx context.Context, tx *database.Tx) (value.Optional[R], error) {
	if err := lockContext(ctx, tx); err != nil {
		return value.Optional[R]{}, err
	}
	return q.reader().First(ctx, tx)
}

// RequireFirst reports database.NotFound when no row can be selected.
func (q LockedResult[S, R]) RequireFirst(ctx context.Context, tx *database.Tx) (R, error) {
	if err := lockContext(ctx, tx); err != nil {
		return *new(R), err
	}
	return q.reader().RequireFirst(ctx, tx)
}

// Each streams typed results. Closing rows does not explicitly unlock rows,
// and a driver may drain unread rows; use Limit to bound the selected set.
// Callbacks run with rows open and cannot execute another operation on this Tx.
func (q LockedResult[S, R]) Each(ctx context.Context, tx *database.Tx, yield func(R) error) error {
	if err := lockContext(ctx, tx); err != nil {
		return err
	}
	return q.reader().Each(ctx, tx, yield)
}
