package wrong_assignment

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"context"
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database"
)

func F() {
	var tx database.Transactor
	orm.Insert(context.Background(), tx, models.Orders.Table, orm.TextSet(models.Users.Email, "x"))
}
