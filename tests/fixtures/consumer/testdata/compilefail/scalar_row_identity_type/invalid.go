package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

var scalar = query.ScalarRowQuery(models.QueryUsers(), query.SelectValue(models.QueryOrders(), models.OrderFields().ID.Value()))
var _ = query.Equal(query.NullableRow(models.UserFields().ID), scalar)
