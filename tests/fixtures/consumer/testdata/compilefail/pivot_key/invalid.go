package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var slot orm.Through[models.User, models.User, models.Membership]
var invalid = orm.ManyToManyOf(slot, orm.KeyExpr(models.Users.ID), orm.ColumnExpr(models.Memberships.UserID), orm.NullableTextExpr(models.Memberships.GroupCode), orm.KeyExpr(models.Users.ID))
