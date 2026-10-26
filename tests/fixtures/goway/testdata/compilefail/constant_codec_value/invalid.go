package constant_codec_value

import (
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

type A struct{}

var _ = expr.Constant[A](codec.Bool[bool](), "x")
