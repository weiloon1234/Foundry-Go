package invalid

import (
	old "foundry.test/goway/oldmodels"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

func invalid() {
	users := query.TextNullableValue(query.Value(old.UserFields().Nickname))
	orders := query.NullableTextValueComparison[old.Order, string](users)
	_ = orders.Eq("x")
}
