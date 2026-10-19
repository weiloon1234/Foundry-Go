package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

var _ = query.WindowFor(models.QueryUsers()).PartitionBy(query.Param(models.OrderFields().TotalCents, 1).Group())
