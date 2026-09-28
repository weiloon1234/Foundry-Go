package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

var invalid = query.CursorFor(models.QueryUsers()).Where(models.UserFields().Email.Eq("Ada"))
