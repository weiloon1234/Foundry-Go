package query

import (
	"time"

	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// These private lifts are used only by row APIs whose inputs already establish
// the row phase. Selected APIs never expose them to consumers.
func orderedRow[S any, V orderedScalar](v Expression[S, V]) OrderedRowExpression[S, V] {
	return OrderedRowExpression[S, V]{RowExpression[S, V]{v}}
}
func nullableOrderedRow[S any, V orderedScalar](v Expression[S, value.Nullable[V]]) NullableOrderedRowExpression[S, V] {
	return NullableOrderedRowExpression[S, V]{RowExpression[S, value.Nullable[V]]{v}}
}
func textRow[S any](v Expression[S, string]) TextRowExpression[S, string] {
	return TextRowExpression[S, string]{orderedRow(v)}
}
func nullableTextRow[S any](v Expression[S, value.Nullable[string]]) NullableTextRowExpression[S, string] {
	return NullableTextRowExpression[S, string]{nullableOrderedRow(v)}
}

// orderedScalar covers computed numbers, strings and supported temporal values.
type orderedScalar interface {
	numericValue | ~string | time.Time | temporal.Date | temporal.Time | temporal.DateTime | temporal.LocalDateTime | temporal.Interval
}

// OrderedRowExpression adds ordered literal comparisons to a concrete row value.
type OrderedRowExpression[S any, V orderedScalar] struct{ RowExpression[S, V] }

// OrderRow exposes ordering comparisons on an existing computed row value.
func OrderRow[S any, V orderedScalar](v RowValue[S, V]) OrderedRowExpression[S, V] {
	return OrderedRowExpression[S, V]{rowInput(v)}
}
func (e OrderedRowExpression[S, V]) Lt(v V) Predicate[S]  { return e.compare(less, v) }
func (e OrderedRowExpression[S, V]) Lte(v V) Predicate[S] { return e.compare(lessOrEqual, v) }
func (e OrderedRowExpression[S, V]) Gt(v V) Predicate[S]  { return e.compare(greater, v) }
func (e OrderedRowExpression[S, V]) Gte(v V) Predicate[S] { return e.compare(greaterOrEqual, v) }

// NullableOrderedRowExpression retains nullable results while comparisons take
// a concrete non-null value, matching generated nullable field comparisons.
type NullableOrderedRowExpression[S any, V orderedScalar] struct {
	RowExpression[S, value.Nullable[V]]
}

// OrderNullableRow exposes comparisons without removing SQL NULL from results.
func OrderNullableRow[S any, V orderedScalar](v RowValue[S, value.Nullable[V]]) NullableOrderedRowExpression[S, V] {
	return NullableOrderedRowExpression[S, V]{rowInput(v)}
}
func (e NullableOrderedRowExpression[S, V]) comparePresent(op operator, v ...V) Predicate[S] {
	return e.compare(op, presentValues(v)...)
}
func (e NullableOrderedRowExpression[S, V]) Eq(v V) Predicate[S] { return e.comparePresent(equal, v) }
func (e NullableOrderedRowExpression[S, V]) Ne(v V) Predicate[S] {
	return e.comparePresent(notEqual, v)
}
func (e NullableOrderedRowExpression[S, V]) In(v ...V) Predicate[S] {
	return e.comparePresent(in, v...)
}
func (e NullableOrderedRowExpression[S, V]) Lt(v V) Predicate[S] { return e.comparePresent(less, v) }
func (e NullableOrderedRowExpression[S, V]) Lte(v V) Predicate[S] {
	return e.comparePresent(lessOrEqual, v)
}
func (e NullableOrderedRowExpression[S, V]) Gt(v V) Predicate[S] { return e.comparePresent(greater, v) }
func (e NullableOrderedRowExpression[S, V]) Gte(v V) Predicate[S] {
	return e.comparePresent(greaterOrEqual, v)
}

func presentValues[V any](values []V) []value.Nullable[V] {
	result := make([]value.Nullable[V], len(values))
	for i, v := range values {
		result[i] = value.Of(v)
	}
	return result
}

// TextRowExpression adds LIKE and literal substring comparisons to row text.
type TextRowExpression[S any, V ~string] struct{ OrderedRowExpression[S, V] }

func (e TextRowExpression[S, V]) Like(v V) Predicate[S]     { return e.compare(like, v) }
func (e TextRowExpression[S, V]) Contains(v V) Predicate[S] { return e.compare(contains, v) }

// NullableTextRowExpression retains SQL NULL and concrete text comparisons.
type NullableTextRowExpression[S any, V ~string] struct {
	NullableOrderedRowExpression[S, V]
}

func (e NullableTextRowExpression[S, V]) Like(v V) Predicate[S] { return e.comparePresent(like, v) }
func (e NullableTextRowExpression[S, V]) Contains(v V) Predicate[S] {
	return e.comparePresent(contains, v)
}

// ValueComparison constructs group predicates over a selected value. It never
// exposes row predicates. SQL grouping/window restrictions still apply.
type ValueComparison[S, V any] struct{ expression Expression[S, V] }

func CompareValue[S, V any](v Expression[S, V]) ValueComparison[S, V] {
	return ValueComparison[S, V]{v}
}
func (e ValueComparison[S, V]) compare(op operator, v ...V) HavingPredicate[S] {
	return HavingPredicate[S]{expression: typedComparison(e.expression.node, op, e.expression.codec, v)}
}
func (e ValueComparison[S, V]) Eq(v V) HavingPredicate[S]     { return e.compare(equal, v) }
func (e ValueComparison[S, V]) Ne(v V) HavingPredicate[S]     { return e.compare(notEqual, v) }
func (e ValueComparison[S, V]) In(v ...V) HavingPredicate[S]  { return e.compare(in, v...) }
func (e ValueComparison[S, V]) IsNull() HavingPredicate[S]    { return e.compare(isNull) }
func (e ValueComparison[S, V]) IsNotNull() HavingPredicate[S] { return e.compare(isNotNull) }

// OrderedValueComparison adds ordered group comparisons for selected values.
type OrderedValueComparison[S any, V orderedScalar] struct{ ValueComparison[S, V] }

func OrderValue[S any, V orderedScalar](v Expression[S, V]) OrderedValueComparison[S, V] {
	return OrderedValueComparison[S, V]{CompareValue(v)}
}
func (e OrderedValueComparison[S, V]) Lt(v V) HavingPredicate[S]  { return e.compare(less, v) }
func (e OrderedValueComparison[S, V]) Lte(v V) HavingPredicate[S] { return e.compare(lessOrEqual, v) }
func (e OrderedValueComparison[S, V]) Gt(v V) HavingPredicate[S]  { return e.compare(greater, v) }
func (e OrderedValueComparison[S, V]) Gte(v V) HavingPredicate[S] {
	return e.compare(greaterOrEqual, v)
}

// NullableOrderedValueComparison uses non-null comparison values without
// changing the selected expression's nullable result contract.
type NullableOrderedValueComparison[S any, V orderedScalar] struct {
	ValueComparison[S, value.Nullable[V]]
}

func OrderNullableValue[S any, V orderedScalar](v Expression[S, value.Nullable[V]]) NullableOrderedValueComparison[S, V] {
	return NullableOrderedValueComparison[S, V]{CompareValue(v)}
}
func (e NullableOrderedValueComparison[S, V]) comparePresent(op operator, v ...V) HavingPredicate[S] {
	return e.compare(op, presentValues(v)...)
}
func (e NullableOrderedValueComparison[S, V]) Eq(v V) HavingPredicate[S] {
	return e.comparePresent(equal, v)
}
func (e NullableOrderedValueComparison[S, V]) Ne(v V) HavingPredicate[S] {
	return e.comparePresent(notEqual, v)
}
func (e NullableOrderedValueComparison[S, V]) In(v ...V) HavingPredicate[S] {
	return e.comparePresent(in, v...)
}
func (e NullableOrderedValueComparison[S, V]) Lt(v V) HavingPredicate[S] {
	return e.comparePresent(less, v)
}
func (e NullableOrderedValueComparison[S, V]) Lte(v V) HavingPredicate[S] {
	return e.comparePresent(lessOrEqual, v)
}
func (e NullableOrderedValueComparison[S, V]) Gt(v V) HavingPredicate[S] {
	return e.comparePresent(greater, v)
}
func (e NullableOrderedValueComparison[S, V]) Gte(v V) HavingPredicate[S] {
	return e.comparePresent(greaterOrEqual, v)
}

// TextValueComparison supplies LIKE/Contains for selected text in HAVING.
type TextValueComparison[S any, V ~string] struct{ OrderedValueComparison[S, V] }

func TextValue[S any, V ~string](v Expression[S, V]) TextValueComparison[S, V] {
	return TextValueComparison[S, V]{OrderValue(v)}
}
func (e TextValueComparison[S, V]) Like(v V) HavingPredicate[S]     { return e.compare(like, v) }
func (e TextValueComparison[S, V]) Contains(v V) HavingPredicate[S] { return e.compare(contains, v) }

// NullableTextValueComparison supplies text conditions while retaining NULL.
type NullableTextValueComparison[S any, V ~string] struct {
	NullableOrderedValueComparison[S, V]
}

func TextNullableValue[S any, V ~string](v Expression[S, value.Nullable[V]]) NullableTextValueComparison[S, V] {
	return NullableTextValueComparison[S, V]{OrderNullableValue(v)}
}
func (e NullableTextValueComparison[S, V]) Like(v V) HavingPredicate[S] {
	return e.comparePresent(like, v)
}
func (e NullableTextValueComparison[S, V]) Contains(v V) HavingPredicate[S] {
	return e.comparePresent(contains, v)
}
