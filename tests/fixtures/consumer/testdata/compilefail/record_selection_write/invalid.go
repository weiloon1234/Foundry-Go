package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

var q = models.QueryUsers()
var invalid = query.SelectRecord(q, q.Scope()).Create
