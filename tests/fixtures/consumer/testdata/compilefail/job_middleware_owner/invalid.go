package invalid

import (
	"foundry.test/consumer/background"
	"github.com/weiloon1234/Foundry-Go/jobs"
)

var foreign jobs.Middleware[background.RemoveExport]
var _ = jobs.HandlerOptions[background.Welcome]{Middleware: []jobs.Middleware[background.Welcome]{foreign}}
