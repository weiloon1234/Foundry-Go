package invalid

import (
	a "foundry.test/goway/models/extensions"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var _ = orm.IdentityOf(a.Authors, a.Articles.Key())
