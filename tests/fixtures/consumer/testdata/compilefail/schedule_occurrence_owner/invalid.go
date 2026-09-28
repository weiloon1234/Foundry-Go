package compilefail

import (
	"foundry.test/consumer/background"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/schedule"
)

func invalid(invocation schedule.Invocation) jobs.ID[background.Welcome] {
	return invocation.Occurrence
}
