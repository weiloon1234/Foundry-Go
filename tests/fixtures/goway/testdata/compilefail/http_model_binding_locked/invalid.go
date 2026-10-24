package invalid

import (
	a "foundry.test/goway/models/extensions"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var _ = orm.Keyed(a.Articles.Query().ForUpdate(), a.Articles.Key())
