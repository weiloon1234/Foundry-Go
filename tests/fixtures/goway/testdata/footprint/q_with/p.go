package q_with

import (
	"context"
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database"
)

func F(ctx context.Context, db database.Executor) ([]models.User, error) {
	return models.Users.Query().With(models.UserRelations.Orders).All(ctx, db)
}
