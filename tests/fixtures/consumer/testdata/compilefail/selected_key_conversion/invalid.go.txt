package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

var _ = query.ProjectionKey[models.Order](query.Value(models.UserFields().Age).Key())
