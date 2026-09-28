package invalid

import (
	"context"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/model"
)

var orderID model.ID[models.Order]
var invalid, _ = models.QueryUsers().Where(models.UserFields().Age.Gt(18)).Find(context.Background(), nil, orderID)
