package compilefail

import (
	"foundry.test/pluginbase"
	"foundry.test/plugindep"
	"github.com/weiloon1234/Foundry-Go/jobs"
)

type Other struct{ Value string }

var _ = jobs.RegisterJob[Other](nil, pluginbase.Dispatcher, plugindep.Job, nil)
