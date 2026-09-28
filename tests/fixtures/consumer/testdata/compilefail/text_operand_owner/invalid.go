package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

type other struct{}

var otherUsers = query.As[other](models.QueryUsers(), "other")
var _ = query.Concat(models.UserFields().Email, models.UserFieldsAt(otherUsers.Scope()).Email)
