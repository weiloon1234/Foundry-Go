package expr

import (
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/value"
)

// TextRowExpression is a row text calculation with typed comparisons. Example: Lower(name).Like("a%").
type TextRowExpression[S any, V ~string] struct {
	rowPhase [0]struct{}
	_        [0]*S
	data     rowData[V]
}

// NullableTextRowExpression preserves NULL and compares present text values. Example: LowerNullable(name).Like("a%").
type NullableTextRowExpression[S any, V ~string] struct {
	rowPhase [0]struct{}
	_        [0]*S
	data     rowData[value.Nullable[V]]
}

func textRow[S any](v Selected[S, string]) TextRowExpression[S, string] {
	return TextRowExpression[S, string]{data: v.data}
}
func nullableTextRow[S any](v Selected[S, value.Nullable[string]]) NullableTextRowExpression[S, string] {
	return NullableTextRowExpression[S, string]{data: v.data}
}
func (e TextRowExpression[S, V]) rowValue(*S) rowData[V] { return e.data }

// Value selects the computed value. Example: expression.Value().
func (e TextRowExpression[S, V]) Value() Selected[S, V] { return Selected[S, V]{data: e.data} }

// Param captures the exact result type. Example: expression.Param(value).
func (e TextRowExpression[S, V]) Param(v V) RowExpression[S, V] {
	return parameter[S](e.data, v)
}

// Eq compares row values before grouping. Example: expression.Eq(value).
func (e TextRowExpression[S, V]) Eq(v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(e.data, querycore.Equal, v)}
}

// Ne compares row values before grouping. Example: expression.Ne(value).
func (e TextRowExpression[S, V]) Ne(v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(e.data, querycore.NotEqual, v)}
}

// In compares row values before grouping. Example: expression.In(value).
func (e TextRowExpression[S, V]) In(vs ...V) Predicate[S] {
	return Predicate[S]{core: rowCompare(e.data, querycore.In, vs...)}
}

// Lt compares row values before grouping. Example: expression.Lt(value).
func (e TextRowExpression[S, V]) Lt(v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(e.data, querycore.Less, v)}
}

// Lte compares row values before grouping. Example: expression.Lte(value).
func (e TextRowExpression[S, V]) Lte(v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(e.data, querycore.LessOrEqual, v)}
}

// Gt compares row values before grouping. Example: expression.Gt(value).
func (e TextRowExpression[S, V]) Gt(v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(e.data, querycore.Greater, v)}
}

// Gte compares row values before grouping. Example: expression.Gte(value).
func (e TextRowExpression[S, V]) Gte(v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(e.data, querycore.GreaterOrEqual, v)}
}

// IsNull tests SQL nullness. Example: expression.IsNull().
func (e TextRowExpression[S, V]) IsNull() Predicate[S] {
	return Predicate[S]{core: rowCompare(e.data, querycore.IsNull)}
}

// IsNotNull tests SQL nullness. Example: expression.IsNotNull().
func (e TextRowExpression[S, V]) IsNotNull() Predicate[S] {
	return Predicate[S]{core: rowCompare(e.data, querycore.IsNotNull)}
}

// Asc orders a computed row value. Example: expression.Asc().
func (e TextRowExpression[S, V]) Asc() Order[S] {
	return Order[S]{core: ormcore.OrderExpression(e.data.core, false)}
}

// Desc orders a computed row value. Example: expression.Desc().
func (e TextRowExpression[S, V]) Desc() Order[S] {
	return Order[S]{core: ormcore.OrderExpression(e.data.core, true)}
}
func (e NullableTextRowExpression[S, V]) rowValue(*S) rowData[value.Nullable[V]] { return e.data }

// Value selects the computed value. Example: expression.Value().
func (e NullableTextRowExpression[S, V]) Value() Selected[S, value.Nullable[V]] {
	return Selected[S, value.Nullable[V]]{data: e.data}
}

// Param captures the exact result type. Example: expression.Param(value).
func (e NullableTextRowExpression[S, V]) Param(v value.Nullable[V]) RowExpression[S, value.Nullable[V]] {
	return parameter[S](e.data, v)
}

// Eq compares row values before grouping. Example: expression.Eq(value).
func (e NullableTextRowExpression[S, V]) Eq(v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(e.data, querycore.Equal, presentValues([]V{v})...)}
}

// Ne compares row values before grouping. Example: expression.Ne(value).
func (e NullableTextRowExpression[S, V]) Ne(v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(e.data, querycore.NotEqual, presentValues([]V{v})...)}
}

// In compares row values before grouping. Example: expression.In(value).
func (e NullableTextRowExpression[S, V]) In(vs ...V) Predicate[S] {
	return Predicate[S]{core: rowCompare(e.data, querycore.In, presentValues(vs)...)}
}

// Lt compares row values before grouping. Example: expression.Lt(value).
func (e NullableTextRowExpression[S, V]) Lt(v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(e.data, querycore.Less, presentValues([]V{v})...)}
}

// Lte compares row values before grouping. Example: expression.Lte(value).
func (e NullableTextRowExpression[S, V]) Lte(v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(e.data, querycore.LessOrEqual, presentValues([]V{v})...)}
}

// Gt compares row values before grouping. Example: expression.Gt(value).
func (e NullableTextRowExpression[S, V]) Gt(v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(e.data, querycore.Greater, presentValues([]V{v})...)}
}

// Gte compares row values before grouping. Example: expression.Gte(value).
func (e NullableTextRowExpression[S, V]) Gte(v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(e.data, querycore.GreaterOrEqual, presentValues([]V{v})...)}
}

// IsNull tests SQL nullness. Example: expression.IsNull().
func (e NullableTextRowExpression[S, V]) IsNull() Predicate[S] {
	return Predicate[S]{core: rowCompare(e.data, querycore.IsNull)}
}

// IsNotNull tests SQL nullness. Example: expression.IsNotNull().
func (e NullableTextRowExpression[S, V]) IsNotNull() Predicate[S] {
	return Predicate[S]{core: rowCompare(e.data, querycore.IsNotNull)}
}

// Asc orders a computed row value. Example: expression.Asc().
func (e NullableTextRowExpression[S, V]) Asc() Order[S] {
	return Order[S]{core: ormcore.OrderExpression(e.data.core, false)}
}

// Desc orders a computed row value. Example: expression.Desc().
func (e NullableTextRowExpression[S, V]) Desc() Order[S] {
	return Order[S]{core: ormcore.OrderExpression(e.data.core, true)}
}

// Like compares typed text using the database collation. Example: expression.Like("a").
func (e TextRowExpression[S, V]) Like(v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(e.data, querycore.Like, v)}
}

// Contains compares typed text using the database collation. Example: expression.Contains("a").
func (e TextRowExpression[S, V]) Contains(v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(e.data, querycore.Contains, v)}
}

// IContains compares typed text using the database collation. Example: expression.IContains("a").
func (e TextRowExpression[S, V]) IContains(v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(e.data, querycore.InsensitiveContains, v)}
}

// StartsWith compares typed text using the database collation. Example: expression.StartsWith("a").
func (e TextRowExpression[S, V]) StartsWith(v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(e.data, querycore.StartsWith, v)}
}

// EndsWith compares typed text using the database collation. Example: expression.EndsWith("a").
func (e TextRowExpression[S, V]) EndsWith(v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(e.data, querycore.EndsWith, v)}
}

// IStartsWith compares typed text using the database collation. Example: expression.IStartsWith("a").
func (e TextRowExpression[S, V]) IStartsWith(v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(e.data, querycore.InsensitiveStartsWith, v)}
}

// IEndsWith compares typed text using the database collation. Example: expression.IEndsWith("a").
func (e TextRowExpression[S, V]) IEndsWith(v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(e.data, querycore.InsensitiveEndsWith, v)}
}

// ILike compares typed text using the database collation. Example: expression.ILike("a").
func (e TextRowExpression[S, V]) ILike(v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(e.data, querycore.InsensitiveLike, v)}
}

// Like compares typed text using the database collation. Example: expression.Like("a").
func (e NullableTextRowExpression[S, V]) Like(v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(e.data, querycore.Like, presentValues([]V{v})...)}
}

// Contains compares typed text using the database collation. Example: expression.Contains("a").
func (e NullableTextRowExpression[S, V]) Contains(v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(e.data, querycore.Contains, presentValues([]V{v})...)}
}

// IContains compares typed text using the database collation. Example: expression.IContains("a").
func (e NullableTextRowExpression[S, V]) IContains(v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(e.data, querycore.InsensitiveContains, presentValues([]V{v})...)}
}

// StartsWith compares typed text using the database collation. Example: expression.StartsWith("a").
func (e NullableTextRowExpression[S, V]) StartsWith(v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(e.data, querycore.StartsWith, presentValues([]V{v})...)}
}

// EndsWith compares typed text using the database collation. Example: expression.EndsWith("a").
func (e NullableTextRowExpression[S, V]) EndsWith(v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(e.data, querycore.EndsWith, presentValues([]V{v})...)}
}

// IStartsWith compares typed text using the database collation. Example: expression.IStartsWith("a").
func (e NullableTextRowExpression[S, V]) IStartsWith(v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(e.data, querycore.InsensitiveStartsWith, presentValues([]V{v})...)}
}

// IEndsWith compares typed text using the database collation. Example: expression.IEndsWith("a").
func (e NullableTextRowExpression[S, V]) IEndsWith(v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(e.data, querycore.InsensitiveEndsWith, presentValues([]V{v})...)}
}

// ILike compares typed text using the database collation. Example: expression.ILike("a").
func (e NullableTextRowExpression[S, V]) ILike(v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(e.data, querycore.InsensitiveLike, presentValues([]V{v})...)}
}
