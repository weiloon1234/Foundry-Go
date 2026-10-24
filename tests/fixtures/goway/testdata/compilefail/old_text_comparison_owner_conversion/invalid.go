package invalid

import (
	old "foundry.test/goway/oldmodels"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

func invalid() {
	users := query.TextValue(query.Value(old.UserFields().Email))
	orders := query.TextValueComparison[old.Order, string](users)
	_ = orders.Eq("x")
}
