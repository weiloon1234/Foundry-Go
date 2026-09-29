package query

import (
	"context"
	"errors"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/fault"
)

// AggregateRelation loads a compiler-checked aggregate into a generated model
// slot. Use With alongside ordinary relationships, including nested loading.
type AggregateRelation[M, V any] struct {
	name, table  string
	get          func(M) relation.Value[V]
	set          func(M, relation.Value[V]) M
	validate     func(string, int, RelationLimits) error
	fetch        func(context.Context, database.Executor, []M, *relationLoadState) ([]V, error)
	bindingError error
}

// Related pairs a relation's typed input with a matching typed aggregate. It
// declares computation; generation supplies the source model's result slot.
func Related[M, N, V any](source AggregateSource[M, N], computation AggregateExpression[N, V]) AggregateRelation[M, V] {
	if nilDescriptor(source) || nilDescriptor(computation) {
		return AggregateRelation[M, V]{}
	}
	aggregate := computation.aggregateValue()
	input := source.aggregateInput()
	return AggregateRelation[M, V]{
		validate: func(table string, depth int, limits RelationLimits) error {
			if input.validate == nil || input.selectNode == nil || input.source.definition == nil {
				return fault.New(fault.Invalid, "aggregate requires a bound relation input")
			}
			if table != input.source.table {
				return fault.New(fault.Invalid, "aggregate belongs to a different source table")
			}
			if err := input.validate(depth, limits); err != nil {
				return err
			}
			_, err := compileRelationAggregate(nil, input, aggregate.node, nil, 1)
			return err
		},
		fetch: func(ctx context.Context, executor database.Executor, parents []M, state *relationLoadState) ([]V, error) {
			return fetchRelationAggregate(ctx, executor, input, aggregate, parents, state)
		},
	}
}

// Bind is a generated declaration boundary requiring a bare source query.
func (r AggregateRelation[M, V]) Bind(name string, source Query[M], get func(M) relation.Value[V], set func(M, relation.Value[V]) M) AggregateRelation[M, V] {
	r.bindingError = nil
	if source.hasQueryOptions() {
		r.bindingError = fault.New(fault.Invalid, "aggregate Bind requires bare source metadata")
	}
	if err := source.validateCore(); err != nil {
		r.bindingError = err
	}
	if source.definition == nil {
		r.bindingError = fault.New(fault.Invalid, "aggregate Bind requires model metadata")
	}
	r.name, r.table, r.get, r.set = name, source.table, get, set
	return r
}

// Using derives this generated slot with another computation of the same source
// and result type, for request-specific scopes. Related still checks its input.
func (r AggregateRelation[M, V]) Using(computation AggregateRelation[M, V]) AggregateRelation[M, V] {
	r.validate, r.fetch = computation.validate, computation.fetch
	r.bindingError = errors.Join(r.bindingError, computation.bindingError)
	return r
}
func (r AggregateRelation[M, V]) relationName() string      { return r.name }
func (r AggregateRelation[M, V]) copyRelation() Relation[M] { return r }
func (r AggregateRelation[M, V]) validateRelation(table string, depth int, limits RelationLimits) error {
	if r.bindingError != nil {
		return r.bindingError
	}
	if r.name == "" || table != r.table || r.get == nil || r.set == nil || r.validate == nil || r.fetch == nil {
		return fault.New(fault.Invalid, "aggregate requires generated metadata, computation and a typed slot")
	}
	return r.validate(table, depth, limits)
}
func (r AggregateRelation[M, V]) loadRelation(ctx context.Context, executor database.Executor, parents []M, state *relationLoadState, depth int, missing bool) ([]M, error) {
	selected, indices := selectedParents(parents, func(m M) bool { return missing && r.get(m).IsLoaded() })
	results, err := r.fetch(ctx, executor, selected, state)
	if err != nil {
		return nil, err
	}
	if err := state.attach(len(results)); err != nil {
		return nil, err
	}
	result := parents // owned by loadRelations, which copied the caller slice once
	for i, v := range results {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		index := indices[i]
		result[index] = r.set(result[index], relation.Computed(v))
	}
	return result, nil
}
