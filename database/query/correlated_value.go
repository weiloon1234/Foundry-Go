package query

import (
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

// CorrelatedValueQuery retains outer ownership while selecting a single typed
// inner value. It cannot execute independently or enter an uncorrelated API.
type CorrelatedValueQuery[O, I, V any] struct {
	query ValueQuery[Correlation[O, I], V]
	outer scopeRequirement
}

// SelectCorrelatedValue selects one inner value while retaining its required
// outer scope. Consume it through a correlated scalar, membership or Exists.
func SelectCorrelatedValue[O, I, V any](source CorrelatedSource[O, I], expression Expression[Correlation[O, I], V]) CorrelatedValueQuery[O, I, V] {
	return CorrelatedValueQuery[O, I, V]{query: SelectValue(source.source, expression), outer: source.outer.scopeRequirement}
}
func (q CorrelatedValueQuery[O, I, V]) Where(predicates ...Predicate[Correlation[O, I]]) CorrelatedValueQuery[O, I, V] {
	q.query = q.query.Where(predicates...)
	return q
}
func (q CorrelatedValueQuery[O, I, V]) GroupBy(groups ...Group[Correlation[O, I]]) CorrelatedValueQuery[O, I, V] {
	q.query = q.query.GroupBy(groups...)
	return q
}
func (q CorrelatedValueQuery[O, I, V]) Having(predicates ...HavingPredicate[Correlation[O, I]]) CorrelatedValueQuery[O, I, V] {
	q.query = q.query.Having(predicates...)
	return q
}
func (q CorrelatedValueQuery[O, I, V]) OrderBy(orders ...ProjectionOrder[Correlation[O, I]]) CorrelatedValueQuery[O, I, V] {
	q.query = q.query.OrderBy(orders...)
	return q
}
func (q CorrelatedValueQuery[O, I, V]) Limit(count int) CorrelatedValueQuery[O, I, V] {
	q.query = q.query.Limit(count)
	return q
}
func (q CorrelatedValueQuery[O, I, V]) Offset(count int) CorrelatedValueQuery[O, I, V] {
	q.query = q.query.Offset(count)
	return q
}

// Exists tests the selected inner result, including grouping and its window.
func (q CorrelatedValueQuery[O, I, V]) Exists() Predicate[O] {
	return Predicate[O]{expression: subqueryPredicate{query: q.correlatedValue().value.subquery}}
}

// CorrelatedValueSource preserves both the required outer scope and result type.
type CorrelatedValueSource[O, V any] interface{ correlatedValue() correlatedValue[O, V] }
type correlatedValue[O, V any] struct {
	_     [0]*O
	value valueSubquery[V]
}

func (q CorrelatedValueQuery[O, I, V]) correlatedValue() correlatedValue[O, V] {
	r := q.query.valueQuery()
	r.correlation = &q.outer
	return correlatedValue[O, V]{value: r}
}
func correlatedValueOf[O, V any](source CorrelatedValueSource[O, V]) valueSubquery[V] {
	if nilDescriptor(source) {
		return valueSubquery[V]{subquery: subquery{err: fault.New(fault.Invalid, "correlated value requires a query")}}
	}
	return source.correlatedValue().value
}

// CorrelatedScalarQuery yields a nullable scalar in its required outer scope.
// No row becomes NULL; multiple rows remain a database cardinality error.
func CorrelatedScalarQuery[O, V any](source CorrelatedValueSource[O, V]) Expression[O, value.Nullable[V]] {
	q := correlatedValueOf(source)
	if value.IsNullableType[V]() {
		q.err = fault.New(fault.Invalid, "nullable correlated scalar inputs require CorrelatedScalarNullableQuery or CorrelatedScalarNullableRowQuery")
	}
	return Expression[O, value.Nullable[V]]{node: scalarSubquery{query: q.subquery}, codec: codec.Nullable(q.codec)}
}

// CorrelatedScalarNullableQuery retains one nullable layer for nullable inputs.
func CorrelatedScalarNullableQuery[O, V any](source CorrelatedValueSource[O, value.Nullable[V]]) Expression[O, value.Nullable[V]] {
	q := correlatedValueOf(source)
	return Expression[O, value.Nullable[V]]{node: scalarSubquery{query: q.subquery}, codec: q.codec}
}

// InCorrelatedQuery compares the field with values bound to this outer scope.
func (f valueField[M, V]) InCorrelatedQuery(source CorrelatedValueSource[M, V]) Predicate[M] {
	return f.inSubquery(correlatedValueOf(source).subquery)
}

// InNullableCorrelatedQuery explicitly admits nullable inner values without
// removing NULL rows or changing SQL's three-valued membership semantics.
func (f valueField[M, V]) InNullableCorrelatedQuery(source CorrelatedValueSource[M, value.Nullable[V]]) Predicate[M] {
	return f.inSubquery(correlatedValueOf(source).subquery)
}
