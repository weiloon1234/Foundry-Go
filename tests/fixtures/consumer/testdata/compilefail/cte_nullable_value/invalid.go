package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

type alias struct{}

var source = query.As[alias](query.CTE("users_copy", models.QueryUsers()), "u")
var names = query.SelectValue(source, models.UserFieldsAt(source.Scope()).Nickname.Value())
var _ = models.UserFields().Email.InQuery(names)
