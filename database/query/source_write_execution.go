package query

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func (p sourceMutation[S, M]) validateExecution(ctx context.Context, writer database.Transactor) error {
	if err := writeContext(ctx, writer); err != nil {
		return err
	}
	_, _, _, err := p.validateShape(true)
	return err
}

func (p sourceMutation[S, M]) needsPreparation() bool {
	return p.kind.sqlKind() != deleteModel && (p.destination.hasFieldMutators() || (mutationPlan[M]{query: p.destination, kind: p.kind}).needsConventions())
}

func (p sourceMutation[S, M]) prepare(returning bool) func(context.Context, *database.Tx) (Statement, error) {
	return func(ctx context.Context, tx *database.Tx) (Statement, error) {
		prepared, err := p.prepareValues(ctx, transactionClock(tx))
		if err != nil {
			return Statement{}, err
		}
		return prepared.compile(returning)
	}
}

func (p sourceMutation[S, M]) prepareValues(ctx context.Context, source clock.Clock) (sourceMutation[S, M], error) {
	if ctx == nil {
		return p, fault.New(fault.Invalid, "source write preparation requires a context")
	}
	if err := ctx.Err(); err != nil {
		return p, err
	}
	if _, _, _, err := p.validateShape(true); err != nil {
		return p, err
	}
	plan := mutationPlan[M]{query: p.destination, kind: p.kind, mutation: p.values}
	if err := plan.applyConventionsFor(ctx, source, false); err != nil {
		return p, err
	}
	p.values = plan.mutation
	if p.kind.sqlKind() != deleteModel && p.destination.hasFieldMutators() {
		mutation, err := p.destination.mutateFields(ctx, p.values)
		if err != nil {
			return p, err
		}
		p.values = mutation
	}
	return p, ctx.Err()
}

func (p sourceMutation[S, M]) exec(ctx context.Context, writer database.Transactor) (int64, error) {
	if err := p.validateExecution(ctx, writer); err != nil {
		return 0, err
	}
	p.destination = p.destination.inContext(ctx)
	return executeModelStatement(ctx, writer, p.needsPreparation(), p.prepare(false), func(ctx context.Context, tx *database.Tx, statement Statement) (int64, error) {
		if len(p.mappings) == 0 {
			result, err := tx.Exec(ctx, statement.sql, statement.arguments...)
			return result.RowsAffected, err
		}
		var affected, maximum int64
		if err := database.ScanOne(ctx, tx, statement.sql, statement.arguments, &affected, &maximum); err != nil {
			return 0, err
		}
		if maximum > 1 {
			return 0, ambiguousSourceMatch()
		}
		return affected, nil
	})
}

func ambiguousSourceMatch() error {
	return database.NewError("SQL-mapped update matched multiple source rows for one model", database.TooManyRows)
}

func (p sourceMutation[S, M]) returning(ctx context.Context, writer database.Transactor, limit int) ([]M, error) {
	if limit <= 0 || limit > MaxInsertRows {
		return nil, fault.New(fault.Invalid, "source write returning requires a positive bounded result limit")
	}
	p.destination = p.destination.inContext(ctx)
	if err := p.validateExecution(ctx, writer); err != nil {
		return nil, err
	}
	return executeModelStatement(ctx, writer, p.needsPreparation(), p.prepare(true), func(ctx context.Context, tx *database.Tx, statement Statement) ([]M, error) {
		scan := p.destination.definition.scan
		if len(p.mappings) != 0 {
			scan = func(row database.Row) (M, error) {
				return p.destination.definition.scan(sourceMatchRow{Row: row})
			}
		}
		return returningModels(ctx, tx, statement, scan, 0, limit)
	})
}

// The existing complete-model decoder owns field destinations. The final private
// column validates the source cardinality before a decoded model is published.
type sourceMatchRow struct{ database.Row }

func (r sourceMatchRow) Scan(destinations ...any) error {
	var matches int64
	withCount := make([]any, len(destinations)+1)
	copy(withCount, destinations)
	withCount[len(destinations)] = &matches
	if err := r.Row.Scan(withCount...); err != nil {
		return err
	}
	if matches > 1 {
		return ambiguousSourceMatch()
	}
	if matches != 1 {
		return fault.New(fault.Invalid, "source write returned invalid match metadata")
	}
	return nil
}
