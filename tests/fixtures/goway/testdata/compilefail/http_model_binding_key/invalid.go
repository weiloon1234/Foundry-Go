package invalid

import (
	a "foundry.test/goway/models/extensions"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/http/modelbinding"
)

var _ = modelbinding.ByKey(nil, orm.Keyed(a.Articles.Query(), a.Articles.Key()), func(int) string { return "" })
