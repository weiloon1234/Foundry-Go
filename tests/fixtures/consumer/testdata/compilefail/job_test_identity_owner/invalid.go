package compilefail

import (
	"foundry.test/consumer/background"
	"github.com/weiloon1234/Foundry-Go/jobs"
	jobstest "github.com/weiloon1234/Foundry-Go/testkit/jobs"
	"testing"
)

func invalid(t *testing.T, dispatcher *jobs.Dispatcher) {
	jobstest.AssertState(t, background.WelcomeJob, dispatcher, jobs.ID[background.RemoveExport]{}, "", jobs.Waiting)
}
