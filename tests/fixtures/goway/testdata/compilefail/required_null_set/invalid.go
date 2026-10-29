package required_null_set

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func F() { orm.NullableNumberSetNull(models.Orders.TotalCents) }
