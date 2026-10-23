package orm

import (
	"context"
	"database/sql/driver"
	"slices"
	"unsafe"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlvalue"
)

func aggregateSelect(a *aggregatePlan, ctx context.Context, keys []driver.Value, limit int) (querycore.SelectNode, error) {
	r := a.input.plan
	target := r.query.ast
	target.ScopeContext = ctx
	node := querycore.SelectNode{Source: querycore.TableSource{Table: target.Table, Columns: target.Model.Columns}, Predicates: slices.Clone(target.EffectivePredicates())}
	alias, group := target.Table, r.foreign
	var unique querycore.FieldRef
	if j := r.join; j != nil {
		pivot := j.pivot.ast
		pivot.ScopeContext = ctx
		alias = querycore.RelationTargetAlias
		if j.hop {
			node.Source.Alias = alias
			for i, p := range node.Predicates {
				node.Predicates[i] = querycore.Requalify(p, alias)
			}
			node = querycore.RelationHopJoin(node, alias, r.foreign, pivot.Table, pivot.Model.Columns, pivot.EffectivePredicates(), j.second)
			group = querycore.FieldRef{Table: querycore.RelationHopAlias, Column: j.first.Column}
		} else {
			node = querycore.RelationThroughSelect(target, pivot, r.foreign, j.second, nil, alias, querycore.RelationPivotAlias)
			group = querycore.FieldRef{Table: querycore.RelationPivotAlias, Column: j.first.Column}
			unique = querycore.FieldRef{Table: querycore.RelationPivotAlias, Column: pivot.Model.Primary}
			if a.input.pivot {
				alias = querycore.RelationPivotAlias
			}
		}
	}
	return querycore.RelationAggregateSelect(node, aggregateOwner(a.input).model.Table, alias, group, a.measure, r.singular, unique, keys, limit)
}

func loadAggregate(ctx context.Context, ex database.Executor, p *relationPlan, base unsafe.Pointer, count int, budget *relationBudget, depth int, missing bool) error {
	a := p.aggregate
	local := aggregateLocal(a.input.plan)
	size := p.slot.owner.typ.Size()
	parent := func(i int) unsafe.Pointer { return unsafe.Add(base, uintptr(i)*size) }
	positions, keys, requested, err := querycore.RelationKeys(ctx, count, budget.limits.BatchSize, func(i int) (driver.Value, sqlvalue.Value, bool, error) {
		if missing && p.loaded(parent(i)) {
			return nil, sqlvalue.Value{}, false, nil
		}
		raw, err := local.get(parent(i))
		if err != nil || raw == nil {
			return nil, sqlvalue.Value{}, false, err
		}
		key, err := querycore.RecordEqualityKey(local.kind, raw)
		return raw, key, true, err
	})
	if err != nil {
		return err
	}
	results := make(map[sqlvalue.Value]func(unsafe.Pointer, uintptr), len(keys))
	err = querycore.FetchRelationBatches(len(keys), budget.limits.BatchSize, &budget.remaining, func(start, end, limit int) (int, error) {
		node, err := aggregateSelect(a, ctx, keys[start:end], limit)
		if err != nil {
			return 0, err
		}
		keyScanner, readKey := a.input.keyScan()
		fetched := 0
		err = ormcore.Read(ctx, ex, node, nil, nil, func(row database.Row) error {
			scanner, attach := a.scan()
			var count, distinct int64
			destinations := []any{keyScanner, scanner}
			joined := a.input.plan.join != nil && !a.input.plan.join.hop
			if a.input.plan.singular || joined {
				destinations = append(destinations, codec.Signed[int64]().Scan(&count))
			}
			if joined {
				destinations = append(destinations, codec.Signed[int64]().Scan(&distinct))
			}
			if err := row.Scan(destinations...); err != nil {
				return err
			}
			if a.input.plan.singular && count > 1 || joined && count != distinct {
				return database.NewError("aggregate relation cardinality", database.TooManyRows)
			}
			raw, err := readKey()
			if err != nil {
				return err
			}
			if raw == nil {
				return invalid("aggregate group key is NULL")
			}
			key, err := querycore.RecordEqualityKey(local.kind, raw)
			if err != nil {
				return err
			}
			if !requested[key] {
				return invalid("aggregate group key does not match requested keys; check key equality semantics")
			}
			if _, exists := results[key]; exists {
				return invalid("aggregate returned repeated group keys; check key equality semantics")
			}
			fetched++
			if fetched > budget.remaining {
				return invalid("aggregate groups exceed the shared loading budget")
			}
			results[key] = attach
			return nil
		})
		return fetched, err
	})
	if err != nil {
		return err
	}
	for i, position := range positions {
		if missing && p.loaded(parent(i)) {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := querycore.ConsumeRelationBudget(&budget.attachments, 1, true); err != nil {
			return err
		}
		attach := a.empty
		if key, present := position.Get(); present && results[key] != nil {
			attach = results[key]
		}
		attach(parent(i), p.slot.offset)
	}
	return nil
}
