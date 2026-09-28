package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

var invalid = query.Related(models.UserRelations().Groups.Pivot(), models.GroupFields().Code.CountDistinct())
