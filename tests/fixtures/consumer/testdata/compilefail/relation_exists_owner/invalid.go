package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var invalid = models.Orders.Query().Where(orm.Has(models.UserLinks.Orders))
