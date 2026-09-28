package query

import (
	"context"
	"fmt"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func fetchThrough[M, N, P any, A, B comparable](ctx context.Context, executor database.Executor, r ThroughRelation[M, N, P], local ScalarField[M, A], pivotLocal ScalarField[P, A], pivotForeign ScalarField[P, B], foreign ScalarField[N, B], parents []M, state *relationLoadState, depth int) ([][]relation.Link[N, P], error) {
	localMeta, _ := r.spec.source.definition.modelField(local.ref.column)
	positions, keys, requested, err := relationKeys(ctx, localMeta, local, parents, state.limits.BatchSize)
	if err != nil {
		return nil, err
	}
	var rows []relation.Link[N, P]
	for offset := 0; offset < len(keys); offset += state.limits.BatchSize {
		end := min(offset+state.limits.BatchSize, len(keys))
		statement, err := r.compileThrough(pivotLocal.In(keys[offset:end]...).expression, state.remaining+1)
		if err != nil {
			return nil, err
		}
		count := 0
		batch, err := collectRead(ctx, executor, statement,
			func(row database.Row) (relation.Link[N, P], error) {
				if count == state.remaining {
					return relation.Link[N, P]{}, fault.New(fault.Invalid, "related rows exceed the shared loading budget")
				}
				count++
				return scanJoined(row, *r.spec.target.definition, *r.pivot.definition)
			}, joinedReadLifecycle(r.spec.target.definition, r.pivot.definition))
		if err != nil {
			return nil, err
		}
		state.remaining -= len(batch)
		rows = append(rows, batch...)
	}
	pivotLocalMeta, _ := r.pivot.definition.modelField(pivotLocal.ref.column)
	pivotForeignMeta, _ := r.pivot.definition.modelField(pivotForeign.ref.column)
	foreignMeta, _ := r.spec.target.definition.modelField(foreign.ref.column)
	primaryMeta, _ := r.pivot.definition.modelField(r.pivot.definition.primary)
	seenPivots := make(map[cursorValue]bool, len(rows))
	owners := make([]cursorValue, len(rows))
	targets := make([]N, len(rows))
	pivots := make([]P, len(rows))
	for i, link := range rows {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		owner, err := readRelationKey(pivotLocalMeta, pivotLocal, link.Pivot)
		if err != nil {
			return nil, err
		}
		key, ok := owner.Get()
		if !ok || !requested[key.identity] {
			return nil, fault.New(fault.Invalid, "pivot source key does not match requested keys; check key equality semantics")
		}
		pivotKey, err := readRelationKey(pivotForeignMeta, pivotForeign, link.Pivot)
		if err != nil {
			return nil, err
		}
		targetKey, err := readRelationKey(foreignMeta, foreign, link.Model)
		if err != nil {
			return nil, err
		}
		pk, pset := pivotKey.Get()
		tk, tset := targetKey.Get()
		if !pset || !tset || pk.identity != tk.identity {
			return nil, fault.New(fault.Invalid, "pivot target key does not match related key; check key equality semantics")
		}
		raw, err := primaryMeta.get(link.Pivot)
		if err != nil {
			return nil, err
		}
		if raw == nil {
			return nil, fault.New(fault.Invalid, "pivot primary key cannot be NULL")
		}
		identity, err := primaryMeta.equalityKey(raw)
		if err != nil {
			return nil, fault.New(fault.Invalid, "pivot primary key has no canonical database representation")
		}
		if seenPivots[identity] {
			return nil, fmt.Errorf("pivot matched more than one target: %w", database.TooManyRows)
		}
		seenPivots[identity] = true
		owners[i], targets[i], pivots[i] = key.identity, link.Model, link.Pivot
	}
	targets, err = r.spec.target.loadRelations(ctx, executor, targets, state, depth, false)
	if err != nil {
		return nil, err
	}
	pivots, err = r.pivot.loadRelations(ctx, executor, pivots, state, depth, false)
	if err != nil {
		return nil, err
	}
	groups := make(map[cursorValue][]relation.Link[N, P], len(keys))
	for i := range rows {
		groups[owners[i]] = append(groups[owners[i]], relation.Link[N, P]{Model: targets[i], Pivot: pivots[i]})
	}
	result := make([][]relation.Link[N, P], len(parents))
	for i, position := range positions {
		if key, ok := position.Get(); ok {
			result[i] = groups[key]
		}
	}
	return result, nil
}
