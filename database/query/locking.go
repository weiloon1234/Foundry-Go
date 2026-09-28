package query

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
)

type lockStrength uint8

const (
	lockUpdate lockStrength = iota + 1
	lockNoKeyUpdate
	lockShare
	lockKeyShare
)

type lockBehavior uint8

const (
	lockWait lockBehavior = iota
	lockNoWait
	lockSkip
)

type lockSpec struct {
	strength lockStrength
	behavior lockBehavior
	targets  []string
	// nil targets means all eligible sources; an explicitly empty Of is invalid.
	explicit bool
}

// LockTarget identifies a non-nullable source in input scope S. Pass a source's
// Scope or a preserved join-side scope; nullable outer-join scopes do not qualify.
type LockTarget[S any] interface{ lockTarget() lockTarget[S] }
type lockTarget[S any] struct {
	_     [0]*S
	table string
}

func (s RecordScope[S, M]) lockTarget() lockTarget[S] { return lockTarget[S]{table: s.table} }

func lockOf[S any](spec lockSpec, targets []LockTarget[S]) lockSpec {
	spec.explicit = true
	spec.targets = make([]string, len(targets))
	for i, target := range targets {
		if !nilDescriptor(target) {
			spec.targets[i] = target.lockTarget().table
		}
	}
	return spec
}

func lockContext(ctx context.Context, tx *database.Tx) error {
	if tx == nil {
		return fault.New(fault.Invalid, "row locking requires an active transaction")
	}
	return executionContext(ctx, tx)
}

// lockSources tracks null extension introduced by every join, including a
// later RIGHT/FULL JOIN that makes an earlier preserved side nullable.
func lockSources(s selectNode) ([]tableSource, []bool) {
	sources := []tableSource{s.source}
	nullable := []bool{false}
	nullablePrefix := 0
	for _, join := range s.joins {
		if join.kind == rightJoin || join.kind == fullJoin {
			nullablePrefix = len(sources)
		}
		sources = append(sources, join.source)
		nullable = append(nullable, join.kind == leftJoin || join.kind == fullJoin)
	}
	for i := 0; i < nullablePrefix; i++ {
		nullable[i] = true
	}
	return sources, nullable
}
