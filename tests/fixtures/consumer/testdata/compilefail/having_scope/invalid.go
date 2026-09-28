package invalid

import (
	"foundry.test/consumer/models"
	"foundry.test/consumer/reports"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

var invalid = reports.SelectBuyerTotals(models.QueryOrders(), reports.BuyerTotalsSelection[models.Order]{}).Having(query.Count[models.User]().Gt(1))
