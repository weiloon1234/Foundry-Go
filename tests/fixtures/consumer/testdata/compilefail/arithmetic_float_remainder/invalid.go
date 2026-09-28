package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

var number = query.FloatOf(models.UserFields().Age)
var _ = query.Remainder(number, number)
