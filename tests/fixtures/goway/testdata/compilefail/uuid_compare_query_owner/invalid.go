package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
)

var _ = models.Users.Query().Where(orm.UUIDGreater(models.Orders.ID, model.ID[models.Order]{}))
