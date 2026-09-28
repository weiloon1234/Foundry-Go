package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

var _ = query.Less(query.Exists[models.User]().Param(true), query.Exists[models.User]().Param(false))
