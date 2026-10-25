package expr

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
)

// Related computes a typed relation aggregate; bind it with a slot's Using method.
// Example: Users.OrdersCount.Using(expr.Related(UserRelations.Orders,expr.Count[Order]())).
func Related[M, N, V any](source orm.AggregateSource[M, N], computation AggregateExpression[N, V]) orm.AggregateRelation[M, V] {
	expression, column := FoundryAggregate(computation, sqlowner.Expressions())
	return orm.FoundryRelated(source, expression, column, sqlowner.Expressions())
}

// RelatedValue computes a relation aggregate per parent without a model slot.
// The aggregate keeps its own nullability: COUNT is present, an empty SUM is NULL.
// Example: count := expr.RelatedValue(UserRelations.Orders,expr.Count[Order]()).
func RelatedValue[M, N, V any](source orm.AggregateSource[M, N], computation AggregateExpression[N, V]) RowExpression[M, V] {
	expression, column := FoundryAggregate(computation, sqlowner.Expressions())
	core := orm.FoundryRelatedValue(source, expression, column, sqlowner.Expressions())
	column.Sensitive = column.Sensitive || core.Sensitive
	return FoundryRowExpression[M](core, column, sqlowner.Expressions())
}
