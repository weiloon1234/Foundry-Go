package aggregate_slot_nested

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"context"
	j "foundry.test/goway/models/aggregates"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

func F(ctx context.Context, db database.Executor) ([]j.Parent, error) {
	r := j.Targets.Count.Using(expr.Related(j.TargetParent, expr.Count[j.Parent]()))
	return orm.All(ctx, db, j.Parents.Query().With(j.DirectTargets.With(r)))
}
