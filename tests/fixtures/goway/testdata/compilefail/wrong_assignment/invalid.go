package wrong_assignment

import (
	"context"
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database"
)

func F() {
	var tx database.Transactor
	models.Orders.Insert(context.Background(), tx, models.Users.Email.Set("x"))
}
