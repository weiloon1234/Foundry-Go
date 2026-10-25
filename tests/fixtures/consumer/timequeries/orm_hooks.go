package timequeries

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/value"
)

func ormMemberHooks(fields MemberTable) orm.Hooks[Member] {
	var state *State
	after := func(ctx context.Context, _ *database.Tx, changes orm.Changes[Member]) error {
		state, _ = ctx.Value(stateKey{}).(*State)
		if state != nil {
			state.ORMChanges = append(state.ORMChanges, changes)
			if state.VetoAfter {
				return Veto
			}
		}
		return nil
	}
	return orm.Hooks[Member]{
		Saving: func(ctx context.Context, _ *database.Tx, _ value.Optional[Member], draft *orm.Write[Member]) error {
			state, _ = ctx.Value(stateKey{}).(*State)
			if state != nil {
				state.ORMCreatedAt = orm.WriteGet(draft, fields.CreatedAt.Set)
				state.ORMUpdatedAt = orm.WriteGet(draft, fields.UpdatedAt.Set)
			}
			return nil
		},
		Creating: func(_ context.Context, _ *database.Tx, draft *orm.Write[Member]) error {
			if !orm.WriteIsSet(draft, fields.Name.Set) {
				return orm.WriteSet(draft, fields.Name.Set(" default "))
			}
			return nil
		},
		Created: after,
		Updated: after,
		Deleted: after,
		AfterCommit: func(_ context.Context, operation lifecycle.Operation, _ orm.Changes[Member]) error {
			if state != nil {
				state.Committed = append(state.Committed, operation)
			}
			return nil
		},
	}
}
