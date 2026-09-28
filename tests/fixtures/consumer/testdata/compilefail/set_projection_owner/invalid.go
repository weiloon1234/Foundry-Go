package invalid

import (
	"foundry.test/consumer/models"
	"foundry.test/consumer/reports"
)

var a = reports.ProjectUserSummary(models.QueryUsers()).Query()
var b = reports.ProjectBuyerTotals(models.QueryOrders()).Query()
var _ = a.Union(b)
