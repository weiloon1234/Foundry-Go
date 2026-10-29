package timequeries

import temporal "github.com/weiloon1234/Foundry-Go/temporal"
import time "time"

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
				state.ORMCreatedAt = orm.WriteGet(draft, func() func(arg0 time.Time) orm.Assignment[Member] {
					receiver := fields.CreatedAt
					return func(arg0 time.Time) orm.Assignment[Member] { return orm.TimeSet(receiver, arg0) }
				}())
				state.ORMUpdatedAt = orm.WriteGet(draft, func() func(arg0 temporal.DateTime) orm.Assignment[Member] {
					receiver := fields.UpdatedAt
					return func(arg0 temporal.DateTime) orm.Assignment[Member] { return orm.TimeSet(receiver, arg0) }
				}())
			}
			return nil
		},
		Creating: func(_ context.Context, _ *database.Tx, draft *orm.Write[Member]) error {
			if !orm.WriteIsSet(draft, func() func(arg0 string) orm.Assignment[Member] {
				receiver := fields.Name
				return func(arg0 string) orm.Assignment[Member] { return orm.InputTextSet(receiver, arg0) }
			}()) {
				return orm.WriteSet(draft, orm.InputTextSet(fields.Name, " default "))
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
