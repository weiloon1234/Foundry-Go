package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

var _ = models.QueryUsers().Where(query.EqualValue(query.Value(models.UserFields().Age), query.Value(models.UserFields().Age)))
