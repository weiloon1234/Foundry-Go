package query

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/database"
)

// Caller-owned update drafts share the same recursion boundary whether the
// enclosing operation selects one model or a bounded per-model batch.
func modelUpdateMutation[M any](ctx context.Context, tx *database.Tx, current M, draft func(context.Context, *database.Tx, M) (Mutation[M], error)) (Mutation[M], error) {
	callbackContext, err := writeHookContext(ctx)
	if err != nil {
		return Mutation[M]{}, err
	}
	return draft(callbackContext, tx, current)
}
