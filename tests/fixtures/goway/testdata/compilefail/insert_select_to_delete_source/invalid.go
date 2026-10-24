package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var source orm.InsertSelect[models.User, models.User]
var _ = orm.DeleteSource[models.User, models.User](source)
