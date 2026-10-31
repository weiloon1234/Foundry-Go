package expr

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
)

// Where applies computed row predicates to an unaliased model query.
//
// Example: expr.Where(q, predicates...)
func Where[M any](q orm.Query[M], predicates ...Predicate[M]) orm.Query[M] {
	for _, p := range predicates {
		q = orm.FoundryWhere(q, p.core, sqlowner.Queries())
	}
	return q
}

// OrderBy applies computed row ordering to an unaliased model query.
//
// Example: expr.OrderBy(q, orders...)
func OrderBy[M any](q orm.Query[M], orders ...Order[M]) orm.Query[M] {
	for _, o := range orders {
		q = orm.FoundryOrderBy(q, o.core, sqlowner.Queries())
	}
	return q
}

type orderedColumn[M, V, P any] interface {
	orm.OrderedExpressionColumn[M, V, P]
}

// GreaterColumn compares same-owner ordered stored columns; enums lack this capability.
//
// Example: expr.GreaterColumn(left, right)
func GreaterColumn[M, V, P any, L orderedColumn[M, V, P], R orderedColumn[M, V, P]](left L, right R) Predicate[M] {
	return compareRowValues(Value(left), Value(right), querycore.Greater)
}
