package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

type alias struct{}

var source = query.As[alias](query.CTE("users_copy", models.QueryUsers()), "u")
var ids = query.SelectValue(source, query.Value(models.UserFieldsAt(source.Scope()).ID))
var _ = models.OrderFields().ID.InQuery(ids)
