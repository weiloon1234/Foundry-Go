package constant_owner

import (
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

type A struct{}
type B struct{}

var _ expr.RowExpression[B, string] = expr.Constant[A](codec.String[string](), "x")
