package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

var _ = query.CommonTable[models.Order](query.CTE("users_copy", models.QueryUsers()))
