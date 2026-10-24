package expr

import (
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/value"
)

// TextValueComparison supplies selected text conditions for HAVING. Example: TextValue(LowerValue(name)).Like("a%").
type TextValueComparison[S any, V ~string] struct {
	selectedPhase [0]struct{}
	_             [0]*S
	data          rowData[V]
}

// NullableTextValueComparison retains NULL and compares present text in HAVING.
type NullableTextValueComparison[S any, V ~string] struct {
	selectedPhase [0]struct{}
	_             [0]*S
	data          rowData[value.Nullable[V]]
}

// TextValue exposes selected text comparisons. Example: TextValue(LowerValue(name)).Contains("x").
func TextValue[S any, V ~string](v Selected[S, V]) TextValueComparison[S, V] {
	return TextValueComparison[S, V]{data: v.data}
}

// TextNullableValue exposes nullable selected text comparisons. Example: TextNullableValue(Min(name).Value()).IsNull().
func TextNullableValue[S any, V ~string](v Selected[S, value.Nullable[V]]) NullableTextValueComparison[S, V] {
	return NullableTextValueComparison[S, V]{data: v.data}
}

// Eq compares selected text without changing its phase. Example: comparison.Eq(value).
func (a TextValueComparison[S, V]) Eq(v V) HavingPredicate[S] {
	return HavingPredicate[S]{core: rowCompare(a.data, querycore.Equal, v)}
}

// Ne compares selected text without changing its phase. Example: comparison.Ne(value).
func (a TextValueComparison[S, V]) Ne(v V) HavingPredicate[S] {
	return HavingPredicate[S]{core: rowCompare(a.data, querycore.NotEqual, v)}
}

// In compares selected text without changing its phase. Example: comparison.In(value).
func (a TextValueComparison[S, V]) In(vs ...V) HavingPredicate[S] {
	return HavingPredicate[S]{core: rowCompare(a.data, querycore.In, vs...)}
}

// Lt compares selected text without changing its phase. Example: comparison.Lt(value).
func (a TextValueComparison[S, V]) Lt(v V) HavingPredicate[S] {
	return HavingPredicate[S]{core: rowCompare(a.data, querycore.Less, v)}
}

// Lte compares selected text without changing its phase. Example: comparison.Lte(value).
func (a TextValueComparison[S, V]) Lte(v V) HavingPredicate[S] {
	return HavingPredicate[S]{core: rowCompare(a.data, querycore.LessOrEqual, v)}
}

// Gt compares selected text without changing its phase. Example: comparison.Gt(value).
func (a TextValueComparison[S, V]) Gt(v V) HavingPredicate[S] {
	return HavingPredicate[S]{core: rowCompare(a.data, querycore.Greater, v)}
}

// Gte compares selected text without changing its phase. Example: comparison.Gte(value).
func (a TextValueComparison[S, V]) Gte(v V) HavingPredicate[S] {
	return HavingPredicate[S]{core: rowCompare(a.data, querycore.GreaterOrEqual, v)}
}

// IsNull compares selected text without changing its phase. Example: comparison.IsNull(value).
func (a TextValueComparison[S, V]) IsNull() HavingPredicate[S] {
	return HavingPredicate[S]{core: rowCompare(a.data, querycore.IsNull)}
}

// IsNotNull compares selected text without changing its phase. Example: comparison.IsNotNull(value).
func (a TextValueComparison[S, V]) IsNotNull() HavingPredicate[S] {
	return HavingPredicate[S]{core: rowCompare(a.data, querycore.IsNotNull)}
}

// Eq compares selected text without changing its phase. Example: comparison.Eq(value).
func (a NullableTextValueComparison[S, V]) Eq(v V) HavingPredicate[S] {
	return HavingPredicate[S]{core: rowCompare(a.data, querycore.Equal, presentValues([]V{v})...)}
}

// Ne compares selected text without changing its phase. Example: comparison.Ne(value).
func (a NullableTextValueComparison[S, V]) Ne(v V) HavingPredicate[S] {
	return HavingPredicate[S]{core: rowCompare(a.data, querycore.NotEqual, presentValues([]V{v})...)}
}

// In compares selected text without changing its phase. Example: comparison.In(value).
func (a NullableTextValueComparison[S, V]) In(vs ...V) HavingPredicate[S] {
	return HavingPredicate[S]{core: rowCompare(a.data, querycore.In, presentValues(vs)...)}
}

// Lt compares selected text without changing its phase. Example: comparison.Lt(value).
func (a NullableTextValueComparison[S, V]) Lt(v V) HavingPredicate[S] {
	return HavingPredicate[S]{core: rowCompare(a.data, querycore.Less, presentValues([]V{v})...)}
}

// Lte compares selected text without changing its phase. Example: comparison.Lte(value).
func (a NullableTextValueComparison[S, V]) Lte(v V) HavingPredicate[S] {
	return HavingPredicate[S]{core: rowCompare(a.data, querycore.LessOrEqual, presentValues([]V{v})...)}
}

// Gt compares selected text without changing its phase. Example: comparison.Gt(value).
func (a NullableTextValueComparison[S, V]) Gt(v V) HavingPredicate[S] {
	return HavingPredicate[S]{core: rowCompare(a.data, querycore.Greater, presentValues([]V{v})...)}
}

// Gte compares selected text without changing its phase. Example: comparison.Gte(value).
func (a NullableTextValueComparison[S, V]) Gte(v V) HavingPredicate[S] {
	return HavingPredicate[S]{core: rowCompare(a.data, querycore.GreaterOrEqual, presentValues([]V{v})...)}
}

// IsNull compares selected text without changing its phase. Example: comparison.IsNull(value).
func (a NullableTextValueComparison[S, V]) IsNull() HavingPredicate[S] {
	return HavingPredicate[S]{core: rowCompare(a.data, querycore.IsNull)}
}

// IsNotNull compares selected text without changing its phase. Example: comparison.IsNotNull(value).
func (a NullableTextValueComparison[S, V]) IsNotNull() HavingPredicate[S] {
	return HavingPredicate[S]{core: rowCompare(a.data, querycore.IsNotNull)}
}

// Like compares selected text using the database collation. Example: comparison.Like("a").
func (e TextValueComparison[S, V]) Like(v V) HavingPredicate[S] {
	return HavingPredicate[S]{core: rowCompare(e.data, querycore.Like, v)}
}

// Contains compares selected text using the database collation. Example: comparison.Contains("a").
func (e TextValueComparison[S, V]) Contains(v V) HavingPredicate[S] {
	return HavingPredicate[S]{core: rowCompare(e.data, querycore.Contains, v)}
}

// IContains compares selected text using the database collation. Example: comparison.IContains("a").
func (e TextValueComparison[S, V]) IContains(v V) HavingPredicate[S] {
	return HavingPredicate[S]{core: rowCompare(e.data, querycore.InsensitiveContains, v)}
}

// StartsWith compares selected text using the database collation. Example: comparison.StartsWith("a").
func (e TextValueComparison[S, V]) StartsWith(v V) HavingPredicate[S] {
	return HavingPredicate[S]{core: rowCompare(e.data, querycore.StartsWith, v)}
}

// EndsWith compares selected text using the database collation. Example: comparison.EndsWith("a").
func (e TextValueComparison[S, V]) EndsWith(v V) HavingPredicate[S] {
	return HavingPredicate[S]{core: rowCompare(e.data, querycore.EndsWith, v)}
}

// IStartsWith compares selected text using the database collation. Example: comparison.IStartsWith("a").
func (e TextValueComparison[S, V]) IStartsWith(v V) HavingPredicate[S] {
	return HavingPredicate[S]{core: rowCompare(e.data, querycore.InsensitiveStartsWith, v)}
}

// IEndsWith compares selected text using the database collation. Example: comparison.IEndsWith("a").
func (e TextValueComparison[S, V]) IEndsWith(v V) HavingPredicate[S] {
	return HavingPredicate[S]{core: rowCompare(e.data, querycore.InsensitiveEndsWith, v)}
}

// ILike compares selected text using the database collation. Example: comparison.ILike("a").
func (e TextValueComparison[S, V]) ILike(v V) HavingPredicate[S] {
	return HavingPredicate[S]{core: rowCompare(e.data, querycore.InsensitiveLike, v)}
}

// Like compares selected text using the database collation. Example: comparison.Like("a").
func (e NullableTextValueComparison[S, V]) Like(v V) HavingPredicate[S] {
	return HavingPredicate[S]{core: rowCompare(e.data, querycore.Like, presentValues([]V{v})...)}
}

// Contains compares selected text using the database collation. Example: comparison.Contains("a").
func (e NullableTextValueComparison[S, V]) Contains(v V) HavingPredicate[S] {
	return HavingPredicate[S]{core: rowCompare(e.data, querycore.Contains, presentValues([]V{v})...)}
}

// IContains compares selected text using the database collation. Example: comparison.IContains("a").
func (e NullableTextValueComparison[S, V]) IContains(v V) HavingPredicate[S] {
	return HavingPredicate[S]{core: rowCompare(e.data, querycore.InsensitiveContains, presentValues([]V{v})...)}
}

// StartsWith compares selected text using the database collation. Example: comparison.StartsWith("a").
func (e NullableTextValueComparison[S, V]) StartsWith(v V) HavingPredicate[S] {
	return HavingPredicate[S]{core: rowCompare(e.data, querycore.StartsWith, presentValues([]V{v})...)}
}

// EndsWith compares selected text using the database collation. Example: comparison.EndsWith("a").
func (e NullableTextValueComparison[S, V]) EndsWith(v V) HavingPredicate[S] {
	return HavingPredicate[S]{core: rowCompare(e.data, querycore.EndsWith, presentValues([]V{v})...)}
}

// IStartsWith compares selected text using the database collation. Example: comparison.IStartsWith("a").
func (e NullableTextValueComparison[S, V]) IStartsWith(v V) HavingPredicate[S] {
	return HavingPredicate[S]{core: rowCompare(e.data, querycore.InsensitiveStartsWith, presentValues([]V{v})...)}
}

// IEndsWith compares selected text using the database collation. Example: comparison.IEndsWith("a").
func (e NullableTextValueComparison[S, V]) IEndsWith(v V) HavingPredicate[S] {
	return HavingPredicate[S]{core: rowCompare(e.data, querycore.InsensitiveEndsWith, presentValues([]V{v})...)}
}

// ILike compares selected text using the database collation. Example: comparison.ILike("a").
func (e NullableTextValueComparison[S, V]) ILike(v V) HavingPredicate[S] {
	return HavingPredicate[S]{core: rowCompare(e.data, querycore.InsensitiveLike, presentValues([]V{v})...)}
}
