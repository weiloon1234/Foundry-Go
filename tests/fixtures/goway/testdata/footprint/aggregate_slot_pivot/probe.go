package aggregate_slot_pivot

import (
	"context"
	j "foundry.test/goway/models/aggregates"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

func F(ctx context.Context, db database.Executor) ([]j.Parent, error) {
	r := j.Parents.Count.Using(expr.Related(orm.Pivot(j.Peers), expr.CountDistinctField(orm.TextExpr(j.Pivots.Role))))
	return orm.All(ctx, db, j.Parents.Query().With(r))
}
