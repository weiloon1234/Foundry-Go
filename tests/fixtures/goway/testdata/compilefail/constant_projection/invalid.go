package constant_projection

import (
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/value"
)

type A struct{}

var _ expr.RowExpression[A, string] = expr.Constant[A](codec.Nullable(codec.String[string]()), value.Null[string]())
