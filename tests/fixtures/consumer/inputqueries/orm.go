package inputqueries

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"

	"github.com/weiloon1234/Foundry-Go/value"
)

var memberFields MemberTable

func init() { memberFields = Members }

func memberHooks() orm.Hooks[Member] { return ormMemberHooks(memberFields) }

func ormMemberHooks(fields MemberTable) orm.Hooks[Member] {
	after := func(ctx context.Context, _ *database.Tx, changes orm.Changes[Member]) error {
		if state, _ := ctx.Value(stateKey{}).(*State); state != nil {
			state.ORMCaptured = append(state.ORMCaptured, changes)
			if state.VetoAfter {
				return Veto
			}
		}
		return nil
	}
	return orm.Hooks[Member]{
		Saving: func(ctx context.Context, _ *database.Tx, _ value.Optional[Member], draft *orm.Write[Member]) error {
			if state, _ := ctx.Value(stateKey{}).(*State); state != nil {
				if state.Inspect != nil {
					if err := state.Inspect(draft); err != nil {
						return err
					}
				}
				state.BeforeInput = orm.WriteGet(draft, func() func(arg0 EmailInput) orm.Assignment[Member] {
					receiver := fields.Email
					return func(arg0 EmailInput) orm.Assignment[Member] { return orm.Set(receiver, arg0) }
				}())
			}
			return nil
		},
		Creating: func(_ context.Context, _ *database.Tx, draft *orm.Write[Member]) error {
			if !orm.WriteIsSet(draft, func() func(arg0 EmailInput) orm.Assignment[Member] {
				receiver := fields.Email
				return func(arg0 EmailInput) orm.Assignment[Member] { return orm.Set(receiver, arg0) }
			}()) {
				return orm.WriteSet(draft, orm.Set(fields.Email, EmailInput{Address: " DEFAULT@EXAMPLE.TEST "}))
			}
			return nil
		}, Created: after, Updated: after, Deleted: after,
	}
}
