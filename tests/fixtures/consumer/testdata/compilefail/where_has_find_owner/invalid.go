package invalid

import (
	"context"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/model"
)

var orderID model.ID[models.Order]
var invalid, _ = models.QueryUsers().WhereHas(models.UserRelations().Orders).Find(context.Background(), nil, orderID)
