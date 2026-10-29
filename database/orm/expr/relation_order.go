package expr

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
)

// RelationOrder adapts a computed row order to a relation's actual target.
// Example: r.OrderBy(expr.RelationOrder(r,expr.Negate(expr.Value(orm.NumberExpr(Users.Age))).Asc())).
func RelationOrder[M, N any](r orm.ExistenceRelation[M, N], o Order[N]) orm.Order[N] {
	return orm.FoundryRelationOrder(r, o.core, sqlowner.Metadata())
}

// RelationPivotOrder adapts a computed row order to the relation's actual pivot.
// Example: r.OrderByPivot(expr.RelationPivotOrder(r,expr.Lower(expr.Value(orm.TextExpr(Links.Note))).Asc())).
func RelationPivotOrder[M, N, P any](r orm.ThroughRelation[M, N, P], o Order[P]) orm.Order[P] {
	return orm.FoundryRelationPivotOrder(r, o.core, sqlowner.Metadata())
}
