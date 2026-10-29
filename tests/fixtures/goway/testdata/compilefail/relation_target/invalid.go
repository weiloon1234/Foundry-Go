package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var _ = orm.HasOneOf(models.Users.Introducer, orm.KeyExpr(models.Users.ID), orm.ColumnExpr(models.Orders.BuyerID))
