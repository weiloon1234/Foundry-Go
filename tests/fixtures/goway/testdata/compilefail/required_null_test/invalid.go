package required_null_test

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func F() { orm.NullableNumberIsNull(models.Orders.TotalCents) }
