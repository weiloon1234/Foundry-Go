package invalid

import (
	"foundry.test/consumer/models"
	"foundry.test/consumer/reports"
)

var invalid = reports.SelectBuyerTotals(models.QueryOrders(), reports.BuyerTotalsSelection[models.Order]{}).Having(models.OrderFields().TotalCents.Gt(1))
