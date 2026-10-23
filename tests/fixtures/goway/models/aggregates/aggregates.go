package aggregates

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// DirectTargets binds keys after table initialization, independently of computations.
var DirectTargets = orm.HasManyOf(Parents.Targets, Parents.ID, Targets.ParentID)

// ParentAggregates keeps computations out of table and relation initializers.
var ParentAggregates = struct {
	Count  orm.AggregateRelation[Parent, int64]
	Total  orm.AggregateRelation[Parent, value.Nullable[decimal.Decimal]]
	Peers  orm.AggregateRelation[Parent, int64]
	Pivots orm.AggregateRelation[Parent, int64]
}{
	Count:  Parents.Count.Using(expr.Related(DirectTargets, expr.Count[Target]())),
	Total:  Parents.Total.Using(expr.Related(DirectTargets, expr.Sum(Targets.Rank))),
	Peers:  Parents.Count.Using(expr.Related(Peers, expr.Count[Target]())),
	Pivots: Parents.OtherCount.Using(expr.Related(orm.Pivot(Peers), expr.Count[Pivot]())),
}
