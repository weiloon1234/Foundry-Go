package query

import (
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

// TransactionCorrelatedValueQuery preserves the exact single-column result,
// declared outer scope and inner locks. It has no standalone execution methods.
type TransactionCorrelatedValueQuery[O, I TransactionScope, V any] struct {
	query ValueQuery[TransactionCorrelation[O, I], V]
	outer scopeRequirement
}

// SelectTransactionCorrelatedValue selects a value for a Tx-owned parent expression.
func SelectTransactionCorrelatedValue[O, I TransactionScope, V any](source TransactionCorrelatedSource[O, I], expression Expression[TransactionCorrelation[O, I], V]) TransactionCorrelatedValueQuery[O, I, V] {
	return TransactionCorrelatedValueQuery[O, I, V]{query: SelectValue(source.source, expression), outer: source.outer.scopeRequirement}
}
func (q TransactionCorrelatedValueQuery[O, I, V]) Where(predicates ...Predicate[TransactionCorrelation[O, I]]) TransactionCorrelatedValueQuery[O, I, V] {
	q.query = q.query.Where(predicates...)
	return q
}
func (q TransactionCorrelatedValueQuery[O, I, V]) OrderBy(orders ...ProjectionOrder[TransactionCorrelation[O, I]]) TransactionCorrelatedValueQuery[O, I, V] {
	q.query = q.query.OrderBy(orders...)
	return q
}
func (q TransactionCorrelatedValueQuery[O, I, V]) GroupBy(groups ...Group[TransactionCorrelation[O, I]]) TransactionCorrelatedValueQuery[O, I, V] {
	q.query = q.query.GroupBy(groups...)
	return q
}
func (q TransactionCorrelatedValueQuery[O, I, V]) Having(predicates ...HavingPredicate[TransactionCorrelation[O, I]]) TransactionCorrelatedValueQuery[O, I, V] {
	q.query = q.query.Having(predicates...)
	return q
}
func (q TransactionCorrelatedValueQuery[O, I, V]) Limit(count int) TransactionCorrelatedValueQuery[O, I, V] {
	q.query = q.query.Limit(count)
	return q
}
func (q TransactionCorrelatedValueQuery[O, I, V]) Offset(count int) TransactionCorrelatedValueQuery[O, I, V] {
	q.query = q.query.Offset(count)
	return q
}

// TransactionCorrelatedValueSource preserves the Tx-required parent and exact
// result type. It deliberately does not implement CorrelatedValueSource.
type TransactionCorrelatedValueSource[O TransactionScope, V any] interface{ transactionCorrelatedValue() correlatedValue[O, V] }

func (q TransactionCorrelatedValueQuery[O, I, V]) transactionCorrelatedValue() correlatedValue[O, V] {
	result := q.query.valueQuery()
	result.correlation = &q.outer
	return correlatedValue[O, V]{value: result}
}
func transactionCorrelatedValueOf[O TransactionScope, V any](source TransactionCorrelatedValueSource[O, V]) valueSubquery[V] {
	if nilDescriptor(source) {
		return valueSubquery[V]{subquery: subquery{err: fault.New(fault.Invalid, "transaction correlated value requires a source")}}
	}
	return source.transactionCorrelatedValue().value
}

type transactionCorrelatedValueAdapter[O TransactionScope, V any] struct{ value valueSubquery[V] }

func (a transactionCorrelatedValueAdapter[O, V]) correlatedValue() correlatedValue[O, V] {
	return correlatedValue[O, V]{value: a.value}
}

// Exists retains the parent requirement, inner result window and locks.
func (q TransactionCorrelatedValueQuery[O, I, V]) Exists() Predicate[O] {
	return Predicate[O]{expression: subqueryPredicate{query: q.transactionCorrelatedValue().value.subquery}}
}

// TransactionCorrelatedScalarQuery returns NULL for no row and preserves database cardinality errors.
func TransactionCorrelatedScalarQuery[O TransactionScope, V any](source TransactionCorrelatedValueSource[O, V]) Expression[O, value.Nullable[V]] {
	return CorrelatedScalarQuery(transactionCorrelatedValueAdapter[O, V]{value: transactionCorrelatedValueOf(source)})
}

// TransactionCorrelatedScalarNullableQuery preserves one nullable layer.
func TransactionCorrelatedScalarNullableQuery[O TransactionScope, V any](source TransactionCorrelatedValueSource[O, value.Nullable[V]]) Expression[O, value.Nullable[V]] {
	return CorrelatedScalarNullableQuery(transactionCorrelatedValueAdapter[O, value.Nullable[V]]{value: transactionCorrelatedValueOf(source)})
}

// TransactionCorrelatedScalarRowQuery makes the inner scalar available to parent row predicates.
func TransactionCorrelatedScalarRowQuery[O TransactionScope, V any](source TransactionCorrelatedValueSource[O, V]) RowExpression[O, value.Nullable[V]] {
	return RowExpression[O, value.Nullable[V]]{TransactionCorrelatedScalarQuery(source)}
}

// TransactionCorrelatedScalarNullableRowQuery preserves nullable parent row values.
func TransactionCorrelatedScalarNullableRowQuery[O TransactionScope, V any](source TransactionCorrelatedValueSource[O, value.Nullable[V]]) RowExpression[O, value.Nullable[V]] {
	return RowExpression[O, value.Nullable[V]]{TransactionCorrelatedScalarNullableQuery(source)}
}

// TransactionInCorrelatedQuery compares a parent row value with a locked correlated SELECT.
func TransactionInCorrelatedQuery[O TransactionScope, V any](left RowValue[O, V], source TransactionCorrelatedValueSource[O, V]) Predicate[O] {
	return transactionMembership(left, transactionCorrelatedValueOf(source).subquery)
}

// TransactionInNullableCorrelatedQuery explicitly admits NULL inner results.
func TransactionInNullableCorrelatedQuery[O TransactionScope, V any](left RowValue[O, V], source TransactionCorrelatedValueSource[O, value.Nullable[V]]) Predicate[O] {
	return transactionMembership(left, transactionCorrelatedValueOf(source).subquery)
}
