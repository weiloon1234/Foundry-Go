package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var source orm.InputColumn[models.User, int, int]
var _ = orm.InputNumber[models.User, int, int](source)
