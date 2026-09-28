package query

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
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

// SQL shape and lifecycle intent are distinct: restoration and soft deletion
// write updates without emitting ordinary saving/updating callbacks.
func (kind mutationKind) sqlKind() mutationKind {
	switch kind {
	case softDeleteModel, restoreModel:
		return updateModel
	case forceDeleteModel:
		return deleteModel
	default:
		return kind
	}
}

func (kind mutationKind) operation() lifecycle.Operation {
	switch kind {
	case insertModel:
		return lifecycle.Create
	case updateModel:
		return lifecycle.Update
	case deleteModel:
		return lifecycle.Delete
	case softDeleteModel:
		return lifecycle.SoftDelete
	case restoreModel:
		return lifecycle.Restore
	case forceDeleteModel:
		return lifecycle.ForceDelete
	default:
		return 0
	}
}
