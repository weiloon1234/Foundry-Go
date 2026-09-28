package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

var _ = models.QueryUsers().Where(query.EqualValue(models.UserFields().Age.Value(), models.UserFields().Age.Value()))
