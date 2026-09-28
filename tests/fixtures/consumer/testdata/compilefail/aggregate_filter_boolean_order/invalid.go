package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

var _ = query.Exists[models.Order]().Filter(models.OrderFields().TotalCents.Gt(0)).Gt(true)
