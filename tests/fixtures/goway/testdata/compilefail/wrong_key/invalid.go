package wrong_key

import (
	"context"
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/model"
)

func F() {
	var ex database.Executor
	var key model.ID[models.User]
	models.Orders.Find(context.Background(), ex, key)
}
