package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

var _ = query.Lag(models.OrderFields().TotalCents.Value(), 1, query.WindowFor(models.QueryUsers()))
