package orm

import (
	"errors"

	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
)

// FoundryConflictData supplies policy data to the expression owner. Example: FoundryConflictData(policy,sqlowner.Expressions()).
func FoundryConflictData[M any](policy Conflict[M], seal sqlowner.ExpressionSeal) ormcore.ConflictPolicy {
	if !seal.Valid() {
		return ormcore.ConflictPolicy{Err: invalid("invalid SQL owner capability")}
	}
	var owner any
	if policy.state.owner != nil {
		owner = policy.state.owner
	}
	return ormcore.ConflictPolicy{Policy: policy.state.core, Owner: owner, Err: policy.state.err}
}

// FoundryConflict binds a framework-built policy to its declaration. Example: FoundryConflict[User](metadata,sqlowner.Expressions()).
func FoundryConflict[M any](metadata ormcore.ConflictPolicy, seal sqlowner.ExpressionSeal) Conflict[M] {
	if !seal.Valid() {
		return Conflict[M]{state: conflictPolicy{err: invalid("invalid SQL owner capability")}}
	}
	owner, ok := metadata.Owner.(*tableCore)
	if metadata.Owner != nil && !ok {
		metadata.Err = errors.Join(metadata.Err, invalid("invalid conflict declaration"))
	}
	return Conflict[M]{state: conflictPolicy{core: metadata.Policy, owner: owner, err: metadata.Err}}
}

// FoundryConflictUpdate binds a framework-built calculation to its destination. Example: FoundryConflictUpdate[User](metadata,sqlowner.Expressions()).
func FoundryConflictUpdate[M any](metadata ormcore.ConflictAssignment, seal sqlowner.ExpressionSeal) ConflictUpdate[M] {
	if !seal.Valid() {
		return ConflictUpdate[M]{state: conflictUpdate{err: invalid("invalid SQL owner capability")}}
	}
	owner, ok := metadata.Owner.(*tableCore)
	if !ok || owner == nil {
		metadata.Err = errors.Join(metadata.Err, invalid("invalid conflict destination"))
	}
	return ConflictUpdate[M]{state: conflictUpdate{update: metadata.Update, owner: owner, err: metadata.Err}}
}
