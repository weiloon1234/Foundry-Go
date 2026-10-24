package rows

import (
	"context"
	"reflect"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"github.com/weiloon1234/Foundry-Go/value"
)

// LockTarget identifies one preserved input; nullable scopes are excluded.
// Example: rows.Target(rows.Scope(users)).
type LockTarget[S any] struct {
	_    [0]*S
	core ormcore.Scope
}

// Target retains scope and declaration ownership for a lock target.
// Example: locked.Of(rows.Target(rows.Scope(users))).
func Target[S, R any](scope expr.Scope[S, R]) LockTarget[S] {
	return LockTarget[S]{core: expr.FoundryScopeData(scope, sqlowner.Expressions())}
}

// RowLock declares one lock strength and wait policy for preserved inputs.
// Example: rows.UpdateLock(rows.Target(scope)).SkipLocked().
type RowLock[S any] struct {
	_    [0]*S
	core ormcore.RowLock
}

// LockedResult requires Tx and offers no count, pagination or ordinary source conversion.
// Example: rows.ForUpdate(rows.Read(report)).All(ctx,tx).
type LockedResult[S, R any] struct {
	_                 [0]*S
	_                 [0]*R
	lockedResultPhase [0]struct{}
	core              ormcore.RecordSource
}

func (q LockedResult[S, R]) transactionRecord(*R) ormcore.RecordSource { return q.core }

func readSourceOf[S, R any](source ReadSource[S, R]) ormcore.RecordSource {
	if ormcore.NilDescriptor(source) {
		return ormcore.RecordSource{Source: ormcore.Source{Err: ormcore.Invalid("read requires a source")}}
	}
	r := source.readSource(nil, nil)
	r.Source.Err = ormcore.ValidateRecordSource(r, reflect.TypeFor[R](), r.Transaction)
	return r
}
func lockTargets[S any](targets []LockTarget[S]) []ormcore.Scope {
	out := make([]ormcore.Scope, len(targets))
	for i, t := range targets {
		out[i] = t.core
	}
	return out
}

// LockRows applies independent clauses using the existing lock compiler.
// Example: rows.LockRows(rows.Read(report),rows.UpdateLock(rows.Target(scope))).
func LockRows[S, R any](source ReadSource[S, R], clauses ...RowLock[S]) LockedResult[S, R] {
	locks := make([]ormcore.RowLock, len(clauses))
	for i, c := range clauses {
		locks[i] = c.core
	}
	return LockedResult[S, R]{core: ormcore.LockRecord(readSourceOf(source), locks)}
}

// UpdateLock declares a lock on explicit preserved targets. Example: rows.UpdateLock(rows.Target(scope)).
func UpdateLock[S any](targets ...LockTarget[S]) RowLock[S] {
	return RowLock[S]{core: ormcore.LockTargets(ormcore.RowLock{Spec: querycore.LockSpec{Strength: querycore.LockUpdate}}, lockTargets(targets))}
}

// ForUpdate locks eligible contributing rows; narrow outer joins with Of.
// Example: rows.ForUpdate(rows.Read(report)).All(ctx,tx).
func ForUpdate[S, R any](source ReadSource[S, R]) LockedResult[S, R] {
	return LockRows(source, RowLock[S]{core: ormcore.RowLock{Spec: querycore.LockSpec{Strength: querycore.LockUpdate}}})
}

// NoKeyUpdateLock declares a lock on explicit preserved targets. Example: rows.NoKeyUpdateLock(rows.Target(scope)).
func NoKeyUpdateLock[S any](targets ...LockTarget[S]) RowLock[S] {
	return RowLock[S]{core: ormcore.LockTargets(ormcore.RowLock{Spec: querycore.LockSpec{Strength: querycore.LockNoKeyUpdate}}, lockTargets(targets))}
}

// ForNoKeyUpdate locks eligible contributing rows; narrow outer joins with Of.
// Example: rows.ForNoKeyUpdate(rows.Read(report)).All(ctx,tx).
func ForNoKeyUpdate[S, R any](source ReadSource[S, R]) LockedResult[S, R] {
	return LockRows(source, RowLock[S]{core: ormcore.RowLock{Spec: querycore.LockSpec{Strength: querycore.LockNoKeyUpdate}}})
}

// ShareLock declares a lock on explicit preserved targets. Example: rows.ShareLock(rows.Target(scope)).
func ShareLock[S any](targets ...LockTarget[S]) RowLock[S] {
	return RowLock[S]{core: ormcore.LockTargets(ormcore.RowLock{Spec: querycore.LockSpec{Strength: querycore.LockShare}}, lockTargets(targets))}
}

// ForShare locks eligible contributing rows; narrow outer joins with Of.
// Example: rows.ForShare(rows.Read(report)).All(ctx,tx).
func ForShare[S, R any](source ReadSource[S, R]) LockedResult[S, R] {
	return LockRows(source, RowLock[S]{core: ormcore.RowLock{Spec: querycore.LockSpec{Strength: querycore.LockShare}}})
}

// KeyShareLock declares a lock on explicit preserved targets. Example: rows.KeyShareLock(rows.Target(scope)).
func KeyShareLock[S any](targets ...LockTarget[S]) RowLock[S] {
	return RowLock[S]{core: ormcore.LockTargets(ormcore.RowLock{Spec: querycore.LockSpec{Strength: querycore.LockKeyShare}}, lockTargets(targets))}
}

// ForKeyShare locks eligible contributing rows; narrow outer joins with Of.
// Example: rows.ForKeyShare(rows.Read(report)).All(ctx,tx).
func ForKeyShare[S, R any](source ReadSource[S, R]) LockedResult[S, R] {
	return LockRows(source, RowLock[S]{core: ormcore.RowLock{Spec: querycore.LockSpec{Strength: querycore.LockKeyShare}}})
}

// NoWait sets this clause's wait policy. Example: clause.NoWait().
func (q RowLock[S]) NoWait() RowLock[S] { q.core.Spec.Behavior = querycore.LockNoWait; return q }

// NoWait sets every clause's wait policy. Example: locked.NoWait().
func (q LockedResult[S, R]) NoWait() LockedResult[S, R] {
	q.core = ormcore.RecordLockBehavior(q.core, querycore.LockNoWait)
	return q
}

// SkipLocked sets this clause's wait policy. Example: clause.SkipLocked().
func (q RowLock[S]) SkipLocked() RowLock[S] { q.core.Spec.Behavior = querycore.LockSkip; return q }

// SkipLocked sets every clause's wait policy. Example: locked.SkipLocked().
func (q LockedResult[S, R]) SkipLocked() LockedResult[S, R] {
	q.core = ormcore.RecordLockBehavior(q.core, querycore.LockSkip)
	return q
}

// Wait sets this clause's wait policy. Example: clause.Wait().
func (q RowLock[S]) Wait() RowLock[S] { q.core.Spec.Behavior = querycore.LockWait; return q }

// Wait sets every clause's wait policy. Example: locked.Wait().
func (q LockedResult[S, R]) Wait() LockedResult[S, R] {
	q.core = ormcore.RecordLockBehavior(q.core, querycore.LockWait)
	return q
}

// Of replaces a single clause's targets with preserved scopes. Example: locked.Of(rows.Target(scope)).
func (q LockedResult[S, R]) Of(targets ...LockTarget[S]) LockedResult[S, R] {
	q.core = ormcore.RecordLockTargets(q.core, lockTargets(targets))
	return q
}

// Where filters contributing rows. Example: locked.Where(id.Ne(zero)).
func (q LockedResult[S, R]) Where(ps ...expr.Predicate[S]) LockedResult[S, R] {
	q.core.Source = whereSource(q.core.Source, ps)
	return q
}

// OrderBy orders contributing rows. Example: locked.OrderBy(id.Asc()).
func (q LockedResult[S, R]) OrderBy(os ...expr.ProjectionOrder[S]) LockedResult[S, R] {
	q.core.Source = recordOrders(q.core.Source, os)
	return q
}

// Limit bounds selected rows. Example: locked.Limit(10).
func (q LockedResult[S, R]) Limit(n int) LockedResult[S, R] {
	q.core.Source.Node.Limit = value.Set(n)
	return q
}

// Offset skips selected rows. Example: locked.Offset(10).
func (q LockedResult[S, R]) Offset(n int) LockedResult[S, R] { q.core.Source.Node.Offset = n; return q }

// All requires a transaction and discards partial results. Example: locked.All(ctx,tx).
func (q LockedResult[S, R]) All(ctx context.Context, tx *database.Tx) ([]R, error) {
	return readRecord[R](ctx, tx, q.core, true)
}

// First preserves an omitted result when no row matches. Example: locked.First(ctx,tx).
func (q LockedResult[S, R]) First(ctx context.Context, tx *database.Tx) (value.Optional[R], error) {
	return firstRecord[R](ctx, tx, q.core, true)
}

// RequireFirst reports database.NotFound when no row matches. Example: locked.RequireFirst(ctx,tx).
func (q LockedResult[S, R]) RequireFirst(ctx context.Context, tx *database.Tx) (R, error) {
	return requireRecord[R](ctx, tx, q.core, true)
}
