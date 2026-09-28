package invalid

import (
	"github.com/weiloon1234/Foundry-Go/validation"
)

type First struct{}
type Second struct{}

var condition validation.Rule[First]
var branch validation.Rule[Second]
var _ = validation.When(condition, branch)
