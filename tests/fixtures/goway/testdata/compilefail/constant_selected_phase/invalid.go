package constant_selected_phase

import (
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

type A struct{}

var _ expr.Selected[A, bool] = expr.Constant[A](codec.Bool[bool](), true)
