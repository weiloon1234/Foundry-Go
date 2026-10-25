package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var _, _ = orm.Upsert(nil, nil, models.Users.Query(), orm.OnConflict[models.Order]().DoNothing())
