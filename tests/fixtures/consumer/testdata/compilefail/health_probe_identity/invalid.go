package compilefail

import "github.com/weiloon1234/Foundry-Go/health"

var dynamic string
var _ = health.Probe{ID: dynamic}
