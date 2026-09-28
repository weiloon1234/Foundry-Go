package query

import "slices"

// RowLock declares one lock clause over preserved inputs of scope S.
// Clause targets and wait behavior stay independent of other clauses.
type RowLock[S any] struct {
	_    [0]*S
	spec lockSpec
}

// UpdateLock requests FOR UPDATE on the specified non-nullable input scopes.
// A descriptor must name at least one target; use ForUpdate for all inputs.
func UpdateLock[S any](targets ...LockTarget[S]) RowLock[S] {
	return RowLock[S]{spec: lockOf(lockSpec{strength: lockUpdate}, targets)}
}

// NoKeyUpdateLock requests FOR NO KEY UPDATE on the specified input scopes.
func NoKeyUpdateLock[S any](targets ...LockTarget[S]) RowLock[S] {
	return RowLock[S]{spec: lockOf(lockSpec{strength: lockNoKeyUpdate}, targets)}
}

// ShareLock requests FOR SHARE on the specified input scopes.
func ShareLock[S any](targets ...LockTarget[S]) RowLock[S] {
	return RowLock[S]{spec: lockOf(lockSpec{strength: lockShare}, targets)}
}

// KeyShareLock requests FOR KEY SHARE on the specified input scopes.
func KeyShareLock[S any](targets ...LockTarget[S]) RowLock[S] {
	return RowLock[S]{spec: lockOf(lockSpec{strength: lockKeyShare}, targets)}
}

// NoWait reports conflicting row locks immediately for this clause.
func (l RowLock[S]) NoWait() RowLock[S] { l.spec.behavior = lockNoWait; return l }

// SkipLocked skips conflicting row locks for this clause.
func (l RowLock[S]) SkipLocked() RowLock[S] { l.spec.behavior = lockSkip; return l }

// Wait restores ordinary context-bounded waiting for this clause.
func (l RowLock[S]) Wait() RowLock[S] { l.spec.behavior = lockWait; return l }

// LockRows selects explicit per-input clauses and requires a transaction for
// execution. PostgreSQL combines overlapping clauses using its strongest lock
// and wait behavior. Empty clauses and nullable/foreign inputs are invalid.
func (q ProjectionQuery[S, R]) LockRows(clauses ...RowLock[S]) LockedResult[S, R] {
	locks := make([]lockSpec, len(clauses))
	for i, clause := range clauses {
		locks[i] = clause.spec
	}
	return LockedResult[S, R]{query: q, locks: locks}
}

// LockRows retains the selected value while applying per-input row locks.
func (q ValueQuery[S, V]) LockRows(clauses ...RowLock[S]) LockedResult[S, V] {
	return q.query.LockRows(clauses...)
}

func withLockBehavior(specs []lockSpec, behavior lockBehavior) []lockSpec {
	specs = slices.Clone(specs)
	for i := range specs {
		specs[i].behavior = behavior
	}
	return specs
}
