package query

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
)

// PruneBatch physically removes at most size models selected by the query, in
// primary-key order, in one transaction, and returns how many it removed. It
// is the execution boundary of the prune package. With mass false each model
// runs its ordinary (force) delete lifecycle, including hooks and observers;
// with mass true one DELETE removes the batch without per-model hooks. A
// soft-delete model is force-deleted within the query's visibility, so select
// trashed models with WithTrashed or OnlyTrashed.
func (q Query[M]) PruneBatch(ctx context.Context, writer database.Transactor, size int, mass bool) (int64, error) {
	if err := writeContext(ctx, writer); err != nil {
		return 0, err
	}
	if err := validatePerModelWriteLimit(size); err != nil {
		return 0, err
	}
	q = q.inContext(ctx)
	if q.definition == nil {
		return 0, fault.New(fault.Invalid, "pruning requires model metadata")
	}
	kind := deleteModel
	if q.hasSoftDeletes() {
		kind = forceDeleteModel
	}
	if _, err := q.modelWriteCompiler(kind, false); err != nil {
		return 0, err
	}
	primary, ok := q.definition.modelField(q.definition.primary)
	if !ok || primary.get == nil {
		return 0, fault.New(fault.Invalid, "pruning requires a primary-key codec")
	}
	return executeWrite(ctx, writer, func(ctx context.Context, tx *database.Tx) (int64, error) {
		statement, err := q.OrderBy(Order[M]{field: fieldRef{q.table, q.definition.primary}}).ForUpdate().Limit(size).Compile()
		if err != nil {
			return 0, err
		}
		candidates, err := returningModels(ctx, tx, statement, q.definition.scan, 0, size)
		if err != nil || len(candidates) == 0 {
			return 0, err
		}
		keys := make([]pivotKey, len(candidates))
		for i, candidate := range candidates {
			if keys[i], err = relationKey(q, fieldRef{q.table, q.definition.primary}, candidate); err != nil {
				return 0, err
			}
		}
		selected := q.Where(keyMembership[M](fieldRef{q.table, q.definition.primary}, primary, keys))
		if !mass {
			removed, err := mutateSelectedModels(ctx, tx, selected, kind, size, nil)
			return int64(len(removed)), err
		}
		plan := mutationPlan[M]{query: selected, kind: kind, setBased: true, countOnly: true}
		prepared, err := prepareMutation(ctx, &plan, transactionClock(tx), transactionKeys(tx))
		if err != nil {
			return 0, err
		}
		result, err := tx.Exec(ctx, prepared.sql, prepared.arguments...)
		return result.RowsAffected, err
	})
}
