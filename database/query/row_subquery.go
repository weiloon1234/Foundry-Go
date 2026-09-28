package query

import "github.com/weiloon1234/Foundry-Go/value"

// ScalarRowQuery evaluates an independent scalar subquery in a row expression.
// Empty results become NULL; multiple rows remain a database cardinality error.
// The inner SELECT retains its own aggregate/window evaluation phase.
func ScalarRowQuery[S, V any](outer ProjectionSource[S], inner ValueQuerySource[V]) RowExpression[S, value.Nullable[V]] {
	return RowExpression[S, value.Nullable[V]]{ScalarQuery(outer, inner)}
}

// ScalarNullableRowQuery preserves one nullable layer for a nullable inner value.
func ScalarNullableRowQuery[S, V any](outer ProjectionSource[S], inner ValueQuerySource[value.Nullable[V]]) RowExpression[S, value.Nullable[V]] {
	return RowExpression[S, value.Nullable[V]]{ScalarNullableQuery(outer, inner)}
}

// CorrelatedScalarRowQuery evaluates a scalar in its declared outer row scope.
// It preserves explicit correlation boundaries and database cardinality errors.
func CorrelatedScalarRowQuery[S, V any](inner CorrelatedValueSource[S, V]) RowExpression[S, value.Nullable[V]] {
	return RowExpression[S, value.Nullable[V]]{CorrelatedScalarQuery(inner)}
}

// CorrelatedScalarNullableRowQuery preserves an already-nullable inner result.
func CorrelatedScalarNullableRowQuery[S, V any](inner CorrelatedValueSource[S, value.Nullable[V]]) RowExpression[S, value.Nullable[V]] {
	return RowExpression[S, value.Nullable[V]]{CorrelatedScalarNullableQuery(inner)}
}
