package invalid

import (
	"foundry.test/consumer/models"
	"foundry.test/consumer/windowqueries"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

var _ = windowqueries.ProjectRankingRow(models.QueryOrders()).SelectNumber(query.Lag(query.Value(models.OrderFields().TotalCents), 1, query.WindowFor(models.QueryOrders())))
