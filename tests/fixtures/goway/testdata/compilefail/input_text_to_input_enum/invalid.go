package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var source orm.InputText[models.User, models.Status, models.Status]
var _ = orm.InputEnum[models.User, models.Status, models.Status](source)
