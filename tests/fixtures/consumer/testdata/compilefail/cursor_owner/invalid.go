package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/value"
)

var wrong query.Cursor[models.Order]
var invalid = query.CursorRequest[models.User]{Size: 2, After: value.Set(wrong)}
