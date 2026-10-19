package invalid

import (
	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/jobs"
)

var _ = application.EventOutbox(foundation.NewKey[*jobs.Outbox]("wrong"), "publications")
