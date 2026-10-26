package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

var count = query.Count[models.User]()
var _ = models.QueryUsers().OrderBy(query.AddValue(count.Value(), count.Value()).Asc())
