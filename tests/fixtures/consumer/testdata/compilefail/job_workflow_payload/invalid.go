package invalid

import (
	"foundry.test/consumer/background"
	"github.com/weiloon1234/Foundry-Go/jobs"
)

var _, _ = jobs.NewChain(background.Welcome{})
