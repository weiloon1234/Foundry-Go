package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/decimal"
)

var invalid query.AggregateRelation[models.User, decimal.Decimal] = models.UserAggregates().OrderTotal
