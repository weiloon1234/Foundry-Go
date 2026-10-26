package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

var invalid = query.RecursiveCTE("tree", models.QueryUsers(), func(self query.RecursiveSelf[models.Order]) query.RecordQuerySource[models.Order] {
	return self
})
