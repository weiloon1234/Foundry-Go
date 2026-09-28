package invalid

import (
	"github.com/weiloon1234/Foundry-Go/validation"
	"github.com/weiloon1234/Foundry-Go/value"
)

type Input struct{ Flag value.Optional[bool] }

var field = validation.DefineField("flag", func(v Input) value.Optional[bool] { return v.Flag })
var _ = field.Rules(validation.Prohibited[int]())
