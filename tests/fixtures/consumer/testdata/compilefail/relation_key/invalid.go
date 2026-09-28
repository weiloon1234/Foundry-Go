package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

var invalid = query.BelongsTo(models.OrderFields().BuyerID, models.OrderFields().ID)
