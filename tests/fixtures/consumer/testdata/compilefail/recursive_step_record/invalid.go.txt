package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

var invalid = query.RecursiveCTE("tree", models.QueryUsers(), func(query.RecursiveSelf[models.User]) query.RecordQuerySource[models.User] {
	return models.QueryOrders()
})
