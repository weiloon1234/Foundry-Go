package wrong_key

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"context"
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/model"
)

func F() {
	var ex database.Executor
	var key model.ID[models.User]
	orm.Find(context.Background(), ex, models.Orders.Table, key)
}
