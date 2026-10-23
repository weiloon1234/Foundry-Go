package negative_test

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database"
	. "github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

var scan = database.ScanOne
var selected = Raw[models.User, int64]("1") // want expr.Raw
