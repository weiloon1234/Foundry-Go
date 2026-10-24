package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var source orm.InputNumber[models.User, models.Level, models.Level]
var _ = orm.InputEnum[models.User, models.Level, models.Level](source)
