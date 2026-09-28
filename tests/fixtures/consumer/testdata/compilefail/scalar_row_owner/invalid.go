package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

var scalar = query.ScalarRowQuery(models.QueryOrders(), query.SelectValue(models.QueryUsers(), models.UserFields().Age.Value()))
var _ = query.GreaterNullable(query.NullableRow(models.UserFields().Age), scalar)
