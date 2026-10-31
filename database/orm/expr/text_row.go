package expr

import (
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/value"
)

// TextRowExpression is a row text calculation with typed comparisons.
//
// Example: expr.TextLike(expr.Lower(name), "a%")
type TextRowExpression[S any, V ~string] = textRowExpression[*S, V]

type textRowExpression[S any, V ~string] struct {
	rowPhase [0]struct{}
	_        [0]S
	data     rowData[V]
}

// NullableTextRowExpression preserves NULL and compares present text values.
//
// Example: expr.NullableTextLike(expr.LowerNullable(name), "a%")
type NullableTextRowExpression[S any, V ~string] = nullableTextRowExpression[*S, V]

type nullableTextRowExpression[S any, V ~string] struct {
	rowPhase [0]struct{}
	_        [0]S
	data     rowData[value.Nullable[V]]
}

func textRow[S any](v Selected[S, string]) TextRowExpression[S, string] {
	return TextRowExpression[S, string]{data: v.data}
}
func nullableTextRow[S any](v Selected[S, value.Nullable[string]]) NullableTextRowExpression[S, string] {
	return NullableTextRowExpression[S, string]{data: v.data}
}
func (e textRowExpression[S, V]) rowValue(S) rowData[V] { return e.data }
func (e textRowExpression[S, V]) orderedRowValue(S)     {}

// TextLike compares typed text using the database collation.
//
// Example: expr.TextLike(e, v)
func TextLike[S any, V ~string](e TextRowExpression[S, V], v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(e.data, querycore.Like, v)}
}

// TextContains compares typed text using the database collation.
//
// Example: expr.TextContains(e, v)
func TextContains[S any, V ~string](e TextRowExpression[S, V], v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(e.data, querycore.Contains, v)}
}

// TextIContains compares typed text using the database collation.
//
// Example: expr.TextIContains(e, v)
func TextIContains[S any, V ~string](e TextRowExpression[S, V], v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(e.data, querycore.InsensitiveContains, v)}
}

// TextStartsWith compares typed text using the database collation.
//
// Example: expr.TextStartsWith(e, v)
func TextStartsWith[S any, V ~string](e TextRowExpression[S, V], v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(e.data, querycore.StartsWith, v)}
}

// TextEndsWith compares typed text using the database collation.
//
// Example: expr.TextEndsWith(e, v)
func TextEndsWith[S any, V ~string](e TextRowExpression[S, V], v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(e.data, querycore.EndsWith, v)}
}

// TextIStartsWith compares typed text using the database collation.
//
// Example: expr.TextIStartsWith(e, v)
func TextIStartsWith[S any, V ~string](e TextRowExpression[S, V], v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(e.data, querycore.InsensitiveStartsWith, v)}
}

// TextIEndsWith compares typed text using the database collation.
//
// Example: expr.TextIEndsWith(e, v)
func TextIEndsWith[S any, V ~string](e TextRowExpression[S, V], v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(e.data, querycore.InsensitiveEndsWith, v)}
}

// TextILike compares typed text using the database collation.
//
// Example: expr.TextILike(e, v)
func TextILike[S any, V ~string](e TextRowExpression[S, V], v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(e.data, querycore.InsensitiveLike, v)}
}

// NullableTextLike compares nullable text using the database collation.
//
// Example: expr.NullableTextLike(e, v)
func NullableTextLike[S any, V ~string](e NullableTextRowExpression[S, V], v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(e.data, querycore.Like, presentValues([]V{v})...)}
}

// NullableTextContains compares nullable text using the database collation.
//
// Example: expr.NullableTextContains(e, v)
func NullableTextContains[S any, V ~string](e NullableTextRowExpression[S, V], v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(e.data, querycore.Contains, presentValues([]V{v})...)}
}

// NullableTextIContains compares nullable text using the database collation.
//
// Example: expr.NullableTextIContains(e, v)
func NullableTextIContains[S any, V ~string](e NullableTextRowExpression[S, V], v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(e.data, querycore.InsensitiveContains, presentValues([]V{v})...)}
}

// NullableTextStartsWith compares nullable text using the database collation.
//
// Example: expr.NullableTextStartsWith(e, v)
func NullableTextStartsWith[S any, V ~string](e NullableTextRowExpression[S, V], v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(e.data, querycore.StartsWith, presentValues([]V{v})...)}
}

// NullableTextEndsWith compares nullable text using the database collation.
//
// Example: expr.NullableTextEndsWith(e, v)
func NullableTextEndsWith[S any, V ~string](e NullableTextRowExpression[S, V], v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(e.data, querycore.EndsWith, presentValues([]V{v})...)}
}

// NullableTextIStartsWith compares nullable text using the database collation.
//
// Example: expr.NullableTextIStartsWith(e, v)
func NullableTextIStartsWith[S any, V ~string](e NullableTextRowExpression[S, V], v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(e.data, querycore.InsensitiveStartsWith, presentValues([]V{v})...)}
}

// NullableTextIEndsWith compares nullable text using the database collation.
//
// Example: expr.NullableTextIEndsWith(e, v)
func NullableTextIEndsWith[S any, V ~string](e NullableTextRowExpression[S, V], v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(e.data, querycore.InsensitiveEndsWith, presentValues([]V{v})...)}
}

// NullableTextILike compares nullable text using the database collation.
//
// Example: expr.NullableTextILike(e, v)
func NullableTextILike[S any, V ~string](e NullableTextRowExpression[S, V], v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(e.data, querycore.InsensitiveLike, presentValues([]V{v})...)}
}

func (e nullableTextRowExpression[S, V]) rowValue(S) rowData[value.Nullable[V]] { return e.data }
func (e nullableTextRowExpression[S, V]) orderedRowValue(S)                     {}
