package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

var _ = query.WhenValue(query.Count[models.User]().Gt(0), query.Count[models.User]().Value()).Else(query.Count[models.User]().Param(0).Value()).Eq(1)
