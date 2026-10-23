package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var _ = orm.LockedQuery[models.User](models.Orders.Query().ForUpdate())
