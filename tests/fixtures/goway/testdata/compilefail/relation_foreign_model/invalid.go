package relation_foreign_model

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func F() { orm.HasManyOf(models.Users.Orders, models.Users.ID, models.Users.ID) }
