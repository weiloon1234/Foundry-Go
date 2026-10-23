package expr

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
)

// Where applies computed row predicates to an unaliased model query.
// Example: expr.Where(Users.Query(),expr.Add(age,age).Gt(18)).
func Where[M any](q orm.Query[M], predicates ...Predicate[M]) orm.Query[M] {
	for _, p := range predicates {
		q = orm.FoundryWhere(q, p.core, sqlowner.Queries())
	}
	return q
}

// OrderBy applies computed row ordering to an unaliased model query.
// Example: expr.OrderBy(Users.Query(),expr.Add(age,age).Asc()).
func OrderBy[M any](q orm.Query[M], orders ...Order[M]) orm.Query[M] {
	for _, o := range orders {
		q = orm.FoundryOrderBy(q, o.core, sqlowner.Queries())
	}
	return q
}

type orderedColumn[M, V, P any] interface {
	orm.ExpressionColumn[M, V, P]
	Gt(V) orm.Predicate[M]
}

// GreaterColumn compares same-owner ordered stored columns; enums lack this capability.
// Example: expr.GreaterColumn(Orders.TotalCents,Orders.TotalCents).
func GreaterColumn[M, V, P any](left, right orderedColumn[M, V, P]) Predicate[M] {
	return compareRowValues(Value(left), Value(right), querycore.Greater)
}
