package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

var invalid = query.HavingAnd(query.Count[models.User]().Gt(1), query.Count[models.Order]().Gt(1))
