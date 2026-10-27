package expr

import (
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"github.com/weiloon1234/Foundry-Go/value"
)

func windowExpression[S, V any](kind querycore.WindowFunction, n int32, w Window[S], c ormcore.Column[V]) Selected[S, V] {
	core := ormcore.WindowExpression(ownedWindow(w), querycore.WindowNode{Kind: kind, N: querycore.RetainOffset(int64(n)), Err: c.Err}, nil)
	return Selected[S, V]{data: rowData[V]{core: core, column: c}}
}

// Over applies a frame to an aggregate, retaining its codec and nullability.
// FILTER must precede Over; COUNT DISTINCT windows are invalid. Example: expr.Over(expr.Count[User](),w).
func Over[S, V any](aggregate AggregateExpression[S, V], w Window[S]) Selected[S, V] {
	core, c := FoundryAggregate(aggregate, sqlowner.Expressions())
	node, _ := core.Node.(querycore.AggregateNode)
	return Selected[S, V]{data: rowData[V]{column: c, core: ormcore.WindowExpression(ownedWindow(w), querycore.WindowNode{Kind: querycore.AggregateWindow, Aggregate: node, Err: c.Err}, &core)}}
}
func windowValue[S, V any](kind querycore.WindowFunction, input Selected[S, V], n int32, w Window[S]) Selected[S, V] {
	input.data.core = ormcore.WindowExpression(ownedWindow(w), querycore.WindowNode{Kind: kind, Input: input.data.core.Node, N: querycore.RetainOffset(int64(n)), Err: input.data.column.Err}, &input.data.core)
	return input
}
func windowFallback[S, V any](kind querycore.WindowFunction, input Selected[S, V], n int32, fallback V, w Window[S]) Selected[S, V] {
	captured := parameter[S](input.data, fallback)
	parameterNode := captured.data.core.Node.(querycore.ParameterNode)
	node := querycore.WindowNode{Kind: kind, Input: input.data.core.Node, N: querycore.RetainOffset(int64(n)), HasFallback: true, Fallback: parameterNode.Value, Err: safeerror.Join("operation failed", input.data.column.Err, captured.data.core.Err)}
	input.data.core = ormcore.WindowExpression(ownedWindow(w), node, &input.data.core)
	return input
}

// RowNumber numbers partition rows from one. Example: expr.RowNumber(w).
func RowNumber[S any](w Window[S]) Selected[S, int64] {
	return windowExpression(querycore.RowNumberWindow, 0, w, columnCodec(codec.Signed[int64]()))
}

// Rank ranks peers equally, leaving gaps. Example: expr.Rank(w).
func Rank[S any](w Window[S]) Selected[S, int64] {
	return windowExpression(querycore.RankWindow, 0, w, columnCodec(codec.Signed[int64]()))
}

// DenseRank ranks peers equally without gaps. Example: expr.DenseRank(w).
func DenseRank[S any](w Window[S]) Selected[S, int64] {
	return windowExpression(querycore.DenseRankWindow, 0, w, columnCodec(codec.Signed[int64]()))
}

// PercentRank returns the relative rank in [0,1]. Example: expr.PercentRank(w).
func PercentRank[S any](w Window[S]) Selected[S, float64] {
	return windowExpression(querycore.PercentRankWindow, 0, w, columnCodec(codec.Float[float64]()))
}

// CumeDist returns the cumulative partition fraction. Example: expr.CumeDist(w).
func CumeDist[S any](w Window[S]) Selected[S, float64] {
	return windowExpression(querycore.CumeDistWindow, 0, w, columnCodec(codec.Float[float64]()))
}

// NTile assigns rows to a positive number of buckets. Example: expr.NTile(4,w).
func NTile[S any](buckets int32, w Window[S]) Selected[S, int32] {
	return windowExpression(querycore.NtileWindow, buckets, w, columnCodec(codec.Signed[int32]()))
}

// Lag returns the offset value or NULL when absent; negative offsets reverse direction.
// Example: expr.Lag(expr.Row(amount).Value(),1,w).
func Lag[S, V any](input Selected[S, V], offset int32, w Window[S]) Selected[S, value.Nullable[V]] {
	return windowValue(querycore.LagWindow, Nullable(input), offset, w)
}

// LagNullable preserves exactly one nullable layer. Example: expr.LagNullable(expr.Row(optional).Value(),1,w).
func LagNullable[S, V any](input Selected[S, value.Nullable[V]], offset int32, w Window[S]) Selected[S, value.Nullable[V]] {
	return windowValue(querycore.LagWindow, input, offset, w)
}

// LagOr uses a captured typed fallback only when the offset row is absent.
// NULL in an existing row remains NULL. Example: expr.LagOr(expr.Row(amount).Value(),1,int64(0),w).
func LagOr[S, V any](input Selected[S, V], offset int32, fallback V, w Window[S]) Selected[S, V] {
	return windowFallback(querycore.LagWindow, input, offset, fallback, w)
}

// Lead returns the offset value or NULL when absent; negative offsets reverse direction.
// Example: expr.Lead(expr.Row(amount).Value(),1,w).
func Lead[S, V any](input Selected[S, V], offset int32, w Window[S]) Selected[S, value.Nullable[V]] {
	return windowValue(querycore.LeadWindow, Nullable(input), offset, w)
}

// LeadNullable preserves exactly one nullable layer. Example: expr.LeadNullable(expr.Row(optional).Value(),1,w).
func LeadNullable[S, V any](input Selected[S, value.Nullable[V]], offset int32, w Window[S]) Selected[S, value.Nullable[V]] {
	return windowValue(querycore.LeadWindow, input, offset, w)
}

// LeadOr uses a captured typed fallback only when the offset row is absent.
// NULL in an existing row remains NULL. Example: expr.LeadOr(expr.Row(amount).Value(),1,int64(0),w).
func LeadOr[S, V any](input Selected[S, V], offset int32, fallback V, w Window[S]) Selected[S, V] {
	return windowFallback(querycore.LeadWindow, input, offset, fallback, w)
}

// FirstValue returns a frame value, or NULL when absent. Example: expr.FirstValue(expr.Row(amount).Value(),w).
func FirstValue[S, V any](input Selected[S, V], w Window[S]) Selected[S, value.Nullable[V]] {
	return windowValue(querycore.FirstValueWindow, Nullable(input), 0, w)
}

// FirstNullableValue preserves one nullable layer. Example: expr.FirstNullableValue(expr.Row(optional).Value(),w).
func FirstNullableValue[S, V any](input Selected[S, value.Nullable[V]], w Window[S]) Selected[S, value.Nullable[V]] {
	return windowValue(querycore.FirstValueWindow, input, 0, w)
}

// LastValue returns a frame value, or NULL when absent. Example: expr.LastValue(expr.Row(amount).Value(),w).
func LastValue[S, V any](input Selected[S, V], w Window[S]) Selected[S, value.Nullable[V]] {
	return windowValue(querycore.LastValueWindow, Nullable(input), 0, w)
}

// LastNullableValue preserves one nullable layer. Example: expr.LastNullableValue(expr.Row(optional).Value(),w).
func LastNullableValue[S, V any](input Selected[S, value.Nullable[V]], w Window[S]) Selected[S, value.Nullable[V]] {
	return windowValue(querycore.LastValueWindow, input, 0, w)
}

// NthValue returns a frame value, or NULL when absent. Example: expr.NthValue(expr.Row(amount).Value(),2,w).
func NthValue[S, V any](input Selected[S, V], n int32, w Window[S]) Selected[S, value.Nullable[V]] {
	return windowValue(querycore.NthValueWindow, Nullable(input), n, w)
}

// NthNullableValue preserves one nullable layer. Example: expr.NthNullableValue(expr.Row(optional).Value(),2,w).
func NthNullableValue[S, V any](input Selected[S, value.Nullable[V]], n int32, w Window[S]) Selected[S, value.Nullable[V]] {
	return windowValue(querycore.NthValueWindow, input, n, w)
}
