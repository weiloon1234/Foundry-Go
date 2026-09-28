package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

var a = query.NullableRow(models.UserFields().Age)
var _ query.Expression[models.User, int] = query.AddNullable(a, a).Value()
