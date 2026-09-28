package invalid

import (
	"foundry.test/consumer/models"
	"foundry.test/consumer/windowqueries"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

var _ = windowqueries.ProjectRankingRow(models.QueryOrders()).SelectNumber(query.PercentRank(query.WindowFor(models.QueryOrders())))
