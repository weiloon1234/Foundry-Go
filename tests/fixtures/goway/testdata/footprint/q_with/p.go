package q_with

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"context"
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database"
)

func F(ctx context.Context, db database.Executor) ([]models.User, error) {
	return orm.All(ctx, db, models.Users.Query().With(models.UserRelations.Orders))
}
