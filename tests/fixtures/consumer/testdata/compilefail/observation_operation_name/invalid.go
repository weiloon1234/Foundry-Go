package compilefail

import "github.com/weiloon1234/Foundry-Go/observability"

var dynamic string
var _ = observability.Operation{Kind: observability.Job, Name: dynamic}
