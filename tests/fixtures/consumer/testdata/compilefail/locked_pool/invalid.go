package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database"
)

var _, _ = orm.AllLocked(nil, (*database.DB)(nil), orm.Lock(models.Users.Query()))
