package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

var _ = query.RangeBoundary[models.Order, int](query.NumericRange(query.WindowFor(models.QueryUsers()), query.Value(models.UserFields().Age)).Preceding(1))
