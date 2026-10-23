package orm

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlvalue"
	"github.com/weiloon1234/Foundry-Go/value"
	"unsafe"
)

func throughLoader[M, N, P any, A, B comparable](local columnData[A], pivotLocal columnData[A], pivotForeign, foreign columnData[B], assign func(*M, []relation.Link[N, P])) func(context.Context, database.Executor, *relationPlan, unsafe.Pointer, int, *relationBudget, int, bool) error {
	return func(ctx context.Context, ex database.Executor, p *relationPlan, base unsafe.Pointer, count int, budget *relationBudget, depth int, missing bool) error {
		parents := unsafe.Slice((*M)(base), count)
		positions, keys, requested, err := querycore.RelationKeys(ctx, count, budget.limits.BatchSize, func(i int) (A, sqlvalue.Value, bool, error) {
			if missing && p.loaded(unsafe.Pointer(&parents[i])) {
				return *new(A), sqlvalue.Value{}, false, nil
			}
			return readRelationKey(local, unsafe.Pointer(&parents[i]))
		})
		if err != nil {
			return err
		}
		var targets []N
		var pivots []P
		err = querycore.FetchRelationBatches(len(keys), budget.limits.BatchSize, &budget.remaining, func(start, end, limit int) (int, error) {
			node, err := relationSelect(p, ctx)
			if err != nil {
				return 0, err
			}
			node.Predicates = append(node.Predicates, querycore.Requalify(comparison[P](pivotLocal, querycore.In, keys[start:end]).expression, querycore.RelationPivotAlias))
			node.Limit = value.Set(limit)
			n, pc := len(p.target.columns), len(p.join.pivot.table.columns)
			fetched := 0
			err = ormcore.Read(ctx, ex, node, nil, nil, func(row database.Row) error {
				var target N
				var pivot P
				if err := hydrate(p.target, ormcore.RelationRow{Row: row, Count: n, Total: n + pc}, unsafe.Pointer(&target)); err != nil {
					return err
				}
				if err := hydrate(p.join.pivot.table, ormcore.RelationRow{Row: row, Start: n, Count: pc, Total: n + pc}, unsafe.Pointer(&pivot)); err != nil {
					return err
				}
				fetched++
				if fetched > budget.remaining {
					return invalid("related rows exceed the shared loading budget")
				}
				targets = append(targets, target)
				pivots = append(pivots, pivot)
				return nil
			})
			return fetched, err
		})
		if err != nil {
			return err
		}
		owners := make([]sqlvalue.Value, len(pivots))
		seenPivots := make(map[sqlvalue.Value]bool, len(pivots))
		for i := range pivots {
			if err := ctx.Err(); err != nil {
				return err
			}
			_, owner, present, err := readRelationKey(pivotLocal, unsafe.Pointer(&pivots[i]))
			if err != nil {
				return err
			}
			if !present || !requested[owner] {
				return invalid("pivot source key does not match requested keys; check key equality semantics")
			}
			_, pk, pset, err := readRelationKey(pivotForeign, unsafe.Pointer(&pivots[i]))
			if err != nil {
				return err
			}
			_, tk, tset, err := readRelationKey(foreign, unsafe.Pointer(&targets[i]))
			if err != nil {
				return err
			}
			if !pset || !tset || pk != tk {
				return invalid("pivot target key does not match related key; check key equality semantics")
			}
			primary := p.join.pivot.table.primary
			raw, err := primary.get(unsafe.Pointer(&pivots[i]))
			if err != nil {
				return err
			}
			if raw == nil {
				return invalid("pivot primary key cannot be NULL")
			}
			identity, err := querycore.RecordEqualityKey(primary.kind, raw)
			if err != nil {
				return invalid("pivot primary key has no canonical database representation")
			}
			if seenPivots[identity] {
				return database.NewError("pivot matched more than one target", database.TooManyRows)
			}
			seenPivots[identity] = true
			owners[i] = owner
		}
		if err := loadPlansAt(ctx, ex, unsafe.Pointer(unsafe.SliceData(targets)), len(targets), p.query.relations, budget, depth, false); err != nil {
			return err
		}
		if err := loadPlansAt(ctx, ex, unsafe.Pointer(unsafe.SliceData(pivots)), len(pivots), p.join.pivot.relations, budget, depth, false); err != nil {
			return err
		}
		groups := make(map[sqlvalue.Value][]relation.Link[N, P], len(keys))
		for i, owner := range owners {
			groups[owner] = append(groups[owner], relation.Link[N, P]{Model: targets[i], Pivot: pivots[i]})
		}
		for i, pos := range positions {
			if missing && p.loaded(unsafe.Pointer(&parents[i])) {
				continue
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			key, _ := pos.Get()
			group := groups[key]
			if err := querycore.ConsumeRelationBudget(&budget.attachments, 2*len(group), true); err != nil {
				return err
			}
			assign(&parents[i], group)
		}
		return nil
	}
}
