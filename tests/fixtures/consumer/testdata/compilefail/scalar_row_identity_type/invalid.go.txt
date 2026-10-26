package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

var scalar = query.ScalarRowQuery(models.QueryUsers(), query.SelectValue(models.QueryOrders(), query.Value(models.OrderFields().ID)))
var _ = query.Equal(query.NullableRow(models.UserFields().ID), scalar)
