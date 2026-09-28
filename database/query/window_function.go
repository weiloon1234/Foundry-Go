package query

import (
	"database/sql/driver"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

type windowFunction uint8

const (
	rowNumberWindow windowFunction = iota + 1
	rankWindow
	denseRankWindow
	percentRankWindow
	cumeDistWindow
	ntileWindow
	lagWindow
	leadWindow
	firstValueWindow
	lastValueWindow
	nthValueWindow
	aggregateWindow
)

type windowNode struct {
	kind        windowFunction
	window      windowSpec
	input       valueExpression
	aggregate   aggregateNode
	n           int32
	fallback    driver.Value
	hasFallback bool
	err         error
}

func (windowNode) valueNode() {}

func windowExpression[S, V any](kind windowFunction, w Window[S], c codec.Codec[V]) Expression[S, V] {
	return Expression[S, V]{node: windowNode{kind: kind, window: w.node}, codec: c}
}

// RowNumber numbers partition rows from one. Order with a unique tie-breaker for stability.
func RowNumber[S any](w Window[S]) Expression[S, int64] {
	return windowExpression(rowNumberWindow, w, codec.Signed[int64]())
}

// Rank gives ordering peers the same rank, leaving gaps after ties.
func Rank[S any](w Window[S]) Expression[S, int64] {
	return windowExpression(rankWindow, w, codec.Signed[int64]())
}

// DenseRank gives ordering peers the same rank without gaps.
func DenseRank[S any](w Window[S]) Expression[S, int64] {
	return windowExpression(denseRankWindow, w, codec.Signed[int64]())
}

// PercentRank computes relative rank in [0, 1].
func PercentRank[S any](w Window[S]) Expression[S, float64] {
	return windowExpression(percentRankWindow, w, codec.Float[float64]())
}

// CumeDist computes the fraction of partition rows preceding or tied with this row.
func CumeDist[S any](w Window[S]) Expression[S, float64] {
	return windowExpression(cumeDistWindow, w, codec.Float[float64]())
}

// NTile divides a partition as evenly as possible into a positive number of buckets.
func NTile[S any](buckets int32, w Window[S]) Expression[S, int32] {
	e := windowExpression(ntileWindow, w, codec.Signed[int32]())
	n := e.node.(windowNode)
	n.n = buckets
	e.node = n
	return e
}

// Over evaluates this aggregate for each row's window frame, preserving its
// codec and nullability. PostgreSQL does not support CountDistinct as a window.
func (a Aggregate[S, V]) Over(w Window[S]) Expression[S, V] {
	return Expression[S, V]{node: windowNode{kind: aggregateWindow, window: w.node, aggregate: a.node}, codec: a.codec}
}

func windowValue[S, V any](kind windowFunction, input Expression[S, V], n int32, w Window[S]) Expression[S, V] {
	return Expression[S, V]{node: windowNode{kind: kind, input: input.node, n: n, window: w.node}, codec: input.codec}
}
func nullableWindowValue[S, V any](kind windowFunction, input Expression[S, V], n int32, w Window[S]) Expression[S, value.Nullable[V]] {
	e := windowValue(kind, input, n, w)
	node := e.node.(windowNode)
	if value.IsNullableType[V]() {
		node.err = fault.New(fault.Invalid, "nullable window input requires its nullable value helper")
	}
	return Expression[S, value.Nullable[V]]{node: node, codec: codec.Nullable(input.codec)}
}

// Lag returns the value offset rows before this row, or NULL if absent. It uses
// the partition, independent of the frame. Negative offsets look forward.
func Lag[S, V any](input Expression[S, V], offset int32, w Window[S]) Expression[S, value.Nullable[V]] {
	return nullableWindowValue(lagWindow, input, offset, w)
}

// Lead returns the value offset rows after this row, or NULL if absent.
func Lead[S, V any](input Expression[S, V], offset int32, w Window[S]) Expression[S, value.Nullable[V]] {
	return nullableWindowValue(leadWindow, input, offset, w)
}

// LagNullable preserves one nullable layer for an already nullable input.
func LagNullable[S, V any](input Expression[S, value.Nullable[V]], offset int32, w Window[S]) Expression[S, value.Nullable[V]] {
	return windowValue(lagWindow, input, offset, w)
}

// LeadNullable preserves one nullable layer for an already nullable input.
func LeadNullable[S, V any](input Expression[S, value.Nullable[V]], offset int32, w Window[S]) Expression[S, value.Nullable[V]] {
	return windowValue(leadWindow, input, offset, w)
}

func windowFallback[S, V any](kind windowFunction, input Expression[S, V], offset int32, fallback V, w Window[S]) Expression[S, V] {
	e := windowValue(kind, input, offset, w)
	n := e.node.(windowNode)
	n.hasFallback = true
	n.fallback, n.err = input.codec.Bind(fallback)
	e.node = n
	return e
}

// LagOr substitutes the typed fallback only when the offset row is absent.
// A NULL value in an existing row remains NULL. The fallback is encoded once.
func LagOr[S, V any](input Expression[S, V], offset int32, fallback V, w Window[S]) Expression[S, V] {
	return windowFallback(lagWindow, input, offset, fallback, w)
}

// LeadOr is the forward counterpart of LagOr, preserving the input value type.
func LeadOr[S, V any](input Expression[S, V], offset int32, fallback V, w Window[S]) Expression[S, V] {
	return windowFallback(leadWindow, input, offset, fallback, w)
}

// FirstValue returns the frame's first value; an empty frame becomes NULL.
func FirstValue[S, V any](input Expression[S, V], w Window[S]) Expression[S, value.Nullable[V]] {
	return nullableWindowValue(firstValueWindow, input, 0, w)
}

// LastValue returns the frame's last value; an empty frame becomes NULL.
func LastValue[S, V any](input Expression[S, V], w Window[S]) Expression[S, value.Nullable[V]] {
	return nullableWindowValue(lastValueWindow, input, 0, w)
}

// NthValue returns the one-based nth frame value, or NULL when absent.
func NthValue[S, V any](input Expression[S, V], n int32, w Window[S]) Expression[S, value.Nullable[V]] {
	return nullableWindowValue(nthValueWindow, input, n, w)
}

// FirstNullableValue preserves the nullable frame value without nesting wrappers.
func FirstNullableValue[S, V any](input Expression[S, value.Nullable[V]], w Window[S]) Expression[S, value.Nullable[V]] {
	return windowValue(firstValueWindow, input, 0, w)
}

// LastNullableValue preserves the nullable frame value without nesting wrappers.
func LastNullableValue[S, V any](input Expression[S, value.Nullable[V]], w Window[S]) Expression[S, value.Nullable[V]] {
	return windowValue(lastValueWindow, input, 0, w)
}

// NthNullableValue preserves the nullable frame value without nesting wrappers.
func NthNullableValue[S, V any](input Expression[S, value.Nullable[V]], n int32, w Window[S]) Expression[S, value.Nullable[V]] {
	return windowValue(nthValueWindow, input, n, w)
}
