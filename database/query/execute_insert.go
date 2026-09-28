package query

import (
	"context"
	"slices"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/value"
)

// InsertOnConflict is the generated Upsert execution boundary. An omitted
// result means DO NOTHING or an unsatisfied update condition skipped the write.
func (q Query[M]) InsertOnConflict(ctx context.Context, writer database.Transactor, mutation Mutation[M], conflict Conflict[M]) (value.Optional[M], error) {
	if err := writeContext(ctx, writer); err != nil {
		return value.Optional[M]{}, err
	}
	plan := insertPlan[M]{query: q, rows: []Mutation[M]{mutation}, conflict: &conflict}
	return executeModelStatement(ctx, writer, q.hasFieldMutators() || q.hasTimestamps(), func(ctx context.Context, tx *database.Tx) (Statement, error) {
		prepared, err := plan.withTimestamps(ctx, transactionClock(tx))
		if err != nil {
			return Statement{}, err
		}
		return prepared.prepare(ctx)
	}, func(ctx context.Context, tx *database.Tx, s Statement) (value.Optional[M], error) {
		items, err := returningModels(ctx, tx, s, q.definition.scan, 0, 1)
		if err != nil || len(items) == 0 {
			return value.Optional[M]{}, err
		}
		return value.Set(items[0]), nil
	})
}

// InsertMany atomically inserts bounded, complete models. A valid empty batch
// performs no transaction. Returned order is the database's RETURNING order.
func (q Query[M]) InsertMany(ctx context.Context, writer database.Transactor, mutations []Mutation[M]) ([]M, error) {
	return q.insertMany(ctx, writer, mutations, nil)
}

// InsertManyOnConflict executes one bounded upsert statement. Skipped conflicts
// have no returned model; repeated update targets are PostgreSQL errors. There
// are no hidden per-row writes, retries or observer dispatches in this bulk API.
func (q Query[M]) InsertManyOnConflict(ctx context.Context, writer database.Transactor, mutations []Mutation[M], conflict Conflict[M]) ([]M, error) {
	return q.insertMany(ctx, writer, mutations, &conflict)
}

func (q Query[M]) insertMany(ctx context.Context, writer database.Transactor, mutations []Mutation[M], conflict *Conflict[M]) ([]M, error) {
	if err := writeContext(ctx, writer); err != nil {
		return nil, err
	}
	plan := insertPlan[M]{query: q, rows: mutations, conflict: conflict}
	if len(mutations) == 0 {
		if _, err := plan.compile(); err != nil {
			return nil, err
		}
		return []M{}, nil
	}
	minimum := 0
	if conflict == nil {
		minimum = len(mutations)
	}
	return executeModelStatement(ctx, writer, q.hasFieldMutators() || q.hasTimestamps(), func(ctx context.Context, tx *database.Tx) (Statement, error) {
		prepared, err := plan.withTimestamps(ctx, transactionClock(tx))
		if err != nil {
			return Statement{}, err
		}
		return prepared.prepare(ctx)
	}, func(ctx context.Context, tx *database.Tx, s Statement) ([]M, error) {
		return returningModels(ctx, tx, s, q.definition.scan, minimum, len(mutations))
	})
}

// Bulk operations normalize explicitly supplied field values without dispatching
// per-model observers. Literal conflict assignments use the same transformation;
// the destination's own EXCLUDED value is already transformed. Other SQL
// assignments to fields with Go mutators fail validation rather than bypass it.
func (p insertPlan[M]) prepare(ctx context.Context) (Statement, error) {
	if !p.query.hasFieldMutators() {
		return p.compile()
	}
	if _, _, _, err := p.validateRows(); err != nil {
		return Statement{}, err
	}
	p.rows = slices.Clone(p.rows)
	for i, row := range p.rows {
		mutation, err := p.query.mutateFields(ctx, row)
		if err != nil {
			return Statement{}, err
		}
		p.rows[i] = mutation
	}
	if p.conflict != nil {
		policy, err := p.conflict.mutateLiterals(ctx, p.query)
		if err != nil {
			return Statement{}, err
		}
		p.conflict = &policy
	}
	return p.compile()
}
