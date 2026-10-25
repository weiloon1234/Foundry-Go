package invalid

import (
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

type user struct{}
type order struct{}

func invalid() {
	users := query.TextValue(query.Value(query.NewTextField[user]("users", "email", codec.String[string]())))
	orders := query.TextValueComparison[order, string](users)
	_ = orders.Eq("x")
}
