package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

var a = query.SelectValue(models.QueryUsers(), query.Value(models.UserFields().Email))
var b = query.SelectValue(models.QueryUsers(), query.Value(models.UserFields().Nickname))
var _ = a.Union(b)
