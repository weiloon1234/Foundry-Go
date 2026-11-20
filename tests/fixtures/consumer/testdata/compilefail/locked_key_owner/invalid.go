package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/consumer/models"
)

var _, _ = orm.FirstLocked(nil, nil, orm.Lock(models.Countries.Query()).SkipLocked().Where(orm.Eq(models.Countries.Name, "x")).Where(orm.Eq(models.Countries.Code, models.LocationCode("A"))))
