package compilefail

import (
	"foundry.test/plugindep"
	"github.com/weiloon1234/Foundry-Go/foundation"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/jobs"
)

var _ = foundryhttp.RegisterRoute(nil, foundation.NewKey[*jobs.Dispatcher]("wrong"), plugindep.Route, nil)
