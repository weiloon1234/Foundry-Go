package aggregate_slot_direct

import (
	"context"
	j "foundry.test/goway/models/aggregates"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

func F(ctx context.Context, db database.Executor) ([]j.Parent, error) {
	r := j.Parents.Count.Using(expr.Related(j.DirectTargets, expr.Count[j.Target]()))
	return j.Parents.Query().With(r, j.ParentAggregates.Total).All(ctx, db)
}
