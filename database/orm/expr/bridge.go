package expr

import (
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
)

// OrderedScalar is the shared ordered row-value constraint. Example: func less[V expr.OrderedScalar](v V).
type OrderedScalar interface{ orderedScalar }

// FoundryRowValue exposes validated row metadata without admitting selected values. Example: FoundryRowValue(e,sqlowner.Expressions()).
func FoundryRowValue[S, V any](e RowValue[S, V], seal sqlowner.ExpressionSeal) ormcore.Expression {
	if !seal.Valid() {
		return ormcore.Expression{Err: ormcore.Invalid("invalid SQL owner capability")}
	}
	return rowInput(e).data.core
}

// FoundrySelectedColumn retains concrete value codecs across a subquery boundary. Example: FoundrySelectedColumn(e,sqlowner.Expressions()).
func FoundrySelectedColumn[S, V any](e Selected[S, V], seal sqlowner.ExpressionSeal) ormcore.Column[V] {
	if !seal.Valid() {
		return ormcore.Column[V]{Err: ormcore.Invalid("invalid SQL owner capability")}
	}
	return e.data.column
}

// FoundryRowExpression constructs a validated row-phase SQL value. Example: FoundryRowExpression[S](core,column,sqlowner.Expressions()).
func FoundryRowExpression[S, V any](core ormcore.Expression, column ormcore.Column[V], seal sqlowner.ExpressionSeal) RowExpression[S, V] {
	if !seal.Valid() {
		return RowExpression[S, V]{data: rowData[V]{core: ormcore.Expression{Err: ormcore.Invalid("invalid SQL owner capability")}}}
	}
	return RowExpression[S, V]{data: rowData[V]{core: core, column: column}}
}

// FoundrySelectedValue constructs a validated selected-phase SQL value. Example: FoundrySelectedValue[S](core,column,sqlowner.Expressions()).
func FoundrySelectedValue[S, V any](core ormcore.Expression, column ormcore.Column[V], seal sqlowner.ExpressionSeal) Selected[S, V] {
	if !seal.Valid() {
		return Selected[S, V]{data: rowData[V]{core: ormcore.Expression{Err: ormcore.Invalid("invalid SQL owner capability")}}}
	}
	return Selected[S, V]{data: rowData[V]{core: core, column: column}}
}

// FoundryPredicateValue constructs a validated scoped SQL condition. Example: FoundryPredicateValue[S](core,sqlowner.Expressions()).
func FoundryPredicateValue[S any](core ormcore.Predicate, seal sqlowner.ExpressionSeal) Predicate[S] {
	if !seal.Valid() {
		return Predicate[S]{core: ormcore.Predicate{Err: ormcore.Invalid("invalid SQL owner capability")}}
	}
	return Predicate[S]{core: core}
}

// Not negates a row condition while preserving SQL NULL semantics. Example: expr.Not(rows.InQuery(id,inner)).
func Not[S any](p Predicate[S]) Predicate[S] {
	return Predicate[S]{core: ormcore.NegatePredicate(p.core)}
}
