package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/model"
)

var _, _ = orm.FirstLocked(nil, nil, orm.Lock(models.Users.Query()).NoWait().Where(orm.KeyEq(models.Users.ID, model.ID[models.Order]{})))
