package query

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

type lockStrength = querycore.LockStrength

const lockUpdate = querycore.LockUpdate

const lockNoKeyUpdate = querycore.LockNoKeyUpdate

const lockShare = querycore.LockShare

const lockKeyShare = querycore.LockKeyShare

type lockBehavior = querycore.LockBehavior

const lockWait = querycore.LockWait

const lockNoWait = querycore.LockNoWait

const lockSkip = querycore.LockSkip

type lockSpec = querycore.LockSpec

func lockSources(s selectNode) ([]tableSource, []bool) { return querycore.LockSources(s) }

// LockTarget identifies a non-nullable source in input scope S. Pass a source's
// Scope or a preserved join-side scope; nullable outer-join scopes do not qualify.
type LockTarget[S any] interface{ lockTarget() lockTarget[S] }

type lockTarget[S any] struct {
	_     [0]*S
	table string
}

func (s RecordScope[S, M]) lockTarget() lockTarget[S] { return lockTarget[S]{table: s.table} }

func lockOf[S any](spec lockSpec, targets []LockTarget[S]) lockSpec {
	spec.Explicit = true
	spec.Targets = make([]string, len(targets))
	for i, target := range targets {
		if !nilDescriptor(target) {
			spec.Targets[i] = target.lockTarget().table
		}
	}
	return spec
}

func lockContext(ctx context.Context, tx *database.Tx) error {
	if tx == nil {
		return safeerror.NewFault(fault.Invalid, "row locking requires an active transaction")
	}
	return executionContext(ctx, tx)
}
