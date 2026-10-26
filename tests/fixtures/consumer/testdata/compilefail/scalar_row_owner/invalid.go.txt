package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

var scalar = query.ScalarRowQuery(models.QueryOrders(), query.SelectValue(models.QueryUsers(), query.Value(models.UserFields().Age)))
var _ = query.GreaterNullable(query.NullableRow(models.UserFields().Age), scalar)
