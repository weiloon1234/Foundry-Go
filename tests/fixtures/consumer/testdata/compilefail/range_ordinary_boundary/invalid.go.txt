package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

var r = query.NumericRange(query.WindowFor(models.QueryUsers()), query.Value(models.UserFields().Age))
var _ = r.Between(query.Preceding(1), r.CurrentRow())
