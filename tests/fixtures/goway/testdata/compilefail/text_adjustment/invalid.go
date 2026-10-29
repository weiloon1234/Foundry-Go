package text_adjustment

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func F() { orm.NumberIncrement(models.Users.Email, 1) }
