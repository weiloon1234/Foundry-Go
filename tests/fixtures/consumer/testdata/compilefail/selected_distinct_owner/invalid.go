package invalid

import (
	"foundry.test/consumer/models"
	foundryquery "github.com/weiloon1234/Foundry-Go/database/query"
)

var _ = models.QueryUsers().DistinctOnValues(foundryquery.Value(models.OrderFields().TotalCents).Key())
