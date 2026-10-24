package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/value"
)

var source orm.InputJSON[models.User, string, string]
var _ = orm.InputColumn[models.User, string, value.JSON[string]](source)
