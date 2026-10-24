package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var source orm.InputColumn[models.User, string, string]
var _ = orm.InputText[models.User, string, string](source)
