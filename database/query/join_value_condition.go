package query

import "github.com/weiloon1234/Foundry-Go/value"

func compareJoinValues[L, R, V any](left RowValue[L, V], right RowValue[R, V], op operator) JoinOn[L, R] {
	return JoinOn[L, R]{expression: binaryComparison{rowInput(left).Value().node, rowInput(right).Value().node, op}}
}

// OnEqual compares equal SQL values; NULL makes the condition unknown.
// Computed operands retain their respective join sides and the same Go value type.
func OnEqual[L, R any, V any](left RowValue[L, V], right RowValue[R, V]) JoinOn[L, R] {
	return compareJoinValues(left, right, equal)
}

// OnNotEqual compares unequal SQL values; NULL makes the condition unknown.
// Computed operands retain their respective join sides and the same Go value type.
func OnNotEqual[L, R any, V any](left RowValue[L, V], right RowValue[R, V]) JoinOn[L, R] {
	return compareJoinValues(left, right, notEqual)
}

// OnLess compares ordered SQL values using <.
// Computed operands retain their respective join sides and the same Go value type.
func OnLess[L, R any, V orderedScalar](left RowValue[L, V], right RowValue[R, V]) JoinOn[L, R] {
	return compareJoinValues(left, right, less)
}

// OnLessNullable retains nullable operands and distinct join-side ownership.
func OnLessNullable[L, R any, V orderedScalar](left RowValue[L, value.Nullable[V]], right RowValue[R, value.Nullable[V]]) JoinOn[L, R] {
	return compareJoinValues(left, right, less)
}

// OnLessOrEqual compares ordered SQL values using <=.
// Computed operands retain their respective join sides and the same Go value type.
func OnLessOrEqual[L, R any, V orderedScalar](left RowValue[L, V], right RowValue[R, V]) JoinOn[L, R] {
	return compareJoinValues(left, right, lessOrEqual)
}

// OnLessOrEqualNullable retains nullable operands and distinct join-side ownership.
func OnLessOrEqualNullable[L, R any, V orderedScalar](left RowValue[L, value.Nullable[V]], right RowValue[R, value.Nullable[V]]) JoinOn[L, R] {
	return compareJoinValues(left, right, lessOrEqual)
}

// OnGreater compares ordered SQL values using >.
// Computed operands retain their respective join sides and the same Go value type.
func OnGreater[L, R any, V orderedScalar](left RowValue[L, V], right RowValue[R, V]) JoinOn[L, R] {
	return compareJoinValues(left, right, greater)
}

// OnGreaterNullable retains nullable operands and distinct join-side ownership.
func OnGreaterNullable[L, R any, V orderedScalar](left RowValue[L, value.Nullable[V]], right RowValue[R, value.Nullable[V]]) JoinOn[L, R] {
	return compareJoinValues(left, right, greater)
}

// OnGreaterOrEqual compares ordered SQL values using >=.
// Computed operands retain their respective join sides and the same Go value type.
func OnGreaterOrEqual[L, R any, V orderedScalar](left RowValue[L, V], right RowValue[R, V]) JoinOn[L, R] {
	return compareJoinValues(left, right, greaterOrEqual)
}

// OnGreaterOrEqualNullable retains nullable operands and distinct join-side ownership.
func OnGreaterOrEqualNullable[L, R any, V orderedScalar](left RowValue[L, value.Nullable[V]], right RowValue[R, value.Nullable[V]]) JoinOn[L, R] {
	return compareJoinValues(left, right, greaterOrEqual)
}

// OnLike matches a typed SQL LIKE pattern; wildcard characters are intentional.
// Computed operands retain their respective join sides and the same Go value type.
func OnLike[L, R any, V ~string](left RowValue[L, V], right RowValue[R, V]) JoinOn[L, R] {
	return compareJoinValues(left, right, like)
}

// OnLikeNullable retains nullable operands and distinct join-side ownership.
func OnLikeNullable[L, R any, V ~string](left RowValue[L, value.Nullable[V]], right RowValue[R, value.Nullable[V]]) JoinOn[L, R] {
	return compareJoinValues(left, right, like)
}

// OnDistinctFrom compares unequal values while treating NULL as a comparable value.
// Computed operands retain their respective join sides and the same Go value type.
func OnDistinctFrom[L, R any, V any](left RowValue[L, V], right RowValue[R, V]) JoinOn[L, R] {
	return compareJoinValues(left, right, isDistinctFrom)
}

// OnNotDistinctFrom compares equal values while treating NULL as a comparable value.
// Computed operands retain their respective join sides and the same Go value type.
func OnNotDistinctFrom[L, R any, V any](left RowValue[L, V], right RowValue[R, V]) JoinOn[L, R] {
	return compareJoinValues(left, right, isNotDistinctFrom)
}
