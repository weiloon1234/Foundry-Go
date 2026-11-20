package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var invalid = expr.Avg(models.Users.Birthday)
