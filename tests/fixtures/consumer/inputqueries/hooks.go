package inputqueries

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/value"
)

// State belongs to one fixture operation. Captured changes contain stored
// snapshots; BeforeInput is test-only evidence of the typed before-hook draft.
type State struct {
	Inspect     func(*orm.Write[Member]) error
	BeforeInput value.Optional[EmailInput]
	ORMCaptured []orm.Changes[Member]
	VetoAfter   bool
}

type stateKey struct{}

func WithState(ctx context.Context, state *State) context.Context {
	return context.WithValue(ctx, stateKey{}, state)
}
