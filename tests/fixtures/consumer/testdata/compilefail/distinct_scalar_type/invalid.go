package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

var _ = models.UserFields().ID.InQuery(query.SelectValue(models.QueryUsers(), models.UserFields().Email.Value()).Distinct())
