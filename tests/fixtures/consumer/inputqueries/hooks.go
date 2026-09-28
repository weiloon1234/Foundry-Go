package inputqueries

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/value"
)

// State belongs to one fixture operation. Captured changes contain stored
// snapshots; BeforeInput is test-only evidence of the typed before-hook draft.
type State struct {
	BeforeInput value.Optional[EmailInput]
	Captured    []MemberChanges
	VetoAfter   bool
}

type stateKey struct{}

func WithState(ctx context.Context, state *State) context.Context {
	return context.WithValue(ctx, stateKey{}, state)
}

func memberHooks() MemberHooks {
	after := func(ctx context.Context, _ *database.Tx, changes MemberChanges) error {
		if state, _ := ctx.Value(stateKey{}).(*State); state != nil {
			state.Captured = append(state.Captured, changes)
			if state.VetoAfter {
				return Veto
			}
		}
		return nil
	}
	return MemberHooks{
		Saving: func(ctx context.Context, _ *database.Tx, _ value.Optional[Member], draft *MemberDraft) error {
			if state, _ := ctx.Value(stateKey{}).(*State); state != nil {
				state.BeforeInput = draft.Email()
			}
			return nil
		},
		Creating: func(_ context.Context, _ *database.Tx, draft *MemberDraft) error {
			if !draft.Email().IsSet() {
				*draft = draft.SetEmail(EmailInput{Address: " DEFAULT@EXAMPLE.TEST "})
			}
			return nil
		},
		Created: after,
		Updated: after,
		Deleted: after,
	}
}
