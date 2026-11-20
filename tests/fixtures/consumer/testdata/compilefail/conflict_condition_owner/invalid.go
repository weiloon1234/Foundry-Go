package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var _ = orm.OnConflict[models.User]().DoNothing().Where(orm.Eq(models.Orders.TotalCents, 1))
