package query

import "github.com/weiloon1234/Foundry-Go/fault"

// Correlated SELECTs never execute independently. Store their locks on their
// SELECT nodes while reusing the normal locked-result validation and policies.
func lockedCorrelatedSelect[S, R any](locked LockedResult[S, R]) ProjectionQuery[S, R] {
	if len(locked.locks) == 0 && locked.query.source.err == nil {
		locked.query.source.err = fault.New(fault.Invalid, "correlated lock policy requires at least one lock clause")
	}
	locked.query.source.node.locks = locked.locks
	return locked.query
}
func correlatedSelectLocks[S, R any](q ProjectionQuery[S, R]) LockedResult[S, R] {
	return LockedResult[S, R]{query: q, locks: q.source.node.locks}
}

// ForUpdate replaces this correlated SELECT's lock while preserving source locks.
func (q TransactionCorrelatedRecordQuery[O, I, R]) ForUpdate() TransactionCorrelatedRecordQuery[O, I, R] {
	q.query = lockedCorrelatedSelect(q.query.ForUpdate())
	return q
}

// ForNoKeyUpdate replaces this correlated SELECT's lock while preserving source locks.
func (q TransactionCorrelatedRecordQuery[O, I, R]) ForNoKeyUpdate() TransactionCorrelatedRecordQuery[O, I, R] {
	q.query = lockedCorrelatedSelect(q.query.ForNoKeyUpdate())
	return q
}

// ForShare replaces this correlated SELECT's lock while preserving source locks.
func (q TransactionCorrelatedRecordQuery[O, I, R]) ForShare() TransactionCorrelatedRecordQuery[O, I, R] {
	q.query = lockedCorrelatedSelect(q.query.ForShare())
	return q
}

// ForKeyShare replaces this correlated SELECT's lock while preserving source locks.
func (q TransactionCorrelatedRecordQuery[O, I, R]) ForKeyShare() TransactionCorrelatedRecordQuery[O, I, R] {
	q.query = lockedCorrelatedSelect(q.query.ForKeyShare())
	return q
}

// LockRows declares independent input policies for this correlated SELECT.
func (q TransactionCorrelatedRecordQuery[O, I, R]) LockRows(clauses ...RowLock[TransactionCorrelation[O, I]]) TransactionCorrelatedRecordQuery[O, I, R] {
	q.query = lockedCorrelatedSelect(q.query.LockRows(clauses...))
	return q
}

// Of derives an existing correlated lock policy; a missing lock is invalid.
func (q TransactionCorrelatedRecordQuery[O, I, R]) Of(targets ...LockTarget[TransactionCorrelation[O, I]]) TransactionCorrelatedRecordQuery[O, I, R] {
	q.query = lockedCorrelatedSelect(correlatedSelectLocks(q.query).Of(targets...))
	return q
}

// NoWait derives an existing correlated lock policy; a missing lock is invalid.
func (q TransactionCorrelatedRecordQuery[O, I, R]) NoWait() TransactionCorrelatedRecordQuery[O, I, R] {
	q.query = lockedCorrelatedSelect(correlatedSelectLocks(q.query).NoWait())
	return q
}

// SkipLocked derives an existing correlated lock policy; a missing lock is invalid.
func (q TransactionCorrelatedRecordQuery[O, I, R]) SkipLocked() TransactionCorrelatedRecordQuery[O, I, R] {
	q.query = lockedCorrelatedSelect(correlatedSelectLocks(q.query).SkipLocked())
	return q
}

// Wait derives an existing correlated lock policy; a missing lock is invalid.
func (q TransactionCorrelatedRecordQuery[O, I, R]) Wait() TransactionCorrelatedRecordQuery[O, I, R] {
	q.query = lockedCorrelatedSelect(correlatedSelectLocks(q.query).Wait())
	return q
}

// ForUpdate replaces this correlated SELECT's lock while preserving source locks.
func (q TransactionCorrelatedValueQuery[O, I, V]) ForUpdate() TransactionCorrelatedValueQuery[O, I, V] {
	q.query.query = lockedCorrelatedSelect(q.query.query.ForUpdate())
	return q
}

// ForNoKeyUpdate replaces this correlated SELECT's lock while preserving source locks.
func (q TransactionCorrelatedValueQuery[O, I, V]) ForNoKeyUpdate() TransactionCorrelatedValueQuery[O, I, V] {
	q.query.query = lockedCorrelatedSelect(q.query.query.ForNoKeyUpdate())
	return q
}

// ForShare replaces this correlated SELECT's lock while preserving source locks.
func (q TransactionCorrelatedValueQuery[O, I, V]) ForShare() TransactionCorrelatedValueQuery[O, I, V] {
	q.query.query = lockedCorrelatedSelect(q.query.query.ForShare())
	return q
}

// ForKeyShare replaces this correlated SELECT's lock while preserving source locks.
func (q TransactionCorrelatedValueQuery[O, I, V]) ForKeyShare() TransactionCorrelatedValueQuery[O, I, V] {
	q.query.query = lockedCorrelatedSelect(q.query.query.ForKeyShare())
	return q
}

// LockRows declares independent input policies for this correlated SELECT.
func (q TransactionCorrelatedValueQuery[O, I, V]) LockRows(clauses ...RowLock[TransactionCorrelation[O, I]]) TransactionCorrelatedValueQuery[O, I, V] {
	q.query.query = lockedCorrelatedSelect(q.query.query.LockRows(clauses...))
	return q
}

// Of derives an existing correlated lock policy; a missing lock is invalid.
func (q TransactionCorrelatedValueQuery[O, I, V]) Of(targets ...LockTarget[TransactionCorrelation[O, I]]) TransactionCorrelatedValueQuery[O, I, V] {
	q.query.query = lockedCorrelatedSelect(correlatedSelectLocks(q.query.query).Of(targets...))
	return q
}

// NoWait derives an existing correlated lock policy; a missing lock is invalid.
func (q TransactionCorrelatedValueQuery[O, I, V]) NoWait() TransactionCorrelatedValueQuery[O, I, V] {
	q.query.query = lockedCorrelatedSelect(correlatedSelectLocks(q.query.query).NoWait())
	return q
}

// SkipLocked derives an existing correlated lock policy; a missing lock is invalid.
func (q TransactionCorrelatedValueQuery[O, I, V]) SkipLocked() TransactionCorrelatedValueQuery[O, I, V] {
	q.query.query = lockedCorrelatedSelect(correlatedSelectLocks(q.query.query).SkipLocked())
	return q
}

// Wait derives an existing correlated lock policy; a missing lock is invalid.
func (q TransactionCorrelatedValueQuery[O, I, V]) Wait() TransactionCorrelatedValueQuery[O, I, V] {
	q.query.query = lockedCorrelatedSelect(correlatedSelectLocks(q.query.query).Wait())
	return q
}
