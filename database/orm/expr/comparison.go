package expr

import (
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/value"
)

func compareRowValues[S, V any](left, right RowValue[S, V], op querycore.Operator) Predicate[S] {
	l, r := rowInput(left).data, rowInput(right).data
	return Predicate[S]{core: ormcore.CompareExpressions(l.core, r.core, op, l.column.Text && r.column.Text)}
}
func compareSelectedValues[S, V any](left, right Selected[S, V], op querycore.Operator) HavingPredicate[S] {
	return HavingPredicate[S]{core: ormcore.CompareExpressions(left.data.core, right.data.core, op, left.data.column.Text && right.data.column.Text)}
}

// Equal compares equal SQL values; NULL makes the condition unknown.
// Both row operands retain the same scope and Go type, including nullability.
//
// Example: expr.Equal(left, right)
func Equal[S any, V any](left, right RowValue[S, V]) Predicate[S] {
	return compareRowValues(left, right, querycore.Equal)
}

// EqualValue is Equal's selected counterpart, producing a HAVING predicate.
// SQL grouping and window restrictions still apply to both operands.
//
// Example: expr.EqualValue(left, right)
func EqualValue[S any, V any](left, right Selected[S, V]) HavingPredicate[S] {
	return compareSelectedValues(left, right, querycore.Equal)
}

// NotEqual compares unequal SQL values; NULL makes the condition unknown.
// Both row operands retain the same scope and Go type, including nullability.
//
// Example: expr.NotEqual(left, right)
func NotEqual[S any, V any](left, right RowValue[S, V]) Predicate[S] {
	return compareRowValues(left, right, querycore.NotEqual)
}

// NotEqualValue is NotEqual's selected counterpart, producing a HAVING predicate.
// SQL grouping and window restrictions still apply to both operands.
//
// Example: expr.NotEqualValue(left, right)
func NotEqualValue[S any, V any](left, right Selected[S, V]) HavingPredicate[S] {
	return compareSelectedValues(left, right, querycore.NotEqual)
}

// Less compares ordered SQL values using <.
// Both row operands retain the same scope and Go type, including nullability.
//
// Example: expr.Less(left, right)
func Less[S any, V orderedScalar](left, right RowValue[S, V]) Predicate[S] {
	return compareRowValues(left, right, querycore.Less)
}

// LessValue is Less's selected counterpart, producing a HAVING predicate.
// SQL grouping and window restrictions still apply to both operands.
//
// Example: expr.LessValue(left, right)
func LessValue[S any, V orderedScalar](left, right Selected[S, V]) HavingPredicate[S] {
	return compareSelectedValues(left, right, querycore.Less)
}

// LessNullable compares nullable operands without removing SQL NULL.
// Widen non-null inputs explicitly with NullableRow.
//
// Example: expr.LessNullable(left, right)
func LessNullable[S any, V orderedScalar](left, right RowValue[S, value.Nullable[V]]) Predicate[S] {
	return compareRowValues(left, right, querycore.Less)
}

// LessNullableValue compares nullable selected operands for HAVING.
//
// Example: expr.LessNullableValue(left, right)
func LessNullableValue[S any, V orderedScalar](left, right Selected[S, value.Nullable[V]]) HavingPredicate[S] {
	return compareSelectedValues(left, right, querycore.Less)
}

// LessOrEqual compares ordered SQL values using <=.
// Both row operands retain the same scope and Go type, including nullability.
//
// Example: expr.LessOrEqual(left, right)
func LessOrEqual[S any, V orderedScalar](left, right RowValue[S, V]) Predicate[S] {
	return compareRowValues(left, right, querycore.LessOrEqual)
}

// LessOrEqualValue is LessOrEqual's selected counterpart, producing a HAVING predicate.
// SQL grouping and window restrictions still apply to both operands.
//
// Example: expr.LessOrEqualValue(left, right)
func LessOrEqualValue[S any, V orderedScalar](left, right Selected[S, V]) HavingPredicate[S] {
	return compareSelectedValues(left, right, querycore.LessOrEqual)
}

// LessOrEqualNullable compares nullable operands without removing SQL NULL.
// Widen non-null inputs explicitly with NullableRow.
//
// Example: expr.LessOrEqualNullable(left, right)
func LessOrEqualNullable[S any, V orderedScalar](left, right RowValue[S, value.Nullable[V]]) Predicate[S] {
	return compareRowValues(left, right, querycore.LessOrEqual)
}

// LessOrEqualNullableValue compares nullable selected operands for HAVING.
//
// Example: expr.LessOrEqualNullableValue(left, right)
func LessOrEqualNullableValue[S any, V orderedScalar](left, right Selected[S, value.Nullable[V]]) HavingPredicate[S] {
	return compareSelectedValues(left, right, querycore.LessOrEqual)
}

// Greater compares ordered SQL values using >.
// Both row operands retain the same scope and Go type, including nullability.
//
// Example: expr.Greater(left, right)
func Greater[S any, V orderedScalar](left, right RowValue[S, V]) Predicate[S] {
	return compareRowValues(left, right, querycore.Greater)
}

// GreaterValue is Greater's selected counterpart, producing a HAVING predicate.
// SQL grouping and window restrictions still apply to both operands.
//
// Example: expr.GreaterValue(left, right)
func GreaterValue[S any, V orderedScalar](left, right Selected[S, V]) HavingPredicate[S] {
	return compareSelectedValues(left, right, querycore.Greater)
}

// GreaterNullable compares nullable operands without removing SQL NULL.
// Widen non-null inputs explicitly with NullableRow.
//
// Example: expr.GreaterNullable(left, right)
func GreaterNullable[S any, V orderedScalar](left, right RowValue[S, value.Nullable[V]]) Predicate[S] {
	return compareRowValues(left, right, querycore.Greater)
}

// GreaterNullableValue compares nullable selected operands for HAVING.
//
// Example: expr.GreaterNullableValue(left, right)
func GreaterNullableValue[S any, V orderedScalar](left, right Selected[S, value.Nullable[V]]) HavingPredicate[S] {
	return compareSelectedValues(left, right, querycore.Greater)
}

// GreaterOrEqual compares ordered SQL values using >=.
// Both row operands retain the same scope and Go type, including nullability.
//
// Example: expr.GreaterOrEqual(left, right)
func GreaterOrEqual[S any, V orderedScalar](left, right RowValue[S, V]) Predicate[S] {
	return compareRowValues(left, right, querycore.GreaterOrEqual)
}

// GreaterOrEqualValue is GreaterOrEqual's selected counterpart, producing a HAVING predicate.
// SQL grouping and window restrictions still apply to both operands.
//
// Example: expr.GreaterOrEqualValue(left, right)
func GreaterOrEqualValue[S any, V orderedScalar](left, right Selected[S, V]) HavingPredicate[S] {
	return compareSelectedValues(left, right, querycore.GreaterOrEqual)
}

// GreaterOrEqualNullable compares nullable operands without removing SQL NULL.
// Widen non-null inputs explicitly with NullableRow.
//
// Example: expr.GreaterOrEqualNullable(left, right)
func GreaterOrEqualNullable[S any, V orderedScalar](left, right RowValue[S, value.Nullable[V]]) Predicate[S] {
	return compareRowValues(left, right, querycore.GreaterOrEqual)
}

// GreaterOrEqualNullableValue compares nullable selected operands for HAVING.
//
// Example: expr.GreaterOrEqualNullableValue(left, right)
func GreaterOrEqualNullableValue[S any, V orderedScalar](left, right Selected[S, value.Nullable[V]]) HavingPredicate[S] {
	return compareSelectedValues(left, right, querycore.GreaterOrEqual)
}

// Like matches a typed SQL LIKE pattern; wildcard characters are intentional.
// Both row operands retain the same scope and Go type, including nullability.
//
// Example: expr.Like(left, right)
func Like[S any, V ~string](left, right RowValue[S, V]) Predicate[S] {
	return compareRowValues(left, right, querycore.Like)
}

// LikeValue is Like's selected counterpart, producing a HAVING predicate.
// SQL grouping and window restrictions still apply to both operands.
//
// Example: expr.LikeValue(left, right)
func LikeValue[S any, V ~string](left, right Selected[S, V]) HavingPredicate[S] {
	return compareSelectedValues(left, right, querycore.Like)
}

// LikeNullable compares nullable operands without removing SQL NULL.
// Widen non-null inputs explicitly with NullableRow.
//
// Example: expr.LikeNullable(left, right)
func LikeNullable[S any, V ~string](left, right RowValue[S, value.Nullable[V]]) Predicate[S] {
	return compareRowValues(left, right, querycore.Like)
}

// LikeNullableValue compares nullable selected operands for HAVING.
//
// Example: expr.LikeNullableValue(left, right)
func LikeNullableValue[S any, V ~string](left, right Selected[S, value.Nullable[V]]) HavingPredicate[S] {
	return compareSelectedValues(left, right, querycore.Like)
}

// DistinctFrom compares unequal values while treating NULL as a comparable value.
// Both row operands retain the same scope and Go type, including nullability.
//
// Example: expr.DistinctFrom(left, right)
func DistinctFrom[S any, V any](left, right RowValue[S, V]) Predicate[S] {
	return compareRowValues(left, right, querycore.IsDistinctFrom)
}

// DistinctFromValue is DistinctFrom's selected counterpart, producing a HAVING predicate.
// SQL grouping and window restrictions still apply to both operands.
//
// Example: expr.DistinctFromValue(left, right)
func DistinctFromValue[S any, V any](left, right Selected[S, V]) HavingPredicate[S] {
	return compareSelectedValues(left, right, querycore.IsDistinctFrom)
}

// NotDistinctFrom compares equal values while treating NULL as a comparable value.
// Both row operands retain the same scope and Go type, including nullability.
//
// Example: expr.NotDistinctFrom(left, right)
func NotDistinctFrom[S any, V any](left, right RowValue[S, V]) Predicate[S] {
	return compareRowValues(left, right, querycore.IsNotDistinctFrom)
}

// NotDistinctFromValue is NotDistinctFrom's selected counterpart, producing a HAVING predicate.
// SQL grouping and window restrictions still apply to both operands.
//
// Example: expr.NotDistinctFromValue(left, right)
func NotDistinctFromValue[S any, V any](left, right Selected[S, V]) HavingPredicate[S] {
	return compareSelectedValues(left, right, querycore.IsNotDistinctFrom)
}
