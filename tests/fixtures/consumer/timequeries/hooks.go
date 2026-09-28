package timequeries

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/value"
)

// State is per-operation test instrumentation; model changes contain stored
// snapshots while Before records the supplied draft before time conventions.
type State struct {
	Before    MemberDraft
	Changes   []MemberChanges
	Committed []lifecycle.Operation
	VetoAfter bool
}

type stateKey struct{}

func WithState(ctx context.Context, state *State) context.Context {
	return context.WithValue(ctx, stateKey{}, state)
}

func memberHooks() MemberHooks {
	var state *State
	after := func(ctx context.Context, _ *database.Tx, changes MemberChanges) error {
		state, _ = ctx.Value(stateKey{}).(*State)
		if state != nil {
			state.Changes = append(state.Changes, changes)
			if state.VetoAfter {
				return Veto
			}
		}
		return nil
	}
	return MemberHooks{
		Saving: func(ctx context.Context, _ *database.Tx, _ value.Optional[Member], draft *MemberDraft) error {
			state, _ = ctx.Value(stateKey{}).(*State)
			if state != nil {
				state.Before = *draft
			}
			return nil
		},
		Creating: func(_ context.Context, _ *database.Tx, draft *MemberDraft) error {
			if !draft.Name().IsSet() {
				*draft = draft.SetName(" default ")
			}
			return nil
		},
		Created: after,
		Updated: after,
		Deleted: after,
		AfterCommit: func(_ context.Context, operation lifecycle.Operation, _ MemberChanges) error {
			if state != nil {
				state.Committed = append(state.Committed, operation)
			}
			return nil
		},
	}
}
