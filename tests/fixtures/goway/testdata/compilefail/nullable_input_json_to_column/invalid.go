package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/value"
)

var source orm.NullableInputJSON[models.User, string, string]
var _ = orm.NullableInputColumn[models.User, string, value.JSON[string]](source)
