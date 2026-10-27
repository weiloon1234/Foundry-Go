package orm

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Upsert inserts or applies a conflict policy. Skipped rows return an omitted value.
// Like the old upsert API, it transforms inputs but skips per-model hooks and observers.
// Example: orm.Upsert(ctx,db,Users.Query(),policy,Users.Email.Set("ada@example.test")).
func Upsert[M any](ctx context.Context, writer database.Transactor, q Query[M], policy Conflict[M], assignments ...Assignment[M]) (value.Optional[M], error) {
	items, err := insertMany(ctx, writer, q.state, [][]Assignment[M]{assignments}, &policy.state)
	if err != nil {
		if failure, ok := safeerror.UnwrapOwned(err).(*WriteError[[]M]); ok && len(failure.candidate) == 1 {
			return value.Optional[M]{}, writeFailure(failure.cause, value.Set(failure.candidate[0]))
		}
		return value.Optional[M]{}, err
	}
	if len(items) == 0 {
		return value.Optional[M]{}, nil
	}
	return value.Set(items[0]), nil
}

// UpsertMany applies one reusable policy to a bounded atomic batch. Skipped rows
// are absent; RETURNING order is not an input-row mapping. Per-model callbacks are skipped.
// Example: orm.UpsertMany(ctx,db,Users.Query(),policy,[][]orm.Assignment[User]{{Users.Email.Set("ada@example.test")}}).
func UpsertMany[M any](ctx context.Context, writer database.Transactor, q Query[M], policy Conflict[M], rows [][]Assignment[M]) ([]M, error) {
	return insertMany(ctx, writer, q.state, rows, &policy.state)
}

func (p writePlan) withConflict(policy *conflictPolicy) (writePlan, error) {
	if policy == nil {
		return p, nil
	}
	if policy.err != nil {
		return p, policy.err
	}
	if policy.owner != nil && policy.owner != p.query.table {
		return p, invalid("conflict policy belongs to a different table declaration")
	}
	p.conflict = &policy.core
	c, err := p.query.ast.WriteCompiler(querycore.InsertModel, false, len(p.query.relations) > 0)
	if err == nil {
		err = p.validateConflict(&c)
	}
	return p, err
}

func (p writePlan) validateConflict(c *querycore.Compiler) error {
	if p.conflict == nil {
		return nil
	}
	if err := p.conflict.ValidateShape(p.query.ast, c, func(update querycore.ConflictUpdate) error {
		for _, column := range p.query.table.columns {
			if column.metadata.Name == update.Field.Column {
				if column.open != nil {
					return invalid("encrypted fields cannot be conflict update targets")
				}
				return querycore.ValidateConflictMutator(update, column.mutator)
			}
		}
		return nil
	}); err != nil {
		return err
	}
	q := p.query.ast
	q.Predicates = p.conflict.Condition
	if err := q.Validate(); err != nil {
		return err
	}
	return p.conflict.ValidateRows(c)
}

func (p writePlan) withConflictTimestamp() writePlan {
	if p.conflict == nil || p.conflict.Action != querycore.ConflictDoUpdate || p.query.table.model.Timestamps == nil {
		return p
	}
	policy := p.conflict.WithUpdatedTimestamp(p.query.ast.Table, p.query.table.model.Timestamps.Updated)
	p.conflict = &policy
	return p
}

func (p writePlan) mutateConflict(ctx context.Context) (writePlan, error) {
	if p.conflict == nil {
		return p, nil
	}
	policy, err := querycore.MutateConflictLiterals(ctx, *p.conflict, p.query.table.mutators)
	if err != nil {
		return p, err
	}
	p.conflict = &policy
	return p, nil
}

func (p writePlan) returning(ctx context.Context, tx *database.Tx, rows rowSource, scan func(database.Row) error) error {
	if p.conflict == nil {
		if p.bounded {
			return p.returningAdjustment(ctx, tx, rows, scan)
		}
		return returningN(ctx, rows, scan, p.returnCount())
	}
	count, err := returningCount(ctx, rows, scan, p.returnCount())
	if err == nil {
		err = p.conflict.CheckReturned(p.query.ast, p.returnCount(), count)
	}
	return err
}
