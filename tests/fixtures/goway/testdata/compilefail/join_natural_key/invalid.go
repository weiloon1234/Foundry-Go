package join_natural_key

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

type countryCode string
type locationCode string
type country struct{ Code countryCode }
type location struct{ Code locationCode }
type countries struct {
	orm.Table[country, countryCode]
}
type locations struct {
	orm.Table[location, locationCode]
}

var cs = orm.Define("countries", func(b orm.Builder[country], r *country) countries { return countries{orm.TableOf(b, &r.Code)} })
var ls = orm.Define("locations", func(b orm.Builder[location], r *location) locations { return locations{orm.TableOf(b, &r.Code)} })
var _ = rows.On(expr.Value(cs.Key()), expr.Value(ls.Key()))
