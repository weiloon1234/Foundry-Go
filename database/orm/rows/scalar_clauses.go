package rows

import (
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/value"
)

// OrderScalar appends row or selected ordering inside the scalar SELECT.
// Example: rows.OrderScalar(selected, expr.Asc(age)).
func OrderScalar[S, V any](q Scalar[S, V], orders ...expr.ProjectionOrder[S]) Scalar[S, V] {
	q.source = recordOrders(q.source, orders)
	return q
}

// LimitScalar replaces the inner limit without changing scalar cardinality rules.
// Example: rows.LimitScalar(selected, 1).
func LimitScalar[S, V any](q Scalar[S, V], n int) Scalar[S, V] {
	q.source.Node.Limit = value.Set(n)
	return q
}

// OffsetScalar replaces the offset inside the scalar SELECT.
// Example: rows.OffsetScalar(selected, 1).
func OffsetScalar[S, V any](q Scalar[S, V], n int) Scalar[S, V] {
	q.source.Node.Offset = n
	return q
}

// GroupByScalar appends row keys inside the scalar SELECT.
// Example: rows.GroupByScalar(selected, expr.Group(buyer)).
func GroupByScalar[S, V any](q Scalar[S, V], keys ...expr.GroupKey[S]) Scalar[S, V] {
	q.source = GroupBy(Query[S, struct{}]{source: q.source}, keys...).source
	return q
}

// HavingScalar appends group predicates inside the scalar SELECT.
// Example: rows.HavingScalar(selected, expr.GtValue(count, 0)).
func HavingScalar[S, V any](q Scalar[S, V], predicates ...expr.HavingPredicate[S]) Scalar[S, V] {
	q.source = Having(Query[S, struct{}]{source: q.source}, predicates...).source
	return q
}

// DistinctScalar removes duplicate scalar results, replacing DISTINCT ON.
// Example: rows.DistinctScalar(selected).
func DistinctScalar[S, V any](q Scalar[S, V]) Scalar[S, V] {
	q.source = Distinct(Query[S, struct{}]{source: q.source}).source
	return q
}

// DistinctOnScalar selects one scalar per row-key combination; order by keys first.
// Example: rows.DistinctOnScalar(selected, expr.Group(buyer)).
func DistinctOnScalar[S, V any](q Scalar[S, V], keys ...expr.GroupKey[S]) Scalar[S, V] {
	q.source = DistinctOn(Query[S, struct{}]{source: q.source}, keys...).source
	return q
}

// DistinctOnScalarValues accepts selected keys and retains group/window validation.
// Example: rows.DistinctOnScalarValues(selected, expr.Key(expr.AsSelected(count))).
func DistinctOnScalarValues[S, V any](q Scalar[S, V], keys ...expr.ProjectionKey[S]) Scalar[S, V] {
	q.source = DistinctOnValues(Query[S, struct{}]{source: q.source}, keys...).source
	return q
}
