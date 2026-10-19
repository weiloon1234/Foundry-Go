package query

import (
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

type aggregateFilter = querycore.AggregateFilter

// Filter appends row predicates to this aggregate only, joined with AND. It
// preserves the aggregate's codec and empty-input behavior. Query Where still
// filters input for every aggregate; Having filters completed groups. Apply
// Filter before Over when evaluating a filtered aggregate over a window.
func (a Aggregate[M, V]) Filter(predicates ...Predicate[M]) Aggregate[M, V] {
	if len(predicates) == 0 {
		return a
	}
	var existing []expression
	if a.node.Filter != nil {
		existing = a.node.Filter.Predicates
	}
	a.node.Filter = &aggregateFilter{Predicates: appendPredicates(existing, predicates)}
	return a
}

// Filter retains ordered comparisons such as Gt and Gte on a filtered count.
func (a OrderedAggregate[M, V]) Filter(predicates ...Predicate[M]) OrderedAggregate[M, V] {
	a.Aggregate = a.Aggregate.Filter(predicates...)
	return a
}

// Filter retains concrete-value comparisons and nullable result decoding.
func (a NullableOrderedAggregate[M, V]) Filter(predicates ...Predicate[M]) NullableOrderedAggregate[M, V] {
	a.Aggregate = a.Aggregate.Filter(predicates...)
	return a
}

func requalifyAggregate(a aggregateNode, alias string) aggregateNode {
	if a.Field != (fieldRef{}) {
		a.Field.Table = alias
	}
	if a.Filter != nil {
		predicates := make([]expression, len(a.Filter.Predicates))
		for i, predicate := range a.Filter.Predicates {
			predicates[i] = requalify(predicate, alias)
		}
		a.Filter = &aggregateFilter{Predicates: predicates}
	}
	return a
}
