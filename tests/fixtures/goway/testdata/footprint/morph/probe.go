package morph

import (
	"context"
	j "foundry.test/goway/models/joined"
	"github.com/weiloon1234/Foundry-Go/database"
)

func F(ctx context.Context, db database.Executor) ([]j.Pivot, error) {
	return j.Pivots.Query().With(j.MorphParent, j.MorphTarget).All(ctx, db)
}
