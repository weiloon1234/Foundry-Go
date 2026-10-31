package expr

import (
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/value"
	"reflect"
)

// HavingPredicate is a group condition and cannot enter row WHERE.
//
// Example: expr.GtValue(expr.Count[User](), 0)
type HavingPredicate[S any] = havingPredicate[*S]

type havingPredicate[S any] struct {
	havingPhase [0]struct{}
	_           [0]S
	core        ormcore.Predicate
}

// Aggregate is an unordered selected aggregate.
//
// Example: expr.Exists[User]()
type Aggregate[S, V any] = aggregate[*S, V]

type aggregate[S, V any] struct {
	aggregatePhase [0]struct{}
	_              [0]S
	data           rowData[V]
}

// OrderedAggregate is an ordered selected aggregate.
//
// Example: expr.GtValue(expr.Count[User](), 1)
type OrderedAggregate[S any, V orderedScalar] = orderedAggregate[*S, V]

type orderedAggregate[S any, V orderedScalar] struct {
	aggregatePhase [0]struct{}
	_              [0]S
	data           rowData[V]
}

// Count counts rows including NULL fields; empty input returns zero.
//
// Example: expr.Count[User]()
func Count[S any]() OrderedAggregate[S, int64] {
	return OrderedAggregate[S, int64]{data: rowData[int64]{core: ormcore.Expression{Node: querycore.AggregateNode{Kind: querycore.CountAll}, Refs: []ormcore.Reference{{Scope: reflect.TypeFor[S]()}}, ReferenceFree: true}, column: columnCodec(codec.Signed[int64]())}}
}

// Exists reports whether any input row exists; empty input is false.
//
// Example: expr.Exists[User]()
func Exists[S any]() Aggregate[S, bool] {
	return Aggregate[S, bool]{data: rowData[bool]{core: ormcore.Expression{Node: querycore.AggregateNode{Kind: querycore.ExistsValues}, Refs: []ormcore.Reference{{Scope: reflect.TypeFor[S]()}}, ReferenceFree: true}, column: columnCodec(codec.Bool[bool]())}}
}

// ValueComparison compares selected values without exposing row predicates.
//
// Example: expr.EqValue(expr.CompareValue(selected), value)
type ValueComparison[S, V any] = valueComparison[*S, V]

type valueComparison[S, V any] struct {
	selectedPhase [0]struct{}
	_             [0]S
	data          rowData[V]
}

// OrderedValueComparison compares ordered selected values.
//
// Example: expr.GtValue(expr.OrderValue(selected), value)
type OrderedValueComparison[S any, V orderedScalar] = orderedValueComparison[*S, V]

type orderedValueComparison[S any, V orderedScalar] struct {
	selectedPhase [0]struct{}
	_             [0]S
	data          rowData[V]
}

// NullableOrderedValueComparison retains nullable results with present-value comparisons.
//
// Example: expr.GtValue(expr.OrderNullableValue(selected), value)
type NullableOrderedValueComparison[S any, V orderedScalar] = nullableOrderedValueComparison[*S, V]

type nullableOrderedValueComparison[S any, V orderedScalar] struct {
	selectedPhase [0]struct{}
	_             [0]S
	data          rowData[value.Nullable[V]]
}

// CompareValue exposes equality comparisons for a selected value.
//
// Example: expr.CompareValue(v)
func CompareValue[S, V any](v Selected[S, V]) ValueComparison[S, V] {
	return ValueComparison[S, V]{data: v.data}
}

// OrderValue exposes ordered comparisons for a selected value.
//
// Example: expr.OrderValue(v)
func OrderValue[S any, V orderedScalar](v Selected[S, V]) OrderedValueComparison[S, V] {
	return OrderedValueComparison[S, V]{data: v.data}
}

// OrderNullableValue compares nullable selected results with present values.
//
// Example: expr.OrderNullableValue(v)
func OrderNullableValue[S any, V orderedScalar](v Selected[S, value.Nullable[V]]) NullableOrderedValueComparison[S, V] {
	return NullableOrderedValueComparison[S, V]{data: v.data}
}
