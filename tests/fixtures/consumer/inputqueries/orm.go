package inputqueries

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

// MemberTable separates fresh inputs from stored values. Example: Members.Email.Set(EmailInput{Address:"Ada@example.test"}).
type MemberTable struct {
	orm.Table[Member, model.ID[Member]]
	Email orm.InputText[Member, EmailInput, StoredEmail]
	Note  orm.NullableInputText[Member, LabelInput, StoredLabel]
}

// Members preserves omitted/null/value inputs and explicit AccessEmail presentation.
// Example: Members.Query().Where(Members.Email.Eq(StoredEmail("ada@example.test"))).
var Members = declareMembers()

func declareMembers() MemberTable {
	var fields MemberTable
	fields = orm.Define("input_members", func(b orm.Builder[Member], m *Member) MemberTable {
		return MemberTable{orm.TableOf(b, &m.ID), orm.InputTextOf[Member, EmailInput](b, &m.Email, nil), orm.NullableInputTextOf[Member, LabelInput](b, &m.Note, nil)}
	}, orm.WithHooks(func() orm.Hooks[Member] { return ormMemberHooks(fields) }))
	return fields
}

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
				state.BeforeInput = orm.WriteGet(draft, fields.Email.Set)
			}
			return nil
		},
		Creating: func(_ context.Context, _ *database.Tx, draft *orm.Write[Member]) error {
			if !orm.WriteIsSet(draft, fields.Email.Set) {
				return orm.WriteSet(draft, fields.Email.Set(EmailInput{Address: " DEFAULT@EXAMPLE.TEST "}))
			}
			return nil
		}, Created: after, Updated: after, Deleted: after,
	}
}
