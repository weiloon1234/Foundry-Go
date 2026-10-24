package rows

import (
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

func rowLocks[S any](clauses []RowLock[S]) []ormcore.RowLock {
	out := make([]ormcore.RowLock, len(clauses))
	for i, c := range clauses {
		out[i] = c.core
	}
	return out
}

// ForUpdate retains this correlated SELECT’s lock without execution. Example: selected.ForUpdate().
func (q TransactionCorrelatedRecord[O, I, R]) ForUpdate() TransactionCorrelatedRecord[O, I, R] {
	q.core.Correlation.Source = ormcore.LockSource(q.core.Correlation.Source, []ormcore.RowLock{{Spec: querycore.LockSpec{Strength: querycore.LockUpdate}}})
	return q
}

// ForNoKeyUpdate retains this correlated SELECT’s lock without execution. Example: selected.ForNoKeyUpdate().
func (q TransactionCorrelatedRecord[O, I, R]) ForNoKeyUpdate() TransactionCorrelatedRecord[O, I, R] {
	q.core.Correlation.Source = ormcore.LockSource(q.core.Correlation.Source, []ormcore.RowLock{{Spec: querycore.LockSpec{Strength: querycore.LockNoKeyUpdate}}})
	return q
}

// ForShare retains this correlated SELECT’s lock without execution. Example: selected.ForShare().
func (q TransactionCorrelatedRecord[O, I, R]) ForShare() TransactionCorrelatedRecord[O, I, R] {
	q.core.Correlation.Source = ormcore.LockSource(q.core.Correlation.Source, []ormcore.RowLock{{Spec: querycore.LockSpec{Strength: querycore.LockShare}}})
	return q
}

// ForKeyShare retains this correlated SELECT’s lock without execution. Example: selected.ForKeyShare().
func (q TransactionCorrelatedRecord[O, I, R]) ForKeyShare() TransactionCorrelatedRecord[O, I, R] {
	q.core.Correlation.Source = ormcore.LockSource(q.core.Correlation.Source, []ormcore.RowLock{{Spec: querycore.LockSpec{Strength: querycore.LockKeyShare}}})
	return q
}

// LockRows sets independent preserved-input locks. Example: selected.LockRows(rows.ShareLock(target)).
func (q TransactionCorrelatedRecord[O, I, R]) LockRows(cs ...RowLock[TransactionCorrelation[O, I]]) TransactionCorrelatedRecord[O, I, R] {
	q.core.Correlation.Source = ormcore.LockSource(q.core.Correlation.Source, rowLocks(cs))
	return q
}

// Of narrows one existing lock to preserved scopes. Example: selected.ForUpdate().Of(target).
func (q TransactionCorrelatedRecord[O, I, R]) Of(ts ...LockTarget[TransactionCorrelation[O, I]]) TransactionCorrelatedRecord[O, I, R] {
	q.core.Correlation.Source = ormcore.SourceLockTargets(q.core.Correlation.Source, lockTargets(ts))
	return q
}

// NoWait derives an existing lock policy. Example: selected.ForUpdate().NoWait().
func (q TransactionCorrelatedRecord[O, I, R]) NoWait() TransactionCorrelatedRecord[O, I, R] {
	q.core.Correlation.Source = ormcore.SourceLockBehavior(q.core.Correlation.Source, querycore.LockNoWait)
	return q
}

// SkipLocked derives an existing lock policy. Example: selected.ForUpdate().SkipLocked().
func (q TransactionCorrelatedRecord[O, I, R]) SkipLocked() TransactionCorrelatedRecord[O, I, R] {
	q.core.Correlation.Source = ormcore.SourceLockBehavior(q.core.Correlation.Source, querycore.LockSkip)
	return q
}

// Wait derives an existing lock policy. Example: selected.ForUpdate().Wait().
func (q TransactionCorrelatedRecord[O, I, R]) Wait() TransactionCorrelatedRecord[O, I, R] {
	q.core.Correlation.Source = ormcore.SourceLockBehavior(q.core.Correlation.Source, querycore.LockWait)
	return q
}

// ForUpdate retains this correlated SELECT’s lock without execution. Example: selected.ForUpdate().
func (q TransactionCorrelatedScalar[O, I, V]) ForUpdate() TransactionCorrelatedScalar[O, I, V] {
	q.core.Source = ormcore.LockSource(q.core.Source, []ormcore.RowLock{{Spec: querycore.LockSpec{Strength: querycore.LockUpdate}}})
	return q
}

// ForNoKeyUpdate retains this correlated SELECT’s lock without execution. Example: selected.ForNoKeyUpdate().
func (q TransactionCorrelatedScalar[O, I, V]) ForNoKeyUpdate() TransactionCorrelatedScalar[O, I, V] {
	q.core.Source = ormcore.LockSource(q.core.Source, []ormcore.RowLock{{Spec: querycore.LockSpec{Strength: querycore.LockNoKeyUpdate}}})
	return q
}

// ForShare retains this correlated SELECT’s lock without execution. Example: selected.ForShare().
func (q TransactionCorrelatedScalar[O, I, V]) ForShare() TransactionCorrelatedScalar[O, I, V] {
	q.core.Source = ormcore.LockSource(q.core.Source, []ormcore.RowLock{{Spec: querycore.LockSpec{Strength: querycore.LockShare}}})
	return q
}

// ForKeyShare retains this correlated SELECT’s lock without execution. Example: selected.ForKeyShare().
func (q TransactionCorrelatedScalar[O, I, V]) ForKeyShare() TransactionCorrelatedScalar[O, I, V] {
	q.core.Source = ormcore.LockSource(q.core.Source, []ormcore.RowLock{{Spec: querycore.LockSpec{Strength: querycore.LockKeyShare}}})
	return q
}

// LockRows sets independent preserved-input locks. Example: selected.LockRows(rows.ShareLock(target)).
func (q TransactionCorrelatedScalar[O, I, V]) LockRows(cs ...RowLock[TransactionCorrelation[O, I]]) TransactionCorrelatedScalar[O, I, V] {
	q.core.Source = ormcore.LockSource(q.core.Source, rowLocks(cs))
	return q
}

// Of narrows one existing lock to preserved scopes. Example: selected.ForUpdate().Of(target).
func (q TransactionCorrelatedScalar[O, I, V]) Of(ts ...LockTarget[TransactionCorrelation[O, I]]) TransactionCorrelatedScalar[O, I, V] {
	q.core.Source = ormcore.SourceLockTargets(q.core.Source, lockTargets(ts))
	return q
}

// NoWait derives an existing lock policy. Example: selected.ForUpdate().NoWait().
func (q TransactionCorrelatedScalar[O, I, V]) NoWait() TransactionCorrelatedScalar[O, I, V] {
	q.core.Source = ormcore.SourceLockBehavior(q.core.Source, querycore.LockNoWait)
	return q
}

// SkipLocked derives an existing lock policy. Example: selected.ForUpdate().SkipLocked().
func (q TransactionCorrelatedScalar[O, I, V]) SkipLocked() TransactionCorrelatedScalar[O, I, V] {
	q.core.Source = ormcore.SourceLockBehavior(q.core.Source, querycore.LockSkip)
	return q
}

// Wait derives an existing lock policy. Example: selected.ForUpdate().Wait().
func (q TransactionCorrelatedScalar[O, I, V]) Wait() TransactionCorrelatedScalar[O, I, V] {
	q.core.Source = ormcore.SourceLockBehavior(q.core.Source, querycore.LockWait)
	return q
}

// ForUpdate retains a scalar SELECT lock for composition. Example: rows.TransactionScalarOf(selected).ForUpdate().
func (q TransactionScalar[S, V]) ForUpdate() TransactionScalar[S, V] {
	q.core = ormcore.LockRecord(q.core, []ormcore.RowLock{{Spec: querycore.LockSpec{Strength: querycore.LockUpdate}}})
	return q
}

// ForNoKeyUpdate retains a scalar SELECT lock for composition. Example: rows.TransactionScalarOf(selected).ForNoKeyUpdate().
func (q TransactionScalar[S, V]) ForNoKeyUpdate() TransactionScalar[S, V] {
	q.core = ormcore.LockRecord(q.core, []ormcore.RowLock{{Spec: querycore.LockSpec{Strength: querycore.LockNoKeyUpdate}}})
	return q
}

// ForShare retains a scalar SELECT lock for composition. Example: rows.TransactionScalarOf(selected).ForShare().
func (q TransactionScalar[S, V]) ForShare() TransactionScalar[S, V] {
	q.core = ormcore.LockRecord(q.core, []ormcore.RowLock{{Spec: querycore.LockSpec{Strength: querycore.LockShare}}})
	return q
}

// ForKeyShare retains a scalar SELECT lock for composition. Example: rows.TransactionScalarOf(selected).ForKeyShare().
func (q TransactionScalar[S, V]) ForKeyShare() TransactionScalar[S, V] {
	q.core = ormcore.LockRecord(q.core, []ormcore.RowLock{{Spec: querycore.LockSpec{Strength: querycore.LockKeyShare}}})
	return q
}

// NoWait derives an existing scalar lock policy. Example: selected.ForUpdate().NoWait().
func (q TransactionScalar[S, V]) NoWait() TransactionScalar[S, V] {
	q.core = ormcore.RecordLockBehavior(q.core, querycore.LockNoWait)
	return q
}

// SkipLocked derives an existing scalar lock policy. Example: selected.ForUpdate().SkipLocked().
func (q TransactionScalar[S, V]) SkipLocked() TransactionScalar[S, V] {
	q.core = ormcore.RecordLockBehavior(q.core, querycore.LockSkip)
	return q
}

// Wait derives an existing scalar lock policy. Example: selected.ForUpdate().Wait().
func (q TransactionScalar[S, V]) Wait() TransactionScalar[S, V] {
	q.core = ormcore.RecordLockBehavior(q.core, querycore.LockWait)
	return q
}

// LockRows applies independent locks to a scalar subquery. Example: selected.LockRows(rows.ShareLock(target)).
func (q TransactionScalar[S, V]) LockRows(cs ...RowLock[S]) TransactionScalar[S, V] {
	q.core = ormcore.LockRecord(q.core, rowLocks(cs))
	return q
}

// Of narrows one scalar lock to preserved inputs. Example: selected.ForUpdate().Of(target).
func (q TransactionScalar[S, V]) Of(ts ...LockTarget[S]) TransactionScalar[S, V] {
	q.core = ormcore.RecordLockTargets(q.core, lockTargets(ts))
	return q
}
