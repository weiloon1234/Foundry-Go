package timequeries

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"time"

	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/value"
)

// State is per-operation test instrumentation; model changes contain stored
// snapshots while ORMCreatedAt and ORMUpdatedAt record inputs before conventions.
type State struct {
	ORMCreatedAt value.Optional[time.Time]
	ORMUpdatedAt value.Optional[temporal.DateTime]
	ORMChanges   []orm.Changes[Member]
	Committed    []lifecycle.Operation
	VetoAfter    bool
}

type stateKey struct{}

func WithState(ctx context.Context, state *State) context.Context {
	return context.WithValue(ctx, stateKey{}, state)
}
