package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var _, _ = orm.Upsert(nil, nil, models.Users.Query(), orm.OnConflict[models.User]().DoNothing(), models.Orders.TotalCents.Set(1))
