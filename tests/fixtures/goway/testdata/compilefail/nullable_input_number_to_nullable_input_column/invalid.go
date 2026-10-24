package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var source orm.NullableInputNumber[models.User, int, int]
var _ = orm.NullableInputColumn[models.User, int, int](source)
