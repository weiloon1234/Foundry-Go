package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/value"
)

var source orm.InputColumn[models.User, string, value.JSON[string]]
var _ = orm.InputJSON[models.User, string, string](source)
