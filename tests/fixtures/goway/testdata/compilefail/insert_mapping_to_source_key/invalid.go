package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var source orm.InsertMapping[models.User, models.User]
var _ = orm.SourceKey[models.User, models.User](source)
