package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var source orm.InputEnum[models.User, models.Level, models.Level]
var _ = orm.InputNumber[models.User, models.Level, models.Level](source)
