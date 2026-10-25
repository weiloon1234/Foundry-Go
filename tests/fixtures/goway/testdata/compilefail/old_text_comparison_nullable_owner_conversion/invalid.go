package invalid

import (
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

type user struct{}
type order struct{}

func invalid() {
	users := query.TextNullableValue(query.Value(query.NewNullableTextField[user]("users", "nickname", codec.String[string]())))
	orders := query.NullableTextValueComparison[order, string](users)
	_ = orders.Eq("x")
}
