package expr

import (
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/value"
)

// OrderedRowValue is a sealed row value with ordered comparison capability.
// Use OrderRow or OrderNullableRow to opt a general row value into this capability.
//
// Example: var operand expr.OrderedRowValue[User, int64] = expr.OrderRow(age)
type OrderedRowValue[S, V any] interface {
	RowValue[S, V]
	orderedRowValue(*S)
}

// OrderedRowExpression is an ordered computed row value.
//
// Example: expr.Gt(expr.Add(a, b), 2)
type OrderedRowExpression[S any, V orderedScalar] = orderedRowExpression[*S, V]

type orderedRowExpression[S any, V orderedScalar] struct {
	rowPhase [0]struct{}
	_        [0]S
	data     rowData[V]
}

// NullableOrderedRowExpression retains nullable results and present-value comparisons.
//
// Example: expr.NullableGt(expr.AddNullable(a, b), 2)
type NullableOrderedRowExpression[S any, V orderedScalar] = nullableOrderedRowExpression[*S, V]

type nullableOrderedRowExpression[S any, V orderedScalar] struct {
	rowPhase [0]struct{}
	_        [0]S
	data     rowData[value.Nullable[V]]
}

func orderedRow[S any, V orderedScalar](v Selected[S, V]) OrderedRowExpression[S, V] {
	return OrderedRowExpression[S, V]{data: v.data}
}
func nullableOrderedRow[S any, V orderedScalar](v Selected[S, value.Nullable[V]]) NullableOrderedRowExpression[S, V] {
	return NullableOrderedRowExpression[S, V]{data: v.data}
}

// OrderRow exposes ordered comparisons on a row value.
//
// Example: expr.OrderRow(v)
func OrderRow[S any, V orderedScalar](v RowValue[S, V]) OrderedRowExpression[S, V] {
	return orderedRow(AsSelected(Row(v)))
}

// OrderNullableRow exposes present-value comparisons.
//
// Example: expr.OrderNullableRow(v)
func OrderNullableRow[S any, V orderedScalar](v RowValue[S, value.Nullable[V]]) NullableOrderedRowExpression[S, V] {
	return nullableOrderedRow(AsSelected(Row(v)))
}

// Lt compares row values before grouping.
//
// Example: expr.Lt(e, v)
func Lt[S any, V orderedScalar](e OrderedRowValue[S, V], v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(rowInput(e).data, querycore.Less, v)}
}

// Lte compares row values before grouping.
//
// Example: expr.Lte(e, v)
func Lte[S any, V orderedScalar](e OrderedRowValue[S, V], v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(rowInput(e).data, querycore.LessOrEqual, v)}
}

// Gt compares row values before grouping.
//
// Example: expr.Gt(e, v)
func Gt[S any, V orderedScalar](e OrderedRowValue[S, V], v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(rowInput(e).data, querycore.Greater, v)}
}

// Gte compares row values before grouping.
//
// Example: expr.Gte(e, v)
func Gte[S any, V orderedScalar](e OrderedRowValue[S, V], v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(rowInput(e).data, querycore.GreaterOrEqual, v)}
}

// NullableLt compares nullable row values with a present value.
//
// Example: expr.NullableLt(e, v)
func NullableLt[S any, V orderedScalar](e OrderedRowValue[S, value.Nullable[V]], v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(rowInput(e).data, querycore.Less, presentValues([]V{v})...)}
}

// NullableLte compares nullable row values with a present value.
//
// Example: expr.NullableLte(e, v)
func NullableLte[S any, V orderedScalar](e OrderedRowValue[S, value.Nullable[V]], v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(rowInput(e).data, querycore.LessOrEqual, presentValues([]V{v})...)}
}

// NullableGt compares nullable row values with a present value.
//
// Example: expr.NullableGt(e, v)
func NullableGt[S any, V orderedScalar](e OrderedRowValue[S, value.Nullable[V]], v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(rowInput(e).data, querycore.Greater, presentValues([]V{v})...)}
}

// NullableGte compares nullable row values with a present value.
//
// Example: expr.NullableGte(e, v)
func NullableGte[S any, V orderedScalar](e OrderedRowValue[S, value.Nullable[V]], v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(rowInput(e).data, querycore.GreaterOrEqual, presentValues([]V{v})...)}
}

func (e orderedRowExpression[S, V]) rowValue(S) rowData[V] { return e.data }
func (e orderedRowExpression[S, V]) orderedRowValue(S)     {}

func (e nullableOrderedRowExpression[S, V]) rowValue(S) rowData[value.Nullable[V]] { return e.data }
func (e nullableOrderedRowExpression[S, V]) orderedRowValue(S)                     {}
