package orm

import (
	"context"
	"unsafe"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlvalue"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Constructors compile the typed bridge once; importers carry thin plans. All
// target batches are collected before nested branches run over the whole set.
func relationLoader[M, N any, K comparable](local columnData[K], foreign columnData[K], assign func(*M, []N) error) func(context.Context, database.Executor, *relationPlan, unsafe.Pointer, int, *relationBudget, int, bool) error {
	return func(ctx context.Context, ex database.Executor, p *relationPlan, base unsafe.Pointer, count int, budget *relationBudget, depth int, missing bool) error {
		parents := unsafe.Slice((*M)(base), count)
		positions, unique, seen, err := querycore.RelationKeys(ctx, count, budget.limits.BatchSize, func(i int) (K, sqlvalue.Value, bool, error) {
			if missing && p.loaded(unsafe.Pointer(&parents[i])) {
				return *new(K), sqlvalue.Value{}, false, nil
			}
			return readRelationKey(local, unsafe.Pointer(&parents[i]))
		})
		if err != nil {
			return err
		}
		target := p.query
		target.relations = nil
		target.ast.ScopeContext = ctx
		target.ast, err = target.ast.StableOrder()
		if err != nil {
			return err
		}
		var rows []N
		err = querycore.FetchRelationBatches(len(unique), budget.limits.BatchSize, &budget.remaining, func(start, end, limit int) (int, error) {
			q := where(target, []Predicate[N]{comparison[N](foreign, querycore.In, unique[start:end])})
			q.ast.Limit = value.Set(limit)
			batch, err := allRows[N](ctx, ex, q)
			if err != nil {
				return 0, err
			}
			rows = append(rows, batch...)
			return len(batch), nil
		})
		if err != nil {
			return err
		}
		if err := loadPlansAt(ctx, ex, unsafe.Pointer(unsafe.SliceData(rows)), len(rows), p.query.relations, budget, depth, false); err != nil {
			return err
		}
		groups, err := querycore.GroupRelationRows(rows, seen, func(row N) (sqlvalue.Value, bool, error) {
			_, identity, present, err := readRelationKey(foreign, unsafe.Pointer(&row))
			return identity, present, err
		})
		if err != nil {
			return err
		}
		for i, position := range positions {
			if missing && p.loaded(unsafe.Pointer(&parents[i])) {
				continue
			}
			key, _ := position.Get()
			group := groups[key]
			if p.singular && len(group) > 1 {
				return database.NewError("singular relation cardinality", database.TooManyRows)
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := querycore.ConsumeRelationBudget(&budget.attachments, len(group), true); err != nil {
				return err
			}
			if err := assign(&parents[i], group); err != nil {
				return err
			}
		}
		return nil
	}
}
func readRelationKey[V any](column columnData[V], base unsafe.Pointer) (V, sqlvalue.Value, bool, error) {
	raw, err := column.core.get(base)
	if err != nil || raw == nil {
		return *new(V), sqlvalue.Value{}, false, err
	}
	v, err := column.decode(raw)
	if err != nil {
		return v, sqlvalue.Value{}, false, err
	}
	identity, err := querycore.RecordEqualityKey(column.core.kind, raw)
	return v, identity, true, err
}
