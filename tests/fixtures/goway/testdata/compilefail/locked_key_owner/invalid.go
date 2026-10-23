package invalid

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

type CountryCode string
type LocationCode string
type Country struct{ Code CountryCode }

var countries = orm.Define("countries", func(b orm.Builder[Country], c *Country) orm.Table[Country, CountryCode] {
	return orm.TableOf(b, &c.Code)
})
var _, _ = countries.Query().Where(countries.Key().Eq(LocationCode("A"))).ForUpdate().SkipLocked().Require(nil, nil)
