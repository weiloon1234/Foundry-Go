package expr

import (
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/value"
	"reflect"
)

// HavingPredicate is a group condition and cannot enter row WHERE. Example: expr.Count[User]().Gt(0).
type HavingPredicate[S any] struct {
	havingPhase [0]struct{}
	_           [0]*S
	core        ormcore.Predicate
}

// Aggregate is an unordered selected aggregate. Example: expr.Exists[User]().
type Aggregate[S, V any] struct {
	aggregatePhase [0]struct{}
	_              [0]*S
	data           rowData[V]
}

// OrderedAggregate is an ordered selected aggregate. Example: expr.Count[User]().Gt(1).
type OrderedAggregate[S any, V orderedScalar] struct {
	aggregatePhase [0]struct{}
	_              [0]*S
	data           rowData[V]
}

// Count counts rows including NULL fields; empty input returns zero. Example: expr.Count[User]().
func Count[S any]() OrderedAggregate[S, int64] {
	return OrderedAggregate[S, int64]{data: rowData[int64]{core: ormcore.Expression{Node: querycore.AggregateNode{Kind: querycore.CountAll}, Refs: []ormcore.Reference{{Scope: reflect.TypeFor[S]()}}, ReferenceFree: true}, column: columnCodec(codec.Signed[int64]())}}
}

// Exists reports whether any input row exists; empty input is false. Example: expr.Exists[User]().
func Exists[S any]() Aggregate[S, bool] {
	return Aggregate[S, bool]{data: rowData[bool]{core: ormcore.Expression{Node: querycore.AggregateNode{Kind: querycore.ExistsValues}, Refs: []ormcore.Reference{{Scope: reflect.TypeFor[S]()}}, ReferenceFree: true}, column: columnCodec(codec.Bool[bool]())}}
}

// ValueComparison compares selected values without exposing row predicates. Example: expr.CompareValue(selected).Eq(value).
type ValueComparison[S, V any] struct {
	selectedPhase [0]struct{}
	_             [0]*S
	data          rowData[V]
}

// OrderedValueComparison compares ordered selected values. Example: expr.OrderValue(selected).Gt(value).
type OrderedValueComparison[S any, V orderedScalar] struct {
	selectedPhase [0]struct{}
	_             [0]*S
	data          rowData[V]
}

// NullableOrderedValueComparison retains nullable results with present-value comparisons. Example: expr.OrderNullableValue(selected).Gt(value).
type NullableOrderedValueComparison[S any, V orderedScalar] struct {
	selectedPhase [0]struct{}
	_             [0]*S
	data          rowData[value.Nullable[V]]
}

// CompareValue exposes equality comparisons for a selected value. Example: expr.CompareValue(expr.Exists[User]().Value()).Eq(true).
func CompareValue[S, V any](v Selected[S, V]) ValueComparison[S, V] {
	return ValueComparison[S, V]{data: v.data}
}

// OrderValue exposes ordered comparisons for a selected value. Example: expr.OrderValue(expr.Count[User]().Value()).Gt(0).
func OrderValue[S any, V orderedScalar](v Selected[S, V]) OrderedValueComparison[S, V] {
	return OrderedValueComparison[S, V]{data: v.data}
}

// OrderNullableValue compares nullable selected results with present values. Example: expr.OrderNullableValue(v).Gt(0).
func OrderNullableValue[S any, V orderedScalar](v Selected[S, value.Nullable[V]]) NullableOrderedValueComparison[S, V] {
	return NullableOrderedValueComparison[S, V]{data: v.data}
}

// Value retains the aggregate result type. Example: aggregate.Value().
func (a Aggregate[S, V]) Value() Selected[S, V] { return Selected[S, V]{data: a.data} }

// Param retains the aggregate result type. Example: aggregate.Param(value).
func (a Aggregate[S, V]) Param(v V) RowExpression[S, V] {
	return parameter[S](a.data, v)
}

// Eq compares groups after selection. Example: expression.Eq(value).
func (a Aggregate[S, V]) Eq(v V) HavingPredicate[S] {
	return HavingPredicate[S]{core: rowCompare(a.data, querycore.Equal, v)}
}

// Ne compares groups after selection. Example: expression.Ne(value).
func (a Aggregate[S, V]) Ne(v V) HavingPredicate[S] {
	return HavingPredicate[S]{core: rowCompare(a.data, querycore.NotEqual, v)}
}

// In compares groups after selection. Example: expression.In(value).
func (a Aggregate[S, V]) In(vs ...V) HavingPredicate[S] {
	return HavingPredicate[S]{core: rowCompare(a.data, querycore.In, vs...)}
}

// Value retains the aggregate result type. Example: aggregate.Value().
func (a OrderedAggregate[S, V]) Value() Selected[S, V] { return Selected[S, V]{data: a.data} }

// Param retains the aggregate result type. Example: aggregate.Param(value).
func (a OrderedAggregate[S, V]) Param(v V) RowExpression[S, V] {
	return parameter[S](a.data, v)
}

// Eq compares groups after selection. Example: expression.Eq(value).
func (a OrderedAggregate[S, V]) Eq(v V) HavingPredicate[S] {
	return HavingPredicate[S]{core: rowCompare(a.data, querycore.Equal, v)}
}

// Ne compares groups after selection. Example: expression.Ne(value).
func (a OrderedAggregate[S, V]) Ne(v V) HavingPredicate[S] {
	return HavingPredicate[S]{core: rowCompare(a.data, querycore.NotEqual, v)}
}

// In compares groups after selection. Example: expression.In(value).
func (a OrderedAggregate[S, V]) In(vs ...V) HavingPredicate[S] {
	return HavingPredicate[S]{core: rowCompare(a.data, querycore.In, vs...)}
}

// Lt compares groups after selection. Example: expression.Lt(value).
func (a OrderedAggregate[S, V]) Lt(v V) HavingPredicate[S] {
	return HavingPredicate[S]{core: rowCompare(a.data, querycore.Less, v)}
}

// Lte compares groups after selection. Example: expression.Lte(value).
func (a OrderedAggregate[S, V]) Lte(v V) HavingPredicate[S] {
	return HavingPredicate[S]{core: rowCompare(a.data, querycore.LessOrEqual, v)}
}

// Gt compares groups after selection. Example: expression.Gt(value).
func (a OrderedAggregate[S, V]) Gt(v V) HavingPredicate[S] {
	return HavingPredicate[S]{core: rowCompare(a.data, querycore.Greater, v)}
}

// Gte compares groups after selection. Example: expression.Gte(value).
func (a OrderedAggregate[S, V]) Gte(v V) HavingPredicate[S] {
	return HavingPredicate[S]{core: rowCompare(a.data, querycore.GreaterOrEqual, v)}
}

// Eq compares groups after selection. Example: expression.Eq(value).
func (a ValueComparison[S, V]) Eq(v V) HavingPredicate[S] {
	return HavingPredicate[S]{core: rowCompare(a.data, querycore.Equal, v)}
}

// Ne compares groups after selection. Example: expression.Ne(value).
func (a ValueComparison[S, V]) Ne(v V) HavingPredicate[S] {
	return HavingPredicate[S]{core: rowCompare(a.data, querycore.NotEqual, v)}
}

// In compares groups after selection. Example: expression.In(value).
func (a ValueComparison[S, V]) In(vs ...V) HavingPredicate[S] {
	return HavingPredicate[S]{core: rowCompare(a.data, querycore.In, vs...)}
}

// IsNull tests a selected SQL value. Example: expression.IsNull().
func (a ValueComparison[S, V]) IsNull() HavingPredicate[S] {
	return HavingPredicate[S]{core: rowCompare(a.data, querycore.IsNull)}
}

// IsNotNull tests a selected SQL value. Example: expression.IsNotNull().
func (a ValueComparison[S, V]) IsNotNull() HavingPredicate[S] {
	return HavingPredicate[S]{core: rowCompare(a.data, querycore.IsNotNull)}
}

// Eq compares groups after selection. Example: expression.Eq(value).
func (a OrderedValueComparison[S, V]) Eq(v V) HavingPredicate[S] {
	return HavingPredicate[S]{core: rowCompare(a.data, querycore.Equal, v)}
}

// Ne compares groups after selection. Example: expression.Ne(value).
func (a OrderedValueComparison[S, V]) Ne(v V) HavingPredicate[S] {
	return HavingPredicate[S]{core: rowCompare(a.data, querycore.NotEqual, v)}
}

// In compares groups after selection. Example: expression.In(value).
func (a OrderedValueComparison[S, V]) In(vs ...V) HavingPredicate[S] {
	return HavingPredicate[S]{core: rowCompare(a.data, querycore.In, vs...)}
}

// Lt compares groups after selection. Example: expression.Lt(value).
func (a OrderedValueComparison[S, V]) Lt(v V) HavingPredicate[S] {
	return HavingPredicate[S]{core: rowCompare(a.data, querycore.Less, v)}
}

// Lte compares groups after selection. Example: expression.Lte(value).
func (a OrderedValueComparison[S, V]) Lte(v V) HavingPredicate[S] {
	return HavingPredicate[S]{core: rowCompare(a.data, querycore.LessOrEqual, v)}
}

// Gt compares groups after selection. Example: expression.Gt(value).
func (a OrderedValueComparison[S, V]) Gt(v V) HavingPredicate[S] {
	return HavingPredicate[S]{core: rowCompare(a.data, querycore.Greater, v)}
}

// Gte compares groups after selection. Example: expression.Gte(value).
func (a OrderedValueComparison[S, V]) Gte(v V) HavingPredicate[S] {
	return HavingPredicate[S]{core: rowCompare(a.data, querycore.GreaterOrEqual, v)}
}

// IsNull tests a selected SQL value. Example: expression.IsNull().
func (a OrderedValueComparison[S, V]) IsNull() HavingPredicate[S] {
	return HavingPredicate[S]{core: rowCompare(a.data, querycore.IsNull)}
}

// IsNotNull tests a selected SQL value. Example: expression.IsNotNull().
func (a OrderedValueComparison[S, V]) IsNotNull() HavingPredicate[S] {
	return HavingPredicate[S]{core: rowCompare(a.data, querycore.IsNotNull)}
}

// Eq compares groups after selection. Example: expression.Eq(value).
func (a NullableOrderedValueComparison[S, V]) Eq(v V) HavingPredicate[S] {
	return HavingPredicate[S]{core: rowCompare(a.data, querycore.Equal, presentValues([]V{v})...)}
}

// Ne compares groups after selection. Example: expression.Ne(value).
func (a NullableOrderedValueComparison[S, V]) Ne(v V) HavingPredicate[S] {
	return HavingPredicate[S]{core: rowCompare(a.data, querycore.NotEqual, presentValues([]V{v})...)}
}

// In compares groups after selection. Example: expression.In(value).
func (a NullableOrderedValueComparison[S, V]) In(vs ...V) HavingPredicate[S] {
	return HavingPredicate[S]{core: rowCompare(a.data, querycore.In, presentValues(vs)...)}
}

// Lt compares groups after selection. Example: expression.Lt(value).
func (a NullableOrderedValueComparison[S, V]) Lt(v V) HavingPredicate[S] {
	return HavingPredicate[S]{core: rowCompare(a.data, querycore.Less, presentValues([]V{v})...)}
}

// Lte compares groups after selection. Example: expression.Lte(value).
func (a NullableOrderedValueComparison[S, V]) Lte(v V) HavingPredicate[S] {
	return HavingPredicate[S]{core: rowCompare(a.data, querycore.LessOrEqual, presentValues([]V{v})...)}
}

// Gt compares groups after selection. Example: expression.Gt(value).
func (a NullableOrderedValueComparison[S, V]) Gt(v V) HavingPredicate[S] {
	return HavingPredicate[S]{core: rowCompare(a.data, querycore.Greater, presentValues([]V{v})...)}
}

// Gte compares groups after selection. Example: expression.Gte(value).
func (a NullableOrderedValueComparison[S, V]) Gte(v V) HavingPredicate[S] {
	return HavingPredicate[S]{core: rowCompare(a.data, querycore.GreaterOrEqual, presentValues([]V{v})...)}
}

// IsNull tests a selected SQL value. Example: expression.IsNull().
func (a NullableOrderedValueComparison[S, V]) IsNull() HavingPredicate[S] {
	return HavingPredicate[S]{core: rowCompare(a.data, querycore.IsNull)}
}

// IsNotNull tests a selected SQL value. Example: expression.IsNotNull().
func (a NullableOrderedValueComparison[S, V]) IsNotNull() HavingPredicate[S] {
	return HavingPredicate[S]{core: rowCompare(a.data, querycore.IsNotNull)}
}
