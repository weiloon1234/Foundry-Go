package invalid

import (
	"context"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
)

var orderID model.ID[models.Order]
var invalid, _ = orm.Keyed(models.Users.Query().Where(models.Users.Age.Gt(18)), models.Users.Key()).Find(context.Background(), nil, orderID)
