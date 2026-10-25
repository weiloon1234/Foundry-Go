package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var invalid = orm.ManyToManyOf(models.Users.Groups, models.Users.ID, models.Memberships.UserID, models.Friendships.ToID, models.Users.ID)
