package invalid

import (
	"github.com/weiloon1234/Foundry-Go/audit"
)

type First struct{}
type Second struct{}

var _ = audit.ActionID[First](audit.ActionID[Second]{})
