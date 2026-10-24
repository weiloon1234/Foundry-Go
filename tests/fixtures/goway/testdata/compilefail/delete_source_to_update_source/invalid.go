package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var source orm.DeleteSource[models.User, models.User]
var _ = orm.UpdateSource[models.User, models.User](source)
