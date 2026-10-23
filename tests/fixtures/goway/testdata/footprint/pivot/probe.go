package pivot

import (
	"context"
	j "foundry.test/goway/models/joined"
	"github.com/weiloon1234/Foundry-Go/database"
)

func F(ctx context.Context, db database.Executor) ([]j.Parent, error) {
	return j.Parents.Query().With(j.Peers.WithPivot(j.PivotTarget)).All(ctx, db)
}
