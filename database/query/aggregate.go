package query

import (
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

type aggregateKind = querycore.AggregateKind

const countAll = querycore.CountAll

const countValues = querycore.CountValues

const countDistinct = querycore.CountDistinct

const sumValues = querycore.SumValues

const averageValues = querycore.AverageValues

const minimumValue = querycore.MinimumValue

const maximumValue = querycore.MaximumValue

const existsValues = querycore.ExistsValues

type aggregateCast = querycore.AggregateCast

const nativeNumber = querycore.NativeNumber

const exactNumberCast = querycore.ExactNumberCast

const floatNumberCast = querycore.FloatNumberCast

const intervalValueCast = querycore.IntervalValueCast

type aggregateNode = querycore.AggregateNode

// Aggregate carries the input model and exact result type. Generated fields
// expose only supported operations. Zero aggregates are invalid declarations.
type Aggregate[M, V any] struct {
	_     [0]*M
	node  aggregateNode
	codec codec.Codec[V]
}

// AggregateExpression accepts the supported aggregate capabilities while
// retaining their exact model and result type through relation declarations.
type AggregateExpression[M, V any] interface{ aggregateValue() Aggregate[M, V] }

func (a Aggregate[M, V]) aggregateValue() Aggregate[M, V] { return a }

// Count counts rows, including rows with NULL fields. An empty group is zero.
func Count[M any]() OrderedAggregate[M, int64] {
	return OrderedAggregate[M, int64]{Aggregate[M, int64]{node: aggregateNode{Kind: countAll}, codec: codec.Signed[int64]()}}
}

// Exists reports whether the group contains any rows, including NULL fields.
func Exists[M any]() Aggregate[M, bool] {
	return Aggregate[M, bool]{node: aggregateNode{Kind: existsValues}, codec: codec.Bool[bool]()}
}

// countField has no field-value type parameter: all stored types use the same
// owner-bound int64 aggregate and shared codec.
func countField[M any](field fieldRef, kind aggregateKind) OrderedAggregate[M, int64] {
	return OrderedAggregate[M, int64]{Aggregate[M, int64]{node: aggregateNode{Kind: kind, Field: field}, codec: codec.Signed[int64]()}}
}

func fieldExtremum[M any, V comparable](f ScalarField[M, V], kind aggregateKind) NullableOrderedAggregate[M, V] {
	return nullableOrderedAggregate[M](aggregateNode{Kind: kind, Field: f.ref}, f.codec)
}
