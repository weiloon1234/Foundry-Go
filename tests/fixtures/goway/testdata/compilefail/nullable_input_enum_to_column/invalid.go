package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var source orm.NullableInputEnum[models.User, models.Level, models.Level]
var _ = orm.NullableInputColumn[models.User, models.Level, models.Level](source)
