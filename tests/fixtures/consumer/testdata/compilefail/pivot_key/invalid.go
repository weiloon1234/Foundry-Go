package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var slot orm.Through[models.User, models.User, models.Membership]
var invalid = orm.ManyToManyOf(slot, orm.JoinKeys(models.Users.ID, models.Memberships.UserID), orm.JoinKeys(models.Memberships.GroupCode, models.Users.ID))
