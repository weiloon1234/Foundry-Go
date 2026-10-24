package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
)

var _ = orm.UUIDLess(models.Orders.TotalCents, model.ID[models.Order]{})
