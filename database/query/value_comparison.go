package query

import "github.com/weiloon1234/Foundry-Go/value"

// binaryOperator shares SQL tokens with literal comparisons. Contains belongs
// only to literal binding, where wildcard escaping is explicit.
func binaryOperator(op operator) (string, bool) {
	if escapedPattern(op) || op == notIn {
		return "", false
	}
	return comparisonOperator(op)
}
func compareRowValues[S, V any](left, right RowValue[S, V], op operator) Predicate[S] {
	return Predicate[S]{expression: binaryComparison{rowInput(left).Value().node, rowInput(right).Value().node, op}}
}
func compareSelectedValues[S, V any](left, right Expression[S, V], op operator) HavingPredicate[S] {
	return HavingPredicate[S]{expression: binaryComparison{left.node, right.node, op}}
}

// Equal compares equal SQL values; NULL makes the condition unknown.
// Both row operands retain the same scope and Go type, including nullability.
func Equal[S any, V any](left, right RowValue[S, V]) Predicate[S] {
	return compareRowValues(left, right, equal)
}

// EqualValue is Equal's selected counterpart, producing a HAVING predicate.
// SQL grouping and window restrictions still apply to both operands.
func EqualValue[S any, V any](left, right Expression[S, V]) HavingPredicate[S] {
	return compareSelectedValues(left, right, equal)
}

// NotEqual compares unequal SQL values; NULL makes the condition unknown.
// Both row operands retain the same scope and Go type, including nullability.
func NotEqual[S any, V any](left, right RowValue[S, V]) Predicate[S] {
	return compareRowValues(left, right, notEqual)
}

// NotEqualValue is NotEqual's selected counterpart, producing a HAVING predicate.
// SQL grouping and window restrictions still apply to both operands.
func NotEqualValue[S any, V any](left, right Expression[S, V]) HavingPredicate[S] {
	return compareSelectedValues(left, right, notEqual)
}

// Less compares ordered SQL values using <.
// Both row operands retain the same scope and Go type, including nullability.
func Less[S any, V orderedScalar](left, right RowValue[S, V]) Predicate[S] {
	return compareRowValues(left, right, less)
}

// LessValue is Less's selected counterpart, producing a HAVING predicate.
// SQL grouping and window restrictions still apply to both operands.
func LessValue[S any, V orderedScalar](left, right Expression[S, V]) HavingPredicate[S] {
	return compareSelectedValues(left, right, less)
}

// LessNullable compares nullable operands without removing SQL NULL.
// Widen non-null inputs explicitly with NullableRow.
func LessNullable[S any, V orderedScalar](left, right RowValue[S, value.Nullable[V]]) Predicate[S] {
	return compareRowValues(left, right, less)
}

// LessNullableValue compares nullable selected operands for HAVING.
func LessNullableValue[S any, V orderedScalar](left, right Expression[S, value.Nullable[V]]) HavingPredicate[S] {
	return compareSelectedValues(left, right, less)
}

// LessOrEqual compares ordered SQL values using <=.
// Both row operands retain the same scope and Go type, including nullability.
func LessOrEqual[S any, V orderedScalar](left, right RowValue[S, V]) Predicate[S] {
	return compareRowValues(left, right, lessOrEqual)
}

// LessOrEqualValue is LessOrEqual's selected counterpart, producing a HAVING predicate.
// SQL grouping and window restrictions still apply to both operands.
func LessOrEqualValue[S any, V orderedScalar](left, right Expression[S, V]) HavingPredicate[S] {
	return compareSelectedValues(left, right, lessOrEqual)
}

// LessOrEqualNullable compares nullable operands without removing SQL NULL.
// Widen non-null inputs explicitly with NullableRow.
func LessOrEqualNullable[S any, V orderedScalar](left, right RowValue[S, value.Nullable[V]]) Predicate[S] {
	return compareRowValues(left, right, lessOrEqual)
}

// LessOrEqualNullableValue compares nullable selected operands for HAVING.
func LessOrEqualNullableValue[S any, V orderedScalar](left, right Expression[S, value.Nullable[V]]) HavingPredicate[S] {
	return compareSelectedValues(left, right, lessOrEqual)
}

// Greater compares ordered SQL values using >.
// Both row operands retain the same scope and Go type, including nullability.
func Greater[S any, V orderedScalar](left, right RowValue[S, V]) Predicate[S] {
	return compareRowValues(left, right, greater)
}

// GreaterValue is Greater's selected counterpart, producing a HAVING predicate.
// SQL grouping and window restrictions still apply to both operands.
func GreaterValue[S any, V orderedScalar](left, right Expression[S, V]) HavingPredicate[S] {
	return compareSelectedValues(left, right, greater)
}

// GreaterNullable compares nullable operands without removing SQL NULL.
// Widen non-null inputs explicitly with NullableRow.
func GreaterNullable[S any, V orderedScalar](left, right RowValue[S, value.Nullable[V]]) Predicate[S] {
	return compareRowValues(left, right, greater)
}

// GreaterNullableValue compares nullable selected operands for HAVING.
func GreaterNullableValue[S any, V orderedScalar](left, right Expression[S, value.Nullable[V]]) HavingPredicate[S] {
	return compareSelectedValues(left, right, greater)
}

// GreaterOrEqual compares ordered SQL values using >=.
// Both row operands retain the same scope and Go type, including nullability.
func GreaterOrEqual[S any, V orderedScalar](left, right RowValue[S, V]) Predicate[S] {
	return compareRowValues(left, right, greaterOrEqual)
}

// GreaterOrEqualValue is GreaterOrEqual's selected counterpart, producing a HAVING predicate.
// SQL grouping and window restrictions still apply to both operands.
func GreaterOrEqualValue[S any, V orderedScalar](left, right Expression[S, V]) HavingPredicate[S] {
	return compareSelectedValues(left, right, greaterOrEqual)
}

// GreaterOrEqualNullable compares nullable operands without removing SQL NULL.
// Widen non-null inputs explicitly with NullableRow.
func GreaterOrEqualNullable[S any, V orderedScalar](left, right RowValue[S, value.Nullable[V]]) Predicate[S] {
	return compareRowValues(left, right, greaterOrEqual)
}

// GreaterOrEqualNullableValue compares nullable selected operands for HAVING.
func GreaterOrEqualNullableValue[S any, V orderedScalar](left, right Expression[S, value.Nullable[V]]) HavingPredicate[S] {
	return compareSelectedValues(left, right, greaterOrEqual)
}

// Like matches a typed SQL LIKE pattern; wildcard characters are intentional.
// Both row operands retain the same scope and Go type, including nullability.
func Like[S any, V ~string](left, right RowValue[S, V]) Predicate[S] {
	return compareRowValues(left, right, like)
}

// LikeValue is Like's selected counterpart, producing a HAVING predicate.
// SQL grouping and window restrictions still apply to both operands.
func LikeValue[S any, V ~string](left, right Expression[S, V]) HavingPredicate[S] {
	return compareSelectedValues(left, right, like)
}

// LikeNullable compares nullable operands without removing SQL NULL.
// Widen non-null inputs explicitly with NullableRow.
func LikeNullable[S any, V ~string](left, right RowValue[S, value.Nullable[V]]) Predicate[S] {
	return compareRowValues(left, right, like)
}

// LikeNullableValue compares nullable selected operands for HAVING.
func LikeNullableValue[S any, V ~string](left, right Expression[S, value.Nullable[V]]) HavingPredicate[S] {
	return compareSelectedValues(left, right, like)
}

// DistinctFrom compares unequal values while treating NULL as a comparable value.
// Both row operands retain the same scope and Go type, including nullability.
func DistinctFrom[S any, V any](left, right RowValue[S, V]) Predicate[S] {
	return compareRowValues(left, right, isDistinctFrom)
}

// DistinctFromValue is DistinctFrom's selected counterpart, producing a HAVING predicate.
// SQL grouping and window restrictions still apply to both operands.
func DistinctFromValue[S any, V any](left, right Expression[S, V]) HavingPredicate[S] {
	return compareSelectedValues(left, right, isDistinctFrom)
}

// NotDistinctFrom compares equal values while treating NULL as a comparable value.
// Both row operands retain the same scope and Go type, including nullability.
func NotDistinctFrom[S any, V any](left, right RowValue[S, V]) Predicate[S] {
	return compareRowValues(left, right, isNotDistinctFrom)
}

// NotDistinctFromValue is NotDistinctFrom's selected counterpart, producing a HAVING predicate.
// SQL grouping and window restrictions still apply to both operands.
func NotDistinctFromValue[S any, V any](left, right Expression[S, V]) HavingPredicate[S] {
	return compareSelectedValues(left, right, isNotDistinctFrom)
}
