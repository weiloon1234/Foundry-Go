package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

var _ = query.SelectValue(models.QueryUsers(), query.Value(models.UserFields().Age)).GroupBy(query.Param(models.OrderFields().TotalCents, 1).Group())
