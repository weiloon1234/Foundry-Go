package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var _ models.User
var _ orm.ExpressionColumn[models.User, string, string]
