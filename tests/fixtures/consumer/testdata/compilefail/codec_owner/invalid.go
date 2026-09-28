package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/model"
)

var orderID model.ID[models.Order]
var invalid = codec.ID[models.User]().Scan(&orderID)
