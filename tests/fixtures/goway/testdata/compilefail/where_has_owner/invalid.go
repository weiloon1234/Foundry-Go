package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var _ = models.Users.Query().Where(orm.Has(models.OrderRelations.Buyer))
