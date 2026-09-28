package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

var r = query.NumericRange(query.WindowFor(models.QueryUsers()), models.UserFields().Age.Value())
var _ = r.Between(query.NumericRange(query.WindowFor(models.QueryUsers()), query.Count[models.User]().Value()).Preceding(1), r.CurrentRow())
