package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

type alias struct{}

var q = models.QueryUsers()
var _ = query.As[alias](query.SelectRecord(q, q.Scope()).LockRows(query.UpdateLock(q.Scope())), "locked")
