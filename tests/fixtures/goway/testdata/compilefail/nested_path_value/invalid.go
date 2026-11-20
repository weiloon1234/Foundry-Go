package invalid

import (
	a "foundry.test/goway/models/extensions"
	"github.com/weiloon1234/Foundry-Go/http/modelbinding"
)

var _ = modelbinding.ByColumn(nil, a.Articles.Query(), a.Articles.Key(), func(int) string { return "" })
