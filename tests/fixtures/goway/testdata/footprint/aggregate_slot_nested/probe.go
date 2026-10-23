package aggregate_slot_nested

import (
	"context"
	j "foundry.test/goway/models/aggregates"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

func F(ctx context.Context, db database.Executor) ([]j.Parent, error) {
	r := j.Targets.Count.Using(expr.Related(j.TargetParent, expr.Count[j.Parent]()))
	return j.Parents.Query().With(j.DirectTargets.With(r)).All(ctx, db)
}
