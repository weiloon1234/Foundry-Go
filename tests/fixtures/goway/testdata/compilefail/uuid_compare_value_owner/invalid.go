package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
)

var _ = orm.UUIDGreater(orm.KeyExpr(models.Orders.ID), model.ID[models.User]{})
