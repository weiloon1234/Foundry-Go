package rows

import (
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"github.com/weiloon1234/Foundry-Go/value"
)

// TransactionCorrelatedScalar keeps one value bound to its enclosing scope. Example: rows.SelectTransactionCorrelatedScalar(c,expr.Select(childID)).
type TransactionCorrelatedScalar[O, I TransactionScopeType, V any] struct {
	_                      [0]*O
	_                      [0]*I
	_                      [0]*V
	transactionScalarPhase [0]struct{}
	core                   ormcore.Correlation
	column                 ormcore.Column[V]
}

// SelectTransactionCorrelatedScalar selects one lifted value without an execution escape. Example: rows.SelectTransactionCorrelatedScalar(c,expr.Select(childID)).
func SelectTransactionCorrelatedScalar[O, I TransactionScopeType, V any](c TransactionCorrelatedSource[O, I], v expr.Selected[TransactionCorrelation[O, I], V]) TransactionCorrelatedScalar[O, I, V] {
	s := SelectScalar(selectionAdapter[TransactionCorrelation[O, I]]{c.core.Source}, v)
	c.core.Source = s.source
	return TransactionCorrelatedScalar[O, I, V]{core: c.core, column: s.column}
}

// Where filters the inner SELECT with lifted predicates. Example: selected.Where(expr.Equal(childID,parentID)).
func (s TransactionCorrelatedScalar[O, I, V]) Where(ps ...expr.Predicate[TransactionCorrelation[O, I]]) TransactionCorrelatedScalar[O, I, V] {
	s.core.Source = whereSource(s.core.Source, ps)
	return s
}

// OrderBy appends inner ordering. Example: selected.OrderBy(childID.Desc()).
func (s TransactionCorrelatedScalar[O, I, V]) OrderBy(os ...expr.ProjectionOrder[TransactionCorrelation[O, I]]) TransactionCorrelatedScalar[O, I, V] {
	s.core.Source = recordOrders(s.core.Source, os)
	return s
}

// Limit retains an explicit inner limit. Example: selected.Limit(1).
func (s TransactionCorrelatedScalar[O, I, V]) Limit(n int) TransactionCorrelatedScalar[O, I, V] {
	s.core.Source.Node.Limit = value.Set(n)
	return s
}

// Offset retains an explicit inner offset. Example: selected.Offset(1).
func (s TransactionCorrelatedScalar[O, I, V]) Offset(n int) TransactionCorrelatedScalar[O, I, V] {
	s.core.Source.Node.Offset = n
	return s
}

// Exists tests the selected result in the enclosing scope. Example: selected.Limit(0).Exists().
func (s TransactionCorrelatedScalar[O, I, V]) Exists() expr.Predicate[O] {
	return expr.FoundryPredicateValue[O](ormcore.CorrelatedExists(s.core), sqlowner.Expressions())
}

// TransactionCorrelatedScalarQuery widens a missing scalar result to NULL. Example: rows.TransactionCorrelatedScalarQuery(selected.Limit(1)).
func TransactionCorrelatedScalarQuery[O, I TransactionScopeType, V any](s TransactionCorrelatedScalar[O, I, V]) expr.Selected[O, value.Nullable[V]] {
	return expr.Nullable(expr.FoundrySelectedValue[O](ormcore.CorrelatedScalarExpression(s.core), s.column, sqlowner.Expressions()))
}

// TransactionCorrelatedScalarNullableQuery keeps one nullable result layer. Example: rows.TransactionCorrelatedScalarNullableQuery(nullableSelection).
func TransactionCorrelatedScalarNullableQuery[O, I TransactionScopeType, V any](s TransactionCorrelatedScalar[O, I, value.Nullable[V]]) expr.Selected[O, value.Nullable[V]] {
	return expr.FoundrySelectedValue[O](ormcore.CorrelatedScalarExpression(s.core), s.column, sqlowner.Expressions())
}

// TransactionCorrelatedScalarRowQuery exposes a correlated scalar in row expressions. Example: expr.GreaterNullable(age,rows.TransactionCorrelatedScalarRowQuery(selected)).
func TransactionCorrelatedScalarRowQuery[O, I TransactionScopeType, V any](s TransactionCorrelatedScalar[O, I, V]) expr.RowExpression[O, value.Nullable[V]] {
	v := TransactionCorrelatedScalarQuery(s)
	return expr.FoundryRowExpression[O](expr.FoundrySelected(v, sqlowner.Expressions()), expr.FoundrySelectedColumn(v, sqlowner.Expressions()), sqlowner.Expressions())
}

// TransactionCorrelatedScalarNullableRowQuery preserves nullable row-expression results. Example: rows.TransactionCorrelatedScalarNullableRowQuery(selected).
func TransactionCorrelatedScalarNullableRowQuery[O, I TransactionScopeType, V any](s TransactionCorrelatedScalar[O, I, value.Nullable[V]]) expr.RowExpression[O, value.Nullable[V]] {
	v := TransactionCorrelatedScalarNullableQuery(s)
	return expr.FoundryRowExpression[O](expr.FoundrySelected(v, sqlowner.Expressions()), expr.FoundrySelectedColumn(v, sqlowner.Expressions()), sqlowner.Expressions())
}

// TransactionInCorrelatedQuery requires exact enclosing scope and value type. Example: rows.TransactionInCorrelatedQuery(userID,selected).
func TransactionInCorrelatedQuery[O, I TransactionScopeType, V any](v expr.RowValue[O, V], s TransactionCorrelatedScalar[O, I, V]) expr.Predicate[O] {
	return expr.FoundryPredicateValue[O](ormcore.CorrelatedMembership(expr.FoundryRowValue(v, sqlowner.Expressions()), s.core), sqlowner.Expressions())
}

// TransactionInNullableCorrelatedQuery admits nullable inner values with SQL NULL semantics. Example: rows.TransactionInNullableCorrelatedQuery(userID,nullableIDs).
func TransactionInNullableCorrelatedQuery[O, I TransactionScopeType, V any](v expr.RowValue[O, V], s TransactionCorrelatedScalar[O, I, value.Nullable[V]]) expr.Predicate[O] {
	return expr.FoundryPredicateValue[O](ormcore.CorrelatedMembership(expr.FoundryRowValue(v, sqlowner.Expressions()), s.core), sqlowner.Expressions())
}

// GroupBy retains the correlated result clause. Example: selected.GroupBy(clause).
func (r TransactionCorrelatedScalar[O, I, V]) GroupBy(keys ...expr.GroupKey[TransactionCorrelation[O, I]]) TransactionCorrelatedScalar[O, I, V] {
	r.core.Source = GroupBy(Query[TransactionCorrelation[O, I], struct{}]{source: r.core.Source}, keys...).source
	return r
}

// Having retains the correlated result clause. Example: selected.Having(clause).
func (r TransactionCorrelatedScalar[O, I, V]) Having(ps ...expr.HavingPredicate[TransactionCorrelation[O, I]]) TransactionCorrelatedScalar[O, I, V] {
	r.core.Source = Having(Query[TransactionCorrelation[O, I], struct{}]{source: r.core.Source}, ps...).source
	return r
}
