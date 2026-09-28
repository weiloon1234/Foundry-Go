package query

import (
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/value"
)

func (a Aggregate[M, V]) Eq(v V) HavingPredicate[M]         { return a.compare(equal, v) }
func (a Aggregate[M, V]) Ne(v V) HavingPredicate[M]         { return a.compare(notEqual, v) }
func (a Aggregate[M, V]) In(values ...V) HavingPredicate[M] { return a.compare(in, values...) }
func (a Aggregate[M, V]) compare(op operator, values ...V) HavingPredicate[M] {
	return HavingPredicate[M]{expression: typedComparison(a.node, op, a.codec, values)}
}

// OrderedAggregate adds range comparisons to an ordered, non-nullable result.
// Row and field counts expose this capability; boolean existence does not.
type OrderedAggregate[M, V any] struct{ Aggregate[M, V] }

func (a OrderedAggregate[M, V]) Lt(v V) HavingPredicate[M]  { return a.compare(less, v) }
func (a OrderedAggregate[M, V]) Lte(v V) HavingPredicate[M] { return a.compare(lessOrEqual, v) }
func (a OrderedAggregate[M, V]) Gt(v V) HavingPredicate[M]  { return a.compare(greater, v) }
func (a OrderedAggregate[M, V]) Gte(v V) HavingPredicate[M] { return a.compare(greaterOrEqual, v) }

// NullableOrderedAggregate preserves nullable projection/loading results while
// comparisons accept a concrete value. SQL NULL uses IsNull or IsNotNull.
type NullableOrderedAggregate[M, V any] struct {
	Aggregate[M, value.Nullable[V]]
	comparisonCodec codec.Codec[V]
}

func nullableOrderedAggregate[M, V any](node aggregateNode, c codec.Codec[V]) NullableOrderedAggregate[M, V] {
	return NullableOrderedAggregate[M, V]{Aggregate: Aggregate[M, value.Nullable[V]]{node: node, codec: codec.Nullable(c)}, comparisonCodec: c}
}
func (a NullableOrderedAggregate[M, V]) Eq(v V) HavingPredicate[M] { return a.compare(equal, v) }
func (a NullableOrderedAggregate[M, V]) Ne(v V) HavingPredicate[M] { return a.compare(notEqual, v) }
func (a NullableOrderedAggregate[M, V]) In(values ...V) HavingPredicate[M] {
	return a.compare(in, values...)
}
func (a NullableOrderedAggregate[M, V]) Lt(v V) HavingPredicate[M]  { return a.compare(less, v) }
func (a NullableOrderedAggregate[M, V]) Lte(v V) HavingPredicate[M] { return a.compare(lessOrEqual, v) }
func (a NullableOrderedAggregate[M, V]) Gt(v V) HavingPredicate[M]  { return a.compare(greater, v) }
func (a NullableOrderedAggregate[M, V]) Gte(v V) HavingPredicate[M] {
	return a.compare(greaterOrEqual, v)
}
func (a NullableOrderedAggregate[M, V]) IsNull() HavingPredicate[M]    { return a.compare(isNull) }
func (a NullableOrderedAggregate[M, V]) IsNotNull() HavingPredicate[M] { return a.compare(isNotNull) }
func (a NullableOrderedAggregate[M, V]) compare(op operator, values ...V) HavingPredicate[M] {
	return HavingPredicate[M]{expression: typedComparison(a.node, op, a.comparisonCodec, values)}
}
