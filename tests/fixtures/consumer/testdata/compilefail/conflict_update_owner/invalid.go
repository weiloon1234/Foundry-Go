package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var _ = orm.OnConflict[models.User]().DoUpdate(orm.ConflictIncoming(models.Orders.TotalCents))
