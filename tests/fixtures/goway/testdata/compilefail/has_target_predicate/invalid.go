package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var _ = orm.Has(models.UserRelations.Orders, models.Users.Age.Gt(18))
