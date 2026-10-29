package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var invalid = orm.ManyToManyOf(models.Users.Groups, orm.KeyExpr(models.Users.ID), orm.ColumnExpr(models.Memberships.UserID), orm.ColumnExpr(models.Friendships.ToID), orm.KeyExpr(models.Users.ID))
