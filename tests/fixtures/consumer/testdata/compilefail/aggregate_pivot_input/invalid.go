package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

var invalid = query.Related(models.UserRelations().Groups.Pivot(), query.CountDistinctField(models.GroupFields().Code))
