package aggregate_slot_pivot

import (
	"context"
	j "foundry.test/goway/models/aggregates"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

func F(ctx context.Context, db database.Executor) ([]j.Parent, error) {
	r := j.Parents.Count.Using(expr.Related(orm.Pivot(j.Peers), expr.CountDistinctField(j.Pivots.Role)))
	return j.Parents.Query().With(r).All(ctx, db)
}
