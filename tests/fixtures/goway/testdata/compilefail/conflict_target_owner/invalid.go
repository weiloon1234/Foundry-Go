package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var _ = orm.OnConflict[models.User](orm.ConflictKey(orm.KeyExpr(models.Orders.Key())))
