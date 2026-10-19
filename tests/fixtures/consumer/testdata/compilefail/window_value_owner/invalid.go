package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

var _ = query.Lag(query.Value(models.OrderFields().TotalCents), 1, query.WindowFor(models.QueryUsers()))
