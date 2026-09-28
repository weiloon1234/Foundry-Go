package invalid

import (
	"github.com/weiloon1234/Foundry-Go/validation"
	"github.com/weiloon1234/Foundry-Go/value"
)

type Input struct{ Count value.Optional[int] }

var field = validation.DefineField("count", func(v Input) value.Optional[int] { return v.Count })
var _ = field.Rules(validation.Required[string]())
