package invalid

import (
	a "foundry.test/goway/models/extensions"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var _ = orm.Keyed(orm.Lock(a.Articles.Query()), a.Articles.Key())
