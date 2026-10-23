package rows

import (
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"github.com/weiloon1234/Foundry-Go/value"
)

// CorrelatedScalar keeps one value bound to its enclosing scope. Example: rows.SelectCorrelatedScalar(c,expr.Select(childID)).
type CorrelatedScalar[O, I, V any] struct {
	_      [0]*O
	_      [0]*I
	_      [0]*V
	core   ormcore.Correlation
	column ormcore.Column[V]
}

// SelectCorrelatedScalar selects one lifted value without an execution escape. Example: rows.SelectCorrelatedScalar(c,expr.Count[rows.Correlation[User,Order]]().Value()).
func SelectCorrelatedScalar[O, I, V any](c CorrelatedSource[O, I], v expr.Selected[Correlation[O, I], V]) CorrelatedScalar[O, I, V] {
	s := SelectScalar(selectionAdapter[Correlation[O, I]]{c.core.Source}, v)
	c.core.Source = s.source
	return CorrelatedScalar[O, I, V]{core: c.core, column: s.column}
}

// Where filters the inner SELECT with lifted predicates. Example: selected.Where(expr.Equal(childID,parentID)).
func (s CorrelatedScalar[O, I, V]) Where(ps ...expr.Predicate[Correlation[O, I]]) CorrelatedScalar[O, I, V] {
	s.core.Source = whereSource(s.core.Source, ps)
	return s
}

// OrderBy appends inner ordering. Example: selected.OrderBy(childID.Desc()).
func (s CorrelatedScalar[O, I, V]) OrderBy(os ...expr.Order[Correlation[O, I]]) CorrelatedScalar[O, I, V] {
	s.core.Source = orderSource(s.core.Source, os)
	return s
}

// Limit retains an explicit inner limit. Example: selected.Limit(1).
func (s CorrelatedScalar[O, I, V]) Limit(n int) CorrelatedScalar[O, I, V] {
	s.core.Source.Node.Limit = value.Set(n)
	return s
}

// Offset retains an explicit inner offset. Example: selected.Offset(1).
func (s CorrelatedScalar[O, I, V]) Offset(n int) CorrelatedScalar[O, I, V] {
	s.core.Source.Node.Offset = n
	return s
}

// Exists tests the selected result in the enclosing scope. Example: selected.Limit(0).Exists().
func (s CorrelatedScalar[O, I, V]) Exists() expr.Predicate[O] {
	return expr.FoundryPredicateValue[O](ormcore.CorrelatedExists(s.core), sqlowner.Expressions())
}

// CorrelatedScalarQuery widens a missing scalar result to NULL. Example: rows.CorrelatedScalarQuery(selected.Limit(1)).
func CorrelatedScalarQuery[O, I, V any](s CorrelatedScalar[O, I, V]) expr.Selected[O, value.Nullable[V]] {
	return expr.Nullable(expr.FoundrySelectedValue[O](ormcore.CorrelatedScalarExpression(s.core), s.column, sqlowner.Expressions()))
}

// CorrelatedScalarNullableQuery keeps one nullable result layer. Example: rows.CorrelatedScalarNullableQuery(nullableSelection).
func CorrelatedScalarNullableQuery[O, I, V any](s CorrelatedScalar[O, I, value.Nullable[V]]) expr.Selected[O, value.Nullable[V]] {
	return expr.FoundrySelectedValue[O](ormcore.CorrelatedScalarExpression(s.core), s.column, sqlowner.Expressions())
}

// CorrelatedScalarRowQuery exposes a correlated scalar in row expressions. Example: expr.GreaterNullable(age,rows.CorrelatedScalarRowQuery(selected)).
func CorrelatedScalarRowQuery[O, I, V any](s CorrelatedScalar[O, I, V]) expr.RowExpression[O, value.Nullable[V]] {
	v := CorrelatedScalarQuery(s)
	return expr.FoundryRowExpression[O](expr.FoundrySelected(v, sqlowner.Expressions()), expr.FoundrySelectedColumn(v, sqlowner.Expressions()), sqlowner.Expressions())
}

// CorrelatedScalarNullableRowQuery preserves nullable row-expression results. Example: rows.CorrelatedScalarNullableRowQuery(selected).
func CorrelatedScalarNullableRowQuery[O, I, V any](s CorrelatedScalar[O, I, value.Nullable[V]]) expr.RowExpression[O, value.Nullable[V]] {
	v := CorrelatedScalarNullableQuery(s)
	return expr.FoundryRowExpression[O](expr.FoundrySelected(v, sqlowner.Expressions()), expr.FoundrySelectedColumn(v, sqlowner.Expressions()), sqlowner.Expressions())
}

// InCorrelatedQuery requires exact enclosing scope and value type. Example: rows.InCorrelatedQuery(userID,selected).
func InCorrelatedQuery[O, I, V any](v expr.RowValue[O, V], s CorrelatedScalar[O, I, V]) expr.Predicate[O] {
	return expr.FoundryPredicateValue[O](ormcore.CorrelatedMembership(expr.FoundryRowValue(v, sqlowner.Expressions()), s.core), sqlowner.Expressions())
}

// InNullableCorrelatedQuery admits nullable inner values with SQL NULL semantics. Example: rows.InNullableCorrelatedQuery(userID,nullableIDs).
func InNullableCorrelatedQuery[O, I, V any](v expr.RowValue[O, V], s CorrelatedScalar[O, I, value.Nullable[V]]) expr.Predicate[O] {
	return expr.FoundryPredicateValue[O](ormcore.CorrelatedMembership(expr.FoundryRowValue(v, sqlowner.Expressions()), s.core), sqlowner.Expressions())
}

type selectionAdapter[S any] struct{ source ormcore.Source }

func (s selectionAdapter[S]) rowSource(*S) ormcore.Source { return s.source }
