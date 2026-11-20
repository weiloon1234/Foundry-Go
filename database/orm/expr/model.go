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

// GreaterColumn compares same-owner ordered stored columns; enums lack this capability.
//
// Example: expr.GreaterColumn(left, right)
func GreaterColumn[M, V, P, I1, I2 any, K1, K2 orm.OrderedKind, R1, R2 orm.ColumnRole](left orm.ColumnHandle[M, V, I1, P, K1, R1], right orm.ColumnHandle[M, V, I2, P, K2, R2]) Predicate[M] {
	return compareRowValues(Value(left), Value(right), querycore.Greater)
}
