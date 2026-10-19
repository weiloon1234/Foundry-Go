package query

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database"
)

// Restore clears the configured deletion instant on one soft-deleted model.
// It selects deleted records, retains explicit predicates and requires a primary
// key equality. Generated Restore methods supply the concrete model key.
func (q Query[M]) Restore(ctx context.Context, writer database.Transactor) (M, error) {
	return executeMutation(ctx, writer, mutationPlan[M]{query: q.OnlyTrashed(), kind: restoreModel})
}

// ForceRemove physically removes one configured soft-delete model within the
// current visibility and explicit filters. WithTrashed includes deleted rows;
// generated ForceDelete methods supply the concrete model key.
func (q Query[M]) ForceRemove(ctx context.Context, writer database.Transactor) (M, error) {
	return executeMutation(ctx, writer, mutationPlan[M]{query: q, kind: forceDeleteModel})
}
