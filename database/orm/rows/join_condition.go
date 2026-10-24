package rows

import (
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"github.com/weiloon1234/Foundry-Go/value"
)

func compareJoinValues[L, R, V any](left expr.RowValue[L, V], right expr.RowValue[R, V], op querycore.Operator) JoinOn[L, R] {
	return JoinOn[L, R]{core: ormcore.JoinComparison(expr.FoundryRowValue(left, sqlowner.Expressions()), expr.FoundryRowValue(right, sqlowner.Expressions()), op, expr.FoundrySelectedColumn(expr.Select(left), sqlowner.Expressions()).Text && expr.FoundrySelectedColumn(expr.Select(right), sqlowner.Expressions()).Text)}
}

// OnEqual compares same-typed row values across distinct join sides. Example: rows.OnEqual(left,right).
func OnEqual[L, R any, V any](left expr.RowValue[L, V], right expr.RowValue[R, V]) JoinOn[L, R] {
	return compareJoinValues(left, right, querycore.Equal)
}

// OnNotEqual compares same-typed row values across distinct join sides. Example: rows.OnNotEqual(left,right).
func OnNotEqual[L, R any, V any](left expr.RowValue[L, V], right expr.RowValue[R, V]) JoinOn[L, R] {
	return compareJoinValues(left, right, querycore.NotEqual)
}

// OnLess compares same-typed row values across distinct join sides. Example: rows.OnLess(left,right).
func OnLess[L, R any, V expr.OrderedScalar](left expr.RowValue[L, V], right expr.RowValue[R, V]) JoinOn[L, R] {
	return compareJoinValues(left, right, querycore.Less)
}

// OnLessNullable compares same-typed row values across distinct join sides. Example: rows.OnLessNullable(left,right).
func OnLessNullable[L, R any, V expr.OrderedScalar](left expr.RowValue[L, value.Nullable[V]], right expr.RowValue[R, value.Nullable[V]]) JoinOn[L, R] {
	return compareJoinValues(left, right, querycore.Less)
}

// OnLessOrEqual compares same-typed row values across distinct join sides. Example: rows.OnLessOrEqual(left,right).
func OnLessOrEqual[L, R any, V expr.OrderedScalar](left expr.RowValue[L, V], right expr.RowValue[R, V]) JoinOn[L, R] {
	return compareJoinValues(left, right, querycore.LessOrEqual)
}

// OnLessOrEqualNullable compares same-typed row values across distinct join sides. Example: rows.OnLessOrEqualNullable(left,right).
func OnLessOrEqualNullable[L, R any, V expr.OrderedScalar](left expr.RowValue[L, value.Nullable[V]], right expr.RowValue[R, value.Nullable[V]]) JoinOn[L, R] {
	return compareJoinValues(left, right, querycore.LessOrEqual)
}

// OnGreater compares same-typed row values across distinct join sides. Example: rows.OnGreater(left,right).
func OnGreater[L, R any, V expr.OrderedScalar](left expr.RowValue[L, V], right expr.RowValue[R, V]) JoinOn[L, R] {
	return compareJoinValues(left, right, querycore.Greater)
}

// OnGreaterNullable compares same-typed row values across distinct join sides. Example: rows.OnGreaterNullable(left,right).
func OnGreaterNullable[L, R any, V expr.OrderedScalar](left expr.RowValue[L, value.Nullable[V]], right expr.RowValue[R, value.Nullable[V]]) JoinOn[L, R] {
	return compareJoinValues(left, right, querycore.Greater)
}

// OnGreaterOrEqual compares same-typed row values across distinct join sides. Example: rows.OnGreaterOrEqual(left,right).
func OnGreaterOrEqual[L, R any, V expr.OrderedScalar](left expr.RowValue[L, V], right expr.RowValue[R, V]) JoinOn[L, R] {
	return compareJoinValues(left, right, querycore.GreaterOrEqual)
}

// OnGreaterOrEqualNullable compares same-typed row values across distinct join sides. Example: rows.OnGreaterOrEqualNullable(left,right).
func OnGreaterOrEqualNullable[L, R any, V expr.OrderedScalar](left expr.RowValue[L, value.Nullable[V]], right expr.RowValue[R, value.Nullable[V]]) JoinOn[L, R] {
	return compareJoinValues(left, right, querycore.GreaterOrEqual)
}

// OnLike compares same-typed row values across distinct join sides. Example: rows.OnLike(left,right).
func OnLike[L, R any, V ~string](left expr.RowValue[L, V], right expr.RowValue[R, V]) JoinOn[L, R] {
	return compareJoinValues(left, right, querycore.Like)
}

// OnLikeNullable compares same-typed row values across distinct join sides. Example: rows.OnLikeNullable(left,right).
func OnLikeNullable[L, R any, V ~string](left expr.RowValue[L, value.Nullable[V]], right expr.RowValue[R, value.Nullable[V]]) JoinOn[L, R] {
	return compareJoinValues(left, right, querycore.Like)
}

// OnDistinctFrom compares same-typed row values across distinct join sides. Example: rows.OnDistinctFrom(left,right).
func OnDistinctFrom[L, R any, V any](left expr.RowValue[L, V], right expr.RowValue[R, V]) JoinOn[L, R] {
	return compareJoinValues(left, right, querycore.IsDistinctFrom)
}

// OnNotDistinctFrom compares same-typed row values across distinct join sides. Example: rows.OnNotDistinctFrom(left,right).
func OnNotDistinctFrom[L, R any, V any](left expr.RowValue[L, V], right expr.RowValue[R, V]) JoinOn[L, R] {
	return compareJoinValues(left, right, querycore.IsNotDistinctFrom)
}

// OnAnd combines conditions with the same left/right scopes. Example: rows.OnAnd(key,age).
func OnAnd[L, R any](conditions ...JoinOn[L, R]) JoinOn[L, R] { return combineOn(false, conditions) }

// OnOr combines alternatives without changing side ownership. Example: rows.OnOr(primary,fallback).
func OnOr[L, R any](conditions ...JoinOn[L, R]) JoinOn[L, R] { return combineOn(true, conditions) }

func combineOn[L, R any](any bool, conditions []JoinOn[L, R]) JoinOn[L, R] {
	if len(conditions) > querycore.MaxExpressionNodes {
		return JoinOn[L, R]{core: ormcore.JoinCondition{Err: ormcore.Invalid("join conditions exceed their resource bound")}}
	}
	inputs := make([]ormcore.JoinCondition, len(conditions))
	for i, c := range conditions {
		inputs[i] = c.core
	}
	return JoinOn[L, R]{core: ormcore.CombineJoinConditions(any, inputs)}
}

// Not negates ON with SQL NULL semantics. Example: condition.Not().
func (on JoinOn[L, R]) Not() JoinOn[L, R] {
	return JoinOn[L, R]{core: ormcore.NegateJoinCondition(on.core)}
}

// WhereLeft adds predicates owned by the left input, preserving outer rows. Example: on.WhereLeft(left.Eq(1)).
func (on JoinOn[L, R]) WhereLeft(predicates ...expr.Predicate[L]) JoinOn[L, R] {
	return filterOn(on, true, predicates)
}

// WhereRight adds predicates owned by the right input. Example: on.WhereRight(right.Eq(1)).
func (on JoinOn[L, R]) WhereRight(predicates ...expr.Predicate[R]) JoinOn[L, R] {
	return filterOn(on, false, predicates)
}

func filterOn[L, R, S any](on JoinOn[L, R], left bool, predicates []expr.Predicate[S]) JoinOn[L, R] {
	if len(predicates) > querycore.MaxExpressionNodes {
		on.core.Err = ormcore.Invalid("join filters exceed their resource bound")
		return on
	}
	inputs := make([]ormcore.Predicate, len(predicates))
	for i, p := range predicates {
		inputs[i] = expr.FoundryPredicate(p, sqlowner.Expressions())
	}
	return JoinOn[L, R]{core: ormcore.FilterJoinCondition(on.core, left, inputs)}
}
