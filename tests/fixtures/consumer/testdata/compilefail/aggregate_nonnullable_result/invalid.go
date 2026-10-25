package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"

	"github.com/weiloon1234/Foundry-Go/decimal"
)

var invalid orm.AggregateRelation[models.User, decimal.Decimal] = models.UserValues.OrderTotal
