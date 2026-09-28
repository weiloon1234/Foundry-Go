package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

var _ = query.NumericRange(query.WindowFor(models.QueryOrders()), models.UserFields().Age.Value())
