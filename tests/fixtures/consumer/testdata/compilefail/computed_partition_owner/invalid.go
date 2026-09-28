package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

var _ = query.WindowFor(models.QueryUsers()).PartitionBy(models.OrderFields().TotalCents.Param(1).Group())
