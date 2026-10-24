package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var source orm.NullableInputText[models.User, models.Status, models.Status]
var _ = orm.NullableInputEnum[models.User, models.Status, models.Status](source)
