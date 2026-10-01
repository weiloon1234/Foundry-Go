package query

import (
	"context"
	"errors"

	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/encryption"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
	"github.com/weiloon1234/Foundry-Go/value"
)

// WriteError preserves a completely hydrated result when commit confirmation
// is uncertain or after-commit work failed. R matches the terminal result type:
// a model, Optional model, model slice or affected-row count. Candidate is reconciliation data, not
// proof of persistence. Normal formatting never includes result fields.
type WriteError[M any] struct {
	cause     error
	candidate M
}

func (e *WriteError[M]) Error() string {
	return "model write requires transaction outcome reconciliation"
}
func (e *WriteError[M]) GoString() string { return e.Error() }
func (e *WriteError[M]) Unwrap() error    { return e.cause }
func (e *WriteError[M]) Candidate() M     { return e.candidate }

// Insert creates one complete model. Generated Create methods construct the
// model-specific mutation and defaults; this is the shared execution boundary.
func (q Query[M]) Insert(ctx context.Context, writer database.Transactor, mutation Mutation[M]) (M, error) {
	return executeMutation(ctx, writer, mutationPlan[M]{query: q, kind: insertModel, mutation: mutation})
}

// Patch updates one model selected by primary-key equality and optional scopes.
// Empty updates, primary-key changes and multi-row RETURNING results fail.
func (q Query[M]) Patch(ctx context.Context, writer database.Transactor, mutation Mutation[M]) (M, error) {
	return executeMutation(ctx, writer, mutationPlan[M]{query: q, kind: updateModel, mutation: mutation})
}

// Remove soft-deletes a configured model and returns the complete stored result.
// Models without soft deletion are physically removed and return pre-deletion data.
// No matching model reports database.NotFound. This is not a set-based bulk API.
func (q Query[M]) Remove(ctx context.Context, writer database.Transactor) (M, error) {
	q, kind := q.removal()
	return executeMutation(ctx, writer, mutationPlan[M]{query: q, kind: kind})
}

// removal shares active-only deletion selection with relation writes.
func (q Query[M]) removal() (Query[M], mutationKind) {
	kind := deleteModel
	if q.hasSoftDeletes() {
		kind = softDeleteModel
		if q.softDeleteScope == trashedRecords {
			q = q.Where(Predicate[M]{expression: q.deletionPredicate(isNull)})
		} else {
			q = q.WithoutTrashed()
		}
	}
	return q, kind
}

func writeContext(ctx context.Context, writer database.Transactor) error {
	if ctx == nil || writer == nil {
		return fault.New(fault.Invalid, "model write requires a context and transactor")
	}
	return ctx.Err()
}

func executeMutation[M any](ctx context.Context, writer database.Transactor, plan mutationPlan[M]) (M, error) {
	if err := writeContext(ctx, writer); err != nil {
		return *new(M), err
	}
	plan.query = plan.query.inContext(ctx)
	if plan.query.skipModelHooks {
		return *new(M), fault.New(fault.Invalid, "WithoutModelHooks applies only to set-based writes; per-model writes always run hooks")
	}
	observers, known := writerObservers(writer)
	if plan.query.definition != nil && (plan.query.definition.hasWriteHooks || !known || observedBy[M](observers, plan.kind)) {
		return executeHookedMutation(ctx, writer, plan)
	}
	// Encrypted fields are sealed inside the transaction with its key ring.
	if plan.kind == insertModel && plan.query.definition != nil && !plan.mutation.encrypted() {
		switch owner := writer.(type) {
		case *database.DB:
			return executeAutocommitInsert(ctx, owner.FoundryAutocommitQuery, owner.Clock(), plan)
		case database.PrimaryExecutor:
			return executeAutocommitInsert(ctx, owner.FoundryAutocommitQuery, owner.Clock(), plan)
		}
	}
	mutate := plan.kind.sqlKind() != deleteModel && (plan.query.hasFieldMutators() || plan.needsConventions() || plan.mutation.encrypted())
	return executeModelStatement(ctx, writer, mutate, func(ctx context.Context, tx *database.Tx) (Statement, error) {
		return prepareMutation(ctx, &plan, transactionClock(tx), transactionKeys(tx))
	}, func(ctx context.Context, tx *database.Tx, s Statement) (M, error) {
		return plan.returning(ctx, tx, s)
	})
}

// prepareMutation shares post-hook validation, once-only field transforms,
// encrypted-field sealing and compilation across ordinary writes and the
// no-observer wrapper fallback.
func prepareMutation[M any](ctx context.Context, plan *mutationPlan[M], source clock.Clock, keys *encryption.Keyring) (Statement, error) {
	if err := plan.applyConventionsFor(ctx, source, !plan.setBased); err != nil {
		return Statement{}, err
	}

	if plan.kind.sqlKind() != deleteModel && plan.query.hasFieldMutators() {
		c, err := plan.query.modelWriteCompiler(plan.kind, !plan.setBased)
		if err != nil {
			return Statement{}, err
		}
		if _, err := plan.validate(&c); err != nil {
			return Statement{}, err
		}
		mutation, err := plan.query.mutateFields(ctx, plan.mutation)
		if err != nil {
			return Statement{}, err
		}
		plan.mutation = mutation
	}
	if err := plan.seal(ctx, keys); err != nil {
		return Statement{}, err
	}
	return plan.compile()
}

// executeModelStatement runs user field transforms inside the write transaction.
// Untransformed statements retain pre-transaction compilation and validation.
func executeModelStatement[R any](ctx context.Context, writer database.Transactor, prepareInTransaction bool, prepare func(context.Context, *database.Tx) (Statement, error), read func(context.Context, *database.Tx, Statement) (R, error)) (R, error) {
	if !prepareInTransaction {
		statement, err := prepare(ctx, nil)
		if err != nil {
			return *new(R), err
		}
		return executeStatement(ctx, writer, statement, read)
	}
	return executeWrite(ctx, writer, func(ctx context.Context, tx *database.Tx) (R, error) {
		statement, err := prepare(ctx, tx)
		if err != nil {
			return *new(R), err
		}
		return read(ctx, tx, statement)
	})
}

func executeStatement[R any](ctx context.Context, writer database.Transactor, statement Statement, read func(context.Context, *database.Tx, Statement) (R, error)) (R, error) {
	return executeWrite(ctx, writer, func(ctx context.Context, tx *database.Tx) (R, error) {
		return read(ctx, tx, statement)
	})
}

func executeWrite[R any](ctx context.Context, writer database.Transactor, write func(context.Context, *database.Tx) (R, error)) (R, error) {
	var candidate value.Optional[R]
	err := writer.Transaction(ctx, func(tx *database.Tx) error {
		model, err := write(ctx, tx)
		if err != nil {
			return err
		}
		candidate = value.Set(model)
		return nil
	})
	if err != nil {
		model, present := candidate.Get()
		if present {
			// A custom transactor may wrap outcome metadata in extension errors.
			// Keep the hydrated candidate if inspection itself is inconclusive.
			reconcile := true
			inspection := callback.Isolated("inspect model write outcome", func() error {
				outcome, found, complete := errorgraph.As[*database.Error](err)
				reconcile = !complete || found && (outcome == nil || outcome.Outcome() == database.Unknown || outcome.Outcome() == database.Committed)
				return nil
			})
			if inspection != nil {
				err = errors.Join(err, inspection)
			}
			if reconcile {
				return *new(R), &WriteError[R]{cause: err, candidate: model}
			}
		}
		return *new(R), err
	}
	model, present := candidate.Get()
	if !present {
		return *new(R), fault.New(fault.Invalid, "transactor returned without a successful model write")
	}
	return model, nil
}

// returning runs the plan's own single-row statement.
func (p mutationPlan[M]) returning(ctx context.Context, tx *database.Tx, statement Statement) (M, error) {
	result, err := returningOne(ctx, tx, statement, p.query.definition.scan)
	if err != nil && p.statementFailure != nil {
		*p.statementFailure = err
	}
	return result, err
}

func returningOne[M any](ctx context.Context, tx *database.Tx, statement Statement, scan func(database.Row) (M, error)) (M, error) {
	items, err := returningModels(ctx, tx, statement, scan, 1, 1)
	if err != nil {
		return *new(M), err
	}
	return items[0], nil
}

func returningModels[M any](ctx context.Context, tx *database.Tx, statement Statement, scan func(database.Row) (M, error), minimum, maximum int) (result []M, err error) {
	rows, err := tx.Query(ctx, statement.sql, statement.arguments...)
	if err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, rows.Close())
		if err != nil {
			result = nil
		}
	}()
	result = make([]M, 0, maximum)
	for rows.Next() {
		if len(result) >= maximum {
			return nil, database.NewError("model write returned too many rows", database.TooManyRows)
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		item, err := scan(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(result) < minimum {
		return nil, database.NewError("model write returned too few rows", database.NotFound)
	}
	return result, nil
}
