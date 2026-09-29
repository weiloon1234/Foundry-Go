package query

import (
	"context"
	"slices"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
)

// enclosingTransaction runs a nested model write directly on the transaction of
// an operation that is already atomic and fails as a unit: per-row batch writes,
// lookup creation and pivot attachment. A savepoint per row would only consume
// PostgreSQL subtransaction IDs, whose per-backend cache overflows after 64 in
// one transaction. A failed row still fails the whole enclosing operation; a
// caller-supplied transaction keeps its single savepoint around that operation.
type enclosingTransaction struct{ tx *database.Tx }

func (e enclosingTransaction) Transaction(_ context.Context, fn func(*database.Tx) error, options ...database.TxOptions) error {
	if len(options) != 0 {
		return fault.New(fault.Invalid, "nested model writes cannot set transaction options")
	}
	return fn(e.tx)
}

// FoundryObservers reports the enclosing transaction's frozen observer set.
func (e enclosingTransaction) FoundryObservers(seal sqlowner.Seal) (lifecycle.Observers, bool) {
	return e.tx.FoundryObservers(seal)
}

// MaxPerModelWriteRows bounds one atomic operation that runs ordinary model
// writes per row. It shares the established atomic insert row budget.
const MaxPerModelWriteRows = MaxInsertRows

// InsertEach creates each supplied input through the ordinary model lifecycle.
// The bounded batch is atomic and results preserve input order. Unlike
// InsertMany, this invokes one normal write and its observers per input. Rows
// run directly in the batch transaction, without a savepoint per row.
// A valid empty batch performs no transaction. Generated CreateEach methods
// preserve concrete drafts and share their normal UUID/input preparation.
func (q Query[M]) InsertEach(ctx context.Context, writer database.Transactor, mutations []Mutation[M]) ([]M, error) {
	if err := writeContext(ctx, writer); err != nil {
		return nil, err
	}
	q = q.inContext(ctx)
	if _, _, _, err := (insertPlan[M]{query: q, rows: mutations}).validateRowShapes(false); err != nil {
		return nil, err
	}
	if len(mutations) == 0 {
		return []M{}, nil
	}
	mutations = slices.Clone(mutations)
	return executeWrite(ctx, writer, func(ctx context.Context, tx *database.Tx) ([]M, error) {
		result := make([]M, 0, len(mutations))
		for _, mutation := range mutations {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			created, err := q.Insert(ctx, enclosingTransaction{tx}, mutation)
			if err != nil {
				return nil, err
			}
			result = append(result, created)
		}
		return result, nil
	})
}

// PatchEach applies a typed draft callback to each locked model through normal
// update behavior. Generated UpdateEach methods expose concrete model drafts.
// The complete candidate set must fit limit before any callback runs. Results
// follow primary-key order and all selected writes are atomic. Ordering,
// pagination and eager-loading query options are rejected. The callback receives
// the actual transaction and must propagate its context to nested operations.
func (q Query[M]) PatchEach(ctx context.Context, writer database.Transactor, limit int, draft func(context.Context, *database.Tx, M) (Mutation[M], error)) ([]M, error) {
	if draft == nil {
		return nil, fault.New(fault.Invalid, "per-model update requires a draft callback")
	}
	return q.writeEach(ctx, writer, updateModel, limit, draft)
}

// RemoveEach deletes each active candidate through ordinary model behavior.
// Soft-delete models remain stored; other models are physically removed. A
// later failure rolls back all selected changes and their after-commit work.
func (q Query[M]) RemoveEach(ctx context.Context, writer database.Transactor, limit int) ([]M, error) {
	selected, kind := q.removal()
	return selected.writeEach(ctx, writer, kind, limit, nil)
}

// RestoreEach restores each selected deleted model through normal restore
// callbacks, retaining explicit predicates and requiring soft-delete metadata.
func (q Query[M]) RestoreEach(ctx context.Context, writer database.Transactor, limit int) ([]M, error) {
	return q.OnlyTrashed().writeEach(ctx, writer, restoreModel, limit, nil)
}

// ForceRemoveEach physically removes each selected soft-delete model through
// normal force-delete callbacks. The caller's active/trashed visibility remains
// in force. Ordinary models use RemoveEach for physical deletion.
func (q Query[M]) ForceRemoveEach(ctx context.Context, writer database.Transactor, limit int) ([]M, error) {
	return q.writeEach(ctx, writer, forceDeleteModel, limit, nil)
}

func (q Query[M]) writeEach(ctx context.Context, writer database.Transactor, kind mutationKind, limit int, draft func(context.Context, *database.Tx, M) (Mutation[M], error)) ([]M, error) {
	if err := writeContext(ctx, writer); err != nil {
		return nil, err
	}
	q = q.inContext(ctx)
	if err := validatePerModelWriteLimit(limit); err != nil {
		return nil, err
	}
	if _, err := q.modelWriteCompiler(kind, false); err != nil {
		return nil, err
	}
	return executeWrite(ctx, writer, func(ctx context.Context, tx *database.Tx) ([]M, error) {
		return mutateSelectedModels(ctx, tx, q, kind, limit, draft)
	})
}

func validatePerModelWriteLimit(limit int) error {
	if limit < 1 || limit > MaxPerModelWriteRows {
		return fault.New(fault.Invalid, "invalid per-model write row limit")
	}
	return nil
}

// Relation detachment and explicit per-model operations share candidate
// selection, limits and the ordinary mutation pipeline. Internal ownership
// hydration skips Retrieved callbacks. Later concurrent inserts are outside
// the statement's selected set, and later writes retain the original filters.
func mutateSelectedModels[M any](ctx context.Context, tx *database.Tx, q Query[M], kind mutationKind, limit int, draft func(context.Context, *database.Tx, M) (Mutation[M], error)) ([]M, error) {
	q = q.inContext(ctx)
	candidates, err := lockedModelWriteCandidates(ctx, tx, q, kind, limit)
	if err != nil {
		return nil, err
	}
	results := make([]M, 0, len(candidates))
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		primary, err := modelWritePredicate(q, candidate)
		if err != nil {
			return nil, err
		}
		var mutation Mutation[M]
		if draft != nil {
			mutation, err = modelUpdateMutation(ctx, tx, candidate, draft)
			if err != nil {
				return nil, err
			}
		}
		changed, err := executeMutation(ctx, enclosingTransaction{tx}, mutationPlan[M]{query: q.Where(primary), kind: kind, mutation: mutation})
		if err != nil {
			return nil, err
		}
		results = append(results, changed)
	}
	return results, nil
}

func lockedModelWriteCandidates[M any](ctx context.Context, tx *database.Tx, q Query[M], kind mutationKind, limit int) ([]M, error) {
	q = q.inContext(ctx)
	if err := validatePerModelWriteLimit(limit); err != nil {
		return nil, err
	}
	if _, err := q.modelWriteCompiler(kind, false); err != nil {
		return nil, err
	}
	primary, ok := q.definition.modelField(q.definition.primary)
	if !ok || primary.get == nil {
		return nil, fault.New(fault.Invalid, "per-model write requires a primary-key codec")
	}
	statement, err := q.OrderBy(Order[M]{field: fieldRef{q.table, q.definition.primary}}).ForUpdate().Limit(limit + 1).Compile()
	if err != nil {
		return nil, err
	}
	candidates, err := returningModels(ctx, tx, statement, q.definition.scan, 0, limit+1)
	if err != nil {
		return nil, err
	}
	if len(candidates) > limit {
		return nil, fault.New(fault.Invalid, "per-model write exceeds its row limit")
	}
	if err := validateModelWriteIdentities(ctx, primary, candidates); err != nil {
		return nil, err
	}
	return candidates, nil
}

// A model declaration does not prove a physical unique constraint. Reject
// ambiguous identities before invoking a per-row draft callback. Equality uses
// the same canonical field semantics as relation loading and pagination.
func validateModelWriteIdentities[M any](ctx context.Context, primary ModelField[M], candidates []M) error {
	seen := make(map[cursorValue]bool, len(candidates))
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return err
		}
		raw, err := primary.get(candidate)
		if err != nil {
			return err
		}
		if raw == nil {
			return fault.New(fault.Invalid, "per-model write primary key cannot be NULL")
		}
		identity, err := primary.equalityKey(raw)
		if err != nil {
			return err
		}
		if seen[identity] {
			return database.NewError("per-model write identity", database.TooManyRows)
		}
		seen[identity] = true
	}
	return nil
}
