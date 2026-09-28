package invalid

import (
	"github.com/weiloon1234/Foundry-Go/validation"
	"github.com/weiloon1234/Foundry-Go/value"
)

type Input struct {
	Text value.Optional[value.Nullable[string]]
}

var field = validation.DefineField("text", func(v Input) value.Optional[value.Nullable[string]] { return v.Text })
var _ = field.Rules(validation.Required[string]())
