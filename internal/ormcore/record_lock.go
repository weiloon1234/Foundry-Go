package ormcore

import (
	"errors"
	"slices"

	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

type RowLock struct {
	Spec    querycore.LockSpec
	Targets []Scope
	Err     error
}

func LockTargets(lock RowLock, targets []Scope) RowLock {
	lock.Targets = slices.Clone(targets)
	lock.Spec.Explicit = true
	return lock
}
func LockRecord(r RecordSource, locks []RowLock) RecordSource {
	r.Source.Err = errors.Join(r.Source.Err, ValidateRecordSource(r, r.Record.Type, r.Transaction))
	r.Source.Node.Locks = make([]querycore.LockSpec, len(locks))
	if len(locks) == 0 {
		r.Source.Err = errors.Join(r.Source.Err, Invalid("locked result requires at least one lock clause"))
	}
	for i, lock := range locks {
		r.Source.Err = errors.Join(r.Source.Err, lock.Err)
		spec := lock.Spec
		if spec.Explicit {
			spec.Targets = make([]string, len(lock.Targets))
		}
		for j, t := range lock.Targets {
			r.Source.Err = errors.Join(r.Source.Err, t.Err, CheckReferences(r.Source, []Reference{t.Reference}))
			spec.Targets[j] = t.Table
		}
		r.Source.Node.Locks[i] = spec
	}
	r.Transaction = true
	r.Direct = false
	return r
}
func RecordLockBehavior(r RecordSource, behavior querycore.LockBehavior) RecordSource {
	r.Source.Node.Locks = slices.Clone(r.Source.Node.Locks)
	for i := range r.Source.Node.Locks {
		r.Source.Node.Locks[i].Behavior = behavior
	}
	return r
}
func RecordLockTargets(r RecordSource, targets []Scope) RecordSource {
	if len(r.Source.Node.Locks) != 1 {
		r.Source.Err = errors.Join(r.Source.Err, Invalid("Of requires a single lock clause"))
		return r
	}
	spec := r.Source.Node.Locks[0]
	return LockRecord(r, []RowLock{LockTargets(RowLock{Spec: spec}, targets)})
}
