package through

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"context"
	j "foundry.test/goway/models/joined"
	"github.com/weiloon1234/Foundry-Go/database"
)

func F(ctx context.Context, db database.Executor) ([]j.Parent, error) {
	return orm.All(ctx, db, j.Parents.Query().With(j.TargetsThrough))
}
