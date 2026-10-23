package expr

import (
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/value"
)

// OrderedRowExpression is an ordered computed row value. Example: expr.Add(a,b).Gt(2).
type OrderedRowExpression[S any, V orderedScalar] struct {
	rowPhase [0]struct{}
	_        [0]*S
	data     rowData[V]
}

// NullableOrderedRowExpression retains nullable results and present-value comparisons. Example: expr.AddNullable(a,b).Gt(2).
type NullableOrderedRowExpression[S any, V orderedScalar] struct {
	rowPhase [0]struct{}
	_        [0]*S
	data     rowData[value.Nullable[V]]
}

func orderedRow[S any, V orderedScalar](v Selected[S, V]) OrderedRowExpression[S, V] {
	return OrderedRowExpression[S, V]{data: v.data}
}
func nullableOrderedRow[S any, V orderedScalar](v Selected[S, value.Nullable[V]]) NullableOrderedRowExpression[S, V] {
	return NullableOrderedRowExpression[S, V]{data: v.data}
}

// OrderRow exposes ordered comparisons on a row value. Example: expr.OrderRow(expr.Value(Users.Age)).Gt(18).
func OrderRow[S any, V orderedScalar](v RowValue[S, V]) OrderedRowExpression[S, V] {
	return orderedRow(Select(v))
}

// OrderNullableRow exposes present-value comparisons. Example: expr.OrderNullableRow(expr.Value(Users.Birthday)).IsNull().
func OrderNullableRow[S any, V orderedScalar](v RowValue[S, value.Nullable[V]]) NullableOrderedRowExpression[S, V] {
	return nullableOrderedRow(Select(v))
}

// Eq compares row values before grouping. Example: expression.Eq(value).
func (e RowExpression[S, V]) Eq(v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(e.data, querycore.Equal, v)}
}

// Ne compares row values before grouping. Example: expression.Ne(value).
func (e RowExpression[S, V]) Ne(v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(e.data, querycore.NotEqual, v)}
}

// In compares row values before grouping. Example: expression.In(value).
func (e RowExpression[S, V]) In(vs ...V) Predicate[S] {
	return Predicate[S]{core: rowCompare(e.data, querycore.In, vs...)}
}

// IsNull tests SQL nullness. Example: expression.IsNull().
func (e RowExpression[S, V]) IsNull() Predicate[S] {
	return Predicate[S]{core: rowCompare(e.data, querycore.IsNull)}
}

// IsNotNull tests SQL nullness. Example: expression.IsNotNull().
func (e RowExpression[S, V]) IsNotNull() Predicate[S] {
	return Predicate[S]{core: rowCompare(e.data, querycore.IsNotNull)}
}

// Asc orders a computed row value. Example: expression.Asc().
func (e RowExpression[S, V]) Asc() Order[S] {
	return Order[S]{core: ormcore.OrderExpression(e.data.core, false)}
}

// Desc orders a computed row value. Example: expression.Desc().
func (e RowExpression[S, V]) Desc() Order[S] {
	return Order[S]{core: ormcore.OrderExpression(e.data.core, true)}
}
func (e OrderedRowExpression[S, V]) rowValue(*S) rowData[V] { return e.data }

// Value selects the computed value. Example: expression.Value().
func (e OrderedRowExpression[S, V]) Value() Selected[S, V] { return Selected[S, V]{data: e.data} }

// Param captures the exact result type. Example: expression.Param(value).
func (e OrderedRowExpression[S, V]) Param(v V) RowExpression[S, V] {
	return parameter[S](e.data, v)
}

// Eq compares row values before grouping. Example: expression.Eq(value).
func (e OrderedRowExpression[S, V]) Eq(v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(e.data, querycore.Equal, v)}
}

// Ne compares row values before grouping. Example: expression.Ne(value).
func (e OrderedRowExpression[S, V]) Ne(v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(e.data, querycore.NotEqual, v)}
}

// In compares row values before grouping. Example: expression.In(value).
func (e OrderedRowExpression[S, V]) In(vs ...V) Predicate[S] {
	return Predicate[S]{core: rowCompare(e.data, querycore.In, vs...)}
}

// Lt compares row values before grouping. Example: expression.Lt(value).
func (e OrderedRowExpression[S, V]) Lt(v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(e.data, querycore.Less, v)}
}

// Lte compares row values before grouping. Example: expression.Lte(value).
func (e OrderedRowExpression[S, V]) Lte(v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(e.data, querycore.LessOrEqual, v)}
}

// Gt compares row values before grouping. Example: expression.Gt(value).
func (e OrderedRowExpression[S, V]) Gt(v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(e.data, querycore.Greater, v)}
}

// Gte compares row values before grouping. Example: expression.Gte(value).
func (e OrderedRowExpression[S, V]) Gte(v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(e.data, querycore.GreaterOrEqual, v)}
}

// IsNull tests SQL nullness. Example: expression.IsNull().
func (e OrderedRowExpression[S, V]) IsNull() Predicate[S] {
	return Predicate[S]{core: rowCompare(e.data, querycore.IsNull)}
}

// IsNotNull tests SQL nullness. Example: expression.IsNotNull().
func (e OrderedRowExpression[S, V]) IsNotNull() Predicate[S] {
	return Predicate[S]{core: rowCompare(e.data, querycore.IsNotNull)}
}

// Asc orders a computed row value. Example: expression.Asc().
func (e OrderedRowExpression[S, V]) Asc() Order[S] {
	return Order[S]{core: ormcore.OrderExpression(e.data.core, false)}
}

// Desc orders a computed row value. Example: expression.Desc().
func (e OrderedRowExpression[S, V]) Desc() Order[S] {
	return Order[S]{core: ormcore.OrderExpression(e.data.core, true)}
}
func (e NullableOrderedRowExpression[S, V]) rowValue(*S) rowData[value.Nullable[V]] { return e.data }

// Value selects the computed value. Example: expression.Value().
func (e NullableOrderedRowExpression[S, V]) Value() Selected[S, value.Nullable[V]] {
	return Selected[S, value.Nullable[V]]{data: e.data}
}

// Param captures the exact result type. Example: expression.Param(value).
func (e NullableOrderedRowExpression[S, V]) Param(v value.Nullable[V]) RowExpression[S, value.Nullable[V]] {
	return parameter[S](e.data, v)
}

// Eq compares row values before grouping. Example: expression.Eq(value).
func (e NullableOrderedRowExpression[S, V]) Eq(v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(e.data, querycore.Equal, presentValues([]V{v})...)}
}

// Ne compares row values before grouping. Example: expression.Ne(value).
func (e NullableOrderedRowExpression[S, V]) Ne(v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(e.data, querycore.NotEqual, presentValues([]V{v})...)}
}

// In compares row values before grouping. Example: expression.In(value).
func (e NullableOrderedRowExpression[S, V]) In(vs ...V) Predicate[S] {
	return Predicate[S]{core: rowCompare(e.data, querycore.In, presentValues(vs)...)}
}

// Lt compares row values before grouping. Example: expression.Lt(value).
func (e NullableOrderedRowExpression[S, V]) Lt(v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(e.data, querycore.Less, presentValues([]V{v})...)}
}

// Lte compares row values before grouping. Example: expression.Lte(value).
func (e NullableOrderedRowExpression[S, V]) Lte(v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(e.data, querycore.LessOrEqual, presentValues([]V{v})...)}
}

// Gt compares row values before grouping. Example: expression.Gt(value).
func (e NullableOrderedRowExpression[S, V]) Gt(v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(e.data, querycore.Greater, presentValues([]V{v})...)}
}

// Gte compares row values before grouping. Example: expression.Gte(value).
func (e NullableOrderedRowExpression[S, V]) Gte(v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(e.data, querycore.GreaterOrEqual, presentValues([]V{v})...)}
}

// IsNull tests SQL nullness. Example: expression.IsNull().
func (e NullableOrderedRowExpression[S, V]) IsNull() Predicate[S] {
	return Predicate[S]{core: rowCompare(e.data, querycore.IsNull)}
}

// IsNotNull tests SQL nullness. Example: expression.IsNotNull().
func (e NullableOrderedRowExpression[S, V]) IsNotNull() Predicate[S] {
	return Predicate[S]{core: rowCompare(e.data, querycore.IsNotNull)}
}

// Asc orders a computed row value. Example: expression.Asc().
func (e NullableOrderedRowExpression[S, V]) Asc() Order[S] {
	return Order[S]{core: ormcore.OrderExpression(e.data.core, false)}
}

// Desc orders a computed row value. Example: expression.Desc().
func (e NullableOrderedRowExpression[S, V]) Desc() Order[S] {
	return Order[S]{core: ormcore.OrderExpression(e.data.core, true)}
}
