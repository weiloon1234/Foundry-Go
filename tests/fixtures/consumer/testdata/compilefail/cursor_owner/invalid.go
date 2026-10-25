package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/value"
)

var wrong orm.Cursor[models.Order]
var invalid = orm.CursorRequest[models.User]{Size: 2, After: value.Set(wrong)}
