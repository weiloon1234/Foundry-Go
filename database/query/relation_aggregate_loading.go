package query

import (
	"context"
	"database/sql/driver"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/value"
)

func compileRelationAggregate[M, N any](ctx context.Context, input aggregateInput[M, N], measure aggregateNode, keys []driver.Value, limit int) (Statement, error) {
	node, err := querycore.RelationAggregateSelect(input.selectNode(), input.inputTable, input.inputAlias, input.group, measure, input.singular, input.unique, keys, limit)
	if err != nil {
		return Statement{}, err
	}
	c := compiler{ScopeContext: ctx, ScopeShapeOnly: ctx == nil}
	c.BeginStatement(ctx)
	sql, err := c.CompileSelect(node)
	if err != nil {
		return Statement{}, err
	}
	return statementFromCore(c.Statement(sql)), nil
}

func aggregateKeys[M any](ctx context.Context, metadata ModelField[M], parents []M, batchSize int) ([]value.Optional[cursorValue], []driver.Value, map[cursorValue]bool, error) {
	positions := make([]value.Optional[cursorValue], len(parents))
	keys := make([]driver.Value, 0, len(parents))
	seen := make(map[cursorValue]bool, len(parents))
	for i, m := range parents {
		if i%batchSize == 0 {
			if err := ctx.Err(); err != nil {
				return nil, nil, nil, err
			}
		}
		raw, err := metadata.get(m)
		if err != nil {
			return nil, nil, nil, err
		}
		if raw == nil {
			continue
		}
		key, err := recordEqualityKey(metadata.kind, raw)
		if err != nil {
			return nil, nil, nil, err
		}
		positions[i] = value.Set(key)
		if !seen[key] {
			seen[key] = true
			keys = append(keys, raw)
		}
	}
	return positions, keys, seen, nil
}

type groupedAggregate[V any] struct {
	key   cursorValue
	value V
}

func fetchRelationAggregate[M, N, V any](ctx context.Context, executor database.Executor, input aggregateInput[M, N], aggregate Aggregate[N, V], parents []M, state *relationLoadState) ([]V, error) {
	metadata, _ := input.source.definition.modelField(input.local.Column)
	positions, keys, requested, err := aggregateKeys(ctx, metadata, parents, state.limits.BatchSize)
	if err != nil {
		return nil, err
	}
	results := make(map[cursorValue]V, len(keys))
	for offset := 0; offset < len(keys); offset += state.limits.BatchSize {
		end := min(offset+state.limits.BatchSize, len(keys))
		statement, err := compileRelationAggregate(ctx, input, aggregate.node, keys[offset:end], state.remaining+1)
		if err != nil {
			return nil, err
		}
		err = database.ForEach(statement.executionContext(ctx), readExecutor{executor}, statement.sql, statement.arguments, func(row database.Row) (groupedAggregate[V], error) {
			var raw any
			var result V
			var count, distinct int64
			destinations := []any{&raw, aggregate.codec.Scan(&result)}
			if input.singular || input.unique != (fieldRef{}) {
				destinations = append(destinations, codec.Signed[int64]().Scan(&count))
			}
			if input.unique != (fieldRef{}) {
				destinations = append(destinations, codec.Signed[int64]().Scan(&distinct))
			}
			if err := row.Scan(destinations...); err != nil {
				return groupedAggregate[V]{}, err
			}
			if (input.singular && count > 1) || (input.unique != (fieldRef{}) && count != distinct) {
				return groupedAggregate[V]{}, database.NewError("aggregate relation cardinality", database.TooManyRows)
			}
			key, err := metadata.decode(raw)
			if err != nil {
				return groupedAggregate[V]{}, err
			}
			if key == nil {
				return groupedAggregate[V]{}, fault.New(fault.Invalid, "aggregate group key is NULL")
			}
			identity, err := recordEqualityKey(metadata.kind, key)
			return groupedAggregate[V]{key: identity, value: result}, err
		}, func(row groupedAggregate[V]) error {
			if state.remaining == 0 {
				return fault.New(fault.Invalid, "aggregate groups exceed the shared loading budget")
			}
			state.remaining--
			if !requested[row.key] {
				return fault.New(fault.Invalid, "aggregate group key does not match requested keys; check key equality semantics")
			}
			if _, exists := results[row.key]; exists {
				return fault.New(fault.Invalid, "aggregate returned repeated group keys; check key equality semantics")
			}
			results[row.key] = row.value
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	values := make([]V, len(parents))
	for i, position := range positions {
		if key, ok := position.Get(); ok {
			values[i] = results[key]
		}
	}
	return values, nil
}
