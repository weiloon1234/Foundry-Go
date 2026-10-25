package load_query

import (
	"context"
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func Run(ctx context.Context, db database.Executor, parents []models.User) ([]models.User, error) {
	q := orm.WithRelationLimits(models.Users.Query().With(models.UserRelations.Orders.With(models.OrderRelations.Buyer)), orm.RelationLimits{BatchSize: 2, MaxRows: 20, MaxDepth: 2})
	loaded, err := orm.LoadFrom(ctx, db, q, parents)
	if err != nil {
		return nil, err
	}
	return orm.LoadMissingFrom(ctx, db, q, loaded)
}
