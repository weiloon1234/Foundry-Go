package ormcore

import (
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
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
	r.Source.Err = safeerror.Join("operation failed", r.Source.Err, ValidateRecordSource(r, r.Record.Type, r.Transaction))
	r.Source = LockSource(r.Source, locks)
	r.Transaction = true
	r.Direct = false
	return r
}

// LockSource is shared by executable records and non-executable correlations.
func LockSource(source Source, locks []RowLock) Source {
	source.Node.Locks = make([]querycore.LockSpec, len(locks))
	if len(locks) == 0 {
		source.Err = safeerror.Join("operation failed", source.Err, Invalid("locked result requires at least one lock clause"))
	}
	for i, lock := range locks {
		source.Err = safeerror.Join("operation failed", source.Err, lock.Err)
		spec := lock.Spec
		if spec.Explicit {
			spec.Targets = make([]string, len(lock.Targets))
		}
		for j, t := range lock.Targets {
			source.Err = safeerror.Join("operation failed", source.Err, t.Err, CheckReferences(source, []Reference{t.Reference}))
			spec.Targets[j] = t.Table
		}
		source.Node.Locks[i] = spec
	}
	return source
}

func RecordLockBehavior(r RecordSource, behavior querycore.LockBehavior) RecordSource {
	r.Source = SourceLockBehavior(r.Source, behavior)
	return r
}
func SourceLockBehavior(s Source, behavior querycore.LockBehavior) Source {
	if len(s.Node.Locks) == 0 {
		s.Err = safeerror.Join("operation failed", s.Err, Invalid("lock policy requires at least one lock clause"))
		return s
	}
	s.Node.Locks = slices.Clone(s.Node.Locks)
	for i := range s.Node.Locks {
		s.Node.Locks[i].Behavior = behavior
	}
	return s
}
func RecordLockTargets(r RecordSource, targets []Scope) RecordSource {
	r.Source = SourceLockTargets(r.Source, targets)
	return r
}
func SourceLockTargets(s Source, targets []Scope) Source {
	if len(s.Node.Locks) != 1 {
		s.Err = safeerror.Join("operation failed", s.Err, Invalid("Of requires a single lock clause"))
		return s
	}
	return LockSource(s, []RowLock{LockTargets(RowLock{Spec: s.Node.Locks[0]}, targets)})
}
