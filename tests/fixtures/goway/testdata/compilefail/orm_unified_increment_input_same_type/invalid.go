package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var input orm.InputNumber[models.User, int64, int64]
var _ = orm.Increment(input, int64(1))
